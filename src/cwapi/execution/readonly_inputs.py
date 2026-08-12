from __future__ import annotations

from dataclasses import dataclass
import hashlib
from pathlib import Path
import shutil
import stat
from typing import Mapping, Sequence

from cwapi.security import SecurityViolation, normalize_relative_paths


READONLY_INPUT_PREFIX = "local-evidence"
_MAX_INPUTS = 16
_ALLOWED_SUFFIXES = (".json", ".pt")


class ReadOnlyInputError(ValueError):
    pass


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _validate_sha256(value: object) -> str:
    if (
        not isinstance(value, str)
        or len(value) != 64
        or any(character not in "0123456789abcdef" for character in value)
    ):
        raise ReadOnlyInputError("readonly input sha256 必须是 64 位小写十六进制。")
    return value


def normalize_readonly_input_specs(
    raw_inputs: object,
    *,
    max_items: int = _MAX_INPUTS,
) -> tuple[Mapping[str, str], ...]:
    if not isinstance(raw_inputs, list) or not raw_inputs:
        raise ReadOnlyInputError("readonly_inputs 必须是非空数组。")
    if len(raw_inputs) > max_items:
        raise ReadOnlyInputError(
            f"readonly_inputs 数量超过限制：{len(raw_inputs)} > {max_items}"
        )
    normalized: list[Mapping[str, str]] = []
    seen: set[str] = set()
    for index, item in enumerate(raw_inputs):
        if not isinstance(item, Mapping) or set(item) != {"path", "sha256"}:
            raise ReadOnlyInputError(
                f"readonly_inputs[{index}] 只能包含 path 和 sha256。"
            )
        path = str(item["path"])
        try:
            (safe_path,) = normalize_relative_paths(
                [path], max_items=1, allow_dot=False
            )
        except SecurityViolation as exc:
            raise ReadOnlyInputError(str(exc)) from exc
        path_object = Path(safe_path)
        parts = path_object.parts
        if not parts or parts[0] != READONLY_INPUT_PREFIX:
            raise ReadOnlyInputError(
                f"readonly input 必须位于 {READONLY_INPUT_PREFIX}/ 下。"
            )
        if path_object.name.startswith("."):
            raise ReadOnlyInputError("readonly input 文件名不能以点开头。")
        if not any(path_object.name.endswith(suffix) for suffix in _ALLOWED_SUFFIXES):
            raise ReadOnlyInputError("readonly input 只允许 JSON 或 PT 文件。")
        if safe_path in seen:
            raise ReadOnlyInputError(f"重复的 readonly input：{safe_path}")
        seen.add(safe_path)
        normalized.append(
            {"path": safe_path, "sha256": _validate_sha256(item["sha256"])}
        )
    return tuple(normalized)


def validate_dsca_training_arguments(arguments: Mapping[str, object]) -> dict[str, object]:
    expected = {"source_root", "config_path", "readonly_inputs"}
    if set(arguments) != expected:
        unknown = set(arguments) - expected
        missing = expected - set(arguments)
        raise ReadOnlyInputError(
            f"dsca_training 参数不同：missing={sorted(missing)} unknown={sorted(unknown)}"
        )
    source_root = arguments["source_root"]
    if not isinstance(source_root, str) or not source_root.strip():
        raise ReadOnlyInputError("source_root 必须是非空字符串。")
    source_path = Path(source_root)
    if not source_path.is_absolute():
        raise ReadOnlyInputError("source_root 必须是绝对路径。")
    config_path = arguments["config_path"]
    if not isinstance(config_path, str):
        raise ReadOnlyInputError("config_path 必须是字符串。")
    specs = normalize_readonly_input_specs(arguments["readonly_inputs"])
    by_path = {str(item["path"]): item for item in specs}
    if config_path not in by_path:
        raise ReadOnlyInputError("config_path 必须同时出现在 readonly_inputs 中。")
    if not config_path.endswith(".json"):
        raise ReadOnlyInputError("dsca_training config_path 必须是 JSON 文件。")
    return {
        "source_root": str(source_path),
        "config_path": config_path,
        "readonly_inputs": list(specs),
    }


def _inside(root: Path, relative: str) -> Path:
    resolved_root = root.resolve()
    resolved = (resolved_root / relative).resolve()
    try:
        resolved.relative_to(resolved_root)
    except ValueError as exc:
        raise ReadOnlyInputError("readonly input 路径逃逸受控根目录。") from exc
    return resolved


@dataclass
class ReadOnlyInputLease:
    source_root: Path
    workspace: Path
    specs: Sequence[Mapping[str, str]]
    materialized_paths: list[Path]

    @classmethod
    def create(
        cls,
        *,
        source_root: Path,
        workspace: Path,
        raw_inputs: object,
    ) -> "ReadOnlyInputLease":
        return cls(
            source_root=source_root,
            workspace=workspace,
            specs=normalize_readonly_input_specs(raw_inputs),
            materialized_paths=[],
        )

    def __enter__(self) -> "ReadOnlyInputLease":
        try:
            for spec in self.specs:
                relative = str(spec["path"])
                expected = str(spec["sha256"])
                source = _inside(self.source_root, relative)
                destination = _inside(self.workspace, relative)
                if not source.is_file():
                    raise ReadOnlyInputError(
                        f"readonly input 不存在或不是文件：{relative}"
                    )
                if source.is_symlink():
                    raise ReadOnlyInputError(
                        f"readonly input 不允许符号链接：{relative}"
                    )
                if _sha256(source) != expected:
                    raise ReadOnlyInputError(
                        f"readonly input 源文件哈希不同：{relative}"
                    )
                if destination.exists() or destination.is_symlink():
                    raise ReadOnlyInputError(
                        f"readonly input 目标已存在：{relative}"
                    )
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, destination)
                if _sha256(destination) != expected:
                    raise ReadOnlyInputError(
                        f"readonly input 复制后哈希不同：{relative}"
                    )
                destination.chmod(stat.S_IREAD)
                self.materialized_paths.append(destination)
            return self
        except BaseException:
            self.cleanup()
            raise

    def cleanup(self) -> None:
        for path in reversed(self.materialized_paths):
            try:
                path.chmod(stat.S_IWRITE | stat.S_IREAD)
            except OSError:
                pass
            path.unlink(missing_ok=True)
        self.materialized_paths.clear()
        root = _inside(self.workspace, READONLY_INPUT_PREFIX)
        if root.exists():
            for directory in sorted(
                (item for item in root.rglob("*") if item.is_dir()),
                key=lambda item: len(item.parts),
                reverse=True,
            ):
                try:
                    directory.rmdir()
                except OSError:
                    pass
            try:
                root.rmdir()
            except OSError:
                pass

    def __exit__(self, exc_type, exc, traceback) -> None:
        del exc_type, exc, traceback
        self.cleanup()

    def audit_summary(self) -> list[dict[str, str]]:
        return [
            {"path": str(spec["path"]), "sha256": str(spec["sha256"])}
            for spec in self.specs
        ]
