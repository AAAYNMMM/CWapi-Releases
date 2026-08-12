from __future__ import annotations

from dataclasses import dataclass
from typing import Iterable, Mapping

from . import __version__


GPT_CONTEXT_SCHEMA = "cwapi.gpt.context.v1"
GPT_ACTION_LIST_SCHEMA = "cwapi.gpt.actions.v1"
GPT_ACTION_DETAIL_SCHEMA = "cwapi.gpt.action.v1"
GPT_PRESET_LIST_SCHEMA = "cwapi.gpt.presets.v1"
GPT_PRESET_DETAIL_SCHEMA = "cwapi.gpt.preset.v1"
GPT_TASK_REQUEST_SCHEMA = "cwapi.gpt.task-request.v1"
GPT_RESULT_SUMMARY_SCHEMA = "cwapi.result-summary.v1"
GPT_RESULT_DETAIL_SCHEMA = "cwapi.gpt.result-detail.v1"
GPT_ERROR_SCHEMA = "cwapi.gpt.error.v1"
GPT_REQUEST_SCHEMA = "cwapi.gpt.request.v1"
GPT_RESPONSE_SCHEMA = "cwapi.gpt.response.v1"


@dataclass(frozen=True)
class FeatureDefinition:
    name: str
    version: str
    operations: tuple[str, ...]
    summary: str


_FEATURE_DEFINITIONS: tuple[FeatureDefinition, ...] = (
    FeatureDefinition(
        name="context",
        version=GPT_CONTEXT_SCHEMA,
        operations=("context.get",),
        summary="Minimal CWapi, project, runtime, and recent-task context.",
    ),
    FeatureDefinition(
        name="action_discovery",
        version=GPT_ACTION_LIST_SCHEMA,
        operations=("actions.list", "actions.get"),
        summary="Progressive Action Registry discovery.",
    ),
    FeatureDefinition(
        name="task_builder",
        version=GPT_TASK_REQUEST_SCHEMA,
        operations=("tasks.create",),
        summary="Build a canonical cwapi.task.v1 from business inputs.",
    ),
    FeatureDefinition(
        name="presets",
        version=GPT_PRESET_LIST_SCHEMA,
        operations=("presets.list", "presets.get"),
        summary="Fixed Action Registry compositions for common workflows.",
    ),
    FeatureDefinition(
        name="result_summary",
        version=GPT_RESULT_SUMMARY_SCHEMA,
        operations=("results.summary", "results.get"),
        summary="Compact result projection with on-demand detail.",
    ),
    FeatureDefinition(
        name="structured_errors",
        version=GPT_ERROR_SCHEMA,
        operations=(),
        summary="Stable error categories and recommended next actions.",
    ),
    FeatureDefinition(
        name="gmail_gateway",
        version=GPT_REQUEST_SCHEMA,
        operations=(),
        summary="REQUEST/RESPONSE access through the existing Gmail Draft channel.",
    ),
)

FEATURE_NAMES: frozenset[str] = frozenset(
    definition.name for definition in _FEATURE_DEFINITIONS
)
_FEATURE_BY_OPERATION = {
    operation: definition.name
    for definition in _FEATURE_DEFINITIONS
    for operation in definition.operations
}
if len(_FEATURE_BY_OPERATION) != sum(
    len(definition.operations) for definition in _FEATURE_DEFINITIONS
):
    raise RuntimeError("GPT operation is assigned to more than one feature")


def feature_for_operation(operation: str) -> str | None:
    return _FEATURE_BY_OPERATION.get(str(operation).strip())


def build_protocol_contract() -> dict[str, object]:
    """Return stable protocol negotiation data without runtime or environment state."""

    return {
        "cwapi_version": __version__,
        "subject_protocol": {
            "current": "CWapi/1",
            "compatible": ["CWapi/1"],
        },
        "schemas": {
            "task": ["cwapi.task.v1"],
            "result": ["cwapi.result.v1"],
            "context": [GPT_CONTEXT_SCHEMA],
            "action_list": [GPT_ACTION_LIST_SCHEMA],
            "action_detail": [GPT_ACTION_DETAIL_SCHEMA],
            "preset_list": [GPT_PRESET_LIST_SCHEMA],
            "preset_detail": [GPT_PRESET_DETAIL_SCHEMA],
            "task_request": [GPT_TASK_REQUEST_SCHEMA],
            "result_summary": [GPT_RESULT_SUMMARY_SCHEMA],
            "result_detail": [GPT_RESULT_DETAIL_SCHEMA],
            "error": [GPT_ERROR_SCHEMA],
            "request": [GPT_REQUEST_SCHEMA],
            "response": [GPT_RESPONSE_SCHEMA],
        },
        "compatibility": {
            "legacy_task_supported": True,
            "legacy_result_supported": True,
            "additive_gateway": True,
        },
    }


def build_feature_contract(
    *,
    available: Iterable[str] = (),
    transports: Mapping[str, Iterable[str]] | None = None,
) -> dict[str, dict[str, object]]:
    """Build feature flags while rejecting misspelled or invented capabilities."""

    available_names = frozenset(str(name) for name in available)
    unknown = available_names - FEATURE_NAMES
    if unknown:
        raise ValueError(f"unknown GPT features: {sorted(unknown)}")

    transport_map = transports or {}
    unknown_transport_features = set(transport_map) - FEATURE_NAMES
    if unknown_transport_features:
        raise ValueError(
            "transport mapping contains unknown GPT features: "
            f"{sorted(unknown_transport_features)}"
        )

    result: dict[str, dict[str, object]] = {}
    for definition in _FEATURE_DEFINITIONS:
        item: dict[str, object] = {
            "available": definition.name in available_names,
            "version": definition.version,
            "operations": list(definition.operations),
            "summary": definition.summary,
        }
        feature_transports = sorted(
            {
                str(value)
                for value in transport_map.get(definition.name, ())
                if str(value).strip()
            }
        )
        if feature_transports:
            item["transports"] = feature_transports
        result[definition.name] = item
    return result


__all__ = [
    "FEATURE_NAMES",
    "GPT_ACTION_DETAIL_SCHEMA",
    "GPT_ACTION_LIST_SCHEMA",
    "GPT_CONTEXT_SCHEMA",
    "GPT_ERROR_SCHEMA",
    "GPT_PRESET_DETAIL_SCHEMA",
    "GPT_PRESET_LIST_SCHEMA",
    "GPT_REQUEST_SCHEMA",
    "GPT_RESPONSE_SCHEMA",
    "GPT_RESULT_SUMMARY_SCHEMA",
    "GPT_RESULT_DETAIL_SCHEMA",
    "GPT_TASK_REQUEST_SCHEMA",
    "build_feature_contract",
    "build_protocol_contract",
    "feature_for_operation",
]
