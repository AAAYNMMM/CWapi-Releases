from __future__ import annotations


class CodexMethodDenied(ValueError):
    pass


TOOL_ONLY_METHODS: frozenset[str] = frozenset(
    {
        "command/exec",
        "command/exec/write",
        "command/exec/resize",
        "command/exec/terminate",
        "mcpServerStatus/list",
        "mcpServer/resource/read",
        "mcpServer/tool/call",
        "fs/readFile",
        "fs/writeFile",
        "fs/createDirectory",
        "fs/getMetadata",
        "fs/readDirectory",
        "fs/remove",
        "fs/copy",
        "fs/watch",
        "fs/unwatch",
    }
)

INTERNAL_SESSION_METHODS: frozenset[str] = frozenset(
    {
        "thread/start",
        "thread/unsubscribe",
    }
)

_FORBIDDEN_PREFIXES: tuple[str, ...] = (
    "account/",
    "turn/",
    "review/",
    "feedback/",
    "process/",
    "thread/realtime/",
)

_FORBIDDEN_METHODS: frozenset[str] = frozenset(
    {
        "thread/resume",
        "thread/fork",
        "thread/shellCommand",
        "thread/inject_items",
        "thread/delete",
        "thread/archive",
    }
)


def _normalize(method: str) -> str:
    normalized = str(method).strip()
    if not normalized:
        raise CodexMethodDenied("Codex RPC method 不能为空。")
    if normalized in _FORBIDDEN_METHODS or normalized.startswith(_FORBIDDEN_PREFIXES):
        raise CodexMethodDenied(f"CWapi 禁止调用 Codex RPC method：{normalized}")
    return normalized


def validate_tool_method(method: str) -> str:
    normalized = _normalize(method)
    if normalized not in TOOL_ONLY_METHODS:
        raise CodexMethodDenied(f"CWapi 未开放 Codex tool method：{normalized}")
    return normalized


def validate_internal_method(method: str) -> str:
    normalized = _normalize(method)
    if normalized not in INTERNAL_SESSION_METHODS:
        raise CodexMethodDenied(f"CWapi 未开放 Codex internal method：{normalized}")
    return normalized
