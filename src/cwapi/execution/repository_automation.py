from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import sys
from typing import Mapping, Sequence

from cwapi.security import SecurityViolation, normalize_relative_path


AUTOMATION_ROOT = "automation"
_ALLOWED_SUFFIXES = frozenset({".py", ".ps1"})
_MAX_SCRIPT_BYTES = 1024 * 1024
_MAX_ARGUMENTS = 64
_MAX_ARGUMENT_LENGTH = 1024
_PERSISTENT_DIRECTORIES = (
    "data",
    "datasets",
    "runs",
    "checkpoints",
    "artifacts",
    "cache",
    "logs",
    "local-evidence",
)

# This is a guardrail, not an operating-system sandbox. The real security boundary
# is the non-admin Windows account and its NTFS permissions.
_FORBIDDEN_FRAGMENTS = (
    "git reset --hard",
    "git clean -f",
    "git push --force",
    "format-volume",
    "clear-disk",
    "initialize-disk",
    "diskpart",
    "vssadmin delete",
    "wbadmin delete",
    "restart-computer",
    "stop-computer",
    "shutdown.exe",
    "reg delete",
    "invoke-expression",
    "-encodedcommand",
    "start-process -verb runas",
)
_PROTECTED_FRAGMENTS = (
    "cwapi.db",
    "/secrets/",
    "\\secrets\\",
    "credentials.json",
    "token.json",
    "/.ssh/",
    "\\.ssh\\",
)


class RepositoryAutomationError(ValueError):
    pass


def _script_bytes(path: Path) -> bytes:
    if path.stat().st_size > _MAX_SCRIPT_BYTES:
        raise RepositoryAutomationError(
            f"脚本超过 {_MAX_SCRIPT_BYTES} 字节限制。"
        )
    return path.read_bytes()


def _normalized_script_text(path: Path) -> str:
    try:
        text = _script_bytes(path).decode("utf-8")
    except UnicodeDecodeError as exc:
        raise RepositoryAutomationError("脚本必须是 UTF-8 文本。") from exc
    return text.replace("\r\n", "\n").replace("\r", "\n")


def _sha256(path: Path) -> str:
    return hashlib.sha256(_script_bytes(path)).hexdigest()


def _normalized_sha256(path: Path) -> str:
    normalized = _normalized_script_text(path).encode("utf-8")
    return hashlib.sha256(normalized).hexdigest()


def _accepted_script_hashes(path: Path) -> frozenset[str]:
    return frozenset({_sha256(path), _normalized_sha256(path)})


def _validate_sha256(value: object) -> str:
    if (
        not isinstance(value, str)
        or len(value) != 64
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise RepositoryAutomationError(
            "script_sha256 必须是 64 位小写十六进制。"
        )
    return value


def _validate_arguments(value: object) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise RepositoryAutomationError("arguments 必须是字符串数组。")
    if len(value) > _MAX_ARGUMENTS:
        raise RepositoryAutomationError(
            f"arguments 数量超过限制：{len(value)} > {_MAX_ARGUMENTS}"
        )
    result: list[str] = []
    for index, item in enumerate(value):
        if not item or len(item) > _MAX_ARGUMENT_LENGTH:
            raise RepositoryAutomationError(
                f"arguments[{index}] 长度必须在 1 到 {_MAX_ARGUMENT_LENGTH} 之间。"
            )
        if "\x00" in item or "\r" in item or "\n" in item:
            raise RepositoryAutomationError(
                f"arguments[{index}] 包含不允许的控制字符。"
            )
        result.append(item)
    return result


def validate_repository_automation_arguments(
    arguments: Mapping[str, object],
) -> dict[str, object]:
    required = {"project_root", "script_path", "script_sha256", "arguments"}
    if set(arguments) != required:
        missing = required - set(arguments)
        unknown = set(arguments) - required
        raise RepositoryAutomationError(
            f"repository_automation 参数不同：missing={sorted(missing)} "
            f"unknown={sorted(unknown)}"
        )

    project_root = arguments["project_root"]
    if not isinstance(project_root, str) or not project_root.strip():
        raise RepositoryAutomationError("project_root 必须是非空字符串。")

    script_path = arguments["script_path"]
    if not isinstance(script_path, str):
        raise RepositoryAutomationError("script_path 必须是字符串。")
    try:
        normalized = normalize_relative_path(script_path, allow_dot=False)
    except SecurityViolation as exc:
        raise RepositoryAutomationError(str(exc)) from exc
    path = Path(normalized)
    if not path.parts or path.parts[0] != AUTOMATION_ROOT:
        raise RepositoryAutomationError(
            f"script_path 必须位于 {AUTOMATION_ROOT}/ 下。"
        )
    if path.suffix.lower() not in _ALLOWED_SUFFIXES:
        raise RepositoryAutomationError("只允许 automation/ 下的 .py 或 .ps1 脚本。")
    if path.name.startswith("."):
        raise RepositoryAutomationError("脚本文件名不能以点开头。")

    return {
        "project_root": str(Path(project_root).resolve()),
        "script_path": normalized,
        "script_sha256": _validate_sha256(arguments["script_sha256"]),
        "arguments": _validate_arguments(arguments["arguments"]),
    }


def _resolve_workspace_script(workspace: Path, relative: str) -> Path:
    root = workspace.resolve()
    script = (root / relative).resolve()
    try:
        script.relative_to(root)
    except ValueError as exc:
        raise RepositoryAutomationError("脚本路径逃逸 detached worktree。") from exc
    if not script.is_file() or script.is_symlink():
        raise RepositoryAutomationError("脚本不存在、不是普通文件或是符号链接。")
    return script


def _scan_script(script: Path) -> None:
    text = _normalized_script_text(script)
    normalized = text.casefold().replace("\\", "/")
    for fragment in _FORBIDDEN_FRAGMENTS:
        if fragment in normalized:
            raise RepositoryAutomationError(
                f"脚本包含禁止的高风险操作片段：{fragment}"
            )
    for fragment in _PROTECTED_FRAGMENTS:
        if fragment.replace("\\", "/") in normalized:
            raise RepositoryAutomationError(
                f"脚本引用 CWapi 凭据或状态保护路径：{fragment}"
            )


def _build_script_command(script: Path, arguments: Sequence[str]) -> list[str]:
    if script.suffix.lower() == ".py":
        trace_stream = os.environ.get("CWAPI_TRACE_STREAM") == "1" or (
            os.environ.get("CWAPI_TRACE_CURRENT") == "memory"
            and os.environ.get("CWAPI_TRACE_HISTORY") == "memory"
        )
        if trace_stream:
            return [
                sys.executable,
                "-m",
                "cwapi.execution.function_trace",
                "--script",
                str(script),
                "--",
                *arguments,
            ]
        return [sys.executable, str(script), *arguments]
    return [
        "powershell.exe",
        "-NoLogo",
        "-NoProfile",
        "-NonInteractive",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        str(script),
        *arguments,
    ]


def _build_environment(workspace: Path, project_root: Path) -> dict[str, str]:
    environment = dict(os.environ)
    candidates = (workspace / "src", workspace)
    environment["PYTHONPATH"] = os.pathsep.join(
        str(candidate) for candidate in candidates if candidate.exists()
    )
    environment["CWAPI_WORKSPACE"] = str(workspace)
    environment["CWAPI_PROJECT_ROOT"] = str(project_root)
    for name in _PERSISTENT_DIRECTORIES:
        target = project_root / name
        target.mkdir(parents=True, exist_ok=True)
        environment[f"CWAPI_{name.replace('-', '_').upper()}_ROOT"] = str(target)
    return environment


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="cwapi-repository-automation")
    parser.add_argument("--project-root", required=True)
    parser.add_argument("--script", required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--arg", action="append", default=[])
    return parser


def run(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    normalized = validate_repository_automation_arguments(
        {
            "project_root": args.project_root,
            "script_path": args.script,
            "script_sha256": args.sha256,
            "arguments": list(args.arg),
        }
    )
    workspace = Path.cwd().resolve()
    project_root = Path(str(normalized["project_root"])).resolve()
    script = _resolve_workspace_script(workspace, str(normalized["script_path"]))
    if normalized["script_sha256"] not in _accepted_script_hashes(script):
        raise RepositoryAutomationError("仓库脚本 SHA-256 与 TASK 不一致。")
    _scan_script(script)
    completed = subprocess.run(
        _build_script_command(script, list(normalized["arguments"])),
        cwd=workspace,
        env=_build_environment(workspace, project_root),
        shell=False,
        check=False,
    )
    return int(completed.returncode)


def main() -> None:
    raise SystemExit(run())


if __name__ == "__main__":
    main()
