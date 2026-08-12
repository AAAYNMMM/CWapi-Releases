from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path
from typing import Mapping

from .codex_toolhost.runtime_lock import verify_codex_runtime
from .config import ConfigError, load_config
from .execution.capability_policy import load_codex_capability_policy
from .git import GitRuntimeError, resolve_configured_git
from .locking import RunnerLock
from .runner import CWapiRunner
from .security import safe_error
from .service import RunnerService
from .state.runtime_store import RuntimeStateStore
from .state.sqlite_store import SQLiteStateStore
from .transports.gmail_drafts import GmailDraftTransport


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="cwapi",
        description="CWapi local work runner",
    )
    parser.add_argument(
        "--config",
        default=None,
        help="配置文件路径，默认 config/cwapi.yaml 或 CWAPI_CONFIG。",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("doctor", help="检查配置、Git、OAuth 和存储目录。")
    subparsers.add_parser("auth-gmail", help="执行本地 Gmail OAuth。")
    subparsers.add_parser("runner-once", help="读取并处理一次 Gmail TASK。")
    runner_start = subparsers.add_parser("runner-start", help="启动常驻 Runner。")
    runner_start.add_argument(
        "--show-output",
        action="store_true",
        help="在控制台跟踪当前步骤追加的 stdout/stderr。",
    )
    subparsers.add_parser("status", help="显示最近任务。")
    subparsers.add_parser("recover", help="恢复 RESULT outbox 和中断任务。")
    subparsers.add_parser("cleanup", help="清理过期 worktree 和结果包。")
    subparsers.add_parser("workspace-list", help="列出受控 worktree。")

    task_show = subparsers.add_parser("task-show", help="显示单个任务详情。")
    task_show.add_argument("task_id")

    task_cancel = subparsers.add_parser(
        "task-cancel",
        help="在本地状态库中请求取消任务。",
    )
    task_cancel.add_argument("task_id")
    task_cancel.add_argument(
        "--reason",
        default="Cancelled by local operator.",
    )
    return parser


def _run_fixed(
    args: list[str],
    *,
    cwd: Path | None = None,
    timeout: int = 15,
    environment: Mapping[str, str] | None = None,
) -> tuple[bool, str]:
    try:
        completed = subprocess.run(
            args,
            cwd=str(cwd) if cwd is not None else None,
            env=dict(environment) if environment is not None else None,
            capture_output=True,
            text=True,
            shell=False,
            timeout=timeout,
            check=False,
        )
    except Exception as exc:
        return False, safe_error(exc)
    detail = (completed.stdout or completed.stderr).strip()
    return completed.returncode == 0, detail


def _writable_directory(path: Path) -> tuple[bool, str]:
    try:
        path.mkdir(parents=True, exist_ok=True)
        probe = path / ".cwapi-write-probe"
        probe.write_text("ok", encoding="utf-8")
        probe.unlink()
        return True, str(path)
    except OSError as exc:
        return False, safe_error(exc)


def _doctor(config_path: str | None) -> int:
    config = load_config(config_path)
    checks: list[tuple[str, bool, str]] = []
    checks.append(("配置文件", True, "已加载"))
    checks.append(
        (
            "Gmail 账号",
            "@" in config.gmail.account
            and "your-main-account" not in config.gmail.account,
            config.gmail.account,
        )
    )
    checks.append(
        (
            "OAuth credentials.json",
            config.gmail.credentials_path.exists(),
            str(config.gmail.credentials_path),
        )
    )
    checks.append(
        (
            "OAuth token.json",
            config.gmail.token_path.exists(),
            str(config.gmail.token_path),
        )
    )

    store_type = RuntimeStateStore if config.runtime.enabled else SQLiteStateStore
    store = store_type(config.state.database_path)
    store.initialize()
    checks.append(
        ("SQLite", config.state.database_path.exists(), str(config.state.database_path))
    )

    try:
        git_runtime = resolve_configured_git(config.git)
    except GitRuntimeError as exc:
        git_runtime = None
        git_ok, git_detail = False, safe_error(exc)
    else:
        git_ok, git_detail = _run_fixed(
            git_runtime.command("--version"),
            environment=git_runtime.environment(),
        )
        git_detail = (
            f"{git_detail} | {git_runtime.executable} | "
            f"private={str(git_runtime.private).lower()}"
        )
    checks.append(("Git", git_ok, git_detail))

    if config.codex_toolhost.enabled:
        codex_path = config.codex_toolhost.executable_path
        project_root = config.codex_toolhost.home_path.parent.parent
        checks.append(
            (
                "CWapi 私有 Codex CLI",
                codex_path.is_absolute() and codex_path.is_file(),
                str(codex_path),
            )
        )
        home_ok, home_detail = _writable_directory(config.codex_toolhost.home_path)
        checks.append(("Codex 私有 CODEX_HOME", home_ok, home_detail))
        log_ok, log_detail = _writable_directory(
            config.codex_toolhost.stderr_log_path.parent
        )
        checks.append(("Codex 日志目录", log_ok, log_detail))
        checks.append(
            (
                "Codex 与用户配置隔离",
                config.codex_toolhost.home_path.resolve()
                != (Path.home() / ".codex").resolve(),
                str(config.codex_toolhost.home_path),
            )
        )

        private_config = config.codex_toolhost.home_path / "config.toml"
        checks.append(
            (
                "Codex 私有配置",
                private_config.is_file(),
                str(private_config),
            )
        )

        capability_policy_path = project_root / "config" / "codex-capabilities.yaml"
        try:
            policy = load_codex_capability_policy(
                capability_policy_path,
                default_permission_profile=config.codex_toolhost.permission_profile,
            )
            policy_detail = (
                f"{capability_policy_path}; profiles={sorted(policy.permission_profiles)}; "
                f"mcp={sorted(policy.mcp_servers)}"
            )
            checks.append(("Codex capability policy", True, policy_detail))
        except (OSError, ValueError) as exc:
            checks.append(
                (
                    "Codex capability policy",
                    False,
                    safe_error(exc),
                )
            )

        try:
            provenance = verify_codex_runtime(codex_path)
            if provenance is None:
                checks.append(
                    (
                        "Codex Fork 运行时锁",
                        False,
                        "未找到 config/codex-runtime.lock.json",
                    )
                )
            else:
                checks.append(
                    (
                        "Codex Fork 运行时锁",
                        True,
                        f"{provenance.repository}@{provenance.source_ref} "
                        f"commit={provenance.source_commit} "
                        f"version={provenance.version}",
                    )
                )
                checks.append(
                    (
                        "Codex executable SHA-256",
                        True,
                        provenance.executable_sha256,
                    )
                )
        except (OSError, ValueError) as exc:
            checks.append(("Codex Fork 运行时锁", False, safe_error(exc)))

        playwright_manifest = project_root / "runtime" / "mcp" / "playwright" / "runtime.json"
        checks.append(
            (
                "Playwright MCP 固定运行时",
                playwright_manifest.is_file(),
                str(playwright_manifest),
            )
        )

    for name, path in (
        ("Mirror 根目录", config.git.mirrors_path),
        ("Worktree 根目录", config.git.worktrees_path),
        ("日志目录", config.storage.logs_path),
        ("结果目录", config.storage.results_path),
    ):
        ok, detail = _writable_directory(path)
        checks.append((name, ok, detail))

    if config.storage.drive_sync_path is not None:
        drive_ok, drive_detail = _writable_directory(config.storage.drive_sync_path)
        checks.append(("Google Drive 同步目录", drive_ok, drive_detail))

    for repo_name, project in config.projects.settings.items():
        checks.append(
            (
                f"项目 [{repo_name}]",
                project.path.exists() and (project.path / ".git").exists(),
                str(project.path),
            )
        )
        if git_runtime is None:
            ok, origin = False, "Git runtime unavailable"
        else:
            ok, origin = _run_fixed(
                git_runtime.command("remote", "get-url", "origin"),
                cwd=project.path,
                environment=git_runtime.environment(),
            )
        normalized = origin.lower().removesuffix(".git").replace(
            "git@github.com:",
            "https://github.com/",
        )
        expected = f"https://github.com/{repo_name}".lower()
        checks.append(("  remote origin", ok and normalized == expected, origin))
        checks.append(
            (
                "  managed remote_url",
                project.remote_url.lower().removesuffix(".git") == expected,
                project.remote_url,
            )
        )

    failed = False
    for name, ok, detail in checks:
        marker = "OK" if ok else "FAIL"
        print(f"[{marker}] {name}: {detail}")
        failed = failed or not ok
    return 1 if failed else 0


def _auth_gmail(config_path: str | None) -> int:
    config = load_config(config_path)
    transport = GmailDraftTransport(
        account=config.gmail.account,
        credentials_path=config.gmail.credentials_path,
        token_path=config.gmail.token_path,
    )
    transport.authorize()
    print(f"Gmail 本地 OAuth 已完成：{config.gmail.token_path}")
    return 0


def _runner_once(config_path: str | None) -> int:
    config = load_config(config_path)
    with RunnerLock(config.state.database_path.with_suffix(".runner.lock")):
        result = CWapiRunner(config).run_once()
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


def _runner_start(config_path: str | None, *, show_output: bool = False) -> int:
    config = load_config(config_path)
    RunnerService(config, show_output=show_output).run_forever()
    return 0


def _store(config_path: str | None) -> RuntimeStateStore | SQLiteStateStore:
    config = load_config(config_path)
    store_type = RuntimeStateStore if config.runtime.enabled else SQLiteStateStore
    store = store_type(config.state.database_path)
    store.initialize()
    return store


def _status(config_path: str | None) -> int:
    store = _store(config_path)
    print(json.dumps(store.list_tasks(), ensure_ascii=False, indent=2))
    return 0


def _task_show(config_path: str | None, task_id: str) -> int:
    store = _store(config_path)
    task = store.get_task(task_id)
    if task is None:
        print(f"找不到任务：{task_id}", file=sys.stderr)
        return 1
    payload = {
        "task": task,
        "steps": store.get_task_steps(task_id),
        "outbox": store.get_outbox_result(task_id),
    }
    if isinstance(store, RuntimeStateStore):
        payload["artifact"] = store.get_artifact(task_id)
        payload["workspaces"] = [
            row for row in store.list_workspaces() if row["task_id"] == task_id
        ]
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


def _task_cancel(
    config_path: str | None,
    task_id: str,
    reason: str,
) -> int:
    store = _store(config_path)
    if not isinstance(store, RuntimeStateStore):
        raise RuntimeError("task-cancel 需要 runtime.enabled=true。")
    if store.get_task(task_id) is None:
        raise RuntimeError(f"找不到任务：{task_id}")
    store.request_cancel_local(task_id=task_id, reason=reason)
    print(f"已请求取消任务：{task_id}")
    return 0


def _recover(config_path: str | None) -> int:
    config = load_config(config_path)
    with RunnerLock(config.state.database_path.with_suffix(".runner.lock")):
        result = CWapiRunner(config).recover()
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


def _cleanup(config_path: str | None) -> int:
    config = load_config(config_path)
    with RunnerLock(config.state.database_path.with_suffix(".runner.lock")):
        result = CWapiRunner(config).cleanup()
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


def _workspace_list(config_path: str | None) -> int:
    store = _store(config_path)
    if not isinstance(store, RuntimeStateStore):
        raise RuntimeError("workspace-list 需要 runtime.enabled=true。")
    print(json.dumps(store.list_workspaces(), ensure_ascii=False, indent=2))
    return 0


def main() -> None:
    args = _parser().parse_args()
    try:
        if args.command == "doctor":
            code = _doctor(args.config)
        elif args.command == "auth-gmail":
            code = _auth_gmail(args.config)
        elif args.command == "runner-once":
            code = _runner_once(args.config)
        elif args.command == "runner-start":
            code = _runner_start(args.config, show_output=args.show_output)
        elif args.command == "status":
            code = _status(args.config)
        elif args.command == "task-show":
            code = _task_show(args.config, args.task_id)
        elif args.command == "task-cancel":
            code = _task_cancel(args.config, args.task_id, args.reason)
        elif args.command == "recover":
            code = _recover(args.config)
        elif args.command == "cleanup":
            code = _cleanup(args.config)
        elif args.command == "workspace-list":
            code = _workspace_list(args.config)
        else:
            raise RuntimeError(f"未知命令：{args.command}")
    except (ConfigError, FileNotFoundError, RuntimeError, ValueError) as exc:
        print(f"错误：{safe_error(exc)}", file=sys.stderr)
        code = 1
    raise SystemExit(code)


if __name__ == "__main__":
    main()
