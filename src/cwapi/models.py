from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timezone
from importlib.resources import files
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator, FormatChecker

from .execution.action_registry import validate_action_arguments
from .hashing import content_sha256


class TaskValidationError(ValueError):
    pass


@dataclass(frozen=True)
class TaskEnvelope:
    task: dict[str, Any]
    content_hash: str

    @property
    def task_id(self) -> str:
        return str(self.task["task_id"])


def _validator() -> Draft202012Validator:
    schema_path = files("cwapi").joinpath("schemas/task-v1.schema.json")
    schema = json.loads(schema_path.read_text(encoding="utf-8"))
    return Draft202012Validator(schema, format_checker=FormatChecker())


def parse_and_validate_task(
    body: str,
    *,
    subject_task_id: str,
    runner_id: str,
    channel_ids: set[str] | frozenset[str],
    allowed_repositories: set[str] | frozenset[str],
    allowed_actions: set[str] | frozenset[str],
    now: datetime | None = None,
    max_step_timeout_seconds: int = 86400,
    max_task_steps: int = 50,
    max_relative_paths: int = 100,
    projects: Any | None = None,
) -> TaskEnvelope:
    try:
        task = json.loads(body)
    except json.JSONDecodeError as exc:
        raise TaskValidationError(f"任务正文不是合法 JSON：{exc}") from exc

    errors = sorted(_validator().iter_errors(task), key=lambda error: list(error.path))
    if errors:
        lines = []
        for error in errors[:20]:
            path = ".".join(str(part) for part in error.path) or "<root>"
            lines.append(f"{path}: {error.message}")
        raise TaskValidationError("任务 Schema 校验失败：\n" + "\n".join(lines))

    if task["task_id"] != subject_task_id:
        raise TaskValidationError("主题中的 task_id 与正文不一致。")
    if task["target_runner"] != runner_id:
        raise TaskValidationError(
            f"任务目标 Runner 为 {task['target_runner']}，本机为 {runner_id}。"
        )
    if task["channel_id"] not in channel_ids:
        raise TaskValidationError(f"不允许的 channel_id：{task['channel_id']}")
    if task["repository"] not in allowed_repositories:
        raise TaskValidationError(f"不允许的仓库：{task['repository']}")

    steps = list(task["steps"])
    if len(steps) > max_task_steps:
        raise TaskValidationError(
            f"任务步骤数量超过限制：{len(steps)} > {max_task_steps}"
        )
    seen_step_ids: set[str] = set()
    try:
        project = projects.get(str(task["repository"])) if projects is not None else None
    except Exception as exc:
        raise TaskValidationError(
            f"仓库缺少可执行项目配置：{task['repository']}"
        ) from exc
    python_executable = getattr(project, "python_executable", None)
    cargo_executable = getattr(project, "cargo_executable", "cargo")
    default_test_paths = getattr(project, "default_test_paths", ("tests",))

    for step in steps:
        step_id = str(step["step_id"])
        if step_id in seen_step_ids:
            raise TaskValidationError(f"重复的 step_id：{step_id}")
        seen_step_ids.add(step_id)

        action = str(step["action"])
        if action not in allowed_actions:
            raise TaskValidationError(f"不允许的 action：{action}")
        timeout_seconds = int(step["timeout_seconds"])
        if timeout_seconds > max_step_timeout_seconds:
            raise TaskValidationError(
                f"步骤 {step_id} 超时上限为 {max_step_timeout_seconds} 秒。"
            )
        if action == "dry_run":
            if step.get("arguments"):
                raise TaskValidationError("dry_run 不接受 arguments。")
            continue
        arguments = dict(step.get("arguments", {}))
        try:
            validate_action_arguments(
                action,
                arguments,
                python_executable=python_executable,
                cargo_executable=cargo_executable,
                default_test_paths=default_test_paths,
                max_relative_paths=max_relative_paths,
            )
        except (ValueError, TypeError) as exc:
            raise TaskValidationError(
                f"步骤 {step_id} 的 arguments 无效：{exc}"
            ) from exc

        root_key = None
        if action == "dsca_training":
            root_key = "source_root"
        elif action in {"repository_automation", "local_command"}:
            root_key = "project_root"
        if root_key is not None:
            if project is None:
                raise TaskValidationError(f"{action} 需要项目配置。")
            supplied_root = Path(str(arguments[root_key])).resolve()
            configured_root = Path(project.path).resolve()
            if supplied_root != configured_root:
                raise TaskValidationError(
                    f"{action} {root_key} 与项目配置路径不一致。"
                )

    has_dry_run = any(str(step["action"]) == "dry_run" for step in steps)
    has_real_action = any(str(step["action"]) != "dry_run" for step in steps)
    side_effect_actions = {
        "dsca_training",
        "repository_automation",
        "local_command",
        "codex_session",
        "codex_mcp_tool",
        "codex_browser",
        "codex_fs",
    }
    has_side_effect_action = any(
        str(step["action"]) in side_effect_actions for step in steps
    )
    if has_dry_run and has_real_action:
        raise TaskValidationError(
            "MIXED_DRY_RUN_ACTIONS：dry_run 与真实 action 不能混合。"
        )
    if has_side_effect_action and bool(task.get("continue_on_failure", False)):
        raise TaskValidationError(
            "含写入、交互、MCP、浏览器或仓库自动化 action 的任务不允许 "
            "continue_on_failure=true。"
        )

    current = now or datetime.now(timezone.utc)
    expires_at = datetime.fromisoformat(task["expires_at"].replace("Z", "+00:00"))
    created_at = datetime.fromisoformat(task["created_at"].replace("Z", "+00:00"))
    if expires_at <= current:
        raise TaskValidationError("任务已经过期。")
    if created_at > expires_at:
        raise TaskValidationError("created_at 不能晚于 expires_at。")

    return TaskEnvelope(task=task, content_hash=content_sha256(task))
