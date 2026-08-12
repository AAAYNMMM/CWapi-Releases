from __future__ import annotations

import base64
from collections import deque
from dataclasses import dataclass
import json
import os
from pathlib import Path
import queue
import subprocess
import threading
import time
from typing import Any, Callable, Mapping, Sequence

from .policy import validate_internal_method, validate_tool_method
from .runtime_lock import RuntimeProvenance, verify_codex_runtime


class CodexToolhostError(RuntimeError):
    pass


@dataclass(frozen=True)
class CodexOutputDelta:
    process_id: str
    stream: str
    data: bytes
    cap_reached: bool = False


@dataclass(frozen=True)
class CodexCommandResponse:
    exit_code: int | None
    stdout: str
    stderr: str
    stdout_cap_reached: bool = False
    stderr_cap_reached: bool = False


NotificationHandler = Callable[[dict[str, Any]], None]


class CodexCommandHandle:
    def __init__(
        self,
        *,
        client: "CodexAppServerClient",
        request_id: int,
        response_queue: "queue.Queue[dict[str, Any] | None]",
        process_id: str,
        timeout_seconds: int,
    ) -> None:
        self._client = client
        self._request_id = request_id
        self._response_queue = response_queue
        self.process_id = process_id
        self.timeout_seconds = timeout_seconds
        self._stdout = bytearray()
        self._stderr = bytearray()
        self._stdout_cap_reached = False
        self._stderr_cap_reached = False
        self._delta_queue: queue.Queue[CodexOutputDelta] = queue.Queue()
        self._closed = False
        client._register_command_handle(process_id, self)

    def _accept_delta(self, delta: CodexOutputDelta) -> None:
        if delta.stream == "stdout":
            self._stdout.extend(delta.data)
            self._stdout_cap_reached = self._stdout_cap_reached or delta.cap_reached
        elif delta.stream == "stderr":
            self._stderr.extend(delta.data)
            self._stderr_cap_reached = self._stderr_cap_reached or delta.cap_reached
        self._delta_queue.put(delta)

    def read_delta(self, timeout_seconds: float | None = None) -> CodexOutputDelta | None:
        try:
            return self._delta_queue.get(timeout=timeout_seconds)
        except queue.Empty:
            return None

    def write(self, data: bytes | str) -> None:
        raw = data.encode("utf-8") if isinstance(data, str) else bytes(data)
        self._client.command_write(self.process_id, data=raw)

    def close_stdin(self) -> None:
        self._client.command_write(self.process_id, close_stdin=True)

    def resize(self, *, rows: int, cols: int) -> None:
        self._client.command_resize(self.process_id, rows=rows, cols=cols)

    def terminate(self) -> None:
        self._client.command_terminate(self.process_id)

    def wait(self, *, timeout_seconds: int | None = None) -> CodexCommandResponse:
        timeout = timeout_seconds or self.timeout_seconds + 30
        try:
            result = self._client._await_response(
                self._request_id,
                self._response_queue,
                timeout_seconds=timeout,
                method="command/exec",
            )
        finally:
            self._close()
        if not isinstance(result, dict):
            raise CodexToolhostError("command/exec 返回值不是对象。")
        exit_code = result.get("exitCode")
        if exit_code is not None and not isinstance(exit_code, int):
            raise CodexToolhostError("command/exec.exitCode 类型无效。")
        final_stdout = str(result.get("stdout") or "")
        final_stderr = str(result.get("stderr") or "")
        stdout = final_stdout or self._stdout.decode("utf-8", errors="replace")
        stderr = final_stderr or self._stderr.decode("utf-8", errors="replace")
        return CodexCommandResponse(
            exit_code=exit_code,
            stdout=stdout,
            stderr=stderr,
            stdout_cap_reached=self._stdout_cap_reached,
            stderr_cap_reached=self._stderr_cap_reached,
        )

    def _close(self) -> None:
        if self._closed:
            return
        self._closed = True
        self._client._unregister_command_handle(self.process_id, self)


class CodexAppServerClient:
    """Concurrent, tool-only client for a private ``codex app-server`` process.

    Agent turns, account operations, reviews, feedback, realtime and every
    ``process/*`` method remain forbidden. A temporary in-memory thread may be
    created internally only to satisfy ``mcpServer/tool/call``'s threadId
    requirement; no turn or model request is started.
    """

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
        self.executable_path = executable_path
        self.codex_home = codex_home
        self.stderr_log_path = stderr_log_path
        self.allowed_environment_variables = tuple(allowed_environment_variables)
        self.startup_timeout_seconds = startup_timeout_seconds
        self.notification_handler = notification_handler

        self._process: subprocess.Popen[str] | None = None
        self._stderr_tail: deque[str] = deque(maxlen=100)
        self._write_lock = threading.Lock()
        self._id_lock = threading.Lock()
        self._pending_lock = threading.Lock()
        self._handles_lock = threading.Lock()
        self._pending: dict[int, queue.Queue[dict[str, Any] | None]] = {}
        self._command_handles: dict[str, CodexCommandHandle] = {}
        self._next_id = 1
        self._closed = False
        self.runtime_provenance: RuntimeProvenance | None = None

    @property
    def stderr_tail(self) -> str:
        return "".join(self._stderr_tail)[-8000:]

    def _release_root(self) -> Path:
        parent = self.executable_path.parent
        return parent.parent if parent.name.lower() == "bin" else parent

    def _launch_environment(self) -> dict[str, str]:
        environment = {
            name: value
            for name in self.allowed_environment_variables
            if (value := os.environ.get(name)) is not None
        }
        release_root = self._release_root()
        private_paths = (
            self.executable_path.parent,
            release_root / "codex-resources",
            release_root / "codex-path",
        )
        path_parts = [str(path) for path in private_paths if path.exists()]
        inherited_path = environment.get("PATH")
        if inherited_path:
            path_parts.append(inherited_path)
        environment["PATH"] = os.pathsep.join(path_parts)
        environment["CODEX_HOME"] = str(self.codex_home)
        environment["RUST_LOG"] = "warn"
        environment["LOG_FORMAT"] = "json"
        for name in (
            "OPENAI_API_KEY",
            "OPENAI_ORG_ID",
            "OPENAI_PROJECT_ID",
            "CODEX_API_KEY",
            "AZURE_OPENAI_API_KEY",
        ):
            environment.pop(name, None)
        return environment

    def start(self) -> None:
        if self._process is not None:
            return
        if self._closed:
            raise CodexToolhostError("Codex app-server client 已关闭。")
        if not self.executable_path.is_absolute():
            raise CodexToolhostError("Codex executable_path 必须是绝对路径。")
        if not self.executable_path.is_file():
            raise CodexToolhostError(f"找不到 CWapi 私有 Codex CLI：{self.executable_path}")

        self.runtime_provenance = verify_codex_runtime(self.executable_path)
        self.codex_home.mkdir(parents=True, exist_ok=True)
        self.stderr_log_path.parent.mkdir(parents=True, exist_ok=True)
        try:
            self._process = subprocess.Popen(
                [str(self.executable_path), "app-server", "--stdio"],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                encoding="utf-8",
                errors="replace",
                bufsize=1,
                shell=False,
                env=self._launch_environment(),
                creationflags=(
                    getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
                    if os.name == "nt"
                    else 0
                ),
                start_new_session=os.name != "nt",
            )
        except OSError as exc:
            raise CodexToolhostError(f"启动 Codex app-server 失败：{exc}") from exc

        threading.Thread(target=self._read_stdout, daemon=True).start()
        threading.Thread(target=self._read_stderr, daemon=True).start()
        initialize_result = self._request_raw(
            "initialize",
            {
                "clientInfo": {
                    "name": "cwapi",
                    "title": "CWapi Codex Tool Host",
                    "version": "1.5.1",
                },
                "capabilities": {
                    "experimentalApi": True,
                    "optOutNotificationMethods": [
                        "item/agentMessage/delta",
                        "turn/started",
                        "turn/completed",
                    ],
                },
            },
            timeout_seconds=self.startup_timeout_seconds,
            validate=False,
        )
        if isinstance(initialize_result, dict):
            reported_home = initialize_result.get("codexHome")
            if reported_home:
                expected_home = self.codex_home.resolve()
                actual_home = Path(str(reported_home)).resolve()
                if actual_home != expected_home:
                    self.close()
                    raise CodexToolhostError(
                        "Codex app-server 使用了错误的 CODEX_HOME："
                        f"expected={expected_home}, actual={actual_home}"
                    )
        self.notify("initialized")

    def _next_request_id(self) -> int:
        with self._id_lock:
            request_id = self._next_id
            self._next_id += 1
            return request_id

    def _read_stdout(self) -> None:
        process = self._process
        if process is None or process.stdout is None:
            self._fail_all_pending()
            return
        for line in process.stdout:
            stripped = line.strip()
            if not stripped:
                continue
            try:
                value = json.loads(stripped)
            except json.JSONDecodeError as exc:
                self._dispatch_protocol_error(
                    f"Codex app-server 输出无效 JSON：{exc}: {stripped[:500]}"
                )
                continue
            if not isinstance(value, dict):
                self._dispatch_protocol_error("Codex app-server 消息不是 JSON 对象。")
                continue
            self._dispatch_message(value)
        self._fail_all_pending()

    def _dispatch_protocol_error(self, message: str) -> None:
        with self._pending_lock:
            pending = list(self._pending.values())
        for response_queue in pending:
            response_queue.put({"_protocol_error": message})

    def _dispatch_message(self, message: dict[str, Any]) -> None:
        if "id" in message and "method" not in message:
            try:
                request_id = int(message["id"])
            except (TypeError, ValueError):
                self._dispatch_protocol_error("Codex app-server 响应 id 无效。")
                return
            with self._pending_lock:
                response_queue = self._pending.get(request_id)
            if response_queue is not None:
                response_queue.put(message)
            return

        if "method" in message and "id" in message:
            self._send(
                {
                    "id": message["id"],
                    "error": {
                        "code": -32601,
                        "message": "CWapi tool-only client does not handle server requests.",
                    },
                }
            )
            return

        if message.get("method") == "command/exec/outputDelta":
            params = message.get("params")
            if isinstance(params, dict):
                process_id = str(params.get("processId") or "")
                stream = str(params.get("stream") or "")
                encoded = str(params.get("deltaBase64") or "")
                try:
                    data = base64.b64decode(encoded, validate=True)
                except (ValueError, TypeError):
                    data = b""
                delta = CodexOutputDelta(
                    process_id=process_id,
                    stream=stream,
                    data=data,
                    cap_reached=bool(params.get("capReached", False)),
                )
                with self._handles_lock:
                    handle = self._command_handles.get(process_id)
                if handle is not None:
                    handle._accept_delta(delta)

        if self.notification_handler is not None:
            self.notification_handler(message)

    def _read_stderr(self) -> None:
        process = self._process
        if process is None or process.stderr is None:
            return
        try:
            with self.stderr_log_path.open("a", encoding="utf-8") as log:
                for line in process.stderr:
                    self._stderr_tail.append(line)
                    log.write(line)
                    log.flush()
        except OSError:
            for line in process.stderr:
                self._stderr_tail.append(line)

    def _send(self, payload: Mapping[str, Any]) -> None:
        process = self._process
        if process is None or process.stdin is None or process.poll() is not None:
            detail = f"\n{self.stderr_tail}" if self.stderr_tail else ""
            raise CodexToolhostError(f"Codex app-server 未运行。{detail}")
        encoded = json.dumps(dict(payload), ensure_ascii=False, separators=(",", ":"))
        with self._write_lock:
            try:
                process.stdin.write(encoded + "\n")
                process.stdin.flush()
            except (BrokenPipeError, OSError) as exc:
                detail = f"\n{self.stderr_tail}" if self.stderr_tail else ""
                raise CodexToolhostError(
                    f"Codex app-server 通信管道已关闭。{detail}"
                ) from exc

    def notify(self, method: str, params: Mapping[str, Any] | None = None) -> None:
        payload: dict[str, Any] = {"method": method}
        if params is not None:
            payload["params"] = dict(params)
        self._send(payload)

    def _request_async(
        self,
        method: str,
        params: Mapping[str, Any] | None,
        *,
        internal: bool = False,
        validate: bool = True,
    ) -> tuple[int, queue.Queue[dict[str, Any] | None]]:
        if self._process is None:
            self.start()
        if validate:
            if internal:
                validate_internal_method(method)
            else:
                validate_tool_method(method)
        request_id = self._next_request_id()
        response_queue: queue.Queue[dict[str, Any] | None] = queue.Queue(maxsize=2)
        with self._pending_lock:
            self._pending[request_id] = response_queue
        payload: dict[str, Any] = {"method": method, "id": request_id}
        if params is not None:
            payload["params"] = dict(params)
        try:
            self._send(payload)
        except Exception:
            with self._pending_lock:
                self._pending.pop(request_id, None)
            raise
        return request_id, response_queue

    def _await_response(
        self,
        request_id: int,
        response_queue: "queue.Queue[dict[str, Any] | None]",
        *,
        timeout_seconds: int,
        method: str,
    ) -> Any:
        if timeout_seconds < 1:
            raise ValueError("Codex RPC timeout_seconds 必须大于 0。")
        try:
            message = response_queue.get(timeout=timeout_seconds)
        except queue.Empty as exc:
            raise CodexToolhostError(f"Codex RPC 超时：{method}") from exc
        finally:
            with self._pending_lock:
                self._pending.pop(request_id, None)
        if message is None:
            detail = f"\n{self.stderr_tail}" if self.stderr_tail else ""
            raise CodexToolhostError(f"Codex app-server 已退出。{detail}")
        protocol_error = message.get("_protocol_error")
        if protocol_error:
            raise CodexToolhostError(str(protocol_error))
        if "error" in message:
            raise CodexToolhostError(f"Codex RPC {method} 失败：{message['error']}")
        return message.get("result")

    def _request_raw(
        self,
        method: str,
        params: Mapping[str, Any] | None,
        *,
        timeout_seconds: int,
        internal: bool = False,
        validate: bool = True,
    ) -> Any:
        request_id, response_queue = self._request_async(
            method,
            params,
            internal=internal,
            validate=validate,
        )
        return self._await_response(
            request_id,
            response_queue,
            timeout_seconds=timeout_seconds,
            method=method,
        )

    def call_tool_method(
        self,
        method: str,
        params: Mapping[str, Any] | None = None,
        *,
        timeout_seconds: int,
    ) -> Any:
        return self._request_raw(method, params, timeout_seconds=timeout_seconds)

    def call_internal_method(
        self,
        method: str,
        params: Mapping[str, Any] | None = None,
        *,
        timeout_seconds: int,
    ) -> Any:
        return self._request_raw(
            method,
            params,
            timeout_seconds=timeout_seconds,
            internal=True,
        )

    def _register_command_handle(
        self,
        process_id: str,
        handle: CodexCommandHandle,
    ) -> None:
        with self._handles_lock:
            if process_id in self._command_handles:
                raise CodexToolhostError(f"重复的 Codex processId：{process_id}")
            self._command_handles[process_id] = handle

    def _unregister_command_handle(
        self,
        process_id: str,
        handle: CodexCommandHandle,
    ) -> None:
        with self._handles_lock:
            if self._command_handles.get(process_id) is handle:
                self._command_handles.pop(process_id, None)

    def command_start(
        self,
        command: Sequence[str],
        *,
        cwd: Path,
        environment: Mapping[str, str | None] | None,
        permission_profile: str,
        timeout_seconds: int,
        process_id: str,
        tty: bool = False,
        stream_stdin: bool = False,
        stream_stdout_stderr: bool = False,
        rows: int = 40,
        cols: int = 120,
        output_bytes_cap: int | None = None,
        disable_timeout: bool = False,
    ) -> CodexCommandHandle:
        if not command or not all(isinstance(value, str) and value for value in command):
            raise ValueError("command 必须是非空字符串数组。")
        if not cwd.is_absolute():
            raise ValueError("command/exec.cwd 必须是绝对路径。")
        if not process_id.strip():
            raise ValueError("流式 command/exec 必须提供 process_id。")
        if timeout_seconds < 1:
            raise ValueError("timeout_seconds 必须大于 0。")
        if rows < 1 or cols < 1:
            raise ValueError("PTY rows/cols 必须大于 0。")
        params: dict[str, Any] = {
            "command": list(command),
            "cwd": str(cwd),
            "permissionProfile": permission_profile,
            "processId": process_id,
            "tty": bool(tty),
            "streamStdin": bool(stream_stdin or tty),
            "streamStdoutStderr": bool(stream_stdout_stderr or tty),
        }
        if disable_timeout:
            params["disableTimeout"] = True
        else:
            params["timeoutMs"] = timeout_seconds * 1000
        if environment is not None:
            params["env"] = dict(environment)
        if tty:
            params["size"] = {"rows": rows, "cols": cols}
        if output_bytes_cap is not None:
            if output_bytes_cap < 1024:
                raise ValueError("output_bytes_cap 不能小于 1024。")
            params["outputBytesCap"] = output_bytes_cap
        if self._process is None:
            self.start()
        validate_tool_method("command/exec")
        request_id = self._next_request_id()
        response_queue: queue.Queue[dict[str, Any] | None] = queue.Queue(maxsize=2)
        handle = CodexCommandHandle(
            client=self,
            request_id=request_id,
            response_queue=response_queue,
            process_id=process_id,
            timeout_seconds=timeout_seconds,
        )
        with self._pending_lock:
            self._pending[request_id] = response_queue
        payload: dict[str, Any] = {
            "method": "command/exec",
            "id": request_id,
            "params": params,
        }
        try:
            self._send(payload)
        except Exception:
            with self._pending_lock:
                self._pending.pop(request_id, None)
            handle._close()
            raise
        return handle

    def command_exec(
        self,
        command: Sequence[str],
        *,
        cwd: Path,
        environment: Mapping[str, str | None] | None,
        permission_profile: str,
        timeout_seconds: int,
        process_id: str | None = None,
        tty: bool = False,
        stream_stdin: bool = False,
        stream_stdout_stderr: bool = False,
        rows: int = 40,
        cols: int = 120,
        output_bytes_cap: int | None = None,
        disable_timeout: bool = False,
    ) -> CodexCommandResponse:
        resolved_process_id = process_id or f"cwapi-{time.monotonic_ns()}"
        handle = self.command_start(
            command,
            cwd=cwd,
            environment=environment,
            permission_profile=permission_profile,
            timeout_seconds=timeout_seconds,
            process_id=resolved_process_id,
            tty=tty,
            stream_stdin=stream_stdin,
            stream_stdout_stderr=stream_stdout_stderr,
            rows=rows,
            cols=cols,
            output_bytes_cap=output_bytes_cap,
            disable_timeout=disable_timeout,
        )
        return handle.wait()

    def command_write(
        self,
        process_id: str,
        *,
        data: bytes | None = None,
        close_stdin: bool = False,
        timeout_seconds: int = 30,
    ) -> None:
        if data is None and not close_stdin:
            raise ValueError("command/exec/write 至少需要 data 或 close_stdin。")
        params: dict[str, Any] = {"processId": process_id}
        if data is not None:
            params["deltaBase64"] = base64.b64encode(data).decode("ascii")
        if close_stdin:
            params["closeStdin"] = True
        self.call_tool_method(
            "command/exec/write",
            params,
            timeout_seconds=timeout_seconds,
        )

    def command_resize(
        self,
        process_id: str,
        *,
        rows: int,
        cols: int,
        timeout_seconds: int = 30,
    ) -> None:
        if rows < 1 or cols < 1:
            raise ValueError("PTY rows/cols 必须大于 0。")
        self.call_tool_method(
            "command/exec/resize",
            {"processId": process_id, "size": {"rows": rows, "cols": cols}},
            timeout_seconds=timeout_seconds,
        )

    def command_terminate(
        self,
        process_id: str,
        *,
        timeout_seconds: int = 30,
    ) -> None:
        self.call_tool_method(
            "command/exec/terminate",
            {"processId": process_id},
            timeout_seconds=timeout_seconds,
        )

    def mcp_status(
        self,
        *,
        detail: str = "full",
        cursor: str | None = None,
        limit: int | None = None,
        timeout_seconds: int = 30,
    ) -> Any:
        params: dict[str, Any] = {"detail": detail}
        if cursor:
            params["cursor"] = cursor
        if limit is not None:
            params["limit"] = limit
        return self.call_tool_method(
            "mcpServerStatus/list",
            params,
            timeout_seconds=timeout_seconds,
        )

    def mcp_resource_read(
        self,
        *,
        server: str,
        uri: str,
        timeout_seconds: int = 30,
    ) -> Any:
        return self.call_tool_method(
            "mcpServer/resource/read",
            {"server": server, "uri": uri},
            timeout_seconds=timeout_seconds,
        )

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
        if not cwd.is_absolute():
            raise ValueError("MCP 临时 Thread cwd 必须是绝对路径。")
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
        params: dict[str, Any] = {
            "threadId": thread_id,
            "server": server,
            "tool": tool,
            "arguments": dict(arguments or {}),
        }
        if meta is not None:
            params["_meta"] = dict(meta)
        try:
            return self.call_tool_method(
                "mcpServer/tool/call",
                params,
                timeout_seconds=timeout_seconds,
            )
        finally:
            try:
                self.call_internal_method(
                    "thread/unsubscribe",
                    {"threadId": thread_id},
                    timeout_seconds=15,
                )
            except CodexToolhostError:
                pass

    @staticmethod
    def _absolute_path(path: Path, method: str) -> Path:
        if not path.is_absolute():
            raise ValueError(f"{method} path 必须是绝对路径。")
        return path

    def fs_read_file(self, path: Path, *, timeout_seconds: int = 30) -> bytes:
        path = self._absolute_path(path, "fs/readFile")
        result = self.call_tool_method(
            "fs/readFile",
            {"path": str(path)},
            timeout_seconds=timeout_seconds,
        )
        if not isinstance(result, dict) or not isinstance(result.get("dataBase64"), str):
            raise CodexToolhostError("fs/readFile 返回值无效。")
        return base64.b64decode(result["dataBase64"])

    def fs_write_file(
        self,
        path: Path,
        data: bytes,
        *,
        timeout_seconds: int = 30,
    ) -> None:
        path = self._absolute_path(path, "fs/writeFile")
        self.call_tool_method(
            "fs/writeFile",
            {
                "path": str(path),
                "dataBase64": base64.b64encode(data).decode("ascii"),
            },
            timeout_seconds=timeout_seconds,
        )

    def fs_create_directory(
        self,
        path: Path,
        *,
        recursive: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        path = self._absolute_path(path, "fs/createDirectory")
        self.call_tool_method(
            "fs/createDirectory",
            {"path": str(path), "recursive": recursive},
            timeout_seconds=timeout_seconds,
        )

    def fs_get_metadata(self, path: Path, *, timeout_seconds: int = 30) -> Any:
        path = self._absolute_path(path, "fs/getMetadata")
        return self.call_tool_method(
            "fs/getMetadata",
            {"path": str(path)},
            timeout_seconds=timeout_seconds,
        )

    def fs_read_directory(self, path: Path, *, timeout_seconds: int = 30) -> Any:
        path = self._absolute_path(path, "fs/readDirectory")
        return self.call_tool_method(
            "fs/readDirectory",
            {"path": str(path)},
            timeout_seconds=timeout_seconds,
        )

    def fs_remove(
        self,
        path: Path,
        *,
        recursive: bool = True,
        force: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        path = self._absolute_path(path, "fs/remove")
        self.call_tool_method(
            "fs/remove",
            {"path": str(path), "recursive": recursive, "force": force},
            timeout_seconds=timeout_seconds,
        )

    def fs_copy(
        self,
        source_path: Path,
        destination_path: Path,
        *,
        recursive: bool = False,
        timeout_seconds: int = 30,
    ) -> None:
        source_path = self._absolute_path(source_path, "fs/copy source")
        destination_path = self._absolute_path(destination_path, "fs/copy destination")
        self.call_tool_method(
            "fs/copy",
            {
                "sourcePath": str(source_path),
                "destinationPath": str(destination_path),
                "recursive": recursive,
            },
            timeout_seconds=timeout_seconds,
        )

    def fs_watch(
        self,
        path: Path,
        *,
        watch_id: str,
        timeout_seconds: int = 30,
    ) -> Any:
        path = self._absolute_path(path, "fs/watch")
        return self.call_tool_method(
            "fs/watch",
            {"path": str(path), "watchId": watch_id},
            timeout_seconds=timeout_seconds,
        )

    def fs_unwatch(self, watch_id: str, *, timeout_seconds: int = 30) -> None:
        self.call_tool_method(
            "fs/unwatch",
            {"watchId": watch_id},
            timeout_seconds=timeout_seconds,
        )

    def _fail_all_pending(self) -> None:
        with self._pending_lock:
            pending = list(self._pending.values())
            self._pending.clear()
        for response_queue in pending:
            response_queue.put(None)

    def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        process = self._process
        self._process = None
        if process is None:
            return
        try:
            if process.stdin is not None:
                process.stdin.close()
        except OSError:
            pass
        if process.poll() is None:
            if os.name == "nt":
                try:
                    subprocess.run(
                        ["taskkill", "/PID", str(process.pid), "/T", "/F"],
                        capture_output=True,
                        shell=False,
                        timeout=15,
                        check=False,
                    )
                except Exception:
                    process.kill()
            else:
                process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=15)
        self._fail_all_pending()

    def __enter__(self) -> "CodexAppServerClient":
        self.start()
        return self

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self.close()
