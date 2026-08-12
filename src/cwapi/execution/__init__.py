from __future__ import annotations

from cwapi.codex_toolhost.shared_client import (
    shared_codex_toolhost_snapshots as _shared_codex_toolhost_snapshots,
)

from . import action_registry as _action_registry
from . import codex_tool_actions as _codex_tool_actions
from .local_command import LocalCommandError, build_action_command
from .result_capture import ExecutionResult, StepResult, build_result_payload
from .task_policy_snapshot import (
    load_task_capability_policy,
    task_policy_scope,
    task_policy_snapshot as _task_policy_snapshot_for_task,
)


def _local_command_args(
    arguments: dict,
    context: _action_registry.ActionBuildContext,
) -> list[str]:
    try:
        return build_action_command(
            arguments,
            python_executable=context.python_executable,
        )
    except LocalCommandError as exc:
        raise _action_registry.InvalidActionArguments(str(exc)) from exc


def _repository_automation_cli_args(
    arguments: dict,
    context: _action_registry.ActionBuildContext,
) -> list[str]:
    command = _action_registry._repository_automation_args(arguments, context)
    command[2] = "cwapi.repository_automation_cli"
    return command


_local_command_spec = _action_registry.ActionSpec(
    "local_command",
    _local_command_args,
    True,
    "Run a direct PowerShell command from a local project root.",
)
_action_registry._COMMAND_MAP["local_command"] = _local_command_spec

_repository_automation_spec = _action_registry._COMMAND_MAP["repository_automation"]
_action_registry._COMMAND_MAP["repository_automation"] = _action_registry.ActionSpec(
    _repository_automation_spec.name,
    _repository_automation_cli_args,
    _repository_automation_spec.executes_repository_code,
    _repository_automation_spec.description,
)
_action_registry.ALLOWED_ACTIONS = frozenset(_action_registry._COMMAND_MAP)
_action_registry.KNOWN_ACTIONS = (
    _action_registry.ALLOWED_ACTIONS | _action_registry.INTERNAL_ACTIONS
)

_original_validate_action_arguments = _action_registry.validate_action_arguments


def _validate_action_arguments(
    action: str,
    arguments: dict,
    **kwargs: object,
) -> None:
    if action == "local_command" and not arguments:
        raise _action_registry.InvalidActionArguments(
            "local_command 需要 project_root 和 command。"
        )
    _original_validate_action_arguments(action, arguments, **kwargs)


_action_registry.validate_action_arguments = _validate_action_arguments

# Both repository automation paths use a top-level CLI module. Running the
# implementation module with ``-m`` imported it during package initialization
# and produced a misleading runpy warning on every successful task.
_original_repository_automation_command = (
    _codex_tool_actions._repository_automation_command
)


def _repository_automation_command_via_cli(*args, **kwargs):
    command = _original_repository_automation_command(*args, **kwargs)
    command[2] = "cwapi.repository_automation_cli"
    return command


_codex_tool_actions._repository_automation_command = (
    _repository_automation_command_via_cli
)

# Codex tool actions used to reload the active YAML policy for every step. Bind
# the loader to a task-local immutable snapshot while keeping the existing
# action implementation and validation surface unchanged.
_codex_tool_actions.load_codex_capability_policy = load_task_capability_policy
_original_execute_codex_tool_action = _codex_tool_actions.execute_codex_tool_action


def _shared_toolhost_identity_fields() -> dict[str, int]:
    healthy = [
        snapshot
        for snapshot in _shared_codex_toolhost_snapshots()
        if snapshot.state == "healthy" and snapshot.pid is not None
    ]
    if len(healthy) != 1:
        return {}
    snapshot = healthy[0]
    return {
        "codex_app_server_pid": int(snapshot.pid),
        "codex_app_server_generation": snapshot.generation,
        "codex_app_server_startup_count": snapshot.startup_count,
    }


def _execute_codex_tool_action_with_snapshot(
    action: str,
    arguments: dict,
    **kwargs: object,
):
    task_id = str(kwargs.get("task_id") or "")
    with task_policy_scope(task_id):
        result = _original_execute_codex_tool_action(action, arguments, **kwargs)
    snapshot = _task_policy_snapshot_for_task(task_id) if task_id else None
    if result.command_receipt is not None:
        result.command_receipt.update(_shared_toolhost_identity_fields())
        if snapshot is not None:
            result.command_receipt.update(
                {
                    "policy_snapshot_task_id": snapshot.task_id,
                    "policy_snapshot_sha256": snapshot.sha256,
                }
            )
    return result


_codex_tool_actions.execute_codex_tool_action = (
    _execute_codex_tool_action_with_snapshot
)

ALLOWED_ACTIONS = _action_registry.ALLOWED_ACTIONS
INTERNAL_ACTIONS = _action_registry.INTERNAL_ACTIONS
KNOWN_ACTIONS = _action_registry.KNOWN_ACTIONS
InvalidActionArguments = _action_registry.InvalidActionArguments
UnknownActionError = _action_registry.UnknownActionError
resolve_action = _action_registry.resolve_action

__all__ = [
    "ALLOWED_ACTIONS",
    "INTERNAL_ACTIONS",
    "KNOWN_ACTIONS",
    "InvalidActionArguments",
    "UnknownActionError",
    "resolve_action",
    "ExecutionResult",
    "StepResult",
    "build_result_payload",
]
