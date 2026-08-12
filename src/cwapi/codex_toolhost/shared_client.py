from __future__ import annotations

import atexit
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
import threading
from typing import Any, Sequence

from .client import CodexToolhostError, NotificationHandler
from .supervisor import CodexToolhostSnapshot, CodexToolhostSupervisor


@dataclass(frozen=True)
class _SupervisorKey:
    executable_path: str
    codex_home: str
    stderr_log_path: str
    allowed_environment_variables: tuple[str, ...]
    startup_timeout_seconds: int


@dataclass(frozen=True)
class LeasedRuntimeProvenance:
    base: Any
    snapshot: CodexToolhostSnapshot

    def __getattr__(self, name: str) -> Any:
        return getattr(self.base, name)

    def receipt_fields(self) -> dict[str, Any]:
        fields = (
            dict(self.base.receipt_fields())
            if self.base is not None and hasattr(self.base, "receipt_fields")
            else {}
        )
        fields.update(
            {
                "codex_app_server_pid": self.snapshot.pid,
                "codex_app_server_generation": self.snapshot.generation,
                "codex_app_server_startup_count": self.snapshot.startup_count,
            }
        )
        return fields


_registry_lock = threading.RLock()
_registry: dict[_SupervisorKey, CodexToolhostSupervisor] = {}


def _key(
    *,
    executable_path: Path,
    codex_home: Path,
    stderr_log_path: Path,
    allowed_environment_variables: Sequence[str],
    startup_timeout_seconds: int,
) -> _SupervisorKey:
    return _SupervisorKey(
        executable_path=str(executable_path.resolve()),
        codex_home=str(codex_home.resolve()),
        stderr_log_path=str(stderr_log_path.resolve()),
        allowed_environment_variables=tuple(allowed_environment_variables),
        startup_timeout_seconds=int(startup_timeout_seconds),
    )


def _get_supervisor(
    *,
    executable_path: Path,
    codex_home: Path,
    stderr_log_path: Path,
    allowed_environment_variables: Sequence[str],
    startup_timeout_seconds: int,
) -> CodexToolhostSupervisor:
    registry_key = _key(
        executable_path=executable_path,
        codex_home=codex_home,
        stderr_log_path=stderr_log_path,
        allowed_environment_variables=allowed_environment_variables,
        startup_timeout_seconds=startup_timeout_seconds,
    )
    with _registry_lock:
        supervisor = _registry.get(registry_key)
        if supervisor is None or supervisor.snapshot().state == "stopped":
            config = SimpleNamespace(
                executable_path=Path(registry_key.executable_path),
                home_path=Path(registry_key.codex_home),
                stderr_log_path=Path(registry_key.stderr_log_path),
                startup_timeout_seconds=registry_key.startup_timeout_seconds,
            )
            supervisor = CodexToolhostSupervisor(
                config=config,
                allowed_environment_variables=(
                    registry_key.allowed_environment_variables
                ),
            )
            _registry[registry_key] = supervisor
        return supervisor


def close_all_shared_codex_toolhosts() -> None:
    with _registry_lock:
        supervisors = list(_registry.values())
        _registry.clear()
    for supervisor in supervisors:
        try:
            supervisor.close()
        except Exception:
            pass


def release_shared_task_resources(task_id: str) -> None:
    """Release task-bound MCP threads without starting a stopped Toolhost."""

    if not task_id:
        return
    with _registry_lock:
        supervisors = list(_registry.values())
    for supervisor in supervisors:
        operation_lock = getattr(supervisor, "_operation_lock")
        state_lock = getattr(supervisor, "_state_lock")
        with operation_lock:
            with state_lock:
                client = getattr(supervisor, "_client", None)
            if client is None:
                continue
            releaser = getattr(client, "release_task_threads", None)
            if callable(releaser):
                try:
                    releaser(task_id)
                except Exception:
                    # Terminal state persistence must not be rolled back because
                    # an already-failing cleanup RPC could not unsubscribe.
                    pass


def shared_codex_toolhost_snapshots() -> list[CodexToolhostSnapshot]:
    with _registry_lock:
        return [supervisor.snapshot() for supervisor in _registry.values()]


atexit.register(close_all_shared_codex_toolhosts)


class SharedCodexClientLease:
    """Compatibility client whose close releases a lease, not the app-server.

    Existing execution code creates a client per step and reliably calls close
    in a finally block. Keeping that public contract lets the mainline recover
    without a wide unsafe rewrite: start lazily borrows the process-scoped
    supervisor, and close releases the serialized operation while leaving the
    healthy app-server alive for the next step.
    """

    def __init__(
        self,
        *,
        executable_path: Path,
        codex_home: Path,
        stderr_log_path: Path,
        allowed_environment_variables: Sequence[str],
        startup_timeout_seconds: int = 30,
        notification_handler: NotificationHandler | None = None,
    ) -> None:
        self.executable_path = executable_path
        self.codex_home = codex_home
        self.stderr_log_path = stderr_log_path
        self.allowed_environment_variables = tuple(allowed_environment_variables)
        self.startup_timeout_seconds = startup_timeout_seconds
        self.notification_handler = notification_handler
        self._supervisor = _get_supervisor(
            executable_path=executable_path,
            codex_home=codex_home,
            stderr_log_path=stderr_log_path,
            allowed_environment_variables=allowed_environment_variables,
            startup_timeout_seconds=startup_timeout_seconds,
        )
        self._operation_context: Any | None = None
        self._client: Any | None = None
        self._closed = False

    def _ensure_client(self) -> Any:
        if self._closed:
            raise CodexToolhostError("Codex client lease 已关闭。")
        if self._client is None:
            context = self._supervisor.operation(
                notification_handler=self.notification_handler
            )
            client = context.__enter__()
            self._operation_context = context
            self._client = client
        return self._client

    def start(self) -> None:
        self._ensure_client()

    @property
    def runtime_provenance(self):
        base = self._ensure_client().runtime_provenance
        if base is None:
            return None
        return LeasedRuntimeProvenance(
            base=base,
            snapshot=self._supervisor.snapshot(),
        )

    @property
    def stderr_tail(self) -> str:
        return str(self._ensure_client().stderr_tail)

    def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        context = self._operation_context
        client = self._client
        self._operation_context = None
        self._client = None

        if client is not None:
            handles = getattr(client, "_command_handles", {})
            if handles:
                try:
                    client.close()
                except Exception:
                    pass
        if context is not None:
            context.__exit__(None, None, None)

    def __getattr__(self, name: str) -> Any:
        if name.startswith("__"):
            raise AttributeError(name)
        return getattr(self._ensure_client(), name)

    def __enter__(self) -> "SharedCodexClientLease":
        self.start()
        return self

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self.close()


__all__ = [
    "LeasedRuntimeProvenance",
    "SharedCodexClientLease",
    "close_all_shared_codex_toolhosts",
    "release_shared_task_resources",
    "shared_codex_toolhost_snapshots",
]
