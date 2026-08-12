from __future__ import annotations

import argparse
from pathlib import Path
import subprocess
from typing import Mapping, Sequence

from .repository_automation import (
    _FORBIDDEN_FRAGMENTS,
    _PROTECTED_FRAGMENTS,
    _build_environment,
)


_MAX_COMMAND_LENGTH = 16384
_LOCAL_FORBIDDEN_FRAGMENTS = (
    "git clean -fdx",
    "remove-item -recurse -force",
    "rd /s /q",
    "rmdir /s /q",
    "del /f /s /q",
)


class LocalCommandError(ValueError):
    pass


def _validate_command(value: object) -> str:
    if not isinstance(value, str):
        raise LocalCommandError("command 必须是字符串。")
    command = value.strip()
    if not command:
        raise LocalCommandError("command 不能为空。")
    if len(command) > _MAX_COMMAND_LENGTH:
        raise LocalCommandError(
            f"command 长度超过限制：{len(command)} > {_MAX_COMMAND_LENGTH}"
        )
    if "\x00" in command:
        raise LocalCommandError("command 包含 NUL 控制字符。")

    normalized = command.casefold().replace("\\", "/")
    for fragment in (*_FORBIDDEN_FRAGMENTS, *_LOCAL_FORBIDDEN_FRAGMENTS):
        if fragment in normalized:
            raise LocalCommandError(f"command 包含禁止的高风险操作片段：{fragment}")
    for fragment in _PROTECTED_FRAGMENTS:
        if fragment.replace("\\", "/") in normalized:
            raise LocalCommandError(
                f"command 引用 CWapi 凭据或状态保护路径：{fragment}"
            )
    return command


def validate_local_command_arguments(
    arguments: Mapping[str, object],
) -> dict[str, str]:
    required = {"project_root", "command"}
    if set(arguments) != required:
        missing = required - set(arguments)
        unknown = set(arguments) - required
        raise LocalCommandError(
            f"local_command 参数不同：missing={sorted(missing)} "
            f"unknown={sorted(unknown)}"
        )

    project_root = arguments["project_root"]
    if not isinstance(project_root, str) or not project_root.strip():
        raise LocalCommandError("project_root 必须是非空字符串。")

    return {
        "project_root": str(Path(project_root).resolve()),
        "command": _validate_command(arguments["command"]),
    }


def build_action_command(
    arguments: Mapping[str, object],
    *,
    python_executable: str,
) -> list[str]:
    if not arguments:
        return [
            python_executable,
            "-m",
            "cwapi.execution.local_command",
            "--help",
        ]
    normalized = validate_local_command_arguments(arguments)
    return [
        python_executable,
        "-m",
        "cwapi.execution.local_command",
        "--project-root",
        normalized["project_root"],
        "--command",
        normalized["command"],
    ]


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="cwapi-local-command")
    parser.add_argument("--project-root", required=True)
    parser.add_argument("--command", required=True)
    return parser


def run(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    normalized = validate_local_command_arguments(
        {
            "project_root": args.project_root,
            "command": args.command,
        }
    )
    project_root = Path(normalized["project_root"]).resolve()
    if not project_root.is_dir():
        raise LocalCommandError("project_root 不存在或不是目录。")

    completed = subprocess.run(
        [
            "powershell.exe",
            "-NoLogo",
            "-NoProfile",
            "-NonInteractive",
            "-ExecutionPolicy",
            "Bypass",
            "-Command",
            normalized["command"],
        ],
        cwd=project_root,
        env=_build_environment(project_root, project_root),
        shell=False,
        check=False,
    )
    return int(completed.returncode)


def main() -> None:
    raise SystemExit(run())


if __name__ == "__main__":
    main()
