from __future__ import annotations

from typing import Iterable

from .execution.action_discovery import (
    action_discovery_detail,
    action_discovery_details,
)
from .gpt_contract import GPT_ACTION_DETAIL_SCHEMA, GPT_ACTION_LIST_SCHEMA


def _enabled(name: str, allowed_actions: frozenset[str]) -> bool:
    return name in allowed_actions


def build_action_list(*, allowed_actions: Iterable[str]) -> dict[str, object]:
    allowed = frozenset(str(value) for value in allowed_actions)
    actions = []
    for detail in action_discovery_details():
        actions.append(
            {
                "name": detail["name"],
                "summary": detail["summary"],
                "category": detail["category"],
                "enabled": _enabled(str(detail["name"]), allowed),
                "side_effects": detail["side_effects"],
                "internal": detail["internal"],
                "detail_operation": "actions.get",
            }
        )
    return {
        "schema": GPT_ACTION_LIST_SCHEMA,
        "count": len(actions),
        "actions": actions,
    }


def build_action_detail(
    name: str,
    *,
    allowed_actions: Iterable[str],
) -> dict[str, object]:
    allowed = frozenset(str(value) for value in allowed_actions)
    detail = action_discovery_detail(name)
    return {
        "schema": GPT_ACTION_DETAIL_SCHEMA,
        "enabled": _enabled(str(detail["name"]), allowed),
        **detail,
    }


__all__ = ["build_action_detail", "build_action_list"]
