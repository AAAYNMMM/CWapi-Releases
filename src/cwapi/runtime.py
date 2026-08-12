from __future__ import annotations

from datetime import datetime, timedelta, timezone
import os
import time
from pathlib import Path
from typing import Any, Callable

from .artifacts import ArtifactError, ArtifactManager
from .config import AppConfig, ProjectConfig
from .execution.action_registry import (
    INTERNAL_ACTIONS,
    InvalidActionArguments,
    UnknownActionError,
    resolve_action,
)
from .execution.codex_process_runner import run_process_via_codex
from .execution.internal_actions import execute_internal_action
from .execution.live_logs import LIVE_LOGS
from .execution.process_runner import build_execution_environment, run_process
from .execution.result_capture import (
    ExecutionResult,
    StepResult,
    build_result_payload,
    build_step_payload,
    make_step_result,
    make_summary,
)
from .git import GitRuntime, RepositoryError, RepositoryManager, WorkspaceLease
from .security import safe_error
from .state.runtime_store import RuntimeStateStore


DeliverResult = Callable[..., str]
PublishProgress = Callable[..., str | None]


_PYTHON_WORKSPACE_ACTIONS = frozenset({"pytest", "pytest_full"})


def build_workspace_execution_environment(
    allowed_names: tuple[str, ...] | list[str],
    *,
    workspace: Path,
    action: str,
    git_runtime: GitRuntime | None = None,
) -> dict[str, str]:
    """Build a controlled environment that imports Python code from the worktree.

    TASK payloads cannot provide environment values. For Python test actions, the
    Runner sets PYTHONPATH to the managed worktree only, preventing an editable
    installation from the operator's daily checkout from shadowing the requested
    commit.
    """
    environment = build_execution_environment(allowed_names)
    if git_runtime is not None:
        environment = git_runtime.environment(environment)
    if action in _PYTHON_WORKSPACE_ACTIONS:
        candidates = (workspace / "src", workspace)
        environment["PYTHONPATH"] = os.pathsep.join(
            str(candidate) for candidate in candidates if candidate.exists()
        )
    return environment


class AdvancedTaskExecutor:
    """Feature-complete local execution path used when runtime.enabled is true."""

    def __init__(
        self,
        *,
        config: AppConfig,
        store: RuntimeStateStore,
        deliver_result: DeliverResult,
        publish_progress: PublishProgress | None = None,
    ) -> None:
        self.config = config
        self.store = store
        self.deliver_result = deliver_result
        self.publish_progress = publish_progress
        self.repositories = RepositoryManager(config.git)
        self.artifacts = ArtifactManager(config.storage)

    def _workspace(
        self,
        *,
        project: ProjectConfig,
        task_id: str,
        expected_commit: str,
    ) -> WorkspaceLease:
        if self.config.git.enabled:
            return self.repositories.prepare(
                project=project,
                task_id=task_id,
                expected_commit=expected_commit,
            )
        return self.repositories.legacy_workspace(
            project=project,
            task_id=task_id,
            expected_commit=expected_commit,
        )

    def _failure_payload(
        self,
        *,
        task_id: str,
        repository: str,
        expected_commit: str,
        actual_commit: str | None,
        source_draft_id: str,
        error_code: str,
        error_message: str,
        workspace_path: str | None = None,
    ) -> dict[str, Any]:
        return {
            "schema": "cwapi.result.v1",
            "task_id": task_id,
            "runner_id": self.config.runner.runner_id,
            "repository": repository,
            "expected_commit": expected_commit,
            "actual_commit": actual_commit,
            "overall_status": "failed",
            "steps": [],
            "exit_code": 1,
            "duration_ms": 0,
            "stdout_summary": "",
            "stderr_summary": make_summary(error_message),
            "log_path": str(self.artifacts.task_log_dir(task_id)),
            "workspace_path": workspace_path,
            "source_draft_id": source_draft_id,
            "error_code": error_code,
            "error_message": error_message,
            "finished_at": datetime.now(timezone.utc).isoformat(),
        }

    def _deliver_failure(
        self,
        *,
        task_id: str,
        payload: dict[str, Any],
        error: str,
    ) -> tuple[str, str]:
        draft_id = self.deliver_result(
            task_id=task_id,
            subject_status="FAILED",
            payload=payload,
            final_execution_status="failed",
            error=error,
        )
        return (draft_id, "failed" if draft_id else "result_pending")

    def _publish_step_progress(
        self,
        *,
        task_id: str,
        step_payload: dict[str, Any],
        completed_steps: int,
        total_steps: int,
    ) -> None:
        if (
            self.publish_progress is None
            or not self.config.runtime.publish_progress
            or self.config.runner.progress_mode != "step"
        ):
            return
        try:
            self.publish_progress(
                task_id=task_id,
                status="STEP",
                payload={
                    "schema": "cwapi.progress.v1",
                    "task_id": task_id,
                    "runner_id": self.config.runner.runner_id,
                    "completed_steps": completed_steps,
                    "total_steps": total_steps,
                    "step": step_payload,
                    "reported_at": datetime.now(timezone.utc).isoformat(),
                },
            )
        except Exception as exc:
            self.store.record_event(
                task_id=task_id,
                direction="outbound",
                message_type="PROGRESS",
                external_id=None,
                status="delivery_failed",
                error_message=safe_error(exc, limit=200),
            )

    @staticmethod
    def _persist_step_logs(
        step_objects: list[StepResult],
        step_log_paths: list[tuple[Path, Path]],
    ) -> None:
        """Persist buffered output once, at the task terminal boundary."""
        for result, (stdout_path, stderr_path) in zip(step_objects, step_log_paths):
            stdout_path.parent.mkdir(parents=True, exist_ok=True)
            stderr_path.parent.mkdir(parents=True, exist_ok=True)
            stdout_path.write_text(result.stdout, encoding="utf-8")
            stderr_path.write_text(result.stderr, encoding="utf-8")

    @staticmethod
    def _publish_nonstreamed_output(task_id: str, step_id: str, result: StepResult) -> None:
        for stream, text in (("stdout", result.stdout), ("stderr", result.stderr)):
            for line in text.splitlines():
                LIVE_LOGS.append_output_line(task_id, step_id, stream, line)

    def execute(self, envelope: Any, source_draft_id: str) -> tuple[str, str]:
        task = envelope.task
        task_id = str(task["task_id"])
        repository = str(task["repository"])
        expected_commit = str(task["expected_commit"])
        project = self.config.projects.get(repository)
        self.store.attach_task_payload(task_id=task_id, task=task)
        LIVE_LOGS.begin_task(task_id)

        try:
            lease = self._workspace(
                project=project,
                task_id=task_id,
                expected_commit=expected_commit,
            )
        except RepositoryError as exc:
            message = safe_error(exc)
            code = (
                "EXPECTED_COMMIT_NOT_FOUND"
                if "EXPECTED_COMMIT_NOT_FOUND" in message
                else "WORKSPACE_PREPARE_FAILED"
            )
            payload = self._failure_payload(
                task_id=task_id,
                repository=repository,
                expected_commit=expected_commit,
                actual_commit=None,
                source_draft_id=source_draft_id,
                error_code=code,
                error_message=message,
            )
            return self._deliver_failure(
                task_id=task_id,
                payload=payload,
                error=message,
            )

        if lease.actual_commit != expected_commit:
            message = (
                f"Expected commit {expected_commit}, actual {lease.actual_commit}"
            )
            payload = self._failure_payload(
                task_id=task_id,
                repository=repository,
                expected_commit=expected_commit,
                actual_commit=lease.actual_commit,
                source_draft_id=source_draft_id,
                error_code="EXPECTED_COMMIT_MISMATCH",
                error_message=message,
                workspace_path=str(lease.worktree_path),
            )
            return self._deliver_failure(
                task_id=task_id,
                payload=payload,
                error=message,
            )

        keep_until = None
        if self.config.git.keep_failed_worktrees_hours > 0:
            keep_until = (
                datetime.now(timezone.utc)
                + timedelta(hours=self.config.git.keep_failed_worktrees_hours)
            ).isoformat()
        self.store.record_workspace(
            task_id=task_id,
            repository=repository,
            mirror_path=str(lease.mirror_path),
            worktree_path=str(lease.worktree_path),
            expected_commit=expected_commit,
            actual_commit=lease.actual_commit,
            managed=lease.managed,
            keep_until=keep_until,
        )
        self.store.set_running(
            task_id=task_id,
            workspace_path=str(lease.worktree_path),
        )

        steps = list(task.get("steps", []))
        step_payloads: list[dict[str, Any]] = []
        step_objects: list[StepResult] = []
        step_log_paths: list[tuple[Path, Path]] = []
        started_at = datetime.now(timezone.utc)
        task_deadline = time.monotonic() + self.config.runner.max_task_runtime_seconds
        overall_status = "completed"
        cancelled = False
        execution_error: str | None = None

        for ordinal, step_spec in enumerate(steps):
            step_id = str(step_spec["step_id"])
            action = str(step_spec["action"])
            arguments = dict(step_spec.get("arguments", {}))
            remaining_task_seconds = max(0, int(task_deadline - time.monotonic()))
            timeout_seconds = min(
                int(step_spec.get("timeout_seconds", 60)),
                self.config.security.max_step_timeout_seconds,
                remaining_task_seconds or 1,
            )
            stdout_path, stderr_path = self.artifacts.step_log_paths(
                task_id,
                step_id,
            )
            step_started_at = datetime.now(timezone.utc).isoformat()
            LIVE_LOGS.update_step(
                task_id=task_id,
                step_id=step_id,
                action=action,
                ordinal=ordinal,
                status="running",
                started_at=step_started_at,
            )
            self.store.start_step(
                task_id=task_id,
                step_id=step_id,
                action=action,
                ordinal=ordinal,
                stdout_path=str(stdout_path),
                stderr_path=str(stderr_path),
            )
            self.store.set_progress(
                task_id=task_id,
                status=f"{ordinal + 1}/{len(steps)}:running:{step_id}",
            )

            nonstreamed = False
            if remaining_task_seconds <= 0:
                now = datetime.now(timezone.utc).isoformat()
                result = make_step_result(
                    task_id=task_id,
                    step_id=step_id,
                    action=action,
                    ordinal=ordinal,
                    exit_code=None,
                    timed_out=True,
                    stdout="",
                    stderr="",
                    started_at=now,
                    finished_at=now,
                    execution_status="timed_out",
                    error_code="TASK_TIMEOUT",
                    error_message=(
                        f"Task exceeded {self.config.runner.max_task_runtime_seconds}s."
                    ),
                )
                nonstreamed = True
            elif self.store.is_cancel_requested(task_id):
                now = datetime.now(timezone.utc).isoformat()
                result = make_step_result(
                    task_id=task_id,
                    step_id=step_id,
                    action=action,
                    ordinal=ordinal,
                    exit_code=None,
                    timed_out=False,
                    stdout="",
                    stderr="",
                    started_at=now,
                    finished_at=now,
                    execution_status="cancelled",
                    error_code="CANCELLED",
                    error_message="Task cancellation was requested.",
                )
                nonstreamed = True
            elif action in INTERNAL_ACTIONS:
                result = execute_internal_action(
                    action,
                    arguments,
                    workspace=lease.worktree_path,
                    task_id=task_id,
                    step_id=step_id,
                    ordinal=ordinal,
                    max_relative_paths=self.config.security.max_relative_paths,
                )
                nonstreamed = True
            else:
                try:
                    if action == "python_pip_check" and not project.allow_dependency_check:
                        raise InvalidActionArguments(
                            "项目配置禁止 python_pip_check。"
                        )
                    command = resolve_action(
                        action,
                        arguments,
                        python_executable=project.python_executable,
                        cargo_executable=project.cargo_executable,
                        git_executable=str(self.repositories.git_runtime.executable),
                        default_test_paths=project.default_test_paths,
                        max_relative_paths=self.config.security.max_relative_paths,
                    )
                    environment = build_workspace_execution_environment(
                        self.config.security.allowed_environment_variables,
                        workspace=lease.worktree_path,
                        action=action,
                        git_runtime=self.repositories.git_runtime,
                    )
                    if action == "repository_automation":
                        # Legacy variable names keep the existing repository
                        # automation wrapper path stable during rolling upgrades.
                        # The tracer ignores both values and performs no file IO.
                        environment["CWAPI_TRACE_CURRENT"] = "memory"
                        environment["CWAPI_TRACE_HISTORY"] = "memory"
                    common = {
                        "timeout_seconds": timeout_seconds,
                        "cwd": str(lease.worktree_path),
                        "task_id": task_id,
                        "step_id": step_id,
                        "action": action,
                        "ordinal": ordinal,
                        "stdout_path": stdout_path,
                        "stderr_path": stderr_path,
                        "cancel_check": lambda: self.store.is_cancel_requested(task_id),
                        "environment": environment,
                        "git_executable": str(
                            self.repositories.git_runtime.executable
                        ),
                        "max_output_bytes": self.config.storage.artifact_max_file_bytes,
                    }
                    if self.config.codex_toolhost.enabled:
                        result = run_process_via_codex(
                            command,
                            codex_config=self.config.codex_toolhost,
                            allowed_environment_variables=(
                                self.config.security.allowed_environment_variables
                            ),
                            **common,
                        )
                    else:
                        result = run_process(command, **common)
                except (UnknownActionError, InvalidActionArguments, ValueError) as exc:
                    now = datetime.now(timezone.utc).isoformat()
                    result = make_step_result(
                        task_id=task_id,
                        step_id=step_id,
                        action=action,
                        ordinal=ordinal,
                        exit_code=None,
                        timed_out=False,
                        stdout="",
                        stderr="",
                        started_at=now,
                        finished_at=now,
                        execution_status="failed",
                        error_code="INVALID_ACTION_ARGUMENTS",
                        error_message=safe_error(exc),
                    )
                    nonstreamed = True

            if nonstreamed:
                self._publish_nonstreamed_output(task_id, step_id, result)
            LIVE_LOGS.update_step(
                task_id=task_id,
                step_id=step_id,
                action=action,
                ordinal=ordinal,
                status=result.execution_status,
                started_at=result.started_at,
                finished_at=result.finished_at,
                duration_ms=result.duration_ms,
                error=result.error_message or result.stderr or None,
            )
            self.store.record_step(
                result=result,
                stdout_path=str(stdout_path),
                stderr_path=str(stderr_path),
            )
            payload = build_step_payload(
                result,
                stdout_path=str(stdout_path),
                stderr_path=str(stderr_path),
            )
            step_objects.append(result)
            step_payloads.append(payload)
            step_log_paths.append((stdout_path, stderr_path))
            self.store.set_progress(
                task_id=task_id,
                status=f"{ordinal + 1}/{len(steps)}:{result.execution_status}",
            )
            self._publish_step_progress(
                task_id=task_id,
                step_payload=payload,
                completed_steps=ordinal + 1,
                total_steps=len(steps),
            )

            if result.execution_status == "cancelled":
                cancelled = True
                overall_status = "cancelled"
                execution_error = result.error_message
                break
            if result.execution_status != "completed":
                overall_status = "failed"
                execution_error = (
                    result.error_message
                    or result.stderr
                    or f"{action} failed"
                )
                if not bool(task.get("continue_on_failure", False)):
                    break

        # Successful step output remains exclusively in memory while a task is
        # active. At the terminal boundary, write each evidence file once. A
        # failed task reaches the same boundary immediately after the failure.
        self._persist_step_logs(step_objects, step_log_paths)

        finished_at = datetime.now(timezone.utc)
        duration_ms = max(
            1,
            int((finished_at - started_at).total_seconds() * 1000),
        )
        stdout_summary = make_summary(
            "\n".join(result.stdout for result in step_objects[-3:] if result.stdout)
        )
        stderr_summary = make_summary(
            "\n".join(result.stderr for result in step_objects[-3:] if result.stderr)
        )
        if execution_error and not stderr_summary:
            stderr_summary = make_summary(execution_error)

        execution_result = ExecutionResult(
            task_id=task_id,
            runner_id=self.config.runner.runner_id,
            repository=repository,
            expected_commit=expected_commit,
            actual_commit=lease.actual_commit,
            overall_status=overall_status,
            steps=step_payloads,
            exit_code=0 if overall_status == "completed" else 1,
            duration_ms=duration_ms,
            stdout_summary=stdout_summary,
            stderr_summary=stderr_summary,
            log_path=str(self.artifacts.task_log_dir(task_id)),
            workspace_path=str(lease.worktree_path),
            source_draft_id=source_draft_id,
            cancelled=cancelled,
        )
        payload = build_result_payload(execution_result)

        if self.config.runtime.collect_artifacts:
            try:
                bundle = self.artifacts.publish(
                    task=task,
                    result_payload=payload,
                    step_log_paths=step_log_paths,
                )
                payload["artifact_bundle"] = bundle.to_payload()
                self.store.record_artifact(
                    task_id=task_id,
                    local_path=bundle.local_path,
                    drive_relative_path=bundle.drive_relative_path,
                    manifest_sha256=bundle.manifest_sha256,
                    total_bytes=bundle.total_bytes,
                    sync_status=bundle.sync_status,
                    zip_path=bundle.zip_path,
                )
            except ArtifactError as exc:
                artifact_error = safe_error(exc)
                payload["artifact_error"] = {
                    "code": "ARTIFACT_PUBLISH_FAILED",
                    "message": artifact_error,
                }
                if overall_status == "completed":
                    overall_status = "failed"
                    payload["overall_status"] = "failed"
                    payload["exit_code"] = 1
                    execution_error = artifact_error

        subject_status = "COMPLETED" if overall_status == "completed" else "FAILED"
        final_execution_status = (
            "completed" if overall_status == "completed" else "failed"
        )
        draft_id = self.deliver_result(
            task_id=task_id,
            subject_status=subject_status,
            payload=payload,
            final_execution_status=final_execution_status,
            error=execution_error,
        )

        release_now = (
            lease.managed
            and overall_status == "completed"
            and self.config.git.cleanup_on_success
        )
        if release_now:
            try:
                self.repositories.release(lease)
                self.store.finalize_workspace(
                    task_id=task_id,
                    status="released",
                    released=True,
                )
            except Exception as exc:
                self.store.finalize_workspace(
                    task_id=task_id,
                    status="release_failed",
                    error=safe_error(exc),
                )
        else:
            if lease.managed:
                self.repositories.mark_retained(
                    lease,
                    keep_until=keep_until,
                    status="failed" if overall_status != "completed" else "retained",
                )
            self.store.finalize_workspace(
                task_id=task_id,
                status="retained" if lease.managed else "legacy_local",
                error=execution_error,
            )

        if not draft_id:
            return "", "result_pending"
        return (
            draft_id,
            "completed" if overall_status == "completed" else "failed",
        )

    def cleanup(self) -> dict[str, Any]:
        worktrees = self.repositories.cleanup_stale()
        for item in worktrees:
            task_id = str(item.get("task_id", ""))
            if task_id and not item.get("error"):
                self.store.finalize_workspace(
                    task_id=task_id,
                    status="cleaned",
                    released=True,
                )
        return {
            "worktrees": worktrees,
            "artifacts": self.artifacts.cleanup_expired(),
        }
