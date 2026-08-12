from __future__ import annotations

from dataclasses import dataclass
import json
import os
from pathlib import Path
from typing import Any, Mapping

import yaml

from cwapi.security import SecurityViolation, normalize_relative_path


class CodexCapabilityPolicyError(ValueError):
    pass


@dataclass(frozen=True)
class McpServerPolicy:
    tools: frozenset[str]
    resource_uri_prefixes: tuple[str, ...]

    def allows_tool(self, tool: str) -> bool:
        return "*" in self.tools or tool in self.tools

    def allows_resource(self, uri: str) -> bool:
        return "*" in self.resource_uri_prefixes or any(
            uri.startswith(prefix) for prefix in self.resource_uri_prefixes
        )


@dataclass(frozen=True)
class FilesystemRootPolicy:
    name: str
    path_template: str
    readable: bool
    writable: bool


@dataclass(frozen=True)
class CodexCapabilityPolicy:
    permission_profiles: frozenset[str]
    mcp_servers: Mapping[str, McpServerPolicy]
    browser_server: str | None
    filesystem_roots: Mapping[str, FilesystemRootPolicy]
    max_mcp_arguments_bytes: int = 262_144
    max_fs_write_bytes: int = 16 * 1024 * 1024
    max_session_interactions: int = 100
    max_session_output_bytes: int = 64 * 1024 * 1024
    max_watch_wait_ms: int = 60_000

    def require_permission_profile(self, profile: str) -> str:
        normalized = profile.strip()
        if normalized not in self.permission_profiles:
            raise CodexCapabilityPolicyError(
                f"Codex permission profile 未获授权：{normalized}"
            )
        return normalized

    def require_mcp_tool(self, server: str, tool: str) -> None:
        configured = self.mcp_servers.get(server)
        if configured is None or not configured.allows_tool(tool):
            raise CodexCapabilityPolicyError(
                f"MCP tool 未获授权：server={server}, tool={tool}"
            )

    def require_mcp_resource(self, server: str, uri: str) -> None:
        configured = self.mcp_servers.get(server)
        if configured is None or not configured.allows_resource(uri):
            raise CodexCapabilityPolicyError(
                f"MCP resource 未获授权：server={server}, uri={uri}"
            )

    def validate_json_size(self, value: Any, *, limit: int, name: str) -> None:
        try:
            encoded = json.dumps(
                value,
                ensure_ascii=False,
                separators=(",", ":"),
            ).encode("utf-8")
        except (TypeError, ValueError) as exc:
            raise CodexCapabilityPolicyError(f"{name} 必须可序列化为 JSON。") from exc
        if len(encoded) > limit:
            raise CodexCapabilityPolicyError(
                f"{name} 超过大小限制：{len(encoded)} > {limit}"
            )

    def resolve_path(
        self,
        *,
        root_name: str,
        relative_path: str,
        workspace: Path,
        task_results: Path,
        task_temp: Path,
        write: bool,
        allow_dot: bool = True,
    ) -> Path:
        root_policy = self.filesystem_roots.get(root_name)
        if root_policy is None:
            raise CodexCapabilityPolicyError(f"未知 Codex filesystem root：{root_name}")
        if write and not root_policy.writable:
            raise CodexCapabilityPolicyError(f"filesystem root 禁止写入：{root_name}")
        if not write and not root_policy.readable:
            raise CodexCapabilityPolicyError(f"filesystem root 禁止读取：{root_name}")

        substitutions = {
            "{workspace}": str(workspace.resolve()),
            "{task_results}": str(task_results.resolve()),
            "{task_temp}": str(task_temp.resolve()),
        }
        root_text = os.path.expandvars(root_policy.path_template)
        for token, value in substitutions.items():
            root_text = root_text.replace(token, value)
        root = Path(root_text).expanduser()
        if not root.is_absolute():
            raise CodexCapabilityPolicyError(
                f"filesystem root 必须解析为绝对路径：{root_name}={root}"
            )
        root = root.resolve()
        try:
            normalized = normalize_relative_path(relative_path, allow_dot=allow_dot)
        except SecurityViolation as exc:
            raise CodexCapabilityPolicyError(str(exc)) from exc
        candidate = (root / normalized).resolve()
        try:
            candidate.relative_to(root)
        except ValueError as exc:
            raise CodexCapabilityPolicyError(
                f"filesystem path 逃逸授权 root：{root_name}/{relative_path}"
            ) from exc
        return candidate


def _as_mapping(value: Any, name: str) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise CodexCapabilityPolicyError(f"{name} 必须是对象。")
    return dict(value)


def _positive_int(value: Any, *, name: str, default: int) -> int:
    resolved = default if value is None else int(value)
    if resolved < 1:
        raise CodexCapabilityPolicyError(f"{name} 必须大于 0。")
    return resolved


def load_codex_capability_policy(
    path: Path,
    *,
    default_permission_profile: str,
) -> CodexCapabilityPolicy:
    if not path.exists():
        raise CodexCapabilityPolicyError(f"找不到 Codex capability policy：{path}")
    raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(raw, dict):
        raise CodexCapabilityPolicyError("Codex capability policy 顶层必须是对象。")
    if raw.get("schema") != "cwapi.codex-capabilities.v1":
        raise CodexCapabilityPolicyError(
            "Codex capability policy schema 必须是 cwapi.codex-capabilities.v1。"
        )

    profiles_raw = raw.get("permission_profiles", [default_permission_profile])
    if not isinstance(profiles_raw, list) or not profiles_raw:
        raise CodexCapabilityPolicyError("permission_profiles 必须是非空数组。")
    profiles = frozenset(str(item).strip() for item in profiles_raw if str(item).strip())
    if default_permission_profile not in profiles:
        raise CodexCapabilityPolicyError(
            "默认 codex_toolhost.permission_profile 必须位于 permission_profiles。"
        )

    mcp_raw = _as_mapping(raw.get("mcp"), "mcp")
    servers_raw = _as_mapping(mcp_raw.get("servers"), "mcp.servers")
    mcp_servers: dict[str, McpServerPolicy] = {}
    for name, value in servers_raw.items():
        item = _as_mapping(value, f"mcp.servers.{name}")
        tools_raw = item.get("tools", [])
        resources_raw = item.get("resource_uri_prefixes", [])
        if not isinstance(tools_raw, list) or not all(
            isinstance(entry, str) and entry for entry in tools_raw
        ):
            raise CodexCapabilityPolicyError(
                f"mcp.servers.{name}.tools 必须是字符串数组。"
            )
        if not isinstance(resources_raw, list) or not all(
            isinstance(entry, str) and entry for entry in resources_raw
        ):
            raise CodexCapabilityPolicyError(
                f"mcp.servers.{name}.resource_uri_prefixes 必须是字符串数组。"
            )
        mcp_servers[str(name)] = McpServerPolicy(
            tools=frozenset(tools_raw),
            resource_uri_prefixes=tuple(resources_raw),
        )

    browser_raw = _as_mapping(raw.get("browser"), "browser")
    browser_server_value = browser_raw.get("server")
    browser_server = str(browser_server_value).strip() if browser_server_value else None
    if browser_server and browser_server not in mcp_servers:
        raise CodexCapabilityPolicyError(
            f"browser.server 未在 mcp.servers 中登记：{browser_server}"
        )

    filesystem_raw = _as_mapping(raw.get("filesystem"), "filesystem")
    roots_raw = _as_mapping(filesystem_raw.get("roots"), "filesystem.roots")
    if not roots_raw:
        raise CodexCapabilityPolicyError("filesystem.roots 不能为空。")
    filesystem_roots: dict[str, FilesystemRootPolicy] = {}
    for name, value in roots_raw.items():
        item = _as_mapping(value, f"filesystem.roots.{name}")
        path_template = str(item.get("path", "")).strip()
        if not path_template:
            raise CodexCapabilityPolicyError(
                f"filesystem.roots.{name}.path 不能为空。"
            )
        filesystem_roots[str(name)] = FilesystemRootPolicy(
            name=str(name),
            path_template=path_template,
            readable=bool(item.get("read", False)),
            writable=bool(item.get("write", False)),
        )

    limits = _as_mapping(raw.get("limits"), "limits")
    return CodexCapabilityPolicy(
        permission_profiles=profiles,
        mcp_servers=mcp_servers,
        browser_server=browser_server,
        filesystem_roots=filesystem_roots,
        max_mcp_arguments_bytes=_positive_int(
            limits.get("max_mcp_arguments_bytes"),
            name="limits.max_mcp_arguments_bytes",
            default=262_144,
        ),
        max_fs_write_bytes=_positive_int(
            limits.get("max_fs_write_bytes"),
            name="limits.max_fs_write_bytes",
            default=16 * 1024 * 1024,
        ),
        max_session_interactions=_positive_int(
            limits.get("max_session_interactions"),
            name="limits.max_session_interactions",
            default=100,
        ),
        max_session_output_bytes=_positive_int(
            limits.get("max_session_output_bytes"),
            name="limits.max_session_output_bytes",
            default=64 * 1024 * 1024,
        ),
        max_watch_wait_ms=_positive_int(
            limits.get("max_watch_wait_ms"),
            name="limits.max_watch_wait_ms",
            default=60_000,
        ),
    )
