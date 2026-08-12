from __future__ import annotations

from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
from typing import Any


class CodexRuntimeLockError(ValueError):
    pass


@dataclass(frozen=True)
class RuntimeProvenance:
    repository: str
    source_ref: str
    source_commit: str
    version: str
    archive_sha256: str
    executable_sha256: str
    lock_path: Path
    manifest_path: Path

    def receipt_fields(self) -> dict[str, str]:
        return {
            "source_repository": self.repository,
            "source_ref": self.source_ref,
            "source_commit": self.source_commit,
            "runtime_version": self.version,
            "archive_sha256": self.archive_sha256,
            "executable_sha256": self.executable_sha256,
        }


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _read_json(path: Path, name: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8-sig"))
    except (OSError, json.JSONDecodeError) as exc:
        raise CodexRuntimeLockError(f"无法读取 {name}：{path}: {exc}") from exc
    if not isinstance(value, dict):
        raise CodexRuntimeLockError(f"{name} 顶层必须是对象：{path}")
    return value


def _project_root(executable_path: Path) -> Path:
    release_root = (
        executable_path.parent.parent
        if executable_path.parent.name.lower() == "bin"
        else executable_path.parent
    )
    if release_root.name.lower() == "current":
        return release_root.parent.parent.parent
    return release_root.parent


def _required_string(value: dict[str, Any], key: str, name: str) -> str:
    resolved = value.get(key)
    if not isinstance(resolved, str) or not resolved.strip():
        raise CodexRuntimeLockError(f"{name}.{key} 必须是非空字符串。")
    return resolved.strip()


def _required_sha256(value: dict[str, Any], key: str, name: str) -> str:
    resolved = _required_string(value, key, name).lower()
    if len(resolved) != 64 or any(ch not in "0123456789abcdef" for ch in resolved):
        raise CodexRuntimeLockError(f"{name}.{key} 必须是 64 位 SHA-256。")
    return resolved


def verify_codex_runtime(executable_path: Path) -> RuntimeProvenance | None:
    """Verify an installed runtime against the pinned fork source and transport."""

    root = _project_root(executable_path.absolute())
    lock_override = os.environ.get("CWAPI_CODEX_RUNTIME_LOCK")
    lock_path = (
        Path(lock_override).expanduser()
        if lock_override
        else root / "config" / "codex-runtime.lock.json"
    )
    if not lock_path.exists():
        return None
    if not lock_path.is_absolute():
        lock_path = (root / lock_path).resolve()
    lock = _read_json(lock_path, "Codex runtime lock")
    if lock.get("schema") != "cwapi.codex-runtime-lock.v1":
        raise CodexRuntimeLockError(
            "Codex runtime lock schema 必须是 cwapi.codex-runtime-lock.v1。"
        )
    if lock.get("state") != "source_ready":
        raise CodexRuntimeLockError(
            "Codex source 尚未锁定。必须固定用户 Fork、ref 与完整 commit。"
        )

    repository = _required_string(lock, "repository", "lock")
    if repository.casefold() == "openai/codex":
        raise CodexRuntimeLockError(
            "CWapi 禁止把官方 openai/codex 直接作为运行时来源；必须使用用户 Fork。"
        )
    source_ref = _required_string(lock, "source_ref", "lock")
    source_commit = _required_string(lock, "source_commit", "lock").lower()
    version = _required_string(lock, "version", "lock")
    install_transport = str(lock.get("install_transport", "git")).strip() or "git"
    if install_transport not in {"git", "github_release"}:
        raise CodexRuntimeLockError(
            "lock.install_transport 只能是 git 或 github_release。"
        )
    if len(source_commit) != 40 or any(
        ch not in "0123456789abcdef" for ch in source_commit
    ):
        raise CodexRuntimeLockError("lock.source_commit 必须是 40 位十六进制提交。")

    manifest_override = os.environ.get("CWAPI_CODEX_RUNTIME_MANIFEST")
    manifest_path = (
        Path(manifest_override).expanduser()
        if manifest_override
        else root / "runtime" / "codex" / "runtime.json"
    )
    if not manifest_path.is_absolute():
        manifest_path = (root / manifest_path).resolve()
    manifest = _read_json(manifest_path, "Codex runtime manifest")

    manifest_transport = _required_string(
        manifest,
        "source_transport",
        "manifest",
    )
    allowed_transports = {install_transport}
    if install_transport == "github_release":
        # The normal path is the prebuilt Release. A source build is accepted only
        # when the operator explicitly used the separate BuildFromSource path.
        allowed_transports.add("git")
    if manifest_transport not in allowed_transports:
        raise CodexRuntimeLockError(
            "Codex runtime manifest 与来源锁不一致："
            f"source_transport expected one of {sorted(allowed_transports)}, "
            f"actual={manifest_transport}"
        )

    expected = {
        "source_repository": repository,
        "source_ref": source_ref,
        "source_commit": source_commit,
        "version": version,
    }
    for key, expected_value in expected.items():
        actual = str(manifest.get(key, "")).strip()
        compare_actual = actual.lower() if key == "source_commit" else actual
        if compare_actual != expected_value:
            raise CodexRuntimeLockError(
                "Codex runtime manifest 与来源锁不一致："
                f"{key} expected={expected_value}, actual={actual}"
            )

    if manifest_transport == "github_release":
        release_repository = _required_string(lock, "release_repository", "lock")
        release_tag = _required_string(lock, "release_tag", "lock")
        for key, expected_value in {
            "release_repository": release_repository,
            "release_tag": release_tag,
        }.items():
            actual = str(manifest.get(key, "")).strip()
            if actual != expected_value:
                raise CodexRuntimeLockError(
                    "Codex runtime manifest 与 Release 锁不一致："
                    f"{key} expected={expected_value}, actual={actual}"
                )

    archive_sha256 = _required_sha256(manifest, "archive_sha256", "manifest")
    executable_sha256 = _required_sha256(
        manifest,
        "executable_sha256",
        "manifest",
    )
    actual_executable_sha256 = _sha256_file(executable_path)
    if actual_executable_sha256 != executable_sha256:
        raise CodexRuntimeLockError(
            "当前 codex.exe SHA-256 与安装 manifest 不一致，运行时可能被替换。"
        )
    return RuntimeProvenance(
        repository=repository,
        source_ref=source_ref,
        source_commit=source_commit,
        version=version,
        archive_sha256=archive_sha256,
        executable_sha256=executable_sha256,
        lock_path=lock_path,
        manifest_path=manifest_path,
    )
