from __future__ import annotations

import base64
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import queue
import threading
import time
from typing import Any, Callable, Mapping, Sequence, TYPE_CHECKING

from cwapi.codex_toolhost.capability_session_client import (
    CodexSessionCapabilityClient,
)
from cwapi.codex_toolhost.client import CodexToolhostError

from .capability_policy import (
    CodexCapabilityPolicy,
    CodexCapabilityPolicyError,
    load_codex_capability_policy,
)
from .repository_automation import (
    RepositoryAutomationError,
    validate_repository_automation_arguments,
)
from .result_capture import StepResult, make_step_result

if TYPE_CHECKING:
    from cwapi.config import CodexToolhostConfig


class CodexToolActionError(ValueError):
    pass


CODEX_TOOL_ACTIONS: frozenset[str] = frozenset(
    {
        "codex_session",
        "codex_mcp_status",
        "codex_mcp_resource",
        "codex_mcp_tool",
        "codex_browser",
        "codex_fs",
    }
)

CODEX_SIDE_EFFECT_ACTIONS: frozenset[str] = frozenset(
    {
        "codex_session",
        "codex_mcp_tool",
        "codex_browser",
        "codex_fs",
    }
)


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _require_string(arguments: Mapping[str, Any], key: str) -> str:
    value = arguments.get(key)
    if not isinstance(value, str) or not value.strip():
        raise CodexToolActionError(f"{key} 必须是非空字符串。")
    return value.strip()


def _optional_string(arguments: Mapping[str, Any], key: str) -> str | None:
    value = arguments.get(key)
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        raise CodexToolActionError(f"{key} 必须是非空字符串或 null。")
    return value.strip()


def _ensure_keys(arguments: Mapping[str, Any], allowed: set[str], action: str) -> None:
    unknown = set(arguments) - allowed
    if unknown:
        raise CodexToolActionError(f"{action} 包含未知参数：{sorted(unknown)}")


def _validate_permission_profile(value: Any) -> str | None:
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        raise CodexToolActionError("permission_profile 必须是非空字符串。")
    return value.strip()


def _validate_interactions(value: Any) -> list[dict[str, Any]]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise CodexToolActionError("interactions 必须是数组。")
    result: list[dict[str, Any]] = []
    previous = -1
    for index, raw in enumerate(value):
        if not isinstance(raw, dict):
            raise CodexToolActionError(f"interactions[{index}] 必须是对象。")
        kind = _require_string(raw, "type")
        after_ms = int(raw.get("after_ms", 0))
        if after_ms < 0 or after_ms < previous:
            raise CodexToolActionError(
                "interactions.after_ms 必须非负并按时间递增。"
            )
        previous = after_ms
        if kind == "write":
            _ensure_keys(raw, {"type", "after_ms", "text", "data_base64"}, kind)
            text = raw.get("text")
            encoded = raw.get("data_base64")
            if (text is None) == (encoded is None):
                raise CodexToolActionError(
                    "write interaction 必须且只能提供 text 或 data_base64。"
                )
            if text is not None and not isinstance(text, str):
                raise CodexToolActionError("write.text 必须是字符串。")
            if encoded is not None:
                if not isinstance(encoded, str):
                    raise CodexToolActionError("write.data_base64 必须是字符串。")
                try:
                    base64.b64decode(encoded, validate=True)
                except ValueError as exc:
                    raise CodexToolActionError("write.data_base64 无效。") from exc
        elif kind == "resize":
            _ensure_keys(raw, {"type", "after_ms", "rows", "cols"}, kind)
            if int(raw.get("rows", 0)) < 1 or int(raw.get("cols", 0)) < 1:
                raise CodexToolActionError("resize rows/cols 必须大于 0。")
        elif kind in {"close_stdin", "terminate"}:
            _ensure_keys(raw, {"type", "after_ms"}, kind)
        else:
            raise CodexToolActionError(f"未知 interaction type：{kind}")
        result.append(dict(raw))
    return result


def _validate_session(arguments: Mapping[str, Any]) -> None:
    allowed = {
        "script_path",
        "script_sha256",
        "arguments",
        "permission_profile",
        "tty",
        "stream_stdin",
        "stream_stdout_stderr",
        "rows",
        "cols",
        "output_bytes_cap",
        "disable_timeout",
        "interactions",
        "expected_exit_codes",
    }
    _ensure_keys(arguments, allowed, "codex_session")
    base = {
        key: arguments[key]
        for key in ("script_path", "script_sha256", "arguments")
        if key in arguments
    }
    base["project_root"] = "."
    try:
        validate_repository_automation_arguments(base)
    except RepositoryAutomationError as exc:
        raise CodexToolActionError(str(exc)) from exc
    _validate_permission_profile(arguments.get("permission_profile"))
    rows = int(arguments.get("rows", 40))
    cols = int(arguments.get("cols", 120))
    if rows < 1 or cols < 1:
        raise CodexToolActionError("rows/cols 必须大于 0。")
    cap = arguments.get("output_bytes_cap")
    if cap is not None and int(cap) < 1024:
        raise CodexToolActionError("output_bytes_cap 不能小于 1024。")
    _validate_interactions(arguments.get("interactions"))
    expected = arguments.get("expected_exit_codes", [0])
    if (
        not isinstance(expected, list)
        or not expected
        or not all(isinstance(value, int) for value in expected)
    ):
        raise CodexToolActionError("expected_exit_codes 必须是非空整数数组。")


def _validate_mcp_status(arguments: Mapping[str, Any]) -> None:
    _ensure_keys(arguments, {"detail", "cursor", "limit"}, "codex_mcp_status")
    detail = str(arguments.get("detail", "full"))
    if detail not in {"full", "summary"}:
        raise CodexToolActionError("detail 只能是 full 或 summary。")
    if arguments.get("limit") is not None and int(arguments["limit"]) < 1:
        raise CodexToolActionError("limit 必须大于 0。")


def _validate_mcp_resource(arguments: Mapping[str, Any]) -> None:
    _ensure_keys(arguments, {"server", "uri"}, "codex_mcp_resource")
    _require_string(arguments, "server")
    _require_string(arguments, "uri")


def _validate_mcp_tool(arguments: Mapping[str, Any], *, browser: bool) -> None:
    allowed = {"tool", "arguments", "meta", "permission_profile"}
    if not browser:
        allowed.add("server")
    _ensure_keys(arguments, allowed, "codex_browser" if browser else "codex_mcp_tool")
    if not browser:
        _require_string(arguments, "server")
    _require_string(arguments, "tool")
    for key in ("arguments", "meta"):
        value = arguments.get(key)
        if value is not None and not isinstance(value, dict):
            raise CodexToolActionError(f"{key} 必须是对象。")
    _validate_permission_profile(arguments.get("permission_profile"))


def _validate_fs(arguments: Mapping[str, Any]) -> None:
    _ensure_keys(arguments, {"operations"}, "codex_fs")
    operations = arguments.get("operations")
    if not isinstance(operations, list) or not operations:
        raise CodexToolActionError("codex_fs.operations 必须是非空数组。")
    if len(operations) > 200:
        raise CodexToolActionError("codex_fs.operations 不能超过 200 项。")
    for index, raw in enumerate(operations):
        if not isinstance(raw, dict):
            raise CodexToolActionError(f"operations[{index}] 必须是对象。")
        kind = _require_string(raw, "type")
        common = {"type", "root", "path"}
        if kind in {"read_file", "metadata", "read_directory", "watch"}:
            _ensure_keys(
                raw,
                common
                | ({"encoding"} if kind == "read_file" else set())
                | ({"watch_id"} if kind == "watch" else set()),
                kind,
            )
            _require_string(raw, "root")
            _require_string(raw, "path")
        elif kind == "write_file":
            _ensure_keys(raw, common | {"text", "data_base64"}, kind)
            _require_string(raw, "root")
            _require_string(raw, "path")
            if (raw.get("text") is None) == (raw.get("data_base64") is None):
                raise CodexToolActionError(
                    "write_file 必须且只能提供 text 或 data_base64。"
                )
        elif kind == "create_directory":
            _ensure_keys(raw, common | {"recursive"}, kind)
            _require_string(raw, "root")
            _require_string(raw, "path")
        elif kind == "remove":
            _ensure_keys(raw, common | {"recursive", "force"}, kind)
            _require_string(raw, "root")
            _require_string(raw, "path")
        elif kind == "copy":
            _ensure_keys(
                raw,
                {
                    "type",
                    "source_root",
                    "source_path",
                    "destination_root",
                    "destination_path",
                    "recursive",
                },
                kind,
            )
            for key in (
                "source_root",
                "source_path",
                "destination_root",
                "destination_path",
            ):
                _require_string(raw, key)
        elif kind == "unwatch":
            _ensure_keys(raw, {"type", "watch_id"}, kind)
            _require_string(raw, "watch_id")
        elif kind == "wait":
            _ensure_keys(raw, {"type", "milliseconds"}, kind)
            if int(raw.get("milliseconds", 0)) < 1:
                raise CodexToolActionError("wait.milliseconds 必须大于 0。")
        else:
            raise CodexToolActionError(f"未知 codex_fs operation：{kind}")


def validate_codex_tool_action_arguments(action: str, arguments: Mapping[str, Any]) -> None:
    if action == "codex_session":
        _validate_session(arguments)
    elif action == "codex_mcp_status":
        _validate_mcp_status(arguments)
    elif action == "codex_mcp_resource":
        _validate_mcp_resource(arguments)
    elif action == "codex_mcp_tool":
        _validate_mcp_tool(arguments, browser=False)
    elif action == "codex_browser":
        _validate_mcp_tool(arguments, browser=True)
    elif action == "codex_fs":
        _validate_fs(arguments)
    else:
        raise CodexToolActionError(f"未知 Codex tool action：{action}")


def codex_action_has_side_effects(action: str) -> bool:
    return action in CODEX_SIDE_EFFECT_ACTIONS


def _repository_automation_command(
    arguments: Mapping[str, Any],
    *,
    python_executable: str,
    workspace: Path,
) -> list[str]:
    normalized = validate_repository_automation_arguments(
        {
            "project_root": str(workspace),
            "script_path": arguments["script_path"],
            "script_sha256": arguments["script_sha256"],
            "arguments": arguments["arguments"],
        }
    )
    command = [
        python_executable,
        "-m",
        "cwapi.execution.repository_automation",
        "--project-root",
        str(normalized["project_root"]),
        "--script",
        str(normalized["script_path"]),
        "--sha256",
        str(normalized["script_sha256"]),
    ]
    for item in normalized["arguments"]:
        command.append(f"--arg={item}")
    return command


def _bounded_json(value: Any, max_bytes: int) -> str:
    text = json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True)
    encoded = text.encode("utf-8")
    if len(encoded) <= max_bytes:
        return text
    marker = "\n... [Codex tool result truncated]"
    keep = max(0, max_bytes - len(marker.encode("utf-8")))
    return encoded[:keep].decode("utf-8", errors="replace") + marker


def _apply_interaction(client: CodexSessionCapabilityClient, process_id: str, item: Mapping[str, Any]) -> None:
    kind = str(item["type"])
    if kind == "write":
        if item.get("text") is not None:
            data = str(item["text"]).encode("utf-8")
        else:
            data = base64.b64decode(str(item["data_base64"]), validate=True)
        client.command_write(process_id, data=data)
    elif kind == "resize":
        client.command_resize(
            process_id,
            rows=int(item["rows"]),
            cols=int(item["cols"]),
        )
    elif kind == "close_stdin":
        client.command_write(process_id, close_stdin=True)
    elif kind == "terminate":
        client.command_terminate(process_id)


def _execute_session(
    *,
    client: CodexSessionCapabilityClient,
    policy: CodexCapabilityPolicy,
    arguments: Mapping[str, Any],
    workspace: Path,
    environment: Mapping[str, str],
    python_executable: str,
    default_permission_profile: str,
    timeout_seconds: int,
    task_id: str,
    step_id: str,
    cancel_check: Callable[[], bool] | None,
) -> tuple[str, str, int | None, str, str | None, str | None, dict[str, Any]]:
    profile = policy.require_permission_profile(
        str(arguments.get("permission_profile") or default_permission_profile)
    )
    interactions = _validate_interactions(arguments.get("interactions"))
    if len(interactions) > policy.max_session_interactions:
        raise CodexToolActionError(
            "codex_session interactions 超过 policy 限制："
            f"{len(interactions)} > {policy.max_session_interactions}"
        )
    tty = bool(arguments.get("tty", False))
    stream_stdin = bool(arguments.get("stream_stdin", bool(interactions)))
    stream_output = bool(arguments.get("stream_stdout_stderr", True))
    output_cap = int(
        arguments.get("output_bytes_cap", policy.max_session_output_bytes)
    )
    if output_cap > policy.max_session_output_bytes:
        raise CodexToolActionError(
            f"output_bytes_cap 超过 policy 限制：{output_cap}"
        )
    command = _repository_automation_command(
        arguments,
        python_executable=python_executable,
        workspace=workspace,
    )
    process_id = f"{task_id}:{step_id}"
    handle = client.command_start(
        command,
        cwd=workspace,
        environment=environment,
        permission_profile=profile,
        timeout_seconds=timeout_seconds,
        process_id=process_id,
        tty=tty,
        stream_stdin=stream_stdin,
        stream_stdout_stderr=stream_output,
        rows=int(arguments.get("rows", 40)),
        cols=int(arguments.get("cols", 120)),
        output_bytes_cap=output_cap,
        disable_timeout=bool(arguments.get("disable_timeout", False)),
    )
    outcome: queue.Queue[Any] = queue.Queue(maxsize=1)

    def waiter() -> None:
        try:
            outcome.put(handle.wait(timeout_seconds=timeout_seconds + 30))
        except BaseException as exc:
            outcome.put(exc)

    worker = threading.Thread(target=waiter, daemon=True)
    worker.start()
    start = time.monotonic()
    interaction_index = 0
    status = "failed"
    error_code: str | None = None
    error_message: str | None = None
    while worker.is_alive():
        elapsed_ms = int((time.monotonic() - start) * 1000)
        while (
            interaction_index < len(interactions)
            and int(interactions[interaction_index].get("after_ms", 0)) <= elapsed_ms
        ):
            _apply_interaction(client, process_id, interactions[interaction_index])
            interaction_index += 1
        if cancel_check is not None and cancel_check():
            status = "cancelled"
            error_code = "CANCELLED"
            error_message = "Task cancellation was requested."
            try:
                client.command_terminate(process_id)
            except CodexToolhostError:
                client.close()
            break
        if time.monotonic() - start >= timeout_seconds:
            status = "timed_out"
            error_code = "TIMEOUT"
            error_message = f"Step timed out after {timeout_seconds}s"
            try:
                client.command_terminate(process_id)
            except CodexToolhostError:
                client.close()
            break
        time.sleep(0.05)

    worker.join(timeout=10)
    if worker.is_alive():
        client.close()
        worker.join(timeout=5)
    try:
        value = outcome.get_nowait()
    except queue.Empty:
        value = CodexToolhostError("Codex command/exec 未返回终态。")
    if isinstance(value, BaseException):
        if error_code is None:
            raise value
        stdout = ""
        stderr = str(value)
        exit_code = None
    else:
        stdout = value.stdout
        stderr = value.stderr
        exit_code = value.exit_code
        if error_code is None:
            expected = {int(code) for code in arguments.get("expected_exit_codes", [0])}
            if exit_code in expected:
                status = "completed"
            else:
                status = "failed"
                error_code = "EXECUTION_FAILED"
                error_message = f"Exit code {exit_code}; expected {sorted(expected)}"
        if value.stdout_cap_reached or value.stderr_cap_reached:
            error_code = error_code or "OUTPUT_LIMIT_REACHED"
            error_message = error_message or "Codex command output reached capture cap."
            if status == "completed":
                status = "failed"
    receipt = {
        "codex_tool_method": "command/exec",
        "permission_profile": profile,
        "process_id": process_id,
        "tty": tty,
        "stream_stdin": stream_stdin,
        "stream_stdout_stderr": stream_output,
        "output_bytes_cap": output_cap,
        "disable_timeout": bool(arguments.get("disable_timeout", False)),
        "interaction_count": len(interactions),
    }
    return stdout, stderr, exit_code, status, error_code, error_message, receipt


def _execute_fs(
    *,
    client: CodexSessionCapabilityClient,
    policy: CodexCapabilityPolicy,
    arguments: Mapping[str, Any],
    workspace: Path,
    task_results: Path,
    task_temp: Path,
    cancel_check: Callable[[], bool] | None,
) -> list[dict[str, Any]]:
    outputs: list[dict[str, Any]] = []
    for index, operation in enumerate(arguments["operations"]):
        if cancel_check is not None and cancel_check():
            raise CodexToolhostError("Task cancellation was requested.")
        kind = str(operation["type"])
        if kind == "wait":
            milliseconds = int(operation["milliseconds"])
            if milliseconds > policy.max_watch_wait_ms:
                raise CodexToolActionError(
                    f"wait 超过 policy 限制：{milliseconds} > {policy.max_watch_wait_ms}"
                )
            deadline = time.monotonic() + milliseconds / 1000
            while time.monotonic() < deadline:
                if cancel_check is not None and cancel_check():
                    raise CodexToolhostError("Task cancellation was requested.")
                time.sleep(min(0.1, max(0.0, deadline - time.monotonic())))
            outputs.append({"index": index, "type": kind, "waited_ms": milliseconds})
            continue
        if kind == "unwatch":
            client.fs_unwatch(str(operation["watch_id"]))
            outputs.append({"index": index, "type": kind, "status": "completed"})
            continue

        if kind == "copy":
            source = policy.resolve_path(
                root_name=str(operation["source_root"]),
                relative_path=str(operation["source_path"]),
                workspace=workspace,
                task_results=task_results,
                task_temp=task_temp,
                write=False,
            )
            destination = policy.resolve_path(
                root_name=str(operation["destination_root"]),
                relative_path=str(operation["destination_path"]),
                workspace=workspace,
                task_results=task_results,
                task_temp=task_temp,
                write=True,
            )
            client.fs_copy(
                source,
                destination,
                recursive=bool(operation.get("recursive", False)),
            )
            outputs.append(
                {
                    "index": index,
                    "type": kind,
                    "source": str(source),
                    "destination": str(destination),
                }
            )
            continue

        write = kind in {"write_file", "create_directory", "remove"}
        path = policy.resolve_path(
            root_name=str(operation["root"]),
            relative_path=str(operation["path"]),
            workspace=workspace,
            task_results=task_results,
            task_temp=task_temp,
            write=write,
        )
        if kind == "read_file":
            data = client.fs_read_file(path)
            encoding = str(operation.get("encoding", "utf-8"))
            value = (
                base64.b64encode(data).decode("ascii")
                if encoding == "base64"
                else data.decode("utf-8", errors="replace")
            )
            outputs.append(
                {"index": index, "type": kind, "path": str(path), "data": value}
            )
        elif kind == "write_file":
            if operation.get("text") is not None:
                data = str(operation["text"]).encode("utf-8")
            else:
                data = base64.b64decode(
                    str(operation["data_base64"]),
                    validate=True,
                )
            if len(data) > policy.max_fs_write_bytes:
                raise CodexToolActionError(
                    f"write_file 超过 policy 限制：{len(data)}"
                )
            client.fs_write_file(path, data)
            outputs.append(
                {
                    "index": index,
                    "type": kind,
                    "path": str(path),
                    "bytes": len(data),
                }
            )
        elif kind == "create_directory":
            client.fs_create_directory(
                path,
                recursive=bool(operation.get("recursive", True)),
            )
            outputs.append({"index": index, "type": kind, "path": str(path)})
        elif kind == "metadata":
            outputs.append(
                {
                    "index": index,
                    "type": kind,
                    "path": str(path),
                    "metadata": client.fs_get_metadata(path),
                }
            )
        elif kind == "read_directory":
            outputs.append(
                {
                    "index": index,
                    "type": kind,
                    "path": str(path),
                    "entries": client.fs_read_directory(path),
                }
            )
        elif kind == "remove":
            client.fs_remove(
                path,
                recursive=bool(operation.get("recursive", True)),
                force=bool(operation.get("force", True)),
            )
            outputs.append({"index": index, "type": kind, "path": str(path)})
        elif kind == "watch":
            result = client.fs_watch(
                path,
                watch_id=str(operation["watch_id"]),
            )
            outputs.append(
                {
                    "index": index,
                    "type": kind,
                    "path": str(path),
                    "result": result,
                }
            )
    return outputs


def execute_codex_tool_action(
    action: str,
    arguments: Mapping[str, Any],
    *,
    codex_config: "CodexToolhostConfig",
    allowed_environment_variables: Sequence[str],
    environment: Mapping[str, str],
    workspace: Path,
    task_results: Path,
    task_temp: Path,
    python_executable: str,
    timeout_seconds: int,
    task_id: str,
    step_id: str,
    ordinal: int,
    cancel_check: Callable[[], bool] | None,
) -> StepResult:
    validate_codex_tool_action_arguments(action, arguments)
    started_at = _now_iso()
    stdout = ""
    stderr = ""
    exit_code: int | None = 0
    status = "completed"
    error_code: str | None = None
    error_message: str | None = None
    method = action
    notifications: list[dict[str, Any]] = []

    def notification_handler(message: dict[str, Any]) -> None:
        if len(notifications) < 500:
            notifications.append(message)

    client = CodexSessionCapabilityClient(
        executable_path=codex_config.executable_path,
        codex_home=codex_config.home_path,
        stderr_log_path=codex_config.stderr_log_path,
        allowed_environment_variables=allowed_environment_variables,
        startup_timeout_seconds=codex_config.startup_timeout_seconds,
        notification_handler=notification_handler,
    )
    try:
        policy_path = Path(
            os.environ.get(
                "CWAPI_CODEX_CAPABILITY_POLICY",
                str(codex_config.home_path.parent.parent / "config" / "codex-capabilities.yaml"),
            )
        )
        policy = load_codex_capability_policy(
            policy_path,
            default_permission_profile=codex_config.permission_profile,
        )
        if action == "codex_session":
            (
                stdout,
                stderr,
                exit_code,
                status,
                error_code,
                error_message,
                receipt_extra,
            ) = _execute_session(
                client=client,
                policy=policy,
                arguments=arguments,
                workspace=workspace,
                environment=environment,
                python_executable=python_executable,
                default_permission_profile=codex_config.permission_profile,
                timeout_seconds=timeout_seconds,
                task_id=task_id,
                step_id=step_id,
                cancel_check=cancel_check,
            )
            method = "command/exec"
        elif action == "codex_mcp_status":
            result = client.mcp_status(
                detail=str(arguments.get("detail", "full")),
                cursor=_optional_string(arguments, "cursor"),
                limit=(int(arguments["limit"]) if arguments.get("limit") is not None else None),
                timeout_seconds=timeout_seconds,
            )
            stdout = _bounded_json(result, policy.max_session_output_bytes)
            receipt_extra = {"codex_tool_method": "mcpServerStatus/list"}
            method = "mcpServerStatus/list"
        elif action == "codex_mcp_resource":
            server = _require_string(arguments, "server")
            uri = _require_string(arguments, "uri")
            policy.require_mcp_resource(server, uri)
            result = client.mcp_resource_read(
                server=server,
                uri=uri,
                timeout_seconds=timeout_seconds,
            )
            stdout = _bounded_json(result, policy.max_session_output_bytes)
            receipt_extra = {
                "codex_tool_method": "mcpServer/resource/read",
                "mcp_server": server,
                "resource_uri": uri,
            }
            method = "mcpServer/resource/read"
        elif action in {"codex_mcp_tool", "codex_browser"}:
            server = (
                policy.browser_server
                if action == "codex_browser"
                else _require_string(arguments, "server")
            )
            if not server:
                raise CodexToolActionError("browser.server 尚未配置。")
            tool = _require_string(arguments, "tool")
            policy.require_mcp_tool(server, tool)
            tool_arguments = dict(arguments.get("arguments") or {})
            meta = dict(arguments.get("meta") or {}) or None
            policy.validate_json_size(
                tool_arguments,
                limit=policy.max_mcp_arguments_bytes,
                name="MCP arguments",
            )
            profile = policy.require_permission_profile(
                str(arguments.get("permission_profile") or codex_config.permission_profile)
            )
            result = client.mcp_tool_call(
                server=server,
                tool=tool,
                arguments=tool_arguments,
                meta=meta,
                cwd=workspace,
                permission_profile=profile,
                timeout_seconds=timeout_seconds,
            )
            stdout = _bounded_json(result, policy.max_session_output_bytes)
            receipt_extra = {
                "codex_tool_method": "mcpServer/tool/call",
                "mcp_server": server,
                "mcp_tool": tool,
                "permission_profile": profile,
                "ephemeral_thread": True,
                "model_turn_started": False,
            }
            method = "mcpServer/tool/call"
        elif action == "codex_fs":
            outputs = _execute_fs(
                client=client,
                policy=policy,
                arguments=arguments,
                workspace=workspace,
                task_results=task_results,
                task_temp=task_temp,
                cancel_check=cancel_check,
            )
            stdout = _bounded_json(
                {"operations": outputs, "notifications": notifications},
                policy.max_session_output_bytes,
            )
            receipt_extra = {
                "codex_tool_method": "fs/*",
                "operation_count": len(outputs),
            }
            method = "fs/*"
        else:
            raise CodexToolActionError(f"未知 Codex tool action：{action}")
    except (CodexToolhostError, CodexToolActionError, CodexCapabilityPolicyError, OSError, ValueError) as exc:
        status = "failed"
        exit_code = None
        error_code = "CODEX_TOOL_ACTION_ERROR"
        error_message = str(exc)
        stderr = str(exc)
        receipt_extra = {"codex_tool_method": method}
    finally:
        client.close()

    receipt = {
        "action": action,
        "execution_backend": "codex_app_server",
        "codex_executable": str(codex_config.executable_path),
        "codex_home": str(codex_config.home_path),
        "capability_policy": str(policy_path) if "policy_path" in locals() else None,
        "process_spawn_enabled": False,
        **receipt_extra,
    }
    return make_step_result(
        task_id=task_id,
        step_id=step_id,
        action=action,
        ordinal=ordinal,
        exit_code=exit_code,
        timed_out=status == "timed_out",
        stdout=stdout,
        stderr=stderr,
        started_at=started_at,
        finished_at=_now_iso(),
        execution_status=status,
        error_code=error_code,
        error_message=error_message,
        command_receipt=receipt,
    )
