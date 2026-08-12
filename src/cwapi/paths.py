from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path, PurePosixPath, PureWindowsPath
import sys


_MANAGED_PATH_PREFIX = "cwapi-data://"
_LEGACY_STATE_ANCHORS = frozenset({"repos", "worktrees", "logs", "results"})


@dataclass(frozen=True)
class PortablePaths:
    """Canonical roots for one CWapi installation/runtime.

    Release mode is rooted at the directory containing CWapi.exe and stores
    managed data below ``CWapi-data``. Source mode is an explicit development
    compatibility mode where the repository root itself is the data root.
    """

    app_root: Path
    data_root: Path
    config_root: Path
    runtime_root: Path
    state_root: Path
    log_root: Path
    result_root: Path
    repo_root: Path
    worktree_root: Path

    @classmethod
    def release(cls, app_root: str | Path) -> "PortablePaths":
        app = Path(app_root).expanduser().resolve()
        return cls._from_roots(app, app / "CWapi-data")

    @classmethod
    def from_executable(cls, executable: str | Path) -> "PortablePaths":
        return cls.release(Path(executable).expanduser().resolve().parent)

    @classmethod
    def source_tree(cls, root: str | Path | None = None) -> "PortablePaths":
        source_root = (
            Path(root).expanduser().resolve()
            if root is not None
            else Path(__file__).resolve().parents[2]
        )
        return cls._from_roots(source_root, source_root)

    @classmethod
    def _from_roots(cls, app_root: Path, data_root: Path) -> "PortablePaths":
        return cls(
            app_root=app_root,
            data_root=data_root,
            config_root=data_root / "config",
            runtime_root=data_root / "runtime",
            state_root=data_root / "state",
            log_root=data_root / "logs",
            result_root=data_root / "results",
            repo_root=data_root / "repos",
            worktree_root=data_root / "worktrees",
        )

    @property
    def config_path(self) -> Path:
        return self.config_root / "cwapi.yaml"


def default_source_root() -> Path:
    return Path(__file__).resolve().parents[2]


def data_root_from_database(database_path: str | Path) -> Path:
    database = Path(database_path).expanduser().resolve()
    parent = database.parent
    return parent.parent if parent.name.lower() == "state" else parent


@dataclass(frozen=True)
class ManagedPathCodec:
    """Store CWapi-owned paths without binding state to an install location."""

    data_root: Path

    @classmethod
    def for_database(cls, database_path: str | Path) -> "ManagedPathCodec":
        return cls(data_root_from_database(database_path))

    def encode(self, value: str | Path | None) -> str | None:
        if value is None:
            return None
        text = str(value)
        if text.startswith(_MANAGED_PATH_PREFIX):
            return text
        candidate = Path(text).expanduser()
        if not candidate.is_absolute():
            return text
        try:
            relative = candidate.resolve(strict=False).relative_to(
                self.data_root.resolve(strict=False)
            )
        except ValueError:
            return text
        return _MANAGED_PATH_PREFIX + relative.as_posix()

    def decode(self, value: str | Path | None) -> str | None:
        if value is None:
            return None
        text = str(value)
        if text.startswith(_MANAGED_PATH_PREFIX):
            relative = text[len(_MANAGED_PATH_PREFIX) :]
            return str((self.data_root / Path(relative)).resolve(strict=False))

        legacy_relative = self._legacy_managed_relative(text)
        if legacy_relative is not None:
            return str(
                (self.data_root / Path(*legacy_relative)).resolve(strict=False)
            )

        candidate = Path(text).expanduser()
        if candidate.is_absolute():
            try:
                candidate.resolve(strict=False).relative_to(
                    self.data_root.resolve(strict=False)
                )
                return str(candidate.resolve(strict=False))
            except ValueError:
                pass
        return text

    def normalize_for_storage(self, value: str | Path | None) -> str | None:
        if value is None:
            return None
        text = str(value)
        if text.startswith(_MANAGED_PATH_PREFIX):
            return text
        legacy_relative = self._legacy_managed_relative(text)
        if legacy_relative is not None:
            return _MANAGED_PATH_PREFIX + PurePosixPath(*legacy_relative).as_posix()
        return self.encode(text)

    @staticmethod
    def _legacy_managed_relative(value: str) -> tuple[str, ...] | None:
        windows = PureWindowsPath(value)
        if windows.drive and windows.root:
            parts = tuple(str(item) for item in windows.parts[1:])
        elif value.startswith("/"):
            parts = tuple(str(item) for item in PurePosixPath(value).parts[1:])
        else:
            return None

        for index, part in enumerate(parts):
            if part.casefold() not in _LEGACY_STATE_ANCHORS:
                continue
            prefix = parts[:index]
            belongs_to_cwapi = any(
                item.casefold() == "cwapi-data" or "cwapi" in item.casefold()
                for item in prefix
            )
            if belongs_to_cwapi:
                return parts[index:]
        return None


def resolve_config_path(
    path: str | Path | None = None,
    *,
    app_root: str | Path | None = None,
) -> Path:
    """Resolve a backend config path without consulting the process cwd.

    The packaged desktop launcher should always provide ``app_root`` (or an
    absolute config path). ``CWAPI_CONFIG`` remains a development/test override;
    relative overrides are rooted at the explicit app root when supplied and at
    the source tree otherwise.
    """

    configured = path
    if configured is None:
        configured = os.environ.get("CWAPI_CONFIG")

    if app_root is not None:
        roots = PortablePaths.release(app_root)
        base = roots.app_root
        default_path = roots.config_path
    else:
        roots = PortablePaths.source_tree()
        base = roots.app_root
        default_path = roots.config_path

    if configured is None or not str(configured).strip():
        return default_path.resolve()

    candidate = Path(str(configured)).expanduser()
    if not candidate.is_absolute():
        candidate = base / candidate
    return candidate.resolve()


def current_release_paths() -> PortablePaths:
    """Resolve release roots from the running executable.

    This helper is intended for a packaged executable. Source/development code
    should prefer ``PortablePaths.source_tree`` or pass explicit roots.
    """

    return PortablePaths.from_executable(sys.executable)
