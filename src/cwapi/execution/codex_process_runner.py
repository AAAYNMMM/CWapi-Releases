from __future__ import annotations

import base64
import json
from pathlib import Path
import queue
import tempfile
import threading
import time
from typing import Callable, Mapping, TYPE_CHECKING

from cwapi.codex_toolhost import CodexAppServerClient, CodexToolhostError
from cwapi.codex_toolhost.capability_client import (
    CodexCapabilityClient,
    CodexCapabilityCommandResponse,
)
from cwapi.codex_toolhost.runtime_lock import (
    RuntimeProvenance,
    verify_codex_runtime,
)

from .codex_tool_actions import execute_codex_tool_action
from .live_logs import LIVE_LOGS, ProcessOutputCollector, sanitize_trace_text
from .process_runner import (
    _VERSION_ACTIONS,
    _build_command_receipt,
    _now_iso,
    _parse_git_status,
    _prepare_command,
)
from .result_capture import StepResult, make_step_result

if TYPE_CHECKING:
    from cwapi.config import CodexToolhostConfig


_CODEX_TOOL_SENTINEL = "__cwapi_codex_tool__"


def _limit_utf8(text: str, limit: int) -> str:
    encoded = text.encode("utf-8", errors="replace")
    if len(encoded) <= limit:
        return text
    marker = f"\n... [truncated {len(encoded) - limit} bytes]"
    available = max(0, limit - len(marker.encode("utf-8")))
    return encoded[:available].decode("utf-8", errors="replace") + marker


def _provenance_fields(provenance: RuntimeProvenance | None) -> dict[str, str]:
    return provenance.receipt_fields() if provenance is not None else {}


def _workspace_status(
    client: CodexAppServerClient,
    *,
    cwd: Path,
    environment: Mapping[str, str],
    permission_profile: str,
    git_executable: str = "git",
) -> list[dict[str, str]] | None:
    try:
        response = client.command_exec(
            [
                git_executable,
                "status",
                "--porcelain=v1",
                "--untracked-files=all",
            ],
            cwd=cwd,
            environment=environment,
            permission_profile=permission_profile,
            timeout_seconds=15,
        )
    except (CodexToolhostError, OSError, ValueError):
        return None
    if response.exit_code != 0:
        return None
    return _parse_git_status(response.stdout)


def _tool_version(
    client: CodexAppServerClient,
    *,
    action: str,
    executable: str,
    cwd: Path,
    environment: Mapping[str, str],
    permission_profile: str,
) -> str | None:
    if action not in _VERSION_ACTIONS:
        return None
    try:
        response = client.command_exec(
            [executable, "--version"],
            cwd=cwd,
            environment=environment,
            permission_profile=permission_profile,
            timeout_seconds=10,
        )
    except (CodexToolhostError, OSError, ValueError):
        return None
    text = (response.stdout or response.stderr).strip()
    return text[:500] or None


def _decode_codex_tool_dispatch(args: list[str], *, expected_action: str) -> dict:
    if len(args) != 2 or args[0] != _CODEX_TOOL_SENTINEL:
        raise ValueError("Codex tool dispatch argv 无效。")
    try:
        raw = base64.urlsafe_b64decode(args[1].encode("ascii"))
        payload = json.loads(raw.decode("utf-8"))
    except (ValueError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError("Codex tool dispatch payload 无效。") from exc
    if not isinstance(payload, dict):
        raise ValueError("Codex tool dispatch payload 必须是对象。")
    if payload.get("schema") != "cwapi.codex-tool-dispatch.v1":
        raise ValueError("Codex tool dispatch schema 无效。")
    if payload.get("action") != expected_action:
        raise ValueError("Codex tool dispatch action 与 TASK 不一致。")
    if not isinstance(payload.get("arguments"), dict):
        raise ValueError("Codex tool dispatch arguments 必须是对象。")
    if not isinstance(payload.get("python_executable"), str):
        raise ValueError("Codex tool dispatch python_executable 无效。")
    return payload


def _publish_completed_output(task_id: str, step_id: str, stdout: str, stderr: str) -> None:
    for stream, text in (("stdout", stdout), ("stderr", stderr)):
        for line in text.splitlines():
            LIVE_LOGS.append_output_line(task_id, step_id, stream, line)


def run_process_via_codex(
    args: list[str],
    *,
    codex_config: "CodexToolhostConfig",
    allowed_environment_variables: tuple[str, ...] | list[str],
    timeout_seconds: int,
    cwd: str | None = None,
    task_id: str = "",
    step_id: str = "",
    action: str = "",
    ordinal: int = 0,
    stdout_path: str | Path | None = None,
    stderr_path: str | Path | None = None,
    cancel_check: Callable[[], bool] | None = None,
    environment: Mapping[str, str] | None = None,
    git_executable: str = "git",
    max_output_bytes: int = 64 * 1024 * 1024,
) -> StepResult:
    if not args or not all(isinstance(value, str) and value for value in args):
        raise ValueError("args 必须是非空字符串数组。")
    if timeout_seconds < 1:
        raise ValueError("timeout_seconds 必须大于 0。")
    if not cwd:
        raise ValueError("Codex toolhost 执行必须提供 cwd。")
    if max_output_bytes < 1024:
        raise ValueError("max_output_bytes 过小。")

    temporary_directory: tempfile.TemporaryDirectory[str] | None = None
    if stdout_path is None or stderr_path is None:
        temporary_directory = tempfile.TemporaryDirectory(prefix="cwapi-codex-")
        temp_root = Path(temporary_directory.name)
        stdout_file_path = temp_root / "stdout.log"
        stderr_file_path = temp_root / "stderr.log"
    else:
        stdout_file_path = Path(stdout_path)
        stderr_file_path = Path(stderr_path)
    stdout_file_path.parent.mkdir(parents=True, exist_ok=True)
    stderr_file_path.parent.mkdir(parents=True, exist_ok=True)
    workspace = Path(cwd)
    command_environment = dict(environment or {})

    if args[0] == _CODEX_TOOL_SENTINEL:
        try:
            dispatch = _decode_codex_tool_dispatch(args, expected_action=action)
            task_root = stdout_file_path.parent
            task_results = task_root / "codex-tool-results"
            task_temp = task_root / "codex-tool-temp"
            task_results.mkdir(parents=True, exist_ok=True)
            task_temp.mkdir(parents=True, exist_ok=True)
            result = execute_codex_tool_action(
                action,
                dispatch["arguments"],
                codex_config=codex_config,
                allowed_environment_variables=allowed_environment_variables,
                environment=command_environment,
                workspace=workspace,
                task_results=task_results,
                task_temp=task_temp,
                python_executable=dispatch["python_executable"],
                timeout_seconds=timeout_seconds,
                task_id=task_id,
                step_id=step_id,
                ordinal=ordinal,
                cancel_check=cancel_check,
            )
            if result.command_receipt is not None:
                provenance = verify_codex_runtime(codex_config.executable_path)
                result.command_receipt.update(_provenance_fields(provenance))
        except (CodexToolhostError, OSError, ValueError) as exc:
            now = _now_iso()
            result = make_step_result(
                task_id=task_id,
                step_id=step_id,
                action=action,
                ordinal=ordinal,
                exit_code=None,
                timed_out=False,
                stdout="",
                stderr=str(exc),
                started_at=now,
                finished_at=now,
                execution_status="failed",
                error_code="CODEX_TOOL_DISPATCH_ERROR",
                error_message=str(exc),
                command_receipt={
                    "action": action,
                    "execution_backend": "codex_app_server",
                    "codex_executable": str(codex_config.executable_path),
                    "codex_home": str(codex_config.home_path),
                    "process_spawn_enabled": False,
                },
            )
        _publish_completed_output(task_id, step_id, result.stdout, result.stderr)
        if temporary_directory is not None:
            temporary_directory.cleanup()
        return result

    started_at = _now_iso()
    resolved_args, diagnostic_report_path = _prepare_command(
        args,
        action=action,
        stdout_path=stdout_file_path,
    )

    status = "failed"
    error_code: str | None = None
    error_message: str | None = None
    exit_code: int | None = None
    timed_out = False
    stdout_text = ""
    stderr_text = ""
    workspace_status_before: list[dict[str, str]] | None = None
    workspace_status_after: list[dict[str, str]] | None = None
    tool_version: str | None = None
    runtime_provenance: RuntimeProvenance | None = None
    collector = ProcessOutputCollector(
        task_id=task_id,
        step_id=step_id,
        max_output_bytes=max_output_bytes,
    )

    client = CodexCapabilityClient(
        executable_path=codex_config.executable_path,
        codex_home=codex_config.home_path,
        stderr_log_path=codex_config.stderr_log_path,
        allowed_environment_variables=allowed_environment_variables,
        startup_timeout_seconds=codex_config.startup_timeout_seconds,
    )
    outcome: queue.Queue[CodexCapabilityCommandResponse | BaseException] = queue.Queue(
        maxsize=1
    )
    process_id = f"{task_id}:{step_id}" if task_id and step_id else "cwapi-command"

    try:
        client.start()
        runtime_provenance = client.runtime_provenance
        workspace_status_before = _workspace_status(
            client,
            cwd=workspace,
            environment=command_environment,
            permission_profile=codex_config.permission_profile,
            git_executable=git_executable,
        )
        tool_version = _tool_version(
            client,
            action=action,
            executable=resolved_args[0],
            cwd=workspace,
            environment=command_environment,
            permission_profile=codex_config.permission_profile,
        )
        handle = client.command_start(
            resolved_args,
            cwd=workspace,
            environment=command_environment,
            permission_profile=codex_config.permission_profile,
            timeout_seconds=timeout_seconds,
            process_id=process_id,
            stream_stdout_stderr=True,
            output_bytes_cap=max_output_bytes,
        )

        def invoke() -> None:
            try:
                outcome.put(handle.wait(timeout_seconds=timeout_seconds + 30))
            except BaseException as exc:
                outcome.put(exc)

        def drain_deltas() -> None:
            while True:
                delta = handle.read_delta(timeout_seconds=0)
                if delta is None:
                    break
                collector.feed(delta.stream, delta.data)

        worker = threading.Thread(target=invoke, daemon=True)
        worker.start()
        deadline = time.monotonic() + timeout_seconds

        while worker.is_alive():
            drain_deltas()
            if cancel_check is not None and cancel_check():
                status = "cancelled"
                error_code = "CANCELLED"
                error_message = "Task cancellation was requested."
                try:
                    handle.terminate()
                except CodexToolhostError:
                    client.close()
                break
            if time.monotonic() >= deadline:
                timed_out = True
                status = "timed_out"
                error_code = "TIMEOUT"
                error_message = f"Step timed out after {timeout_seconds}s"
                try:
                    handle.terminate()
                except CodexToolhostError:
                    client.close()
                break
            if collector.output_limit_exceeded:
                status = "failed"
                error_code = "OUTPUT_LIMIT_EXCEEDED"
                error_message = f"Combined process output exceeded {max_output_bytes} bytes."
                try:
                    handle.terminate()
                except CodexToolhostError:
                    client.close()
                break
            time.sleep(0.1)

        worker.join(timeout=10)
        drain_deltas()
        if worker.is_alive():
            client.close()
            worker.join(timeout=5)
            drain_deltas()
        collector.finish()

        try:
            value = outcome.get_nowait()
        except queue.Empty as exc:
            if error_code is None:
                raise CodexToolhostError(
                    "Codex command/exec 未返回结果。"
                ) from exc
            value = None
        if isinstance(value, BaseException):
            if error_code is None:
                raise value
            stderr_text = str(value)
        elif value is not None:
            exit_code = value.exit_code
            stdout_text = sanitize_trace_text(
                value.stdout,
                task_id=task_id,
                step_id=step_id,
            )
            stderr_text = sanitize_trace_text(
                value.stderr,
                task_id=task_id,
                step_id=step_id,
            )
            if value.stdout_cap_reached or value.stderr_cap_reached:
                status = "failed"
                error_code = "OUTPUT_LIMIT_EXCEEDED"
                error_message = (
                    f"Codex command output reached {max_output_bytes} bytes per stream."
                )
            elif error_code is None:
                status = "completed" if exit_code == 0 else "failed"
                if exit_code != 0:
                    error_code = "EXECUTION_FAILED"
                    error_message = f"Exit code {exit_code}"

        if not timed_out and status != "cancelled":
            workspace_status_after = _workspace_status(
                client,
                cwd=workspace,
                environment=command_environment,
                permission_profile=codex_config.permission_profile,
                git_executable=git_executable,
            )
    except (CodexToolhostError, OSError, ValueError) as exc:
        collector.finish()
        if error_code is None:
            status = "failed"
            error_code = "CODEX_TOOLHOST_ERROR"
            error_message = str(exc)
        if not stderr_text:
            stderr_text = str(exc)
    finally:
        client.close()

    if not stdout_text:
        stdout_text = collector.text("stdout")
    if not stderr_text:
        stderr_text = collector.text("stderr")

    combined_size = len(stdout_text.encode("utf-8")) + len(
        stderr_text.encode("utf-8")
    )
    if combined_size > max_output_bytes:
        status = "failed"
        error_code = "OUTPUT_LIMIT_EXCEEDED"
        error_message = f"Combined process output exceeded {max_output_bytes} bytes."
        per_stream = max(512, max_output_bytes // 2)
        stdout_text = _limit_utf8(stdout_text, per_stream)
        stderr_text = _limit_utf8(stderr_text, per_stream)

    finished_at = _now_iso()
    command_receipt = _build_command_receipt(
        action=action,
        args=resolved_args,
        cwd=cwd,
        timeout_seconds=timeout_seconds,
        tool_version=tool_version,
    )
    command_receipt.update(
        {
            "execution_backend": "codex_app_server",
            "codex_executable": str(codex_config.executable_path),
            "codex_home": str(codex_config.home_path),
            "permission_profile": codex_config.permission_profile,
            "stream_stdout_stderr": True,
            "process_id": process_id,
            "output_bytes_cap": max_output_bytes,
            "process_spawn_enabled": False,
            **_provenance_fields(runtime_provenance),
        }
    )

    result = make_step_result(
        task_id=task_id,
        step_id=step_id,
        action=action,
        ordinal=ordinal,
        exit_code=exit_code,
        timed_out=timed_out,
        stdout=stdout_text,
        stderr=stderr_text,
        started_at=started_at,
        finished_at=finished_at,
        execution_status=status,
        error_code=error_code,
        error_message=error_message,
        command_receipt=command_receipt,
        diagnostic_report_path=diagnostic_report_path,
        workspace_status_before=workspace_status_before,
        workspace_status_after=workspace_status_after,
    )
    if temporary_directory is not None:
        temporary_directory.cleanup()
    return result
