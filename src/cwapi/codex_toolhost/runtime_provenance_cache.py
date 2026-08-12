from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import threading

from .runtime_lock import RuntimeProvenance, verify_codex_runtime as _verify_runtime


@dataclass(frozen=True)
class _ExecutableVersion:
    path: str
    size: int
    mtime_ns: int


_lock = threading.RLock()
_cache: dict[_ExecutableVersion, RuntimeProvenance | None] = {}
_MAX_ENTRIES = 16


def _version(path: Path) -> _ExecutableVersion:
    resolved = path.resolve()
    stat = resolved.stat()
    return _ExecutableVersion(
        path=str(resolved),
        size=int(stat.st_size),
        mtime_ns=int(stat.st_mtime_ns),
    )


def verify_codex_runtime_cached(
    executable_path: Path,
) -> RuntimeProvenance | None:
    version = _version(executable_path)
    with _lock:
        if version in _cache:
            return _cache[version]

    provenance = _verify_runtime(executable_path)
    with _lock:
        if version in _cache:
            return _cache[version]
        if len(_cache) >= _MAX_ENTRIES:
            oldest = next(iter(_cache))
            _cache.pop(oldest, None)
        _cache[version] = provenance
    return provenance


def clear_runtime_provenance_cache() -> None:
    with _lock:
        _cache.clear()


__all__ = [
    "clear_runtime_provenance_cache",
    "verify_codex_runtime_cached",
]
