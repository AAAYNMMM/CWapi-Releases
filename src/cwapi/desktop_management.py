from __future__ import annotations

from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
import tomllib
from typing import Any

import yaml

from .config import AppConfig, load_config
from .execution.capability_policy import load_codex_capability_policy
from .portable_release import portable_diagnostics, verify_release_manifest
from .security import ensure_within, safe_error


class DesktopManagementError(RuntimeError):
    pass


_EDITABLE: dict[str, tuple[str, ...] | str] = {
    "runner": (
        "channel_ids",
        "max_tasks_per_poll",
        "poll_interval_seconds",
        "cancel_poll_seconds",
        "cleanup_interval_seconds",
        "max_task_runtime_seconds",
        "progress_mode",
    ),
    "gmail": ("account", "max_results"),
    "projects": "projects",
    "security": (
        "allowed_repositories",
        "allowed_actions",
        "allowed_environment_variables",
        "max_step_timeout_seconds",
        "max_task_steps",
        "max_relative_paths",
    ),
    "git": (
        "enabled",
        "fetch_timeout_seconds",
        "cleanup_on_success",
        "keep_failed_worktrees_hours",
        "stale_worktree_hours",
    ),
    "storage": (
        "drive_sync_path",
        "drive_subdirectory",
        "artifact_max_file_bytes",
        "artifact_max_total_bytes",
        "result_retention_days",
        "create_zip_bundle",
    ),
    "runtime": (
        "enabled",
        "publish_progress",
        "collect_artifacts",
        "enable_cancel_drafts",
    ),
    "codex_toolhost": ("enabled", "permission_profile", "startup_timeout_seconds"),
}
_PROJECT_EDITABLE = (
    "name",
    "path",
    "remote_url",
    "python_executable",
    "cargo_executable",
    "default_test_paths",
    "allow_dependency_check",
)


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _sha(path: Path) -> str:
    if not path.is_file():
        return "missing"
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _read_yaml(path: Path) -> dict[str, Any]:
    try:
        value = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as exc:
        raise DesktopManagementError(f"读取 YAML 失败：{safe_error(exc)}") from exc
    if not isinstance(value, dict):
        raise DesktopManagementError(f"YAML 顶层必须是对象：{path.name}")
    return value


def _atomic_yaml(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    text = yaml.safe_dump(value, allow_unicode=True, sort_keys=False)
    temporary = path.with_name(f".{path.name}.tmp")
    temporary.write_text(text, encoding="utf-8", newline="\n")
    temporary.replace(path)


def _editable_config(raw: dict[str, Any]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for section, fields in _EDITABLE.items():
        source = raw.get(section, {})
        if not isinstance(source, dict):
            source = {}
        if fields == "projects":
            projects: dict[str, Any] = {}
            for repository, item in source.items():
                if not isinstance(item, dict):
                    continue
                projects[str(repository)] = {
                    key: deepcopy(item[key]) for key in _PROJECT_EDITABLE if key in item
                }
            result[section] = projects
        else:
            result[section] = {key: deepcopy(source[key]) for key in fields if key in source}
    return result


def _merge_config(current: dict[str, Any], editable: dict[str, Any]) -> dict[str, Any]:
    unknown_sections = set(editable) - set(_EDITABLE)
    if unknown_sections:
        raise DesktopManagementError(f"包含不可编辑配置区：{sorted(unknown_sections)}")
    merged = deepcopy(current)
    for section, fields in _EDITABLE.items():
        if section not in editable:
            continue
        proposed = editable[section]
        if not isinstance(proposed, dict):
            raise DesktopManagementError(f"{section} 必须是对象。")
        if fields == "projects":
            rebuilt: dict[str, Any] = {}
            current_projects = current.get("projects", {})
            for repository, item in proposed.items():
                if not isinstance(item, dict):
                    raise DesktopManagementError(f"projects.{repository} 必须是对象。")
                base = (
                    deepcopy(current_projects.get(repository, {}))
                    if isinstance(current_projects, dict)
                    else {}
                )
                unknown = set(item) - set(_PROJECT_EDITABLE)
                if unknown:
                    raise DesktopManagementError(
                        f"projects.{repository} 包含不可编辑字段：{sorted(unknown)}"
                    )
                for key, value in item.items():
                    base[key] = deepcopy(value)
                rebuilt[str(repository)] = base
            merged[section] = rebuilt
            continue
        unknown = set(proposed) - set(fields)
        if unknown:
            raise DesktopManagementError(f"{section} 包含不可编辑字段：{sorted(unknown)}")
        target = deepcopy(merged.get(section, {}))
        if not isinstance(target, dict):
            target = {}
        for key, value in proposed.items():
            target[key] = deepcopy(value)
        merged[section] = target
    return merged


def _policy_path(config_path: Path) -> Path:
    return config_path.parent / "codex-capabilities.yaml"


def _pending_path(path: Path) -> Path:
    return path.with_name(f".{path.name}.pending")


def has_pending_settings(config_path: Path) -> bool:
    config_path = config_path.resolve()
    return (
        _pending_path(config_path).is_file() or _pending_path(_policy_path(config_path)).is_file()
    )


def _active_task_id(service: Any) -> str | None:
    store = service.runner.store
    try:
        tasks = store.list_tasks(200)
    except Exception:
        return None
    for item in tasks:
        if str(item.get("execution_status") or "") in {"claimed", "running"}:
            return str(item.get("task_id") or "") or None
    return None


def _diff(before: Any, after: Any, prefix: str = "") -> list[dict[str, Any]]:
    if before == after:
        return []
    if isinstance(before, dict) and isinstance(after, dict):
        rows: list[dict[str, Any]] = []
        for key in sorted(set(before) | set(after)):
            child = f"{prefix}.{key}" if prefix else str(key)
            rows.extend(_diff(before.get(key), after.get(key), child))
        return rows
    return [{"path": prefix, "before": before, "after": after}]


def _validate_config_value(config_path: Path, value: dict[str, Any]) -> AppConfig:
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            suffix=".yaml",
            prefix=".cwapi-validate-",
            dir=config_path.parent,
            delete=False,
        ) as handle:
            yaml.safe_dump(value, handle, allow_unicode=True, sort_keys=False)
            temporary = Path(handle.name)
        return load_config(temporary)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _validate_policy_value(path: Path, value: dict[str, Any], profile: str) -> None:
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            suffix=".yaml",
            prefix=".policy-validate-",
            dir=path.parent,
            delete=False,
        ) as handle:
            yaml.safe_dump(value, handle, allow_unicode=True, sort_keys=False)
            temporary = Path(handle.name)
        load_codex_capability_policy(
            temporary,
            default_permission_profile=profile,
        )
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _network_profiles(config: AppConfig) -> dict[str, Any]:
    path = config.codex_toolhost.home_path / "config.toml"
    if not path.is_file():
        return {"source": "private CODEX_HOME/config.toml", "available": False, "profiles": {}}
    try:
        raw = tomllib.loads(path.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError):
        return {"source": "private CODEX_HOME/config.toml", "available": False, "profiles": {}}
    profiles: dict[str, Any] = {}
    permissions = raw.get("permissions", {})
    if isinstance(permissions, dict):
        for name, item in permissions.items():
            if not isinstance(item, dict):
                continue
            network = item.get("network", {})
            if not isinstance(network, dict):
                network = {}
            domains = network.get("domains", {})
            allowed_domains = (
                sorted(
                    str(domain)
                    for domain, decision in domains.items()
                    if str(decision).casefold() == "allow"
                )
                if isinstance(domains, dict)
                else []
            )
            profiles[str(name)] = {
                "network_enabled": bool(network.get("enabled", False)),
                "network_mode": network.get("mode"),
                "allow_local_binding": bool(network.get("allow_local_binding", False)),
                "allowed_domains": allowed_domains,
            }
    return {"source": "private CODEX_HOME/config.toml", "available": True, "profiles": profiles}


def settings_snapshot(service: Any, *, config_path: Path) -> dict[str, Any]:
    config_path = config_path.resolve()
    policy_path = _policy_path(config_path)
    raw = _read_yaml(config_path)
    policy = _read_yaml(policy_path)
    active = _active_task_id(service)
    pending_on_disk = has_pending_settings(config_path)
    read_only = {
        "config_path": str(config_path),
        "data_root": str(service.config.state.database_path.parent.parent),
        "credentials_path": str(service.config.gmail.credentials_path),
        "credentials_present": service.config.gmail.credentials_path.is_file(),
        "token_present": service.config.gmail.token_path.is_file(),
        "state_database": str(service.config.state.database_path),
        "logs_path": str(service.config.storage.logs_path),
        "results_path": str(service.config.storage.results_path),
        "mirror_path": str(service.config.git.mirrors_path),
        "worktrees_path": str(service.config.git.worktrees_path),
        "codex_executable": str(service.config.codex_toolhost.executable_path),
        "codex_home": str(service.config.codex_toolhost.home_path),
        "codex_stderr_log": str(service.config.codex_toolhost.stderr_log_path),
    }
    return {
        "schema": "cwapi.desktop.settings.v1",
        "generated_at": _utc_now(),
        "active_task_id": active,
        "accepting_new_tasks": not bool(
            getattr(service, "configuration_stale", False)
            or getattr(service, "settings_pending", False)
            or pending_on_disk
        ),
        "restart_required": bool(getattr(service, "configuration_stale", False)),
        "pending": {
            "config": _pending_path(config_path).is_file(),
            "capability_policy": _pending_path(policy_path).is_file(),
            "blocks_new_tasks": bool(
                getattr(service, "settings_pending", False) or pending_on_disk
            ),
        },
        "config": {
            "revision": _sha(config_path),
            "editable": _editable_config(raw),
            "read_only": read_only,
        },
        "capability_policy": {
            "path": str(policy_path),
            "revision": _sha(policy_path),
            "editable": policy,
            "effective_network": _network_profiles(service.config),
        },
    }


def validate_settings(
    service: Any,
    *,
    config_path: Path,
    kind: str,
    editable: dict[str, Any],
) -> dict[str, Any]:
    config_path = config_path.resolve()
    if kind == "config":
        current = _read_yaml(config_path)
        candidate = _merge_config(current, editable)
        _validate_config_value(config_path, candidate)
        return {
            "valid": True,
            "kind": kind,
            "diff": _diff(_editable_config(current), _editable_config(candidate)),
        }
    if kind == "capability_policy":
        path = _policy_path(config_path)
        if not isinstance(editable, dict):
            raise DesktopManagementError("capability policy 必须是对象。")
        _validate_policy_value(
            path,
            editable,
            service.config.codex_toolhost.permission_profile,
        )
        return {"valid": True, "kind": kind, "diff": _diff(_read_yaml(path), editable)}
    raise DesktopManagementError(f"未知设置类型：{kind}")


def save_settings(
    service: Any,
    *,
    config_path: Path,
    kind: str,
    revision: str,
    editable: dict[str, Any],
) -> dict[str, Any]:
    config_path = config_path.resolve()
    active = _active_task_id(service)
    if kind == "config":
        path = config_path
        if revision != _sha(path):
            raise DesktopManagementError("配置已被其他进程修改，请刷新后重试。")
        current = _read_yaml(path)
        candidate = _merge_config(current, editable)
        _validate_config_value(path, candidate)
    elif kind == "capability_policy":
        path = _policy_path(config_path)
        if revision != _sha(path):
            raise DesktopManagementError("Capability policy 已变化，请刷新后重试。")
        candidate = deepcopy(editable)
        _validate_policy_value(
            path,
            candidate,
            service.config.codex_toolhost.permission_profile,
        )
    else:
        raise DesktopManagementError(f"未知设置类型：{kind}")

    destination = _pending_path(path) if active else path
    _atomic_yaml(destination, candidate)
    if active:
        service.settings_pending = True
    elif kind == "config":
        service.configuration_stale = True
    return {
        "saved": True,
        "kind": kind,
        "deferred": bool(active),
        "active_task_id": active,
        "restart_required": bool(getattr(service, "configuration_stale", False)),
        "new_revision": _sha(destination),
        "destination": "pending" if active else "active",
    }


def apply_pending_settings(service: Any, *, config_path: Path) -> dict[str, Any]:
    active = _active_task_id(service)
    if active:
        raise DesktopManagementError(f"TASK {active} 仍在运行，不能应用 pending 设置。")
    config_path = config_path.resolve()
    policy_path = _policy_path(config_path)
    applied: list[str] = []
    pending_config = _pending_path(config_path)
    if pending_config.is_file():
        value = _read_yaml(pending_config)
        _validate_config_value(config_path, value)
        _atomic_yaml(config_path, value)
        pending_config.unlink(missing_ok=True)
        service.configuration_stale = True
        applied.append("config")
    pending_policy = _pending_path(policy_path)
    if pending_policy.is_file():
        value = _read_yaml(pending_policy)
        _validate_policy_value(policy_path, value, service.config.codex_toolhost.permission_profile)
        _atomic_yaml(policy_path, value)
        pending_policy.unlink(missing_ok=True)
        applied.append("capability_policy")
    service.settings_pending = False
    return {
        "applied": applied,
        "restart_required": bool(getattr(service, "configuration_stale", False)),
    }


def gmail_status(service: Any) -> dict[str, Any]:
    snapshot = service.transport_runtime.snapshot()
    token_present = service.config.gmail.token_path.is_file()
    if not token_present or snapshot.authorization_required:
        mode = "manual_reauthorization_required"
    elif snapshot.state in {"backoff", "retrying", "degraded"}:
        mode = "automatic_refresh_or_retry"
    else:
        mode = "authorized"
    return {
        "schema": "cwapi.gmail.auth-status.v1",
        "account": service.config.gmail.account,
        "credentials_present": service.config.gmail.credentials_path.is_file(),
        "token_present": token_present,
        "transport_state": snapshot.state,
        "authorization_required": bool(snapshot.authorization_required or not token_present),
        "recovery_mode": mode,
    }


def remove_gmail_authorization(service: Any, *, config_path: Path) -> dict[str, Any]:
    active = _active_task_id(service)
    if active:
        raise DesktopManagementError(f"TASK {active} 正在运行，不能移除 Gmail 授权。")
    data_root = config_path.resolve().parent.parent
    token = service.config.gmail.token_path.resolve()
    try:
        token.relative_to(data_root)
    except ValueError as exc:
        raise DesktopManagementError("拒绝删除 CWapi data_root 之外的 token。") from exc
    existed = token.is_file()
    token.unlink(missing_ok=True)
    service.request_stop()
    return {"removed": existed, "authorization_required": True, "restart_required": True}


def _sqlite_check(path: Path) -> str:
    path = path.resolve()
    if not path.is_file():
        raise DesktopManagementError(f"SQLite 数据库不存在：{path}")
    connection = sqlite3.connect(path)
    try:
        row = connection.execute("PRAGMA quick_check").fetchone()
        return str(row[0] if row else "no result")
    finally:
        connection.close()


def doctor_snapshot(service: Any, *, config_path: Path) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []

    def add(name: str, ok: bool, detail: str, category: str) -> None:
        checks.append({"name": name, "ok": bool(ok), "detail": detail[:600], "category": category})

    try:
        load_config(config_path)
        add("cwapi.yaml", True, "结构与路径验证通过", "config")
    except Exception as exc:
        add("cwapi.yaml", False, safe_error(exc), "config")
    try:
        load_codex_capability_policy(
            _policy_path(config_path),
            default_permission_profile=service.config.codex_toolhost.permission_profile,
        )
        add("Codex capability policy", True, "policy schema/权限/limits 验证通过", "policy")
    except Exception as exc:
        add("Codex capability policy", False, safe_error(exc), "policy")

    auth = gmail_status(service)
    transport = service.transport_runtime.snapshot()
    add(
        "Go Transport protocol",
        bool(transport.url and transport.version and transport.authentication_enabled),
        f"state={transport.state} / version={transport.version} / authenticated={transport.authentication_enabled}",
        "transport",
    )
    heartbeat = service.runner.store.get_heartbeat(service.config.runner.runner_id)
    add(
        "Runner heartbeat",
        isinstance(heartbeat, dict) and bool(heartbeat.get("status")),
        str((heartbeat or {}).get("status") or "missing"),
        "runner",
    )
    add(
        "Gmail authorization",
        not auth["authorization_required"],
        f"{auth['recovery_mode']} / transport={auth['transport_state']}",
        "gmail",
    )
    add(
        "Runner lock",
        Path(getattr(service.runner_lock, "path", "")).exists(),
        str(getattr(service.runner_lock, "path", "")),
        "runner",
    )
    try:
        sqlite_result = _sqlite_check(Path(service.config.state.database_path))
        add("SQLite quick_check", sqlite_result.casefold() == "ok", sqlite_result, "state")
    except Exception as exc:
        add("SQLite quick_check", False, safe_error(exc), "state")

    for name, path in (
        ("Runtime logs", Path(service.config.storage.logs_path)),
        ("Results", Path(service.config.storage.results_path)),
    ):
        add(name, path.exists() and path.is_dir(), str(path), "storage")
    if service.config.storage.drive_sync_path is not None:
        drive = Path(service.config.storage.drive_sync_path)
        add("Google Drive sync", drive.exists() and drive.is_dir(), str(drive), "storage")
    for repository, project in service.config.projects.settings.items():
        git_head = project.path / ".git"
        add(
            f"Project {repository}",
            project.path.is_dir() and git_head.exists(),
            str(project.path),
            "git",
        )
    codex = Path(service.config.codex_toolhost.executable_path)
    add(
        "Codex runtime",
        (not service.config.codex_toolhost.enabled) or codex.is_file(),
        str(codex),
        "codex",
    )
    portable = portable_diagnostics(config_path)
    if portable.get("portable"):
        add(
            "Portable release",
            bool(portable.get("ok")),
            str(portable.get("detail") or ""),
            "runtime",
        )
    pending = (
        _pending_path(config_path).is_file() or _pending_path(_policy_path(config_path)).is_file()
    )
    add(
        "Pending settings",
        not pending,
        "等待空闲后应用" if pending else "无 pending 设置",
        "config",
    )
    return {
        "schema": "cwapi.doctor.v1",
        "generated_at": _utc_now(),
        "overall": "pass" if all(item["ok"] for item in checks) else "attention",
        "authorization": auth,
        "checks": checks,
    }


def _verify_manifest(service: Any, task_id: str) -> dict[str, Any]:
    store = service.runner.store
    artifact = store.get_artifact(task_id) if hasattr(store, "get_artifact") else None
    if not isinstance(artifact, dict) or not artifact.get("local_path"):
        raise DesktopManagementError(f"TASK {task_id} 没有 artifact。")
    root = Path(str(artifact["local_path"])).resolve()
    results_root = Path(service.config.storage.results_path).resolve()
    try:
        root.relative_to(results_root)
    except ValueError as exc:
        raise DesktopManagementError("artifact 不在 CWapi results 根目录。") from exc
    manifest_path = root / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    failures: list[str] = []
    for item in manifest.get("files", []):
        relative = str(item.get("path") or "")
        target = ensure_within(root, root / relative)
        if not target.is_file():
            failures.append(f"missing:{relative}")
            continue
        digest = hashlib.sha256(target.read_bytes()).hexdigest()
        if digest != str(item.get("sha256") or ""):
            failures.append(f"sha256:{relative}")
    return {"task_id": task_id, "verified": not failures, "failures": failures}


def run_maintenance(
    service: Any,
    *,
    config_path: Path,
    action: str,
    task_id: str | None = None,
) -> dict[str, Any]:
    active = _active_task_id(service)
    destructive = {
        "cleanup",
        "recover",
        "retry_results",
        "refresh_mirrors",
        "apply_pending_settings",
    }
    if active and action in destructive:
        raise DesktopManagementError(f"TASK {active} 正在运行，维护操作 {action} 已拒绝。")
    if action == "cleanup":
        return {"action": action, "result": service.runner.cleanup()}
    if action == "recover":
        return {"action": action, "result": service.runner.recover()}
    if action == "retry_results":
        return {"action": action, "result": service.runner._retry_pending_results()}
    if action == "refresh_mirrors":
        advanced = service.runner.advanced
        if advanced is None:
            raise DesktopManagementError("Git runtime 未启用。")
        mirrors = []
        for repository, project in service.config.projects.settings.items():
            path = advanced.repositories.ensure_mirror(project)
            mirrors.append({"repository": repository, "path": str(path)})
        return {"action": action, "result": {"mirrors": mirrors}}
    if action == "sqlite_check":
        return {
            "action": action,
            "result": {"quick_check": _sqlite_check(Path(service.config.state.database_path))},
        }
    if action == "verify_manifest":
        if not task_id:
            raise DesktopManagementError("verify_manifest 需要 task_id。")
        return {"action": action, "result": _verify_manifest(service, task_id)}
    if action == "apply_pending_settings":
        return {
            "action": action,
            "result": apply_pending_settings(service, config_path=config_path),
        }
    if action == "verify_portable_release":
        return {
            "action": action,
            "result": verify_release_manifest(config_path, verify_hashes=True),
        }
    raise DesktopManagementError(f"未知维护操作：{action}")
