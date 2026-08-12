from __future__ import annotations

from dataclasses import dataclass
import json
import os
from pathlib import Path
import secrets
import subprocess
import threading
import time
from typing import IO, Any

from ..config import AppConfig
from ..paths import data_root_from_database
from .go_service import GoDraftTransportClient


class GoTransportRuntimeError(RuntimeError):
    pass


@dataclass(frozen=True)
class GoTransportRuntimeSnapshot:
    state: str
    pid: int | None
    url: str | None
    version: str | None
    authentication_enabled: bool
    authorization_required: bool = False


class GoTransportRuntime:
    """Own one private Go Gmail Transport for a Python CWapi runtime session."""

    def __init__(
        self,
        *,
        account: str,
        credentials_path: Path,
        token_path: Path,
        events_path: Path,
        executable_path: Path | None = None,
        startup_timeout_seconds: float = 15.0,
    ) -> None:
        self.account = account
        self.credentials_path = credentials_path
        self.token_path = token_path
        self.events_path = events_path
        self.executable_path = executable_path or discover_go_transport_executable(token_path)
        self.startup_timeout_seconds = startup_timeout_seconds

        self._lock = threading.RLock()
        self._process: subprocess.Popen[str] | None = None
        self._stderr_thread: threading.Thread | None = None
        self._stderr_file: IO[str] | None = None
        self._client: GoDraftTransportClient | None = None
        self._secret: str | None = None
        self._url: str | None = None
        self._version: str | None = None
        self._state = "stopped"

    @classmethod
    def from_config(
        cls,
        config: AppConfig,
        *,
        executable_path: Path | None = None,
    ) -> "GoTransportRuntime":
        return cls(
            account=config.gmail.account,
            credentials_path=config.gmail.credentials_path,
            token_path=config.gmail.token_path,
            events_path=config.storage.logs_path / "runtime" / "go-transport.ndjson",
            executable_path=executable_path,
        )

    @property
    def secret(self) -> str:
        with self._lock:
            if self._secret is None:
                raise GoTransportRuntimeError("Go Transport 尚未启动。")
            return self._secret

    @property
    def url(self) -> str:
        with self._lock:
            if self._url is None:
                raise GoTransportRuntimeError("Go Transport 尚未启动。")
            return self._url

    def snapshot(self) -> GoTransportRuntimeSnapshot:
        with self._lock:
            process = self._process
            pid = process.pid if process is not None and process.poll() is None else None
            state = self._state
            authorization_required = False
            if pid is not None and self._client is not None:
                try:
                    health = self._client.health()
                except Exception:
                    health = None
                if isinstance(health, dict):
                    live_state = str(health.get("status") or "").strip()
                    if live_state:
                        state = live_state
                    authorization_required = bool(health.get("authorization_required", False))
            return GoTransportRuntimeSnapshot(
                state=state,
                pid=pid,
                url=self._url,
                version=self._version,
                authentication_enabled=self._secret is not None,
                authorization_required=authorization_required,
            )

    def start(self) -> GoDraftTransportClient:
        with self._lock:
            if self._process is not None and self._process.poll() is None:
                if self._client is not None:
                    return self._client
                self._client = GoDraftTransportClient(self.url, secret=self.secret)
                return self._client
            if not self.executable_path.is_file():
                raise GoTransportRuntimeError(
                    f"找不到受管 Go Transport：{self.executable_path}"
                )

            self.events_path.parent.mkdir(parents=True, exist_ok=True)
            self.token_path.parent.mkdir(parents=True, exist_ok=True)
            self._secret = secrets.token_urlsafe(32)
            environment = dict(os.environ)
            environment["CWAPI_TRANSPORT_SECRET"] = self._secret
            command = [
                str(self.executable_path),
                "--listen",
                "127.0.0.1:0",
                "--account",
                self.account,
                "--credentials",
                str(self.credentials_path),
                "--token",
                str(self.token_path),
                "--events-file",
                str(self.events_path),
            ]
            creationflags = (
                getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
                if os.name == "nt"
                else 0
            )
            self._state = "starting"
            try:
                process = subprocess.Popen(
                    command,
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    bufsize=1,
                    shell=False,
                    env=environment,
                    creationflags=creationflags,
                    start_new_session=os.name != "nt",
                )
            except OSError as exc:
                self._state = "failed"
                self._secret = None
                raise GoTransportRuntimeError(
                    f"启动 Go Transport 失败：{exc}"
                ) from exc
            self._process = process
            assert process.stdout is not None
            assert process.stderr is not None
            self._start_stderr_capture(process.stderr)

            try:
                ready = self._read_ready(process)
                self._url = str(ready["url"])
                self._version = str(ready["version"])
                client = GoDraftTransportClient(self._url, secret=self._secret)
                health = client.health()
                if health.get("status") not in {
                    "healthy",
                    "backoff",
                    "degraded",
                    "authorization_required",
                }:
                    raise GoTransportRuntimeError(
                        f"Go Transport health 返回未知状态：{health.get('status')!r}"
                    )
                self._client = client
            except Exception:
                self._state = "failed"
                self._terminate_locked()
                raise
            self._state = "healthy"
            return client

    def _read_ready(self, process: subprocess.Popen[str]) -> dict[str, Any]:
        assert process.stdout is not None
        deadline = time.monotonic() + self.startup_timeout_seconds
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise GoTransportRuntimeError(
                    f"Go Transport 启动前退出：exit={process.returncode}"
                )
            line = process.stdout.readline()
            if not line:
                time.sleep(0.02)
                continue
            try:
                ready = json.loads(line)
            except json.JSONDecodeError:
                continue
            if not isinstance(ready, dict):
                continue
            if ready.get("schema") != "cwapi.transport.ready.v1":
                continue
            url = str(ready.get("url") or "")
            if not url.startswith(("http://127.0.0.1:", "http://localhost:")):
                raise GoTransportRuntimeError(
                    f"Go Transport 返回非 loopback endpoint：{url!r}"
                )
            return ready
        raise GoTransportRuntimeError("等待 Go Transport readiness 超时。")

    def _start_stderr_capture(self, stream: IO[str]) -> None:
        stderr_path = self.events_path.with_name("go-transport.stderr.log")
        self._stderr_file = stderr_path.open("a", encoding="utf-8", newline="\n")

        def consume() -> None:
            try:
                for line in stream:
                    if self._stderr_file is not None:
                        self._stderr_file.write(line)
                        self._stderr_file.flush()
            except (OSError, ValueError):
                return

        self._stderr_thread = threading.Thread(
            target=consume,
            name="cwapi-go-transport-stderr",
            daemon=True,
        )
        self._stderr_thread.start()

    def close(self) -> None:
        with self._lock:
            self._state = "stopping"
            self._terminate_locked()
            self._state = "stopped"
            self._url = None
            self._version = None
            self._secret = None
            self._client = None

    def _terminate_locked(self) -> None:
        process = self._process
        self._process = None
        self._client = None
        if process is not None and process.poll() is None:
            try:
                process.terminate()
                process.wait(timeout=10)
            except (OSError, subprocess.TimeoutExpired):
                try:
                    process.kill()
                    process.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    pass
        thread = self._stderr_thread
        self._stderr_thread = None
        if thread is not None:
            thread.join(timeout=2)
        stderr_file = self._stderr_file
        self._stderr_file = None
        if stderr_file is not None:
            try:
                stderr_file.close()
            except OSError:
                pass

    def __enter__(self) -> "GoTransportRuntime":
        self.start()
        return self

    def __exit__(self, *_args: object) -> None:
        self.close()


def discover_go_transport_executable(token_path: Path) -> Path:
    root = (
        token_path.parent.parent
        if token_path.parent.name.casefold() == "secrets"
        else data_root_from_database(token_path.parent / "state" / "cwapi.db")
    )
    executable_name = "cwapi-transport.exe" if os.name == "nt" else "cwapi-transport"
    candidates = (
        root / "runtime" / "transport" / executable_name,
        root / executable_name,
    )
    for candidate in candidates:
        if candidate.is_file():
            return candidate.resolve()
    return candidates[0].resolve()
