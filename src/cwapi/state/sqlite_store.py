from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


@dataclass(frozen=True)
class ClaimResult:
    disposition: str
    existing_hash: str | None = None
    existing_status: str | None = None


class SQLiteStateStore:
    def __init__(self, path: Path) -> None:
        self.path = path

    def connect(self) -> sqlite3.Connection:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        connection = sqlite3.connect(self.path)
        connection.row_factory = sqlite3.Row
        connection.execute("PRAGMA journal_mode=WAL")
        connection.execute("PRAGMA foreign_keys=ON")
        return connection

    def initialize(self) -> None:
        with self.connect() as db:
            db.executescript(
                """
                CREATE TABLE IF NOT EXISTS tasks (
                    task_id TEXT PRIMARY KEY,
                    content_sha256 TEXT NOT NULL,
                    source_draft_id TEXT NOT NULL,
                    repository TEXT NOT NULL,
                    expected_commit TEXT NOT NULL,
                    execution_status TEXT NOT NULL,
                    result_status TEXT NOT NULL,
                    received_at TEXT NOT NULL,
                    finished_at TEXT,
                    result_draft_id TEXT,
                    last_error TEXT,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS transport_events (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    task_id TEXT,
                    direction TEXT NOT NULL,
                    message_type TEXT NOT NULL,
                    external_id TEXT,
                    status TEXT NOT NULL,
                    created_at TEXT NOT NULL,
                    error_message TEXT
                );

                CREATE TABLE IF NOT EXISTS task_steps (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    task_id TEXT NOT NULL,
                    step_id TEXT NOT NULL,
                    action TEXT NOT NULL,
                    ordinal INTEGER NOT NULL,
                    execution_status TEXT NOT NULL,
                    started_at TEXT,
                    finished_at TEXT,
                    duration_ms INTEGER,
                    exit_code INTEGER,
                    timed_out INTEGER,
                    stdout_path TEXT,
                    stderr_path TEXT,
                    error_code TEXT,
                    error_message TEXT
                );

                CREATE TABLE IF NOT EXISTS result_outbox (
                    task_id TEXT PRIMARY KEY,
                    subject TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    final_execution_status TEXT NOT NULL,
                    delivery_status TEXT NOT NULL DEFAULT 'pending',
                    attempt_count INTEGER NOT NULL DEFAULT 0,
                    last_attempt_at TEXT,
                    delivered_at TEXT,
                    gmail_draft_id TEXT,
                    last_error TEXT,
                    execution_error TEXT,
                    summary_subject TEXT,
                    summary_payload_json TEXT,
                    summary_delivery_status TEXT NOT NULL DEFAULT 'not_required',
                    summary_attempt_count INTEGER NOT NULL DEFAULT 0,
                    summary_last_attempt_at TEXT,
                    summary_delivered_at TEXT,
                    summary_gmail_draft_id TEXT,
                    summary_last_error TEXT,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                -- task_steps idempotency: unique per (task_id, step_id)
                DELETE FROM task_steps
                WHERE id NOT IN (
                    SELECT MIN(id) FROM task_steps GROUP BY task_id, step_id
                );

                CREATE UNIQUE INDEX IF NOT EXISTS
                ux_task_steps_task_step
                ON task_steps(task_id, step_id);

                CREATE INDEX IF NOT EXISTS
                ix_task_steps_task_ordinal
                ON task_steps(task_id, ordinal);

                CREATE TABLE IF NOT EXISTS rejected_inputs (
                    source_draft_id TEXT NOT NULL,
                    raw_content_sha256 TEXT NOT NULL,
                    task_id TEXT NOT NULL,
                    reason TEXT NOT NULL,
                    result_draft_id TEXT NOT NULL,
                    rejected_at TEXT NOT NULL,
                    PRIMARY KEY (source_draft_id, raw_content_sha256)
                );
                """
            )

            outbox_columns = {
                str(row["name"])
                for row in db.execute("PRAGMA table_info(result_outbox)").fetchall()
            }
            additive_outbox_columns = {
                "execution_error": "TEXT",
                "summary_subject": "TEXT",
                "summary_payload_json": "TEXT",
                "summary_delivery_status": "TEXT NOT NULL DEFAULT 'not_required'",
                "summary_attempt_count": "INTEGER NOT NULL DEFAULT 0",
                "summary_last_attempt_at": "TEXT",
                "summary_delivered_at": "TEXT",
                "summary_gmail_draft_id": "TEXT",
                "summary_last_error": "TEXT",
            }
            for column, declaration in additive_outbox_columns.items():
                if column not in outbox_columns:
                    db.execute(
                        f"ALTER TABLE result_outbox ADD COLUMN {column} {declaration}"
                    )

    def get_task(self, task_id: str) -> dict[str, Any] | None:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT task_id, content_sha256, source_draft_id,
                       execution_status, result_status, result_draft_id,
                       finished_at, last_error, updated_at
                FROM tasks
                WHERE task_id = ?
                """,
                (task_id,),
            ).fetchone()
        return dict(row) if row is not None else None

    def claim_task(
        self,
        *,
        task_id: str,
        content_hash: str,
        source_draft_id: str,
        repository: str,
        expected_commit: str,
    ) -> ClaimResult:
        now = utc_now()
        with self.connect() as db:
            db.execute("BEGIN IMMEDIATE")
            existing = db.execute(
                "SELECT content_sha256, execution_status FROM tasks WHERE task_id = ?",
                (task_id,),
            ).fetchone()

            if existing is not None:
                existing_hash = str(existing["content_sha256"])
                existing_status = str(existing["execution_status"])
                db.commit()
                if existing_hash != content_hash:
                    return ClaimResult(
                        "hash_conflict",
                        existing_hash=existing_hash,
                        existing_status=existing_status,
                    )
                return ClaimResult(
                    "duplicate",
                    existing_hash=existing_hash,
                    existing_status=existing_status,
                )

            rejected = db.execute(
                """
                SELECT raw_content_sha256
                FROM rejected_inputs
                WHERE task_id = ?
                ORDER BY rejected_at ASC
                LIMIT 1
                """,
                (task_id,),
            ).fetchone()
            if rejected is not None:
                db.commit()
                return ClaimResult(
                    "duplicate",
                    existing_hash=str(rejected["raw_content_sha256"]),
                    existing_status="rejected",
                )

            db.execute(
                """
                INSERT INTO tasks (
                    task_id, content_sha256, source_draft_id, repository,
                    expected_commit, execution_status, result_status,
                    received_at, updated_at
                )
                VALUES (?, ?, ?, ?, ?, 'claimed', 'not_ready', ?, ?)
                """,
                (
                    task_id,
                    content_hash,
                    source_draft_id,
                    repository,
                    expected_commit,
                    now,
                    now,
                ),
            )
            db.commit()
            return ClaimResult("claimed")

    def has_rejected_input(
        self,
        *,
        source_draft_id: str,
        raw_content_hash: str,
    ) -> bool:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT 1
                FROM rejected_inputs
                WHERE source_draft_id = ?
                  AND raw_content_sha256 = ?
                """,
                (source_draft_id, raw_content_hash),
            ).fetchone()
        return row is not None

    def record_rejected_input(
        self,
        *,
        source_draft_id: str,
        raw_content_hash: str,
        task_id: str,
        reason: str,
        result_draft_id: str,
    ) -> None:
        with self.connect() as db:
            db.execute(
                """
                INSERT OR IGNORE INTO rejected_inputs (
                    source_draft_id, raw_content_sha256, task_id,
                    reason, result_draft_id, rejected_at
                )
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                (
                    source_draft_id,
                    raw_content_hash,
                    task_id,
                    reason,
                    result_draft_id,
                    utc_now(),
                ),
            )

    def mark_completed(
        self,
        *,
        task_id: str,
        result_draft_id: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET execution_status = 'completed',
                    result_status = 'uploaded',
                    finished_at = ?,
                    result_draft_id = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (now, result_draft_id, now, task_id),
            )

    def record_step(self, *, result, stdout_path: str, stderr_path: str) -> None:
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO task_steps (
                    task_id, step_id, action, ordinal,
                    execution_status, started_at, finished_at,
                    duration_ms, exit_code, timed_out,
                    stdout_path, stderr_path, error_code, error_message
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(task_id, step_id) DO UPDATE SET
                    action=excluded.action,
                    ordinal=excluded.ordinal,
                    execution_status=excluded.execution_status,
                    started_at=excluded.started_at,
                    finished_at=excluded.finished_at,
                    duration_ms=excluded.duration_ms,
                    exit_code=excluded.exit_code,
                    timed_out=excluded.timed_out,
                    stdout_path=excluded.stdout_path,
                    stderr_path=excluded.stderr_path,
                    error_code=excluded.error_code,
                    error_message=excluded.error_message
                """,
                (
                    result.task_id,
                    result.step_id,
                    result.action,
                    result.ordinal,
                    result.execution_status,
                    result.started_at,
                    result.finished_at,
                    result.duration_ms,
                    result.exit_code,
                    1 if result.timed_out else 0,
                    stdout_path,
                    stderr_path,
                    result.error_code,
                    result.error_message,
                ),
            )

    def get_task_steps(self, task_id: str) -> list[dict[str, object]]:
        with self.connect() as db:
            rows = db.execute(
                """
                SELECT task_id, step_id, action, ordinal,
                       execution_status, started_at, finished_at,
                       duration_ms, exit_code, timed_out,
                       stdout_path, stderr_path, error_code, error_message
                FROM task_steps
                WHERE task_id = ?
                ORDER BY ordinal ASC
                """,
                (task_id,),
            ).fetchall()
        return [dict(row) for row in rows]

    def mark_failed(self, *, task_id: str, error: str) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET execution_status = 'failed',
                    result_status = 'not_ready',
                    finished_at = ?,
                    last_error = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (now, error, now, task_id),
            )

    def mark_result_pending(self, *, task_id: str, execution_status: str, error: str | None = None) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET execution_status = ?,
                    result_status = 'pending',
                    finished_at = ?,
                    result_draft_id = NULL,
                    last_error = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (execution_status, now, error or "", now, task_id),
            )

    def finalize_failed_result(
        self,
        *,
        task_id: str,
        result_draft_id: str,
        error: str | None = None,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE tasks
                SET execution_status = 'failed',
                    result_status = 'uploaded',
                    finished_at = ?,
                    result_draft_id = ?,
                    last_error = ?,
                    updated_at = ?
                WHERE task_id = ?
            """,
                (now, result_draft_id, error or '', now, task_id),
            )
    
    
    def record_event(
        self,
        *,
        task_id: str | None,
        direction: str,
        message_type: str,
        external_id: str | None,
        status: str,
        error_message: str | None = None,
    ) -> None:
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO transport_events (
                    task_id, direction, message_type, external_id,
                    status, created_at, error_message
                )
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    task_id,
                    direction,
                    message_type,
                    external_id,
                    status,
                    utc_now(),
                    error_message,
                ),
            )

    # ── result_outbox methods ──

    def save_result_outbox(
        self,
        *,
        task_id: str,
        subject: str,
        payload_json: str,
        final_execution_status: str,
        execution_error: str | None = None,
        summary_subject: str | None = None,
        summary_payload_json: str | None = None,
    ) -> None:
        self.save_or_update_outbox(
            task_id=task_id,
            subject=subject,
            payload_json=payload_json,
            final_execution_status=final_execution_status,
            execution_error=execution_error,
            summary_subject=summary_subject,
            summary_payload_json=summary_payload_json,
        )

    def save_or_update_outbox(
        self,
        *,
        task_id: str,
        subject: str,
        payload_json: str,
        final_execution_status: str,
        execution_error: str | None = None,
        summary_subject: str | None = None,
        summary_payload_json: str | None = None,
    ) -> None:
        if (summary_subject is None) != (summary_payload_json is None):
            raise ValueError("summary subject and payload must be stored together")
        summary_delivery_status = (
            "pending" if summary_subject is not None else "not_required"
        )
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                INSERT INTO result_outbox (
                    task_id, subject, payload_json, final_execution_status,
                    delivery_status, attempt_count, last_attempt_at,
                    delivered_at, gmail_draft_id, last_error,
                    execution_error, summary_subject, summary_payload_json,
                    summary_delivery_status, summary_attempt_count,
                    summary_last_attempt_at, summary_delivered_at,
                    summary_gmail_draft_id, summary_last_error,
                    created_at, updated_at
                )
                VALUES (
                    ?, ?, ?, ?, 'pending', 0, NULL, NULL, NULL, NULL,
                    ?, ?, ?, ?, 0, NULL, NULL, NULL, NULL, ?, ?
                )
                ON CONFLICT(task_id) DO UPDATE SET
                    subject = excluded.subject,
                    payload_json = excluded.payload_json,
                    final_execution_status = excluded.final_execution_status,
                    delivery_status = 'pending',
                    delivered_at = NULL,
                    gmail_draft_id = NULL,
                    last_error = NULL,
                    execution_error = excluded.execution_error,
                    summary_subject = excluded.summary_subject,
                    summary_payload_json = excluded.summary_payload_json,
                    summary_delivery_status = excluded.summary_delivery_status,
                    summary_delivered_at = NULL,
                    summary_gmail_draft_id = NULL,
                    summary_last_error = NULL,
                    updated_at = excluded.updated_at
                """,
                (
                    task_id,
                    subject,
                    payload_json,
                    final_execution_status,
                    execution_error,
                    summary_subject,
                    summary_payload_json,
                    summary_delivery_status,
                    now,
                    now,
                ),
            )

    def get_pending_result(self, task_id: str) -> dict[str, object] | None:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT task_id, subject, payload_json, final_execution_status,
                       delivery_status, attempt_count, last_attempt_at,
                       delivered_at, gmail_draft_id, last_error,
                       execution_error, summary_subject, summary_payload_json,
                       summary_delivery_status, summary_attempt_count,
                       summary_last_attempt_at, summary_delivered_at,
                       summary_gmail_draft_id, summary_last_error,
                       created_at, updated_at
                FROM result_outbox
                WHERE task_id = ? AND (
                    delivery_status = 'pending'
                    OR summary_delivery_status = 'pending'
                )
                """,
                (task_id,),
            ).fetchone()
        return dict(row) if row is not None else None

    def get_outbox_result(self, task_id: str) -> dict[str, object] | None:
        with self.connect() as db:
            row = db.execute(
                """
                SELECT task_id, subject, payload_json, final_execution_status,
                       delivery_status, attempt_count, last_attempt_at,
                       delivered_at, gmail_draft_id, last_error,
                       execution_error, summary_subject, summary_payload_json,
                       summary_delivery_status, summary_attempt_count,
                       summary_last_attempt_at, summary_delivered_at,
                       summary_gmail_draft_id, summary_last_error,
                       created_at, updated_at
                FROM result_outbox
                WHERE task_id = ?
                """,
                (task_id,),
            ).fetchone()
        return dict(row) if row is not None else None

    def list_pending_results(self, limit: int = 50) -> list[dict[str, object]]:
        with self.connect() as db:
            rows = db.execute(
                """
                SELECT task_id, subject, payload_json, final_execution_status,
                       delivery_status, attempt_count, last_attempt_at,
                       delivered_at, gmail_draft_id, last_error,
                       execution_error, summary_subject, summary_payload_json,
                       summary_delivery_status, summary_attempt_count,
                       summary_last_attempt_at, summary_delivered_at,
                       summary_gmail_draft_id, summary_last_error,
                       created_at, updated_at
                FROM result_outbox
                WHERE delivery_status = 'pending'
                   OR summary_delivery_status = 'pending'
                ORDER BY created_at ASC
                LIMIT ?
                """,
                (limit,),
            ).fetchall()
        return [dict(row) for row in rows]

    def mark_result_delivery_attempt(self, *, task_id: str) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE result_outbox
                SET attempt_count = attempt_count + 1,
                    last_attempt_at = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (now, now, task_id),
            )

    def mark_result_delivered(
        self,
        *,
        task_id: str,
        gmail_draft_id: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE result_outbox
                SET delivery_status = 'delivered',
                    gmail_draft_id = ?,
                    delivered_at = ?,
                    last_error = NULL,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (gmail_draft_id, now, now, task_id),
            )

    def mark_result_delivery_failed(
        self,
        *,
        task_id: str,
        error: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE result_outbox
                SET delivery_status = 'pending',
                    last_error = ?,
                    updated_at = ?
                WHERE task_id = ?
                """,
                (error[:500], now, task_id),
            )

    def mark_summary_delivery_attempt(self, *, task_id: str) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE result_outbox
                SET summary_attempt_count = summary_attempt_count + 1,
                    summary_last_attempt_at = ?,
                    updated_at = ?
                WHERE task_id = ? AND summary_delivery_status = 'pending'
                """,
                (now, now, task_id),
            )

    def mark_summary_delivered(
        self,
        *,
        task_id: str,
        gmail_draft_id: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE result_outbox
                SET summary_delivery_status = 'delivered',
                    summary_gmail_draft_id = ?,
                    summary_delivered_at = ?,
                    summary_last_error = NULL,
                    updated_at = ?
                WHERE task_id = ? AND summary_delivery_status = 'pending'
                """,
                (gmail_draft_id, now, now, task_id),
            )

    def mark_summary_delivery_failed(
        self,
        *,
        task_id: str,
        error: str,
    ) -> None:
        now = utc_now()
        with self.connect() as db:
            db.execute(
                """
                UPDATE result_outbox
                SET summary_delivery_status = 'pending',
                    summary_last_error = ?,
                    updated_at = ?
                WHERE task_id = ? AND summary_delivery_status = 'pending'
                """,
                (error[:500], now, task_id),
            )

    def list_tasks(self, limit: int = 50) -> list[dict[str, object]]:
        with self.connect() as db:
            rows = db.execute(
                """
                SELECT task_id, repository, expected_commit, execution_status,
                       result_status, received_at, finished_at, last_error
                FROM tasks
                ORDER BY received_at DESC
                LIMIT ?
                """,
                (limit,),
            ).fetchall()
        return [dict(row) for row in rows]
