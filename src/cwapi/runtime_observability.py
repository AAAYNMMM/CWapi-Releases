from __future__ import annotations

from dataclasses import asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .codex_toolhost.shared_client import shared_codex_toolhost_snapshots
from .execution.live_logs import LIVE_LOGS
from .git import GitRuntimeError, resolve_configured_git
from .state.runtime_store import RuntimeStateStore


LAMP_PRESENTATION: dict[str, dict[str, str]] = {
    "unavailable": {"color": "gray", "animation": "static", "fill": "empty"},
    "healthy": {"color": "green", "animation": "static", "fill": "solid"},
    "working": {"color": "green", "animation": "chase", "fill": "solid"},
    "waiting": {"color": "blue", "animation": "chase", "fill": "solid"},
    "retrying": {"color": "yellow", "animation": "chase", "fill": "solid"},
    "warning": {"color": "yellow", "animation": "static", "fill": "solid"},
    "failed": {"color": "red", "animation": "static", "fill": "solid"},
    "stopped": {"color": "gray", "animation": "static", "fill": "solid"},
}


def _component(
    name: str,
    state: str,
    *,
    detail: str = "",
    enabled: bool = True,
    **extra: Any,
) -> dict[str, Any]:
    state = state if state in LAMP_PRESENTATION else "unavailable"
    return {
        "name": name,
        "state": state,
        "enabled": enabled,
        "detail": detail,
        "lamp": dict(LAMP_PRESENTATION[state]),
        **extra,
    }


def _current_task(store: Any) -> dict[str, Any] | None:
    try:
        tasks = store.list_tasks(200)
    except Exception:
        return None
    for task in tasks:
        if str(task.get("execution_status") or "") in {"claimed", "running"}:
            return dict(task)
    return None



def build_runtime_snapshot(service: Any) -> dict[str, Any]:
    store = service.runner.store
    current = _current_task(store)
    stopping = bool(service.stop_event.is_set())
    heartbeat = (
        store.get_heartbeat(service.config.runner.runner_id)
        if isinstance(store, RuntimeStateStore)
        else None
    )
    heartbeat_state = str((heartbeat or {}).get("status") or "")
    if stopping:
        runner_state = "stopped"
    elif current is not None:
        runner_state = "working"
    elif heartbeat_state == "degraded":
        runner_state = "warning"
    elif heartbeat_state in {"starting"}:
        runner_state = "waiting"
    else:
        runner_state = "healthy"

    transport = service.transport_runtime.snapshot()
    transport_state = str(transport.state or "").casefold()
    if transport.authorization_required:
        gmail_state = "warning"
        gmail_detail = "authorization_required"
    elif transport_state in {"backoff", "retrying"}:
        gmail_state = "retrying"
        gmail_detail = transport_state
    elif transport_state in {"degraded"}:
        gmail_state = "warning"
        gmail_detail = transport_state
    elif transport_state in {"failed", "error"}:
        gmail_state = "failed"
        gmail_detail = transport_state
    elif transport_state in {"starting"}:
        gmail_state = "waiting"
        gmail_detail = transport_state
    elif transport.pid is not None:
        gmail_state = "working" if current is not None else "healthy"
        gmail_detail = transport_state or "healthy"
    else:
        gmail_state = "unavailable"
        gmail_detail = transport_state or "stopped"

    codex_enabled = bool(getattr(service.config.codex_toolhost, "enabled", False))
    snapshots = shared_codex_toolhost_snapshots() if codex_enabled else []
    if not codex_enabled:
        codex = _component("Codex", "unavailable", detail="disabled", enabled=False)
    elif snapshots:
        snapshot = snapshots[-1]
        raw_state = str(snapshot.state).casefold()
        if raw_state in {"failed", "error"}:
            state = "failed"
        elif raw_state in {"starting", "restarting"}:
            state = "waiting"
        elif getattr(snapshot, "active_operations", 0):
            state = "working"
        elif raw_state in {"running", "ready", "healthy"}:
            state = "healthy"
        else:
            state = "warning"
        codex = _component(
            "Codex",
            state,
            detail=raw_state,
            pid=snapshot.pid,
            generation=snapshot.generation,
            startup_count=snapshot.startup_count,
            active_operations=getattr(snapshot, "active_operations", 0),
        )
    else:
        executable = Path(service.config.codex_toolhost.executable_path)
        codex = _component(
            "Codex",
            "stopped" if executable.is_file() else "unavailable",
            detail="idle_not_started" if executable.is_file() else "runtime_missing",
        )

    try:
        git_runtime = getattr(service.runner, "git_runtime", None)
        if git_runtime is None:
            git_runtime = resolve_configured_git(getattr(service.config, "git", object()))
    except (AttributeError, GitRuntimeError):
        git_runtime = None
    git = _component(
        "Git",
        "working" if current is not None else ("healthy" if git_runtime else "unavailable"),
        detail=(
            "task_workspace_active"
            if current is not None
            else (
                "private_runtime"
                if git_runtime and git_runtime.private
                else ("available" if git_runtime else "not_found")
            )
        ),
        executable=str(git_runtime.executable) if git_runtime else None,
        private=bool(git_runtime and git_runtime.private),
    )

    paths = [
        Path(service.config.state.database_path).parent,
        Path(service.config.storage.logs_path),
        Path(service.config.storage.results_path),
    ]
    storage_ok = True
    for path in paths:
        try:
            path.mkdir(parents=True, exist_ok=True)
            probe = path / ".cwapi-health-probe"
            probe.write_text("ok", encoding="utf-8")
            probe.unlink(missing_ok=True)
        except OSError:
            storage_ok = False
            break
    storage = _component(
        "Storage",
        "healthy" if storage_ok else "failed",
        detail="writable" if storage_ok else "not_writable",
    )

    drive_path = getattr(service.config.storage, "drive_sync_path", None)
    if drive_path is None:
        drive = _component("Drive", "unavailable", detail="not_configured", enabled=False)
    else:
        drive_path = Path(drive_path)
        drive = _component(
            "Drive",
            "healthy" if drive_path.exists() else "warning",
            detail="available" if drive_path.exists() else "path_missing",
            enabled=True,
        )

    components = {
        "runner": _component(
            "Runner",
            runner_state,
            detail=heartbeat_state or ("stopping" if stopping else "running"),
            task_id=(str(current.get("task_id")) if current else None),
        ),
        "gmail": _component(
            "Gmail",
            gmail_state,
            detail=gmail_detail,
            authorization_required=bool(transport.authorization_required),
            transport=asdict(transport),
        ),
        "codex": codex,
        "git": git,
        "storage": storage,
        "drive": drive,
    }
    severity = {"failed": 4, "warning": 3, "retrying": 2, "waiting": 1}
    enabled_states = [item["state"] for item in components.values() if item["enabled"]]
    overall = "healthy"
    if enabled_states:
        worst = max(enabled_states, key=lambda state: severity.get(state, 0))
        if severity.get(worst, 0) >= 4:
            overall = "failed"
        elif severity.get(worst, 0) >= 2:
            overall = "degraded"
        elif current is not None:
            overall = "working"
    return {
        "schema": "cwapi.runtime.state.v1",
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "overall": overall,
        "current_task_id": str(current.get("task_id")) if current else None,
        "components": components,
    }


def _runner_memory_stream(service: Any, tail_bytes: int) -> list[dict[str, Any]]:
    console = getattr(service, "console", None)
    if console is not None and hasattr(console, "memory_snapshot"):
        sequence, text = console.memory_snapshot(tail_bytes=tail_bytes)
    else:
        sequence, text = LIVE_LOGS.runtime_text(tail_bytes=tail_bytes)
    if not text:
        return []
    encoded_size = len(text.encode("utf-8", errors="replace"))
    return [
        {
            "id": f"stream:runner:memory:{sequence}",
            "step_id": "",
            "stream": "runner",
            "path": str(Path(service.config.storage.logs_path) / "runtime" / "runner.log"),
            "size_bytes": encoded_size,
            "start_offset": 0,
            "end_offset": encoded_size,
            "text": text,
            "truncated": False,
            "memory": True,
        }
    ]


def build_execution_snapshot(
    service: Any,
    *,
    task_id: str | None = None,
    limit: int = 250,
    tail_bytes: int = 16 * 1024,
) -> dict[str, Any]:
    """Build the GUI live snapshot without reading any log/trace files.

    SQLite remains the durable task-state store. Human-readable live execution
    logs and Runner output are process-local memory and disappear on restart.
    """
    limit = max(20, min(int(limit), 500))
    tail_bytes = max(1024, min(int(tail_bytes), 256 * 1024))
    store = service.runner.store
    streams = _runner_memory_stream(service, tail_bytes)

    if task_id is None:
        selected = _current_task(store)
        task_id = str(selected.get("task_id")) if selected else None
    if not task_id:
        return {
            "schema": "cwapi.execution.live.v1",
            "task_id": None,
            "events": [],
            "streams": streams,
            "trace_current": None,
            "bounded": {"max_events": limit, "tail_bytes": tail_bytes},
        }

    task = store.get_task(task_id)
    if task is None:
        return {
            "schema": "cwapi.execution.live.v1",
            "task_id": task_id,
            "events": [],
            "streams": streams,
            "trace_current": None,
            "bounded": {"max_events": limit, "tail_bytes": tail_bytes},
        }

    events: list[dict[str, Any]] = []
    if isinstance(store, RuntimeStateStore):
        for item in store.list_events(task_id=task_id, limit=limit):
            events.append(
                {
                    "id": f"transport:{item['id']}",
                    "source": "runner",
                    "type": str(item["message_type"]).lower(),
                    "status": item["status"],
                    "timestamp": item["created_at"],
                    "message": item.get("error_message")
                    or f"{item['message_type']} {item['status']}",
                    "data": item,
                }
            )

    # Step/function lifecycle records are intentionally sourced only from RAM.
    events.extend(LIVE_LOGS.structured_events(task_id, limit=limit))
    events.sort(key=lambda item: str(item.get("timestamp") or ""))
    return {
        "schema": "cwapi.execution.live.v1",
        "task_id": task_id,
        "task": task,
        "events": events[-limit:],
        "streams": streams,
        "trace_current": LIVE_LOGS.current_function(task_id),
        "bounded": {"max_events": limit, "tail_bytes": tail_bytes},
    }
