from __future__ import annotations

import json
import os
import shutil
import sys
import threading
from datetime import datetime
from pathlib import Path
from typing import Any, TextIO

from .execution.live_logs import LIVE_LOGS
from .state.runtime_store import RuntimeStateStore


_ANSI_CODES = {
    "reset": "0",
    "bold": "1",
    "dim": "2",
    "red": "31",
    "green": "32",
    "yellow": "33",
    "blue": "34",
    "magenta": "35",
    "cyan": "36",
    "gray": "90",
    "bright_red": "91",
    "bright_green": "92",
    "bright_yellow": "93",
    "bright_blue": "94",
    "bright_magenta": "95",
    "bright_cyan": "96",
}

_CATEGORY_STYLES: dict[str, tuple[str, str]] = {
    "START": ("◆", "bright_cyan"),
    "RECOVER": ("↺", "bright_blue"),
    "TASK": ("◆", "bright_magenta"),
    "WORKTREE": ("◇", "cyan"),
    "STEP": ("●", "bright_blue"),
    "ACTIVE": ("◌", "bright_cyan"),
    "RESULT": ("◎", "bright_green"),
    "ARTIFACT": ("⬢", "bright_blue"),
    "POLL": ("↻", "cyan"),
    "CANCEL": ("!", "bright_yellow"),
    "CLEANUP": ("⌁", "green"),
    "TRANSPORT": ("↯", "bright_blue"),
    "STOP": ("■", "gray"),
    "OUT": ("│", "gray"),
    "ERR": ("│", "bright_yellow"),
    "WATCH": ("!", "bright_yellow"),
}

_ASCII_ICONS = {
    "◆": "*",
    "↺": "~",
    "◇": "+",
    "●": ">",
    "◌": ".",
    "◎": "@",
    "⬢": "#",
    "↻": "~",
    "⌁": "~",
    "↯": "!",
    "■": "#",
    "│": "|",
    "✓": "+",
    "✕": "x",
}

_DETAIL_LABELS = {
    "task_id": "task",
    "repository": "repo",
    "runner_id": "runner",
    "expected_commit": "expected",
    "actual_commit": "actual",
    "commit": "commit",
    "execution": "exec",
    "result": "result",
    "progress": "progress",
    "workspace": "workspace",
    "path": "path",
    "worktree_path": "worktree",
    "local_path": "local",
    "drive_path": "drive",
    "drive_relative_path": "drive",
    "manifest": "manifest",
    "manifest_sha256": "manifest",
    "gmail_access_mode": "gmail",
    "access_mode": "mode",
    "poll_seconds": "poll",
    "cancel_poll_seconds": "cancel-poll",
    "show_output": "tail",
    "log_path": "log",
    "exit_code": "exit",
    "duration_ms": "duration",
    "error_code": "code",
    "error": "error",
    "stdout": "stdout",
    "stderr": "stderr",
    "count": "count",
    "pid": "pid",
}

_DETAIL_PRIORITY = (
    "task_id",
    "repository",
    "runner_id",
    "commit",
    "expected_commit",
    "actual_commit",
    "execution",
    "result",
    "exit_code",
    "duration_ms",
    "error_code",
    "gmail_access_mode",
    "access_mode",
    "poll_seconds",
    "cancel_poll_seconds",
    "show_output",
    "count",
    "pid",
    "path",
    "worktree_path",
    "local_path",
    "drive_path",
    "drive_relative_path",
    "manifest",
    "manifest_sha256",
    "stdout",
    "stderr",
    "log_path",
    "error",
)

_MAJOR_CATEGORIES = frozenset({"START", "TASK", "RESULT", "STOP"})


class RunnerConsole:
    """Thread-safe themed terminal output backed by an in-memory audit buffer."""

    def __init__(
        self,
        log_path: Path,
        *,
        stream: TextIO | None = None,
        max_bytes: int = 5 * 1024 * 1024,
        color: bool | None = None,
        console_width: int | None = None,
    ) -> None:
        self.log_path = log_path
        self.stream = stream or sys.stdout
        self.max_bytes = max_bytes
        self.color = self._detect_color() if color is None else color
        self.console_width = console_width
        self._unicode = self._detect_unicode()
        self._lock = threading.RLock()
        self._has_console_output = False

    def emit(
        self,
        category: str,
        message: str,
        *,
        level: str = "INFO",
        details: dict[str, Any] | None = None,
    ) -> None:
        now = datetime.now().astimezone()
        normalized_category = str(category).upper()
        normalized_level = str(level).upper()
        audit_line = self._audit_line(
            now,
            normalized_category,
            normalized_level,
            message,
            details,
        )
        console_lines = self._console_lines(
            now,
            normalized_category,
            normalized_level,
            message,
            details,
        )
        with self._lock:
            if normalized_category in _MAJOR_CATEGORIES and self._has_console_output:
                print("", file=self.stream, flush=True)
            for line in console_lines:
                print(line, file=self.stream, flush=True)
            self._has_console_output = True
            LIVE_LOGS.append_runtime(audit_line)
        if normalized_level == "ERROR":
            self.persist_snapshot(reason="error")

    def persist_snapshot(self, *, reason: str = "terminal") -> None:
        """Write the bounded Runner audit snapshot once at a terminal/error boundary."""
        del reason
        lines = LIVE_LOGS.runtime_lines()
        if not lines:
            return
        payload = ("\n".join(lines) + "\n").encode("utf-8", errors="replace")
        if len(payload) > self.max_bytes:
            payload = payload[-self.max_bytes :]
            newline = payload.find(b"\n")
            if newline >= 0:
                payload = payload[newline + 1 :]
        with self._lock:
            self.log_path.parent.mkdir(parents=True, exist_ok=True)
            backup = self.log_path.with_suffix(self.log_path.suffix + ".1")
            if self.log_path.exists():
                backup.unlink(missing_ok=True)
                self.log_path.replace(backup)
            temporary = self.log_path.with_suffix(self.log_path.suffix + ".tmp")
            temporary.write_bytes(payload)
            temporary.replace(self.log_path)

    def memory_snapshot(self, *, tail_bytes: int) -> tuple[int, str]:
        return LIVE_LOGS.runtime_text(tail_bytes=tail_bytes)

    def _audit_line(
        self,
        now: datetime,
        category: str,
        level: str,
        message: str,
        details: dict[str, Any] | None,
    ) -> str:
        timestamp = now.strftime("%Y-%m-%d %H:%M:%S")
        suffix = ""
        if details:
            suffix = " " + json.dumps(
                details,
                ensure_ascii=False,
                sort_keys=True,
                default=str,
            )
        return f"[{timestamp}] [{level}] [{category}] {message}{suffix}"

    def _console_lines(
        self,
        now: datetime,
        category: str,
        level: str,
        message: str,
        details: dict[str, Any] | None,
    ) -> list[str]:
        icon, category_color = self._event_style(category, level, message)
        if not self._unicode:
            icon = _ASCII_ICONS.get(icon, "*")
        timestamp = self._paint(now.strftime("%H:%M:%S"), "dim")
        category_badge = self._paint(f"{icon} {category:<9}", category_color, "bold")
        message_style = (
            ("bright_red", "bold")
            if level == "ERROR"
            else ("bright_yellow",)
            if level == "WARN"
            else ()
        )
        headline = f"{timestamp}  {category_badge} {self._paint(message, *message_style)}"
        lines = [headline.rstrip()]
        detail_rows = self._format_detail_rows(details or {})
        for index, row in enumerate(detail_rows):
            connector = "└" if index == len(detail_rows) - 1 else "├"
            if not self._unicode:
                connector = "`" if index == len(detail_rows) - 1 else "|"
            lines.append(self._paint(f"          {connector} {row}", "dim"))
        return lines

    def _event_style(self, category: str, level: str, message: str) -> tuple[str, str]:
        if level == "ERROR":
            return "✕", "bright_red"
        if level == "WARN":
            return "!", "bright_yellow"
        if category == "STEP" and "completed" in message.casefold():
            return "✓", "bright_green"
        if category == "RESULT" and any(
            marker in message.casefold() for marker in ("completed", "uploaded", "delivered")
        ):
            return "✓", "bright_green"
        return _CATEGORY_STYLES.get(category, ("●", "bright_blue"))

    def _format_detail_rows(self, details: dict[str, Any]) -> list[str]:
        if not details:
            return []
        width = self.console_width or shutil.get_terminal_size(fallback=(120, 24)).columns
        available = max(36, width - 14)
        ordered_keys = [key for key in _DETAIL_PRIORITY if key in details]
        ordered_keys.extend(sorted(key for key in details if key not in ordered_keys))
        tokens: list[str] = []
        for key in ordered_keys:
            value = details.get(key)
            if value is None or value == "":
                continue
            label = _DETAIL_LABELS.get(key, key.replace("_", "-"))
            rendered = self._format_detail_value(key, value, max_length=min(72, available))
            tokens.append(f"{label}={rendered}")
        if not tokens:
            return []

        separator = "  ·  " if self._unicode else "  |  "
        rows: list[str] = []
        current = ""
        for token in tokens:
            candidate = token if not current else current + separator + token
            if current and len(candidate) > available:
                rows.append(current)
                current = token
            else:
                current = candidate
        if current:
            rows.append(current)
        return rows

    def _format_detail_value(self, key: str, value: Any, *, max_length: int) -> str:
        if isinstance(value, bool):
            text = "on" if value else "off"
        elif key == "duration_ms" and isinstance(value, (int, float)):
            text = f"{value:.0f}ms" if value < 1000 else f"{value / 1000:.1f}s"
        elif key in {"poll_seconds", "cancel_poll_seconds"} and isinstance(
            value, (int, float)
        ):
            text = f"{value:g}s"
        elif isinstance(value, (dict, list, tuple)):
            text = json.dumps(
                value,
                ensure_ascii=False,
                sort_keys=True,
                separators=(",", ":"),
                default=str,
            )
        else:
            text = " ".join(str(value).split())
        return self._shorten_middle(text, max_length)

    @staticmethod
    def _shorten_middle(text: str, max_length: int) -> str:
        if len(text) <= max_length:
            return text
        if max_length < 12:
            return text[:max_length]
        head = max_length // 2 - 1
        tail = max_length - head - 1
        return f"{text[:head]}…{text[-tail:]}"

    def _paint(self, text: str, *styles: str) -> str:
        if not self.color or not styles:
            return text
        codes = [_ANSI_CODES[style] for style in styles if style in _ANSI_CODES]
        if not codes:
            return text
        return f"\x1b[{';'.join(codes)}m{text}\x1b[0m"

    def _detect_color(self) -> bool:
        if "NO_COLOR" in os.environ:
            return False
        forced = os.environ.get("CLICOLOR_FORCE", "")
        if forced and forced != "0":
            return True
        if os.environ.get("TERM", "").casefold() == "dumb":
            return False
        isatty = getattr(self.stream, "isatty", None)
        return bool(callable(isatty) and isatty())

    def _detect_unicode(self) -> bool:
        encoding = getattr(self.stream, "encoding", None) or "utf-8"
        try:
            "◆✓✕…│└·".encode(encoding)
        except (LookupError, UnicodeEncodeError):
            return False
        return True


class RunnerActivityWatcher(threading.Thread):
    """Observe task state and in-memory process output while run_once blocks."""

    def __init__(
        self,
        *,
        store: RuntimeStateStore,
        logs_path: Path,
        console: RunnerConsole,
        stop_event: threading.Event,
        show_output: bool = False,
        poll_seconds: float = 0.5,
        alive_seconds: float = 15.0,
    ) -> None:
        super().__init__(name="cwapi-activity-watcher", daemon=True)
        self.store = store
        self.logs_path = logs_path  # compatibility only; live output is memory-backed
        self.console = console
        self.stop_event = stop_event
        self.show_output = show_output
        self.poll_seconds = poll_seconds
        self.alive_seconds = alive_seconds
        self._task_state: dict[str, tuple[Any, ...]] = {}
        self._step_state: dict[str, dict[str, tuple[Any, ...]]] = {}
        self._workspace_state: dict[str, tuple[Any, ...]] = {}
        self._artifact_state: dict[str, tuple[Any, ...]] = {}
        self._last_activity_report: dict[str, float] = {}
        self._output_offsets: dict[tuple[str, str, str], int] = {}
        self._initial_poll = True

    def run(self) -> None:
        while not self.stop_event.is_set():
            try:
                self.poll_once()
            except Exception as exc:  # pragma: no cover - defensive service guard
                self.console.emit(
                    "WATCH",
                    "活动监视失败，将在下一轮重试。",
                    level="WARN",
                    details={"error": str(exc)[:300]},
                )
            self.stop_event.wait(self.poll_seconds)

    def poll_once(self) -> None:
        now = datetime.now().timestamp()
        tasks = self.store.list_tasks(limit=100)
        workspaces = {
            str(item["task_id"]): item for item in self.store.list_workspaces(limit=100)
        }

        for summary in reversed(tasks):
            task_id = str(summary["task_id"])
            task = self.store.get_task(task_id)
            if task is None:
                continue
            payload = self._task_payload(task)
            steps_spec = list(payload.get("steps", [])) if payload else []
            step_rows = self.store.get_task_steps(task_id)

            self._report_task(task, initial=self._initial_poll)
            self._report_steps(task, steps_spec, step_rows, initial=self._initial_poll)
            self._report_workspace(task_id, workspaces.get(task_id), initial=self._initial_poll)
            self._report_artifact(task_id, initial=self._initial_poll)

            if str(task.get("execution_status")) == "running":
                previous = self._last_activity_report.get(task_id, 0.0)
                if now - previous >= self.alive_seconds:
                    self._report_running(task, steps_spec, step_rows)
                    self._last_activity_report[task_id] = now
                if self.show_output:
                    self._tail_current_output(task_id, steps_spec, step_rows)

        self._initial_poll = False

    @staticmethod
    def _task_payload(task: dict[str, Any]) -> dict[str, Any]:
        raw = task.get("task_json")
        if not raw:
            return {}
        try:
            value = json.loads(str(raw))
        except json.JSONDecodeError:
            return {}
        return value if isinstance(value, dict) else {}

    def _report_task(self, task: dict[str, Any], *, initial: bool) -> None:
        task_id = str(task["task_id"])
        current = (
            task.get("execution_status"),
            task.get("result_status"),
            task.get("progress_status"),
            task.get("cancel_requested"),
            task.get("workspace_path"),
            task.get("artifact_path"),
            task.get("last_error"),
        )
        previous = self._task_state.get(task_id)
        self._task_state[task_id] = current

        active = str(task.get("execution_status")) in {"claimed", "running"}
        pending = str(task.get("result_status")) == "pending"
        if previous is None:
            if initial and not (active or pending):
                return
            self.console.emit(
                "TASK",
                "发现活动任务。" if initial else "认领新任务。",
                details={
                    "task_id": task_id,
                    "repository": task.get("repository"),
                    "commit": str(task.get("expected_commit", ""))[:12],
                    "execution": task.get("execution_status"),
                    "result": task.get("result_status"),
                },
            )
            return
        if previous == current:
            return

        if previous[0] != current[0]:
            level = "ERROR" if current[0] == "failed" else "INFO"
            self.console.emit(
                "TASK",
                f"执行状态变为 {current[0]}。",
                level=level,
                details={"task_id": task_id, "error": task.get("last_error") or None},
            )
        if previous[1] != current[1]:
            level = "WARN" if current[1] == "pending" else "INFO"
            self.console.emit(
                "RESULT",
                f"RESULT 状态变为 {current[1]}。",
                level=level,
                details={"task_id": task_id},
            )
        if not previous[3] and current[3]:
            self.console.emit(
                "CANCEL",
                "收到取消请求。",
                level="WARN",
                details={"task_id": task_id},
            )

    def _report_steps(
        self,
        task: dict[str, Any],
        steps_spec: list[Any],
        step_rows: list[dict[str, object]],
        *,
        initial: bool,
    ) -> None:
        task_id = str(task["task_id"])
        known = self._step_state.setdefault(task_id, {})
        total = len(steps_spec) or len(step_rows)
        active = str(task.get("execution_status")) in {"claimed", "running"}
        for row in step_rows:
            step_id = str(row["step_id"])
            current = (
                row.get("execution_status"),
                row.get("exit_code"),
                row.get("duration_ms"),
                row.get("finished_at"),
            )
            previous = known.get(step_id)
            known[step_id] = current
            if previous == current or (initial and not active):
                continue
            status = str(row.get("execution_status"))
            level = "INFO" if status in {"running", "completed"} else "ERROR"
            ordinal = int(row.get("ordinal") or 0) + 1
            self.console.emit(
                "STEP",
                f"{ordinal}/{total} {step_id} ({row.get('action')}) -> {status}",
                level=level,
                details={
                    "task_id": task_id,
                    "exit_code": row.get("exit_code"),
                    "duration_ms": row.get("duration_ms"),
                    "error_code": row.get("error_code"),
                },
            )

    def _report_running(
        self,
        task: dict[str, Any],
        steps_spec: list[Any],
        step_rows: list[dict[str, object]],
    ) -> None:
        task_id = str(task["task_id"])
        completed = sum(
            1 for row in step_rows if str(row.get("execution_status")) == "completed"
        )
        current = next(
            (row for row in step_rows if str(row.get("execution_status")) == "running"),
            None,
        )
        if current is not None:
            message = (
                f"仍在执行 {int(current.get('ordinal') or 0) + 1}/{len(steps_spec) or len(step_rows)} "
                f"{current.get('step_id')} ({current.get('action')})。"
            )
        elif completed < len(steps_spec) and isinstance(steps_spec[completed], dict):
            step = steps_spec[completed]
            message = (
                f"仍在执行 {completed + 1}/{len(steps_spec)} "
                f"{step.get('step_id')} ({step.get('action')})。"
            )
        else:
            message = "任务仍在执行，正在整理结果或产物。"
        self.console.emit("ACTIVE", message, details={"task_id": task_id})

    def _report_workspace(
        self,
        task_id: str,
        workspace: dict[str, Any] | None,
        *,
        initial: bool,
    ) -> None:
        if workspace is None:
            return
        current = (
            workspace.get("workspace_status"),
            workspace.get("worktree_path"),
            workspace.get("actual_commit"),
            workspace.get("last_error"),
        )
        previous = self._workspace_state.get(task_id)
        self._workspace_state[task_id] = current
        if previous == current or (previous is None and initial and current[0] not in {"ready"}):
            return
        self.console.emit(
            "WORKTREE",
            f"工作区状态为 {current[0]}。",
            details={
                "task_id": task_id,
                "path": current[1],
                "commit": str(current[2] or "")[:12],
                "error": current[3],
            },
        )

    def _report_artifact(self, task_id: str, *, initial: bool) -> None:
        artifact = self.store.get_artifact(task_id)
        if artifact is None:
            return
        current = (
            artifact.get("sync_status"),
            artifact.get("local_path"),
            artifact.get("drive_relative_path"),
            artifact.get("manifest_sha256"),
        )
        previous = self._artifact_state.get(task_id)
        self._artifact_state[task_id] = current
        if previous == current or (previous is None and initial):
            return
        self.console.emit(
            "ARTIFACT",
            f"结果包状态为 {current[0]}。",
            details={
                "task_id": task_id,
                "local_path": current[1],
                "drive_path": current[2],
                "manifest": current[3],
            },
        )

    def _tail_current_output(
        self,
        task_id: str,
        steps_spec: list[Any],
        step_rows: list[dict[str, object]],
    ) -> None:
        current = next(
            (row for row in step_rows if str(row.get("execution_status")) == "running"),
            None,
        )
        if current is not None:
            step_id = str(current.get("step_id") or "")
        else:
            completed = sum(
                1 for row in step_rows if str(row.get("execution_status")) == "completed"
            )
            if completed >= len(steps_spec) or not isinstance(steps_spec[completed], dict):
                return
            step_id = str(steps_spec[completed].get("step_id", f"step-{completed + 1}"))
        if not step_id:
            return
        for stream_name, category, level in (
            ("stdout", "OUT", "INFO"),
            ("stderr", "ERR", "WARN"),
        ):
            key = (task_id, step_id, stream_name)
            after = self._output_offsets.get(key, 0)
            values = LIVE_LOGS.output_since(task_id, step_id, stream_name, after)
            for seq, line in values:
                self._output_offsets[key] = seq
                if line.strip():
                    self.console.emit(
                        category,
                        line.rstrip(),
                        level=level,
                        details={"task_id": task_id, "step_id": step_id},
                    )
