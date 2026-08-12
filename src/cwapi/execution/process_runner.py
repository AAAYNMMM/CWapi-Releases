from __future__ import annotations

import hashlib
import json
import os
import re
import signal
import subprocess
import tempfile
import threading
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Mapping

from .live_logs import ProcessOutputCollector
from .result_capture import StepResult, make_step_result

_SENSITIVE_OPTION_RE = re.compile(
    r"(?i)^--?(?:api[-_]?key|authorization|credential|password|secret|token)(?:=|$)"
)
_VERSION_ACTIONS = {
    "python_environment": "python",
    "python_pip_check": "python",
    "python_compileall": "python",
    "pytest": "python",
    "pytest_full": "python",
    "cargo_check": "cargo",
    "cargo_test": "cargo",
    "cargo_fmt_check": "cargo",
    "git_rev_parse": "git",
    "git_status": "git",
}


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _terminate_process_tree(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
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
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except Exception:
            process.kill()


def build_execution_environment(
    allowed_names: tuple[str, ...] | list[str] | None = None,
    overrides: Mapping[str, str] | None = None,
) -> dict[str, str]:
    names = tuple(
        allowed_names
        or (
            "PATH",
            "PATHEXT",
            "SYSTEMROOT",
            "WINDIR",
            "COMSPEC",
            "TEMP",
            "TMP",
            "USERNAME",
            "USERPROFILE",
            "HOMEDRIVE",
            "HOMEPATH",
            "LOCALAPPDATA",
            "APPDATA",
            "PROGRAMFILES",
            "PROGRAMFILES(X86)",
            "NUMBER_OF_PROCESSORS",
            "PROCESSOR_ARCHITECTURE",
        )
    )
    environment = {
        name: value
        for name in names
        if (value := os.environ.get(name)) is not None
    }
    environment["PYTHONUTF8"] = "1"
    environment["PYTHONIOENCODING"] = "utf-8"
    environment["PIP_DISABLE_PIP_VERSION_CHECK"] = "1"
    if overrides:
        for key, value in overrides.items():
            if key not in names:
                raise ValueError(f"不允许覆盖环境变量：{key}")
            environment[key] = value
    return environment


def _redact_argv(args: list[str]) -> list[str]:
    redacted: list[str] = []
    hide_next = False
    for value in args:
        if hide_next:
            redacted.append("<redacted>")
            hide_next = False
            continue
        if _SENSITIVE_OPTION_RE.match(value):
            if "=" in value:
                name, _, _ = value.partition("=")
                redacted.append(f"{name}=<redacted>")
            else:
                redacted.append(value)
                hide_next = True
            continue
        redacted.append(value)
    return redacted


def _command_sha256(payload: dict[str, Any]) -> str:
    encoded = json.dumps(
        payload,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _tool_version(
    *,
    action: str,
    executable: str,
    cwd: str | None,
    environment: Mapping[str, str] | None,
) -> str | None:
    if action not in _VERSION_ACTIONS:
        return None
    command = [executable, "--version"]
    try:
        completed = subprocess.run(
            command,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            shell=False,
            cwd=cwd,
            env=dict(environment) if environment is not None else None,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    text = (completed.stdout or completed.stderr).strip()
    return text[:500] or None


def _parse_git_status(text: str) -> list[dict[str, str]]:
    changes: list[dict[str, str]] = []
    for line in text.splitlines():
        if len(line) < 4:
            continue
        status = line[:2]
        path = line[3:]
        if " -> " in path:
            previous, _, current = path.partition(" -> ")
            changes.append(
                {"status": status, "path": current, "previous_path": previous}
            )
        else:
            changes.append({"status": status, "path": path})
    return changes


def _workspace_status(
    cwd: str | None,
    environment: Mapping[str, str] | None,
    git_executable: str = "git",
) -> list[dict[str, str]] | None:
    if not cwd:
        return None
    try:
        completed = subprocess.run(
            [
                git_executable,
                "status",
                "--porcelain=v1",
                "--untracked-files=all",
            ],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            shell=False,
            cwd=cwd,
            env=dict(environment) if environment is not None else None,
            timeout=15,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if completed.returncode != 0:
        return None
    return _parse_git_status(completed.stdout)


def _prepare_command(
    args: list[str],
    *,
    action: str,
    stdout_path: Path,
) -> tuple[list[str], str | None]:
    resolved = list(args)
    diagnostic_report_path: str | None = None
    if action in {"pytest", "pytest_full"} and not any(
        value.startswith("--junitxml") for value in resolved
    ):
        report = stdout_path.with_name(f"{stdout_path.stem}.junit.xml")
        resolved.extend((f"--junitxml={report}", "-o", "junit_family=xunit2"))
        diagnostic_report_path = str(report)
    if action in {"cargo_check", "cargo_test"} and not any(
        value.startswith("--message-format") for value in resolved
    ):
        resolved.append("--message-format=json-render-diagnostics")
    return resolved, diagnostic_report_path


def _build_command_receipt(
    *,
    action: str,
    args: list[str],
    cwd: str | None,
    timeout_seconds: int,
    tool_version: str | None,
) -> dict[str, Any]:
    redacted = _redact_argv(args)
    canonical = {
        "action": action,
        "cwd": cwd,
        "resolved_argv_redacted": redacted,
        "timeout_seconds": timeout_seconds,
    }
    receipt: dict[str, Any] = {
        **canonical,
        "resolved_command_sha256": _command_sha256(canonical),
    }
    if tool_version:
        receipt["tool_version"] = tool_version
    return receipt


def run_process(
    args: list[str],
    *,
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
    if max_output_bytes < 1024:
        raise ValueError("max_output_bytes 过小。")

    started_at = _now_iso()
    temporary_directory: tempfile.TemporaryDirectory[str] | None = None
    if stdout_path is None or stderr_path is None:
        temporary_directory = tempfile.TemporaryDirectory(prefix="cwapi-process-")
        temp_root = Path(temporary_directory.name)
        stdout_file_path = temp_root / "stdout.log"
        stderr_file_path = temp_root / "stderr.log"
    else:
        stdout_file_path = Path(stdout_path)
        stderr_file_path = Path(stderr_path)
    stdout_file_path.parent.mkdir(parents=True, exist_ok=True)
    stderr_file_path.parent.mkdir(parents=True, exist_ok=True)

    resolved_args, diagnostic_report_path = _prepare_command(
        args,
        action=action,
        stdout_path=stdout_file_path,
    )
    workspace_status_before = _workspace_status(
        cwd,
        environment,
        git_executable,
    )
    version = _tool_version(
        action=action,
        executable=resolved_args[0],
        cwd=cwd,
        environment=environment,
    )
    command_receipt = _build_command_receipt(
        action=action,
        args=resolved_args,
        cwd=cwd,
        timeout_seconds=timeout_seconds,
        tool_version=version,
    )

    creationflags = 0
    popen_kwargs: dict[str, object] = {}
    if os.name == "nt":
        creationflags = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
    else:
        popen_kwargs["start_new_session"] = True

    status = "failed"
    error_code: str | None = None
    error_message: str | None = None
    exit_code: int | None = None
    timed_out = False
    collector = ProcessOutputCollector(
        task_id=task_id,
        step_id=step_id,
        max_output_bytes=max_output_bytes,
    )
    readers: list[threading.Thread] = []

    def drain(pipe: Any, stream_name: str) -> None:
        try:
            while True:
                # BufferedReader.read(n) is allowed to wait for n bytes even when
                # the child already flushed a shorter line. read1(n) performs a
                # single underlying pipe read, so available output reaches the
                # in-memory live bus immediately instead of waiting for EOF.
                if hasattr(pipe, "read1"):
                    chunk = pipe.read1(8192)
                else:
                    chunk = pipe.read(8192)
                if not chunk:
                    break
                collector.feed(stream_name, chunk)
        finally:
            try:
                pipe.close()
            except OSError:
                pass

    try:
        try:
            process = subprocess.Popen(
                resolved_args,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                shell=False,
                cwd=cwd,
                env=dict(environment) if environment is not None else None,
                creationflags=creationflags,
                **popen_kwargs,
            )
        except FileNotFoundError as exc:
            finished_at = _now_iso()
            return make_step_result(
                task_id=task_id,
                step_id=step_id,
                action=action,
                ordinal=ordinal,
                exit_code=None,
                timed_out=False,
                stdout="",
                stderr="",
                started_at=started_at,
                finished_at=finished_at,
                execution_status="failed",
                error_code="EXECUTABLE_NOT_FOUND",
                error_message=str(exc),
                command_receipt=command_receipt,
                diagnostic_report_path=diagnostic_report_path,
                workspace_status_before=workspace_status_before,
                workspace_status_after=_workspace_status(
                    cwd,
                    environment,
                    git_executable,
                ),
            )

        assert process.stdout is not None
        assert process.stderr is not None
        for pipe, stream_name in ((process.stdout, "stdout"), (process.stderr, "stderr")):
            reader = threading.Thread(
                target=drain,
                args=(pipe, stream_name),
                name=f"cwapi-{stream_name}-{step_id or 'process'}",
                daemon=True,
            )
            reader.start()
            readers.append(reader)

        deadline = time.monotonic() + timeout_seconds
        while process.poll() is None:
            if cancel_check is not None and cancel_check():
                _terminate_process_tree(process)
                status = "cancelled"
                error_code = "CANCELLED"
                error_message = "Task cancellation was requested."
                break
            if time.monotonic() >= deadline:
                timed_out = True
                _terminate_process_tree(process)
                status = "timed_out"
                error_code = "TIMEOUT"
                error_message = f"Step timed out after {timeout_seconds}s"
                break
            if collector.output_limit_exceeded:
                _terminate_process_tree(process)
                status = "failed"
                error_code = "OUTPUT_LIMIT_EXCEEDED"
                error_message = (
                    f"Combined process output exceeded {max_output_bytes} bytes."
                )
                break
            time.sleep(0.1)

        try:
            exit_code = process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            _terminate_process_tree(process)
            exit_code = process.wait(timeout=15)
        for reader in readers:
            reader.join(timeout=5)
        collector.finish()

        if error_code is None:
            status = "completed" if exit_code == 0 else "failed"
            if exit_code != 0:
                error_code = "EXECUTION_FAILED"
                error_message = f"Exit code {exit_code}"
    except OSError as exc:
        status = "failed"
        error_code = "PROCESS_IO_ERROR"
        error_message = str(exc)
        collector.finish()

    finished_at = _now_iso()
    stdout_text = collector.text("stdout")
    stderr_text = collector.text("stderr")
    workspace_status_after = _workspace_status(
        cwd,
        environment,
        git_executable,
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
