from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .config import AppConfig
from .execution.action_registry import resolve_action, UnknownActionError
from .execution.process_runner import run_process
from .execution.result_capture import (
    ExecutionResult,
    StepResult,
    build_result_payload,
    build_step_payload,
)
from .gpt_contract import GPT_RESULT_SUMMARY_SCHEMA
from .gpt_errors import build_gpt_error
from .git import GitRuntimeError, resolve_configured_git
from .hashing import content_sha256
from .models import TaskValidationError, parse_and_validate_task
from .result_summary import build_result_summary
from .security import safe_error
from .state.runtime_store import RuntimeStateStore
from .state.sqlite_store import SQLiteStateStore
from .subjects import build_subject, parse_subject
from .transports.gmail_drafts import GmailDraftTransport


class Phase1Runner:
    """Backward-compatible runner name; runtime mode provides the v1 executor."""

    def __init__(self, config: AppConfig) -> None:
        self.config = config
        try:
            self.git_runtime = resolve_configured_git(config.git)
        except GitRuntimeError as exc:
            raise RuntimeError(str(exc)) from exc
        store_type = RuntimeStateStore if config.runtime.enabled else SQLiteStateStore
        self.store = store_type(config.state.database_path)
        self.transport = GmailDraftTransport(
            account=config.gmail.account,
            credentials_path=config.gmail.credentials_path,
            token_path=config.gmail.token_path,
        )
        self.advanced = None
        if config.runtime.enabled:
            from .runtime import AdvancedTaskExecutor

            self.advanced = AdvancedTaskExecutor(
                config=config,
                store=self.store,
                deliver_result=self._deliver_or_outbox,
                publish_progress=self._publish_progress,
            )

    def _resolve_project_path(self, repository: str) -> str:
        try:
            return str(self.config.projects.mapping[repository])
        except KeyError as exc:
            raise RuntimeError(f"未知项目：{repository}") from exc

    def run_once(self) -> dict[str, int]:
        self.store.initialize()
        cancel_count = self.poll_cancellations() if self.config.runtime.enabled else 0
        retry_counts = self._retry_pending_results()

        drafts = self.transport.list_drafts(
            query=self.config.gmail.task_query,
            max_results=self.config.gmail.max_results,
        )
        counts = {
            "seen": len(drafts),
            "claimed": 0,
            "completed": 0,
            "failed": 0,
            "duplicate": 0,
            "rejected": 0,
            "result_retried": retry_counts.get("retried", 0),
            "result_pending": retry_counts.get("still_pending", 0),
            "cancel_requested": cancel_count,
        }

        for draft in drafts[: self.config.runner.max_tasks_per_poll]:
            try:
                self._process_draft(draft, counts)
            except Exception as exc:
                parsed = parse_subject(draft.subject)
                task_id = parsed.entity_id if parsed is not None else None
                self.store.record_event(
                    task_id=task_id,
                    direction="internal",
                    message_type="TASK",
                    external_id=draft.draft_id,
                    status="processing_failed",
                    error_message=safe_error(exc, limit=200),
                )
                counts["failed"] += 1
        return counts

    def _process_draft(self, draft: Any, counts: dict[str, int]) -> None:
        parsed = parse_subject(draft.subject)
        if (
            parsed is None
            or parsed.message_type != "TASK"
            or parsed.status != "PENDING"
        ):
            return

        raw_content_hash = content_sha256(
            {"subject": draft.subject, "body": draft.body}
        )
        existing = self.store.get_task(parsed.entity_id)
        if existing is not None:
            exec_status = str(existing.get("execution_status", ""))
            result_status = str(existing.get("result_status", ""))
            same_draft = existing["source_draft_id"] == draft.draft_id
            if result_status == "uploaded":
                counts["duplicate"] += 1
                return
            if (
                result_status == "pending"
                and self.store.get_pending_result(parsed.entity_id)
            ):
                counts["duplicate"] += 1
                return
            if (
                exec_status in {"claimed", "running"}
                and not self.store.get_pending_result(parsed.entity_id)
            ):
                reason = (
                    "INTERRUPTED_EXECUTION: task execution interrupted; "
                    "resubmit with a new task_id."
                )
                payload = {
                    "schema": "cwapi.result.v1",
                    "task_id": parsed.entity_id,
                    "runner_id": self.config.runner.runner_id,
                    "status": "failed",
                    "overall_status": "failed",
                    "error_code": "INTERRUPTED_EXECUTION",
                    "error_message": reason,
                    "finished_at": datetime.now(timezone.utc).isoformat(),
                }
                self._deliver_or_outbox(
                    task_id=parsed.entity_id,
                    subject_status="FAILED",
                    payload=payload,
                    final_execution_status="failed",
                    error=reason,
                )
                counts["failed"] += 1
                return
            if same_draft:
                counts["duplicate"] += 1
                return

        try:
            envelope = parse_and_validate_task(
                draft.body,
                subject_task_id=parsed.entity_id,
                runner_id=self.config.runner.runner_id,
                channel_ids=set(self.config.runner.channel_ids),
                allowed_repositories=set(
                    self.config.security.allowed_repositories
                ),
                allowed_actions=set(self.config.security.allowed_actions),
                max_step_timeout_seconds=self.config.security.max_step_timeout_seconds,
                max_task_steps=self.config.security.max_task_steps,
                max_relative_paths=self.config.security.max_relative_paths,
                projects=self.config.projects,
            )
        except TaskValidationError as exc:
            if self.store.has_rejected_input(
                source_draft_id=draft.draft_id,
                raw_content_hash=raw_content_hash,
            ):
                counts["duplicate"] += 1
                return
            reason = str(exc)
            result_draft_id = self._safe_publish_rejected(
                task_id=parsed.entity_id,
                source_draft_id=draft.draft_id,
                reason=reason,
            )
            self.store.record_rejected_input(
                source_draft_id=draft.draft_id,
                raw_content_hash=raw_content_hash,
                task_id=parsed.entity_id,
                reason=reason,
                result_draft_id=result_draft_id,
            )
            counts["rejected"] += 1
            return

        task = envelope.task
        claim = self.store.claim_task(
            task_id=envelope.task_id,
            content_hash=envelope.content_hash,
            source_draft_id=draft.draft_id,
            repository=str(task["repository"]),
            expected_commit=str(task["expected_commit"]),
        )
        if claim.disposition == "duplicate":
            counts["duplicate"] += 1
            return
        if claim.disposition == "hash_conflict":
            reason = "TASK_ID_REUSE_CONFLICT：同一 task_id 的任务内容发生变化。"
            if not self.store.has_rejected_input(
                source_draft_id=draft.draft_id,
                raw_content_hash=raw_content_hash,
            ):
                result_draft_id = self._safe_publish_rejected(
                    task_id=envelope.task_id,
                    source_draft_id=draft.draft_id,
                    reason=reason,
                )
                self.store.record_rejected_input(
                    source_draft_id=draft.draft_id,
                    raw_content_hash=raw_content_hash,
                    task_id=envelope.task_id,
                    reason=reason,
                    result_draft_id=result_draft_id,
                )
            counts["rejected"] += 1
            return

        if isinstance(self.store, RuntimeStateStore):
            self.store.attach_task_payload(task_id=envelope.task_id, task=task)

        counts["claimed"] += 1
        try:
            self._publish_ack(
                task_id=envelope.task_id,
                content_hash=envelope.content_hash,
                source_draft_id=draft.draft_id,
            )
        except Exception as exc:
            self.store.record_event(
                task_id=envelope.task_id,
                direction="outbound",
                message_type="ACK",
                external_id=None,
                status="delivery_failed",
                error_message=safe_error(exc, limit=200),
            )

        steps = list(task.get("steps", []))
        has_dry_run = any(step.get("action") == "dry_run" for step in steps)
        has_real = any(step.get("action") != "dry_run" for step in steps)
        if has_dry_run and has_real:
            reason = "MIXED_DRY_RUN_ACTIONS：dry_run 与真实 action 不能混合。"
            result_draft_id = self._safe_publish_rejected(
                task_id=envelope.task_id,
                source_draft_id=draft.draft_id,
                reason=reason,
            )
            self.store.record_rejected_input(
                source_draft_id=draft.draft_id,
                raw_content_hash=raw_content_hash,
                task_id=envelope.task_id,
                reason=reason,
                result_draft_id=result_draft_id,
            )
            counts["rejected"] += 1
            return

        if has_dry_run and not has_real:
            result_draft_id = self._publish_dry_run_result(
                task=task,
                content_hash=envelope.content_hash,
            )
            if result_draft_id:
                counts["completed"] += 1
            else:
                counts["result_pending"] += 1
            return

        try:
            result_draft_id, overall_status = self._execute_task(
                envelope,
                draft.draft_id,
            )
        except Exception as exc:
            error = safe_error(exc)
            payload = {
                "schema": "cwapi.result.v1",
                "task_id": envelope.task_id,
                "runner_id": self.config.runner.runner_id,
                "repository": str(task["repository"]),
                "expected_commit": str(task["expected_commit"]),
                "overall_status": "failed",
                "steps": [],
                "exit_code": 1,
                "duration_ms": 0,
                "stdout_summary": "",
                "stderr_summary": error,
                "error_code": "INTERNAL_RUNNER_ERROR",
                "error_message": error,
                "source_draft_id": draft.draft_id,
                "finished_at": datetime.now(timezone.utc).isoformat(),
            }
            result_draft_id = self._deliver_or_outbox(
                task_id=envelope.task_id,
                subject_status="FAILED",
                payload=payload,
                final_execution_status="failed",
                error=error,
            )
            overall_status = "failed" if result_draft_id else "result_pending"

        if overall_status == "completed":
            counts["completed"] += 1
        elif overall_status == "result_pending":
            counts["result_pending"] += 1
        else:
            counts["failed"] += 1

    def _git_rev_parse_project(self, project_path: str) -> str:
        result = run_process(
            self.git_runtime.command("rev-parse", "HEAD"),
            timeout_seconds=30,
            cwd=project_path,
            environment=self.git_runtime.environment(),
            git_executable=str(self.git_runtime.executable),
        )
        if result.exit_code != 0:
            raise RuntimeError(f"git rev-parse HEAD 失败：{result.stderr}")
        return result.stdout.strip()

    def _write_log_file(
        self,
        task_id: str,
        step: StepResult,
    ) -> tuple[str, str]:
        log_dir = (
            Path(self.config.state.database_path).parent.parent / "logs" / task_id
        )
        log_dir.mkdir(parents=True, exist_ok=True)
        stdout_path = str(log_dir / f"{step.step_id}_stdout.log")
        stderr_path = str(log_dir / f"{step.step_id}_stderr.log")
        Path(stdout_path).write_text(step.stdout, encoding="utf-8")
        Path(stderr_path).write_text(step.stderr, encoding="utf-8")
        return stdout_path, stderr_path

    def _execute_task(
        self,
        envelope: Any,
        source_draft_id: str,
    ) -> tuple[str, str]:
        if self.advanced is not None:
            return self.advanced.execute(envelope, source_draft_id)
        return self._execute_legacy_task(envelope, source_draft_id)

    def _execute_legacy_task(
        self,
        envelope: Any,
        source_draft_id: str,
    ) -> tuple[str, str]:
        task = envelope.task
        task_id = str(task["task_id"])
        repository = str(task["repository"])
        expected_commit = str(task["expected_commit"])
        project_path = self._resolve_project_path(repository)
        actual_commit = self._git_rev_parse_project(project_path)

        if actual_commit != expected_commit:
            error = f"Expected commit {expected_commit}, actual {actual_commit}"
            payload = {
                "schema": "cwapi.result.v1",
                "task_id": task_id,
                "runner_id": self.config.runner.runner_id,
                "status": "failed",
                "execution_mode": "validation",
                "expected_commit": expected_commit,
                "actual_commit": actual_commit,
                "content_sha256": envelope.content_hash,
                "error_code": "EXPECTED_COMMIT_MISMATCH",
                "error_message": error,
                "finished_at": datetime.now(timezone.utc).isoformat(),
            }
            draft_id = self._deliver_or_outbox(
                task_id=task_id,
                subject_status="FAILED",
                payload=payload,
                final_execution_status="failed",
                error=error,
            )
            return draft_id, "failed" if draft_id else "result_pending"

        step_payloads: list[dict[str, Any]] = []
        step_objects: list[StepResult] = []
        started_at = datetime.now(timezone.utc)
        overall_status = "completed"
        execution_error: str | None = None

        for ordinal, step_spec in enumerate(task.get("steps", [])):
            step_id = str(step_spec["step_id"])
            action = str(step_spec["action"])
            arguments = dict(step_spec.get("arguments", {}))
            timeout_seconds = int(step_spec.get("timeout_seconds", 60))
            try:
                command = resolve_action(
                    action,
                    arguments,
                    git_executable=str(self.git_runtime.executable),
                )
                result = run_process(
                    command,
                    timeout_seconds=timeout_seconds,
                    cwd=project_path,
                    task_id=task_id,
                    step_id=step_id,
                    action=action,
                    ordinal=ordinal,
                    environment=self.git_runtime.environment(),
                    git_executable=str(self.git_runtime.executable),
                )
            except UnknownActionError:
                now = datetime.now(timezone.utc).isoformat()
                result = StepResult(
                    task_id=task_id,
                    step_id=step_id,
                    action=action,
                    ordinal=ordinal,
                    execution_status="failed",
                    started_at=now,
                    finished_at=now,
                    duration_ms=0,
                    exit_code=None,
                    timed_out=False,
                    stdout="",
                    stderr="",
                    error_code="UNKNOWN_ACTION",
                    error_message=f"Unknown action: {action}",
                )

            stdout_path, stderr_path = self._write_log_file(task_id, result)
            self.store.record_step(
                result=result,
                stdout_path=stdout_path,
                stderr_path=stderr_path,
            )
            step_objects.append(result)
            step_payloads.append(
                build_step_payload(
                    result,
                    stdout_path=stdout_path,
                    stderr_path=stderr_path,
                )
            )
            if result.execution_status != "completed":
                overall_status = "failed"
                execution_error = (
                    result.error_message or result.stderr or f"{action} failed"
                )
                if not bool(task.get("continue_on_failure", False)):
                    break

        finished_at = datetime.now(timezone.utc)
        duration_ms = max(
            1,
            int((finished_at - started_at).total_seconds() * 1000),
        )
        stdout_summary = "\n".join(
            item.stdout[:200] for item in step_objects[-3:] if item.stdout
        )[:200]
        stderr_summary = "\n".join(
            item.stderr[:200] for item in step_objects[-3:] if item.stderr
        )[:200]
        result = ExecutionResult(
            task_id=task_id,
            runner_id=self.config.runner.runner_id,
            repository=repository,
            expected_commit=expected_commit,
            actual_commit=actual_commit,
            overall_status=overall_status,
            steps=step_payloads,
            exit_code=0 if overall_status == "completed" else 1,
            duration_ms=duration_ms,
            stdout_summary=stdout_summary,
            stderr_summary=stderr_summary,
            log_path=str(
                Path(self.config.state.database_path).parent.parent
                / "logs"
                / task_id
            ),
            source_draft_id=source_draft_id,
        )
        payload = build_result_payload(result)
        draft_id = self._deliver_or_outbox(
            task_id=task_id,
            subject_status=(
                "COMPLETED" if overall_status == "completed" else "FAILED"
            ),
            payload=payload,
            final_execution_status=(
                "completed" if overall_status == "completed" else "failed"
            ),
            error=execution_error,
        )
        if not draft_id:
            return "", "result_pending"
        return draft_id, overall_status

    def _publish_real_result(
        self,
        *,
        subject_status: str,
        payload: dict[str, Any],
    ) -> str:
        normalized_subject_status = str(subject_status).strip().upper()
        completed = normalized_subject_status in {"COMPLETED", "DRY_RUN"}
        return self._deliver_or_outbox(
            task_id=str(payload["task_id"]),
            subject_status=normalized_subject_status,
            payload=payload,
            final_execution_status="completed" if completed else "failed",
            error=(
                str(payload.get("error_message") or "") or None
                if not completed
                else None
            ),
        )

    def _publish_ack(
        self,
        *,
        task_id: str,
        content_hash: str,
        source_draft_id: str,
    ) -> str:
        payload = {
            "schema": "cwapi.ack.v1",
            "task_id": task_id,
            "runner_id": self.config.runner.runner_id,
            "status": "claimed",
            "claimed_at": datetime.now(timezone.utc).isoformat(),
            "content_sha256": content_hash,
            "source_draft_id": source_draft_id,
        }
        draft_id = self.transport.create_draft(
            subject=build_subject("ACK", "CLAIMED", task_id),
            body=json.dumps(payload, ensure_ascii=False, indent=2),
        )
        self.store.record_event(
            task_id=task_id,
            direction="outbound",
            message_type="ACK",
            external_id=draft_id,
            status="created",
        )
        return draft_id

    def _build_dry_run_payload(
        self,
        *,
        task: dict[str, Any],
        content_hash: str,
    ) -> dict[str, Any]:
        now = datetime.now(timezone.utc).isoformat()
        return {
            "schema": "cwapi.result.v1",
            "task_id": task["task_id"],
            "runner_id": self.config.runner.runner_id,
            "status": "completed",
            "execution_mode": "dry_run",
            "source_commit": task["expected_commit"],
            "content_sha256": content_hash,
            "started_at": now,
            "finished_at": now,
            "summary": {
                "success": True,
                "message": "任务已完成校验；未执行本地命令。",
            },
            "accepted_steps": [
                {
                    "step_id": step["step_id"],
                    "action": step["action"],
                    "status": "validated",
                }
                for step in task["steps"]
            ],
        }

    def _publish_dry_run_result(
        self,
        *,
        task: dict[str, Any],
        content_hash: str,
    ) -> str:
        payload = self._build_dry_run_payload(
            task=task,
            content_hash=content_hash,
        )
        return self._deliver_or_outbox(
            task_id=str(task["task_id"]),
            subject_status="DRY_RUN",
            payload=payload,
            final_execution_status="completed",
        )

    def _safe_publish_rejected(
        self,
        *,
        task_id: str,
        source_draft_id: str,
        reason: str,
    ) -> str:
        try:
            return self._publish_rejected(
                task_id=task_id,
                source_draft_id=source_draft_id,
                reason=reason,
            )
        except Exception as exc:
            self.store.record_event(
                task_id=task_id,
                direction="outbound",
                message_type="RESULT",
                external_id=None,
                status="rejected_delivery_failed",
                error_message=safe_error(exc, limit=200),
            )
            return ""

    def _publish_rejected(
        self,
        *,
        task_id: str,
        source_draft_id: str,
        reason: str,
    ) -> str:
        payload = {
            "schema": "cwapi.result.v1",
            "task_id": task_id,
            "runner_id": self.config.runner.runner_id,
            "status": "rejected",
            "execution_mode": "none",
            "finished_at": datetime.now(timezone.utc).isoformat(),
            "source_draft_id": source_draft_id,
            "summary": {"success": False, "reason": reason},
        }
        return self._deliver_or_outbox(
            task_id=task_id,
            subject_status="REJECTED",
            payload=payload,
            final_execution_status="failed",
            error=reason,
        )

    def _publish_progress(
        self,
        *,
        task_id: str,
        status: str,
        payload: dict[str, Any],
    ) -> str | None:
        if not self.config.runtime.publish_progress:
            return None
        draft_id = self.transport.create_draft(
            subject=build_subject("PROGRESS", status, task_id),
            body=json.dumps(payload, ensure_ascii=False, indent=2),
        )
        self.store.record_event(
            task_id=task_id,
            direction="outbound",
            message_type="PROGRESS",
            external_id=draft_id,
            status="created",
        )
        return draft_id

    def poll_cancellations(
        self,
        *,
        transport: GmailDraftTransport | None = None,
    ) -> int:
        if not isinstance(self.store, RuntimeStateStore):
            return 0
        active_transport = transport or self.transport
        drafts = active_transport.list_drafts(
            query=self.config.gmail.cancel_query,
            max_results=self.config.gmail.max_results,
        )
        recorded = 0
        for draft in drafts:
            parsed = parse_subject(draft.subject)
            if (
                parsed is None
                or parsed.message_type != "CANCEL"
                or parsed.status != "REQUESTED"
            ):
                continue
            reason = "Cancellation requested through Gmail draft."
            try:
                body = json.loads(draft.body)
                if isinstance(body, dict):
                    body_task_id = body.get("task_id")
                    if body_task_id and str(body_task_id) != parsed.entity_id:
                        self.store.record_event(
                            task_id=parsed.entity_id,
                            direction="inbound",
                            message_type="CANCEL",
                            external_id=draft.draft_id,
                            status="rejected",
                            error_message="CANCEL body task_id does not match subject.",
                        )
                        continue
                    if body.get("reason"):
                        reason = str(body["reason"])[:500]
            except json.JSONDecodeError:
                pass
            if self.store.request_cancel(
                task_id=parsed.entity_id,
                source_draft_id=draft.draft_id,
                reason=reason,
            ):
                recorded += 1
        return recorded

    def _retry_pending_results(self) -> dict[str, int]:
        retried = 0
        still_pending = 0
        for outbox in self.store.list_pending_results(limit=50):
            task_id = str(outbox["task_id"])
            try:
                self._attempt_outbox_delivery(outbox)
            except Exception as exc:
                error = safe_error(exc, limit=200)
                current = self.store.get_pending_result(task_id)
                if current is not None and current.get("delivery_status") == "pending":
                    self.store.mark_result_delivery_failed(
                        task_id=task_id,
                        error=error,
                    )
                elif current is not None:
                    self.store.mark_summary_delivery_failed(
                        task_id=task_id,
                        error=error,
                    )
            if self.store.get_pending_result(task_id) is not None:
                still_pending += 1
            else:
                retried += 1
        return {"retried": retried, "still_pending": still_pending}

    @staticmethod
    def _fallback_result_summary(task_id: str) -> dict[str, Any]:
        return {
            "schema": GPT_RESULT_SUMMARY_SCHEMA,
            "task_id": task_id,
            "repository": None,
            "status": "unknown",
            "exit_code": None,
            "finished_at": None,
            "commit": {"expected": None, "actual": None, "verified": None},
            "workspace": {
                "clean_before": None,
                "clean_after": None,
                "evidence_available": False,
            },
            "steps": {
                "total": 0,
                "completed": 0,
                "failed_count": 0,
                "failed": [],
                "failed_truncated": False,
            },
            "artifact": {"available": False},
            "details_available": True,
            "detail_operation": "results.get",
            "source_result_schema": "cwapi.result.v1",
            "needs_attention": True,
            "attention": [
                {
                    "code": "summary_generation_failed",
                    "source": "summary",
                    "details_required": True,
                }
            ],
            "attention_count": 1,
            "attention_truncated": False,
            "error": build_gpt_error(
                code="summary_generation_failed",
                category="internal",
                concise_message="Compact result summary generation failed.",
                recommended_next_action=(
                    "Request the full result; do not rerun completed task actions."
                ),
                retryable=False,
                operation="results.summary",
                details_required=True,
            ),
        }

    def _build_summary_message(
        self,
        *,
        task_id: str,
        payload: dict[str, Any],
    ) -> tuple[str, str]:
        try:
            summary = build_result_summary(payload)
        except Exception:
            summary = self._fallback_result_summary(task_id)
        summary_status = "ATTENTION" if summary["needs_attention"] else "READY"
        return (
            build_subject("SUMMARY", summary_status, task_id),
            json.dumps(
                summary,
                ensure_ascii=False,
                separators=(",", ":"),
                sort_keys=True,
            ),
        )

    def _deliver_or_outbox(
        self,
        *,
        task_id: str,
        subject_status: str,
        payload: dict[str, Any],
        final_execution_status: str,
        error: str | None = None,
    ) -> str:
        subject = build_subject("RESULT", subject_status, task_id)
        payload_json = json.dumps(
            payload,
            ensure_ascii=False,
            indent=2,
            sort_keys=True,
        )
        normalized_status = (
            "completed"
            if final_execution_status == "completed"
            else "failed"
        )
        summary_subject, summary_payload_json = self._build_summary_message(
            task_id=task_id,
            payload=payload,
        )
        self.store.save_or_update_outbox(
            task_id=task_id,
            subject=subject,
            payload_json=payload_json,
            final_execution_status=normalized_status,
            execution_error=error,
            summary_subject=summary_subject,
            summary_payload_json=summary_payload_json,
        )
        self.store.mark_result_pending(
            task_id=task_id,
            execution_status=normalized_status,
            error=error,
        )
        outbox = self.store.get_pending_result(task_id)
        if outbox is None:
            raise RuntimeError(f"outbox row missing for task {task_id}")
        return self._attempt_outbox_delivery(outbox) or ""

    def _attempt_outbox_delivery(self, outbox: dict[str, Any]) -> str | None:
        task_id = str(outbox["task_id"])
        result_draft_id = (
            str(outbox.get("gmail_draft_id") or "")
            if outbox.get("delivery_status") == "delivered"
            else ""
        )
        if result_draft_id:
            self._finalize_task_delivery_state(outbox, result_draft_id)
        if not result_draft_id:
            result_draft_id = self._attempt_result_delivery(outbox) or ""
            if not result_draft_id:
                return None
            outbox = self.store.get_outbox_result(task_id) or outbox

        if outbox.get("summary_delivery_status") == "pending":
            self._attempt_summary_delivery(outbox)
        return result_draft_id

    def _attempt_result_delivery(self, outbox: dict[str, Any]) -> str | None:
        task_id = str(outbox["task_id"])
        subject = str(outbox["subject"])
        try:
            existing_draft_id = self.transport.find_exact_draft_by_subject(subject)
        except Exception as exc:
            self.store.mark_result_delivery_failed(
                task_id=task_id,
                error=safe_error(exc, limit=200),
            )
            return None

        if existing_draft_id is not None:
            self._finalize_outbox_delivery(outbox, existing_draft_id)
            return existing_draft_id

        self.store.mark_result_delivery_attempt(task_id=task_id)
        try:
            draft_id = self.transport.create_draft(
                subject=subject,
                body=str(outbox["payload_json"]),
            )
        except Exception as exc:
            self.store.mark_result_delivery_failed(
                task_id=task_id,
                error=safe_error(exc, limit=200),
            )
            return None

        self._finalize_outbox_delivery(outbox, draft_id)
        return draft_id

    def _attempt_summary_delivery(self, outbox: dict[str, Any]) -> str | None:
        task_id = str(outbox["task_id"])
        subject = str(outbox.get("summary_subject") or "")
        payload_json = str(outbox.get("summary_payload_json") or "")
        if not subject or not payload_json:
            self.store.mark_summary_delivery_failed(
                task_id=task_id,
                error="SUMMARY_OUTBOX_INVALID",
            )
            return None
        try:
            existing_draft_id = self.transport.find_exact_draft_by_subject(subject)
        except Exception as exc:
            self.store.mark_summary_delivery_failed(
                task_id=task_id,
                error=safe_error(exc, limit=200),
            )
            return None

        if existing_draft_id is not None:
            self.store.mark_summary_delivered(
                task_id=task_id,
                gmail_draft_id=existing_draft_id,
            )
            return existing_draft_id

        self.store.mark_summary_delivery_attempt(task_id=task_id)
        try:
            draft_id = self.transport.create_draft(
                subject=subject,
                body=payload_json,
            )
        except Exception as exc:
            self.store.mark_summary_delivery_failed(
                task_id=task_id,
                error=safe_error(exc, limit=200),
            )
            return None

        self.store.mark_summary_delivered(
            task_id=task_id,
            gmail_draft_id=draft_id,
        )
        return draft_id

    def _finalize_outbox_delivery(
        self,
        outbox: dict[str, Any],
        draft_id: str,
    ) -> None:
        task_id = str(outbox["task_id"])
        self.store.mark_result_delivered(
            task_id=task_id,
            gmail_draft_id=draft_id,
        )
        self._finalize_task_delivery_state(outbox, draft_id)

    def _finalize_task_delivery_state(
        self,
        outbox: dict[str, Any],
        draft_id: str,
    ) -> None:
        task_id = str(outbox["task_id"])
        if outbox["final_execution_status"] == "completed":
            self.store.mark_completed(
                task_id=task_id,
                result_draft_id=draft_id,
            )
        else:
            self.store.finalize_failed_result(
                task_id=task_id,
                result_draft_id=draft_id,
                error=outbox.get("execution_error") or None,
            )

    def recover(self) -> dict[str, int]:
        self.store.initialize()
        retried = self._retry_pending_results()
        interrupted = 0
        if isinstance(self.store, RuntimeStateStore):
            for task in self.store.list_recoverable_tasks():
                task_id = str(task["task_id"])
                if self.store.get_pending_result(task_id):
                    continue
                reason = "INTERRUPTED_EXECUTION: recovered during runner startup."
                payload = {
                    "schema": "cwapi.result.v1",
                    "task_id": task_id,
                    "runner_id": self.config.runner.runner_id,
                    "overall_status": "failed",
                    "error_code": "INTERRUPTED_EXECUTION",
                    "error_message": reason,
                    "finished_at": datetime.now(timezone.utc).isoformat(),
                }
                self._deliver_or_outbox(
                    task_id=task_id,
                    subject_status="FAILED",
                    payload=payload,
                    final_execution_status="failed",
                    error=reason,
                )
                interrupted += 1
        return {
            "result_retried": retried["retried"],
            "result_pending": retried["still_pending"],
            "interrupted": interrupted,
        }

    def cleanup(self) -> dict[str, Any]:
        if self.advanced is None:
            return {"worktrees": [], "artifacts": []}
        return self.advanced.cleanup()


CWapiRunner = Phase1Runner
