from __future__ import annotations

import os
import signal
import threading
import time
from datetime import datetime, timezone
from typing import Any, Callable

from .config import AppConfig
from .gpt_gateway import GmailGPTGateway
from .locking import RunnerLock
from .matrix_console import MatrixRunnerConsole
from .observability import RunnerActivityWatcher
from .runner import CWapiRunner
from .security import safe_error
from .state.runtime_store import RuntimeStateStore
from .transports.event_watcher import TransportEventWatcher
from .transports.gmail_drafts import (
    GmailDraftTransport,
    clear_managed_go_client,
    install_managed_go_client,
)
from .transports.go_runtime import GoTransportRuntime


_REPORTABLE_POLL_KEYS = (
    "claimed",
    "completed",
    "failed",
    "rejected",
    "result_retried",
    "cancel_requested",
)
_TERMINAL_POLL_KEYS = ("completed", "failed", "rejected")


def has_reportable_poll_activity(counts: dict[str, int]) -> bool:
    """Return whether a poll produced new user-visible activity."""

    return any(int(counts.get(key, 0)) > 0 for key in _REPORTABLE_POLL_KEYS)


def has_terminal_poll_activity(counts: dict[str, int]) -> bool:
    return any(int(counts.get(key, 0)) > 0 for key in _TERMINAL_POLL_KEYS)


class RunnerService:
    def __init__(self, config: AppConfig, *, show_output: bool = False) -> None:
        if not config.runtime.enabled:
            raise RuntimeError("runner-start 需要 runtime.enabled=true。")
        self.config = config
        self.transport_runtime = GoTransportRuntime.from_config(config)
        go_client = self.transport_runtime.start()
        install_managed_go_client(go_client)
        try:
            self.runner = CWapiRunner(config)
        except Exception:
            clear_managed_go_client(go_client)
            self.transport_runtime.close()
            raise
        if not isinstance(self.runner.store, RuntimeStateStore):
            self.runner.transport.close()
            clear_managed_go_client(go_client)
            self.transport_runtime.close()
            raise RuntimeError("v1 服务需要 RuntimeStateStore。")
        self.stop_event = threading.Event()
        self.gpt_gateway = GmailGPTGateway(self)
        self.last_task_poll_monotonic = 0.0
        self.last_cancel_poll_monotonic = 0.0
        self.last_cleanup_monotonic = 0.0
        self.runner_lock = RunnerLock(config.state.database_path.with_suffix(".runner.lock"))
        self.console = MatrixRunnerConsole(config.storage.logs_path / "runtime" / "runner.log")
        self.show_output = show_output
        self.activity_watcher: RunnerActivityWatcher | None = None
        self.transport_watcher: TransportEventWatcher | None = None
        self.cancel_watcher: threading.Thread | None = None
        self.configuration_stale = False
        self.settings_pending = False
        self.pending_settings_callback: Callable[[], dict[str, Any]] | None = None

    def request_stop(self, *_: Any) -> None:
        if not self.stop_event.is_set():
            self.console.emit("STOP", "收到停止信号，等待当前受控操作收尾。")
        self.stop_event.set()

    def _install_signal_handlers(self) -> None:
        signal.signal(signal.SIGINT, self.request_stop)
        if hasattr(signal, "SIGTERM"):
            signal.signal(signal.SIGTERM, self.request_stop)

    def _heartbeat(
        self,
        *,
        status: str,
        details: dict[str, Any] | None = None,
        polled: bool = False,
        cleaned: bool = False,
    ) -> None:
        now = datetime.now(timezone.utc).isoformat()
        self.runner.store.heartbeat(
            runner_id=self.config.runner.runner_id,
            pid=os.getpid(),
            status=status,
            last_poll_at=now if polled else None,
            last_cleanup_at=now if cleaned else None,
            details=details,
        )

    def run_forever(self) -> None:
        self.runner_lock.acquire()
        try:
            self._run_locked()
        finally:
            self.runner_lock.release()

    def _run_task_poll(self, now: float) -> None:
        settings_pending = bool(getattr(self, "settings_pending", False))
        pending_settings_callback = getattr(self, "pending_settings_callback", None)
        if settings_pending and pending_settings_callback is not None:
            try:
                applied = pending_settings_callback()
                if applied.get("applied"):
                    self.console.emit(
                        "SETTINGS", "已在 TASK 空闲边界自动应用 pending 设置。", details=applied
                    )
            except Exception as exc:
                self._heartbeat(
                    status="settings_pending",
                    details={"settings_pending": True, "apply_error": safe_error(exc)},
                )
                self.last_task_poll_monotonic = now
                return
        configuration_stale = bool(getattr(self, "configuration_stale", False))
        settings_pending = bool(getattr(self, "settings_pending", False))
        if configuration_stale or settings_pending:
            self._heartbeat(
                status="configuration_stale" if configuration_stale else "settings_pending",
                details={
                    "restart_required": configuration_stale,
                    "settings_pending": settings_pending,
                },
            )
            self.last_task_poll_monotonic = now
            return
        try:
            gateway_counts: dict[str, int] | None = None
            gateway = getattr(self, "gpt_gateway", None)
            if gateway is not None:
                try:
                    gateway_counts = gateway.poll()
                    if any(
                        int(gateway_counts.get(key, 0)) > 0
                        for key in (
                            "new",
                            "resumed",
                            "conflicts",
                            "responses_created",
                            "responses_reused",
                            "responses_failed",
                            "poll_errors",
                        )
                    ):
                        self.console.emit(
                            "GPT",
                            "Gmail GPT REQUEST 轮询发现活动。",
                            details=gateway_counts,
                        )
                except Exception as exc:
                    gateway_counts = {"poll_errors": 1}
                    self.console.emit(
                        "GPT",
                        "Gmail GPT REQUEST 轮询失败，将自动重试。",
                        level="WARN",
                        details={"error": safe_error(exc, limit=200)},
                    )
            counts = self.runner.run_once()
            self._heartbeat(
                status="running",
                details={
                    "last_counts": counts,
                    "gpt_gateway_counts": gateway_counts,
                },
                polled=True,
            )
            if has_reportable_poll_activity(counts):
                self.console.emit(
                    "POLL",
                    "Gmail TASK 轮询发现活动。",
                    details=counts,
                )
            if has_terminal_poll_activity(counts):
                self.console.persist_snapshot(reason="task_terminal")
        except Exception as exc:
            error = safe_error(exc)
            self._heartbeat(
                status="degraded",
                details={"last_error": error},
                polled=True,
            )
            self.console.emit(
                "POLL",
                "Gmail TASK 轮询失败，将自动重试。",
                level="ERROR",
                details={"error": error},
            )
        finally:
            self.last_task_poll_monotonic = now

    def _run_cancel_poll(
        self,
        now: float,
        *,
        transport: GmailDraftTransport | None = None,
    ) -> None:
        try:
            recorded = self.runner.poll_cancellations(transport=transport)
            if recorded:
                self.console.emit(
                    "CANCEL",
                    "已记录 Gmail 取消请求。",
                    details={"count": recorded},
                )
        except Exception as exc:
            error = safe_error(exc, limit=200)
            self.runner.store.record_event(
                task_id=None,
                direction="inbound",
                message_type="CANCEL",
                external_id=None,
                status="poll_failed",
                error_message=error,
            )
            self.console.emit(
                "CANCEL",
                "取消草稿轮询失败，将自动重试。",
                level="WARN",
                details={"error": error},
            )
        finally:
            self.last_cancel_poll_monotonic = now

    def _run_cancel_watcher(self) -> None:
        transport = self.runner.transport
        while not self.stop_event.is_set():
            self._run_cancel_poll(
                time.monotonic(),
                transport=transport,
            )
            self.stop_event.wait(self.config.runner.cancel_poll_seconds)

    def _run_cleanup(self, now: float) -> None:
        try:
            cleanup = self.runner.cleanup()
            self._heartbeat(
                status="running",
                details={"cleanup": cleanup},
                cleaned=True,
            )
            if cleanup.get("worktrees") or cleanup.get("artifacts"):
                self.console.emit("CLEANUP", "受控清理完成。", details=cleanup)
        except Exception as exc:
            error = safe_error(exc)
            self._heartbeat(
                status="degraded",
                details={"cleanup_error": error},
                cleaned=True,
            )
            self.console.emit(
                "CLEANUP",
                "受控清理失败，将在下个周期重试。",
                level="WARN",
                details={"error": error},
            )
        finally:
            self.last_cleanup_monotonic = now

    def _next_wait_seconds(self, now: float) -> float:
        deadlines = [
            self.last_task_poll_monotonic + self.config.runner.poll_interval_seconds,
            self.last_cleanup_monotonic + self.config.runner.cleanup_interval_seconds,
        ]
        return max(0.1, min(deadlines) - now)

    def _run_locked(self) -> None:
        self._install_signal_handlers()
        self.runner.store.initialize()
        go_client = self.runner.transport.go_client
        runtime_snapshot = self.transport_runtime.snapshot()
        self.console.emit(
            "START",
            "CWapi Runner 已启动。",
            details={
                "runner_id": self.config.runner.runner_id,
                "pid": os.getpid(),
                "poll_seconds": self.config.runner.poll_interval_seconds,
                "cancel_poll_seconds": self.config.runner.cancel_poll_seconds,
                "show_output": self.show_output,
                "log_path": str(self.console.log_path),
                "gmail_access_mode": "go_service",
                "go_transport_pid": runtime_snapshot.pid,
                "go_transport_url": runtime_snapshot.url,
                "go_transport_version": runtime_snapshot.version,
                "go_transport_authentication": runtime_snapshot.authentication_enabled,
            },
        )

        recovery = self.runner.recover()
        self._heartbeat(status="starting", details={"recovery": recovery})
        self.console.emit("RECOVER", "启动恢复完成。", details=recovery)

        self.activity_watcher = RunnerActivityWatcher(
            store=self.runner.store,
            logs_path=self.config.storage.logs_path,
            console=self.console,
            stop_event=self.stop_event,
            show_output=self.show_output,
        )
        self.activity_watcher.start()

        self.transport_watcher = TransportEventWatcher(
            client=go_client,
            console=self.console,
            stop_event=self.stop_event,
        )
        self.transport_watcher.start()

        if self.config.runtime.enable_cancel_drafts:
            self.console.emit(
                "CANCEL",
                "Gmail 取消草稿监视已启用。",
                details={
                    "poll_seconds": self.config.runner.cancel_poll_seconds,
                    "access_mode": "go_service",
                },
            )
            self.cancel_watcher = threading.Thread(
                target=self._run_cancel_watcher,
                name="cwapi-cancel-watcher",
                daemon=True,
            )
            self.cancel_watcher.start()

        try:
            while not self.stop_event.is_set():
                now = time.monotonic()
                task_due = (
                    now - self.last_task_poll_monotonic >= self.config.runner.poll_interval_seconds
                )
                cleanup_due = (
                    now - self.last_cleanup_monotonic >= self.config.runner.cleanup_interval_seconds
                )

                if task_due:
                    self._run_task_poll(now)

                if cleanup_due:
                    self._run_cleanup(now)

                self.stop_event.wait(self._next_wait_seconds(time.monotonic()))
        finally:
            self.stop_event.set()
            if self.cancel_watcher is not None:
                self.cancel_watcher.join(timeout=10)
            if self.transport_watcher is not None:
                self.transport_watcher.join(timeout=10)
            if self.activity_watcher is not None:
                self.activity_watcher.join(timeout=10)
            self.runner.transport.close()
            clear_managed_go_client(go_client)
            self.transport_runtime.close()
            self._heartbeat(status="stopped")
            self.console.emit("STOP", "CWapi Runner 已停止。")
            self.console.persist_snapshot(reason="stop")
