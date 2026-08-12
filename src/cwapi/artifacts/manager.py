from __future__ import annotations

import hashlib
import json
import shutil
import tempfile
import zipfile
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable

from cwapi.config import StorageConfig
from cwapi.security import (
    ensure_within,
    redact_text,
    repository_key,
    validate_task_id,
)


class ArtifactError(RuntimeError):
    pass


@dataclass(frozen=True)
class ArtifactBundle:
    task_id: str
    repository: str
    local_path: str
    drive_relative_path: str | None
    manifest_sha256: str
    total_bytes: int
    sync_status: str
    zip_path: str | None = None
    large_file_transport: str = "google_drive_only"

    def to_payload(self) -> dict[str, Any]:
        return asdict(self)


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _json_bytes(value: Any) -> bytes:
    return (
        json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    ).encode("utf-8")


def _artifact_role(relative: str) -> str:
    if relative == "task.json":
        return "task"
    if relative == "result.json":
        return "result"
    if relative == "manifest.json":
        return "manifest"
    if relative == "sha256.txt":
        return "checksum_index"
    if relative.startswith("steps/") and relative.endswith(".stdout.log"):
        return "step_stdout"
    if relative.startswith("steps/") and relative.endswith(".stderr.log"):
        return "step_stderr"
    if relative.startswith("steps/") and relative.endswith(".junit.xml"):
        return "test_report"
    return "artifact"


class ArtifactManager:
    def __init__(self, config: StorageConfig) -> None:
        self.config = config
        self.config.logs_path.mkdir(parents=True, exist_ok=True)
        (self.config.logs_path / "runtime").mkdir(parents=True, exist_ok=True)
        (self.config.logs_path / "tasks").mkdir(parents=True, exist_ok=True)
        self.config.results_path.mkdir(parents=True, exist_ok=True)
        if self.config.drive_sync_path is not None:
            self.config.drive_sync_path.mkdir(parents=True, exist_ok=True)

    def task_log_dir(self, task_id: str) -> Path:
        validate_task_id(task_id)
        path = ensure_within(
            self.config.logs_path,
            self.config.logs_path / "tasks" / task_id,
        )
        path.mkdir(parents=True, exist_ok=True)
        return path

    def step_log_paths(self, task_id: str, step_id: str) -> tuple[Path, Path]:
        log_dir = self.task_log_dir(task_id)
        safe_step = "".join(
            character if character.isalnum() or character in "._-" else "_"
            for character in step_id
        )
        return (
            log_dir / f"{safe_step}.stdout.log",
            log_dir / f"{safe_step}.stderr.log",
        )

    def _write_json(self, path: Path, value: Any) -> None:
        path.write_bytes(_json_bytes(value))

    def _copy_redacted_text(
        self,
        source: Path,
        destination: Path,
        *,
        remaining_bytes: int,
    ) -> int:
        if not source.exists() or not source.is_file():
            return 0
        source_size = source.stat().st_size
        if source_size > self.config.artifact_max_file_bytes:
            raise ArtifactError(
                f"证据文件超过单文件限制：{source} ({source_size} bytes)"
            )
        if source_size > remaining_bytes:
            raise ArtifactError("产物总大小超过限制。")
        text = source.read_text(encoding="utf-8", errors="replace")
        redacted = redact_text(text)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(redacted, encoding="utf-8")
        return destination.stat().st_size

    def _manifest(self, root: Path) -> dict[str, Any]:
        files = []
        total = 0
        for path in sorted(root.rglob("*")):
            if not path.is_file():
                continue
            relative = path.relative_to(root).as_posix()
            size = path.stat().st_size
            total += size
            files.append(
                {
                    "path": relative,
                    "role": _artifact_role(relative),
                    "size_bytes": size,
                    "sha256": _sha256_file(path),
                }
            )
        return {
            "schema": "cwapi.artifact-manifest.v1",
            "created_at": datetime.now(timezone.utc).isoformat(),
            "large_file_transport": "google_drive_only",
            "file_count": len(files),
            "total_bytes": total,
            "files": files,
        }

    def _atomic_copy_directory(self, source: Path, destination: Path) -> None:
        destination.parent.mkdir(parents=True, exist_ok=True)
        if destination.exists():
            shutil.rmtree(destination)
        staging = destination.parent / f".{destination.name}.staging"
        if staging.exists():
            shutil.rmtree(staging)
        shutil.copytree(source, staging)
        staging.replace(destination)

    def publish(
        self,
        *,
        task: dict[str, Any],
        result_payload: dict[str, Any],
        step_log_paths: Iterable[tuple[Path, Path]],
    ) -> ArtifactBundle:
        task_id = validate_task_id(str(task["task_id"]))
        repository = str(task["repository"])
        repo_key = repository_key(repository)
        final_root = ensure_within(
            self.config.results_path,
            self.config.results_path / repo_key / task_id,
        )

        with tempfile.TemporaryDirectory(
            prefix=f"cwapi-artifact-{task_id}-",
            dir=str(self.config.results_path),
        ) as temporary:
            staging_root = Path(temporary)
            self._write_json(staging_root / "task.json", task)
            self._write_json(staging_root / "result.json", result_payload)

            consumed = sum(
                path.stat().st_size
                for path in (staging_root / "task.json", staging_root / "result.json")
            )
            steps_root = staging_root / "steps"
            for index, (stdout_source, stderr_source) in enumerate(
                step_log_paths,
                start=1,
            ):
                stdout_destination = steps_root / f"{index:03d}.stdout.log"
                stderr_destination = steps_root / f"{index:03d}.stderr.log"
                junit_source = stdout_source.with_name(
                    f"{stdout_source.stem}.junit.xml"
                )
                junit_destination = steps_root / f"{index:03d}.junit.xml"
                consumed += self._copy_redacted_text(
                    stdout_source,
                    stdout_destination,
                    remaining_bytes=self.config.artifact_max_total_bytes - consumed,
                )
                consumed += self._copy_redacted_text(
                    stderr_source,
                    stderr_destination,
                    remaining_bytes=self.config.artifact_max_total_bytes - consumed,
                )
                consumed += self._copy_redacted_text(
                    junit_source,
                    junit_destination,
                    remaining_bytes=self.config.artifact_max_total_bytes - consumed,
                )
                if consumed > self.config.artifact_max_total_bytes:
                    raise ArtifactError("产物总大小超过限制。")

            manifest = self._manifest(staging_root)
            self._write_json(staging_root / "manifest.json", manifest)
            manifest_sha256 = _sha256_file(staging_root / "manifest.json")
            (staging_root / "sha256.txt").write_text(
                "\n".join(
                    f"{entry['sha256']}  {entry['path']}"
                    for entry in manifest["files"]
                )
                + "\n",
                encoding="utf-8",
            )

            final_manifest = self._manifest(staging_root)
            total_bytes = int(final_manifest["total_bytes"])
            if total_bytes > self.config.artifact_max_total_bytes:
                raise ArtifactError("产物总大小超过限制。")

            final_root.parent.mkdir(parents=True, exist_ok=True)
            if final_root.exists():
                shutil.rmtree(final_root)
            shutil.copytree(staging_root, final_root)

        zip_path: Path | None = None
        if self.config.create_zip_bundle:
            zip_path = final_root.with_suffix(".zip")
            temporary_zip = zip_path.with_suffix(".zip.tmp")
            if temporary_zip.exists():
                temporary_zip.unlink()
            with zipfile.ZipFile(
                temporary_zip,
                "w",
                compression=zipfile.ZIP_DEFLATED,
                compresslevel=6,
            ) as archive:
                for path in sorted(final_root.rglob("*")):
                    if path.is_file():
                        archive.write(path, path.relative_to(final_root).as_posix())
            temporary_zip.replace(zip_path)

        drive_relative_path: str | None = None
        sync_status = "local_only"
        if self.config.drive_sync_path is not None:
            drive_root = ensure_within(
                self.config.drive_sync_path,
                self.config.drive_sync_path
                / self.config.drive_subdirectory
                / repo_key
                / task_id,
            )
            self._atomic_copy_directory(final_root, drive_root)
            if zip_path is not None:
                drive_zip = drive_root.parent / f"{task_id}.zip"
                shutil.copy2(zip_path, drive_zip)
            drive_relative_path = drive_root.relative_to(
                self.config.drive_sync_path
            ).as_posix()
            sync_status = "queued_for_google_drive_desktop"

        return ArtifactBundle(
            task_id=task_id,
            repository=repository,
            local_path=str(final_root),
            drive_relative_path=drive_relative_path,
            manifest_sha256=manifest_sha256,
            total_bytes=total_bytes,
            sync_status=sync_status,
            zip_path=str(zip_path) if zip_path is not None else None,
        )

    def cleanup_expired(
        self,
        *,
        now: datetime | None = None,
    ) -> list[str]:
        """Historical RESULT/Artifact output is user-managed and never time-expired."""
        del now
        return []
