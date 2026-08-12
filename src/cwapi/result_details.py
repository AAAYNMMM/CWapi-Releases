from __future__ import annotations

from collections.abc import Mapping
import json
from pathlib import Path
from typing import Any

from .gpt_contract import GPT_RESULT_DETAIL_SCHEMA, GPT_RESULT_SUMMARY_SCHEMA
from .result_summary import build_result_summary


RESULT_DETAIL_TYPES = frozenset({"full_result", "step", "logs", "manifest"})


class ResultDetailError(RuntimeError):
    def __init__(
        self,
        message: str,
        *,
        code: str,
        category: str,
        recommended_next_action: str,
        retryable: bool = False,
        affected_step: str | None = None,
        details_required: bool = False,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.category = category
        self.recommended_next_action = recommended_next_action
        self.retryable = retryable
        self.affected_step = affected_step
        self.details_required = details_required


def _error(
    message: str,
    *,
    code: str,
    category: str,
    next_action: str,
    retryable: bool = False,
    affected_step: str | None = None,
    details_required: bool = False,
) -> ResultDetailError:
    return ResultDetailError(
        message,
        code=code,
        category=category,
        recommended_next_action=next_action,
        retryable=retryable,
        affected_step=affected_step,
        details_required=details_required,
    )


def _outbox(store: Any, task_id: str) -> Mapping[str, Any]:
    try:
        row = store.get_outbox_result(task_id)
    except Exception as exc:
        raise _error(
            "result state is unavailable",
            code="result_state_unavailable",
            category="internal",
            next_action="Retry after backend health is restored.",
            retryable=True,
        ) from exc
    if not isinstance(row, Mapping):
        raise _error(
            "result was not found",
            code="result_not_found",
            category="not_found",
            next_action="Call context.get and use an existing task_id.",
        )
    return row


def _json_object(raw: object, *, label: str) -> dict[str, Any]:
    try:
        value = json.loads(str(raw))
    except (TypeError, ValueError, json.JSONDecodeError) as exc:
        raise _error(
            f"persisted {label} is invalid",
            code="result_evidence_invalid",
            category="evidence",
            next_action="Inspect backend health before relying on this result.",
            details_required=True,
        ) from exc
    if not isinstance(value, dict):
        raise _error(
            f"persisted {label} is not an object",
            code="result_evidence_invalid",
            category="evidence",
            next_action="Inspect backend health before relying on this result.",
            details_required=True,
        )
    return value


def _full_result(store: Any, task_id: str) -> dict[str, Any]:
    outbox = _outbox(store, task_id)
    result = _json_object(outbox.get("payload_json"), label="full result")
    if result.get("schema") != "cwapi.result.v1" or result.get("task_id") != task_id:
        raise _error(
            "persisted full result identity is invalid",
            code="result_identity_invalid",
            category="integrity",
            next_action="Inspect backend state before relying on this result.",
            details_required=True,
        )
    return result


def load_result_summary(store: Any, task_id: str) -> dict[str, Any]:
    outbox = _outbox(store, task_id)
    raw_summary = outbox.get("summary_payload_json")
    if raw_summary:
        summary = _json_object(raw_summary, label="result summary")
    else:
        try:
            summary = build_result_summary(_full_result(store, task_id))
        except ResultDetailError:
            raise
        except Exception as exc:
            raise _error(
                "result summary could not be rebuilt",
                code="result_evidence_invalid",
                category="evidence",
                next_action="Request full_result and inspect backend health.",
                details_required=True,
            ) from exc
    if summary.get("schema") != GPT_RESULT_SUMMARY_SCHEMA or summary.get("task_id") != task_id:
        raise _error(
            "persisted result summary identity is invalid",
            code="result_identity_invalid",
            category="integrity",
            next_action="Request full_result and inspect backend state.",
            details_required=True,
        )
    return summary


def _stored_steps(store: Any, task_id: str) -> list[dict[str, Any]]:
    try:
        raw_steps = store.get_task_steps(task_id)
    except Exception as exc:
        raise _error(
            "step state is unavailable",
            code="result_detail_unavailable",
            category="evidence",
            next_action="Request full_result or retry after backend health is restored.",
            retryable=True,
            details_required=True,
        ) from exc
    return [dict(item) for item in raw_steps if isinstance(item, Mapping)]


def _step_from_result(result: Mapping[str, Any], step_id: str) -> dict[str, Any] | None:
    raw_steps = result.get("steps")
    if raw_steps is None:
        raw_steps = result.get("accepted_steps")
    steps = raw_steps if isinstance(raw_steps, list) else []
    return next(
        (
            dict(item)
            for item in steps
            if isinstance(item, Mapping) and str(item.get("step_id")) == step_id
        ),
        None,
    )


def _path_location(value: object) -> dict[str, object] | None:
    if not value:
        return None
    path = Path(str(value))
    try:
        available = path.is_file()
    except (OSError, ValueError):
        available = False
    return {
        "local_path": str(path),
        "available": available,
    }


def _step_locations(step: Mapping[str, Any]) -> dict[str, object]:
    return {
        stream: location
        for stream in ("stdout", "stderr")
        if (location := _path_location(step.get(f"{stream}_path"))) is not None
    }


def _evidence_log_locations(result: Mapping[str, Any]) -> list[dict[str, object]]:
    evidence = result.get("evidence")
    evidence_object = evidence if isinstance(evidence, Mapping) else {}
    raw_files = evidence_object.get("files")
    files = raw_files if isinstance(raw_files, list) else []
    output: list[dict[str, object]] = []
    for item in files:
        if not isinstance(item, Mapping):
            continue
        role = str(item.get("role") or "")
        if not role.endswith(("stdout", "stderr")):
            continue
        output.append(
            {
                "step_id": str(item.get("step_id") or "") or None,
                "stream": "stdout" if role.endswith("stdout") else "stderr",
                "artifact_path": str(item.get("artifact_path") or "") or None,
            }
        )
    return output


def _manifest_detail(store: Any, task_id: str, result: Mapping[str, Any]) -> dict[str, Any]:
    artifact = None
    getter = getattr(store, "get_artifact", None)
    if callable(getter):
        try:
            artifact = getter(task_id)
        except Exception as exc:
            raise _error(
                "artifact state is unavailable",
                code="result_detail_unavailable",
                category="evidence",
                next_action="Retry manifest detail after backend health is restored.",
                retryable=True,
                details_required=True,
            ) from exc
    stored = dict(artifact) if isinstance(artifact, Mapping) else {}
    raw_bundle = result.get("artifact_bundle")
    bundle = dict(raw_bundle) if isinstance(raw_bundle, Mapping) else {}
    if not stored and not bundle:
        raise _error(
            "artifact manifest is not available",
            code="result_detail_not_found",
            category="not_found",
            next_action="Request full_result and check whether the task produced artifacts.",
        )

    local_root = stored.get("local_path")
    manifest_path = Path(str(local_root)) / "manifest.json" if local_root else None
    drive_root = stored.get("drive_relative_path") or bundle.get("drive_relative_path")
    try:
        local_available = manifest_path.is_file() if manifest_path is not None else False
    except (OSError, ValueError):
        local_available = False
    return {
        "manifest_sha256": stored.get("manifest_sha256")
        or bundle.get("manifest_sha256"),
        "sync_status": stored.get("sync_status") or bundle.get("sync_status"),
        "total_bytes": stored.get("total_bytes") or bundle.get("total_bytes"),
        "local_path": str(manifest_path) if manifest_path is not None else None,
        "local_available": local_available,
        "drive_relative_path": (
            f"{str(drive_root).rstrip('/\\')}/manifest.json" if drive_root else None
        ),
        "zip_path": stored.get("zip_path"),
    }


def load_result_detail(
    store: Any,
    task_id: str,
    *,
    detail: str,
    step_id: str | None = None,
) -> dict[str, Any]:
    normalized_detail = str(detail).strip()
    if normalized_detail not in RESULT_DETAIL_TYPES:
        raise _error(
            "unknown result detail selector",
            code="invalid_result_detail",
            category="validation",
            next_action=(
                "Use one of full_result, step, logs, or manifest in a new REQUEST draft."
            ),
        )
    normalized_step_id = str(step_id).strip() if step_id is not None else None
    if normalized_detail == "step" and not normalized_step_id:
        raise _error(
            "step detail requires step_id",
            code="invalid_result_detail",
            category="validation",
            next_action="Provide a step_id listed by results.summary or full_result.",
        )
    if normalized_detail in {"full_result", "manifest"} and normalized_step_id:
        raise _error(
            "step_id is not valid for this detail selector",
            code="invalid_result_detail",
            category="validation",
            next_action="Remove step_id or select step/logs detail.",
        )

    result = _full_result(store, task_id)
    response: dict[str, Any] = {
        "schema": GPT_RESULT_DETAIL_SCHEMA,
        "task_id": task_id,
        "detail": normalized_detail,
    }
    if normalized_detail == "full_result":
        response["full_result"] = result
        return response

    if normalized_detail == "step":
        stored_steps = _stored_steps(store, task_id)
        stored_step = next(
            (item for item in stored_steps if str(item.get("step_id")) == normalized_step_id),
            None,
        )
        result_step = _step_from_result(result, normalized_step_id or "")
        if result_step is None and stored_step is None:
            raise _error(
                "result step was not found",
                code="result_step_not_found",
                category="not_found",
                next_action="Request full_result and choose a listed step_id.",
                affected_step=normalized_step_id,
            )
        response["step"] = result_step or {
            key: value
            for key, value in (stored_step or {}).items()
            if key not in {"stdout_path", "stderr_path"}
        }
        response["log_locations"] = _step_locations(stored_step or {})
        return response

    if normalized_detail == "logs":
        stored_steps = _stored_steps(store, task_id)
        selected = [
            item
            for item in stored_steps
            if normalized_step_id is None or str(item.get("step_id")) == normalized_step_id
        ]
        artifact_locations = [
            item
            for item in _evidence_log_locations(result)
            if normalized_step_id is None or item.get("step_id") == normalized_step_id
        ]
        if normalized_step_id and not selected and not artifact_locations:
            raise _error(
                "result step logs were not found",
                code="result_step_not_found",
                category="not_found",
                next_action="Request full_result and choose a listed step_id.",
                affected_step=normalized_step_id,
            )
        locations = []
        for item in selected:
            step_locations = _step_locations(item)
            if step_locations:
                locations.append(
                    {
                        "step_id": item.get("step_id"),
                        "locations": step_locations,
                    }
                )
        response["logs"] = {
            "step_id": normalized_step_id,
            "items": locations,
            "artifact_locations": artifact_locations,
            "task_log_root": result.get("log_path"),
            "content_included": False,
        }
        return response

    response["manifest"] = _manifest_detail(store, task_id, result)
    return response


__all__ = [
    "RESULT_DETAIL_TYPES",
    "ResultDetailError",
    "load_result_detail",
    "load_result_summary",
]
