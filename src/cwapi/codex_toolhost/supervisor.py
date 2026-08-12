from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
import threading
import time
from typing import Callable, Iterator, Sequence, TYPE_CHECKING

from .client import CodexToolhostError, NotificationHandler
from .runtime_lock import RuntimeProvenance
from .session_client_impl import CodexSessionAppServerClient
from .windows_lock_diagnostics import (
    WindowsLockRecovery,
    describe_launch_os_error,
    recover_private_codex_lock,
)

if TYPE_CHECKING:
    from cwapi.config import CodexToolhostConfig


@dataclass(frozen=True)
class CodexToolhostSnapshot:
    state: str
    startup_count: int
    generation: int
    pid: int | None
    active_operations: int


def _client_is_running(client: CodexSessionAppServerClient | None) -> bool:
    if client is None or getattr(client, "_closed", False):
        return False
    process = getattr(client, "_process", None)
    return process is not None and process.poll() is None


def _client_pid(client: CodexSessionAppServerClient | None) -> int | None:
    if not _client_is_running(client):
        return None
    process = getattr(client, "_process", None)
    return int(process.pid) if process is not None else None


class CodexToolhostSupervisor:
    """Process-scoped owner of one long-lived private Codex app-server.

    A production Runner is single-instance, so the process scope is the Runner
    scope. Operations are serialized initially. Task isolation still comes from
    each command's cwd, environment, permission profile and process_id.
    """

    def __init__(
        self,
        *,
        config: "CodexToolhostConfig",
        allowed_environment_variables: Sequence[str],
        startup_attempts: int = 5,
        startup_backoff_seconds: float = 0.5,
        client_factory: Callable[..., CodexSessionAppServerClient] = (
            CodexSessionAppServerClient
        ),
    ) -> None:
        if startup_attempts < 1:
            raise ValueError("startup_attempts 必须大于 0。")
        if startup_backoff_seconds < 0:
            raise ValueError("startup_backoff_seconds 不能小于 0。")
        self.config = config
        self.allowed_environment_variables = tuple(allowed_environment_variables)
        self.startup_attempts = startup_attempts
        self.startup_backoff_seconds = startup_backoff_seconds
        self.client_factory = client_factory

        self._state_lock = threading.RLock()
        self._operation_lock = threading.RLock()
        self._client: CodexSessionAppServerClient | None = None
        self._state = "stopped"
        self._startup_count = 0
        self._generation = 0
        self._active_operations = 0
        self._closed = False

    @property
    def runtime_provenance(self) -> RuntimeProvenance | None:
        with self._state_lock:
            client = self._client
            return client.runtime_provenance if client is not None else None

    def snapshot(self) -> CodexToolhostSnapshot:
        with self._state_lock:
            return CodexToolhostSnapshot(
                state=self._state,
                startup_count=self._startup_count,
                generation=self._generation,
                pid=_client_pid(self._client),
                active_operations=self._active_operations,
            )

    def _new_client(
        self,
        notification_handler: NotificationHandler | None = None,
    ) -> CodexSessionAppServerClient:
        return self.client_factory(
            executable_path=self.config.executable_path,
            codex_home=self.config.home_path,
            stderr_log_path=self.config.stderr_log_path,
            allowed_environment_variables=self.allowed_environment_variables,
            startup_timeout_seconds=self.config.startup_timeout_seconds,
            notification_handler=notification_handler,
        )

    @staticmethod
    def _root_os_error(exc: BaseException) -> OSError | None:
        current: BaseException | None = exc
        while current is not None:
            if isinstance(current, OSError):
                return current
            current = current.__cause__
        return None

    @classmethod
    def _is_windows_sharing_violation(cls, exc: BaseException) -> bool:
        current: BaseException | None = exc
        while current is not None:
            if getattr(current, "winerror", None) == 32:
                return True
            text = str(current).lower()
            if "winerror 32" in text or "sharing violation" in text:
                return True
            current = current.__cause__
        return False

    @staticmethod
    def _recovery_details(recovery: WindowsLockRecovery | None) -> str:
        return recovery.describe() if recovery is not None else "lock_recovery=not-run"

    def _start_locked(self) -> CodexSessionAppServerClient:
        if self._closed:
            raise CodexToolhostError("Codex Toolhost Supervisor 已关闭。")
        if _client_is_running(self._client):
            self._state = "healthy"
            assert self._client is not None
            return self._client

        stale = self._client
        self._client = None
        if stale is not None:
            try:
                stale.close()
            except Exception:
                pass

        self._state = "starting"
        last_error: BaseException | None = None
        last_recovery: WindowsLockRecovery | None = None
        for attempt in range(1, self.startup_attempts + 1):
            client = self._new_client()
            try:
                client.start()
            except BaseException as exc:
                last_error = exc
                try:
                    client.close()
                except Exception:
                    pass
                retryable = self._is_windows_sharing_violation(exc)
                if retryable:
                    try:
                        last_recovery = recover_private_codex_lock(
                            self.config.executable_path
                        )
                    except Exception as recovery_exc:
                        last_recovery = WindowsLockRecovery(
                            holders=(),
                            terminated_pids=(),
                            termination_errors=(str(recovery_exc),),
                        )

                if not retryable or attempt >= self.startup_attempts:
                    self._state = "unhealthy"
                    root_error = self._root_os_error(exc)
                    os_details = (
                        describe_launch_os_error(root_error)
                        if root_error is not None
                        else describe_launch_os_error(exc)
                    )
                    raise CodexToolhostError(
                        "启动长期 Codex app-server 失败"
                        f"（attempt={attempt}/{self.startup_attempts}）：{exc}; "
                        f"{os_details}; {self._recovery_details(last_recovery)}"
                    ) from exc

                time.sleep(self.startup_backoff_seconds * (2 ** (attempt - 1)))
                continue

            self._client = client
            self._startup_count += 1
            self._generation += 1
            self._state = "healthy"
            return client

        self._state = "unhealthy"
        raise CodexToolhostError(
            f"启动长期 Codex app-server 失败：{last_error}; "
            f"{self._recovery_details(last_recovery)}"
        ) from last_error

    @contextmanager
    def operation(
        self,
        *,
        notification_handler: NotificationHandler | None = None,
    ) -> Iterator[CodexSessionAppServerClient]:
        """Borrow the shared client for one serialized high-level operation."""

        with self._operation_lock:
            with self._state_lock:
                if self._closed or self._state in {"draining", "stopping"}:
                    raise CodexToolhostError(
                        f"Codex Toolhost 当前不可接收操作：{self._state}"
                    )
                client = self._start_locked()
                self._active_operations += 1
                previous_handler = client.notification_handler
                client.notification_handler = notification_handler
            try:
                yield client
            finally:
                with self._state_lock:
                    client.notification_handler = previous_handler
                    self._active_operations = max(0, self._active_operations - 1)
                    if not _client_is_running(client):
                        if self._client is client:
                            self._client = None
                        if not self._closed:
                            self._state = "unhealthy"

    def invalidate(self) -> None:
        """Discard the current client after a transport-level failure."""

        with self._operation_lock:
            with self._state_lock:
                client = self._client
                self._client = None
                if not self._closed:
                    self._state = "unhealthy"
            if client is not None:
                client.close()

    def close(self) -> None:
        with self._operation_lock:
            with self._state_lock:
                if self._closed:
                    return
                self._state = "stopping"
                self._closed = True
                client = self._client
                self._client = None
            if client is not None:
                client.close()
            with self._state_lock:
                self._state = "stopped"

    def __enter__(self) -> "CodexToolhostSupervisor":
        return self

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self.close()
