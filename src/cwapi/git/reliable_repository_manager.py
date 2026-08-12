from __future__ import annotations

from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
import threading
import time
from typing import Iterator

from cwapi.config import ProjectConfig
from cwapi.security import ensure_within, validate_task_id

from .repository_manager import (
    RepositoryError,
    RepositoryManager,
    WorkspaceLease,
    _normalize_remote,
    _run_git,
)


class ReliableRepositoryManager(RepositoryManager):
    """Coordinate mirror maintenance and avoid unconditional task fetches."""

    _registry_lock = threading.RLock()
    _repository_locks: dict[str, threading.RLock] = {}
    _last_fetch_monotonic: dict[str, float] = {}
    _thread_context = threading.local()

    fetch_attempts = 3
    fetch_backoff_seconds = 0.5
    fetch_freshness_seconds = 30.0

    @classmethod
    def _lock_for(cls, mirror: Path) -> threading.RLock:
        key = str(mirror.resolve())
        with cls._registry_lock:
            return cls._repository_locks.setdefault(key, threading.RLock())

    @classmethod
    def _last_fetch(cls, mirror: Path) -> float | None:
        key = str(mirror.resolve())
        with cls._registry_lock:
            return cls._last_fetch_monotonic.get(key)

    @classmethod
    def _record_fetch(cls, mirror: Path) -> None:
        key = str(mirror.resolve())
        with cls._registry_lock:
            cls._last_fetch_monotonic[key] = time.monotonic()

    @contextmanager
    def _expected_commit(self, commit: str) -> Iterator[None]:
        previous = getattr(self._thread_context, "expected_commit", None)
        self._thread_context.expected_commit = commit
        try:
            yield
        finally:
            if previous is None:
                try:
                    del self._thread_context.expected_commit
                except AttributeError:
                    pass
            else:
                self._thread_context.expected_commit = previous

    def _run_with_retry(
        self,
        args: list[str],
        *,
        timeout_seconds: int,
        operation: str,
    ):
        last_error: RepositoryError | None = None
        for attempt in range(1, self.fetch_attempts + 1):
            try:
                return _run_git(
                    args,
                    timeout_seconds=timeout_seconds,
                    runtime=self.git_runtime,
                )
            except RepositoryError as exc:
                last_error = exc
                if attempt >= self.fetch_attempts:
                    break
                time.sleep(self.fetch_backoff_seconds * (2 ** (attempt - 1)))
        assert last_error is not None
        raise RepositoryError(
            f"{operation} 在 {self.fetch_attempts} 次尝试后失败：{last_error}"
        ) from last_error

    def _validate_existing_mirror(
        self,
        *,
        project: ProjectConfig,
        mirror: Path,
    ) -> None:
        if not (mirror / "HEAD").exists():
            raise RepositoryError(f"Mirror 目录不是裸 Git 仓库：{mirror}")
        origin = _run_git(
            ["--git-dir", str(mirror), "config", "--get", "remote.origin.url"],
            timeout_seconds=30,
            runtime=self.git_runtime,
        ).stdout.strip()
        if _normalize_remote(origin) != _normalize_remote(project.remote_url):
            raise RepositoryError(
                f"Mirror origin 不匹配：{origin} != {project.remote_url}"
            )

    def ensure_mirror(self, project: ProjectConfig) -> Path:
        self.validate_project_remote(project)
        mirror = self.mirror_path(project.repository_id)
        lock = self._lock_for(mirror)
        with lock:
            if not mirror.exists():
                mirror.parent.mkdir(parents=True, exist_ok=True)
                self._run_with_retry(
                    ["clone", "--mirror", project.remote_url, str(mirror)],
                    timeout_seconds=self.config.fetch_timeout_seconds,
                    operation="Git mirror clone",
                )
                self._record_fetch(mirror)
                return mirror

            self._validate_existing_mirror(project=project, mirror=mirror)
            expected_commit = getattr(
                self._thread_context,
                "expected_commit",
                None,
            )
            if expected_commit and self.commit_exists(mirror, str(expected_commit)):
                return mirror

            last_fetch = self._last_fetch(mirror)
            if (
                expected_commit is None
                and last_fetch is not None
                and time.monotonic() - last_fetch < self.fetch_freshness_seconds
            ):
                return mirror

            self._run_with_retry(
                [
                    "--git-dir",
                    str(mirror),
                    "fetch",
                    "--prune",
                    "--tags",
                    "origin",
                    "+refs/heads/*:refs/heads/*",
                ],
                timeout_seconds=self.config.fetch_timeout_seconds,
                operation="Git mirror fetch",
            )
            self._record_fetch(mirror)
            return mirror

    def prepare(
        self,
        *,
        project: ProjectConfig,
        task_id: str,
        expected_commit: str,
    ) -> WorkspaceLease:
        validate_task_id(task_id)
        mirror = self.mirror_path(project.repository_id)
        with self._lock_for(mirror):
            with self._expected_commit(expected_commit):
                mirror = self.ensure_mirror(project)
            if not self.commit_exists(mirror, expected_commit):
                raise RepositoryError(
                    "EXPECTED_COMMIT_NOT_FOUND：远程仓库中不存在提交 "
                    f"{expected_commit}"
                )

            worktree = self.worktree_path(project.repository_id, task_id)
            marker_path = self._marker_path(worktree)
            if not worktree.exists() and marker_path.exists():
                marker_path.unlink()

            existing_marker = self._read_marker(worktree) if worktree.exists() else None
            if existing_marker is not None:
                try:
                    actual = _run_git(
                        ["rev-parse", "HEAD"],
                        cwd=worktree,
                        timeout_seconds=30,
                        runtime=self.git_runtime,
                    ).stdout.strip()
                except RepositoryError:
                    actual = ""
                if (
                    existing_marker.get("repository") == project.repository_id
                    and existing_marker.get("task_id") == task_id
                    and actual == expected_commit
                ):
                    return WorkspaceLease(
                        repository=project.repository_id,
                        task_id=task_id,
                        expected_commit=expected_commit,
                        actual_commit=actual,
                        mirror_path=mirror,
                        worktree_path=worktree,
                        managed=True,
                        created_at=str(existing_marker.get("created_at", "")),
                    )
                self.release_path(mirror=mirror, worktree=worktree)

            worktree.parent.mkdir(parents=True, exist_ok=True)
            _run_git(
                [
                    "--git-dir",
                    str(mirror),
                    "worktree",
                    "add",
                    "--detach",
                    str(worktree),
                    expected_commit,
                ],
                timeout_seconds=self.config.fetch_timeout_seconds,
                runtime=self.git_runtime,
            )
            actual_commit = _run_git(
                ["rev-parse", "HEAD"],
                cwd=worktree,
                timeout_seconds=30,
                runtime=self.git_runtime,
            ).stdout.strip()
            if actual_commit != expected_commit:
                self.release_path(mirror=mirror, worktree=worktree)
                raise RepositoryError(
                    f"Worktree commit 不匹配：{actual_commit} != {expected_commit}"
                )

            lease = WorkspaceLease(
                repository=project.repository_id,
                task_id=task_id,
                expected_commit=expected_commit,
                actual_commit=actual_commit,
                mirror_path=mirror,
                worktree_path=worktree,
                managed=True,
                created_at=datetime.now(timezone.utc).isoformat(),
            )
            self._write_marker(lease)
            return lease

    def release_path(self, *, mirror: Path, worktree: Path) -> None:
        with self._lock_for(mirror):
            target = ensure_within(self.config.worktrees_path, worktree)
            marker = self._marker_path(target)
            try:
                _run_git(
                    [
                        "--git-dir",
                        str(mirror),
                        "worktree",
                        "remove",
                        "--force",
                        str(target),
                    ],
                    timeout_seconds=120,
                    runtime=self.git_runtime,
                )
            except RepositoryError:
                self._remove_managed_path(target)
            if marker.exists():
                marker.unlink()

    def cleanup_stale(self, *, now: datetime | None = None) -> list[dict[str, str]]:
        removed = super().cleanup_stale(now=now)
        if self.config.mirrors_path.exists():
            for mirror in self.config.mirrors_path.glob("*.git"):
                if not (mirror / "HEAD").exists():
                    continue
                with self._lock_for(mirror):
                    try:
                        _run_git(
                            ["--git-dir", str(mirror), "worktree", "prune"],
                            timeout_seconds=60,
                            runtime=self.git_runtime,
                        )
                    except RepositoryError:
                        pass
        return removed


__all__ = ["ReliableRepositoryManager"]
