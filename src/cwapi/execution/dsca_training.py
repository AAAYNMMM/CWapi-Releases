from __future__ import annotations

import argparse
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import sys
from typing import Mapping, Sequence

from .readonly_inputs import (
    READONLY_INPUT_PREFIX,
    ReadOnlyInputError,
    ReadOnlyInputLease,
    validate_dsca_training_arguments,
)


TASK5H1_CONFIG_SCHEMA = "dsca.task5h1_gradient_routing_diagnosis_config.v1.8"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="cwapi-dsca-training")
    parser.add_argument("--source-root", required=True)
    parser.add_argument("--config", required=True)
    parser.add_argument(
        "--input",
        action="append",
        nargs=2,
        metavar=("PATH", "SHA256"),
        required=True,
    )
    return parser


def build_child_environment(workspace: Path) -> dict[str, str]:
    environment = dict(os.environ)
    candidates = (workspace / "src", workspace)
    environment["PYTHONPATH"] = os.pathsep.join(
        str(candidate) for candidate in candidates if candidate.exists()
    )
    return environment


def _safe_relative(value: object, *, name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ReadOnlyInputError(f"{name} 必须是非空相对路径。")
    candidate = value.replace("\\", "/").strip()
    pure = PurePosixPath(candidate)
    if pure.is_absolute() or any(part in {"", ".."} for part in pure.parts):
        raise ReadOnlyInputError(f"{name} 不是安全相对路径。")
    return pure.as_posix()


def validate_materialized_task5h1_config(
    workspace: Path,
    *,
    config_path: str,
    readonly_inputs: Sequence[Mapping[str, str]],
) -> Mapping[str, object]:
    input_paths = {str(item["path"]) for item in readonly_inputs}
    if len(input_paths) != 3:
        raise ReadOnlyInputError("Task 5H-1 必须恰好提供 config、checkpoint 和 sidecar。")
    path = (workspace / config_path).resolve()
    try:
        path.relative_to(workspace.resolve())
    except ValueError as exc:
        raise ReadOnlyInputError("Task 5H-1 config 路径越界。") from exc
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ReadOnlyInputError("Task 5H-1 config 不是可读 JSON。") from exc
    if not isinstance(payload, Mapping):
        raise ReadOnlyInputError("Task 5H-1 config 顶层必须是对象。")
    if payload.get("schema_version") != TASK5H1_CONFIG_SCHEMA:
        raise ReadOnlyInputError("dsca_training 只允许冻结的 Task 5H-1 config schema。")
    clean_path = _safe_relative(
        payload.get("clean_initialization_path"),
        name="clean_initialization_path",
    )
    if not clean_path.startswith(f"{READONLY_INPUT_PREFIX}/"):
        raise ReadOnlyInputError("clean_initialization_path 必须位于 local-evidence/。")
    sidecar_path = f"{clean_path}.sha256.json"
    required = {config_path, clean_path, sidecar_path}
    if input_paths != required:
        raise ReadOnlyInputError(
            "readonly_inputs 必须精确匹配 config、clean initialization 和 sidecar。"
        )
    output_dir = _safe_relative(payload.get("output_dir"), name="output_dir")
    if output_dir == READONLY_INPUT_PREFIX or output_dir.startswith(
        f"{READONLY_INPUT_PREFIX}/"
    ):
        raise ReadOnlyInputError("Task 5H-1 output_dir 不能位于 local-evidence/。")
    return dict(payload)


def run(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    arguments = {
        "source_root": args.source_root,
        "config_path": args.config,
        "readonly_inputs": [
            {"path": path, "sha256": sha256} for path, sha256 in args.input
        ],
    }
    normalized = validate_dsca_training_arguments(arguments)
    workspace = Path.cwd().resolve()
    source_root = Path(str(normalized["source_root"])).resolve()
    with ReadOnlyInputLease.create(
        source_root=source_root,
        workspace=workspace,
        raw_inputs=normalized["readonly_inputs"],
    ):
        validate_materialized_task5h1_config(
            workspace,
            config_path=str(normalized["config_path"]),
            readonly_inputs=normalized["readonly_inputs"],
        )
        completed = subprocess.run(
            [
                sys.executable,
                "-m",
                "dsca_net.training",
                "--config",
                str(normalized["config_path"]),
            ],
            cwd=workspace,
            env=build_child_environment(workspace),
            shell=False,
            check=False,
        )
        return int(completed.returncode)


def main() -> None:
    raise SystemExit(run())


if __name__ == "__main__":
    main()
