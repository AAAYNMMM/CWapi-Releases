from __future__ import annotations

import argparse
from dataclasses import asdict
from datetime import datetime, timezone
import hmac
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import threading
from typing import Any
from urllib.parse import parse_qs, unquote, urlparse

from .config import AppConfig, load_config
from .desktop_management import (
    DesktopManagementError,
    apply_pending_settings,
    doctor_snapshot,
    gmail_status,
    has_pending_settings,
    remove_gmail_authorization,
    run_maintenance,
    save_settings,
    settings_snapshot,
    validate_settings,
)
from .gpt_facade import GPTFacade
from .portable_release import prepare_portable_runtime
from .result_details import ResultDetailError
from .security import safe_error
from .runtime_observability import build_execution_snapshot, build_runtime_snapshot
from .task_builder import TaskBuildError
from .workbench import build_workbench_snapshot
from .service import RunnerService
from .state.runtime_store import RuntimeStateStore
from .transports.gmail_drafts import clear_managed_go_client


_MAX_BODY_BYTES = 1024 * 1024
_MAX_TASK_RESULTS = 200


class DesktopAPIError(RuntimeError):
    pass


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _config_snapshot(config: AppConfig) -> dict[str, Any]:
    return {
        "runner": {
            "id": config.runner.runner_id,
            "channel_ids": list(config.runner.channel_ids),
            "poll_interval_seconds": config.runner.poll_interval_seconds,
            "cancel_poll_seconds": config.runner.cancel_poll_seconds,
            "progress_mode": config.runner.progress_mode,
        },
        "gmail": {
            "account": config.gmail.account,
            "credentials_present": config.gmail.credentials_path.is_file(),
            "token_present": config.gmail.token_path.is_file(),
        },
        "runtime": {
            "enabled": config.runtime.enabled,
            "publish_progress": config.runtime.publish_progress,
            "collect_artifacts": config.runtime.collect_artifacts,
            "enable_cancel_drafts": config.runtime.enable_cancel_drafts,
        },
        "projects": sorted(config.projects.settings),
        "security": {
            "allowed_repositories": sorted(config.security.allowed_repositories),
            "allowed_actions": sorted(config.security.allowed_actions),
            "allowed_environment_variables": list(config.security.allowed_environment_variables),
        },
    }


class DesktopControlServer:
    """Authenticated loopback control/read API for the Tauri desktop shell.

    The server deliberately exposes Runner public state through Python instead of
    letting the GUI open SQLite. The temporary IPC secret is supplied by the Rust
    desktop process and is never included in snapshots or readiness output.
    """

    def __init__(
        self,
        *,
        service: RunnerService,
        config_path: Path,
        secret: str,
        listen: str = "127.0.0.1:0",
    ) -> None:
        if len(secret) < 32:
            raise DesktopAPIError("desktop IPC secret is missing or too short")
        host, separator, raw_port = listen.rpartition(":")
        if not separator or host not in {"127.0.0.1", "localhost"}:
            raise DesktopAPIError("desktop API must bind to IPv4 loopback")
        try:
            port = int(raw_port)
        except ValueError as exc:
            raise DesktopAPIError("desktop API port is invalid") from exc
        if port < 0 or port > 65535:
            raise DesktopAPIError("desktop API port is out of range")

        self.service = service
        self.config_path = config_path.resolve()
        self.secret = secret
        self.started_at = _utc_now()
        self.gpt = GPTFacade(service, transport_name="desktop_api")
        self._thread: threading.Thread | None = None
        handler = self._handler_type()
        self._server = ThreadingHTTPServer((host, port), handler)
        self._server.daemon_threads = True

    @property
    def url(self) -> str:
        host, port = self._server.server_address[:2]
        return f"http://{host}:{port}"

    def readiness(self) -> dict[str, Any]:
        return {
            "schema": "cwapi.desktop.ready.v1",
            "url": self.url,
            "pid": os.getpid(),
            "config_path": str(self.config_path),
        }

    def start(self) -> None:
        if self._thread is not None:
            return
        self._thread = threading.Thread(
            target=self._server.serve_forever,
            kwargs={"poll_interval": 0.2},
            name="cwapi-desktop-api",
            daemon=True,
        )
        self._thread.start()

    def close(self) -> None:
        thread = self._thread
        self._thread = None
        if thread is not None:
            self._server.shutdown()
            thread.join(timeout=5)
        self._server.server_close()

    def health(self) -> dict[str, Any]:
        transport = asdict(self.service.transport_runtime.snapshot())
        return {
            "schema": "cwapi.desktop.health.v1",
            "status": "stopping" if self.service.stop_event.is_set() else "healthy",
            "pid": os.getpid(),
            "runner_id": self.service.config.runner.runner_id,
            "started_at": self.started_at,
            "transport": transport,
        }

    def processes(self) -> list[dict[str, Any]]:
        transport = self.service.transport_runtime.snapshot()
        result = [
            {
                "pid": os.getpid(),
                "component": "python-backend",
                "owner": "cwapi-desktop",
                "parent_pid": os.getppid(),
                "task_id": None,
                "started_at": self.started_at,
                "state": "running",
            }
        ]
        if transport.pid is not None:
            result.append(
                {
                    "pid": transport.pid,
                    "component": "go-transport",
                    "owner": "python-backend",
                    "parent_pid": os.getpid(),
                    "task_id": None,
                    "started_at": self.started_at,
                    "state": transport.state,
                }
            )
        return result

    def tasks(self, limit: int) -> list[dict[str, Any]]:
        bounded = max(1, min(int(limit), _MAX_TASK_RESULTS))
        store = self.service.runner.store
        rows = [dict(item) for item in store.list_tasks(bounded)]
        workspace_by_task: dict[str, dict[str, Any]] = {}
        if isinstance(store, RuntimeStateStore):
            for workspace in store.list_workspaces(max(200, bounded)):
                workspace_by_task.setdefault(str(workspace.get("task_id") or ""), dict(workspace))
        enriched: list[dict[str, Any]] = []
        for row in rows:
            task_id = str(row.get("task_id") or "")
            detail = store.get_task(task_id) or {}
            payload = None
            raw_task = detail.get("task_json") if isinstance(detail, dict) else None
            if raw_task:
                try:
                    parsed = json.loads(str(raw_task))
                    payload = parsed if isinstance(parsed, dict) else None
                except json.JSONDecodeError:
                    payload = None
            steps = payload.get("steps", []) if isinstance(payload, dict) else []
            actions = [
                str(step.get("action"))
                for step in steps
                if isinstance(step, dict) and step.get("action")
            ]
            workspace = workspace_by_task.get(task_id, {})
            enriched.append(
                {
                    **row,
                    "actual_commit": workspace.get("actual_commit"),
                    "progress_status": detail.get("progress_status")
                    if isinstance(detail, dict)
                    else None,
                    "source_draft_id": detail.get("source_draft_id")
                    if isinstance(detail, dict)
                    else None,
                    "workspace_path": detail.get("workspace_path")
                    if isinstance(detail, dict)
                    else None,
                    "artifact_path": detail.get("artifact_path")
                    if isinstance(detail, dict)
                    else None,
                    "cancel_requested": bool(detail.get("cancel_requested"))
                    if isinstance(detail, dict)
                    else False,
                    "actions": actions,
                    "step_count": len(steps),
                }
            )
        return enriched

    def task_detail(self, task_id: str) -> dict[str, Any] | None:
        store = self.service.runner.store
        task = store.get_task(task_id)
        if task is None:
            return None
        outbox = store.get_outbox_result(task_id)
        payload: dict[str, Any] = {
            "task": task,
            "steps": store.get_task_steps(task_id),
            "outbox": outbox,
        }
        raw_task = task.get("task_json") if isinstance(task, dict) else None
        if raw_task:
            try:
                parsed_task = json.loads(str(raw_task))
                payload["task_payload"] = parsed_task if isinstance(parsed_task, dict) else None
            except json.JSONDecodeError:
                payload["task_payload"] = None
        if isinstance(outbox, dict) and outbox.get("payload_json"):
            try:
                payload["result_payload"] = json.loads(str(outbox["payload_json"]))
            except json.JSONDecodeError:
                payload["result_payload"] = None
        if isinstance(store, RuntimeStateStore):
            payload["artifact"] = store.get_artifact(task_id)
            payload["workspaces"] = [
                item for item in store.list_workspaces() if item["task_id"] == task_id
            ]
        return payload

    def submit_task(self, body: dict[str, Any]) -> dict[str, Any]:
        try:
            return self.gpt.create_task(
                body,
                task_id_prefix="GUI",
                strict_request=False,
            )
        except TaskBuildError as exc:
            raise DesktopAPIError(str(exc)) from exc

    def submit_gpt_task(self, body: dict[str, Any]) -> dict[str, Any]:
        try:
            return self.gpt.create_task(body)
        except TaskBuildError as exc:
            raise DesktopAPIError(str(exc)) from exc

    def current_execution(self) -> dict[str, Any] | None:
        for task in self.tasks(_MAX_TASK_RESULTS):
            if task.get("execution_status") in {"claimed", "running"}:
                return self.task_detail(str(task["task_id"]))
        return None

    def runtime_state(self) -> dict[str, Any]:
        return build_runtime_snapshot(self.service)

    def workbench(self) -> dict[str, Any]:
        return build_workbench_snapshot(self.service, config_path=self.config_path)

    def management(self) -> dict[str, Any]:
        return settings_snapshot(self.service, config_path=self.config_path)

    def validate_settings_payload(self, body: dict[str, Any]) -> dict[str, Any]:
        kind = str(body.get("kind") or "").strip()
        editable = body.get("editable")
        if not isinstance(editable, dict):
            raise DesktopAPIError("editable must be an object")
        return validate_settings(
            self.service, config_path=self.config_path, kind=kind, editable=editable
        )

    def save_settings_payload(self, body: dict[str, Any]) -> dict[str, Any]:
        kind = str(body.get("kind") or "").strip()
        revision = str(body.get("revision") or "").strip()
        editable = body.get("editable")
        if not revision or not isinstance(editable, dict):
            raise DesktopAPIError("revision and editable are required")
        return save_settings(
            self.service,
            config_path=self.config_path,
            kind=kind,
            revision=revision,
            editable=editable,
        )

    def doctor(self) -> dict[str, Any]:
        return doctor_snapshot(self.service, config_path=self.config_path)

    def maintenance(self, body: dict[str, Any]) -> dict[str, Any]:
        action = str(body.get("action") or "").strip()
        task_id = str(body.get("task_id") or "").strip() or None
        return run_maintenance(
            self.service, config_path=self.config_path, action=action, task_id=task_id
        )

    def gmail_auth_status(self) -> dict[str, Any]:
        return gmail_status(self.service)

    def remove_gmail_auth(self) -> dict[str, Any]:
        return remove_gmail_authorization(self.service, config_path=self.config_path)

    def execution_events(
        self, *, task_id: str | None, limit: int, tail_bytes: int
    ) -> dict[str, Any]:
        return build_execution_snapshot(
            self.service, task_id=task_id, limit=limit, tail_bytes=tail_bytes
        )

    def reveal_roots(self) -> dict[str, Any]:
        roots = {Path(self.service.config.state.database_path).resolve().parent.parent}
        for project in self.service.config.projects.settings.values():
            roots.add(Path(project.path).resolve())
        if self.service.config.storage.drive_sync_path is not None:
            roots.add(Path(self.service.config.storage.drive_sync_path).resolve())
        return {"roots": [str(path) for path in sorted(roots, key=lambda item: str(item).casefold())]}

    def validate_config(self) -> dict[str, Any]:
        config = load_config(self.config_path)
        return {
            "valid": True,
            "config_path": str(self.config_path),
            "snapshot": _config_snapshot(config),
        }

    def cancel_task(self, task_id: str, reason: str) -> dict[str, Any]:
        store = self.service.runner.store
        if not isinstance(store, RuntimeStateStore):
            raise DesktopAPIError("runtime state is not enabled")
        if store.get_task(task_id) is None:
            raise KeyError(task_id)
        store.request_cancel_local(
            task_id=task_id,
            reason=(reason.strip() or "Cancelled from CWapi desktop.")[:500],
        )
        return {"accepted": True, "task_id": task_id}

    def authorize_gmail(self) -> dict[str, Any]:
        self.service.runner.transport.authorize()
        return {
            "authorized": True,
            "account": self.service.config.gmail.account,
            "token_present": self.service.config.gmail.token_path.is_file(),
        }

    def request_application_shutdown(self) -> dict[str, Any]:
        interrupted_task_id: str | None = None
        store = self.service.runner.store
        if isinstance(store, RuntimeStateStore):
            current = self.current_execution()
            task = current.get("task") if isinstance(current, dict) else None
            if isinstance(task, dict) and task.get("task_id"):
                interrupted_task_id = str(task["task_id"])
                store.request_cancel_local(
                    task_id=interrupted_task_id,
                    reason="APPLICATION_SHUTDOWN",
                )
                store.record_event(
                    task_id=interrupted_task_id,
                    direction="internal",
                    message_type="SHUTDOWN",
                    external_id=None,
                    status="requested",
                    error_message="APPLICATION_SHUTDOWN",
                )
        self.service.request_stop()
        return {
            "accepted": True,
            "reason": "APPLICATION_SHUTDOWN",
            "interrupted_task_id": interrupted_task_id,
        }

    def _handler_type(self) -> type[BaseHTTPRequestHandler]:
        application = self

        class Handler(BaseHTTPRequestHandler):
            server_version = "CWapiDesktop/1"

            def log_message(self, _format: str, *_args: object) -> None:
                return None

            def _write(self, status: HTTPStatus, payload: Any) -> None:
                raw = json.dumps(payload, ensure_ascii=False).encode("utf-8")
                self.send_response(int(status))
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(raw)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.wfile.write(raw)

            def _error(self, status: HTTPStatus, message: str) -> None:
                self._write(status, {"error": message})

            def _authorized(self) -> bool:
                supplied = self.headers.get("Authorization", "")
                expected = f"Bearer {application.secret}"
                if hmac.compare_digest(supplied, expected):
                    return True
                self._error(HTTPStatus.UNAUTHORIZED, "invalid desktop IPC secret")
                return False

            def _body(self) -> dict[str, Any]:
                try:
                    length = int(self.headers.get("Content-Length", "0"))
                except ValueError as exc:
                    raise DesktopAPIError("invalid Content-Length") from exc
                if length < 0 or length > _MAX_BODY_BYTES:
                    raise DesktopAPIError("request body is too large")
                raw = self.rfile.read(length) if length else b"{}"
                try:
                    value = json.loads(raw.decode("utf-8"))
                except (UnicodeDecodeError, json.JSONDecodeError) as exc:
                    raise DesktopAPIError("invalid JSON body") from exc
                if not isinstance(value, dict):
                    raise DesktopAPIError("JSON body must be an object")
                return value

            def do_GET(self) -> None:  # noqa: N802
                if not self._authorized():
                    return
                parsed = urlparse(self.path)
                try:
                    if parsed.path == "/health":
                        self._write(HTTPStatus.OK, application.health())
                        return
                    if parsed.path == "/v1/gpt/contract":
                        self._write(HTTPStatus.OK, application.gpt.contract())
                        return
                    if parsed.path == "/v1/gpt/context":
                        repository = parse_qs(parsed.query).get("repository", [None])[0]
                        self._write(
                            HTTPStatus.OK,
                            application.gpt.context(repository=repository),
                        )
                        return
                    if parsed.path == "/v1/gpt/actions":
                        self._write(HTTPStatus.OK, application.gpt.actions())
                        return
                    if parsed.path.startswith("/v1/gpt/actions/"):
                        action = unquote(parsed.path.removeprefix("/v1/gpt/actions/"))
                        try:
                            detail = application.gpt.action(action)
                        except KeyError:
                            self._error(HTTPStatus.NOT_FOUND, "action not found")
                        else:
                            self._write(HTTPStatus.OK, detail)
                        return
                    if parsed.path == "/v1/gpt/presets":
                        self._write(HTTPStatus.OK, application.gpt.presets())
                        return
                    if parsed.path.startswith("/v1/gpt/presets/"):
                        preset = unquote(parsed.path.removeprefix("/v1/gpt/presets/"))
                        try:
                            detail = application.gpt.preset(preset)
                        except KeyError:
                            self._error(HTTPStatus.NOT_FOUND, "preset not found")
                        else:
                            self._write(HTTPStatus.OK, detail)
                        return
                    if parsed.path.startswith("/v1/gpt/results/"):
                        suffix = parsed.path.removeprefix("/v1/gpt/results/")
                        if suffix.endswith("/summary"):
                            task_id = unquote(suffix.removesuffix("/summary"))
                            try:
                                result = application.gpt.result_summary(task_id)
                            except ResultDetailError as exc:
                                status = (
                                    HTTPStatus.NOT_FOUND
                                    if exc.category == "not_found"
                                    else HTTPStatus.CONFLICT
                                )
                                self._error(status, safe_error(exc, limit=300))
                            else:
                                self._write(HTTPStatus.OK, result)
                            return
                        task_id = unquote(suffix)
                        parameters = parse_qs(parsed.query)
                        detail_name = parameters.get("detail", [""])[0]
                        step_id = parameters.get("step_id", [None])[0]
                        try:
                            result = application.gpt.result_detail(
                                task_id,
                                detail=detail_name,
                                step_id=step_id,
                            )
                        except ResultDetailError as exc:
                            status = (
                                HTTPStatus.NOT_FOUND
                                if exc.category == "not_found"
                                else HTTPStatus.BAD_REQUEST
                            )
                            self._error(status, safe_error(exc, limit=300))
                        else:
                            self._write(HTTPStatus.OK, result)
                        return
                    if parsed.path == "/v1/tasks":
                        raw_limit = parse_qs(parsed.query).get("limit", ["50"])[0]
                        self._write(
                            HTTPStatus.OK,
                            {"tasks": application.tasks(int(raw_limit))},
                        )
                        return
                    if parsed.path.startswith("/v1/tasks/"):
                        task_id = unquote(parsed.path.removeprefix("/v1/tasks/"))
                        detail = application.task_detail(task_id)
                        if detail is None:
                            self._error(HTTPStatus.NOT_FOUND, "task not found")
                        else:
                            self._write(HTTPStatus.OK, detail)
                        return
                    if parsed.path == "/v1/execution/current":
                        self._write(HTTPStatus.OK, {"execution": application.current_execution()})
                        return
                    if parsed.path == "/v1/runtime/state":
                        self._write(HTTPStatus.OK, application.runtime_state())
                        return
                    if parsed.path == "/v1/workbench":
                        self._write(HTTPStatus.OK, application.workbench())
                        return
                    if parsed.path == "/v1/management":
                        self._write(HTTPStatus.OK, application.management())
                        return
                    if parsed.path == "/v1/doctor":
                        self._write(HTTPStatus.OK, application.doctor())
                        return
                    if parsed.path == "/v1/auth/gmail/status":
                        self._write(HTTPStatus.OK, application.gmail_auth_status())
                        return
                    if parsed.path == "/v1/execution/events":
                        query = parse_qs(parsed.query)
                        task_id = query.get("task_id", [None])[0]
                        limit = int(query.get("limit", ["250"])[0])
                        tail_bytes = int(query.get("tail_bytes", [str(16 * 1024)])[0])
                        self._write(
                            HTTPStatus.OK,
                            application.execution_events(
                                task_id=task_id, limit=limit, tail_bytes=tail_bytes
                            ),
                        )
                        return
                    if parsed.path == "/v1/reveal-roots":
                        self._write(HTTPStatus.OK, application.reveal_roots())
                        return
                    if parsed.path == "/v1/config":
                        self._write(
                            HTTPStatus.OK,
                            {
                                "config_path": str(application.config_path),
                                "snapshot": _config_snapshot(application.service.config),
                            },
                        )
                        return
                    if parsed.path == "/v1/processes":
                        self._write(
                            HTTPStatus.OK,
                            {"processes": application.processes()},
                        )
                        return
                    self._error(HTTPStatus.NOT_FOUND, "endpoint not found")
                except (DesktopAPIError, DesktopManagementError, ValueError) as exc:
                    self._error(HTTPStatus.BAD_REQUEST, safe_error(exc, limit=300))
                except Exception as exc:
                    self._error(
                        HTTPStatus.INTERNAL_SERVER_ERROR,
                        safe_error(exc, limit=300),
                    )

            def do_POST(self) -> None:  # noqa: N802
                if not self._authorized():
                    return
                parsed = urlparse(self.path)
                try:
                    body = self._body()
                    if parsed.path == "/v1/tasks":
                        self._write(HTTPStatus.ACCEPTED, application.submit_task(body))
                        return
                    if parsed.path == "/v1/gpt/tasks":
                        self._write(
                            HTTPStatus.ACCEPTED,
                            application.submit_gpt_task(body),
                        )
                        return
                    if parsed.path == "/v1/config/validate":
                        if body:
                            self._write(HTTPStatus.OK, application.validate_settings_payload(body))
                        else:
                            self._write(HTTPStatus.OK, application.validate_config())
                        return
                    if parsed.path == "/v1/config/save":
                        self._write(HTTPStatus.OK, application.save_settings_payload(body))
                        return
                    if parsed.path == "/v1/maintenance":
                        self._write(HTTPStatus.OK, application.maintenance(body))
                        return
                    if parsed.path == "/v1/operations/cancel":
                        task_id = str(body.get("task_id") or "").strip()
                        if not task_id:
                            raise DesktopAPIError("task_id is required")
                        try:
                            result = application.cancel_task(
                                task_id,
                                str(body.get("reason") or ""),
                            )
                        except KeyError:
                            self._error(HTTPStatus.NOT_FOUND, "task not found")
                            return
                        self._write(HTTPStatus.ACCEPTED, result)
                        return
                    if parsed.path == "/v1/auth/gmail":
                        if body:
                            raise DesktopAPIError("Gmail authorization accepts no body")
                        self._write(HTTPStatus.OK, application.authorize_gmail())
                        return
                    if parsed.path == "/v1/auth/gmail/remove":
                        if body:
                            raise DesktopAPIError("Gmail authorization removal accepts no body")
                        self._write(HTTPStatus.OK, application.remove_gmail_auth())
                        return
                    if parsed.path == "/v1/shutdown":
                        if body:
                            raise DesktopAPIError("shutdown accepts no body")
                        self._write(
                            HTTPStatus.ACCEPTED,
                            application.request_application_shutdown(),
                        )
                        return
                    self._error(HTTPStatus.NOT_FOUND, "endpoint not found")
                except (DesktopAPIError, DesktopManagementError) as exc:
                    self._error(HTTPStatus.BAD_REQUEST, safe_error(exc, limit=300))
                except Exception as exc:
                    self._error(
                        HTTPStatus.INTERNAL_SERVER_ERROR,
                        safe_error(exc, limit=300),
                    )

        return Handler


def run_desktop_backend(
    *,
    config_path: Path,
    listen: str,
    secret: str,
) -> int:
    prepare_portable_runtime(config_path)
    config = load_config(config_path)
    service = RunnerService(config)
    service.settings_pending = has_pending_settings(config_path)
    service.pending_settings_callback = lambda: apply_pending_settings(
        service, config_path=config_path
    )
    control: DesktopControlServer | None = None
    lock_acquired = False
    runner_entered = False
    try:
        service.runner_lock.acquire()
        lock_acquired = True
        control = DesktopControlServer(
            service=service,
            config_path=config_path,
            secret=secret,
            listen=listen,
        )
        control.start()
        print(json.dumps(control.readiness(), ensure_ascii=False), flush=True)
        runner_entered = True
        service._run_locked()
        return 0
    finally:
        if control is not None:
            control.close()
        if lock_acquired:
            service.runner_lock.release()
        if not runner_entered:
            try:
                go_client = service.runner.transport.go_client
                service.runner.transport.close()
                clear_managed_go_client(go_client)
            finally:
                service.transport_runtime.close()


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="python -m cwapi.desktop_api")
    parser.add_argument("--config", required=True)
    parser.add_argument("--listen", default="127.0.0.1:0")
    return parser


def main() -> None:
    args = _parser().parse_args()
    secret = os.environ.get("CWAPI_DESKTOP_IPC_SECRET", "")
    try:
        code = run_desktop_backend(
            config_path=Path(args.config).expanduser().resolve(),
            listen=args.listen,
            secret=secret,
        )
    except (DesktopAPIError, FileNotFoundError, RuntimeError, ValueError) as exc:
        print(
            json.dumps(
                {
                    "schema": "cwapi.desktop.error.v1",
                    "error": safe_error(exc, limit=500),
                },
                ensure_ascii=False,
            ),
            flush=True,
        )
        code = 1
    raise SystemExit(code)


if __name__ == "__main__":
    main()
