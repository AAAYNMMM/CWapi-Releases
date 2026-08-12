from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
import hashlib
from pathlib import Path
import threading
from typing import Iterator

from .capability_policy import (
    CodexCapabilityPolicy,
    load_codex_capability_policy as _load_active_policy,
)


@dataclass(frozen=True)
class TaskPolicySnapshot:
    task_id: str
    path: Path
    sha256: str
    policy: CodexCapabilityPolicy


_lock = threading.RLock()
_task_local = threading.local()
_snapshots: dict[tuple[str, str, str], TaskPolicySnapshot] = {}
_MAX_SNAPSHOTS = 2048


@contextmanager
def task_policy_scope(task_id: str) -> Iterator[None]:
    previous = getattr(_task_local, "task_id", None)
    _task_local.task_id = task_id
    try:
        yield
    finally:
        if previous is None:
            try:
                del _task_local.task_id
            except AttributeError:
                pass
        else:
            _task_local.task_id = previous


def current_task_id() -> str | None:
    value = getattr(_task_local, "task_id", None)
    return str(value) if value else None


def _snapshot_key(
    task_id: str,
    path: Path,
    default_permission_profile: str,
) -> tuple[str, str, str]:
    return (
        task_id,
        str(path.resolve()),
        default_permission_profile,
    )


def load_task_capability_policy(
    path: Path,
    *,
    default_permission_profile: str,
) -> CodexCapabilityPolicy:
    task_id = current_task_id()
    if not task_id:
        return _load_active_policy(
            path,
            default_permission_profile=default_permission_profile,
        )

    key = _snapshot_key(task_id, path, default_permission_profile)
    with _lock:
        existing = _snapshots.get(key)
        if existing is not None:
            return existing.policy

    raw = path.read_bytes()
    policy = _load_active_policy(
        path,
        default_permission_profile=default_permission_profile,
    )
    snapshot = TaskPolicySnapshot(
        task_id=task_id,
        path=path.resolve(),
        sha256=hashlib.sha256(raw).hexdigest(),
        policy=policy,
    )
    with _lock:
        existing = _snapshots.get(key)
        if existing is not None:
            return existing.policy
        if len(_snapshots) >= _MAX_SNAPSHOTS:
            oldest_key = next(iter(_snapshots))
            _snapshots.pop(oldest_key, None)
        _snapshots[key] = snapshot
    return policy


def task_policy_snapshot(task_id: str) -> TaskPolicySnapshot | None:
    with _lock:
        for key, snapshot in reversed(tuple(_snapshots.items())):
            if key[0] == task_id:
                return snapshot
    return None


def release_task_policy_snapshot(task_id: str) -> None:
    with _lock:
        keys = [key for key in _snapshots if key[0] == task_id]
        for key in keys:
            _snapshots.pop(key, None)


__all__ = [
    "TaskPolicySnapshot",
    "current_task_id",
    "load_task_capability_policy",
    "release_task_policy_snapshot",
    "task_policy_scope",
    "task_policy_snapshot",
]
