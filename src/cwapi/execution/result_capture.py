from __future__ import annotations

import json
import math
import re
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

STDOUT_LIMIT = 65536
STDERR_LIMIT = 65536
SUMMARY_LIMIT = 2048
STEP_SUCCESS_SUMMARY_LIMIT = 512
STEP_FAILURE_STDOUT_LIMIT = 768
STEP_FAILURE_STDERR_LIMIT = 1536
RESULT_STDOUT_LIMIT = 2048
RESULT_STDERR_LIMIT = 3072
REPORT_PREFIX = "CWAPI_REPORT:"
REPORT_JSON_PREFIX = "CWAPI_REPORT_JSON:"
MAX_REPORT_LINES = 20
MAX_STRUCTURED_REPORTS = 20
MAX_STRUCTURED_REPORT_BYTES = 8192
MAX_FAILED_TESTS = 50
MAX_CARGO_ERRORS = 20
MAX_DIAGNOSTIC_MESSAGE = 1000
MAX_WORKSPACE_CHANGES = 200
RECENT_STEP_OUTPUTS = 3
_PYTEST_COUNT_RE = re.compile(
    r"(?P<count>\d+)\s+(?P<kind>passed|failed|skipped|xfailed|xpassed|error|errors)\b",
    re.IGNORECASE,
)
_PYTEST_FAILED_RE = re.compile(r"^FAILED\s+(?P<node>\S+)", re.MULTILINE)
_GIT_STATUS_RE = re.compile(r"^(?P<status>.{2})\s(?P<path>.+)$")


@dataclass
class StepResult:
    task_id: str
    step_id: str
    action: str
    ordinal: int
    execution_status: str
    started_at: str
    finished_at: str
    duration_ms: int
    exit_code: int | None
    timed_out: bool
    stdout: str
    stderr: str
    error_code: str | None
    error_message: str | None
    command_receipt: dict[str, Any] | None = None
    diagnostics: list[dict[str, Any]] | None = None
    diagnostic_warnings: list[dict[str, Any]] | None = None
    diagnostic_report_path: str | None = None
    workspace_status_before: list[dict[str, str]] | None = None
    workspace_status_after: list[dict[str, str]] | None = None


@dataclass
class ExecutionResult:
    task_id: str
    runner_id: str
    repository: str
    expected_commit: str
    actual_commit: str
    overall_status: str
    steps: list[dict[str, Any]]
    exit_code: int | None
    duration_ms: int
    stdout_summary: str
    stderr_summary: str
    log_path: str
    workspace_path: str | None = None
    artifact_bundle: dict[str, Any] | None = None
    source_draft_id: str | None = None
    cancelled: bool = False


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _truncate_edges(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    omitted = len(text) - limit
    marker = f"\n... [truncated {omitted} bytes] ...\n"
    available = max(0, limit - len(marker))
    head = available // 4
    tail = available - head
    return text[:head] + marker + text[-tail:] if tail else text[:head] + marker


def _report_summary(text: str) -> str:
    reports: list[str] = []
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith(REPORT_PREFIX):
            value = stripped[len(REPORT_PREFIX) :].strip()
            if value:
                reports.append(value)
    return "\n".join(reports[-MAX_REPORT_LINES:])


def _structured_reports(text: str) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    reports: list[dict[str, Any]] = []
    warnings: list[dict[str, Any]] = []
    limit_reported = False
    for line_number, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if not stripped.startswith(REPORT_JSON_PREFIX):
            continue
        raw = stripped[len(REPORT_JSON_PREFIX) :].strip()
        if len(raw.encode("utf-8")) > MAX_STRUCTURED_REPORT_BYTES:
            warnings.append(
                {
                    "code": "STRUCTURED_REPORT_TOO_LARGE",
                    "line": line_number,
                    "max_bytes": MAX_STRUCTURED_REPORT_BYTES,
                }
            )
            continue
        try:
            value = json.loads(raw)
        except json.JSONDecodeError as exc:
            warnings.append(
                {
                    "code": "STRUCTURED_REPORT_INVALID_JSON",
                    "line": line_number,
                    "message": str(exc)[:500],
                }
            )
            continue
        if not isinstance(value, dict):
            warnings.append(
                {
                    "code": "STRUCTURED_REPORT_NOT_OBJECT",
                    "line": line_number,
                }
            )
            continue
        if len(reports) >= MAX_STRUCTURED_REPORTS:
            if not limit_reported:
                warnings.append(
                    {
                        "code": "STRUCTURED_REPORT_LIMIT",
                        "max_reports": MAX_STRUCTURED_REPORTS,
                    }
                )
                limit_reported = True
            continue
        reports.append(value)
    return reports, warnings


def capture_stdout(text: str) -> str:
    return _truncate_edges(text, STDOUT_LIMIT)


def capture_stderr(text: str) -> str:
    return _truncate_edges(text, STDERR_LIMIT)


def make_summary(text: str, limit: int = SUMMARY_LIMIT) -> str:
    preferred = _report_summary(text)
    return _truncate_edges(preferred or text, limit)


def _parse_git_porcelain(text: str) -> list[dict[str, str]]:
    changes: list[dict[str, str]] = []
    for line in text.splitlines():
        match = _GIT_STATUS_RE.match(line.rstrip())
        if match is None:
            continue
        status = match.group("status")
        path = match.group("path")
        if " -> " in path:
            old_path, _, new_path = path.partition(" -> ")
            changes.append(
                {
                    "status": status,
                    "path": new_path,
                    "previous_path": old_path,
                }
            )
        else:
            changes.append({"status": status, "path": path})
        if len(changes) >= MAX_WORKSPACE_CHANGES:
            break
    return changes


def _parse_pytest_text(stdout: str, stderr: str) -> dict[str, Any] | None:
    combined = "\n".join(value for value in (stdout, stderr) if value)
    counts: dict[str, int] = {}
    for match in _PYTEST_COUNT_RE.finditer(combined):
        kind = match.group("kind").lower()
        if kind == "errors":
            kind = "error"
        counts[kind] = int(match.group("count"))
    failed_tests = list(dict.fromkeys(_PYTEST_FAILED_RE.findall(combined)))
    if not counts and not failed_tests:
        return None
    result: dict[str, Any] = {"kind": "pytest"}
    result.update(counts)
    if failed_tests:
        result["failed_tests"] = failed_tests[:MAX_FAILED_TESTS]
    return result


def _parse_pytest_junit(path: str | None) -> dict[str, Any] | None:
    if not path:
        return None
    report = Path(path)
    if not report.is_file():
        return None
    root = ET.parse(report).getroot()
    suites = [root] if root.tag == "testsuite" else list(root.findall(".//testsuite"))
    totals = {"tests": 0, "failures": 0, "errors": 0, "skipped": 0}
    for suite in suites:
        for key in totals:
            try:
                totals[key] += int(suite.attrib.get(key, "0"))
            except ValueError:
                pass
    failed_tests: list[str] = []
    locations: list[dict[str, Any]] = []
    for case in root.findall(".//testcase"):
        if case.find("failure") is None and case.find("error") is None:
            continue
        classname = case.attrib.get("classname", "")
        name = case.attrib.get("name", "")
        node = f"{classname}::{name}" if classname else name
        if node and len(failed_tests) < MAX_FAILED_TESTS:
            failed_tests.append(node)
        file_name = case.attrib.get("file")
        line = case.attrib.get("line")
        if file_name and len(locations) < MAX_FAILED_TESTS:
            location: dict[str, Any] = {"path": file_name}
            if line and line.isdigit():
                location["line"] = int(line)
            locations.append(location)
    result: dict[str, Any] = {
        "kind": "pytest",
        "collected": totals["tests"],
        "passed": max(
            0,
            totals["tests"]
            - totals["failures"]
            - totals["errors"]
            - totals["skipped"],
        ),
        "failed": totals["failures"],
        "errors": totals["errors"],
        "skipped": totals["skipped"],
    }
    if failed_tests:
        result["failed_tests"] = failed_tests
    if locations:
        result["locations"] = locations
    return result


def _parse_cargo_json(stdout: str) -> dict[str, Any] | None:
    errors: list[dict[str, Any]] = []
    messages = 0
    total_errors = 0
    for line in stdout.splitlines():
        stripped = line.strip()
        if not stripped.startswith("{"):
            continue
        try:
            value = json.loads(stripped)
        except json.JSONDecodeError:
            continue
        if not isinstance(value, dict) or value.get("reason") != "compiler-message":
            continue
        messages += 1
        message = value.get("message")
        if not isinstance(message, dict) or message.get("level") != "error":
            continue
        total_errors += 1
        if len(errors) >= MAX_CARGO_ERRORS:
            continue
        item: dict[str, Any] = {
            "message": str(message.get("message", ""))[:MAX_DIAGNOSTIC_MESSAGE],
        }
        code = message.get("code")
        if isinstance(code, dict) and code.get("code"):
            item["code"] = str(code["code"])
        spans = message.get("spans")
        if isinstance(spans, list):
            primary = next(
                (
                    span
                    for span in spans
                    if isinstance(span, dict) and span.get("is_primary")
                ),
                None,
            )
            if primary is not None:
                item["path"] = str(primary.get("file_name", ""))
                if isinstance(primary.get("line_start"), int):
                    item["line"] = primary["line_start"]
                if isinstance(primary.get("column_start"), int):
                    item["column"] = primary["column_start"]
        errors.append(item)
    if messages == 0:
        return None
    return {
        "kind": "cargo",
        "compiler_messages": messages,
        "error_count": total_errors,
        "errors": errors,
        "errors_truncated": total_errors > len(errors),
    }


def build_action_diagnostics(
    *,
    action: str,
    stdout: str,
    stderr: str,
    diagnostic_report_path: str | None = None,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    diagnostics: list[dict[str, Any]] = []
    warnings: list[dict[str, Any]] = []
    if action in {"pytest", "pytest_full"}:
        try:
            parsed = _parse_pytest_junit(diagnostic_report_path)
        except (OSError, ET.ParseError, ValueError) as exc:
            warnings.append(
                {
                    "code": "PYTEST_JUNIT_PARSE_FAILED",
                    "message": str(exc)[:500],
                }
            )
            parsed = None
        parsed = parsed or _parse_pytest_text(stdout, stderr)
        if parsed is not None:
            diagnostics.append(parsed)
    elif action in {"cargo_check", "cargo_test"}:
        parsed = _parse_cargo_json(stdout)
        if parsed is not None:
            diagnostics.append(parsed)
    elif action == "git_status":
        diagnostics.append(
            {"kind": "git_status", "changes": _parse_git_porcelain(stdout)}
        )

    reports, report_warnings = _structured_reports("\n".join((stdout, stderr)))
    diagnostics.extend(
        {"kind": "structured_report", "report": report} for report in reports
    )
    warnings.extend(report_warnings)
    return diagnostics, warnings


def make_step_result(
    task_id: str,
    step_id: str,
    action: str,
    ordinal: int,
    exit_code: int | None,
    timed_out: bool,
    stdout: str,
    stderr: str,
    started_at: str | None = None,
    finished_at: str | None = None,
    *,
    execution_status: str | None = None,
    error_code: str | None = None,
    error_message: str | None = None,
    command_receipt: dict[str, Any] | None = None,
    diagnostic_report_path: str | None = None,
    workspace_status_before: list[dict[str, str]] | None = None,
    workspace_status_after: list[dict[str, str]] | None = None,
) -> StepResult:
    started = started_at or _now_iso()
    finished = finished_at or _now_iso()
    start_dt = datetime.fromisoformat(started)
    finish_dt = datetime.fromisoformat(finished)
    duration_ms = math.ceil((finish_dt - start_dt).total_seconds() * 1000)

    status = execution_status or ("completed" if exit_code == 0 else "failed")
    resolved_error_code = error_code
    resolved_error_message = error_message
    if status != "completed" and resolved_error_code is None:
        resolved_error_code = "EXECUTION_FAILED"
    if status != "completed" and resolved_error_message is None:
        resolved_error_message = (
            f"Exit code {exit_code}" if exit_code is not None else status
        )
    diagnostics, diagnostic_warnings = build_action_diagnostics(
        action=action,
        stdout=stdout,
        stderr=stderr,
        diagnostic_report_path=diagnostic_report_path,
    )

    return StepResult(
        task_id=task_id,
        step_id=step_id,
        action=action,
        ordinal=ordinal,
        execution_status=status,
        started_at=started,
        finished_at=finished,
        duration_ms=max(0, duration_ms),
        exit_code=exit_code,
        timed_out=timed_out,
        stdout=capture_stdout(stdout),
        stderr=capture_stderr(stderr),
        error_code=resolved_error_code,
        error_message=resolved_error_message,
        command_receipt=command_receipt,
        diagnostics=diagnostics or None,
        diagnostic_warnings=diagnostic_warnings or None,
        diagnostic_report_path=diagnostic_report_path,
        workspace_status_before=workspace_status_before,
        workspace_status_after=workspace_status_after,
    )


def make_timeout_result(
    task_id: str,
    step_id: str,
    action: str,
    ordinal: int,
    timeout_seconds: int,
) -> StepResult:
    now = _now_iso()
    return StepResult(
        task_id=task_id,
        step_id=step_id,
        action=action,
        ordinal=ordinal,
        execution_status="timed_out",
        started_at=now,
        finished_at=now,
        duration_ms=0,
        exit_code=None,
        timed_out=True,
        stdout="",
        stderr="",
        error_code="TIMEOUT",
        error_message=f"Step timed out after {timeout_seconds}s",
    )


def build_step_payload(
    result: StepResult,
    *,
    stdout_path: str | None = None,
    stderr_path: str | None = None,
) -> dict[str, Any]:
    failed = result.execution_status != "completed"
    stdout_limit = STEP_FAILURE_STDOUT_LIMIT if failed else STEP_SUCCESS_SUMMARY_LIMIT
    stderr_limit = STEP_FAILURE_STDERR_LIMIT if failed else STEP_SUCCESS_SUMMARY_LIMIT
    payload: dict[str, Any] = {
        "step_id": result.step_id,
        "action": result.action,
        "ordinal": result.ordinal,
        "execution_status": result.execution_status,
        "started_at": result.started_at,
        "finished_at": result.finished_at,
        "duration_ms": result.duration_ms,
        "exit_code": result.exit_code,
        "timed_out": result.timed_out,
        "stdout_summary": make_summary(result.stdout, stdout_limit),
        "stderr_summary": make_summary(result.stderr, stderr_limit),
        "stdout_truncated": "... [truncated " in result.stdout,
        "stderr_truncated": "... [truncated " in result.stderr,
        "stdout_path": stdout_path,
        "stderr_path": stderr_path,
        "error_code": result.error_code,
        "error_message": result.error_message,
    }
    if result.command_receipt is not None:
        payload["command_receipt"] = result.command_receipt
    if result.diagnostics:
        payload["diagnostics"] = result.diagnostics
    if result.diagnostic_warnings:
        payload["diagnostic_warnings"] = result.diagnostic_warnings
    if result.diagnostic_report_path:
        payload["diagnostic_report_path"] = result.diagnostic_report_path
    if result.workspace_status_before is not None:
        payload["workspace_status_before"] = result.workspace_status_before
    if result.workspace_status_after is not None:
        payload["workspace_status_after"] = result.workspace_status_after
    return payload


def _compact_step_payloads(steps: list[dict[str, Any]]) -> list[dict[str, Any]]:
    compact: list[dict[str, Any]] = []
    recent_start = max(0, len(steps) - RECENT_STEP_OUTPUTS)
    for index, step in enumerate(steps):
        failed = str(step.get("execution_status", "")) != "completed"
        item: dict[str, Any] = {
            "step_id": step.get("step_id"),
            "action": step.get("action"),
            "ordinal": step.get("ordinal"),
            "execution_status": step.get("execution_status"),
            "duration_ms": step.get("duration_ms"),
            "exit_code": step.get("exit_code"),
            "timed_out": bool(step.get("timed_out", False)),
        }
        if failed or index >= recent_start:
            stdout_summary = str(step.get("stdout_summary") or "")
            stderr_summary = str(step.get("stderr_summary") or "")
            if stdout_summary:
                item["stdout_summary"] = make_summary(
                    stdout_summary,
                    STEP_FAILURE_STDOUT_LIMIT if failed else STEP_SUCCESS_SUMMARY_LIMIT,
                )
            if stderr_summary:
                item["stderr_summary"] = make_summary(
                    stderr_summary,
                    STEP_FAILURE_STDERR_LIMIT if failed else STEP_SUCCESS_SUMMARY_LIMIT,
                )
        for key in (
            "stdout_truncated",
            "stderr_truncated",
            "error_code",
            "diagnostic_warnings",
        ):
            if step.get(key) not in (None, False, [], {}):
                item[key] = step.get(key)
        if step.get("error_message"):
            item["error_message"] = make_summary(str(step["error_message"]), 1024)
        compact.append(item)
    return compact


def _compact_artifact_bundle(bundle: dict[str, Any]) -> dict[str, Any]:
    keys = (
        "drive_relative_path",
        "large_file_transport",
        "manifest_sha256",
        "repository",
        "sync_status",
        "task_id",
        "total_bytes",
    )
    return {key: bundle[key] for key in keys if bundle.get(key) is not None}


def _primary_failure(steps: list[dict[str, Any]]) -> dict[str, Any] | None:
    for step in steps:
        if str(step.get("execution_status", "")) == "completed":
            continue
        return {
            key: step.get(key)
            for key in (
                "step_id",
                "action",
                "ordinal",
                "execution_status",
                "exit_code",
                "timed_out",
                "error_code",
                "error_message",
            )
            if step.get(key) is not None
        }
    return None


def _execution_context(
    result: ExecutionResult,
    steps: list[dict[str, Any]],
) -> dict[str, Any]:
    receipts = [
        step["command_receipt"]
        for step in steps
        if isinstance(step.get("command_receipt"), dict)
    ]
    before = next(
        (
            step.get("workspace_status_before")
            for step in steps
            if step.get("workspace_status_before") is not None
        ),
        None,
    )
    after = next(
        (
            step.get("workspace_status_after")
            for step in reversed(steps)
            if step.get("workspace_status_after") is not None
        ),
        None,
    )
    context: dict[str, Any] = {
        "workspace_path": result.workspace_path,
        "expected_commit": result.expected_commit,
        "actual_commit": result.actual_commit,
        "commit_verified": result.actual_commit == result.expected_commit,
        "workspace_clean_before": before == [] if before is not None else None,
        "workspace_clean_after": after == [] if after is not None else None,
        "workspace_status_before": before,
        "workspace_status_after": after,
        "commands": receipts,
    }
    return {key: value for key, value in context.items() if value is not None}


def _evidence(
    steps: list[dict[str, Any]],
    primary: dict[str, Any] | None,
) -> dict[str, Any]:
    files: list[dict[str, Any]] = []
    recent_start = max(1, len(steps) - RECENT_STEP_OUTPUTS + 1)
    for index, step in enumerate(steps, start=1):
        step_id = str(step.get("step_id", ""))
        failed = str(step.get("execution_status", "")) != "completed"
        if failed or index >= recent_start:
            files.extend(
                (
                    {
                        "role": (
                            "primary_stdout"
                            if primary and primary.get("step_id") == step_id
                            else "step_stdout"
                        ),
                        "step_id": step_id,
                        "artifact_path": f"steps/{index:03d}.stdout.log",
                    },
                    {
                        "role": (
                            "primary_stderr"
                            if primary and primary.get("step_id") == step_id
                            else "step_stderr"
                        ),
                        "step_id": step_id,
                        "artifact_path": f"steps/{index:03d}.stderr.log",
                    },
                )
            )
        if step.get("action") in {"pytest", "pytest_full"} and step.get(
            "diagnostic_report_path"
        ):
            files.append(
                {
                    "role": "test_report",
                    "step_id": step_id,
                    "artifact_path": f"steps/{index:03d}.junit.xml",
                }
            )
    inline: list[dict[str, Any]] = []
    if primary is not None:
        step = next(
            (item for item in steps if item.get("step_id") == primary.get("step_id")),
            None,
        )
        if step is not None:
            content = str(step.get("stderr_summary") or step.get("stdout_summary") or "")
            if content:
                inline.append(
                    {
                        "role": "primary_error",
                        "step_id": primary.get("step_id"),
                        "content": make_summary(content, STEP_FAILURE_STDERR_LIMIT),
                    }
                )
    return {"inline": inline, "files": files}


def build_result_payload(result: ExecutionResult) -> dict[str, Any]:
    compact_steps = _compact_step_payloads(result.steps)
    primary = _primary_failure(compact_steps)
    diagnostics = [
        {"step_id": step.get("step_id"), **diagnostic}
        for step in result.steps
        for diagnostic in step.get("diagnostics", [])
        if isinstance(diagnostic, dict)
    ]
    diagnostic_warnings = [
        {"step_id": step.get("step_id"), **warning}
        for step in result.steps
        for warning in step.get("diagnostic_warnings", [])
        if isinstance(warning, dict)
    ]
    payload: dict[str, Any] = {
        "schema": "cwapi.result.v1",
        "task_id": result.task_id,
        "runner_id": result.runner_id,
        "repository": result.repository,
        "expected_commit": result.expected_commit,
        "actual_commit": result.actual_commit,
        "overall_status": result.overall_status,
        "step_count": len(compact_steps),
        "steps": compact_steps,
        "exit_code": result.exit_code,
        "duration_ms": result.duration_ms,
        "stdout_summary": make_summary(result.stdout_summary, RESULT_STDOUT_LIMIT),
        "stderr_summary": make_summary(result.stderr_summary, RESULT_STDERR_LIMIT),
        "log_path": result.log_path,
        "execution_context": _execution_context(result, result.steps),
        "evidence": _evidence(result.steps, primary),
        "finished_at": _now_iso(),
    }
    if primary is not None:
        payload["primary_failure"] = primary
    if diagnostics:
        payload["diagnostics"] = diagnostics
    if diagnostic_warnings:
        payload["diagnostic_warnings"] = diagnostic_warnings
    if result.workspace_path:
        payload["workspace_path"] = result.workspace_path
    if result.artifact_bundle is not None:
        payload["artifact_bundle"] = _compact_artifact_bundle(result.artifact_bundle)
    if result.source_draft_id:
        payload["source_draft_id"] = result.source_draft_id
    if result.cancelled:
        payload["cancelled"] = True
    return payload
