from __future__ import annotations

import json
import os
from pathlib import Path
from typing import BinaryIO


class RunnerAlreadyActive(RuntimeError):
    pass


class RunnerLock:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.handle: BinaryIO | None = None

    def acquire(self) -> None:
        if self.handle is not None:
            return
        self.path.parent.mkdir(parents=True, exist_ok=True)
        handle = self.path.open("a+b")
        handle.seek(0, os.SEEK_END)
        if handle.tell() == 0:
            handle.write(b"0")
            handle.flush()
        handle.seek(0)
        try:
            if os.name == "nt":
                import msvcrt

                msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl

                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as exc:
            handle.close()
            raise RunnerAlreadyActive(
                f"CWapi Runner 已经运行或锁不可用：{self.path}"
            ) from exc
        handle.seek(0)
        handle.truncate()
        handle.write(
            json.dumps({"pid": os.getpid()}, sort_keys=True).encode("utf-8")
        )
        handle.flush()
        handle.seek(0)
        self.handle = handle

    def release(self) -> None:
        handle = self.handle
        if handle is None:
            return
        handle.seek(0)
        try:
            if os.name == "nt":
                import msvcrt

                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                import fcntl

                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        finally:
            handle.close()
            self.handle = None

    def __enter__(self) -> "RunnerLock":
        self.acquire()
        return self

    def __exit__(self, exc_type, exc, traceback) -> None:
        del exc_type, exc, traceback
        self.release()
