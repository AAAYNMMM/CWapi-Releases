from __future__ import annotations

from collections import OrderedDict, deque
from copy import deepcopy
from datetime import datetime, timezone
import json
import threading
from typing import Any


TRACE_PREFIX = "@@CWAPI_TRACE@@"
_MAX_RUNTIME_LINES = 4000
_MAX_OUTPUT_LINES_PER_STREAM = 2000
_MAX_STRUCTURED_EVENTS_PER_TASK = 1000
_MAX_TASKS = 24


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _duration_ms(started_at: object, *, now: datetime | None = None) -> int | None:
    if not isinstance(started_at, str) or not started_at:
        return None
    try:
        started = datetime.fromisoformat(started_at.replace("Z", "+00:00"))
    except ValueError:
        return None
    if started.tzinfo is None:
        started = started.replace(tzinfo=timezone.utc)
    current = now or datetime.now(timezone.utc)
    return max(0, int((current - started.astimezone(timezone.utc)).total_seconds() * 1000))


class LiveExecutionLogBus:
    """Process-local bounded buffers used by the GUI and Runner observers.

    Live logs are intentionally ephemeral. Runtime persistence is handled by
    explicit terminal/error flushes outside this bus, never by append-per-line IO.
    """

    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._runtime: deque[tuple[int, str]] = deque(maxlen=_MAX_RUNTIME_LINES)
        self._runtime_seq = 0
        self._outputs: dict[tuple[str, str, str], deque[tuple[int, str]]] = {}
        self._output_seq: dict[tuple[str, str, str], int] = {}
        self._structured: OrderedDict[str, OrderedDict[str, dict[str, Any]]] = OrderedDict()

    def reset(self) -> None:
        with self._lock:
            self._runtime.clear()
            self._runtime_seq = 0
            self._outputs.clear()
            self._output_seq.clear()
            self._structured.clear()

    def begin_task(self, task_id: str) -> None:
        with self._lock:
            self._structured.pop(task_id, None)
            self._structured[task_id] = OrderedDict()
            for key in [item for item in self._outputs if item[0] == task_id]:
                self._outputs.pop(key, None)
                self._output_seq.pop(key, None)
            while len(self._structured) > _MAX_TASKS:
                old_task, _ = self._structured.popitem(last=False)
                for key in [item for item in self._outputs if item[0] == old_task]:
                    self._outputs.pop(key, None)
                    self._output_seq.pop(key, None)

    def latest_task_id(self) -> str | None:
        with self._lock:
            return next(reversed(self._structured), None)

    def append_runtime(self, line: str) -> int:
        clean = str(line).rstrip("\r\n")
        with self._lock:
            self._runtime_seq += 1
            self._runtime.append((self._runtime_seq, clean))
            return self._runtime_seq

    def runtime_text(self, *, tail_bytes: int) -> tuple[int, str]:
        limit = max(1024, int(tail_bytes))
        with self._lock:
            seq = self._runtime_seq
            lines = [line for _, line in self._runtime]
        if not lines:
            return seq, ""
        encoded = ("\n".join(lines) + "\n").encode("utf-8", errors="replace")
        if len(encoded) > limit:
            encoded = encoded[-limit:]
            newline = encoded.find(b"\n")
            if newline >= 0:
                encoded = encoded[newline + 1 :]
        return seq, encoded.decode("utf-8", errors="replace")

    def runtime_lines(self) -> list[str]:
        with self._lock:
            return [line for _, line in self._runtime]

    def runtime_since(self, after_seq: int) -> list[tuple[int, str]]:
        with self._lock:
            return [(seq, line) for seq, line in self._runtime if seq > int(after_seq)]

    def append_output_line(self, task_id: str, step_id: str, stream: str, line: str) -> int:
        key = (task_id, step_id, stream)
        with self._lock:
            seq = self._output_seq.get(key, 0) + 1
            self._output_seq[key] = seq
            bucket = self._outputs.setdefault(
                key,
                deque(maxlen=_MAX_OUTPUT_LINES_PER_STREAM),
            )
            bucket.append((seq, str(line).rstrip("\r\n")))
            return seq

    def output_since(
        self,
        task_id: str,
        step_id: str,
        stream: str,
        after_seq: int,
    ) -> list[tuple[int, str]]:
        key = (task_id, step_id, stream)
        with self._lock:
            bucket = self._outputs.get(key)
            if not bucket:
                return []
            return [(seq, line) for seq, line in bucket if seq > after_seq]

    def update_step(
        self,
        *,
        task_id: str,
        step_id: str,
        action: str,
        ordinal: int,
        status: str,
        started_at: str,
        finished_at: str | None = None,
        duration_ms: int | None = None,
        error: str | None = None,
    ) -> None:
        event_id = f"step:{ordinal}:{step_id}"
        data: dict[str, Any] = {
            "step_id": step_id,
            "action": action,
            "ordinal": ordinal,
            "execution_status": status,
            "started_at": started_at,
            "finished_at": finished_at,
            "duration_ms": duration_ms,
        }
        if error:
            data["error"] = error[:500]
        self._upsert_structured(
            task_id,
            event_id,
            {
                "id": event_id,
                "source": "action",
                "type": "step",
                "status": status,
                "timestamp": started_at,
                "message": step_id,
                "data": data,
            },
        )

    def ingest_trace(self, *, task_id: str, step_id: str, payload: dict[str, Any]) -> bool:
        if payload.get("schema") != "cwapi.function.trace.v2":
            return False
        lifecycle_id = str(payload.get("lifecycle_id") or "")
        function = str(payload.get("function") or "")
        if not lifecycle_id or not function:
            return False
        status = str(payload.get("status") or "running")
        event_id = f"function:{step_id}:{lifecycle_id}"
        data = dict(payload)
        data["step_id"] = step_id
        self._upsert_structured(
            task_id,
            event_id,
            {
                "id": event_id,
                "source": "python",
                "type": "function",
                "status": status,
                "timestamp": payload.get("started_at") or _now_iso(),
                "message": function,
                "data": data,
            },
        )
        return True

    def structured_events(self, task_id: str, *, limit: int) -> list[dict[str, Any]]:
        now = datetime.now(timezone.utc)
        with self._lock:
            bucket = self._structured.get(task_id)
            raw = list(bucket.values())[-max(1, int(limit)) :] if bucket else []
            result = deepcopy(raw)
        for event in result:
            if str(event.get("status") or "") != "running":
                continue
            data = event.get("data")
            if not isinstance(data, dict):
                continue
            elapsed = _duration_ms(data.get("started_at"), now=now)
            if elapsed is not None:
                data["duration_ms"] = elapsed
        return result

    def current_function(self, task_id: str) -> dict[str, Any] | None:
        events = self.structured_events(task_id, limit=_MAX_STRUCTURED_EVENTS_PER_TASK)
        for event in reversed(events):
            if event.get("type") == "function" and event.get("status") == "running":
                data = event.get("data")
                if isinstance(data, dict):
                    return {
                        "schema": "cwapi.function.current.v2",
                        "updated_at": _now_iso(),
                        "active": data,
                    }
        return None

    def _upsert_structured(self, task_id: str, event_id: str, event: dict[str, Any]) -> None:
        with self._lock:
            bucket = self._structured.get(task_id)
            if bucket is None:
                bucket = OrderedDict()
                self._structured[task_id] = bucket
            bucket[event_id] = event
            while len(bucket) > _MAX_STRUCTURED_EVENTS_PER_TASK:
                removable = next(
                    (
                        key
                        for key, item in bucket.items()
                        if str(item.get("status") or "") != "running"
                    ),
                    next(iter(bucket)),
                )
                bucket.pop(removable, None)
            self._structured.move_to_end(task_id)
            while len(self._structured) > _MAX_TASKS:
                old_task, _ = self._structured.popitem(last=False)
                for key in [item for item in self._outputs if item[0] == old_task]:
                    self._outputs.pop(key, None)
                    self._output_seq.pop(key, None)


class ProcessOutputCollector:
    """Collect subprocess output in memory and consume CWapi trace protocol lines."""

    def __init__(
        self,
        *,
        task_id: str,
        step_id: str,
        max_output_bytes: int,
        bus: LiveExecutionLogBus | None = None,
    ) -> None:
        self.task_id = task_id
        self.step_id = step_id
        self.max_output_bytes = max_output_bytes
        self.bus = bus or LIVE_LOGS
        self._lock = threading.RLock()
        self._buffers = {"stdout": bytearray(), "stderr": bytearray()}
        self._partials = {"stdout": bytearray(), "stderr": bytearray()}
        self.raw_bytes_seen = 0
        self.output_limit_exceeded = False

    def feed(self, stream: str, data: bytes) -> None:
        if stream not in self._buffers or not data:
            return
        with self._lock:
            self.raw_bytes_seen += len(data)
            if self.raw_bytes_seen > self.max_output_bytes:
                self.output_limit_exceeded = True
            partial = self._partials[stream]
            partial.extend(data)
            while True:
                newline = partial.find(b"\n")
                if newline < 0:
                    break
                raw_line = bytes(partial[:newline])
                del partial[: newline + 1]
                self._consume_line(stream, raw_line, had_newline=True)

    def finish(self) -> None:
        with self._lock:
            for stream in ("stdout", "stderr"):
                partial = self._partials[stream]
                if partial:
                    raw_line = bytes(partial)
                    partial.clear()
                    self._consume_line(stream, raw_line, had_newline=False)

    def text(self, stream: str) -> str:
        with self._lock:
            return bytes(self._buffers[stream]).decode("utf-8", errors="replace")

    def _consume_line(self, stream: str, raw_line: bytes, *, had_newline: bool) -> None:
        normalized = raw_line.rstrip(b"\r")
        decoded = normalized.decode("utf-8", errors="replace")
        if stream == "stdout" and decoded.startswith(TRACE_PREFIX):
            try:
                payload = json.loads(decoded[len(TRACE_PREFIX) :])
            except json.JSONDecodeError:
                payload = None
            if isinstance(payload, dict) and self.bus.ingest_trace(
                task_id=self.task_id,
                step_id=self.step_id,
                payload=payload,
            ):
                return
        target = self._buffers[stream]
        target.extend(raw_line)
        if had_newline:
            target.extend(b"\n")
        self.bus.append_output_line(self.task_id, self.step_id, stream, decoded)


def sanitize_trace_text(
    text: str,
    *,
    task_id: str,
    step_id: str,
    bus: LiveExecutionLogBus | None = None,
) -> str:
    selected_bus = bus or LIVE_LOGS
    output: list[str] = []
    for line in text.splitlines(keepends=True):
        normalized = line.rstrip("\r\n")
        if normalized.startswith(TRACE_PREFIX):
            try:
                payload = json.loads(normalized[len(TRACE_PREFIX) :])
            except json.JSONDecodeError:
                payload = None
            if isinstance(payload, dict) and selected_bus.ingest_trace(
                task_id=task_id,
                step_id=step_id,
                payload=payload,
            ):
                continue
        output.append(line)
    return "".join(output)


LIVE_LOGS = LiveExecutionLogBus()
