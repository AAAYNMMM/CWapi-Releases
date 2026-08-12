from __future__ import annotations

import json
import shutil
import subprocess
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

from cwapi.config import GitConfig, ProjectConfig
from cwapi.security import (
    SecurityViolation,
    ensure_within,
    repository_key,
    safe_error,
    validate_task_id,
)

from .runtime import GitRuntime, GitRuntimeError, resolve_configured_git


class RepositoryError(RuntimeError):
    pass


@dataclass(frozen=True)
class WorkspaceLease:
    repository: str
    task_id: str
    expected_commit: str
    actual_commit: str
    mirror_path: Path
    worktree_path: Path
    managed: bool
    created_at: str


def _run_git(
    args: list[str],
    *,
    cwd: Path | None = None,
    timeout_seconds: int = 300,
    runtime: GitRuntime | None = None,
) -> subprocess.CompletedProcess[str]:
    active_runtime = runtime
    if active_runtime is None:
        try:
            active_runtime = resolve_configured_git(GitConfig())
        except GitRuntimeError as exc:
            raise RepositoryError(str(exc)) from exc
    command = active_runtime.command(*args)
    try:
        completed = subprocess.run(
            command,
            cwd=str(cwd) if cwd is not None else None,
            env=active_runtime.environment(),
            capture_output=True,
            text=True,
            shell=False,
            timeout=timeout_seconds,
            check=False,
        )
    except FileNotFoundError as exc:
        raise RepositoryError("找不到 CWapi Git 可执行文件。") from exc
    except subprocess.TimeoutExpired as exc:
        raise RepositoryError(f"Git 命令超时：git {' '.join(args)}") from exc

    if completed.returncode != 0:
        detail = completed.stderr.strip() or completed.stdout.strip()
        raise RepositoryError(
            f"Git 命令失败 ({completed.returncode})：git {' '.join(args)}\n"
            f"{detail[:1000]}"
        )
    return completed


def _normalize_remote(remote_url: str) -> str:
    value = remote_url.strip().rstrip("/")
    if value.endswith(".git"):
        value = value[:-4]
    if value.startswith("git@github.com:"):
        value = "https://github.com/" + value.split(":", 1)[1]
    return value.lower()


class RepositoryManager:
    def __init__(self, config: GitConfig) -> None:
        self.config = config
        try:
            self.git_runtime = resolve_configured_git(config)
        except GitRuntimeError as exc:
            raise RepositoryError(str(exc)) from exc
        self.config.mirrors_path.mkdir(parents=True, exist_ok=True)
        self.config.worktrees_path.mkdir(parents=True, exist_ok=True)

    def validate_project_remote(self, project: ProjectConfig) -> None:
        expected = f"https://github.com/{project.repository_id}".lower()
        normalized = _normalize_remote(project.remote_url)
        if normalized != expected:
            raise RepositoryError(
                f"项目 {project.repository_id} 的 remote_url 与仓库名不一致："
                f"{project.remote_url}"
            )

    def mirror_path(self, repository: str) -> Path:
        return ensure_within(
            self.config.mirrors_path,
            self.config.mirrors_path / f"{repository_key(repository)}.git",
        )

    def worktree_path(self, repository: str, task_id: str) -> Path:
        validate_task_id(task_id)
        return ensure_within(
            self.config.worktrees_path,
            self.config.worktrees_path / repository_key(repository) / task_id,
        )

    def ensure_mirror(self, project: ProjectConfig) -> Path:
        self.validate_project_remote(project)
        mirror = self.mirror_path(project.repository_id)
        if not mirror.exists():
            mirror.parent.mkdir(parents=True, exist_ok=True)
            _run_git(
                ["clone", "--mirror", project.remote_url, str(mirror)],
                timeout_seconds=self.config.fetch_timeout_seconds,
                runtime=self.git_runtime,
            )
        else:
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

        _run_git(
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
            runtime=self.git_runtime,
        )
        return mirror

    def commit_exists(self, mirror: Path, commit: str) -> bool:
        try:
            _run_git(
                ["--git-dir", str(mirror), "cat-file", "-e", f"{commit}^{{commit}}"],
                timeout_seconds=30,
                runtime=self.git_runtime,
            )
            return True
        except RepositoryError:
            return False

    def _marker_path(self, worktree: Path) -> Path:
        return worktree.parent / f".{worktree.name}.cwapi-workspace.json"

    def _read_marker(self, worktree: Path) -> dict[str, Any] | None:
        marker = self._marker_path(worktree)
        if not marker.exists():
            return None
        try:
            value = json.loads(marker.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return None
        return value if isinstance(value, dict) else None

    def _write_marker(self, lease: WorkspaceLease) -> None:
        marker = self._marker_path(lease.worktree_path)
        marker.write_text(
            json.dumps(
                {
                    "schema": "cwapi.workspace.v1",
                    "repository": lease.repository,
                    "task_id": lease.task_id,
                    "expected_commit": lease.expected_commit,
                    "actual_commit": lease.actual_commit,
                    "mirror_path": str(lease.mirror_path),
                    "worktree_path": str(lease.worktree_path),
                    "created_at": lease.created_at,
                },
                ensure_ascii=False,
                indent=2,
                sort_keys=True,
            ),
            encoding="utf-8",
        )

    def _remove_managed_path(self, path: Path) -> None:
        target = ensure_within(self.config.worktrees_path, path)
        if target.exists():
            shutil.rmtree(target)

    def prepare(
        self,
        *,
        project: ProjectConfig,
        task_id: str,
        expected_commit: str,
    ) -> WorkspaceLease:
        validate_task_id(task_id)
        mirror = self.ensure_mirror(project)
        if not self.commit_exists(mirror, expected_commit):
            raise RepositoryError(
                f"EXPECTED_COMMIT_NOT_FOUND：远程仓库中不存在提交 {expected_commit}"
            )

        worktree = self.worktree_path(project.repository_id, task_id)
        marker_path = self._marker_path(worktree)
        if not worktree.exists() and marker_path.exists():
            marker_path.unlink()
        try:
            _run_git(
                ["--git-dir", str(mirror), "worktree", "prune"],
                timeout_seconds=60,
                runtime=self.git_runtime,
            )
        except RepositoryError:
            pass
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

    def legacy_workspace(
        self,
        *,
        project: ProjectConfig,
        task_id: str,
        expected_commit: str,
    ) -> WorkspaceLease:
        actual = _run_git(
            ["rev-parse", "HEAD"],
            cwd=project.path,
            timeout_seconds=30,
            runtime=self.git_runtime,
        ).stdout.strip()
        return WorkspaceLease(
            repository=project.repository_id,
            task_id=task_id,
            expected_commit=expected_commit,
            actual_commit=actual,
            mirror_path=project.path / ".git",
            worktree_path=project.path,
            managed=False,
            created_at=datetime.now(timezone.utc).isoformat(),
        )

    def release_path(self, *, mirror: Path, worktree: Path) -> None:
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
        try:
            _run_git(
                ["--git-dir", str(mirror), "worktree", "prune"],
                timeout_seconds=60,
                runtime=self.git_runtime,
            )
        except RepositoryError:
            pass

    def release(self, lease: WorkspaceLease) -> None:
        if lease.managed:
            self.release_path(
                mirror=lease.mirror_path,
                worktree=lease.worktree_path,
            )

    def mark_retained(
        self,
        lease: WorkspaceLease,
        *,
        keep_until: str | None,
        status: str,
    ) -> None:
        if not lease.managed:
            return
        marker = self._marker_path(lease.worktree_path)
        metadata = self._read_marker(lease.worktree_path) or {
            "schema": "cwapi.workspace.v1",
            "repository": lease.repository,
            "task_id": lease.task_id,
            "expected_commit": lease.expected_commit,
            "actual_commit": lease.actual_commit,
            "mirror_path": str(lease.mirror_path),
            "worktree_path": str(lease.worktree_path),
            "created_at": lease.created_at,
        }
        metadata["keep_until"] = keep_until
        metadata["status"] = status
        marker.write_text(
            json.dumps(metadata, ensure_ascii=False, indent=2, sort_keys=True),
            encoding="utf-8",
        )

    def cleanup_stale(self, *, now: datetime | None = None) -> list[dict[str, str]]:
        current = now or datetime.now(timezone.utc)
        threshold = current - timedelta(hours=self.config.stale_worktree_hours)
        removed: list[dict[str, str]] = []
        if not self.config.worktrees_path.exists():
            return removed

        for marker in self.config.worktrees_path.rglob(".*.cwapi-workspace.json"):
            try:
                metadata = json.loads(marker.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError):
                continue
            if not isinstance(metadata, dict):
                continue
            worktree_value = metadata.get("worktree_path")
            if worktree_value:
                worktree = Path(str(worktree_value))
            else:
                marker_name = marker.name
                task_name = marker_name[1:-len(".cwapi-workspace.json")]
                worktree = marker.parent / task_name
            created_raw = str(metadata.get("created_at", ""))
            try:
                created_at = datetime.fromisoformat(created_raw)
            except ValueError:
                created_at = datetime.fromtimestamp(
                    marker.stat().st_mtime,
                    tz=timezone.utc,
                )
            keep_until_raw = metadata.get("keep_until")
            if keep_until_raw:
                try:
                    keep_until = datetime.fromisoformat(str(keep_until_raw))
                except ValueError:
                    keep_until = threshold
                if current < keep_until:
                    continue
            elif created_at > threshold:
                continue
            repository = str(metadata.get("repository", ""))
            if not repository:
                continue
            mirror = self.mirror_path(repository)
            try:
                self.release_path(mirror=mirror, worktree=worktree)
                removed.append(
                    {
                        "repository": repository,
                        "task_id": str(metadata.get("task_id", "")),
                        "worktree_path": str(worktree),
                    }
                )
            except (OSError, RepositoryError, SecurityViolation) as exc:
                removed.append(
                    {
                        "repository": repository,
                        "task_id": str(metadata.get("task_id", "")),
                        "worktree_path": str(worktree),
                        "error": safe_error(exc),
                    }
                )
        return removed
