from __future__ import annotations

from typing import Any

from .gpt_contract import GPT_ERROR_SCHEMA


GPT_ERROR_CATEGORIES = frozenset(
    {
        "request",
        "capability",
        "validation",
        "not_found",
        "conflict",
        "execution",
        "timeout",
        "cancellation",
        "integrity",
        "workspace",
        "evidence",
        "warning",
        "transport",
        "internal",
    }
)


def build_gpt_error(
    *,
    code: str,
    category: str,
    concise_message: str,
    recommended_next_action: str,
    retryable: bool,
    operation: str | None = None,
    feature: str | None = None,
    affected_step: str | None = None,
    details_required: bool = False,
) -> dict[str, Any]:
    normalized_code = str(code).strip()
    normalized_category = str(category).strip()
    normalized_message = str(concise_message).strip()
    normalized_action = str(recommended_next_action).strip()
    if not normalized_code or not normalized_message or not normalized_action:
        raise ValueError("structured GPT error fields must be non-empty")
    if normalized_category not in GPT_ERROR_CATEGORIES:
        raise ValueError(f"unknown GPT error category: {normalized_category}")
    payload: dict[str, Any] = {
        "schema": GPT_ERROR_SCHEMA,
        "code": normalized_code,
        "category": normalized_category,
        "retryable": bool(retryable),
        "affected_step": str(affected_step).strip() if affected_step else None,
        "concise_message": normalized_message[:300],
        "recommended_next_action": normalized_action[:300],
        "details_required": bool(details_required),
    }
    if operation:
        payload["operation"] = str(operation).strip()
    if feature:
        payload["feature"] = str(feature).strip()
    return payload


__all__ = ["GPT_ERROR_CATEGORIES", "build_gpt_error"]
