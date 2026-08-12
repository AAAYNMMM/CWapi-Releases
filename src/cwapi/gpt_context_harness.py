from __future__ import annotations

from collections.abc import Callable, Mapping
from dataclasses import asdict, dataclass
import json
import re


ENTRY_MAX_LINES = 120
CONTEXT_MAX_BYTES = 16 * 1024
INDEX_MAX_BYTES = 8 * 1024
DETAIL_MAX_BYTES = 16 * 1024
SUMMARY_MAX_BYTES = 8 * 1024
PREFLIGHT_MAX_BYTES = 32 * 1024
REQUEST_SUBJECT = "[CWapi/1][REQUEST][PENDING][AUTO]"

OperationCaller = Callable[[dict[str, object]], Mapping[str, object]]


class GPTContextHarnessError(RuntimeError):
    pass


@dataclass(frozen=True)
class HarnessMeasurement:
    phase: str
    operation: str
    response_bytes: int


@dataclass(frozen=True)
class GPTContextHarnessReport:
    schema: str
    entry_lines: int
    selected_preset: str
    selected_action: str
    created_task_id: str
    success_task_id: str
    success_decision: str
    attention_task_id: str
    attention_code: str
    attention_detail: str
    preflight_bytes: int
    normal_flow_bytes: int
    attention_extension_bytes: int
    measurements: tuple[HarnessMeasurement, ...]

    def as_dict(self) -> dict[str, object]:
        payload = asdict(self)
        payload["measurements"] = [asdict(item) for item in self.measurements]
        return payload


def _mapping(value: object, *, label: str) -> dict[str, object]:
    if not isinstance(value, Mapping):
        raise GPTContextHarnessError(f"{label} must be an object")
    return dict(value)


def _items(value: object, *, label: str) -> list[dict[str, object]]:
    if not isinstance(value, list):
        raise GPTContextHarnessError(f"{label} must be an array")
    return [_mapping(item, label=f"{label} item") for item in value]


def _text(value: object, *, label: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise GPTContextHarnessError(f"{label} must be a non-empty string")
    return value.strip()


def _encoded_size(value: Mapping[str, object]) -> int:
    return len(
        json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    )


def _invoke(
    caller: OperationCaller,
    request: dict[str, object],
    *,
    phase: str,
    max_bytes: int,
    measurements: list[HarnessMeasurement],
) -> dict[str, object]:
    operation = _text(request.get("operation"), label="operation")
    response = _mapping(caller(dict(request)), label=f"{operation} response")
    size = _encoded_size(response)
    if size > max_bytes:
        raise GPTContextHarnessError(
            f"{operation} exceeds byte budget: {size} > {max_bytes}"
        )
    measurements.append(
        HarnessMeasurement(
            phase=phase,
            operation=operation,
            response_bytes=size,
        )
    )
    return response


def _validate_entry(entry_text: str) -> int:
    if not isinstance(entry_text, str) or not entry_text.strip():
        raise GPTContextHarnessError("short entry must be non-empty")
    lines = len(entry_text.splitlines())
    if lines > ENTRY_MAX_LINES:
        raise GPTContextHarnessError(
            f"short entry exceeds line budget: {lines} > {ENTRY_MAX_LINES}"
        )
    required = {
        REQUEST_SUBJECT,
        "context.get",
        "actions.list",
        "presets.list",
        "tasks.create",
        "results.summary",
        "results.get",
        "needs_attention=false",
        "needs_attention=true",
    }
    missing = sorted(item for item in required if item not in entry_text)
    if missing:
        raise GPTContextHarnessError(f"short entry is missing: {missing}")
    if "[CWapi/1][TASK][PENDING]" in entry_text:
        raise GPTContextHarnessError("short entry must not restore manual TASK authoring")
    return lines


def _require_features(context: Mapping[str, object]) -> None:
    control_plane = _mapping(context.get("control_plane"), label="control_plane")
    features = _mapping(control_plane.get("features"), label="features")
    required = {
        "context": "context.get",
        "action_discovery": "actions.list",
        "presets": "presets.list",
        "task_builder": "tasks.create",
        "result_summary": "results.summary",
    }
    for name, operation in required.items():
        feature = _mapping(features.get(name), label=f"feature {name}")
        if feature.get("available") is not True:
            raise GPTContextHarnessError(f"required feature is unavailable: {name}")
        operations = feature.get("operations")
        if not isinstance(operations, list) or operation not in operations:
            raise GPTContextHarnessError(
                f"feature {name} does not advertise {operation}"
            )
        transports = feature.get("transports")
        if not isinstance(transports, list) or "gmail" not in transports:
            raise GPTContextHarnessError(f"feature {name} is not available over gmail")


def run_fresh_gpt_context_harness(
    caller: OperationCaller,
    *,
    entry_text: str,
    repository: str,
    expected_commit: str,
    success_task_id: str,
    attention_task_id: str,
    intent_category: str = "git",
) -> GPTContextHarnessReport:
    """Exercise the summary-first flow with no preloaded action or preset names."""

    entry_lines = _validate_entry(entry_text)
    normalized_repository = _text(repository, label="repository")
    normalized_commit = _text(expected_commit, label="expected_commit")
    if re.fullmatch(r"[0-9A-Fa-f]{40}", normalized_commit) is None:
        raise GPTContextHarnessError("expected_commit must be a 40-character SHA")
    success_id = _text(success_task_id, label="success_task_id")
    attention_id = _text(attention_task_id, label="attention_task_id")
    category = _text(intent_category, label="intent_category")
    measurements: list[HarnessMeasurement] = []

    context = _invoke(
        caller,
        {"operation": "context.get", "repository": normalized_repository},
        phase="context",
        max_bytes=CONTEXT_MAX_BYTES,
        measurements=measurements,
    )
    if context.get("schema") != "cwapi.gpt.context.v1":
        raise GPTContextHarnessError("context schema is invalid")
    _require_features(context)

    preset_index = _invoke(
        caller,
        {"operation": "presets.list"},
        phase="discovery",
        max_bytes=INDEX_MAX_BYTES,
        measurements=measurements,
    )
    presets = _items(preset_index.get("presets"), label="presets")
    selected_preset_item = next(
        (
            item
            for item in presets
            if item.get("enabled") is True and item.get("category") == category
        ),
        None,
    )
    if selected_preset_item is None:
        raise GPTContextHarnessError(
            f"no enabled preset was discovered for category: {category}"
        )
    selected_preset = _text(
        selected_preset_item.get("name"), label="selected preset"
    )
    preset_detail = _invoke(
        caller,
        {"operation": "presets.get", "name": selected_preset},
        phase="discovery",
        max_bytes=DETAIL_MAX_BYTES,
        measurements=measurements,
    )
    parameter_schema = _mapping(
        preset_detail.get("parameters_schema"), label="preset parameters_schema"
    )
    required_parameters = parameter_schema.get("required", [])
    if not isinstance(required_parameters, list) or required_parameters:
        raise GPTContextHarnessError(
            "fresh harness requires a discovered preset with no mandatory parameters"
        )
    required_actions = preset_detail.get("required_actions")
    if not isinstance(required_actions, list) or not required_actions:
        raise GPTContextHarnessError("selected preset has no required actions")

    action_index = _invoke(
        caller,
        {"operation": "actions.list"},
        phase="discovery",
        max_bytes=INDEX_MAX_BYTES,
        measurements=measurements,
    )
    actions = {
        _text(item.get("name"), label="action name"): item
        for item in _items(action_index.get("actions"), label="actions")
    }
    for action in required_actions:
        action_name = _text(action, label="required action")
        if actions.get(action_name, {}).get("enabled") is not True:
            raise GPTContextHarnessError(
                f"preset requires an unavailable action: {action_name}"
            )
    selected_action = _text(required_actions[0], label="selected action")
    action_detail = _invoke(
        caller,
        {"operation": "actions.get", "name": selected_action},
        phase="discovery",
        max_bytes=DETAIL_MAX_BYTES,
        measurements=measurements,
    )
    if action_detail.get("enabled") is not True:
        raise GPTContextHarnessError("selected action detail is not enabled")

    preflight_bytes = sum(item.response_bytes for item in measurements)
    if preflight_bytes > PREFLIGHT_MAX_BYTES:
        raise GPTContextHarnessError(
            f"preflight exceeds byte budget: {preflight_bytes} > {PREFLIGHT_MAX_BYTES}"
        )

    created = _invoke(
        caller,
        {
            "operation": "tasks.create",
            "repository": normalized_repository,
            "expected_commit": normalized_commit,
            "preset": selected_preset,
        },
        phase="create",
        max_bytes=INDEX_MAX_BYTES,
        measurements=measurements,
    )
    if created.get("accepted") is not True:
        raise GPTContextHarnessError("tasks.create was not accepted")
    created_task_id = _text(created.get("task_id"), label="created task_id")

    success = _invoke(
        caller,
        {"operation": "results.summary", "task_id": success_id},
        phase="success",
        max_bytes=SUMMARY_MAX_BYTES,
        measurements=measurements,
    )
    if success.get("schema") != "cwapi.result-summary.v1":
        raise GPTContextHarnessError("success summary schema is invalid")
    if success.get("status") != "completed" or success.get("needs_attention") is not False:
        raise GPTContextHarnessError(
            "normal success must be completed with needs_attention=false"
        )
    normal_flow_bytes = sum(
        item.response_bytes
        for item in measurements
        if item.phase in {"context", "discovery", "create", "success"}
    )

    attention = _invoke(
        caller,
        {"operation": "results.summary", "task_id": attention_id},
        phase="attention",
        max_bytes=SUMMARY_MAX_BYTES,
        measurements=measurements,
    )
    if attention.get("needs_attention") is not True:
        raise GPTContextHarnessError("failure summary must require attention")
    error = _mapping(attention.get("error"), label="attention error")
    attention_code = _text(error.get("code"), label="attention error code")
    _text(
        error.get("recommended_next_action"),
        label="attention recommended_next_action",
    )
    if error.get("details_required") is not True:
        raise GPTContextHarnessError("failure scenario must require detail expansion")
    affected_step = error.get("affected_step")
    if isinstance(affected_step, str) and affected_step.strip():
        attention_detail = "step"
        detail_request: dict[str, object] = {
            "operation": "results.get",
            "task_id": attention_id,
            "detail": "step",
            "step_id": affected_step.strip(),
        }
    else:
        attention_detail = "full_result"
        detail_request = {
            "operation": "results.get",
            "task_id": attention_id,
            "detail": "full_result",
        }
    detail = _invoke(
        caller,
        detail_request,
        phase="attention",
        max_bytes=DETAIL_MAX_BYTES,
        measurements=measurements,
    )
    if detail.get("schema") != "cwapi.gpt.result-detail.v1":
        raise GPTContextHarnessError("attention detail schema is invalid")
    if detail.get("detail") != attention_detail:
        raise GPTContextHarnessError("attention detail selector was not preserved")

    attention_extension_bytes = sum(
        item.response_bytes for item in measurements if item.phase == "attention"
    )
    return GPTContextHarnessReport(
        schema="cwapi.gpt.context-harness.v1",
        entry_lines=entry_lines,
        selected_preset=selected_preset,
        selected_action=selected_action,
        created_task_id=created_task_id,
        success_task_id=success_id,
        success_decision="summary_only",
        attention_task_id=attention_id,
        attention_code=attention_code,
        attention_detail=attention_detail,
        preflight_bytes=preflight_bytes,
        normal_flow_bytes=normal_flow_bytes,
        attention_extension_bytes=attention_extension_bytes,
        measurements=tuple(measurements),
    )


__all__ = [
    "CONTEXT_MAX_BYTES",
    "DETAIL_MAX_BYTES",
    "ENTRY_MAX_LINES",
    "GPTContextHarnessError",
    "GPTContextHarnessReport",
    "HarnessMeasurement",
    "INDEX_MAX_BYTES",
    "PREFLIGHT_MAX_BYTES",
    "SUMMARY_MAX_BYTES",
    "run_fresh_gpt_context_harness",
]
