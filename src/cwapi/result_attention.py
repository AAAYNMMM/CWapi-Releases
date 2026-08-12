from __future__ import annotations

from collections.abc import Mapping
from typing import Any


DEFAULT_ATTENTION_REASON_LIMIT = 12


class ResultAttentionError(ValueError):
    pass


def _text(value: object, *, limit: int) -> str | None:
    if value is None:
        return None
    normalized = str(value).strip()
    return normalized[:limit] if normalized else None


def _reason(
    code: str,
    source: str,
    *,
    details_required: bool,
    **fields: object,
) -> dict[str, object]:
    item: dict[str, object] = {
        "code": code,
        "source": source,
        "details_required": details_required,
    }
    item.update(
        {
            key: value
            for key, value in fields.items()
            if value not in (None, "", [], {})
        }
    )
    return item


def _append_unique(
    reasons: list[dict[str, object]],
    seen: set[tuple[object, ...]],
    reason: dict[str, object],
) -> None:
    identity = (
        reason.get("code"),
        reason.get("source"),
        reason.get("step_id"),
        reason.get("phase"),
    )
    if identity not in seen:
        seen.add(identity)
        reasons.append(reason)


def evaluate_result_attention(
    result: Mapping[str, Any],
    summary: Mapping[str, Any],
    *,
    reason_limit: int = DEFAULT_ATTENTION_REASON_LIMIT,
) -> dict[str, object]:
    if not isinstance(result, Mapping) or not isinstance(summary, Mapping):
        raise ResultAttentionError("result and summary must be objects")
    if reason_limit < 1:
        raise ResultAttentionError("reason_limit must be positive")

    reasons: list[dict[str, object]] = []
    seen: set[tuple[object, ...]] = set()
    status = str(summary.get("status") or "unknown")
    status_codes = {
        "failed": "result_failed",
        "rejected": "result_rejected",
        "cancelled": "result_cancelled",
        "unknown": "result_status_unknown",
    }
    if status in status_codes:
        _append_unique(
            reasons,
            seen,
            _reason(
                status_codes[status],
                "result",
                details_required=status != "rejected",
            ),
        )

    commit_raw = summary.get("commit")
    commit = commit_raw if isinstance(commit_raw, Mapping) else {}
    workspace_raw = summary.get("workspace")
    workspace = workspace_raw if isinstance(workspace_raw, Mapping) else {}
    steps_raw = summary.get("steps")
    steps = steps_raw if isinstance(steps_raw, Mapping) else {}

    if status not in {"dry_run", "rejected"}:
        expected = commit.get("expected")
        actual = commit.get("actual")
        verified = commit.get("verified")
        if verified is False:
            _append_unique(
                reasons,
                seen,
                _reason(
                    "commit_mismatch",
                    "commit",
                    details_required=False,
                ),
            )
        elif status == "completed" and verified is not True:
            _append_unique(
                reasons,
                seen,
                _reason(
                    "commit_evidence_missing",
                    "commit",
                    details_required=True,
                ),
            )
        elif expected is not None and actual is None:
            _append_unique(
                reasons,
                seen,
                _reason(
                    "commit_evidence_missing",
                    "commit",
                    details_required=True,
                ),
            )

        clean_before = workspace.get("clean_before")
        clean_after = workspace.get("clean_after")
        if clean_before is False:
            _append_unique(
                reasons,
                seen,
                _reason(
                    "workspace_dirty",
                    "workspace",
                    details_required=True,
                    phase="before",
                ),
            )
        if clean_after is False:
            _append_unique(
                reasons,
                seen,
                _reason(
                    "workspace_dirty",
                    "workspace",
                    details_required=True,
                    phase="after",
                ),
            )
        workspace_has_evidence = isinstance(clean_before, bool) or isinstance(
            clean_after, bool
        )
        if status == "completed" and not bool(workspace.get("evidence_available")):
            _append_unique(
                reasons,
                seen,
                _reason(
                    "workspace_evidence_missing",
                    "workspace",
                    details_required=True,
                ),
            )
        elif workspace_has_evidence and not bool(workspace.get("evidence_available")):
            _append_unique(
                reasons,
                seen,
                _reason(
                    "workspace_evidence_missing",
                    "workspace",
                    details_required=True,
                ),
            )

    failed_count = steps.get("failed_count")
    if status == "completed" and isinstance(failed_count, int) and failed_count > 0:
        _append_unique(
            reasons,
            seen,
            _reason(
                "failed_steps_present",
                "steps",
                details_required=True,
            ),
        )
    if bool(steps.get("failed_truncated")):
        _append_unique(
            reasons,
            seen,
            _reason(
                "failed_steps_truncated",
                "steps",
                details_required=True,
            ),
        )
    exit_code = summary.get("exit_code")
    if status == "completed" and isinstance(exit_code, int) and exit_code != 0:
        _append_unique(
            reasons,
            seen,
            _reason(
                "nonzero_exit_for_completed_result",
                "result",
                details_required=True,
            ),
        )

    raw_steps = result.get("steps")
    full_steps = raw_steps if isinstance(raw_steps, list) else []
    for step in full_steps:
        if not isinstance(step, Mapping):
            continue
        streams = [
            name
            for name in ("stdout", "stderr")
            if bool(step.get(f"{name}_truncated"))
        ]
        if streams:
            _append_unique(
                reasons,
                seen,
                _reason(
                    "output_truncated",
                    "step",
                    details_required=True,
                    step_id=_text(step.get("step_id"), limit=128),
                    streams=streams,
                ),
            )

    raw_warnings = result.get("diagnostic_warnings")
    warnings = list(raw_warnings) if isinstance(raw_warnings, list) else []
    for step in full_steps:
        if not isinstance(step, Mapping):
            continue
        nested = step.get("diagnostic_warnings")
        if isinstance(nested, list):
            warnings.extend(
                {"step_id": step.get("step_id")}
                for warning in nested
                if warning not in (None, "", {}, [])
            )
    for warning in warnings:
        item = warning if isinstance(warning, Mapping) else {}
        _append_unique(
            reasons,
            seen,
            _reason(
                "diagnostic_warning",
                "step" if item.get("step_id") else "result",
                details_required=True,
                step_id=_text(item.get("step_id"), limit=128),
            ),
        )

    generic_warnings = result.get("warnings")
    if isinstance(generic_warnings, list) and generic_warnings:
        _append_unique(
            reasons,
            seen,
            _reason(
                "result_warning",
                "result",
                details_required=True,
            ),
        )

    total = steps.get("total")
    evidence = result.get("evidence")
    evidence_mapping = evidence if isinstance(evidence, Mapping) else {}
    evidence_files = evidence_mapping.get("files")
    evidence_inline = evidence_mapping.get("inline")
    has_execution_evidence = bool(
        (isinstance(evidence_files, list) and evidence_files)
        or (isinstance(evidence_inline, list) and evidence_inline)
    )
    if (
        status in {"completed", "failed", "cancelled"}
        and isinstance(total, int)
        and total > 0
        and not has_execution_evidence
    ):
        _append_unique(
            reasons,
            seen,
            _reason(
                "execution_evidence_missing",
                "evidence",
                details_required=True,
            ),
        )

    artifact_raw = summary.get("artifact")
    artifact = artifact_raw if isinstance(artifact_raw, Mapping) else {}
    if bool(artifact.get("available")) and (
        not artifact.get("manifest_sha256") or not artifact.get("sync_status")
    ):
        _append_unique(
            reasons,
            seen,
            _reason(
                "artifact_evidence_incomplete",
                "artifact",
                details_required=True,
            ),
        )
    artifact_sync = str(artifact.get("sync_status") or "").strip().lower()
    if bool(artifact.get("available")) and artifact_sync in {
        "failed",
        "error",
        "pending",
        "retrying",
    }:
        _append_unique(
            reasons,
            seen,
            _reason(
                "artifact_sync_incomplete",
                "artifact",
                details_required=True,
            ),
        )

    return {
        "needs_attention": bool(reasons),
        "attention": reasons[:reason_limit],
        "attention_count": len(reasons),
        "attention_truncated": len(reasons) > reason_limit,
    }


__all__ = [
    "DEFAULT_ATTENTION_REASON_LIMIT",
    "ResultAttentionError",
    "evaluate_result_attention",
]
