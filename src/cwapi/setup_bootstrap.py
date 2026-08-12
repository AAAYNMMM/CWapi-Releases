from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import tempfile
from typing import Any

import yaml

from .config import load_config
from .execution.action_registry import KNOWN_ACTIONS
from .paths import PortablePaths
from .security import safe_error
from .transports.go_runtime import GoTransportRuntime


class SetupBootstrapError(RuntimeError):
    pass


def _validate_installed_credentials(path: Path) -> None:
    if not path.is_file():
        raise SetupBootstrapError(f"credentials.json 不存在：{path}")
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise SetupBootstrapError("credentials.json 不是有效 JSON。") from exc
    if not isinstance(payload, dict):
        raise SetupBootstrapError("credentials.json 顶层必须是对象。")
    installed = payload.get("installed")
    if not isinstance(installed, dict):
        raise SetupBootstrapError(
            "CWapi 需要 Google Desktop/Installed App 类型的 credentials.json。"
        )
    for key in ("client_id", "client_secret", "auth_uri", "token_uri"):
        if not str(installed.get(key) or "").strip():
            raise SetupBootstrapError(f"credentials.json 缺少 installed.{key}。")


def _atomic_copy(source: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if source.resolve() == destination.resolve():
        return
    with tempfile.NamedTemporaryFile(
        mode="wb",
        dir=destination.parent,
        prefix=destination.name + ".",
        suffix=".tmp",
        delete=False,
    ) as handle:
        temporary = Path(handle.name)
        with source.open("rb") as input_file:
            shutil.copyfileobj(input_file, handle)
        handle.flush()
        os.fsync(handle.fileno())
    try:
        temporary.replace(destination)
    except Exception:
        temporary.unlink(missing_ok=True)
        raise


def _atomic_yaml(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    text = yaml.safe_dump(
        payload,
        allow_unicode=True,
        sort_keys=False,
        default_flow_style=False,
    )
    with tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        newline="\n",
        dir=path.parent,
        prefix=path.name + ".",
        suffix=".tmp",
        delete=False,
    ) as handle:
        temporary = Path(handle.name)
        handle.write(text)
        handle.flush()
        os.fsync(handle.fileno())
    try:
        temporary.replace(path)
    except Exception:
        temporary.unlink(missing_ok=True)
        raise


def _default_config(account: str) -> dict[str, Any]:
    return {
        "runner": {
            "id": "cwapi-win-01",
            "channel_ids": ["default"],
            "max_tasks_per_poll": 20,
            "poll_interval_seconds": 30,
            "cancel_poll_seconds": 5,
            "cleanup_interval_seconds": 900,
            "max_task_runtime_seconds": 86400,
            "progress_mode": "step",
        },
        "gmail": {
            "account": account,
            "credentials_path": "../secrets/credentials.json",
            "token_path": "../secrets/token.json",
            "task_query": 'in:drafts subject:"[CWapi/1][TASK][PENDING]"',
            "cancel_query": 'in:drafts subject:"[CWapi/1][CANCEL][REQUESTED]"',
            "max_results": 100,
        },
        "state": {"database_path": "../state/cwapi.db"},
        "runtime": {
            "enabled": True,
            "publish_progress": True,
            "collect_artifacts": True,
            "enable_cancel_drafts": True,
        },
        "codex_toolhost": {
            "enabled": True,
            "executable_path": "../runtime/codex/current/bin/codex.exe",
            "home_path": "../state/codex-home",
            "stderr_log_path": "../logs/codex-app-server.log",
            "permission_profile": ":workspace",
            "startup_timeout_seconds": 30,
        },
        "git": {
            "enabled": True,
            "mirrors_path": "../repos",
            "worktrees_path": "../worktrees",
            "fetch_timeout_seconds": 300,
            "cleanup_on_success": True,
            "keep_failed_worktrees_hours": 24,
            "stale_worktree_hours": 72,
        },
        "storage": {
            "logs_path": "../logs",
            "results_path": "../results",
            "drive_sync_path": None,
            "drive_subdirectory": "CWapi",
            "artifact_max_file_bytes": 67108864,
            "artifact_max_total_bytes": 536870912,
            "result_retention_days": 30,
            "create_zip_bundle": True,
        },
        "projects": {},
        "security": {
            "allowed_repositories": [],
            "allowed_actions": sorted(KNOWN_ACTIONS | {"dry_run"}),
            "max_step_timeout_seconds": 86400,
            "max_task_steps": 50,
            "max_relative_paths": 100,
        },
    }


def bootstrap_first_run(
    *,
    data_root: Path,
    credentials_source: Path,
    transport_executable: Path | None = None,
) -> dict[str, Any]:
    data_root = data_root.expanduser().resolve()
    paths = PortablePaths._from_roots(data_root.parent, data_root)
    for directory in (
        paths.config_root,
        paths.runtime_root,
        paths.state_root,
        paths.log_root,
        paths.result_root,
        paths.repo_root,
        paths.worktree_root,
        data_root / "secrets",
        data_root / "cache",
        data_root / "temp",
    ):
        directory.mkdir(parents=True, exist_ok=True)

    source = credentials_source.expanduser().resolve()
    _validate_installed_credentials(source)
    credentials_path = data_root / "secrets" / "credentials.json"
    token_path = data_root / "secrets" / "token.json"
    _atomic_copy(source, credentials_path)

    runtime = GoTransportRuntime(
        account="cwapi-bootstrap@invalid.local",
        credentials_path=credentials_path,
        token_path=token_path,
        events_path=data_root / "logs" / "runtime" / "bootstrap-go-transport.ndjson",
        executable_path=(
            transport_executable.expanduser().resolve()
            if transport_executable is not None
            else None
        ),
    )
    try:
        client = runtime.start()
        client.authorize()
        profile = client.profile()
    finally:
        runtime.close()

    account = str(profile.get("email_address") or "").strip()
    if not account or "@" not in account:
        raise SetupBootstrapError("Google OAuth 已完成，但无法识别 Gmail 账号。")
    if not token_path.is_file():
        raise SetupBootstrapError("Google OAuth 未生成 token.json。")

    config_path = paths.config_root / "cwapi.yaml"
    _atomic_yaml(config_path, _default_config(account))
    load_config(config_path)

    return {
        "schema": "cwapi.setup.completed.v1",
        "account": account,
        "config_path": str(config_path),
        "credentials_present": credentials_path.is_file(),
        "token_present": token_path.is_file(),
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="python -m cwapi.setup_bootstrap")
    parser.add_argument("--data-root", required=True)
    parser.add_argument("--credentials-source", required=True)
    parser.add_argument("--transport-executable")
    return parser


def main() -> None:
    args = _parser().parse_args()
    try:
        payload = bootstrap_first_run(
            data_root=Path(args.data_root),
            credentials_source=Path(args.credentials_source),
            transport_executable=(
                Path(args.transport_executable)
                if args.transport_executable
                else None
            ),
        )
        print(json.dumps(payload, ensure_ascii=False), flush=True)
        code = 0
    except (OSError, SetupBootstrapError, RuntimeError, ValueError) as exc:
        print(
            json.dumps(
                {
                    "schema": "cwapi.setup.failed.v1",
                    "error": safe_error(exc, limit=500),
                },
                ensure_ascii=False,
            ),
            flush=True,
        )
        code = 1
    raise SystemExit(code)


if __name__ == "__main__":
    main()
