from __future__ import annotations

from functools import wraps
from typing import Any, Callable


def install_workspace_diagnostic_invalidation(client_type: type) -> None:
    if getattr(client_type, "_cwapi_diagnostic_invalidation_installed", False):
        return

    def wrap_method(name: str) -> None:
        original: Callable[..., Any] = getattr(client_type, name)

        @wraps(original)
        def wrapped(self: Any, *args: Any, **kwargs: Any) -> Any:
            lock = getattr(self, "_diagnostic_lock", None)
            cache = getattr(self, "_workspace_status_cache", None)
            if lock is not None and cache is not None:
                with lock:
                    cache.clear()
            return original(self, *args, **kwargs)

        setattr(client_type, name, wrapped)

    for method_name in (
        "mcp_tool_call",
        "fs_write_file",
        "fs_create_directory",
        "fs_remove",
        "fs_copy",
    ):
        wrap_method(method_name)

    setattr(client_type, "_cwapi_diagnostic_invalidation_installed", True)


__all__ = ["install_workspace_diagnostic_invalidation"]
