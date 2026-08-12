from __future__ import annotations

from copy import deepcopy
from dataclasses import dataclass
from typing import Any

from .action_registry import KNOWN_ACTIONS, action_catalog
from .readonly_inputs import READONLY_INPUT_PREFIX


def _string(*, pattern: str | None = None, max_length: int | None = None) -> dict[str, Any]:
    value: dict[str, Any] = {"type": "string", "minLength": 1}
    if pattern is not None:
        value["pattern"] = pattern
    if max_length is not None:
        value["maxLength"] = max_length
    return value


def _string_array(*, max_items: int = 100) -> dict[str, Any]:
    return {
        "type": "array",
        "items": {"type": "string"},
        "maxItems": max_items,
    }


def _object(
    properties: dict[str, Any] | None = None,
    *,
    required: tuple[str, ...] = (),
    constraints: tuple[str, ...] = (),
) -> dict[str, Any]:
    schema: dict[str, Any] = {
        "type": "object",
        "additionalProperties": False,
        "properties": properties or {},
    }
    if required:
        schema["required"] = list(required)
    if constraints:
        schema["x-cwapi-constraints"] = list(constraints)
    return schema


def _readonly_inputs() -> dict[str, Any]:
    return {
        "type": "array",
        "minItems": 1,
        "maxItems": 16,
        "items": _object(
            {
                "path": _string(
                    pattern=rf"^{READONLY_INPUT_PREFIX}/.+\.(json|pt)$"
                ),
                "sha256": _string(pattern="^[0-9a-f]{64}$"),
            },
            required=("path", "sha256"),
        ),
    }


def _codex_interactions() -> dict[str, Any]:
    timed = {"after_ms": {"type": "integer", "minimum": 0}}
    return {
        "type": "array",
        "items": {
            "oneOf": [
                _object(
                    {
                        "type": {"const": "write"},
                        **timed,
                        "text": {"type": "string"},
                        "data_base64": {"type": "string"},
                    },
                    required=("type",),
                    constraints=("Exactly one of text or data_base64 is required.",),
                ),
                _object(
                    {
                        "type": {"const": "resize"},
                        **timed,
                        "rows": {"type": "integer", "minimum": 1},
                        "cols": {"type": "integer", "minimum": 1},
                    },
                    required=("type", "rows", "cols"),
                ),
                _object(
                    {"type": {"enum": ["close_stdin", "terminate"]}, **timed},
                    required=("type",),
                ),
            ]
        },
        "x-cwapi-constraints": ["after_ms values must be non-decreasing."],
    }


def _codex_fs_operations() -> dict[str, Any]:
    rooted = {"root": _string(), "path": _string()}
    return {
        "type": "array",
        "minItems": 1,
        "maxItems": 200,
        "items": {
            "oneOf": [
                _object(
                    {
                        "type": {"const": "read_file"},
                        **rooted,
                        "encoding": _string(),
                    },
                    required=("type", "root", "path"),
                ),
                _object(
                    {"type": {"enum": ["metadata", "read_directory"]}, **rooted},
                    required=("type", "root", "path"),
                ),
                _object(
                    {
                        "type": {"const": "watch"},
                        **rooted,
                        "watch_id": _string(),
                    },
                    required=("type", "root", "path"),
                ),
                _object(
                    {
                        "type": {"const": "write_file"},
                        **rooted,
                        "text": {"type": "string"},
                        "data_base64": {"type": "string"},
                    },
                    required=("type", "root", "path"),
                    constraints=("Exactly one of text or data_base64 is required.",),
                ),
                _object(
                    {
                        "type": {"const": "create_directory"},
                        **rooted,
                        "recursive": {"type": "boolean"},
                    },
                    required=("type", "root", "path"),
                ),
                _object(
                    {
                        "type": {"const": "remove"},
                        **rooted,
                        "recursive": {"type": "boolean"},
                        "force": {"type": "boolean"},
                    },
                    required=("type", "root", "path"),
                ),
                _object(
                    {
                        "type": {"const": "copy"},
                        "source_root": _string(),
                        "source_path": _string(),
                        "destination_root": _string(),
                        "destination_path": _string(),
                        "recursive": {"type": "boolean"},
                    },
                    required=(
                        "type",
                        "source_root",
                        "source_path",
                        "destination_root",
                        "destination_path",
                    ),
                ),
                _object(
                    {"type": {"const": "unwatch"}, "watch_id": _string()},
                    required=("type", "watch_id"),
                ),
                _object(
                    {
                        "type": {"const": "wait"},
                        "milliseconds": {"type": "integer", "minimum": 1},
                    },
                    required=("type", "milliseconds"),
                ),
            ]
        },
    }


@dataclass(frozen=True)
class ActionDiscoveryDetail:
    arguments_schema: dict[str, Any]
    default_timeout_seconds: int
    category: str
    side_effects: bool
    derived_arguments: tuple[str, ...] = ()
    safety_notes: tuple[str, ...] = ()


_NO_ARGS = _object()
_PROJECT_ROOT = {
    **_string(),
    "x-cwapi-derived": True,
    "description": "Derived from the configured project; callers normally omit it.",
}
_SCRIPT_SHA = _string(pattern="^[0-9a-fA-F]{64}$")
_AUTOMATION_ARGS = _string_array(max_items=100)
_CARGO_PROPERTIES = {
    "workspace": {"type": "boolean", "default": True},
    "all_targets": {"type": "boolean", "default": False},
    "package": _string(pattern="^[A-Za-z0-9_-]{1,100}$"),
}

_DETAILS: dict[str, ActionDiscoveryDetail] = {
    "dry_run": ActionDiscoveryDetail(_NO_ARGS, 60, "protocol", False),
    "python_environment": ActionDiscoveryDetail(_NO_ARGS, 60, "python", False),
    "git_rev_parse": ActionDiscoveryDetail(_NO_ARGS, 60, "git", False),
    "git_status": ActionDiscoveryDetail(_NO_ARGS, 60, "git", False),
    "python_pip_check": ActionDiscoveryDetail(_NO_ARGS, 300, "python", False),
    "python_compileall": ActionDiscoveryDetail(
        _object(
            {
                "paths": _string_array(),
                "quiet": {"type": "boolean", "default": True},
            }
        ),
        600,
        "python",
        False,
    ),
    "dsca_training": ActionDiscoveryDetail(
        _object(
            {
                "source_root": _PROJECT_ROOT,
                "config_path": _string(
                    pattern=rf"^{READONLY_INPUT_PREFIX}/.+\.json$"
                ),
                "readonly_inputs": _readonly_inputs(),
            },
            required=("config_path", "readonly_inputs"),
            constraints=(
                "config_path must also appear in readonly_inputs and end in .json.",
            ),
        ),
        86400,
        "automation",
        True,
        derived_arguments=("source_root",),
        safety_notes=("All read-only inputs are SHA-256 bound.",),
    ),
    "repository_automation": ActionDiscoveryDetail(
        _object(
            {
                "project_root": _PROJECT_ROOT,
                "script_path": _string(pattern="^automation/.+\\.(py|ps1)$"),
                "script_sha256": _SCRIPT_SHA,
                "arguments": _AUTOMATION_ARGS,
            },
            required=("script_path", "script_sha256", "arguments"),
        ),
        3600,
        "automation",
        True,
        derived_arguments=("project_root",),
        safety_notes=(
            "Script must be repository-relative under automation/ and hash-bound.",
        ),
    ),
    "local_command": ActionDiscoveryDetail(
        _object(
            {
                "project_root": _PROJECT_ROOT,
                "command": _string(max_length=16384),
            },
            required=("command",),
        ),
        3600,
        "automation",
        True,
        derived_arguments=("project_root",),
        safety_notes=(
            "Controlled PowerShell only; high-risk fragments and protected paths are denied.",
        ),
    ),
    "pytest": ActionDiscoveryDetail(
        _object(
            {
                "paths": _string_array(),
                "extra_args": _string_array(max_items=100),
            },
            constraints=("Only the pytest flags accepted by the Action Registry are allowed.",),
        ),
        1800,
        "testing",
        False,
    ),
    "pytest_full": ActionDiscoveryDetail(
        _object(
            {"extra_args": _string_array(max_items=100)},
            constraints=("paths is intentionally unavailable; configured test paths are used.",),
        ),
        7200,
        "testing",
        False,
    ),
    "cargo_check": ActionDiscoveryDetail(
        _object(deepcopy(_CARGO_PROPERTIES)),
        3600,
        "cargo",
        False,
    ),
    "cargo_test": ActionDiscoveryDetail(
        _object(
            {
                **deepcopy(_CARGO_PROPERTIES),
                "no_fail_fast": {"type": "boolean", "default": True},
            }
        ),
        7200,
        "cargo",
        False,
    ),
    "cargo_fmt_check": ActionDiscoveryDetail(_NO_ARGS, 600, "cargo", False),
    "collect_files": ActionDiscoveryDetail(
        _object(
            {
                "paths": _string_array(),
                "max_files": {"type": "integer", "minimum": 1, "maximum": 10000},
                "max_file_bytes": {
                    "type": "integer",
                    "minimum": 1,
                    "maximum": 67108864,
                },
            }
        ),
        600,
        "evidence",
        False,
    ),
    "collect_hashes": ActionDiscoveryDetail(
        _object(
            {
                "paths": _string_array(),
                "max_files": {"type": "integer", "minimum": 1, "maximum": 10000},
                "max_file_bytes": {
                    "type": "integer",
                    "minimum": 1,
                    "maximum": 67108864,
                },
            }
        ),
        600,
        "evidence",
        False,
    ),
    "codex_session": ActionDiscoveryDetail(
        _object(
            {
                "script_path": _string(pattern="^automation/.+\\.(py|ps1)$"),
                "script_sha256": _SCRIPT_SHA,
                "arguments": _AUTOMATION_ARGS,
                "permission_profile": _string(),
                "tty": {"type": "boolean"},
                "stream_stdin": {"type": "boolean"},
                "stream_stdout_stderr": {"type": "boolean", "default": True},
                "rows": {"type": "integer", "minimum": 1, "default": 40},
                "cols": {"type": "integer", "minimum": 1, "default": 120},
                "output_bytes_cap": {"type": "integer", "minimum": 1024},
                "disable_timeout": {"type": "boolean"},
                "interactions": _codex_interactions(),
                "expected_exit_codes": {
                    "type": "array",
                    "minItems": 1,
                    "items": {"type": "integer"},
                    "default": [0],
                },
            },
            required=("script_path", "script_sha256", "arguments"),
        ),
        3600,
        "codex",
        True,
        safety_notes=("Script and all Codex capabilities remain policy-bound.",),
    ),
    "codex_mcp_status": ActionDiscoveryDetail(
        _object(
            {
                "detail": {"type": "string", "enum": ["full", "summary"]},
                "cursor": _string(),
                "limit": {"type": "integer", "minimum": 1},
            }
        ),
        300,
        "codex",
        False,
    ),
    "codex_mcp_resource": ActionDiscoveryDetail(
        _object(
            {"server": _string(), "uri": _string()},
            required=("server", "uri"),
        ),
        900,
        "codex",
        False,
    ),
    "codex_mcp_tool": ActionDiscoveryDetail(
        _object(
            {
                "server": _string(),
                "tool": _string(),
                "arguments": {"type": "object"},
                "meta": {"type": "object"},
                "permission_profile": _string(),
            },
            required=("server", "tool"),
        ),
        1800,
        "codex",
        True,
        safety_notes=("Server and tool must be allowed by the task-local capability policy.",),
    ),
    "codex_browser": ActionDiscoveryDetail(
        _object(
            {
                "tool": _string(),
                "arguments": {"type": "object"},
                "meta": {"type": "object"},
                "permission_profile": _string(),
            },
            required=("tool",),
        ),
        1800,
        "codex",
        True,
        safety_notes=("Browser server and tool must be allowed by capability policy.",),
    ),
    "codex_fs": ActionDiscoveryDetail(
        _object({"operations": _codex_fs_operations()}, required=("operations",)),
        1800,
        "codex",
        True,
        safety_notes=("Every root and operation is checked by capability policy.",),
    ),
}

_EXPECTED_DISCOVERY_ACTIONS = set(KNOWN_ACTIONS) | {"dry_run"}
if set(_DETAILS) != _EXPECTED_DISCOVERY_ACTIONS:
    missing = sorted(_EXPECTED_DISCOVERY_ACTIONS - set(_DETAILS))
    extra = sorted(set(_DETAILS) - _EXPECTED_DISCOVERY_ACTIONS)
    raise RuntimeError(f"Action discovery metadata drift: missing={missing} extra={extra}")

_CATALOG = {str(item["name"]): dict(item) for item in action_catalog()}


def action_discovery_details() -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    for name in sorted(_DETAILS):
        detail = _DETAILS[name]
        catalog = _CATALOG[name]
        result.append(
            {
                "name": name,
                "summary": str(catalog["description"]),
                "category": detail.category,
                "executes_repository_code": bool(
                    catalog.get("executes_repository_code", False)
                ),
                "side_effects": detail.side_effects,
                "internal": bool(catalog.get("internal", False)),
                "default_timeout_seconds": detail.default_timeout_seconds,
                "derived_arguments": list(detail.derived_arguments),
                "safety_notes": list(detail.safety_notes),
                "arguments_schema": deepcopy(detail.arguments_schema),
                "validator_authority": "cwapi.execution.action_registry.validate_action_arguments",
            }
        )
    return result


def action_discovery_detail(name: str) -> dict[str, Any]:
    normalized = str(name).strip()
    for detail in action_discovery_details():
        if detail["name"] == normalized:
            return detail
    raise KeyError(normalized)


__all__ = [
    "ActionDiscoveryDetail",
    "action_discovery_detail",
    "action_discovery_details",
]
