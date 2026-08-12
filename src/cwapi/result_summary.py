from __future__ import annotations

from collections.abc import Mapping
import json
from typing import Any

from .gpt_contract import GPT_RESULT_SUMMARY_SCHEMA
from .result_attention import evaluate_result_attention
from .result_errors import classify_result_error


DEFAULT_SUMMARY_MAX_BYTES = 8 * 1024
DEFAULT_FAILED_STEP_LIMIT = 10
_SUCCESS_STEP_STATES = frozenset({"completed", "validated", "skipped"})


class ResultSummaryError(ValueError):
    pass


def _text(value: object, *, limit: int) -> str | None:
    if value is None:
        return None
    normalized = str(value).strip()
    return normalized[:limit] if normalized else None


def _result_status(result: Mapping[str, Any]) -> str:
    if bool(result.get("cancelled")):
        return "cancelled"
    execution_mode = str(result.get("execution_mode") or "").strip().lower()
    if execution_mode == "dry_run":
        return "dry_run"
    status = str(result.get("overall_status") or result.get("status") or "unknown").strip().lower()
    if status in {"completed", "failed", "rejected", "cancelled"}:
        return status
    return "unknown"


def _commit_projection(result: Mapping[str, Any]) -> dict[str, object]:
    context = result.get("execution_context")
    execution_context = context if isinstance(context, Mapping) else {}
    expected = _text(
        result.get("expected_commit")
        or execution_context.get("expected_commit")
        or result.get("source_commit"),
        limit=40,
    )
    actual = _text(
        result.get("actual_commit") or execution_context.get("actual_commit"),
        limit=40,
    )
    verified = execution_context.get("commit_verified")
    if not isinstance(verified, bool):
        verified = expected == actual if expected is not None and actual is not None else None
    return {
        "expected": expected,
        "actual": actual,
        "verified": verified,
    }


def _workspace_projection(result: Mapping[str, Any]) -> dict[str, object]:
    context = result.get("execution_context")
    execution_context = context if isinstance(context, Mapping) else {}
    clean_before = execution_context.get("workspace_clean_before")
    clean_after = execution_context.get("workspace_clean_after")
    return {
        "clean_before": clean_before if isinstance(clean_before, bool) else None,
        "clean_after": clean_after if isinstance(clean_after, bool) else None,
        "evidence_available": isinstance(clean_before, bool) and isinstance(clean_after, bool),
    }


def _step_items(result: Mapping[str, Any]) -> list[Mapping[str, Any]]:
    raw = result.get("steps")
    if raw is None:
        raw = result.get("accepted_steps", [])
    if not isinstance(raw, list):
        raise ResultSummaryError("result steps must be an array")
    return [item for item in raw if isinstance(item, Mapping)]


def _failed_step(step: Mapping[str, Any]) -> dict[str, object]:
    status = (
        _text(
            step.get("execution_status") or step.get("status"),
            limit=40,
        )
        or "unknown"
    )
    item: dict[str, object] = {
        "step_id": _text(step.get("step_id"), limit=128),
        "action": _text(step.get("action"), limit=128),
        "status": status,
        "timed_out": bool(step.get("timed_out", False)),
    }
    if isinstance(step.get("exit_code"), int) and not isinstance(step.get("exit_code"), bool):
        item["exit_code"] = step["exit_code"]
    error_code = _text(step.get("error_code"), limit=80)
    if error_code is not None:
        item["error_code"] = error_code
    return {key: value for key, value in item.items() if value is not None}


def _steps_projection(
    result: Mapping[str, Any],
    *,
    failed_step_limit: int,
) -> dict[str, object]:
    steps = _step_items(result)
    completed = 0
    failed: list[dict[str, object]] = []
    for step in steps:
        status = (
            str(step.get("execution_status") or step.get("status") or "unknown").strip().lower()
        )
        if status in _SUCCESS_STEP_STATES:
            completed += 1
        else:
            failed.append(_failed_step(step))
    raw_total = result.get("step_count")
    total = (
        raw_total
        if isinstance(raw_total, int) and not isinstance(raw_total, bool) and raw_total >= 0
        else len(steps)
    )
    return {
        "total": total,
        "completed": completed,
        "failed_count": len(failed),
        "failed": failed[:failed_step_limit],
        "failed_truncated": len(failed) > failed_step_limit,
    }


def _artifact_projection(result: Mapping[str, Any]) -> dict[str, object]:
    raw = result.get("artifact_bundle")
    if not isinstance(raw, Mapping):
        return {"available": False}
    total_bytes = raw.get("total_bytes")
    return {
        "available": True,
        "sync_status": _text(raw.get("sync_status"), limit=40),
        "manifest_sha256": _text(raw.get("manifest_sha256"), limit=64),
        "total_bytes": (
            total_bytes
            if isinstance(total_bytes, int)
            and not isinstance(total_bytes, bool)
            and total_bytes >= 0
            else None
        ),
    }


def build_result_summary(
    result: Mapping[str, Any],
    *,
    failed_step_limit: int = DEFAULT_FAILED_STEP_LIMIT,
    max_bytes: int = DEFAULT_SUMMARY_MAX_BYTES,
) -> dict[str, object]:
    if not isinstance(result, Mapping):
        raise ResultSummaryError("result must be an object")
    if result.get("schema") != "cwapi.result.v1":
        raise ResultSummaryError("result schema must be cwapi.result.v1")
    if failed_step_limit < 1 or max_bytes < 1024:
        raise ResultSummaryError("invalid result summary limits")
    task_id = _text(result.get("task_id"), limit=128)
    if task_id is None:
        raise ResultSummaryError("result task_id is required")

    payload: dict[str, object] = {
        "schema": GPT_RESULT_SUMMARY_SCHEMA,
        "task_id": task_id,
        "repository": _text(result.get("repository"), limit=256),
        "status": _result_status(result),
        "exit_code": (
            result.get("exit_code")
            if isinstance(result.get("exit_code"), int)
            and not isinstance(result.get("exit_code"), bool)
            else None
        ),
        "finished_at": _text(result.get("finished_at"), limit=40),
        "commit": _commit_projection(result),
        "workspace": _workspace_projection(result),
        "steps": _steps_projection(result, failed_step_limit=failed_step_limit),
        "artifact": _artifact_projection(result),
        "details_available": True,
        "detail_operation": "results.get",
        "source_result_schema": "cwapi.result.v1",
    }
    payload.update(evaluate_result_attention(result, payload))
    error = classify_result_error(result, payload)
    if error is not None:
        payload["error"] = error
    size = len(json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
    if size > max_bytes:
        raise ResultSummaryError(f"result summary exceeds byte budget: {size} > {max_bytes}")
    return payload


__all__ = [
    "DEFAULT_FAILED_STEP_LIMIT",
    "DEFAULT_SUMMARY_MAX_BYTES",
    "ResultSummaryError",
    "build_result_summary",
]
