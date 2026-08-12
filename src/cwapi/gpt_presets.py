from __future__ import annotations

from collections.abc import Iterable, Mapping
from copy import deepcopy
from dataclasses import dataclass
from typing import Any, Callable

from jsonschema import Draft202012Validator

from .gpt_contract import GPT_PRESET_DETAIL_SCHEMA, GPT_PRESET_LIST_SCHEMA


class PresetError(ValueError):
    pass


@dataclass(frozen=True)
class PresetDefinition:
    name: str
    summary: str
    category: str
    required_actions: tuple[str, ...]
    parameters_schema: dict[str, Any]
    expand: Callable[[dict[str, Any]], list[dict[str, Any]]]


def _string_array(*, min_items: int = 0) -> dict[str, Any]:
    return {
        "type": "array",
        "minItems": min_items,
        "maxItems": 100,
        "items": {"type": "string", "minLength": 1, "maxLength": 512},
    }


def _parameters(
    properties: dict[str, Any] | None = None,
    *,
    required: tuple[str, ...] = (),
) -> dict[str, Any]:
    schema: dict[str, Any] = {
        "type": "object",
        "additionalProperties": False,
        "properties": properties or {},
    }
    if required:
        schema["required"] = list(required)
    return schema


def _step(
    step_id: str,
    action: str,
    arguments: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
    return {
        "step_id": step_id,
        "action": action,
        "arguments": dict(arguments or {}),
    }


def _commit_verify(_: dict[str, Any]) -> list[dict[str, Any]]:
    return [
        _step("commit", "git_rev_parse"),
        _step("workspace", "git_status"),
    ]


def _python_compile_arguments(parameters: dict[str, Any]) -> dict[str, Any]:
    arguments: dict[str, Any] = {"quiet": True}
    if "compile_paths" in parameters:
        arguments["paths"] = parameters["compile_paths"]
    return arguments


def _python_focused(parameters: dict[str, Any]) -> list[dict[str, Any]]:
    return [
        _step("commit", "git_rev_parse"),
        _step(
            "compile",
            "python_compileall",
            _python_compile_arguments(parameters),
        ),
        _step(
            "focused-tests",
            "pytest",
            {
                "paths": parameters["test_paths"],
                "extra_args": parameters.get(
                    "pytest_extra_args", ["-q", "--maxfail=1"]
                ),
            },
        ),
        _step("workspace", "git_status"),
    ]


def _python_full(parameters: dict[str, Any]) -> list[dict[str, Any]]:
    return [
        _step("commit", "git_rev_parse"),
        _step(
            "compile",
            "python_compileall",
            _python_compile_arguments(parameters),
        ),
        _step(
            "full-tests",
            "pytest_full",
            {
                "extra_args": parameters.get("pytest_extra_args", ["-q"]),
            },
        ),
        _step("workspace", "git_status"),
    ]


def _cargo_arguments(parameters: dict[str, Any]) -> dict[str, Any]:
    arguments: dict[str, Any] = {
        "workspace": parameters.get("workspace", True),
        "all_targets": parameters.get("all_targets", False),
    }
    if "package" in parameters:
        arguments["package"] = parameters["package"]
    return arguments


def _cargo_full(parameters: dict[str, Any]) -> list[dict[str, Any]]:
    base = _cargo_arguments(parameters)
    return [
        _step("commit", "git_rev_parse"),
        _step("format", "cargo_fmt_check"),
        _step("check", "cargo_check", base),
        _step(
            "tests",
            "cargo_test",
            {
                **base,
                "no_fail_fast": parameters.get("no_fail_fast", True),
            },
        ),
        _step("workspace", "git_status"),
    ]


_PYTHON_PARAMETERS = {
    "compile_paths": {
        **_string_array(min_items=1),
        "description": "Optional paths; omission uses the Action Registry project-root default.",
    },
    "pytest_extra_args": {
        **_string_array(),
        "default": ["-q"],
        "description": "Flags remain restricted by the pytest Action Registry.",
    },
}
_CARGO_PARAMETERS = {
    "workspace": {"type": "boolean", "default": True},
    "all_targets": {"type": "boolean", "default": False},
    "package": {
        "type": "string",
        "pattern": "^[A-Za-z0-9_-]{1,100}$",
    },
    "no_fail_fast": {"type": "boolean", "default": True},
}

_PRESETS: tuple[PresetDefinition, ...] = (
    PresetDefinition(
        name="cargo_full_validation",
        summary="Verify commit, formatting, Cargo check/tests, and final workspace.",
        category="cargo",
        required_actions=(
            "git_rev_parse",
            "cargo_fmt_check",
            "cargo_check",
            "cargo_test",
            "git_status",
        ),
        parameters_schema=_parameters(deepcopy(_CARGO_PARAMETERS)),
        expand=_cargo_full,
    ),
    PresetDefinition(
        name="commit_verify",
        summary="Record detached HEAD and final worktree status.",
        category="git",
        required_actions=("git_rev_parse", "git_status"),
        parameters_schema=_parameters(),
        expand=_commit_verify,
    ),
    PresetDefinition(
        name="python_focused_validation",
        summary="Verify commit, compile Python, run selected tests, and check workspace.",
        category="python",
        required_actions=(
            "git_rev_parse",
            "python_compileall",
            "pytest",
            "git_status",
        ),
        parameters_schema=_parameters(
            {
                **deepcopy(_PYTHON_PARAMETERS),
                "pytest_extra_args": {
                    **_string_array(),
                    "default": ["-q", "--maxfail=1"],
                    "description": (
                        "Flags remain restricted by the pytest Action Registry."
                    ),
                },
                "test_paths": _string_array(min_items=1),
            },
            required=("test_paths",),
        ),
        expand=_python_focused,
    ),
    PresetDefinition(
        name="python_full_validation",
        summary="Verify commit, compile Python, run configured full tests, and check workspace.",
        category="python",
        required_actions=(
            "git_rev_parse",
            "python_compileall",
            "pytest_full",
            "git_status",
        ),
        parameters_schema=_parameters(deepcopy(_PYTHON_PARAMETERS)),
        expand=_python_full,
    ),
)
_PRESET_BY_NAME = {preset.name: preset for preset in _PRESETS}


def _definition(name: str) -> PresetDefinition:
    normalized = str(name).strip()
    try:
        return _PRESET_BY_NAME[normalized]
    except KeyError as exc:
        raise KeyError(normalized) from exc


def _validate_parameters(
    definition: PresetDefinition,
    parameters: Mapping[str, Any] | None,
) -> dict[str, Any]:
    if parameters is None:
        normalized: dict[str, Any] = {}
    elif isinstance(parameters, Mapping):
        normalized = dict(parameters)
    else:
        raise PresetError("preset_parameters must be an object")
    errors = sorted(
        Draft202012Validator(definition.parameters_schema).iter_errors(normalized),
        key=lambda error: list(error.path),
    )
    if errors:
        error = errors[0]
        path = ".".join(str(part) for part in error.path) or "<root>"
        raise PresetError(
            f"invalid {definition.name} parameters at {path}: {error.message}"
        )
    return normalized


def expand_preset(
    name: str,
    parameters: Mapping[str, Any] | None = None,
) -> list[dict[str, Any]]:
    definition = _definition(name)
    normalized = _validate_parameters(definition, parameters)
    return deepcopy(definition.expand(normalized))


def build_preset_list(*, allowed_actions: Iterable[str]) -> dict[str, object]:
    allowed = frozenset(str(value) for value in allowed_actions)
    presets = []
    for definition in _PRESETS:
        missing = sorted(set(definition.required_actions) - allowed)
        presets.append(
            {
                "name": definition.name,
                "summary": definition.summary,
                "category": definition.category,
                "enabled": not missing,
                "required_actions": list(definition.required_actions),
                "detail_operation": "presets.get",
            }
        )
    return {
        "schema": GPT_PRESET_LIST_SCHEMA,
        "count": len(presets),
        "presets": presets,
    }


def build_preset_detail(
    name: str,
    *,
    allowed_actions: Iterable[str],
) -> dict[str, object]:
    definition = _definition(name)
    allowed = frozenset(str(value) for value in allowed_actions)
    missing = sorted(set(definition.required_actions) - allowed)
    return {
        "schema": GPT_PRESET_DETAIL_SCHEMA,
        "name": definition.name,
        "summary": definition.summary,
        "category": definition.category,
        "enabled": not missing,
        "required_actions": list(definition.required_actions),
        "missing_actions": missing,
        "parameters_schema": deepcopy(definition.parameters_schema),
        "expansion": {
            "actions": list(definition.required_actions),
            "final_validation": "cwapi.task.v1 parser and Action Registry",
        },
        "task_request_fields": ["preset", "preset_parameters"],
    }


__all__ = [
    "PresetError",
    "build_preset_detail",
    "build_preset_list",
    "expand_preset",
]
