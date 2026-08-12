from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from cwapi.security import (
    SecurityViolation,
    ensure_within,
    normalize_relative_paths,
)
from .result_capture import StepResult, make_step_result


_INTERNAL_ACTIONS = {"collect_files", "collect_hashes"}


def validate_internal_action_arguments(
    action: str,
    arguments: dict,
    *,
    max_relative_paths: int = 100,
) -> None:
    if action not in _INTERNAL_ACTIONS:
        raise ValueError(f"未知内部 action：{action}")
    unknown = set(arguments) - {"paths", "max_files", "max_file_bytes"}
    if unknown:
        raise ValueError(f"{action} 包含未知参数：{sorted(unknown)}")
    paths = arguments.get("paths", ["."])
    if not isinstance(paths, list) or not all(isinstance(value, str) for value in paths):
        raise ValueError("paths 必须是字符串数组。")
    normalize_relative_paths(
        paths,
        max_items=max_relative_paths,
        allow_dot=True,
    )
    max_files = int(arguments.get("max_files", 500))
    max_file_bytes = int(arguments.get("max_file_bytes", 8 * 1024 * 1024))
    if not 1 <= max_files <= 10000:
        raise ValueError("max_files 必须在 1 到 10000 之间。")
    if not 1 <= max_file_bytes <= 64 * 1024 * 1024:
        raise ValueError("max_file_bytes 必须在 1 到 67108864 之间。")


def _iter_files(
    workspace: Path,
    relative_paths: tuple[str, ...],
    *,
    max_files: int,
) -> list[Path]:
    selected: list[Path] = []
    for relative in relative_paths:
        candidate = ensure_within(workspace, workspace / relative)
        if not candidate.exists() or candidate.is_symlink():
            continue
        if candidate.is_file():
            ensure_within(workspace, candidate)
            selected.append(candidate)
        else:
            for path in sorted(candidate.rglob("*")):
                if len(selected) >= max_files:
                    break
                if path.is_symlink() or not path.is_file():
                    continue
                ensure_within(workspace, path)
                relative_parts = path.relative_to(workspace).parts
                if ".git" in relative_parts or "__pycache__" in relative_parts:
                    continue
                selected.append(path)
        if len(selected) >= max_files:
            break
    return selected[:max_files]


def _file_record(workspace: Path, path: Path) -> dict[str, Any]:
    stat = path.stat()
    return {
        "path": path.relative_to(workspace).as_posix(),
        "size_bytes": stat.st_size,
        "modified_ns": stat.st_mtime_ns,
    }


def execute_internal_action(
    action: str,
    arguments: dict,
    *,
    workspace: Path,
    task_id: str,
    step_id: str,
    ordinal: int,
    max_relative_paths: int = 100,
) -> StepResult:
    started_at = datetime.now(timezone.utc).isoformat()
    try:
        validate_internal_action_arguments(
            action,
            arguments,
            max_relative_paths=max_relative_paths,
        )
        relative_paths = normalize_relative_paths(
            arguments.get("paths", ["."]),
            max_items=max_relative_paths,
            allow_dot=True,
        )
        max_files = int(arguments.get("max_files", 500))
        max_file_bytes = int(arguments.get("max_file_bytes", 8 * 1024 * 1024))
        files = _iter_files(workspace, relative_paths, max_files=max_files)

        if action == "collect_files":
            payload = {
                "workspace": str(workspace),
                "file_count": len(files),
                "files": [_file_record(workspace, path) for path in files],
            }
        elif action == "collect_hashes":
            records = []
            skipped = []
            for path in files:
                size = path.stat().st_size
                relative = path.relative_to(workspace).as_posix()
                if size > max_file_bytes:
                    skipped.append(
                        {
                            "path": relative,
                            "reason": "file_too_large",
                            "size_bytes": size,
                        }
                    )
                    continue
                digest = hashlib.sha256()
                with path.open("rb") as handle:
                    for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                        digest.update(chunk)
                records.append(
                    {
                        "path": relative,
                        "size_bytes": size,
                        "sha256": digest.hexdigest(),
                    }
                )
            payload = {
                "workspace": str(workspace),
                "hashed_count": len(records),
                "hashes": records,
                "skipped": skipped,
            }
        else:
            raise ValueError(f"未知内部 action：{action}")

        stdout = json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True)
        finished_at = datetime.now(timezone.utc).isoformat()
        return make_step_result(
            task_id=task_id,
            step_id=step_id,
            action=action,
            ordinal=ordinal,
            exit_code=0,
            timed_out=False,
            stdout=stdout,
            stderr="",
            started_at=started_at,
            finished_at=finished_at,
        )
    except (OSError, ValueError, SecurityViolation) as exc:
        finished_at = datetime.now(timezone.utc).isoformat()
        return make_step_result(
            task_id=task_id,
            step_id=step_id,
            action=action,
            ordinal=ordinal,
            exit_code=None,
            timed_out=False,
            stdout="",
            stderr="",
            started_at=started_at,
            finished_at=finished_at,
            execution_status="failed",
            error_code="INTERNAL_ACTION_FAILED",
            error_message=str(exc),
        )
