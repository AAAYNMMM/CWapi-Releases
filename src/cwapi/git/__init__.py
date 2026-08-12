from . import repository_manager as _repository_manager_module
from .reliable_repository_manager import ReliableRepositoryManager
from .runtime import GitRuntime, GitRuntimeError, resolve_configured_git, resolve_git_runtime

RepositoryError = _repository_manager_module.RepositoryError
WorkspaceLease = _repository_manager_module.WorkspaceLease
_repository_manager_module.RepositoryManager = ReliableRepositoryManager
RepositoryManager = ReliableRepositoryManager

# Backward-compatible public name used by early Phase 2 imports.
Workspace = WorkspaceLease

__all__ = [
    "RepositoryError",
    "RepositoryManager",
    "GitRuntime",
    "GitRuntimeError",
    "Workspace",
    "WorkspaceLease",
    "resolve_configured_git",
    "resolve_git_runtime",
]
