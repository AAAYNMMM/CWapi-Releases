from . import client as _client_module
from . import runtime_lock as _runtime_lock
from .capability_client import (
    CodexCapabilityClient,
    CodexCapabilityCommandResponse,
    CodexCommandHandle,
    CodexOutputDelta,
)
from .capability_session_client import CodexSessionCapabilityClient
from .client import CodexAppServerClient, CodexCommandResponse, CodexToolhostError
from .policy import (
    CodexMethodDenied,
    INTERNAL_SESSION_METHODS,
    TOOL_ONLY_METHODS,
    validate_internal_method,
    validate_tool_method,
)
from .process_cleanup import close_codex_app_server
from .runtime_provenance_cache import verify_codex_runtime_cached
from .session_client_impl import CodexSessionAppServerClient
from .supervisor import CodexToolhostSnapshot, CodexToolhostSupervisor
from .workspace_diagnostics import install_workspace_diagnostic_invalidation

# Later imports, including the Runner execution backend, receive the cached
# verifier. The raw client already verifies once at app-server startup.
_runtime_lock.verify_codex_runtime = verify_codex_runtime_cached

# Replace the legacy best-effort close path. Dynamic method lookup means the
# long-lived subclass also receives verified process-tree cleanup.
_client_module.CodexAppServerClient.close = close_codex_app_server
install_workspace_diagnostic_invalidation(CodexSessionAppServerClient)

__all__ = [
    "CodexAppServerClient",
    "CodexCommandResponse",
    "CodexToolhostError",
    "CodexCapabilityClient",
    "CodexCapabilityCommandResponse",
    "CodexCommandHandle",
    "CodexOutputDelta",
    "CodexSessionCapabilityClient",
    "CodexToolhostSnapshot",
    "CodexToolhostSupervisor",
    "CodexMethodDenied",
    "TOOL_ONLY_METHODS",
    "INTERNAL_SESSION_METHODS",
    "validate_tool_method",
    "validate_internal_method",
    "verify_codex_runtime_cached",
]
