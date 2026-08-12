from __future__ import annotations

from collections.abc import Mapping
import json
from typing import Any

from .gpt_actions import build_action_detail, build_action_list
from .gpt_context import build_gpt_context
from .gpt_contract import build_feature_contract, build_protocol_contract
from .gpt_presets import build_preset_detail, build_preset_list
from .models import TaskEnvelope
from .result_details import load_result_detail, load_result_summary
from .subjects import build_subject
from .task_builder import build_canonical_task


FOUNDATION_FEATURES = frozenset(
    {"context", "action_discovery", "task_builder", "presets", "result_summary"}
)


class GPTFacade:
    def __init__(
        self,
        service: Any,
        *,
        transport_name: str,
        available_features: frozenset[str] = FOUNDATION_FEATURES,
    ) -> None:
        normalized_transport = str(transport_name).strip()
        if not normalized_transport:
            raise ValueError("transport_name must be non-empty")
        self.service = service
        self.transport_name = normalized_transport
        self.available_features = available_features

    def _feature_transports(self) -> dict[str, tuple[str, ...]]:
        return {
            name: (self.transport_name,)
            for name in self.available_features
        }

    def contract(self) -> dict[str, object]:
        return {
            "protocol": build_protocol_contract(),
            "features": build_feature_contract(
                available=self.available_features,
                transports=self._feature_transports(),
            ),
        }

    def context(self, *, repository: str | None = None) -> dict[str, object]:
        return build_gpt_context(
            self.service,
            repository=repository,
            available_features=self.available_features,
            feature_transports=self._feature_transports(),
        )

    def actions(self) -> dict[str, object]:
        return build_action_list(
            allowed_actions=self.service.config.security.allowed_actions
        )

    def action(self, name: str) -> dict[str, object]:
        return build_action_detail(
            name,
            allowed_actions=self.service.config.security.allowed_actions,
        )

    def build_task(
        self,
        request: Mapping[str, Any],
        *,
        task_id: str | None = None,
        task_id_prefix: str = "GPT",
        strict_request: bool = True,
    ) -> TaskEnvelope:
        return build_canonical_task(
            request,
            config=self.service.config,
            task_id=task_id,
            task_id_prefix=task_id_prefix,
            strict_request=strict_request,
        )

    def presets(self) -> dict[str, object]:
        return build_preset_list(
            allowed_actions=self.service.config.security.allowed_actions
        )

    def preset(self, name: str) -> dict[str, object]:
        return build_preset_detail(
            name,
            allowed_actions=self.service.config.security.allowed_actions,
        )

    def result_summary(self, task_id: str) -> dict[str, object]:
        return load_result_summary(self.service.runner.store, task_id)

    def result_detail(
        self,
        task_id: str,
        *,
        detail: str,
        step_id: str | None = None,
    ) -> dict[str, object]:
        return load_result_detail(
            self.service.runner.store,
            task_id,
            detail=detail,
            step_id=step_id,
        )

    def create_task(
        self,
        request: Mapping[str, Any],
        *,
        task_id: str | None = None,
        task_id_prefix: str = "GPT",
        strict_request: bool = True,
    ) -> dict[str, object]:
        envelope = self.build_task(
            request,
            task_id=task_id,
            task_id_prefix=task_id_prefix,
            strict_request=strict_request,
        )
        task = envelope.task
        raw = json.dumps(task, ensure_ascii=False, separators=(",", ":"))
        subject = build_subject("TASK", "PENDING", envelope.task_id)
        self.service.runner.transport.create_draft(subject=subject, body=raw)
        return {
            "accepted": True,
            "task_id": envelope.task_id,
            "subject": subject,
            "repository": task["repository"],
            "expected_commit": task["expected_commit"],
        }


__all__ = ["FOUNDATION_FEATURES", "GPTFacade"]
