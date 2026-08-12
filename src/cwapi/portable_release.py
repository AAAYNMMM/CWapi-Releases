from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import tempfile
from typing import Any


RELEASE_SCHEMA = "cwapi.portable-release.v2"
SUPPORTED_RELEASE_SCHEMAS = frozenset(
    {"cwapi.portable-release.v1", RELEASE_SCHEMA}
)
PORTABLE_STATE_SCHEMA = "cwapi.portable-state.v1"
PORTABLE_STATE_VERSION = 1
MCP_RUNTIME_SCHEMA = "cwapi.playwright-mcp-runtime.v2"


class PortableReleaseError(RuntimeError):
    pass


@dataclass(frozen=True)
class PortableReleasePaths:
    app_root: Path
    data_root: Path
    manifest_path: Path
    state_path: Path
    codex_template_path: Path
    codex_config_path: Path

    @classmethod
    def for_config(cls, config_path: Path) -> "PortableReleasePaths":
        config = config_path.expanduser().resolve()
        data_root = config.parent.parent
        app_root = data_root.parent
        return cls(
            app_root=app_root,
            data_root=data_root,
            manifest_path=data_root / "app" / "release-manifest.json",
            state_path=data_root / "state" / "portable-state.json",
            codex_template_path=data_root / "app" / "config" / "codex-home.template.toml",
            codex_config_path=data_root / "state" / "codex-home" / "config.toml",
        )


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _atomic_text(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
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


def _atomic_json(path: Path, payload: dict[str, Any]) -> None:
    _atomic_text(path, json.dumps(payload, ensure_ascii=False, indent=2) + "\n")


def _read_json(path: Path, *, label: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8-sig"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise PortableReleaseError(f"无法读取 {label}：{path}: {exc}") from exc
    if not isinstance(value, dict):
        raise PortableReleaseError(f"{label} 顶层必须是对象：{path}")
    return value


def ensure_portable_layout(data_root: Path) -> None:
    root = data_root.expanduser().resolve()
    for relative in (
        "app/config",
        "runtime/python",
        "runtime/transport",
        "runtime/codex",
        "runtime/node",
        "runtime/mcp/playwright",
        "runtime/browser",
        "config",
        "secrets",
        "state/codex-home",
        "state/runtime-home/appdata",
        "state/runtime-home/localappdata",
        "repos",
        "worktrees",
        "logs/runtime",
        "logs/tasks",
        "logs/browser",
        "results",
        "cache",
        "temp",
    ):
        (root / relative).mkdir(parents=True, exist_ok=True)


def _release_manifest(paths: PortableReleasePaths) -> dict[str, Any] | None:
    if not paths.manifest_path.is_file():
        return None
    value = _read_json(paths.manifest_path, label="portable release manifest")
    if value.get("schema") not in SUPPORTED_RELEASE_SCHEMAS:
        raise PortableReleaseError(
            "portable release manifest schema 必须是 "
            f"{sorted(SUPPORTED_RELEASE_SCHEMAS)} 之一。"
        )
    if int(value.get("layout_version", 0)) != 1:
        raise PortableReleaseError("不支持的 portable layout_version。")
    return value


def _resolve_package_relative(paths: PortableReleasePaths, relative: str) -> Path:
    raw = str(relative).replace("\\", "/").strip().lstrip("/")
    if not raw or raw.startswith("../") or "/../" in f"/{raw}/":
        raise PortableReleaseError(f"非法 release manifest path：{relative!r}")
    target = (paths.app_root / Path(raw)).resolve(strict=False)
    try:
        target.relative_to(paths.app_root.resolve(strict=False))
    except ValueError as exc:
        raise PortableReleaseError(f"release manifest path 越界：{relative!r}") from exc
    return target


def verify_release_manifest(
    config_path: Path,
    *,
    verify_hashes: bool = False,
) -> dict[str, Any]:
    paths = PortableReleasePaths.for_config(config_path)
    manifest = _release_manifest(paths)
    if manifest is None:
        return {
            "portable": False,
            "verified": True,
            "hashes_verified": False,
            "manifest_path": str(paths.manifest_path),
            "failures": [],
        }
    failures: list[str] = []
    critical = manifest.get("critical_files")
    if not isinstance(critical, list) or not critical:
        raise PortableReleaseError("portable release manifest 缺少 critical_files。")
    checked = 0
    for item in critical:
        if not isinstance(item, dict):
            failures.append("invalid-critical-file-entry")
            continue
        relative = str(item.get("path") or "")
        expected = str(item.get("sha256") or "").lower()
        target = _resolve_package_relative(paths, relative)
        checked += 1
        if not target.is_file():
            failures.append(f"missing:{relative}")
            continue
        if verify_hashes:
            if len(expected) != 64 or any(ch not in "0123456789abcdef" for ch in expected):
                failures.append(f"invalid-sha256:{relative}")
                continue
            if _sha256_file(target) != expected:
                failures.append(f"sha256:{relative}")
    return {
        "portable": True,
        "verified": not failures,
        "hashes_verified": bool(verify_hashes),
        "manifest_path": str(paths.manifest_path),
        "layout_version": manifest.get("layout_version"),
        "manifest_schema": manifest.get("schema"),
        "release_version": manifest.get("version"),
        "source_commit": manifest.get("source_commit"),
        "critical_files_checked": checked,
        "failures": failures,
    }


def migrate_portable_state(config_path: Path) -> dict[str, Any]:
    paths = PortableReleasePaths.for_config(config_path)
    ensure_portable_layout(paths.data_root)
    if not paths.state_path.is_file():
        payload = {
            "schema": PORTABLE_STATE_SCHEMA,
            "version": PORTABLE_STATE_VERSION,
            "created_at": _utc_now(),
            "last_migrated_at": _utc_now(),
        }
        _atomic_json(paths.state_path, payload)
        return {"created": True, "from": None, "to": PORTABLE_STATE_VERSION}
    payload = _read_json(paths.state_path, label="portable state")
    if payload.get("schema") != PORTABLE_STATE_SCHEMA:
        raise PortableReleaseError(f"portable state schema 必须是 {PORTABLE_STATE_SCHEMA}。")
    try:
        version = int(payload.get("version", 0))
    except (TypeError, ValueError) as exc:
        raise PortableReleaseError("portable state version 无效。") from exc
    if version > PORTABLE_STATE_VERSION:
        raise PortableReleaseError(
            f"portable state version={version} 高于当前支持版本={PORTABLE_STATE_VERSION}。"
        )
    original = version
    if version < 1:
        payload["version"] = 1
        payload["last_migrated_at"] = _utc_now()
        _atomic_json(paths.state_path, payload)
        version = 1
    return {"created": False, "from": original, "to": version}


def _toml_escape(value: Path | str) -> str:
    return str(value).replace("\\", "\\\\").replace('"', '\\"')


def _mcp_runtime(paths: PortableReleasePaths) -> dict[str, Any]:
    runtime_path = paths.data_root / "runtime" / "mcp" / "playwright" / "runtime.json"
    value = _read_json(runtime_path, label="Playwright MCP runtime manifest")
    if value.get("schema") != MCP_RUNTIME_SCHEMA:
        raise PortableReleaseError(
            f"Playwright MCP runtime manifest schema 必须是 {MCP_RUNTIME_SCHEMA}。"
        )
    return value


def _resolve_data_relative(data_root: Path, relative: str, *, label: str) -> Path:
    raw = str(relative).replace("\\", "/").strip().lstrip("/")
    if not raw or raw.startswith("../") or "/../" in f"/{raw}/":
        raise PortableReleaseError(f"{label} 必须是 DATA_ROOT 内的相对路径。")
    target = (data_root / Path(raw)).resolve(strict=False)
    try:
        target.relative_to(data_root.resolve(strict=False))
    except ValueError as exc:
        raise PortableReleaseError(f"{label} 越出 DATA_ROOT。") from exc
    return target


def render_private_codex_home(config_path: Path) -> dict[str, Any]:
    paths = PortableReleasePaths.for_config(config_path)
    if not paths.codex_template_path.is_file():
        raise PortableReleaseError(f"缺少 Codex config template：{paths.codex_template_path}")
    runtime = _mcp_runtime(paths)
    node = _resolve_data_relative(
        paths.data_root,
        str(runtime.get("node_relative") or ""),
        label="node_relative",
    )
    cli = _resolve_data_relative(
        paths.data_root,
        str(runtime.get("cli_relative") or ""),
        label="cli_relative",
    )
    browsers = _resolve_data_relative(
        paths.data_root,
        str(runtime.get("browsers_relative") or ""),
        label="browsers_relative",
    )
    if not node.is_file():
        raise PortableReleaseError(f"portable Node 不存在：{node}")
    if not cli.is_file():
        raise PortableReleaseError(f"portable Playwright MCP CLI 不存在：{cli}")
    if not browsers.is_dir():
        raise PortableReleaseError(f"portable Chromium runtime 不存在：{browsers}")

    template_text = paths.codex_template_path.read_text(encoding="utf-8")
    browser_executable: Path | None = None
    if "__BROWSER_EXE__" in template_text:
        browser_executable = _resolve_data_relative(
            paths.data_root,
            str(runtime.get("browser_executable_relative") or ""),
            label="browser_executable_relative",
        )
        if not browser_executable.is_file():
            raise PortableReleaseError(
                f"portable Chromium executable 不存在：{browser_executable}"
            )

    runtime_home = paths.data_root / "state" / "runtime-home"
    appdata = runtime_home / "appdata"
    localappdata = runtime_home / "localappdata"
    browser_output = paths.data_root / "logs" / "browser"
    temp_root = paths.data_root / "temp"
    for directory in (runtime_home, appdata, localappdata, browser_output, temp_root):
        directory.mkdir(parents=True, exist_ok=True)

    replacements: dict[str, str] = {
        "__NODE_EXE__": _toml_escape(node),
        "__PLAYWRIGHT_MCP_CLI__": _toml_escape(cli),
        "__PLAYWRIGHT_BROWSERS_PATH__": _toml_escape(browsers),
        "__BROWSER_OUTPUT_DIR__": _toml_escape(browser_output),
        "__SYSTEM_ROOT__": _toml_escape(os.environ.get("SystemRoot", r"C:\\Windows")),
        "__APPDATA__": _toml_escape(appdata),
        "__LOCALAPPDATA__": _toml_escape(localappdata),
        "__HOME__": _toml_escape(runtime_home),
        "__TEMP__": _toml_escape(temp_root),
    }
    if browser_executable is not None:
        replacements["__BROWSER_EXE__"] = _toml_escape(browser_executable)
    text = template_text
    for marker, value in replacements.items():
        text = text.replace(marker, value)
    import re

    unresolved = sorted(set(re.findall(r"__[A-Z0-9_]+__", text)))
    if unresolved:
        raise PortableReleaseError(
            "Codex config template 仍有未解析 placeholder：" + ", ".join(unresolved)
        )
    _atomic_text(paths.codex_config_path, text)
    return {
        "config_path": str(paths.codex_config_path),
        "node": str(node),
        "mcp_cli": str(cli),
        "browsers": str(browsers),
        "browser_executable": (
            str(browser_executable) if browser_executable is not None else None
        ),
    }


def prepare_portable_runtime(config_path: Path) -> dict[str, Any]:
    paths = PortableReleasePaths.for_config(config_path)
    manifest = _release_manifest(paths)
    if manifest is None:
        return {"portable": False, "prepared": False}
    ensure_portable_layout(paths.data_root)
    verification = verify_release_manifest(config_path, verify_hashes=False)
    if not verification["verified"]:
        raise PortableReleaseError(
            "portable release 缺少关键文件：" + ", ".join(verification["failures"])
        )
    migration = migrate_portable_state(config_path)
    codex = render_private_codex_home(config_path)
    return {
        "portable": True,
        "prepared": True,
        "release_version": manifest.get("version"),
        "migration": migration,
        "codex": codex,
    }


def backup_boundary(config_path: Path) -> dict[str, Any]:
    paths = PortableReleasePaths.for_config(config_path)
    return {
        "schema": "cwapi.portable-backup-boundary.v1",
        "persistent_required": [
            "CWapi-data/config",
            "CWapi-data/secrets",
            "CWapi-data/state",
        ],
        "persistent_optional": ["CWapi-data/results"],
        "release_restorable": [
            "CWapi.exe",
            "CWapi-data/app",
            "CWapi-data/runtime",
        ],
        "rebuildable_or_disposable": [
            "CWapi-data/repos",
            "CWapi-data/worktrees",
            "CWapi-data/logs",
            "CWapi-data/cache",
            "CWapi-data/temp",
        ],
        "external_projects": "absolute paths are references only; never copied or rewritten",
        "contains_secrets": True,
        "app_root": str(paths.app_root),
    }


def portable_diagnostics(config_path: Path) -> dict[str, Any]:
    paths = PortableReleasePaths.for_config(config_path)
    if not paths.manifest_path.is_file():
        return {"portable": False, "ok": True, "detail": "source/development mode"}
    try:
        release = verify_release_manifest(config_path, verify_hashes=False)
        state = migrate_portable_state(config_path)
        return {
            "portable": True,
            "ok": bool(release["verified"]),
            "detail": (
                f"release={release.get('release_version')} / layout={release.get('layout_version')} "
                f"/ state={state.get('to')} / critical={release.get('critical_files_checked')}"
            ),
            "release": release,
            "backup": backup_boundary(config_path),
        }
    except Exception as exc:
        return {"portable": True, "ok": False, "detail": str(exc)[:600]}
