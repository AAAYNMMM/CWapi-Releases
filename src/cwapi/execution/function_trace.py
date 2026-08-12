from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import runpy
import sys
import threading
import time
from types import FrameType
from typing import Any

from .live_logs import TRACE_PREFIX


class FunctionTracer:
    """Emit Python function lifecycle events over the existing stdout pipe.

    The parent Runner consumes TRACE_PREFIX records into its process-local
    memory bus. No current/history trace files are created or updated.
    """

    def __init__(self, *, source_root: Path) -> None:
        self.source_root = source_root.resolve()
        self.started: dict[int, tuple[float, str, str, int, str, str]] = {}
        self.active_stack: list[int] = []
        self.pending_exceptions: dict[int, str] = {}
        self.lock = threading.RLock()
        self._sequence = 0

    def _tracked(self, frame: FrameType) -> bool:
        try:
            Path(frame.f_code.co_filename).resolve().relative_to(self.source_root)
            return True
        except (OSError, ValueError):
            return False

    @staticmethod
    def _now() -> str:
        return datetime.now(timezone.utc).isoformat()

    def _next_lifecycle_id(self) -> str:
        self._sequence += 1
        return f"fn-{self._sequence}"

    @staticmethod
    def _emit(payload: dict[str, Any]) -> None:
        print(
            TRACE_PREFIX
            + json.dumps(
                payload,
                ensure_ascii=False,
                sort_keys=True,
                separators=(",", ":"),
            ),
            flush=True,
        )

    def _start(self, frame: FrameType) -> None:
        key = id(frame)
        started_at = self._now()
        lifecycle_id = self._next_lifecycle_id()
        file_name = Path(frame.f_code.co_filename).name
        line = frame.f_code.co_firstlineno
        function_name = frame.f_code.co_name
        self.started[key] = (
            time.monotonic(),
            started_at,
            file_name,
            line,
            function_name,
            lifecycle_id,
        )
        self.active_stack.append(key)
        self._emit(
            {
                "schema": "cwapi.function.trace.v2",
                "lifecycle_id": lifecycle_id,
                "function": function_name,
                "file": file_name,
                "line": line,
                "status": "running",
                "started_at": started_at,
            }
        )

    def _finish(self, frame: FrameType, status: str, error: str | None = None) -> None:
        key = id(frame)
        started = self.started.pop(key, None)
        self.pending_exceptions.pop(key, None)
        if key in self.active_stack:
            self.active_stack.remove(key)
        if started is None:
            return
        started_monotonic, started_at, file_name, line, function_name, lifecycle_id = started
        item: dict[str, Any] = {
            "schema": "cwapi.function.trace.v2",
            "lifecycle_id": lifecycle_id,
            "function": function_name,
            "file": file_name,
            "line": line,
            "status": status,
            "started_at": started_at,
            "finished_at": self._now(),
            "duration_ms": max(
                0,
                int((time.monotonic() - started_monotonic) * 1000),
            ),
        }
        if error:
            item["error"] = error[:500]
        self._emit(item)

    def __call__(self, frame: FrameType, event: str, arg: Any):
        if not self._tracked(frame):
            return None
        key = id(frame)
        frame.f_trace_lines = key in self.pending_exceptions
        with self.lock:
            if event == "call":
                self._start(frame)
                frame.f_trace_lines = False
            elif event == "exception":
                exc_type, exc, _ = arg
                if isinstance(exc, SystemExit) and (exc.code is None or exc.code == 0):
                    self.pending_exceptions.pop(key, None)
                    frame.f_trace_lines = False
                else:
                    self.pending_exceptions[key] = (
                        f"{getattr(exc_type, '__name__', 'Exception')}: {exc}"
                    )
                    frame.f_trace_lines = True
            elif event == "line" and key in self.pending_exceptions:
                self.pending_exceptions.pop(key, None)
                frame.f_trace_lines = False
            elif event == "return":
                error = self.pending_exceptions.get(key)
                self._finish(frame, "failed" if error else "completed", error)
        return self


def run_script(script: Path, arguments: list[str]) -> int:
    tracer = FunctionTracer(source_root=script.parent)
    old_argv = sys.argv
    sys.argv = [str(script), *arguments]
    sys.settrace(tracer)
    try:
        try:
            runpy.run_path(str(script), run_name="__main__")
            return 0
        except SystemExit as exc:
            if exc.code is None:
                return 0
            if isinstance(exc.code, int):
                return exc.code
            print(str(exc.code), file=sys.stderr)
            return 1
    finally:
        sys.settrace(None)
        sys.argv = old_argv
        with tracer.lock:
            tracer.active_stack.clear()
            tracer.started.clear()
            tracer.pending_exceptions.clear()


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    parser.add_argument("--script", required=True)
    # Kept only so packaged runners from the previous trace-file protocol can
    # invoke this module during a rolling upgrade. Values are deliberately ignored.
    parser.add_argument("--current", required=False)
    parser.add_argument("--history", required=False)
    parser.add_argument("arguments", nargs=argparse.REMAINDER)
    return parser


def main() -> None:
    args = _parser().parse_args()
    arguments = list(args.arguments)
    if arguments and arguments[0] == "--":
        arguments = arguments[1:]
    raise SystemExit(run_script(Path(args.script).resolve(), arguments))


if __name__ == "__main__":
    main()
