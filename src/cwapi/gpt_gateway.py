from __future__ import annotations

import json
from typing import Any

from .gpt_contract import feature_for_operation
from .gpt_errors import build_gpt_error
from .gpt_facade import GPTFacade
from .gpt_protocol import (
    GPTProtocolError,
    GPTRequestEnvelope,
    GPT_REQUEST_QUERY,
    GPT_REQUEST_SUBJECT,
    build_gpt_response,
    build_rejected_request_envelope,
    derive_task_id,
    parse_gpt_request,
)
from .result_details import ResultDetailError
from .security import safe_error
from .state.runtime_store import RuntimeStateStore
from .subjects import build_subject
from .task_builder import TaskBuildError


GMAIL_GATEWAY_FEATURES = frozenset(
    {
        "context",
        "action_discovery",
        "task_builder",
        "presets",
        "result_summary",
        "structured_errors",
        "gmail_gateway",
    }
)
GMAIL_READ_FEATURES = GMAIL_GATEWAY_FEATURES


class GPTGatewayError(RuntimeError):
    pass


class GPTGatewayRequestError(GPTProtocolError):
    def __init__(
        self,
        message: str,
        *,
        code: str,
        category: str,
        recommended_next_action: str,
        feature: str | None = None,
        retryable: bool = False,
        affected_step: str | None = None,
        details_required: bool = False,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.category = category
        self.recommended_next_action = recommended_next_action
        self.feature = feature
        self.retryable = retryable
        self.affected_step = affected_step
        self.details_required = details_required


class GmailGPTGateway:
    def __init__(self, service: Any, *, facade: Any | None = None) -> None:
        store = service.runner.store
        if not isinstance(store, RuntimeStateStore):
            raise GPTGatewayError("Gmail GPT Gateway requires RuntimeStateStore")
        self.service = service
        self.store = store
        self.transport = service.runner.transport
        self.facade = facade or GPTFacade(
            service,
            transport_name="gmail",
            available_features=GMAIL_GATEWAY_FEATURES,
        )

    @staticmethod
    def _reject_unknown_fields(
        request: GPTRequestEnvelope,
        *,
        allowed: set[str],
    ) -> None:
        unknown = set(request.payload) - {"operation", *allowed}
        if unknown:
            raise GPTProtocolError(
                f"{request.operation} contains unknown fields: {sorted(unknown)}"
            )

    def _dispatch(self, request: GPTRequestEnvelope) -> dict[str, object]:
        if request.operation == "context.get":
            self._reject_unknown_fields(request, allowed={"repository"})
            repository = request.parameters.get("repository")
            if repository is not None and (
                not isinstance(repository, str) or not repository.strip()
            ):
                raise GPTProtocolError("context.get repository must be a string")
            return self.facade.context(
                repository=repository.strip() if isinstance(repository, str) else None
            )
        if request.operation == "actions.list":
            self._reject_unknown_fields(request, allowed=set())
            return self.facade.actions()
        if request.operation == "actions.get":
            self._reject_unknown_fields(request, allowed={"name"})
            name = request.parameters.get("name")
            if not isinstance(name, str) or not name.strip():
                raise GPTProtocolError("actions.get name must be a non-empty string")
            try:
                return self.facade.action(name.strip())
            except KeyError as exc:
                raise GPTGatewayRequestError(
                    f"unknown action: {name.strip()}",
                    code="resource_not_found",
                    category="not_found",
                    recommended_next_action=(
                        "Call actions.list, then create a new REQUEST draft with a listed action."
                    ),
                ) from exc
        if request.operation == "presets.list":
            self._reject_unknown_fields(request, allowed=set())
            return self.facade.presets()
        if request.operation == "presets.get":
            self._reject_unknown_fields(request, allowed={"name"})
            name = request.parameters.get("name")
            if not isinstance(name, str) or not name.strip():
                raise GPTProtocolError("presets.get name must be a non-empty string")
            try:
                return self.facade.preset(name.strip())
            except KeyError as exc:
                raise GPTGatewayRequestError(
                    f"unknown preset: {name.strip()}",
                    code="resource_not_found",
                    category="not_found",
                    recommended_next_action=(
                        "Call presets.list, then create a new REQUEST draft with a listed preset."
                    ),
                ) from exc
        if request.operation == "tasks.create":
            return self._create_task(request)
        if request.operation == "results.summary":
            self._reject_unknown_fields(request, allowed={"task_id"})
            task_id = request.parameters.get("task_id")
            if not isinstance(task_id, str) or not task_id.strip():
                raise GPTProtocolError(
                    "results.summary task_id must be a non-empty string"
                )
            try:
                return self.facade.result_summary(task_id.strip())
            except ResultDetailError as exc:
                raise self._result_request_error(exc) from exc
        if request.operation == "results.get":
            self._reject_unknown_fields(
                request,
                allowed={"task_id", "detail", "step_id"},
            )
            task_id = request.parameters.get("task_id")
            detail = request.parameters.get("detail")
            step_id = request.parameters.get("step_id")
            if not isinstance(task_id, str) or not task_id.strip():
                raise GPTProtocolError("results.get task_id must be a non-empty string")
            if not isinstance(detail, str) or not detail.strip():
                raise GPTProtocolError("results.get detail must be a non-empty string")
            if step_id is not None and (
                not isinstance(step_id, str) or not step_id.strip()
            ):
                raise GPTProtocolError("results.get step_id must be a non-empty string")
            try:
                return self.facade.result_detail(
                    task_id.strip(),
                    detail=detail.strip(),
                    step_id=step_id.strip() if isinstance(step_id, str) else None,
                )
            except ResultDetailError as exc:
                raise self._result_request_error(exc) from exc
        feature = feature_for_operation(request.operation)
        if feature is not None and feature not in GMAIL_GATEWAY_FEATURES:
            raise GPTGatewayRequestError(
                f"feature is not available through Gmail: {feature}",
                code="unsupported_feature",
                category="capability",
                recommended_next_action=(
                    "Call context.get and choose an operation whose feature is available."
                ),
                feature=feature,
            )
        raise GPTGatewayRequestError(
            f"unknown operation: {request.operation}",
            code="invalid_operation",
            category="request",
            recommended_next_action=(
                "Call context.get and create a new REQUEST draft with a listed operation."
            ),
        )

    @staticmethod
    def _result_request_error(error: ResultDetailError) -> GPTGatewayRequestError:
        return GPTGatewayRequestError(
            str(error),
            code=error.code,
            category=error.category,
            recommended_next_action=error.recommended_next_action,
            retryable=error.retryable,
            affected_step=error.affected_step,
            details_required=error.details_required,
        )

    @staticmethod
    def _canonical_body(payload: dict[str, Any]) -> str:
        return json.dumps(
            payload,
            ensure_ascii=False,
            separators=(",", ":"),
            sort_keys=True,
        )

    def _create_task(self, request: GPTRequestEnvelope) -> dict[str, object]:
        record = self.store.get_gpt_request(request.request_id)
        if record is None:
            raise GPTGatewayError("GPT request is not registered")
        task = record.get("task")
        task_id = derive_task_id(request.request_id)
        if task is None:
            envelope = self.facade.build_task(
                request.parameters,
                task_id=task_id,
                strict_request=True,
            )
            task = envelope.task
            subject = build_subject("TASK", "PENDING", envelope.task_id)
            record = self.store.reserve_gpt_task(
                request_id=request.request_id,
                task_id=envelope.task_id,
                task_subject=subject,
                task=task,
            )
        else:
            if not isinstance(task, dict):
                raise GPTGatewayError("persisted task reservation is invalid")
            if record.get("task_id") != task_id:
                raise GPTGatewayError("persisted task identity is invalid")

        self._deliver_reserved_task(record)
        return {
            "accepted": True,
            "task_id": task_id,
            "task_status": "pending",
        }

    def _deliver_reserved_task(self, record: dict[str, Any]) -> str:
        request_id = str(record["request_id"])
        if record.get("task_draft_id"):
            return "replayed"
        subject = str(record.get("task_subject") or "")
        task = record.get("task")
        if not subject or not isinstance(task, dict):
            raise GPTGatewayError("persisted task reservation is incomplete")
        body = self._canonical_body(task)
        try:
            found = self.transport.find_exact_draft_by_subject(subject)
            if found:
                existing = self.transport.get_draft(found)
                if existing.subject.strip() != subject or existing.body != body:
                    raise GPTGatewayError(
                        "existing TASK draft does not match persisted reservation"
                    )
                task_draft_id = found
                disposition = "reused"
            else:
                task_draft_id = self.transport.create_draft(
                    subject=subject,
                    body=body,
                )
                disposition = "created"
            self.store.mark_gpt_task_published(
                request_id=request_id,
                task_draft_id=task_draft_id,
            )
            return disposition
        except Exception as exc:
            self.store.mark_gpt_task_publish_failed(
                request_id=request_id,
                error=safe_error(exc, limit=300),
            )
            raise

    def _reserve_operation_response(
        self,
        request: GPTRequestEnvelope,
    ) -> tuple[dict[str, Any], bool]:
        try:
            result = self._dispatch(request)
            response_status = "ready"
        except GPTGatewayRequestError as exc:
            result = {
                "error": build_gpt_error(
                    code=exc.code,
                    category=exc.category,
                    concise_message=safe_error(exc, limit=300),
                    recommended_next_action=exc.recommended_next_action,
                    retryable=exc.retryable,
                    operation=request.operation,
                    feature=exc.feature,
                    affected_step=exc.affected_step,
                    details_required=exc.details_required,
                )
            }
            response_status = "failed"
        except TaskBuildError as exc:
            result = {
                "error": build_gpt_error(
                    code="builder_rejected",
                    category="validation",
                    concise_message=safe_error(exc, limit=300),
                    recommended_next_action=(
                        "Correct the business fields and create a new REQUEST draft."
                    ),
                    retryable=False,
                    operation=request.operation,
                )
            }
            response_status = "failed"
        except GPTProtocolError as exc:
            result = {
                "error": build_gpt_error(
                    code="invalid_request",
                    category="request",
                    concise_message=safe_error(exc, limit=300),
                    recommended_next_action=(
                        "Correct the request fields and create a new REQUEST draft."
                    ),
                    retryable=False,
                    operation=request.operation,
                )
            }
            response_status = "failed"
        return self._reserve_response(
            request,
            response_status=response_status,
            result=result,
        )

    def _reserve_invalid_request_response(
        self,
        request: GPTRequestEnvelope,
        error: GPTProtocolError,
    ) -> tuple[dict[str, Any], bool]:
        return self._reserve_response(
            request,
            response_status="failed",
            result={
                "error": build_gpt_error(
                    code="invalid_request",
                    category="request",
                    concise_message=safe_error(error, limit=300),
                    recommended_next_action=(
                        "Correct the JSON and create a new REQUEST draft."
                    ),
                    retryable=False,
                    operation=request.operation,
                )
            },
        )

    def _reserve_conflict_response(
        self,
        request: GPTRequestEnvelope,
    ) -> tuple[dict[str, Any], bool]:
        return self._reserve_response(
            request,
            response_status="failed",
            result={
                "error": build_gpt_error(
                    code="request_conflict",
                    category="conflict",
                    concise_message=(
                        "REQUEST draft content changed after its identity was registered."
                    ),
                    recommended_next_action=(
                        "Keep REQUEST drafts immutable and create a new draft for changes."
                    ),
                    retryable=False,
                    operation=request.operation,
                )
            },
        )

    def _reserve_response(
        self,
        request: GPTRequestEnvelope,
        *,
        response_status: str,
        result: dict[str, Any],
    ) -> tuple[dict[str, Any], bool]:
        subject, payload = build_gpt_response(
            request,
            status=response_status,
            result=result,
        )
        record = self.store.reserve_gpt_response(
            request_id=request.request_id,
            response_status=response_status,
            response_subject=subject,
            response=payload,
        )
        return record, response_status == "ready"

    def _deliver_reserved_response(self, record: dict[str, Any]) -> str:
        request_id = str(record["request_id"])
        existing_draft_id = record.get("response_draft_id")
        if existing_draft_id:
            return "replayed"
        subject = str(record.get("response_subject") or "")
        payload = record.get("response")
        if not subject or not isinstance(payload, dict):
            raise GPTGatewayError("persisted response reservation is incomplete")
        body = self._canonical_body(payload)
        try:
            found = self.transport.find_exact_draft_by_subject(subject)
            if found:
                existing = self.transport.get_draft(found)
                if existing.subject.strip() != subject or existing.body != body:
                    raise GPTGatewayError(
                        "existing RESPONSE draft does not match persisted reservation"
                    )
                response_draft_id = found
                disposition = "reused"
            else:
                response_draft_id = self.transport.create_draft(
                    subject=subject,
                    body=body,
                )
                disposition = "created"
            self.store.mark_gpt_response_delivered(
                request_id=request_id,
                response_draft_id=response_draft_id,
            )
            return disposition
        except Exception as exc:
            self.store.mark_gpt_response_delivery_failed(
                request_id=request_id,
                error=safe_error(exc, limit=300),
            )
            raise

    def poll(self) -> dict[str, int]:
        drafts = self.transport.list_drafts(
            query=GPT_REQUEST_QUERY,
            max_results=self.service.config.gmail.max_results,
        )
        counts = {
            "seen": len(drafts),
            "matched": 0,
            "new": 0,
            "resumed": 0,
            "replayed": 0,
            "conflicts": 0,
            "responses_created": 0,
            "responses_reused": 0,
            "responses_failed": 0,
            "poll_errors": 0,
        }
        for draft in drafts:
            if draft.subject.strip() != GPT_REQUEST_SUBJECT:
                continue
            counts["matched"] += 1
            try:
                parse_error: GPTProtocolError | None = None
                try:
                    request = parse_gpt_request(
                        draft.body,
                        source_draft_id=draft.draft_id,
                        source_message_id=draft.message_id,
                    )
                except GPTProtocolError as exc:
                    parse_error = exc
                    request = build_rejected_request_envelope(
                        draft.body,
                        source_draft_id=draft.draft_id,
                        source_message_id=draft.message_id,
                    )
                registration = self.store.register_gpt_request(request)
                if registration.disposition == "conflict":
                    counts["conflicts"] += 1
                    record = registration.record
                    if record.get("response_draft_id"):
                        continue
                    if record.get("response") is None:
                        record, _ = self._reserve_conflict_response(request)
                        counts["responses_failed"] += 1
                    delivery = self._deliver_reserved_response(record)
                    if delivery == "created":
                        counts["responses_created"] += 1
                    elif delivery == "reused":
                        counts["responses_reused"] += 1
                    else:
                        counts["replayed"] += 1
                    continue
                if registration.disposition == "replay":
                    counts["replayed"] += 1
                    continue
                if registration.disposition == "new":
                    counts["new"] += 1
                else:
                    counts["resumed"] += 1
                record = registration.record
                if record.get("response") is None:
                    if parse_error is None:
                        record, ready = self._reserve_operation_response(request)
                    else:
                        record, ready = self._reserve_invalid_request_response(
                            request,
                            parse_error,
                        )
                    if not ready:
                        counts["responses_failed"] += 1
                delivery = self._deliver_reserved_response(record)
                if delivery == "created":
                    counts["responses_created"] += 1
                elif delivery == "reused":
                    counts["responses_reused"] += 1
                else:
                    counts["replayed"] += 1
            except Exception:
                counts["poll_errors"] += 1
        return counts


__all__ = [
    "GMAIL_GATEWAY_FEATURES",
    "GMAIL_READ_FEATURES",
    "GPTGatewayError",
    "GmailGPTGateway",
]
