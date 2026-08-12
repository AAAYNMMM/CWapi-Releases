from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import shutil
from typing import Mapping


class GitRuntimeError(RuntimeError):
    pass


@dataclass(frozen=True)
class GitRuntime:
    executable: Path
    root: Path | None
    private: bool
    source: str

    def command(self, *arguments: str) -> list[str]:
        return [str(self.executable), *arguments]

    @property
    def path_entries(self) -> tuple[Path, ...]:
        if self.root is None:
            return (self.executable.parent,)
        candidates = (
            self.root / "cmd",
            self.root / "mingw64" / "bin",
            self.root / "usr" / "bin",
        )
        return tuple(path for path in candidates if path.is_dir())

    def environment(
        self,
        base: Mapping[str, str] | None = None,
    ) -> dict[str, str]:
        environment = dict(os.environ if base is None else base)
        path_key = next(
            (name for name in environment if name.casefold() == "path"),
            "PATH",
        )
        inherited = environment.get(path_key, "")
        entries = [str(path) for path in self.path_entries]
        seen = {value.casefold() for value in entries}
        for value in inherited.split(os.pathsep):
            stripped = value.strip()
            if stripped and stripped.casefold() not in seen:
                entries.append(stripped)
                seen.add(stripped.casefold())
        environment[path_key] = os.pathsep.join(entries)
        return environment


def _private_root(executable: Path, data_root: Path | None) -> Path | None:
    if data_root is not None:
        expected = (data_root / "runtime" / "git").resolve(strict=False)
        try:
            executable.resolve(strict=False).relative_to(expected)
        except ValueError:
            pass
        else:
            return expected
    if executable.parent.name.casefold() == "cmd":
        parent = executable.parent.parent
        if (parent / "mingw64").is_dir() or (parent / "usr").is_dir():
            return parent.resolve(strict=False)
    return None


def resolve_git_runtime(
    *,
    executable_path: str | Path | None = None,
    data_root: str | Path | None = None,
    require_private: bool = False,
    allow_system: bool = True,
) -> GitRuntime:
    root = Path(data_root).expanduser().resolve() if data_root is not None else None
    candidate: Path | None = None
    source = ""

    if executable_path is not None and str(executable_path).strip():
        candidate = Path(executable_path).expanduser().resolve(strict=False)
        source = "configured"
    elif root is not None:
        bundled = root / "runtime" / "git" / "cmd" / "git.exe"
        if bundled.is_file() or require_private:
            candidate = bundled.resolve(strict=False)
            source = "bundled"
    else:
        override = os.environ.get("CWAPI_GIT_EXECUTABLE")
        if override:
            candidate = Path(override).expanduser().resolve(strict=False)
            source = "environment_override"

    if candidate is not None:
        private_root = _private_root(candidate, root)
        if not candidate.is_file():
            if require_private or executable_path is not None:
                raise GitRuntimeError(f"CWapi Git runtime 不存在：{candidate}")
        else:
            if require_private and private_root is None:
                raise GitRuntimeError("便携模式只允许 CWapi-data/runtime/git 私有 Git。")
            return GitRuntime(
                executable=candidate,
                root=private_root,
                private=private_root is not None,
                source=source,
            )

    if require_private:
        raise GitRuntimeError("便携模式缺少 CWapi 私有 Git runtime。")
    if not allow_system:
        raise GitRuntimeError("未配置可用 Git runtime。")
    discovered = shutil.which("git.exe") or shutil.which("git")
    if not discovered:
        raise GitRuntimeError("找不到 Git 可执行文件。")
    executable = Path(discovered).resolve()
    return GitRuntime(
        executable=executable,
        root=None,
        private=False,
        source="system_path",
    )


def resolve_configured_git(config: object) -> GitRuntime:
    return resolve_git_runtime(
        executable_path=getattr(config, "executable_path", None),
        data_root=getattr(config, "data_root", None),
        require_private=bool(getattr(config, "require_private_runtime", False)),
        allow_system=not bool(getattr(config, "require_private_runtime", False)),
    )


__all__ = [
    "GitRuntime",
    "GitRuntimeError",
    "resolve_configured_git",
    "resolve_git_runtime",
]
