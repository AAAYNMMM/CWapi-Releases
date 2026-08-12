from __future__ import annotations

from collections.abc import Mapping
from datetime import datetime, timedelta, timezone
import json
from typing import Any
import uuid

from .config import AppConfig
from .execution.action_discovery import action_discovery_detail
from .gpt_presets import PresetError, expand_preset
from .models import TaskEnvelope, TaskValidationError, parse_and_validate_task


class TaskBuildError(ValueError):
    pass


_REQUEST_FIELDS = {
    "repository",
    "expected_commit",
    "steps",
    "action",
    "arguments",
    "timeout_seconds",
    "continue_on_failure",
    "expires_in_seconds",
    "channel_id",
    "preset",
    "preset_parameters",
}
_STEP_FIELDS = {"step_id", "action", "arguments", "timeout_seconds"}
_DERIVED_PROJECT_ROOTS = {
    "dsca_training": "source_root",
    "repository_automation": "project_root",
    "local_command": "project_root",
}


def _expand_request_preset(request: Mapping[str, Any]) -> dict[str, Any]:
    normalized = dict(request)
    raw_preset = normalized.get("preset")
    has_parameters = "preset_parameters" in normalized
    if raw_preset is None:
        if has_parameters:
            raise TaskBuildError("preset_parameters requires preset")
        return normalized
    if not isinstance(raw_preset, str) or not raw_preset.strip():
        raise TaskBuildError("preset must be a non-empty string")
    if "action" in normalized or "steps" in normalized:
        raise TaskBuildError("provide preset, action, or steps; only one is allowed")
    if "arguments" in normalized or "timeout_seconds" in normalized:
        raise TaskBuildError("arguments and timeout_seconds require action")
    try:
        steps = expand_preset(
            raw_preset.strip(),
            normalized.get("preset_parameters"),
        )
    except KeyError as exc:
        raise TaskBuildError(f"unknown preset: {raw_preset.strip()}") from exc
    except PresetError as exc:
        raise TaskBuildError(str(exc)) from exc
    normalized.pop("preset", None)
    normalized.pop("preset_parameters", None)
    normalized["steps"] = steps
    return normalized


def _task_id(prefix: str) -> str:
    normalized = str(prefix).strip()
    if not normalized:
        raise TaskBuildError("task_id_prefix must be non-empty")
    return normalized + uuid.uuid4().hex.upper()


def _security_limit(config: AppConfig, name: str, fallback: int) -> int:
    return int(getattr(config.security, name, fallback))


def _default_timeout(action: str, *, maximum: int) -> int:
    try:
        discovered = int(action_discovery_detail(action)["default_timeout_seconds"])
    except (KeyError, TypeError, ValueError):
        discovered = min(60, maximum)
    return min(discovered, maximum)


def _normalize_steps(
    raw_steps: object,
    *,
    repository: str,
    config: AppConfig,
    max_step_timeout_seconds: int,
) -> list[dict[str, Any]]:
    if not isinstance(raw_steps, list) or not raw_steps:
        raise TaskBuildError("steps must be a non-empty array")

    normalized: list[dict[str, Any]] = []
    project = None
    for index, raw_step in enumerate(raw_steps, start=1):
        if not isinstance(raw_step, Mapping):
            raise TaskBuildError(f"steps[{index - 1}] must be an object")
        unknown = set(raw_step) - _STEP_FIELDS
        if unknown:
            raise TaskBuildError(
                f"steps[{index - 1}] has unknown fields: {sorted(unknown)}"
            )

        raw_action = raw_step.get("action")
        if not isinstance(raw_action, str) or not raw_action.strip():
            raise TaskBuildError(f"steps[{index - 1}].action must be a non-empty string")
        action = raw_action.strip()

        raw_arguments = raw_step.get("arguments", {})
        if not isinstance(raw_arguments, Mapping):
            raise TaskBuildError(f"steps[{index - 1}].arguments must be an object")
        arguments = dict(raw_arguments)
        root_key = _DERIVED_PROJECT_ROOTS.get(action)
        if root_key is not None and root_key not in arguments:
            if project is None:
                try:
                    project = config.projects.get(repository)
                except Exception as exc:
                    raise TaskBuildError(
                        f"repository has no executable project config: {repository}"
                    ) from exc
            arguments[root_key] = str(project.path)

        raw_step_id = raw_step.get("step_id")
        if raw_step_id is None:
            step_id = f"step-{index:03d}"
        elif not isinstance(raw_step_id, str) or not raw_step_id.strip():
            raise TaskBuildError(f"steps[{index - 1}].step_id must be a non-empty string")
        else:
            step_id = raw_step_id.strip()

        raw_timeout = raw_step.get("timeout_seconds")
        if raw_timeout is None:
            timeout_seconds = _default_timeout(
                action,
                maximum=max_step_timeout_seconds,
            )
        elif isinstance(raw_timeout, bool) or not isinstance(raw_timeout, int):
            raise TaskBuildError(
                f"steps[{index - 1}].timeout_seconds must be an integer"
            )
        else:
            timeout_seconds = raw_timeout

        normalized.append(
            {
                "step_id": step_id,
                "action": action,
                "arguments": arguments,
                "timeout_seconds": timeout_seconds,
            }
        )
    return normalized


def build_canonical_task(
    request: Mapping[str, Any],
    *,
    config: AppConfig,
    now: datetime | None = None,
    task_id: str | None = None,
    task_id_prefix: str = "TASK",
    strict_request: bool = True,
) -> TaskEnvelope:
    if not isinstance(request, Mapping):
        raise TaskBuildError("task request must be an object")
    unknown = set(request) - _REQUEST_FIELDS
    if strict_request and unknown:
        raise TaskBuildError(f"task request has unknown fields: {sorted(unknown)}")
    request = _expand_request_preset(request)

    repository = request.get("repository")
    expected_commit = request.get("expected_commit")
    if not isinstance(repository, str) or not repository.strip():
        raise TaskBuildError("repository and expected_commit are required")
    if not isinstance(expected_commit, str) or not expected_commit.strip():
        raise TaskBuildError("repository and expected_commit are required")
    repository = repository.strip()
    expected_commit = expected_commit.strip()

    continue_on_failure = request.get("continue_on_failure", False)
    if not isinstance(continue_on_failure, bool):
        raise TaskBuildError("continue_on_failure must be a boolean")

    raw_expires = request.get("expires_in_seconds", 8 * 60 * 60)
    if isinstance(raw_expires, bool):
        raise TaskBuildError("expires_in_seconds must be an integer")
    try:
        expires_in_seconds = int(raw_expires)
    except (TypeError, ValueError) as exc:
        raise TaskBuildError("expires_in_seconds must be an integer") from exc
    if expires_in_seconds < 60 or expires_in_seconds > 24 * 60 * 60:
        raise TaskBuildError("expires_in_seconds must be between 60 and 86400")

    channels = tuple(str(value) for value in config.runner.channel_ids)
    if not channels:
        raise TaskBuildError("runner has no configured channel")
    raw_channel_id = request.get("channel_id", channels[0])
    if not isinstance(raw_channel_id, str) or not raw_channel_id.strip():
        raise TaskBuildError("channel_id must be a non-empty string")
    channel_id = raw_channel_id.strip()

    created_at = now or datetime.now(timezone.utc)
    if created_at.tzinfo is None or created_at.utcoffset() is None:
        raise TaskBuildError("now must be timezone-aware")

    max_step_timeout_seconds = _security_limit(
        config, "max_step_timeout_seconds", 86400
    )
    max_task_steps = _security_limit(config, "max_task_steps", 50)
    max_relative_paths = _security_limit(config, "max_relative_paths", 100)
    raw_steps = request.get("steps")
    raw_action = request.get("action")
    if raw_steps is not None and raw_action is not None:
        raise TaskBuildError("provide either action or steps, not both")
    if raw_action is not None:
        single_step: dict[str, Any] = {"action": raw_action}
        if "arguments" in request:
            single_step["arguments"] = request["arguments"]
        if "timeout_seconds" in request:
            single_step["timeout_seconds"] = request["timeout_seconds"]
        raw_steps = [single_step]
    elif "arguments" in request or "timeout_seconds" in request:
        raise TaskBuildError("arguments and timeout_seconds require action")
    steps = _normalize_steps(
        raw_steps,
        repository=repository,
        config=config,
        max_step_timeout_seconds=max_step_timeout_seconds,
    )

    resolved_task_id = (
        str(task_id).strip() if task_id is not None else _task_id(task_id_prefix)
    )
    if not resolved_task_id:
        raise TaskBuildError("task_id must be non-empty")
    task = {
        "schema": "cwapi.task.v1",
        "task_id": resolved_task_id,
        "created_at": created_at.isoformat(),
        "expires_at": (created_at + timedelta(seconds=expires_in_seconds)).isoformat(),
        "channel_id": channel_id,
        "target_runner": config.runner.runner_id,
        "repository": repository,
        "expected_commit": expected_commit,
        "workspace_mode": "isolated_worktree",
        "continue_on_failure": continue_on_failure,
        "steps": steps,
    }
    raw = json.dumps(task, ensure_ascii=False, separators=(",", ":"))
    try:
        return parse_and_validate_task(
            raw,
            subject_task_id=resolved_task_id,
            runner_id=config.runner.runner_id,
            channel_ids=frozenset(channels),
            allowed_repositories=config.security.allowed_repositories,
            allowed_actions=config.security.allowed_actions,
            now=created_at,
            max_step_timeout_seconds=max_step_timeout_seconds,
            max_task_steps=max_task_steps,
            max_relative_paths=max_relative_paths,
            projects=config.projects,
        )
    except TaskValidationError as exc:
        raise TaskBuildError(str(exc)) from exc


__all__ = ["TaskBuildError", "build_canonical_task"]
