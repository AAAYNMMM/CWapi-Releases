from __future__ import annotations

from ..action_registry import resolve_action


def get_command_args(arguments: dict) -> list[str]:
    return resolve_action("git_status", arguments)
