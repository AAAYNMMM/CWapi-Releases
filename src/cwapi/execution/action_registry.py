from __future__ import annotations

import base64
import json
import re
import sys
from dataclasses import dataclass
from typing import Callable, Mapping, Sequence

from cwapi.security import SecurityViolation, normalize_relative_paths

from .codex_tool_actions import (
    CODEX_SIDE_EFFECT_ACTIONS,
    CODEX_TOOL_ACTIONS,
    validate_codex_tool_action_arguments,
)
from .readonly_inputs import ReadOnlyInputError, validate_dsca_training_arguments
from .local_command import LocalCommandError, build_action_command as build_local_command
from .repository_automation import (
    RepositoryAutomationError,
    validate_repository_automation_arguments,
)


class UnknownActionError(ValueError):
    def __init__(self, action: str) -> None:
        self.action = action
        super().__init__(f"Unknown action: {action}")


class InvalidActionArguments(ValueError):
    pass


@dataclass(frozen=True)
class ActionSpec:
    name: str
    builder: Callable[[dict, "ActionBuildContext"], list[str]]
    executes_repository_code: bool
    description: str


@dataclass(frozen=True)
class ActionBuildContext:
    python_executable: str = sys.executable
    cargo_executable: str = "cargo"
    git_executable: str = "git"
    default_test_paths: tuple[str, ...] = ("tests",)
    max_relative_paths: int = 100


def _string_list(arguments: Mapping[str, object], key: str) -> list[str]:
    value = arguments.get(key)
    if value is None:
        return []
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise InvalidActionArguments(f"{key} 必须是字符串数组。")
    return list(value)


def _no_args(arguments: dict, command: list[str], name: str) -> list[str]:
    if arguments:
        raise InvalidActionArguments(f"{name} 不接受参数。")
    return command


def _python_environment_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _no_args(
        arguments,
        [context.python_executable, "--version"],
        "python_environment",
    )


def _git_rev_parse_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _no_args(
        arguments,
        [context.git_executable, "rev-parse", "HEAD"],
        "git_rev_parse",
    )


def _git_status_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _no_args(
        arguments,
        [context.git_executable, "status", "--porcelain"],
        "git_status",
    )


def _python_pip_check_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _no_args(
        arguments,
        [context.python_executable, "-m", "pip", "check"],
        "python_pip_check",
    )


def _python_compileall_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    unknown = set(arguments) - {"paths", "quiet"}
    if unknown:
        raise InvalidActionArguments(f"python_compileall 包含未知参数：{sorted(unknown)}")
    paths = normalize_relative_paths(
        _string_list(arguments, "paths") or ["."],
        max_items=context.max_relative_paths,
        allow_dot=True,
    )
    command = [context.python_executable, "-m", "compileall"]
    if bool(arguments.get("quiet", True)):
        command.append("-q")
    return [*command, *paths]


def _dsca_training_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    try:
        normalized = validate_dsca_training_arguments(arguments)
    except ReadOnlyInputError as exc:
        raise InvalidActionArguments(str(exc)) from exc
    command = [
        context.python_executable,
        "-m",
        "cwapi.execution.dsca_training",
        "--source-root",
        str(normalized["source_root"]),
        "--config",
        str(normalized["config_path"]),
    ]
    for item in normalized["readonly_inputs"]:
        command.extend(("--input", str(item["path"]), str(item["sha256"])))
    return command


def _local_command_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    try:
        return build_local_command(arguments, python_executable=context.python_executable)
    except LocalCommandError as exc:
        raise InvalidActionArguments(str(exc)) from exc


def _repository_automation_args(
    arguments: dict,
    context: ActionBuildContext,
) -> list[str]:
    if not arguments:
        return [
            context.python_executable,
            "-m",
            "cwapi.execution.repository_automation",
            "--help",
        ]
    try:
        normalized = validate_repository_automation_arguments(arguments)
    except RepositoryAutomationError as exc:
        raise InvalidActionArguments(str(exc)) from exc
    command = [
        context.python_executable,
        "-m",
        "cwapi.execution.repository_automation",
        "--project-root",
        str(normalized["project_root"]),
        "--script",
        str(normalized["script_path"]),
        "--sha256",
        str(normalized["script_sha256"]),
    ]
    for item in normalized["arguments"]:
        command.append(f"--arg={item}")
    return command


def _codex_tool_args(
    action: str,
    arguments: dict,
    context: ActionBuildContext,
) -> list[str]:
    try:
        validate_codex_tool_action_arguments(action, arguments)
    except ValueError as exc:
        raise InvalidActionArguments(str(exc)) from exc
    payload = {
        "schema": "cwapi.codex-tool-dispatch.v1",
        "action": action,
        "arguments": arguments,
        "python_executable": context.python_executable,
    }
    encoded = base64.urlsafe_b64encode(
        json.dumps(
            payload,
            ensure_ascii=False,
            separators=(",", ":"),
            sort_keys=True,
        ).encode("utf-8")
    ).decode("ascii")
    return ["__cwapi_codex_tool__", encoded]


_ALLOWED_PYTEST_FLAGS = {
    "-q",
    "-x",
    "-s",
    "--disable-warnings",
    "--strict-markers",
    "--strict-config",
    "--collect-only",
}
_ALLOWED_TB = {"auto", "long", "short", "line", "native", "no"}
_EXPR_RE = re.compile(r"^[A-Za-z0-9_ .()!&|<>=:+\-/*\[\],']{1,300}$")


def _validate_pytest_extra_args(values: Sequence[str]) -> list[str]:
    result: list[str] = []
    index = 0
    while index < len(values):
        value = values[index]
        if value in _ALLOWED_PYTEST_FLAGS:
            result.append(value)
            index += 1
            continue
        if value.startswith("--maxfail="):
            suffix = value.partition("=")[2]
            if not suffix.isdigit() or not 1 <= int(suffix) <= 100:
                raise InvalidActionArguments("--maxfail 必须在 1 到 100 之间。")
            result.append(value)
            index += 1
            continue
        if value.startswith("--tb="):
            suffix = value.partition("=")[2]
            if suffix not in _ALLOWED_TB:
                raise InvalidActionArguments(f"不允许的 --tb 模式：{suffix}")
            result.append(value)
            index += 1
            continue
        if value in {"-k", "-m"}:
            if index + 1 >= len(values):
                raise InvalidActionArguments(f"{value} 缺少表达式。")
            expression = values[index + 1]
            if not _EXPR_RE.fullmatch(expression):
                raise InvalidActionArguments(f"{value} 表达式包含不允许的字符。")
            result.extend((value, expression))
            index += 2
            continue
        raise InvalidActionArguments(f"不允许的 pytest 参数：{value}")
    return result


def _pytest_args(
    arguments: dict,
    context: ActionBuildContext,
    *,
    full: bool,
) -> list[str]:
    unknown = set(arguments) - {"paths", "extra_args"}
    if unknown:
        raise InvalidActionArguments(f"pytest 包含未知参数：{sorted(unknown)}")
    raw_paths = _string_list(arguments, "paths")
    if full:
        if raw_paths:
            raise InvalidActionArguments("pytest_full 不接受 paths，请使用 pytest。")
        selected = context.default_test_paths
    else:
        selected = tuple(raw_paths) if raw_paths else context.default_test_paths
    paths = normalize_relative_paths(
        selected,
        max_items=context.max_relative_paths,
        allow_dot=True,
    )
    extra = _validate_pytest_extra_args(_string_list(arguments, "extra_args"))
    return [context.python_executable, "-m", "pytest", *extra, *paths]


def _pytest_focused_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _pytest_args(arguments, context, full=False)


def _pytest_full_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _pytest_args(arguments, context, full=True)


_CARGO_PACKAGE_RE = re.compile(r"^[A-Za-z0-9_-]{1,100}$")


def _cargo_base_args(
    arguments: dict,
    context: ActionBuildContext,
    subcommand: str,
    *,
    allow_no_fail_fast: bool = False,
) -> list[str]:
    allowed = {"workspace", "all_targets", "package"}
    if allow_no_fail_fast:
        allowed.add("no_fail_fast")
    unknown = set(arguments) - allowed
    if unknown:
        raise InvalidActionArguments(f"cargo {subcommand} 包含未知参数：{sorted(unknown)}")
    command = [context.cargo_executable, subcommand]
    package = arguments.get("package")
    if package is not None:
        if not isinstance(package, str) or not _CARGO_PACKAGE_RE.fullmatch(package):
            raise InvalidActionArguments("cargo package 名称无效。")
    elif bool(arguments.get("workspace", True)):
        command.append("--workspace")
    if bool(arguments.get("all_targets", False)):
        command.append("--all-targets")
    if package is not None:
        command.extend(("--package", package))
    if allow_no_fail_fast and bool(arguments.get("no_fail_fast", True)):
        command.append("--no-fail-fast")
    return command


def _cargo_check_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _cargo_base_args(arguments, context, "check")


def _cargo_test_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _cargo_base_args(arguments, context, "test", allow_no_fail_fast=True)


def _cargo_fmt_check_args(arguments: dict, context: ActionBuildContext) -> list[str]:
    return _no_args(
        arguments,
        [context.cargo_executable, "fmt", "--all", "--", "--check"],
        "cargo_fmt_check",
    )


_COMMAND_MAP: dict[str, ActionSpec] = {
    "python_environment": ActionSpec(
        "python_environment", _python_environment_args, False, "Python version."
    ),
    "git_rev_parse": ActionSpec(
        "git_rev_parse", _git_rev_parse_args, False, "Detached worktree HEAD."
    ),
    "git_status": ActionSpec("git_status", _git_status_args, False, "Worktree modifications."),
    "python_pip_check": ActionSpec(
        "python_pip_check", _python_pip_check_args, False, "Dependency consistency."
    ),
    "python_compileall": ActionSpec(
        "python_compileall", _python_compileall_args, True, "Compile Python sources."
    ),
    "dsca_training": ActionSpec(
        "dsca_training",
        _dsca_training_args,
        True,
        "Run the fixed DSCA Task 5H-1 training launcher.",
    ),
    "repository_automation": ActionSpec(
        "repository_automation",
        _repository_automation_args,
        True,
        "Run a hash-bound repository automation script.",
    ),
    "local_command": ActionSpec(
        "local_command",
        _local_command_args,
        True,
        "Controlled PowerShell command bound to the configured project root.",
    ),
    "pytest": ActionSpec("pytest", _pytest_focused_args, True, "Focused pytest."),
    "pytest_full": ActionSpec("pytest_full", _pytest_full_args, True, "Full pytest."),
    "cargo_check": ActionSpec("cargo_check", _cargo_check_args, True, "Cargo check."),
    "cargo_test": ActionSpec("cargo_test", _cargo_test_args, True, "Cargo test."),
    "cargo_fmt_check": ActionSpec(
        "cargo_fmt_check", _cargo_fmt_check_args, True, "Cargo format check."
    ),
}

for _codex_action in sorted(CODEX_TOOL_ACTIONS):
    _COMMAND_MAP[_codex_action] = ActionSpec(
        _codex_action,
        lambda arguments, context, action=_codex_action: _codex_tool_args(
            action,
            arguments,
            context,
        ),
        _codex_action in CODEX_SIDE_EFFECT_ACTIONS,
        "Controlled Codex app-server tool action.",
    )

ALLOWED_ACTIONS: frozenset[str] = frozenset(_COMMAND_MAP)
INTERNAL_ACTIONS: frozenset[str] = frozenset({"collect_files", "collect_hashes"})
KNOWN_ACTIONS: frozenset[str] = ALLOWED_ACTIONS | INTERNAL_ACTIONS


def resolve_action(
    action: str,
    arguments: dict,
    *,
    python_executable: str | None = None,
    cargo_executable: str = "cargo",
    git_executable: str = "git",
    default_test_paths: Sequence[str] = ("tests",),
    max_relative_paths: int = 100,
) -> list[str]:
    spec = _COMMAND_MAP.get(action)
    if spec is None:
        raise UnknownActionError(action)
    context = ActionBuildContext(
        python_executable=python_executable or sys.executable,
        cargo_executable=cargo_executable,
        git_executable=git_executable,
        default_test_paths=tuple(str(value) for value in default_test_paths),
        max_relative_paths=max_relative_paths,
    )
    try:
        return spec.builder(arguments, context)
    except SecurityViolation as exc:
        raise InvalidActionArguments(str(exc)) from exc


def validate_action_arguments(
    action: str,
    arguments: dict,
    *,
    python_executable: str | None = None,
    cargo_executable: str = "cargo",
    git_executable: str = "git",
    default_test_paths: Sequence[str] = ("tests",),
    max_relative_paths: int = 100,
) -> None:
    if action in INTERNAL_ACTIONS:
        from .internal_actions import validate_internal_action_arguments

        validate_internal_action_arguments(
            action,
            arguments,
            max_relative_paths=max_relative_paths,
        )
        return
    if action == "repository_automation" and not arguments:
        raise InvalidActionArguments("repository_automation 需要完整参数。")
    resolve_action(
        action,
        arguments,
        python_executable=python_executable,
        cargo_executable=cargo_executable,
        git_executable=git_executable,
        default_test_paths=default_test_paths,
        max_relative_paths=max_relative_paths,
    )


def action_executes_repository_code(action: str) -> bool:
    spec = _COMMAND_MAP.get(action)
    return bool(spec and spec.executes_repository_code)


def action_catalog() -> list[dict[str, object]]:
    result: list[dict[str, object]] = [
        {
            "name": "dry_run",
            "description": "Protocol-level validation without executing actions.",
            "executes_repository_code": False,
            "internal": False,
        }
    ]
    internal_descriptions = {
        "collect_files": "Collect bounded task evidence files.",
        "collect_hashes": "Collect SHA-256 evidence for bounded paths.",
    }
    for name in sorted(KNOWN_ACTIONS):
        spec = _COMMAND_MAP.get(name)
        result.append(
            {
                "name": name,
                "description": (
                    spec.description
                    if spec is not None
                    else internal_descriptions.get(name, "CWapi internal action.")
                ),
                "executes_repository_code": bool(spec and spec.executes_repository_code),
                "internal": name in INTERNAL_ACTIONS,
            }
        )
    return result
