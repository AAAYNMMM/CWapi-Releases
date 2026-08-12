from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from .gpt_errors import build_gpt_error


class ResultErrorClassificationError(ValueError):
    pass


def _text(value: object, *, limit: int) -> str | None:
    if value is None:
        return None
    normalized = str(value).strip()
    return normalized[:limit] if normalized else None


def _attention_codes(summary: Mapping[str, Any]) -> set[str]:
    raw = summary.get("attention")
    reasons = raw if isinstance(raw, list) else []
    return {
        str(item.get("code")) for item in reasons if isinstance(item, Mapping) and item.get("code")
    }


def _primary_failure(result: Mapping[str, Any]) -> Mapping[str, Any] | None:
    explicit = result.get("primary_failure")
    if isinstance(explicit, Mapping):
        return explicit
    raw_steps = result.get("steps")
    steps = raw_steps if isinstance(raw_steps, list) else []
    for step in steps:
        if not isinstance(step, Mapping):
            continue
        status = (
            str(step.get("execution_status") or step.get("status") or "unknown").strip().lower()
        )
        if status not in {"completed", "validated", "skipped"}:
            return step
    return None


def _error(
    *,
    code: str,
    category: str,
    message: str,
    next_action: str,
    retryable: bool,
    affected_step: str | None = None,
    details_required: bool = True,
) -> dict[str, Any]:
    return build_gpt_error(
        code=code,
        category=category,
        concise_message=message,
        recommended_next_action=next_action,
        retryable=retryable,
        operation="results.summary",
        affected_step=affected_step,
        details_required=details_required,
    )


def _step_failure_error(
    result: Mapping[str, Any],
) -> dict[str, Any] | None:
    failure = _primary_failure(result)
    top_code = str(result.get("error_code") or "").strip().upper()
    failure_code = (
        str(failure.get("error_code") or "").strip().upper() if failure is not None else ""
    )
    raw_code = failure_code or top_code
    step_id = _text(failure.get("step_id"), limit=128) if failure else None
    action = _text(failure.get("action"), limit=128) if failure else None
    label = f"Step {step_id}" if step_id else "Task execution"
    if action:
        label += f" ({action})"

    timed_out = bool(failure.get("timed_out")) if failure else False
    if timed_out or "TIMEOUT" in raw_code or "TIMED_OUT" in raw_code:
        return _error(
            code="step_timed_out",
            category="timeout",
            message=f"{label} exceeded its controlled timeout.",
            next_action=(
                "Request the affected step detail, then create a new task with an allowed timeout if appropriate."
            ),
            retryable=True,
            affected_step=step_id,
        )
    if "EXPECTED_COMMIT_MISMATCH" in raw_code:
        return _error(
            code="commit_mismatch",
            category="integrity",
            message="The checked-out commit did not match the requested commit.",
            next_action=(
                "Resolve the intended remote commit and create a new task with its exact 40-character SHA."
            ),
            retryable=False,
            details_required=False,
        )
    if "CANCEL" in raw_code:
        return _error(
            code="task_cancelled",
            category="cancellation",
            message=f"{label} was cancelled.",
            next_action="Create a new task only if execution should resume.",
            retryable=False,
            affected_step=step_id,
            details_required=False,
        )
    if "INTERNAL" in raw_code or "INTERRUPTED" in raw_code:
        return _error(
            code="runner_internal_error",
            category="internal",
            message="Runner execution ended because of an internal or interrupted state.",
            next_action=(
                "Check runtime health and full result detail, then create a new task with a new task_id."
            ),
            retryable=True,
            affected_step=step_id,
        )
    if any(token in raw_code for token in ("GMAIL", "TRANSPORT", "DELIVERY")):
        return _error(
            code="result_transport_error",
            category="transport",
            message="Result transport did not complete normally.",
            next_action="Retry result retrieval; do not rerun completed task actions.",
            retryable=True,
        )
    if failure is not None:
        return _error(
            code="step_failed",
            category="execution",
            message=f"{label} failed.",
            next_action=(
                "Request the affected step detail, correct the cause, then create a new task."
            ),
            retryable=False,
            affected_step=step_id,
        )
    return None


def classify_result_error(
    result: Mapping[str, Any],
    summary: Mapping[str, Any],
) -> dict[str, Any] | None:
    if not isinstance(result, Mapping) or not isinstance(summary, Mapping):
        raise ResultErrorClassificationError("result and summary must be objects")
    if not bool(summary.get("needs_attention")):
        return None

    codes = _attention_codes(summary)
    status = str(summary.get("status") or "unknown")
    if "result_cancelled" in codes:
        return _error(
            code="task_cancelled",
            category="cancellation",
            message="Task execution was cancelled.",
            next_action="Create a new task only if execution should resume.",
            retryable=False,
            details_required=False,
        )
    if "result_rejected" in codes:
        return _error(
            code="task_rejected",
            category="validation",
            message="The task was rejected before controlled execution.",
            next_action=(
                "Use context, action, and preset discovery to correct the request, then create a new task."
            ),
            retryable=False,
        )
    if "commit_mismatch" in codes:
        return _error(
            code="commit_mismatch",
            category="integrity",
            message="Expected and actual commits do not match.",
            next_action=(
                "Resolve the intended remote commit and create a new task with its exact 40-character SHA."
            ),
            retryable=False,
            details_required=False,
        )

    if status == "failed":
        failure_error = _step_failure_error(result)
        if failure_error is not None:
            return failure_error
        return _error(
            code="task_failed",
            category="execution",
            message="Task execution failed without a completed success result.",
            next_action="Request full result detail, correct the cause, then create a new task.",
            retryable=False,
        )

    if "workspace_dirty" in codes:
        return _error(
            code="workspace_dirty",
            category="workspace",
            message="Workspace cleanliness evidence is not clean.",
            next_action=(
                "Request workspace detail, remove unintended changes, then create a new task."
            ),
            retryable=False,
        )
    if "failed_steps_present" in codes or "nonzero_exit_for_completed_result" in codes:
        return _error(
            code="result_inconsistent",
            category="integrity",
            message="Completed status conflicts with step or exit evidence.",
            next_action="Request full result detail and do not rely on the completed status alone.",
            retryable=False,
        )
    if "artifact_sync_incomplete" in codes:
        return _error(
            code="artifact_sync_incomplete",
            category="transport",
            message="Artifact synchronization is incomplete.",
            next_action="Retry result detail retrieval later; do not rerun completed task actions.",
            retryable=True,
        )
    evidence_codes = {
        "commit_evidence_missing",
        "workspace_evidence_missing",
        "failed_steps_truncated",
        "output_truncated",
        "execution_evidence_missing",
        "artifact_evidence_incomplete",
    }
    if codes & evidence_codes:
        return _error(
            code="result_evidence_incomplete",
            category="evidence",
            message="Result evidence is missing or truncated.",
            next_action="Request full result or step detail before relying on this result.",
            retryable=False,
        )
    if "diagnostic_warning" in codes or "result_warning" in codes:
        return _error(
            code="result_warning",
            category="warning",
            message="Result diagnostics contain warnings that require review.",
            next_action="Request warning detail before relying on this result.",
            retryable=False,
        )
    return _error(
        code="result_attention_required",
        category="internal" if status == "unknown" else "evidence",
        message="Result requires attention for an unclassified condition.",
        next_action="Request full result detail before deciding what to do next.",
        retryable=False,
    )


__all__ = [
    "ResultErrorClassificationError",
    "classify_result_error",
]
