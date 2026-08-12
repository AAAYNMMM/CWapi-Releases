from __future__ import annotations

from pathlib import Path
import threading
from typing import Any, Mapping, Sequence

from .client import CodexAppServerClient, CodexToolhostError, NotificationHandler


_WORKSPACE_STATUS_ARGUMENTS = (
    "status",
    "--porcelain=v1",
    "--untracked-files=all",
)


def _current_task_id() -> str | None:
    # Keep the low-level client independent from the execution package during
    # module import. The import occurs only when an MCP call is made.
    from cwapi.execution.task_policy_snapshot import current_task_id

    return current_task_id()


def _mcp_error_message(result: Any) -> str | None:
    if not isinstance(result, Mapping) or result.get("isError") is not True:
        return None

    messages: list[str] = []
    content = result.get("content")
    if isinstance(content, list):
        for item in content:
            if not isinstance(item, Mapping):
                continue
            text = item.get("text")
            if isinstance(text, str) and text.strip():
                messages.append(text.strip())

    return "\n".join(messages) or "MCP tool returned isError=true."


class CodexSessionAppServerClient(CodexAppServerClient):
    """Long-lived client with task-scoped MCP and diagnostic reuse."""

    def __init__(
        self,
        *,
        executable_path: Path,
        codex_home: Path,
        stderr_log_path: Path,
        allowed_environment_variables: Sequence[str],
        startup_timeout_seconds: int = 30,
        notification_handler: NotificationHandler | None = None,
    ) -> None:
        super().__init__(
            executable_path=executable_path,
            codex_home=codex_home,
            stderr_log_path=stderr_log_path,
            allowed_environment_variables=allowed_environment_variables,
            startup_timeout_seconds=startup_timeout_seconds,
            notification_handler=notification_handler,
        )
        self._task_thread_lock = threading.RLock()
        self._task_threads: dict[tuple[str, str, str], str] = {}
        self._last_mcp_task_id: str | None = None
        self._diagnostic_lock = threading.RLock()
        self._version_cache: dict[tuple[tuple[str, ...], str, str], Any] = {}
        self._workspace_status_cache: dict[tuple[str, str], Any] = {}

    @staticmethod
    def _is_version_command(command: Sequence[str]) -> bool:
        return len(command) == 2 and command[1] == "--version"

    @staticmethod
    def _is_git_executable(value: str) -> bool:
        return Path(value).name.casefold() in {"git", "git.exe"}

    @classmethod
    def _is_workspace_status_command(
        cls,
        command: Sequence[str],
    ) -> bool:
        return (
            len(command) == 4
            and cls._is_git_executable(command[0])
            and tuple(command[1:]) == _WORKSPACE_STATUS_ARGUMENTS
        )

    @classmethod
    def _is_read_only_command(cls, command: Sequence[str]) -> bool:
        if cls._is_version_command(command) or cls._is_workspace_status_command(command):
            return True
        if len(command) >= 2 and cls._is_git_executable(command[0]):
            return command[1] in {
                "rev-parse",
                "status",
                "diff",
                "show",
                "log",
                "cat-file",
            }
        return False

    def command_start(self, command: Sequence[str], **kwargs: Any):
        cwd = Path(kwargs["cwd"])
        profile = str(kwargs["permission_profile"])
        if not self._is_read_only_command(command):
            with self._diagnostic_lock:
                self._workspace_status_cache.pop(
                    (str(cwd.resolve()), profile),
                    None,
                )
        return super().command_start(command, **kwargs)

    def command_exec(self, command: Sequence[str], **kwargs: Any):
        cwd = Path(kwargs["cwd"])
        profile = str(kwargs["permission_profile"])
        cwd_key = str(cwd.resolve())
        version_key = (tuple(command), cwd_key, profile)
        status_key = (cwd_key, profile)

        with self._diagnostic_lock:
            if self._is_version_command(command) and version_key in self._version_cache:
                return self._version_cache[version_key]
            if (
                self._is_workspace_status_command(command)
                and status_key in self._workspace_status_cache
            ):
                return self._workspace_status_cache[status_key]

        response = super().command_exec(command, **kwargs)
        with self._diagnostic_lock:
            if self._is_version_command(command):
                self._version_cache[version_key] = response
            elif self._is_workspace_status_command(command):
                self._workspace_status_cache[status_key] = response
        return response

    def _unsubscribe_thread(self, thread_id: str) -> None:
        try:
            self.call_internal_method(
                "thread/unsubscribe",
                {"threadId": thread_id},
                timeout_seconds=15,
            )
        except CodexToolhostError:
            pass

    def release_task_threads(self, task_id: str) -> None:
        with self._task_thread_lock:
            selected = [
                (key, thread_id)
                for key, thread_id in self._task_threads.items()
                if key[0] == task_id
            ]
            for key, _ in selected:
                self._task_threads.pop(key, None)
        for _, thread_id in selected:
            self._unsubscribe_thread(thread_id)

    def _release_other_task_threads(self, task_id: str) -> None:
        with self._task_thread_lock:
            other_ids = {key[0] for key in self._task_threads if key[0] != task_id}
        for other_id in other_ids:
            self.release_task_threads(other_id)

    def _task_thread(
        self,
        *,
        task_id: str,
        cwd: Path,
        permission_profile: str,
        timeout_seconds: int,
    ) -> str:
        key = (task_id, str(cwd.resolve()), permission_profile)
        with self._task_thread_lock:
            existing = self._task_threads.get(key)
            if existing:
                return existing

        thread_result = self.call_internal_method(
            "thread/start",
            {
                "cwd": str(cwd),
                "ephemeral": True,
                "approvalPolicy": "never",
                "permissions": permission_profile,
            },
            timeout_seconds=timeout_seconds,
        )
        if not isinstance(thread_result, dict):
            raise CodexToolhostError("thread/start 返回值不是对象。")
        thread = thread_result.get("thread")
        if not isinstance(thread, dict) or not thread.get("id"):
            raise CodexToolhostError("thread/start 未返回 thread.id。")
        thread_id = str(thread["id"])
        with self._task_thread_lock:
            existing = self._task_threads.setdefault(key, thread_id)
        if existing != thread_id:
            self._unsubscribe_thread(thread_id)
        return existing

    def mcp_tool_call(
        self,
        *,
        server: str,
        tool: str,
        arguments: Mapping[str, Any] | None,
        cwd: Path,
        permission_profile: str,
        timeout_seconds: int = 60,
        meta: Mapping[str, Any] | None = None,
    ) -> Any:
        task_id = _current_task_id()
        if not task_id:
            result = super().mcp_tool_call(
                server=server,
                tool=tool,
                arguments=arguments,
                cwd=cwd,
                permission_profile=permission_profile,
                timeout_seconds=timeout_seconds,
                meta=meta,
            )
        else:
            if self._last_mcp_task_id != task_id:
                self._release_other_task_threads(task_id)
                self._last_mcp_task_id = task_id
            thread_id = self._task_thread(
                task_id=task_id,
                cwd=cwd,
                permission_profile=permission_profile,
                timeout_seconds=timeout_seconds,
            )
            params: dict[str, Any] = {
                "threadId": thread_id,
                "server": server,
                "tool": tool,
                "arguments": dict(arguments or {}),
            }
            if meta is not None:
                params["_meta"] = dict(meta)
            result = self.call_tool_method(
                "mcpServer/tool/call",
                params,
                timeout_seconds=timeout_seconds,
            )

        if error_message := _mcp_error_message(result):
            raise CodexToolhostError(error_message)
        return result

    def close(self) -> None:
        with self._task_thread_lock:
            thread_ids = list(self._task_threads.values())
            self._task_threads.clear()
        for thread_id in thread_ids:
            self._unsubscribe_thread(thread_id)
        with self._diagnostic_lock:
            self._version_cache.clear()
            self._workspace_status_cache.clear()
        super().close()


__all__ = ["CodexSessionAppServerClient", "_mcp_error_message"]
