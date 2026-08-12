from __future__ import annotations

from dataclasses import asdict
from datetime import datetime, timezone
from functools import lru_cache
import json
import os
from pathlib import Path
import platform
import shutil
import sqlite3
import subprocess
import sys
from typing import Any, Mapping

from . import __version__
from .codex_toolhost.runtime_lock import CodexRuntimeLockError, verify_codex_runtime
from .codex_toolhost.shared_client import shared_codex_toolhost_snapshots
from .execution.action_registry import action_catalog
from .git import GitRuntime, GitRuntimeError, resolve_configured_git
from .state.runtime_store import RuntimeStateStore


def _run_text(
    command: list[str],
    *,
    cwd: Path | None = None,
    timeout: int = 5,
    environment: Mapping[str, str] | None = None,
) -> str | None:
    try:
        completed = subprocess.run(
            command,
            cwd=cwd,
            env=dict(environment) if environment is not None else None,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            shell=False,
            timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if completed.returncode != 0:
        return None
    value = (completed.stdout or completed.stderr).strip()
    return value[:2000] if value else None


def _git_snapshot(path: Path, runtime: GitRuntime) -> dict[str, Any]:
    if not path.is_dir():
        return {"available": False, "reason": "path_missing"}
    environment = runtime.environment()
    head = _run_text(
        runtime.command("rev-parse", "HEAD"),
        cwd=path,
        environment=environment,
    )
    if head is None:
        return {"available": False, "reason": "not_git_repository"}
    branch = _run_text(
        runtime.command("branch", "--show-current"),
        cwd=path,
        environment=environment,
    ) or "detached"
    status = _run_text(
        runtime.command("status", "--porcelain=v1", "--untracked-files=no"),
        cwd=path,
        environment=environment,
    )
    changes = [line for line in (status or "").splitlines() if line.strip()]
    return {
        "available": True,
        "head": head,
        "branch": branch,
        "clean": not changes,
        "change_count": len(changes),
    }


def _project_snapshots(service: Any) -> list[dict[str, Any]]:
    store = service.runner.store
    git_runtime = getattr(service.runner, "git_runtime", None)
    if git_runtime is None:
        git_runtime = resolve_configured_git(getattr(service.config, "git", object()))
    workspaces = store.list_workspaces(200) if isinstance(store, RuntimeStateStore) else []
    output: list[dict[str, Any]] = []
    for repository, project in sorted(service.config.projects.settings.items()):
        project_workspaces = [
            dict(item) for item in workspaces if item.get("repository") == repository
        ]
        output.append(
            {
                "repository": repository,
                "name": project.name,
                "path": str(project.path),
                "remote_url": project.remote_url,
                "python_executable": project.python_executable,
                "cargo_executable": project.cargo_executable,
                "default_test_paths": list(project.default_test_paths),
                "allow_dependency_check": bool(project.allow_dependency_check),
                "allowed": repository in service.config.security.allowed_repositories,
                "git": _git_snapshot(Path(project.path), git_runtime),
                "workspaces": project_workspaces[:20],
            }
        )
    return output


def _category(action: str) -> str:
    if action.startswith("codex_"):
        return "Codex Toolhost"
    if action.startswith("cargo_"):
        return "Cargo"
    if action.startswith("pytest"):
        return "Testing"
    if action.startswith("git_"):
        return "Git"
    if action in {"collect_files", "collect_hashes"}:
        return "Evidence"
    if action in {"repository_automation", "local_command", "dsca_training"}:
        return "Automation"
    if action.startswith("python_"):
        return "Python"
    if action == "dry_run":
        return "Protocol"
    return "Runner"


def _capabilities(service: Any) -> list[dict[str, Any]]:
    allowed = set(service.config.security.allowed_actions)
    result: list[dict[str, Any]] = []
    for item in action_catalog():
        name = str(item["name"])
        enabled = name == "dry_run" or name in allowed
        entry = {
            **item,
            "category": _category(name),
            "enabled": enabled,
            "policy": "allowed" if enabled else "blocked",
        }
        if name == "local_command":
            entry["display_name"] = "local_command · 受控 PowerShell"
            entry["guardrails"] = [
                "Action Registry allowlist",
                "configured project root",
                "environment allowlist",
                "high-risk fragment deny rules",
                "protected CWapi paths",
            ]
            entry["unrestricted_shell"] = False
        result.append(entry)
    return result


@lru_cache(maxsize=8)
def _provenance_cached(path: str, mtime_ns: int, size: int) -> dict[str, Any]:
    del mtime_ns, size
    executable = Path(path)
    try:
        provenance = verify_codex_runtime(executable)
    except CodexRuntimeLockError as exc:
        return {"verified": False, "state": "invalid", "error": str(exc)[:500]}
    except OSError as exc:
        return {"verified": False, "state": "unavailable", "error": str(exc)[:500]}
    if provenance is None:
        return {"verified": False, "state": "not_locked"}
    return {
        "verified": True,
        "state": "verified",
        "source_repository": provenance.repository,
        "source_ref": provenance.source_ref,
        "source_commit": provenance.source_commit,
        "runtime_version": provenance.version,
        "archive_sha256": provenance.archive_sha256,
        "executable_sha256": provenance.executable_sha256,
        "lock_path": str(provenance.lock_path),
        "manifest_path": str(provenance.manifest_path),
    }


def _runtime_provenance(executable: Path) -> dict[str, Any]:
    try:
        stat = executable.stat()
    except OSError:
        return {"verified": False, "state": "executable_missing", "path": str(executable)}
    return {
        "path": str(executable),
        **_provenance_cached(str(executable), stat.st_mtime_ns, stat.st_size),
    }


def _toolchain(service: Any) -> dict[str, Any]:
    root = Path(service.config.state.database_path).resolve().parent.parent

    manifest_path = root / "runtime" / "mcp" / "playwright" / "runtime.json"
    manifest: dict[str, Any] = {}
    if manifest_path.is_file():
        try:
            loaded = json.loads(manifest_path.read_text(encoding="utf-8-sig"))
            if isinstance(loaded, dict):
                manifest = loaded
        except (OSError, json.JSONDecodeError):
            manifest = {}

    node_candidate = root / "runtime" / "node" / "node.exe"
    if not node_candidate.is_file() and manifest.get("node_executable"):
        manifest_node = Path(str(manifest["node_executable"]))
        if manifest_node.is_file():
            node_candidate = manifest_node
    node_path = str(node_candidate) if node_candidate.is_file() else shutil.which("node")

    cli_path: Path | None = None
    if manifest.get("cli_relative"):
        cli_path = root / str(manifest["cli_relative"])
    elif manifest.get("cli_path"):
        cli_path = Path(str(manifest["cli_path"]))
    playwright_available = bool(cli_path and cli_path.is_file())

    browser_root = root / "runtime" / "browser"
    browser_candidates = sorted(browser_root.rglob("chrome.exe")) if browser_root.is_dir() else []
    browser_path = str(browser_candidates[0]) if browser_candidates else (
        shutil.which("chromium") or shutil.which("chromium.exe") or shutil.which("chrome") or shutil.which("chrome.exe")
    )

    try:
        git_runtime = getattr(service.runner, "git_runtime", None)
        if git_runtime is None:
            git_runtime = resolve_configured_git(getattr(service.config, "git", object()))
    except (AttributeError, GitRuntimeError):
        git_runtime = None
    cargo_path = shutil.which("cargo")
    return {
        "python": {"available": bool(sys.executable), "path": sys.executable, "version": platform.python_version()},
        "git": {
            "available": git_runtime is not None,
            "path": str(git_runtime.executable) if git_runtime else None,
            "private": bool(git_runtime and git_runtime.private),
            "source": git_runtime.source if git_runtime else None,
            "version": (
                _run_text(
                    git_runtime.command("--version"),
                    environment=git_runtime.environment(),
                )
                if git_runtime
                else None
            ),
        },
        "cargo": {"available": bool(cargo_path), "path": cargo_path, "version": _run_text(["cargo", "--version"]) if cargo_path else None},
        "node": {"available": bool(node_path), "path": node_path, "version": _run_text([str(node_path), "--version"]) if node_path else None},
        "chromium": {"available": bool(browser_path), "path": browser_path},
        "playwright_mcp": {
            "available": playwright_available,
            "path": str(cli_path) if cli_path else None,
            "version": manifest.get("version"),
        },
        "sqlite": {"available": True, "version": sqlite3.sqlite_version},
        "platform": {
            "system": platform.system(),
            "release": platform.release(),
            "machine": platform.machine(),
        },
    }


def _runner_lock_snapshot(service: Any) -> dict[str, Any]:
    lock = service.runner_lock
    candidate = getattr(lock, "path", None) or getattr(lock, "lock_path", None)
    return {
        "path": str(candidate) if candidate else None,
        "process_owned": True,
        "runner_id": service.config.runner.runner_id,
    }


def _codex_snapshot(service: Any) -> dict[str, Any]:
    enabled = bool(service.config.codex_toolhost.enabled)
    snapshots = shared_codex_toolhost_snapshots() if enabled else []
    items = [asdict(item) for item in snapshots]
    return {
        "enabled": enabled,
        "permission_profile": service.config.codex_toolhost.permission_profile,
        "home_path": str(service.config.codex_toolhost.home_path),
        "stderr_log_path": str(service.config.codex_toolhost.stderr_log_path),
        "instances": items,
        "provenance": _runtime_provenance(Path(service.config.codex_toolhost.executable_path)),
    }


def _diagnostics(
    service: Any, projects: list[dict[str, Any]], provenance: dict[str, Any]
) -> list[dict[str, Any]]:
    checks = [
        {
            "name": "State database",
            "ok": Path(service.config.state.database_path).parent.exists(),
            "detail": str(service.config.state.database_path),
        },
        {
            "name": "Runtime logs",
            "ok": Path(service.config.storage.logs_path).exists(),
            "detail": str(service.config.storage.logs_path),
        },
        {
            "name": "Results",
            "ok": Path(service.config.storage.results_path).exists(),
            "detail": str(service.config.storage.results_path),
        },
        {
            "name": "Codex runtime provenance",
            "ok": bool(provenance.get("verified")) or not service.config.codex_toolhost.enabled,
            "detail": str(provenance.get("state") or "unknown"),
        },
    ]
    for project in projects:
        checks.append(
            {
                "name": f"Project {project['repository']}",
                "ok": bool(project.get("git", {}).get("available")),
                "detail": str(project.get("path")),
            }
        )
    return checks


def build_workbench_snapshot(service: Any, *, config_path: Path) -> dict[str, Any]:
    store = service.runner.store
    projects = _project_snapshots(service)
    codex = _codex_snapshot(service)
    transport = dict(vars(service.transport_runtime.snapshot()))
    heartbeat = (
        store.get_heartbeat(service.config.runner.runner_id)
        if isinstance(store, RuntimeStateStore)
        else None
    )
    capabilities = _capabilities(service)
    toolchain = _toolchain(service)
    paths = {
        "config": str(config_path),
        "state_database": str(service.config.state.database_path),
        "logs": str(service.config.storage.logs_path),
        "results": str(service.config.storage.results_path),
        "mirrors": str(service.config.git.mirrors_path),
        "worktrees": str(service.config.git.worktrees_path),
        "drive": str(service.config.storage.drive_sync_path)
        if service.config.storage.drive_sync_path
        else None,
    }
    configuration = {
        "runner": {
            "id": service.config.runner.runner_id,
            "poll_interval_seconds": service.config.runner.poll_interval_seconds,
            "cancel_poll_seconds": service.config.runner.cancel_poll_seconds,
            "max_task_runtime_seconds": service.config.runner.max_task_runtime_seconds,
            "progress_mode": service.config.runner.progress_mode,
        },
        "security": {
            "allowed_repositories": sorted(service.config.security.allowed_repositories),
            "allowed_actions": sorted(service.config.security.allowed_actions),
            "allowed_environment_variables": list(
                service.config.security.allowed_environment_variables
            ),
            "max_step_timeout_seconds": service.config.security.max_step_timeout_seconds,
            "max_task_steps": service.config.security.max_task_steps,
            "max_relative_paths": service.config.security.max_relative_paths,
        },
        "storage": {
            "artifact_max_file_bytes": service.config.storage.artifact_max_file_bytes,
            "artifact_max_total_bytes": service.config.storage.artifact_max_total_bytes,
            "result_retention_days": service.config.storage.result_retention_days,
            "drive_enabled": service.config.storage.drive_sync_path is not None,
        },
        "codex": {
            "enabled": service.config.codex_toolhost.enabled,
            "permission_profile": service.config.codex_toolhost.permission_profile,
        },
    }
    return {
        "schema": "cwapi.workbench.v1",
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "projects": projects,
        "capabilities": capabilities,
        "environment": {
            "paths": paths,
            "tools": toolchain,
            "drive_enabled": service.config.storage.drive_sync_path is not None,
        },
        "configuration": configuration,
        "diagnostics": _diagnostics(service, projects, codex["provenance"]),
        "other": {
            "transport": transport,
            "runner_lock": _runner_lock_snapshot(service),
            "heartbeat": heartbeat,
            "codex": codex,
            "state": {
                "database_path": str(service.config.state.database_path),
                "sqlite_version": sqlite3.sqlite_version,
            },
            "runtime_build": {
                "cwapi_version": __version__,
                "python_version": platform.python_version(),
                "platform": platform.platform(),
                "process_id": os.getpid(),
            },
        },
    }
