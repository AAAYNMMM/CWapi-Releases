from __future__ import annotations

from collections.abc import Iterable, Mapping
from datetime import datetime, timezone
import json
import re
from typing import Any

from .gpt_contract import (
    GPT_CONTEXT_SCHEMA,
    build_feature_contract,
    build_protocol_contract,
)
from .runtime_observability import build_runtime_snapshot


DEFAULT_CONTEXT_MAX_BYTES = 16 * 1024
DEFAULT_PROJECT_LIMIT = 25
DEFAULT_RECENT_TASK_LIMIT = 5
_LOCAL_FEATURES = frozenset(
    {"context", "action_discovery", "task_builder", "presets", "result_summary"}
)
_ATTENTION_STATES = frozenset({"failed", "warning", "retrying", "unavailable", "stopped"})
_PUBLIC_COMPONENT_STATES = frozenset(
    {
        "unavailable",
        "healthy",
        "working",
        "waiting",
        "retrying",
        "warning",
        "failed",
        "stopped",
    }
)
_PUBLIC_DETAIL = re.compile(r"^[A-Za-z0-9_.:-]{1,80}$")


class ContextProjectionError(RuntimeError):
    pass


def _text(value: object, *, limit: int) -> str | None:
    if value is None:
        return None
    normalized = str(value)
    return normalized[:limit]


def _component_projection(name: str, raw: object) -> dict[str, object]:
    item = raw if isinstance(raw, Mapping) else {}
    enabled = bool(item.get("enabled", True))
    state = str(item.get("state") or "unavailable").casefold()
    if state not in _PUBLIC_COMPONENT_STATES:
        state = "unavailable"
    raw_detail = str(item.get("detail") or "")
    detail = raw_detail if _PUBLIC_DETAIL.fullmatch(raw_detail) else "details_available"
    needs_attention = enabled and state in _ATTENTION_STATES
    projected: dict[str, object] = {
        "name": name,
        "enabled": enabled,
        "state": state,
        "needs_attention": needs_attention,
    }
    if detail:
        projected["reason"] = detail
    if name == "gmail" and bool(item.get("authorization_required")):
        projected["authorization_required"] = True
    return projected


def _task_projection(raw: Mapping[str, object]) -> dict[str, object]:
    return {
        "task_id": _text(raw.get("task_id"), limit=128),
        "repository": _text(raw.get("repository"), limit=256),
        "expected_commit": _text(raw.get("expected_commit"), limit=40),
        "execution_status": _text(raw.get("execution_status"), limit=40),
        "result_status": _text(raw.get("result_status"), limit=40),
        "received_at": _text(raw.get("received_at"), limit=40),
        "finished_at": _text(raw.get("finished_at"), limit=40),
    }


def _task_context(
    service: Any,
    *,
    recent_limit: int,
) -> tuple[dict[str, object], list[dict[str, str]]]:
    attention: list[dict[str, str]] = []
    try:
        raw_tasks = service.runner.store.list_tasks(max(20, recent_limit + 5))
    except Exception:
        attention.append(
            {
                "source": "task_state",
                "reason": "task_state_unavailable",
            }
        )
        return {
            "available": False,
            "current": None,
            "recent": [],
        }, attention

    tasks = [dict(item) for item in raw_tasks if isinstance(item, Mapping)]
    current_raw = next(
        (
            item
            for item in tasks
            if str(item.get("execution_status") or "") in {"claimed", "running"}
        ),
        None,
    )
    recent = [
        _task_projection(item)
        for item in tasks
        if current_raw is None or item.get("task_id") != current_raw.get("task_id")
    ][:recent_limit]
    return {
        "available": True,
        "current": _task_projection(current_raw) if current_raw is not None else None,
        "recent": recent,
        "recent_limit": recent_limit,
    }, attention


def _project_context(
    service: Any,
    *,
    repository: str | None,
    project_limit: int,
) -> tuple[dict[str, object], list[dict[str, str]]]:
    allowed = sorted(str(value) for value in service.config.security.allowed_repositories)
    requested = str(repository).strip() if repository is not None else None
    if requested:
        selected = [requested] if requested in allowed else []
    else:
        selected = allowed[:project_limit]

    attention: list[dict[str, str]] = []
    projects: list[dict[str, object]] = []
    for name in selected:
        try:
            service.config.projects.get(name)
            configured = True
        except Exception:
            configured = False
            attention.append(
                {
                    "source": "project",
                    "repository": name,
                    "reason": "project_config_missing",
                }
            )
        projects.append({"repository": name, "configured": configured})

    if requested and requested not in allowed:
        attention.append(
            {
                "source": "project",
                "repository": requested[:256],
                "reason": "repository_not_allowed",
            }
        )
    return {
        "requested": requested,
        "requested_available": requested in allowed if requested else None,
        "count": len(allowed),
        "items": projects,
        "truncated": not requested and len(allowed) > len(selected),
        "filter_supported": True,
    }, attention


def build_gpt_context(
    service: Any,
    *,
    repository: str | None = None,
    now: datetime | None = None,
    runtime_snapshot: Mapping[str, object] | None = None,
    project_limit: int = DEFAULT_PROJECT_LIMIT,
    recent_task_limit: int = DEFAULT_RECENT_TASK_LIMIT,
    max_bytes: int = DEFAULT_CONTEXT_MAX_BYTES,
    available_features: Iterable[str] = _LOCAL_FEATURES,
    feature_transports: Mapping[str, Iterable[str]] | None = None,
) -> dict[str, object]:
    if project_limit < 1 or recent_task_limit < 0 or max_bytes < 1024:
        raise ContextProjectionError("invalid GPT context projection limits")
    generated_at = now or datetime.now(timezone.utc)
    if generated_at.tzinfo is None or generated_at.utcoffset() is None:
        raise ContextProjectionError("now must be timezone-aware")

    runtime_attention: list[dict[str, str]] = []
    if runtime_snapshot is None:
        try:
            runtime_snapshot = build_runtime_snapshot(service)
        except Exception:
            runtime_snapshot = {"overall": "degraded", "components": {}}
            runtime_attention.append(
                {
                    "source": "runtime",
                    "reason": "runtime_state_unavailable",
                }
            )

    raw_components = runtime_snapshot.get("components")
    component_mapping = raw_components if isinstance(raw_components, Mapping) else {}
    components = {
        str(name): _component_projection(str(name), raw)
        for name, raw in sorted(component_mapping.items(), key=lambda item: str(item[0]))
    }
    for name, item in components.items():
        if item["needs_attention"]:
            runtime_attention.append(
                {
                    "source": "component",
                    "component": name,
                    "reason": str(item.get("reason") or item["state"]),
                }
            )

    projects, project_attention = _project_context(
        service,
        repository=repository,
        project_limit=project_limit,
    )
    tasks, task_attention = _task_context(
        service,
        recent_limit=recent_task_limit,
    )
    attention = runtime_attention + project_attention + task_attention
    protocol = build_protocol_contract()
    payload: dict[str, object] = {
        "schema": GPT_CONTEXT_SCHEMA,
        "generated_at": generated_at.isoformat(),
        "needs_attention": bool(attention),
        "attention": attention,
        "runtime": {
            "overall": _text(runtime_snapshot.get("overall"), limit=40)
            or "unavailable",
            "runner_id": _text(service.config.runner.runner_id, limit=128),
            "components": components,
        },
        "projects": projects,
        "tasks": tasks,
        "control_plane": {
            "cwapi_version": protocol["cwapi_version"],
            "subject_protocol": protocol["subject_protocol"],
            "compatibility": protocol["compatibility"],
            "features": build_feature_contract(
                available=available_features,
                transports=feature_transports,
            ),
        },
        "detail_policy": {
            "actions": "call actions.get only for selected actions",
            "logs": "call results.get with detail=logs only when needed",
            "results": "summary by default; explicit results.get for detail",
            "local_paths": "returned only by explicit logs or manifest detail",
        },
        "sources": {
            "configuration": "validated AppConfig policy projection",
            "runtime": "cwapi.runtime.state.v1 projection",
            "tasks": "backend SQLite task-state projection",
        },
    }
    size = len(
        json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    )
    if size > max_bytes:
        raise ContextProjectionError(
            f"GPT context exceeds byte budget: {size} > {max_bytes}"
        )
    return payload


__all__ = [
    "ContextProjectionError",
    "DEFAULT_CONTEXT_MAX_BYTES",
    "build_gpt_context",
]
