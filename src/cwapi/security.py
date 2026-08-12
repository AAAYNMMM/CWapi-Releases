from __future__ import annotations

import os
import re
from pathlib import Path, PurePosixPath
from typing import Iterable


class SecurityViolation(ValueError):
    pass


_SAFE_TASK_ID = re.compile(r"^[A-Za-z0-9_.-]{8,128}$")
_SAFE_REPOSITORY = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
_SECRET_PATTERNS = (
    re.compile(r"(?i)(authorization\s*:\s*bearer\s+)[^\s]+"),
    re.compile(r"(?i)(client_secret[\"'\s:=]+)[^\"'\s,}]+"),
    re.compile(r"(?i)(refresh_token[\"'\s:=]+)[^\"'\s,}]+"),
    re.compile(r"(?i)(access_token[\"'\s:=]+)[^\"'\s,}]+"),
    re.compile(r"\bgh[pousr]_[A-Za-z0-9_]{20,}\b"),
)


def validate_task_id(value: str) -> str:
    if not _SAFE_TASK_ID.fullmatch(value):
        raise SecurityViolation(f"不安全的 task_id：{value!r}")
    return value


def validate_repository_name(value: str) -> str:
    if not _SAFE_REPOSITORY.fullmatch(value):
        raise SecurityViolation(f"不安全的仓库名：{value!r}")
    return value


def repository_key(repository: str) -> str:
    validate_repository_name(repository)
    return repository.replace("/", "__")


def normalize_relative_path(value: str, *, allow_dot: bool = True) -> str:
    if "\x00" in value:
        raise SecurityViolation("路径包含 NUL 字符。")
    candidate = value.replace("\\", "/").strip()
    if not candidate:
        raise SecurityViolation("相对路径不能为空。")
    if re.match(r"^[A-Za-z]:", candidate) or candidate.startswith("/"):
        raise SecurityViolation(f"不允许绝对路径：{value}")
    pure = PurePosixPath(candidate)
    if any(part in {"..", ""} for part in pure.parts):
        raise SecurityViolation(f"路径不能越界：{value}")
    normalized = pure.as_posix()
    if normalized == "." and not allow_dot:
        raise SecurityViolation("此处不允许使用当前目录。")
    return normalized


def normalize_relative_paths(
    values: Iterable[str],
    *,
    max_items: int,
    allow_dot: bool = True,
) -> tuple[str, ...]:
    items = tuple(
        normalize_relative_path(str(value), allow_dot=allow_dot)
        for value in values
    )
    if not items:
        raise SecurityViolation("路径列表不能为空。")
    if len(items) > max_items:
        raise SecurityViolation(f"路径数量超过限制：{len(items)} > {max_items}")
    return items


def ensure_within(root: Path, candidate: Path) -> Path:
    root_resolved = root.resolve()
    candidate_resolved = candidate.resolve(strict=False)
    try:
        common = Path(os.path.commonpath([root_resolved, candidate_resolved]))
    except ValueError as exc:
        raise SecurityViolation(f"路径不属于受控根目录：{candidate}") from exc
    if common != root_resolved:
        raise SecurityViolation(f"路径越界：{candidate}")
    return candidate_resolved


def redact_text(value: str) -> str:
    redacted = value
    for pattern in _SECRET_PATTERNS:
        if pattern.groups:
            redacted = pattern.sub(r"\1[REDACTED]", redacted)
        else:
            redacted = pattern.sub("[REDACTED]", redacted)
    return redacted


def safe_error(exc: BaseException, *, limit: int = 500) -> str:
    return redact_text(str(exc))[:limit]
