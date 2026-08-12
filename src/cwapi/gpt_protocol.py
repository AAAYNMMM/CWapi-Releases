from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
import hashlib
import json
import re
from typing import Any

from .gpt_contract import GPT_RESPONSE_SCHEMA
from .hashing import content_sha256
from .subjects import build_subject


GPT_REQUEST_SUBJECT = "[CWapi/1][REQUEST][PENDING][AUTO]"
GPT_REQUEST_QUERY = f'in:drafts subject:"{GPT_REQUEST_SUBJECT}"'
MAX_GPT_REQUEST_BYTES = 64 * 1024
_OPERATION = re.compile(r"^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$")
_FORBIDDEN_CALLER_FIELDS = frozenset(
    {
        "schema",
        "request_id",
        "task_id",
        "created_at",
        "expires_at",
        "channel_id",
        "target_runner",
        "runner_id",
        "workspace_mode",
        "source_draft_id",
        "source_message_id",
    }
)
_TASK_ID_SELECTOR_OPERATIONS = frozenset({"results.summary", "results.get"})


class GPTProtocolError(ValueError):
    pass


@dataclass(frozen=True)
class GPTRequestEnvelope:
    request_id: str
    operation: str
    payload: dict[str, Any]
    content_hash: str
    source_draft_id: str
    source_message_id: str | None

    @property
    def parameters(self) -> dict[str, Any]:
        return {
            key: value
            for key, value in self.payload.items()
            if key != "operation"
        }


def derive_request_id(source_draft_id: str) -> str:
    normalized = str(source_draft_id).strip()
    if not normalized:
        raise GPTProtocolError("source draft identity is required")
    digest = hashlib.sha256(
        b"cwapi.gpt.request.v1\x00" + normalized.encode("utf-8")
    ).hexdigest().upper()
    return "REQ" + digest[:29]


def derive_task_id(request_id: str) -> str:
    normalized = str(request_id).strip()
    if not re.fullmatch(r"REQ[0-9A-F]{29}", normalized):
        raise GPTProtocolError("request_id is invalid")
    digest = hashlib.sha256(
        b"cwapi.gpt.task.v1\x00" + normalized.encode("ascii")
    ).hexdigest().upper()
    return "GPT" + digest[:29]


def build_rejected_request_envelope(
    body: str,
    *,
    source_draft_id: str,
    source_message_id: str | None = None,
) -> GPTRequestEnvelope:
    if not isinstance(body, str):
        raise GPTProtocolError("request body must be text")
    normalized_source_message = (
        str(source_message_id).strip() if source_message_id is not None else None
    )
    if normalized_source_message == "":
        normalized_source_message = None
    raw_hash = hashlib.sha256(
        b"cwapi.gpt.rejected-request.v1\x00" + body.encode("utf-8")
    ).hexdigest()
    return GPTRequestEnvelope(
        request_id=derive_request_id(source_draft_id),
        operation="request.invalid",
        payload={"operation": "request.invalid"},
        content_hash=raw_hash,
        source_draft_id=str(source_draft_id).strip(),
        source_message_id=normalized_source_message,
    )


def _object_without_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise GPTProtocolError(f"duplicate JSON field: {key}")
        result[key] = value
    return result


def _reject_non_json_constant(value: str) -> None:
    raise GPTProtocolError(f"invalid JSON constant: {value}")


def parse_gpt_request(
    body: str,
    *,
    source_draft_id: str,
    source_message_id: str | None = None,
) -> GPTRequestEnvelope:
    if not isinstance(body, str):
        raise GPTProtocolError("request body must be text")
    size = len(body.encode("utf-8"))
    if size > MAX_GPT_REQUEST_BYTES:
        raise GPTProtocolError(
            f"request body exceeds byte limit: {size} > {MAX_GPT_REQUEST_BYTES}"
        )
    try:
        payload = json.loads(
            body,
            object_pairs_hook=_object_without_duplicates,
            parse_constant=_reject_non_json_constant,
        )
    except GPTProtocolError:
        raise
    except json.JSONDecodeError as exc:
        raise GPTProtocolError(f"request body is not valid JSON: {exc}") from exc
    if not isinstance(payload, dict):
        raise GPTProtocolError("request body must be a JSON object")

    operation = payload.get("operation")
    if not isinstance(operation, str) or not _OPERATION.fullmatch(operation):
        raise GPTProtocolError(
            "operation must use a lowercase dotted name such as context.get"
        )
    forbidden_fields = _FORBIDDEN_CALLER_FIELDS
    if operation in _TASK_ID_SELECTOR_OPERATIONS:
        forbidden_fields = forbidden_fields - {"task_id"}
    forbidden = sorted(set(payload) & forbidden_fields)
    if forbidden:
        raise GPTProtocolError(
            f"backend-owned request fields are not accepted: {forbidden}"
        )

    normalized_source_message = (
        str(source_message_id).strip() if source_message_id is not None else None
    )
    if normalized_source_message == "":
        normalized_source_message = None
    return GPTRequestEnvelope(
        request_id=derive_request_id(source_draft_id),
        operation=operation,
        payload=payload,
        content_hash=content_sha256(payload),
        source_draft_id=str(source_draft_id).strip(),
        source_message_id=normalized_source_message,
    )


def build_gpt_response_subject(request_id: str, *, status: str) -> str:
    normalized_status = str(status).strip().upper()
    if normalized_status not in {"READY", "FAILED"}:
        raise GPTProtocolError("response status must be READY or FAILED")
    normalized_request_id = str(request_id).strip()
    if not re.fullmatch(r"REQ[0-9A-F]{29}", normalized_request_id):
        raise GPTProtocolError("request_id is invalid")
    return build_subject("RESPONSE", normalized_status, normalized_request_id)


def build_gpt_response(
    request: GPTRequestEnvelope,
    *,
    status: str,
    result: Mapping[str, Any],
) -> tuple[str, dict[str, Any]]:
    normalized_status = str(status).strip().lower()
    if normalized_status not in {"ready", "failed"}:
        raise GPTProtocolError("response status must be ready or failed")
    if not isinstance(result, Mapping):
        raise GPTProtocolError("response result must be an object")
    subject = build_gpt_response_subject(
        request.request_id,
        status=normalized_status.upper(),
    )
    return subject, {
        "schema": GPT_RESPONSE_SCHEMA,
        "request_id": request.request_id,
        "operation": request.operation,
        "status": normalized_status,
        "result": dict(result),
    }


__all__ = [
    "GPTProtocolError",
    "GPTRequestEnvelope",
    "GPT_REQUEST_QUERY",
    "GPT_REQUEST_SUBJECT",
    "MAX_GPT_REQUEST_BYTES",
    "build_gpt_response",
    "build_gpt_response_subject",
    "build_rejected_request_envelope",
    "derive_request_id",
    "derive_task_id",
    "parse_gpt_request",
]
