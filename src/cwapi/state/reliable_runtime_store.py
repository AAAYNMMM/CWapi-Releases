from __future__ import annotations

from pathlib import Path
import threading
import time

from .runtime_store import RuntimeStateStore
from .sqlite_store import utc_now


class ReliableRuntimeStateStore(RuntimeStateStore):
    """Runtime store with process-local lifecycle coordination."""

    cancellation_sync_seconds = 1.0

    def __init__(self, path: Path) -> None:
        super().__init__(path)
        self._initialize_lock = threading.Lock()
        self._initialized = False
        self._cancellation_lock = threading.RLock()
        self._cancellation_events: dict[str, threading.Event] = {}
        self._cancellation_last_sync: dict[str, float] = {}

    def initialize(self) -> None:
        if self._initialized:
            return
        with self._initialize_lock:
            if self._initialized:
                return
            super().initialize()
            self._initialized = True

    def _cancellation_event(self, task_id: str) -> threading.Event:
        with self._cancellation_lock:
            return self._cancellation_events.setdefault(task_id, threading.Event())

    def request_cancel(
        self,
        *,
        task_id: str,
        source_draft_id: str,
        reason: str | None,
    ) -> bool:
        inserted = super().request_cancel(
            task_id=task_id,
            source_draft_id=source_draft_id,
            reason=reason,
        )
        event = self._cancellation_event(task_id)
        event.set()
        with self._cancellation_lock:
            self._cancellation_last_sync[task_id] = time.monotonic()
        return inserted

    def is_cancel_requested(self, task_id: str) -> bool:
        event = self._cancellation_event(task_id)
        if event.is_set():
            return True

        now = time.monotonic()
        with self._cancellation_lock:
            last_sync = self._cancellation_last_sync.get(task_id)
            if (
                last_sync is not None
                and now - last_sync < self.cancellation_sync_seconds
            ):
                return False
            self._cancellation_last_sync[task_id] = now

        requested = super().is_cancel_requested(task_id)
        if requested:
            event.set()
        return requested

    def finalize_workspace(
        self,
        *,
        task_id: str,
        status: str,
        error: str | None = None,
        released: bool = False,
    ) -> None:
        super().finalize_workspace(
            task_id=task_id,
            status=status,
            error=error,
            released=released,
        )
        # The durable terminal transition commits before process-local resource
        # cleanup. Cleanup failure therefore cannot erase task evidence.
        from cwapi.codex_toolhost.shared_client import release_shared_task_resources
        from cwapi.execution.task_policy_snapshot import release_task_policy_snapshot

        release_shared_task_resources(task_id)
        release_task_policy_snapshot(task_id)
        with self._cancellation_lock:
            self._cancellation_events.pop(task_id, None)
            self._cancellation_last_sync.pop(task_id, None)

    def has_rejected_input(
        self,
        *,
        source_draft_id: str,
        raw_content_hash: str,
    ) -> bool:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT result_draft_id
                FROM rejected_inputs
                WHERE source_draft_id = ?
                  AND raw_content_sha256 = ?
                """,
                (source_draft_id, raw_content_hash),
            ).fetchone()
        if row is None:
            return False
        return bool(str(row["result_draft_id"] or "").strip())

    def record_rejected_input(
        self,
        *,
        source_draft_id: str,
        raw_content_hash: str,
        task_id: str,
        reason: str,
        result_draft_id: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO rejected_inputs (
                    source_draft_id, raw_content_sha256, task_id,
                    reason, result_draft_id, rejected_at
                )
                VALUES (?, ?, ?, ?, ?, ?)
                ON CONFLICT(source_draft_id, raw_content_sha256) DO UPDATE SET
                    task_id = excluded.task_id,
                    reason = excluded.reason,
                    result_draft_id = CASE
                        WHEN excluded.result_draft_id <> ''
                        THEN excluded.result_draft_id
                        ELSE rejected_inputs.result_draft_id
                    END,
                    rejected_at = CASE
                        WHEN excluded.result_draft_id <> ''
                        THEN excluded.rejected_at
                        ELSE rejected_inputs.rejected_at
                    END
                """,
                (
                    source_draft_id,
                    raw_content_hash,
                    task_id,
                    reason,
                    result_draft_id,
                    now,
                ),
            )
