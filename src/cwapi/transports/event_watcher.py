from __future__ import annotations

import threading
from typing import Any

from ..observability import RunnerConsole
from .go_service import GoDraftTransportClient


_EVENT_MESSAGES = {
    "transport_started": "Go Gmail 传输服务已启动。",
    "transport_degraded": "Google 连接异常，Go 传输层进入退避。",
    "transport_retry": "Google 请求正在退避重试。",
    "transport_recovered": "Google 连接已恢复。",
}


class TransportEventWatcher(threading.Thread):
    def __init__(
        self,
        *,
        client: GoDraftTransportClient,
        console: RunnerConsole,
        stop_event: threading.Event,
        poll_seconds: float = 2.0,
    ) -> None:
        super().__init__(name="cwapi-transport-event-watcher", daemon=True)
        self.client = client
        self.console = console
        self.stop_event = stop_event
        self.poll_seconds = poll_seconds
        self.last_event_id = 0
        self._local_unavailable = False

    def run(self) -> None:
        while not self.stop_event.is_set():
            self.poll_once()
            self.stop_event.wait(self.poll_seconds)

    def poll_once(self) -> None:
        try:
            events = self.client.events(after_id=self.last_event_id)
        except Exception as exc:
            if not self._local_unavailable:
                self.console.emit(
                    "TRANSPORT",
                    "无法读取本机 Go 传输服务状态。",
                    level="WARN",
                    details={"error": str(exc)[:500]},
                )
                self._local_unavailable = True
            return

        if self._local_unavailable:
            self.console.emit("TRANSPORT", "本机 Go 传输服务连接已恢复。")
            self._local_unavailable = False

        for event in events:
            event_id = self._event_id(event)
            if event_id <= self.last_event_id:
                continue
            self.last_event_id = event_id
            event_type = str(event.get("type", "transport_event"))
            message = _EVENT_MESSAGES.get(
                event_type,
                str(event.get("message", "Go transport event.")),
            )
            level = str(event.get("level", "INFO")).upper()
            details = event.get("details")
            self.console.emit(
                "TRANSPORT",
                message,
                level=level if level in {"INFO", "WARN", "ERROR"} else "INFO",
                details=dict(details) if isinstance(details, dict) else None,
            )

    @staticmethod
    def _event_id(event: dict[str, Any]) -> int:
        try:
            return max(0, int(event.get("id", 0)))
        except (TypeError, ValueError):
            return 0
