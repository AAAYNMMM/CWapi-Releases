from __future__ import annotations

import os
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml

from .paths import resolve_config_path


class ConfigError(RuntimeError):
    pass


@dataclass(frozen=True)
class RunnerConfig:
    runner_id: str
    channel_ids: tuple[str, ...]
    max_tasks_per_poll: int
    poll_interval_seconds: int = 30
    cancel_poll_seconds: int = 5
    cleanup_interval_seconds: int = 900
    max_task_runtime_seconds: int = 21600
    progress_mode: str = "step"


@dataclass(frozen=True)
class GmailConfig:
    account: str
    credentials_path: Path
    token_path: Path
    task_query: str
    max_results: int
    cancel_query: str = 'in:drafts subject:"[CWapi/1][CANCEL][REQUESTED]"'


@dataclass(frozen=True)
class StateConfig:
    database_path: Path


@dataclass(frozen=True)
class ProjectConfig:
    name: str
    path: Path
    remote_url: str
    repository: str | None = None
    python_executable: str = sys.executable
    cargo_executable: str = "cargo"
    default_test_paths: tuple[str, ...] = ("tests",)
    allow_dependency_check: bool = True

    @property
    def repository_id(self) -> str:
        return self.repository or self.name


@dataclass(frozen=True)
class ProjectsConfig:
    mapping: dict[str, Path]
    settings: dict[str, ProjectConfig] = field(default_factory=dict)

    def get(self, repository: str) -> ProjectConfig:
        configured = self.settings.get(repository)
        if configured is not None:
            return configured
        try:
            path = self.mapping[repository]
        except KeyError as exc:
            raise ConfigError(f"未知项目：{repository}") from exc
        return ProjectConfig(
            name=repository,
            path=path,
            remote_url=f"https://github.com/{repository}.git",
            repository=repository,
        )


@dataclass(frozen=True)
class SecurityConfig:
    allowed_repositories: frozenset[str]
    allowed_actions: frozenset[str]
    max_step_timeout_seconds: int = 3600
    max_task_steps: int = 50
    max_relative_paths: int = 100
    allowed_environment_variables: tuple[str, ...] = (
        "PATH",
        "PATHEXT",
        "SYSTEMROOT",
        "WINDIR",
        "COMSPEC",
        "TEMP",
        "TMP",
        "USERNAME",
        "USERPROFILE",
        "HOMEDRIVE",
        "HOMEPATH",
        "LOCALAPPDATA",
        "APPDATA",
        "PROGRAMFILES",
        "PROGRAMFILES(X86)",
        "NUMBER_OF_PROCESSORS",
        "PROCESSOR_ARCHITECTURE",
    )


@dataclass(frozen=True)
class GitConfig:
    enabled: bool = False
    mirrors_path: Path = Path("repos")
    worktrees_path: Path = Path("worktrees")
    executable_path: Path | None = None
    data_root: Path | None = None
    require_private_runtime: bool = False
    fetch_timeout_seconds: int = 300
    cleanup_on_success: bool = True
    keep_failed_worktrees_hours: int = 24
    stale_worktree_hours: int = 72


@dataclass(frozen=True)
class StorageConfig:
    logs_path: Path = Path("logs")
    results_path: Path = Path("results")
    drive_sync_path: Path | None = None
    drive_subdirectory: str = "CWapi"
    artifact_max_file_bytes: int = 64 * 1024 * 1024
    artifact_max_total_bytes: int = 512 * 1024 * 1024
    result_retention_days: int = 30
    create_zip_bundle: bool = True


@dataclass(frozen=True)
class RuntimeConfig:
    enabled: bool = False
    publish_progress: bool = True
    collect_artifacts: bool = True
    enable_cancel_drafts: bool = True


@dataclass(frozen=True)
class CodexToolhostConfig:
    enabled: bool = False
    executable_path: Path = Path("runtime/codex/current/bin/codex.exe")
    home_path: Path = Path("state/codex-home")
    stderr_log_path: Path = Path("logs/codex-app-server.log")
    permission_profile: str = ":workspace"
    startup_timeout_seconds: int = 30


@dataclass(frozen=True)
class AppConfig:
    runner: RunnerConfig
    gmail: GmailConfig
    state: StateConfig
    projects: ProjectsConfig
    security: SecurityConfig
    git: GitConfig = field(default_factory=GitConfig)
    storage: StorageConfig = field(default_factory=StorageConfig)
    runtime: RuntimeConfig = field(default_factory=RuntimeConfig)
    codex_toolhost: CodexToolhostConfig = field(default_factory=CodexToolhostConfig)


def _path(value: str, *, base_dir: Path | None = None) -> Path:
    resolved = Path(os.path.expandvars(value)).expanduser()
    if base_dir is not None and not resolved.is_absolute():
        resolved = base_dir / resolved
    return resolved.resolve()


def _require(mapping: dict[str, Any], key: str) -> Any:
    if key not in mapping:
        raise ConfigError(f"配置缺少字段：{key}")
    return mapping[key]


def _ensure_mapping(value: Any, name: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ConfigError(f"'{name}' 必须是对象。")
    return value


def _default_root(database_path: Path) -> Path:
    parent = database_path.parent
    return parent.parent if parent.name.lower() == "state" else parent


def _validate_positive(name: str, value: int, minimum: int = 1) -> int:
    if value < minimum:
        raise ConfigError(f"{name} 必须大于或等于 {minimum}。")
    return value


def load_config(
    path: str | Path | None = None,
    *,
    app_root: str | Path | None = None,
) -> AppConfig:
    config_path = resolve_config_path(path, app_root=app_root)
    config_dir = config_path.parent

    if not config_path.exists():
        raise ConfigError(
            f"找不到配置文件：{config_path}\n"
            "请通过 CWapi GUI 初始化配置，或在开发环境创建 config/cwapi.yaml。"
        )

    raw = yaml.safe_load(config_path.read_text(encoding="utf-8"))
    if not isinstance(raw, dict):
        raise ConfigError("配置文件顶层必须是对象。")

    runner = _ensure_mapping(_require(raw, "runner"), "runner")
    gmail = _ensure_mapping(_require(raw, "gmail"), "gmail")
    state = _ensure_mapping(_require(raw, "state"), "state")
    security = _ensure_mapping(_require(raw, "security"), "security")

    database_path = _path(
        str(_require(state, "database_path")),
        base_dir=config_dir,
    )
    root_path = _default_root(database_path)

    allowed_repositories = frozenset(
        str(v) for v in _require(security, "allowed_repositories")
    )
    allowed_actions = frozenset(
        str(v) for v in _require(security, "allowed_actions")
    )
    from .execution.action_registry import KNOWN_ACTIONS

    unknown_actions = allowed_actions - (KNOWN_ACTIONS | {"dry_run"})
    if unknown_actions:
        raise ConfigError(
            f"security.allowed_actions 包含未知 action：{sorted(unknown_actions)}"
        )

    raw_projects = raw.get("projects", {})
    if not isinstance(raw_projects, dict):
        raise ConfigError("'projects' 必须是字典。")

    project_map: dict[str, Path] = {}
    project_settings: dict[str, ProjectConfig] = {}
    for repo_name, repo_cfg_value in raw_projects.items():
        repo_name = str(repo_name)
        if repo_name not in allowed_repositories:
            raise ConfigError(f"项目 {repo_name} 不在 security.allowed_repositories 中。")
        repo_cfg = _ensure_mapping(repo_cfg_value, f"projects.{repo_name}")
        repo_path = _path(
            str(_require(repo_cfg, "path")),
            base_dir=config_dir,
        )
        if not repo_path.exists():
            raise ConfigError(f"项目路径不存在：{repo_path}")
        if not (repo_path / ".git").exists():
            raise ConfigError(f"项目路径不是 Git 仓库：{repo_path}")

        remote_url = str(
            repo_cfg.get("remote_url", f"https://github.com/{repo_name}.git")
        ).strip()
        if not remote_url:
            raise ConfigError(f"项目 {repo_name} 的 remote_url 不能为空。")

        test_paths_raw = repo_cfg.get("default_test_paths", ["tests"])
        if not isinstance(test_paths_raw, list) or not test_paths_raw:
            raise ConfigError(
                f"项目 {repo_name} 的 default_test_paths 必须是非空数组。"
            )

        project_map[repo_name] = repo_path
        project_settings[repo_name] = ProjectConfig(
            name=str(repo_cfg.get("name", repo_name)).strip() or repo_name,
            path=repo_path,
            remote_url=remote_url,
            repository=repo_name,
            python_executable=str(repo_cfg.get("python_executable", sys.executable)),
            cargo_executable=str(repo_cfg.get("cargo_executable", "cargo")),
            default_test_paths=tuple(str(v) for v in test_paths_raw),
            allow_dependency_check=bool(repo_cfg.get("allow_dependency_check", True)),
        )

    raw_git = _ensure_mapping(raw.get("git", {}), "git")
    raw_storage = _ensure_mapping(raw.get("storage", {}), "storage")
    raw_runtime = _ensure_mapping(raw.get("runtime", {}), "runtime")
    raw_codex = _ensure_mapping(raw.get("codex_toolhost", {}), "codex_toolhost")

    portable_release = (root_path / "app" / "release-manifest.json").is_file()
    git_executable_value = raw_git.get("executable_path")
    git_executable_path = (
        _path(str(git_executable_value), base_dir=config_dir)
        if git_executable_value
        else (
            (root_path / "runtime" / "git" / "cmd" / "git.exe").resolve()
            if portable_release
            else None
        )
    )
    require_private_git = bool(
        raw_git.get("require_private_runtime", portable_release)
    )

    drive_value = raw_storage.get("drive_sync_path")
    drive_sync_path = (
        _path(str(drive_value), base_dir=config_dir) if drive_value else None
    )

    runner_config = RunnerConfig(
        runner_id=str(_require(runner, "id")),
        channel_ids=tuple(str(v) for v in _require(runner, "channel_ids")),
        max_tasks_per_poll=_validate_positive(
            "runner.max_tasks_per_poll",
            int(runner.get("max_tasks_per_poll", 20)),
        ),
        poll_interval_seconds=_validate_positive(
            "runner.poll_interval_seconds",
            int(runner.get("poll_interval_seconds", 30)),
        ),
        cancel_poll_seconds=_validate_positive(
            "runner.cancel_poll_seconds",
            int(runner.get("cancel_poll_seconds", 5)),
        ),
        cleanup_interval_seconds=_validate_positive(
            "runner.cleanup_interval_seconds",
            int(runner.get("cleanup_interval_seconds", 900)),
        ),
        max_task_runtime_seconds=_validate_positive(
            "runner.max_task_runtime_seconds",
            int(runner.get("max_task_runtime_seconds", 21600)),
        ),
        progress_mode=str(runner.get("progress_mode", "step")),
    )
    if runner_config.progress_mode not in {"none", "step"}:
        raise ConfigError("runner.progress_mode 只能是 none 或 step。")

    security_config = SecurityConfig(
        allowed_repositories=allowed_repositories,
        allowed_actions=allowed_actions,
        max_step_timeout_seconds=_validate_positive(
            "security.max_step_timeout_seconds",
            int(security.get("max_step_timeout_seconds", 3600)),
        ),
        max_task_steps=_validate_positive(
            "security.max_task_steps",
            int(security.get("max_task_steps", 50)),
        ),
        max_relative_paths=_validate_positive(
            "security.max_relative_paths",
            int(security.get("max_relative_paths", 100)),
        ),
        allowed_environment_variables=tuple(
            str(v)
            for v in security.get(
                "allowed_environment_variables",
                SecurityConfig.allowed_environment_variables,
            )
        ),
    )

    codex_enabled = bool(raw_codex.get("enabled", False))
    codex_executable_path = _path(
        str(
            raw_codex.get(
                "executable_path",
                root_path / "runtime" / "codex" / "current" / "bin" / "codex.exe",
            )
        ),
        base_dir=config_dir,
    )
    codex_home_path = _path(
        str(raw_codex.get("home_path", root_path / "state" / "codex-home")),
        base_dir=config_dir,
    )
    codex_stderr_log_path = _path(
        str(
            raw_codex.get(
                "stderr_log_path",
                root_path / "logs" / "codex-app-server.log",
            )
        ),
        base_dir=config_dir,
    )
    permission_profile = str(
        raw_codex.get("permission_profile", ":workspace")
    ).strip()
    if not permission_profile:
        raise ConfigError("codex_toolhost.permission_profile 不能为空。")
    if codex_enabled:
        for name, value in (
            ("codex_toolhost.executable_path", codex_executable_path),
            ("codex_toolhost.home_path", codex_home_path),
            ("codex_toolhost.stderr_log_path", codex_stderr_log_path),
        ):
            if not value.is_absolute():
                raise ConfigError(f"{name} 必须是绝对路径，防止调用系统 Codex。")

    return AppConfig(
        runner=runner_config,
        gmail=GmailConfig(
            account=str(_require(gmail, "account")),
            credentials_path=_path(
                str(_require(gmail, "credentials_path")),
                base_dir=config_dir,
            ),
            token_path=_path(
                str(_require(gmail, "token_path")),
                base_dir=config_dir,
            ),
            task_query=str(_require(gmail, "task_query")),
            max_results=_validate_positive(
                "gmail.max_results", int(gmail.get("max_results", 100))
            ),
            cancel_query=str(
                gmail.get(
                    "cancel_query",
                    'in:drafts subject:"[CWapi/1][CANCEL][REQUESTED]"',
                )
            ),
        ),
        state=StateConfig(database_path=database_path),
        projects=ProjectsConfig(mapping=project_map, settings=project_settings),
        security=security_config,
        git=GitConfig(
            enabled=bool(raw_git.get("enabled", bool(raw_runtime.get("enabled", False)))),
            mirrors_path=_path(
                str(raw_git.get("mirrors_path", root_path / "repos")),
                base_dir=config_dir,
            ),
            worktrees_path=_path(
                str(raw_git.get("worktrees_path", root_path / "worktrees")),
                base_dir=config_dir,
            ),
            executable_path=git_executable_path,
            data_root=root_path.resolve(),
            require_private_runtime=require_private_git,
            fetch_timeout_seconds=_validate_positive(
                "git.fetch_timeout_seconds",
                int(raw_git.get("fetch_timeout_seconds", 300)),
            ),
            cleanup_on_success=bool(raw_git.get("cleanup_on_success", True)),
            keep_failed_worktrees_hours=max(
                0, int(raw_git.get("keep_failed_worktrees_hours", 24))
            ),
            stale_worktree_hours=_validate_positive(
                "git.stale_worktree_hours",
                int(raw_git.get("stale_worktree_hours", 72)),
            ),
        ),
        storage=StorageConfig(
            logs_path=_path(
                str(raw_storage.get("logs_path", root_path / "logs")),
                base_dir=config_dir,
            ),
            results_path=_path(
                str(raw_storage.get("results_path", root_path / "results")),
                base_dir=config_dir,
            ),
            drive_sync_path=drive_sync_path,
            drive_subdirectory=str(raw_storage.get("drive_subdirectory", "CWapi")),
            artifact_max_file_bytes=_validate_positive(
                "storage.artifact_max_file_bytes",
                int(raw_storage.get("artifact_max_file_bytes", 64 * 1024 * 1024)),
            ),
            artifact_max_total_bytes=_validate_positive(
                "storage.artifact_max_total_bytes",
                int(raw_storage.get("artifact_max_total_bytes", 512 * 1024 * 1024)),
            ),
            result_retention_days=_validate_positive(
                "storage.result_retention_days",
                int(raw_storage.get("result_retention_days", 30)),
            ),
            create_zip_bundle=bool(raw_storage.get("create_zip_bundle", True)),
        ),
        runtime=RuntimeConfig(
            enabled=bool(raw_runtime.get("enabled", False)),
            publish_progress=bool(raw_runtime.get("publish_progress", True)),
            collect_artifacts=bool(raw_runtime.get("collect_artifacts", True)),
            enable_cancel_drafts=bool(raw_runtime.get("enable_cancel_drafts", True)),
        ),
        codex_toolhost=CodexToolhostConfig(
            enabled=codex_enabled,
            executable_path=codex_executable_path,
            home_path=codex_home_path,
            stderr_log_path=codex_stderr_log_path,
            permission_profile=permission_profile,
            startup_timeout_seconds=_validate_positive(
                "codex_toolhost.startup_timeout_seconds",
                int(raw_codex.get("startup_timeout_seconds", 30)),
            ),
        ),
    )
