from .session_client_impl import _mcp_error_message
from .shared_client import SharedCodexClientLease

CodexSessionCapabilityClient = SharedCodexClientLease

__all__ = ["CodexSessionCapabilityClient", "_mcp_error_message"]
