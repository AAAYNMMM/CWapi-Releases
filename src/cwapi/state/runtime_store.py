from __future__ import annotations

from collections.abc import Mapping
import json
from dataclasses import dataclass
from pathlib import Path
import sqlite3
from typing import Any

from ..gpt_protocol import GPTRequestEnvelope
from ..paths import ManagedPathCodec
from .sqlite_store import SQLiteStateStore, utc_now


class GPTRequestStateError(RuntimeError):
    pass


@dataclass(frozen=True)
class GPTRequestRegistration:
    disposition: str
    request_id: str
    reason: str | None
    record: dict[str, Any]


class RuntimeStateStore(SQLiteStateStore):
    """Additive state used by the v1 runtime without breaking Phase 2.1 APIs."""

    def __init__(self, path: Path) -> None:
        super().__init__(path)
        self.path_codec = ManagedPathCodec.for_database(path)

    def _store_path(self, value: str | Path | None) -> str | None:
        return self.path_codec.normalize_for_storage(value)

    def _read_path(self, value: str | Path | None) -> str | None:
        return self.path_codec.decode(value)

    def _decode_fields(
        self,
        value: dict[str, Any],
        *fields: str,
    ) -> dict[str, Any]:
        result = dict(value)
        for field in fields:
            if result.get(field) is not None:
                result[field] = self._read_path(str(result[field]))
        return result

    def initialize(self) -> None:
        super().initialize()
        with self.connect() as db:
            self._ensure_task_columns(db)
            self._ensure_step_columns(db)
            db.executescript(
                """
                CREATE TABLE IF NOT EXISTS workspaces (
                    task_id TEXT PRIMARY KEY,
                    repository TEXT NOT NULL,
                    mirror_path TEXT NOT NULL,
                    worktree_path TEXT NOT NULL,
                    expected_commit TEXT NOT NULL,
                    actual_commit TEXT,
                    workspace_status TEXT NOT NULL,
                    managed INTEGER NOT NULL,
                    created_at TEXT NOT NULL,
                    released_at TEXT,
                    keep_until TEXT,
                    last_error TEXT,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS task_artifacts (
                    task_id TEXT PRIMARY KEY,
                    local_path TEXT NOT NULL,
                    drive_relative_path TEXT,
                    manifest_sha256 TEXT NOT NULL,
                    total_bytes INTEGER NOT NULL,
                    sync_status TEXT NOT NULL,
                    zip_path TEXT,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS cancel_requests (
                    source_draft_id TEXT PRIMARY KEY,
                    task_id TEXT NOT NULL,
                    reason TEXT,
                    requested_at TEXT NOT NULL
                );

                CREATE INDEX IF NOT EXISTS ix_cancel_requests_task
                ON cancel_requests(task_id, requested_at);

                CREATE TABLE IF NOT EXISTS runner_heartbeat (
                    runner_id TEXT PRIMARY KEY,
                    pid INTEGER,
                    status TEXT NOT NULL,
                    last_poll_at TEXT,
                    last_cleanup_at TEXT,
                    details_json TEXT,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS gpt_requests (
                    request_id TEXT PRIMARY KEY,
                    source_draft_id TEXT NOT NULL UNIQUE,
                    source_message_id TEXT,
                    content_sha256 TEXT NOT NULL,
                    operation TEXT NOT NULL,
                    request_json TEXT NOT NULL,
                    status TEXT NOT NULL,
                    task_id TEXT UNIQUE,
                    task_subject TEXT UNIQUE,
                    task_json TEXT,
                    task_draft_id TEXT,
                    task_publish_attempt_count INTEGER NOT NULL DEFAULT 0,
                    task_last_attempt_at TEXT,
                    task_last_error TEXT,
                    response_status TEXT,
                    response_subject TEXT UNIQUE,
                    response_json TEXT,
                    response_draft_id TEXT UNIQUE,
                    response_attempt_count INTEGER NOT NULL DEFAULT 0,
                    response_last_attempt_at TEXT,
                    response_last_error TEXT,
                    conflict_count INTEGER NOT NULL DEFAULT 0,
                    last_conflict_at TEXT,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    completed_at TEXT
                );

                CREATE INDEX IF NOT EXISTS ix_gpt_requests_status
                ON gpt_requests(status, updated_at);
                """
            )
            self._ensure_gpt_request_columns(db)
            db.execute(
                """
                CREATE UNIQUE INDEX IF NOT EXISTS ix_gpt_requests_task_draft
                ON gpt_requests(task_draft_id)
                WHERE task_draft_id IS NOT NULL
                """
            )
            self._migrate_managed_paths(db)

    @staticmethod
    def _decode_gpt_request(row: sqlite3.Row | None) -> dict[str, Any] | None:
        if row is None:
            return None
        item = dict(row)
        raw_request = item.pop("request_json", None)
        raw_task = item.pop("task_json", None)
        raw_response = item.pop("response_json", None)
        item["request"] = json.loads(str(raw_request)) if raw_request else None
        item["task"] = json.loads(str(raw_task)) if raw_task else None
        item["response"] = json.loads(str(raw_response)) if raw_response else None
        return item

    def get_gpt_request(self, request_id: str) -> dict[str, Any] | None:
        with self.connect() as db:
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
        return self._decode_gpt_request(row)

    def register_gpt_request(
        self,
        envelope: GPTRequestEnvelope,
    ) -> GPTRequestRegistration:
        now = utc_now()
        request_json = json.dumps(
            envelope.payload,
            ensure_ascii=False,
            separators=(",", ":"),
            sort_keys=True,
        )
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            rows = db.execute(
                """
                SELECT * FROM gpt_requests
                WHERE request_id = ? OR source_draft_id = ?
                """,
                (envelope.request_id, envelope.source_draft_id),
            ).fetchall()
            if len(rows) > 1:
                raise GPTRequestStateError(
                    "request identity maps to multiple persisted rows"
                )
            if not rows:
                db.execute(
                    """
                    INSERT INTO gpt_requests (
                        request_id, source_draft_id, source_message_id,
                        content_sha256, operation, request_json, status,
                        created_at, updated_at
                    )
                    VALUES (?, ?, ?, ?, ?, ?, 'received', ?, ?)
                    """,
                    (
                        envelope.request_id,
                        envelope.source_draft_id,
                        envelope.source_message_id,
                        envelope.content_hash,
                        envelope.operation,
                        request_json,
                        now,
                        now,
                    ),
                )
                row = db.execute(
                    "SELECT * FROM gpt_requests WHERE request_id = ?",
                    (envelope.request_id,),
                ).fetchone()
                db.commit()
                record = self._decode_gpt_request(row)
                assert record is not None
                return GPTRequestRegistration(
                    disposition="new",
                    request_id=envelope.request_id,
                    reason=None,
                    record=record,
                )

            row = rows[0]
            if (
                str(row["request_id"]) != envelope.request_id
                or str(row["source_draft_id"]) != envelope.source_draft_id
                or str(row["content_sha256"]) != envelope.content_hash
            ):
                db.execute(
                    """
                    UPDATE gpt_requests
                    SET conflict_count = conflict_count + 1,
                        last_conflict_at = ?, updated_at = ?
                    WHERE request_id = ?
                    """,
                    (now, now, row["request_id"]),
                )
                updated = db.execute(
                    "SELECT * FROM gpt_requests WHERE request_id = ?",
                    (row["request_id"],),
                ).fetchone()
                db.commit()
                record = self._decode_gpt_request(updated)
                assert record is not None
                return GPTRequestRegistration(
                    disposition="conflict",
                    request_id=str(row["request_id"]),
                    reason="source_draft_content_changed",
                    record=record,
                )

            if (
                envelope.source_message_id
                and envelope.source_message_id != row["source_message_id"]
            ):
                db.execute(
                    """
                    UPDATE gpt_requests
                    SET source_message_id = ?, updated_at = ?
                    WHERE request_id = ?
                    """,
                    (envelope.source_message_id, now, envelope.request_id),
                )
            current = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (envelope.request_id,),
            ).fetchone()
            db.commit()
        record = self._decode_gpt_request(current)
        assert record is not None
        return GPTRequestRegistration(
            disposition=("replay" if record["response_draft_id"] else "resume"),
            request_id=envelope.request_id,
            reason=None,
            record=record,
        )

    def reserve_gpt_task(
        self,
        *,
        request_id: str,
        task_id: str,
        task_subject: str,
        task: Mapping[str, Any],
    ) -> dict[str, Any]:
        task_json = json.dumps(
            dict(task),
            ensure_ascii=False,
            separators=(",", ":"),
            sort_keys=True,
        )
        now = utc_now()
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            if row is None:
                raise GPTRequestStateError("request is not registered")
            existing = (row["task_id"], row["task_subject"], row["task_json"])
            requested = (task_id, task_subject, task_json)
            if existing == (None, None, None):
                db.execute(
                    """
                    UPDATE gpt_requests
                    SET task_id = ?, task_subject = ?, task_json = ?,
                        status = 'task_ready', updated_at = ?
                    WHERE request_id = ?
                    """,
                    (task_id, task_subject, task_json, now, request_id),
                )
            elif existing != requested:
                raise GPTRequestStateError(
                    "request already reserved a different canonical task"
                )
            current = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            db.commit()
        record = self._decode_gpt_request(current)
        assert record is not None
        return record

    def mark_gpt_task_published(
        self,
        *,
        request_id: str,
        task_draft_id: str,
    ) -> dict[str, Any]:
        now = utc_now()
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            if row is None:
                raise GPTRequestStateError("request is not registered")
            if row["task_subject"] is None or row["task_json"] is None:
                raise GPTRequestStateError("task is not reserved")
            existing = row["task_draft_id"]
            if existing is None:
                db.execute(
                    """
                    UPDATE gpt_requests
                    SET task_draft_id = ?, status = 'task_published',
                        task_publish_attempt_count = task_publish_attempt_count + 1,
                        task_last_attempt_at = ?, task_last_error = NULL,
                        updated_at = ?
                    WHERE request_id = ?
                    """,
                    (task_draft_id, now, now, request_id),
                )
            elif existing != task_draft_id:
                raise GPTRequestStateError(
                    "request task was published with a different draft identity"
                )
            current = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            db.commit()
        record = self._decode_gpt_request(current)
        assert record is not None
        return record

    def mark_gpt_task_publish_failed(
        self,
        *,
        request_id: str,
        error: str,
    ) -> dict[str, Any]:
        now = utc_now()
        with self.connect() as db:
            updated = db.execute(
                """
                UPDATE gpt_requests
                SET status = 'task_ready',
                    task_publish_attempt_count = task_publish_attempt_count + 1,
                    task_last_attempt_at = ?, task_last_error = ?,
                    updated_at = ?
                WHERE request_id = ? AND task_json IS NOT NULL
                """,
                (now, str(error)[:500], now, request_id),
            ).rowcount
            if not updated:
                raise GPTRequestStateError(
                    "request task is not reserved for publishing retry"
                )
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
        record = self._decode_gpt_request(row)
        assert record is not None
        return record

    def reserve_gpt_response(
        self,
        *,
        request_id: str,
        response_status: str,
        response_subject: str,
        response: Mapping[str, Any],
    ) -> dict[str, Any]:
        normalized_status = str(response_status).strip().lower()
        if normalized_status not in {"ready", "failed"}:
            raise GPTRequestStateError("response status must be ready or failed")
        response_json = json.dumps(
            dict(response),
            ensure_ascii=False,
            separators=(",", ":"),
            sort_keys=True,
        )
        now = utc_now()
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            if row is None:
                raise GPTRequestStateError("request is not registered")
            existing = (
                row["response_status"],
                row["response_subject"],
                row["response_json"],
            )
            requested = (normalized_status, response_subject, response_json)
            if existing == (None, None, None):
                db.execute(
                    """
                    UPDATE gpt_requests
                    SET response_status = ?, response_subject = ?,
                        response_json = ?, status = 'response_ready',
                        updated_at = ?
                    WHERE request_id = ?
                    """,
                    (
                        normalized_status,
                        response_subject,
                        response_json,
                        now,
                        request_id,
                    ),
                )
            elif existing != requested:
                raise GPTRequestStateError(
                    "request already reserved a different response"
                )
            current = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            db.commit()
        record = self._decode_gpt_request(current)
        assert record is not None
        return record

    def mark_gpt_response_delivered(
        self,
        *,
        request_id: str,
        response_draft_id: str,
    ) -> dict[str, Any]:
        now = utc_now()
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            if row is None:
                raise GPTRequestStateError("request is not registered")
            if row["response_subject"] is None or row["response_json"] is None:
                raise GPTRequestStateError("response is not reserved")
            existing = row["response_draft_id"]
            if existing is None:
                db.execute(
                    """
                    UPDATE gpt_requests
                    SET response_draft_id = ?, status = 'response_delivered',
                        response_attempt_count = response_attempt_count + 1,
                        response_last_attempt_at = ?, response_last_error = NULL,
                        completed_at = ?, updated_at = ?
                    WHERE request_id = ?
                    """,
                    (response_draft_id, now, now, now, request_id),
                )
            elif existing != response_draft_id:
                raise GPTRequestStateError(
                    "request response was delivered with a different draft identity"
                )
            current = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
            db.commit()
        record = self._decode_gpt_request(current)
        assert record is not None
        return record

    def mark_gpt_response_delivery_failed(
        self,
        *,
        request_id: str,
        error: str,
    ) -> dict[str, Any]:
        now = utc_now()
        with self.connect() as db:
            updated = db.execute(
                """
                UPDATE gpt_requests
                SET status = 'response_ready',
                    response_attempt_count = response_attempt_count + 1,
                    response_last_attempt_at = ?,
                    response_last_error = ?,
                    updated_at = ?
                WHERE request_id = ? AND response_json IS NOT NULL
                """,
                (now, str(error)[:500], now, request_id),
            ).rowcount
            if not updated:
                raise GPTRequestStateError(
                    "request response is not reserved for delivery retry"
                )
            row = db.execute(
                "SELECT * FROM gpt_requests WHERE request_id = ?",
                (request_id,),
            ).fetchone()
        record = self._decode_gpt_request(row)
        assert record is not None
        return record

    def _ensure_step_columns(self, db: sqlite3.Connection) -> None:
        columns = {str(row["name"]) for row in db.execute("PRAGMA table_info(task_steps)").fetchall()}
        additions = {
            "command_receipt_json": "TEXT",
            "diagnostics_json": "TEXT",
            "diagnostic_warnings_json": "TEXT",
            "workspace_status_before_json": "TEXT",
            "workspace_status_after_json": "TEXT",
        }
        for name, declaration in additions.items():
            if name not in columns:
                db.execute(f"ALTER TABLE task_steps ADD COLUMN {name} {declaration}")

    def _ensure_task_columns(self, db: sqlite3.Connection) -> None:
        columns = {
            str(row["name"])
            for row in db.execute("PRAGMA table_info(tasks)").fetchall()
        }
        additions = {
            "task_json": "TEXT",
            "cancel_requested": "INTEGER NOT NULL DEFAULT 0",
            "progress_status": "TEXT",
            "workspace_path": "TEXT",
            "artifact_path": "TEXT",
        }
        for name, declaration in additions.items():
            if name not in columns:
                db.execute(f"ALTER TABLE tasks ADD COLUMN {name} {declaration}")

    def _ensure_gpt_request_columns(self, db: sqlite3.Connection) -> None:
        columns = {
            str(row["name"])
            for row in db.execute("PRAGMA table_info(gpt_requests)").fetchall()
        }
        additions = {
            "task_json": "TEXT",
            "task_draft_id": "TEXT",
            "task_publish_attempt_count": "INTEGER NOT NULL DEFAULT 0",
            "task_last_attempt_at": "TEXT",
            "task_last_error": "TEXT",
        }
        for name, declaration in additions.items():
            if name not in columns:
                db.execute(
                    f"ALTER TABLE gpt_requests ADD COLUMN {name} {declaration}"
                )

    def _migrate_managed_paths(self, db: sqlite3.Connection) -> None:
        specifications = (
            ("tasks", "task_id", ("workspace_path", "artifact_path")),
            ("workspaces", "task_id", ("mirror_path", "worktree_path")),
            ("task_artifacts", "task_id", ("local_path", "zip_path")),
            ("task_steps", "id", ("stdout_path", "stderr_path")),
        )
        for table, key, columns in specifications:
            selected = ", ".join((key, *columns))
            rows = db.execute(f"SELECT {selected} FROM {table}").fetchall()
            for row in rows:
                updates: dict[str, str] = {}
                for column in columns:
                    current = row[column]
                    if current is None:
                        continue
                    normalized = self._store_path(str(current))
                    if normalized is not None and normalized != current:
                        updates[column] = normalized
                if not updates:
                    continue
                assignment = ", ".join(f"{name} = ?" for name in updates)
                db.execute(
                    f"UPDATE {table} SET {assignment} WHERE {key} = ?",
                    (*updates.values(), row[key]),
                )

    def attach_task_payload(self, *, task_id: str, task: dict[str, Any]) -> None:
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET task_json = ?, updated_at = ?
                WHERE task_id = ?
                """,
                (
                    json.dumps(task, ensure_ascii=False, sort_keys=True),
                    utc_now(),
                    task_id,
                ),
            )

    def get_task(self, task_id: str) -> dict[str, Any] | None:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT task_id, content_sha256, source_draft_id, repository,
                       expected_commit, execution_status, result_status,
                       received_at, finished_at, result_draft_id, last_error,
                       updated_at, task_json, cancel_requested, progress_status,
                       workspace_path, artifact_path
                FROM tasks
                WHERE task_id = ?
                """,
                (task_id,),
            ).fetchone()
        if row is None:
            return None
        return self._decode_fields(dict(row), "workspace_path", "artifact_path")

    def set_running(self, *, task_id: str, workspace_path: str) -> None:
        now = utc_now()
        stored_workspace = self._store_path(workspace_path)
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET execution_status = 'running',
                    workspace_path = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (stored_workspace, now, task_id),
            )

    def set_progress(self, *, task_id: str, status: str) -> None:
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET progress_status = ?, updated_at = ?
                WHERE task_id = ?
                """,
                (status, utc_now(), task_id),
            )

    def request_cancel(
        self,
        *,
        task_id: str,
        source_draft_id: str,
        reason: str | None,
    ) -> bool:
        now = utc_now()
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            inserted = db.execute(
                """
                INSERT OR IGNORE INTO cancel_requests (
                    source_draft_id, task_id, reason, requested_at
                )
                VALUES (?, ?, ?, ?)
                """,
                (source_draft_id, task_id, reason, now),
            ).rowcount
            db.execute(
                """
                UPDATE tasks
                SET cancel_requested = 1,
                    last_error = CASE
                        WHEN ? IS NULL OR ? = '' THEN last_error
                        ELSE ?
                    END,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (reason, reason, reason, now, task_id),
            )
            db.commit()
        return bool(inserted)

    def request_cancel_local(self, *, task_id: str, reason: str) -> None:
        self.request_cancel(
            task_id=task_id,
            source_draft_id=f"local:{task_id}:{utc_now()}",
            reason=reason,
        )

    def is_cancel_requested(self, task_id: str) -> bool:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT (
                    COALESCE(
                        (SELECT cancel_requested FROM tasks WHERE task_id = ?),
                        0
                    ) = 1
                    OR EXISTS (
                        SELECT 1 FROM cancel_requests WHERE task_id = ?
                    )
                ) AS cancel_requested
                """,
                (task_id, task_id),
            ).fetchone()
        return bool(row and int(row["cancel_requested"]))

    def start_step(
        self,
        *,
        task_id: str,
        step_id: str,
        action: str,
        ordinal: int,
        stdout_path: str,
        stderr_path: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO task_steps (
                    task_id, step_id, action, ordinal, execution_status,
                    started_at, finished_at, duration_ms, exit_code, timed_out,
                    stdout_path, stderr_path, error_code, error_message
                )
                VALUES (?, ?, ?, ?, 'running', ?, NULL, NULL, NULL, 0, ?, ?, NULL, NULL)
                ON CONFLICT(task_id, step_id) DO UPDATE SET
                    action = excluded.action,
                    ordinal = excluded.ordinal,
                    execution_status = 'running',
                    started_at = excluded.started_at,
                    finished_at = NULL,
                    duration_ms = NULL,
                    exit_code = NULL,
                    timed_out = 0,
                    stdout_path = excluded.stdout_path,
                    stderr_path = excluded.stderr_path,
                    error_code = NULL,
                    error_message = NULL
                """,
                (
                    task_id,
                    step_id,
                    action,
                    ordinal,
                    now,
                    self._store_path(stdout_path) or stdout_path,
                    self._store_path(stderr_path) or stderr_path,
                ),
            )

    def record_step(self, *, result: Any, stdout_path: str, stderr_path: str) -> None:
        super().record_step(
            result=result,
            stdout_path=self._store_path(stdout_path) or stdout_path,
            stderr_path=self._store_path(stderr_path) or stderr_path,
        )
        with self.connect() as db:
            db.execute(
                """
                UPDATE task_steps SET
                    command_receipt_json = ?, diagnostics_json = ?,
                    diagnostic_warnings_json = ?, workspace_status_before_json = ?,
                    workspace_status_after_json = ?
                WHERE task_id = ? AND step_id = ?
                """,
                (
                    json.dumps(getattr(result, "command_receipt", None), ensure_ascii=False, sort_keys=True) if getattr(result, "command_receipt", None) is not None else None,
                    json.dumps(getattr(result, "diagnostics", None), ensure_ascii=False, sort_keys=True) if getattr(result, "diagnostics", None) is not None else None,
                    json.dumps(getattr(result, "diagnostic_warnings", None), ensure_ascii=False, sort_keys=True) if getattr(result, "diagnostic_warnings", None) is not None else None,
                    json.dumps(getattr(result, "workspace_status_before", None), ensure_ascii=False, sort_keys=True) if getattr(result, "workspace_status_before", None) is not None else None,
                    json.dumps(getattr(result, "workspace_status_after", None), ensure_ascii=False, sort_keys=True) if getattr(result, "workspace_status_after", None) is not None else None,
                    result.task_id, result.step_id,
                ),
            )

    def get_task_steps(self, task_id: str) -> list[dict[str, object]]:
        with self.connect() as db:
            rows = db.execute(
                """
                SELECT task_id, step_id, action, ordinal, execution_status, started_at, finished_at,
                       duration_ms, exit_code, timed_out, stdout_path, stderr_path, error_code, error_message,
                       command_receipt_json, diagnostics_json, diagnostic_warnings_json,
                       workspace_status_before_json, workspace_status_after_json
                FROM task_steps WHERE task_id = ? ORDER BY ordinal ASC
                """, (task_id,),
            ).fetchall()
        output: list[dict[str, object]] = []
        for row in rows:
            item = self._decode_fields(dict(row), "stdout_path", "stderr_path")
            for source, target in (
                ("command_receipt_json", "command_receipt"),
                ("diagnostics_json", "diagnostics"),
                ("diagnostic_warnings_json", "diagnostic_warnings"),
                ("workspace_status_before_json", "workspace_status_before"),
                ("workspace_status_after_json", "workspace_status_after"),
            ):
                raw = item.pop(source, None)
                item[target] = json.loads(str(raw)) if raw else None
            output.append(item)
        return output

    def get_heartbeat(self, runner_id: str) -> dict[str, Any] | None:
        with self.connect() as db:
            row = db.execute("SELECT * FROM runner_heartbeat WHERE runner_id = ?", (runner_id,)).fetchone()
        if row is None:
            return None
        item = dict(row)
        raw = item.pop("details_json", None)
        item["details"] = json.loads(str(raw)) if raw else {}
        return item

    def list_events(self, *, task_id: str, limit: int = 250) -> list[dict[str, Any]]:
        bounded = max(1, min(int(limit), 500))
        with self.connect() as db:
            rows = db.execute(
                """SELECT id, task_id, direction, message_type, external_id, status, created_at, error_message
                   FROM transport_events WHERE task_id = ? ORDER BY id DESC LIMIT ?""",
                (task_id, bounded),
            ).fetchall()
        return [dict(row) for row in reversed(rows)]

    def record_workspace(
        self,
        *,
        task_id: str,
        repository: str,
        mirror_path: str,
        worktree_path: str,
        expected_commit: str,
        actual_commit: str,
        managed: bool,
        workspace_status: str = "ready",
        keep_until: str | None = None,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO workspaces (
                    task_id, repository, mirror_path, worktree_path,
                    expected_commit, actual_commit, workspace_status,
                    managed, created_at, keep_until, updated_at
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(task_id) DO UPDATE SET
                    repository = excluded.repository,
                    mirror_path = excluded.mirror_path,
                    worktree_path = excluded.worktree_path,
                    expected_commit = excluded.expected_commit,
                    actual_commit = excluded.actual_commit,
                    workspace_status = excluded.workspace_status,
                    managed = excluded.managed,
                    keep_until = excluded.keep_until,
                    last_error = NULL,
                    updated_at = excluded.updated_at
                """,
                (
                    task_id,
                    repository,
                    self._store_path(mirror_path),
                    self._store_path(worktree_path),
                    expected_commit,
                    actual_commit,
                    workspace_status,
                    1 if managed else 0,
                    now,
                    keep_until,
                    now,
                ),
            )

    def finalize_workspace(
        self,
        *,
        task_id: str,
        status: str,
        error: str | None = None,
        released: bool = False,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE workspaces
                SET workspace_status = ?,
                    released_at = CASE WHEN ? THEN ? ELSE released_at END,
                    last_error = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (status, 1 if released else 0, now, error, now, task_id),
            )

    def list_workspaces(self, limit: int = 100) -> list[dict[str, Any]]:
        with self.connect() as db:
            rows = db.execute(
                """
                SELECT task_id, repository, mirror_path, worktree_path,
                       expected_commit, actual_commit, workspace_status,
                       managed, created_at, released_at, keep_until,
                       last_error, updated_at
                FROM workspaces
                ORDER BY created_at DESC
                LIMIT ?
                """,
                (limit,),
            ).fetchall()
        return [
            self._decode_fields(dict(row), "mirror_path", "worktree_path")
            for row in rows
        ]

    def record_artifact(
        self,
        *,
        task_id: str,
        local_path: str,
        drive_relative_path: str | None,
        manifest_sha256: str,
        total_bytes: int,
        sync_status: str,
        zip_path: str | None,
    ) -> None:
        now = utc_now()
        stored_local = self._store_path(local_path)
        stored_zip = self._store_path(zip_path)
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO task_artifacts (
                    task_id, local_path, drive_relative_path,
                    manifest_sha256, total_bytes, sync_status,
                    zip_path, created_at, updated_at
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(task_id) DO UPDATE SET
                    local_path = excluded.local_path,
                    drive_relative_path = excluded.drive_relative_path,
                    manifest_sha256 = excluded.manifest_sha256,
                    total_bytes = excluded.total_bytes,
                    sync_status = excluded.sync_status,
                    zip_path = excluded.zip_path,
                    updated_at = excluded.updated_at
                """,
                (
                    task_id,
                    stored_local,
                    drive_relative_path,
                    manifest_sha256,
                    total_bytes,
                    sync_status,
                    stored_zip,
                    now,
                    now,
                ),
            )
            db.execute(
                """
                UPDATE tasks
                SET artifact_path = ?, updated_at = ?
                WHERE task_id = ?
                """,
                (stored_local, now, task_id),
            )

    def get_artifact(self, task_id: str) -> dict[str, Any] | None:
        with self.connect() as db:
            row = db.execute(
                "SELECT * FROM task_artifacts WHERE task_id = ?",
                (task_id,),
            ).fetchone()
        if row is None:
            return None
        return self._decode_fields(dict(row), "local_path", "zip_path")

    def list_recoverable_tasks(self, limit: int = 100) -> list[dict[str, Any]]:
        with self.connect() as db:
            rows = db.execute(
                """
                SELECT task_id, task_json, source_draft_id,
                       execution_status, result_status
                FROM tasks
                WHERE execution_status IN ('claimed', 'running')
                  AND result_status != 'uploaded'
                ORDER BY received_at ASC
                LIMIT ?
                """,
                (limit,),
            ).fetchall()
        return [dict(row) for row in rows]

    def heartbeat(
        self,
        *,
        runner_id: str,
        pid: int | None,
        status: str,
        last_poll_at: str | None = None,
        last_cleanup_at: str | None = None,
        details: dict[str, Any] | None = None,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO runner_heartbeat (
                    runner_id, pid, status, last_poll_at,
                    last_cleanup_at, details_json, updated_at
                )
                VALUES (?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(runner_id) DO UPDATE SET
                    pid = excluded.pid,
                    status = excluded.status,
                    last_poll_at = COALESCE(excluded.last_poll_at, runner_heartbeat.last_poll_at),
                    last_cleanup_at = COALESCE(excluded.last_cleanup_at, runner_heartbeat.last_cleanup_at),
                    details_json = excluded.details_json,
                    updated_at = excluded.updated_at
                """,
                (
                    runner_id,
                    pid,
                    status,
                    last_poll_at,
                    last_cleanup_at,
                    json.dumps(details or {}, ensure_ascii=False, sort_keys=True),
                    now,
                ),
            )
