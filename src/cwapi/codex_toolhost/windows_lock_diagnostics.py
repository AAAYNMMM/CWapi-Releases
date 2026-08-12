from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import subprocess
import time
from typing import Any


@dataclass(frozen=True)
class WindowsLockHolder:
    pid: int
    app_name: str
    service_name: str
    image_path: str | None
    restartable: bool

    def to_payload(self) -> dict[str, Any]:
        return {
            "pid": self.pid,
            "app_name": self.app_name,
            "service_name": self.service_name,
            "image_path": self.image_path,
            "restartable": self.restartable,
        }


@dataclass(frozen=True)
class WindowsLockRecovery:
    holders: tuple[WindowsLockHolder, ...]
    terminated_pids: tuple[int, ...]
    termination_errors: tuple[str, ...]

    def describe(self) -> str:
        if not self.holders:
            return "lock_holders=[]"
        values = []
        for holder in self.holders:
            values.append(
                "pid={pid}, app={app}, image={image}, service={service}".format(
                    pid=holder.pid,
                    app=holder.app_name or "?",
                    image=holder.image_path or "?",
                    service=holder.service_name or "-",
                )
            )
        terminated = ",".join(str(pid) for pid in self.terminated_pids) or "-"
        errors = "; ".join(self.termination_errors) or "-"
        return (
            f"lock_holders=[{' | '.join(values)}]; "
            f"terminated_private_pids={terminated}; termination_errors={errors}"
        )


def _process_image_path(pid: int) -> str | None:
    if os.name != "nt":
        return None
    import ctypes
    from ctypes import wintypes

    process_query_limited_information = 0x1000
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    open_process = kernel32.OpenProcess
    open_process.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    open_process.restype = wintypes.HANDLE
    query_image = kernel32.QueryFullProcessImageNameW
    query_image.argtypes = [
        wintypes.HANDLE,
        wintypes.DWORD,
        wintypes.LPWSTR,
        ctypes.POINTER(wintypes.DWORD),
    ]
    query_image.restype = wintypes.BOOL
    close_handle = kernel32.CloseHandle
    close_handle.argtypes = [wintypes.HANDLE]
    close_handle.restype = wintypes.BOOL

    handle = open_process(process_query_limited_information, False, pid)
    if not handle:
        return None
    try:
        size = wintypes.DWORD(32768)
        buffer = ctypes.create_unicode_buffer(size.value)
        if not query_image(handle, 0, buffer, ctypes.byref(size)):
            return None
        return buffer.value
    finally:
        close_handle(handle)


def list_windows_lock_holders(path: Path) -> tuple[WindowsLockHolder, ...]:
    """Return processes reported by Windows Restart Manager for one file."""

    if os.name != "nt":
        return ()

    import ctypes
    from ctypes import wintypes

    error_more_data = 234
    cch_rm_session_key = 32
    cch_rm_max_app_name = 255
    cch_rm_max_svc_name = 63

    class RM_UNIQUE_PROCESS(ctypes.Structure):
        _fields_ = [
            ("dwProcessId", wintypes.DWORD),
            ("ProcessStartTime", wintypes.FILETIME),
        ]

    class RM_PROCESS_INFO(ctypes.Structure):
        _fields_ = [
            ("Process", RM_UNIQUE_PROCESS),
            ("strAppName", wintypes.WCHAR * (cch_rm_max_app_name + 1)),
            ("strServiceShortName", wintypes.WCHAR * (cch_rm_max_svc_name + 1)),
            ("ApplicationType", wintypes.DWORD),
            ("AppStatus", wintypes.ULONG),
            ("TSSessionId", wintypes.DWORD),
            ("bRestartable", wintypes.BOOL),
        ]

    restart_manager = ctypes.WinDLL("Rstrtmgr", use_last_error=True)
    start_session = restart_manager.RmStartSession
    start_session.argtypes = [
        ctypes.POINTER(wintypes.DWORD),
        wintypes.DWORD,
        wintypes.LPWSTR,
    ]
    start_session.restype = wintypes.DWORD
    register_resources = restart_manager.RmRegisterResources
    register_resources.argtypes = [
        wintypes.DWORD,
        wintypes.UINT,
        ctypes.POINTER(wintypes.LPCWSTR),
        wintypes.UINT,
        ctypes.c_void_p,
        wintypes.UINT,
        ctypes.c_void_p,
    ]
    register_resources.restype = wintypes.DWORD
    get_list = restart_manager.RmGetList
    get_list.argtypes = [
        wintypes.DWORD,
        ctypes.POINTER(wintypes.UINT),
        ctypes.POINTER(wintypes.UINT),
        ctypes.POINTER(RM_PROCESS_INFO),
        ctypes.POINTER(wintypes.DWORD),
    ]
    get_list.restype = wintypes.DWORD
    end_session = restart_manager.RmEndSession
    end_session.argtypes = [wintypes.DWORD]
    end_session.restype = wintypes.DWORD

    session = wintypes.DWORD()
    session_key = ctypes.create_unicode_buffer(cch_rm_session_key + 1)
    result = start_session(ctypes.byref(session), 0, session_key)
    if result != 0:
        return ()
    try:
        resources = (wintypes.LPCWSTR * 1)(str(path.resolve()))
        result = register_resources(session, 1, resources, 0, None, 0, None)
        if result != 0:
            return ()

        needed = wintypes.UINT(0)
        count = wintypes.UINT(0)
        reboot_reasons = wintypes.DWORD(0)
        result = get_list(
            session,
            ctypes.byref(needed),
            ctypes.byref(count),
            None,
            ctypes.byref(reboot_reasons),
        )
        if result == 0 and needed.value == 0:
            return ()
        if result != error_more_data or needed.value == 0:
            return ()

        entries = (RM_PROCESS_INFO * needed.value)()
        count = wintypes.UINT(needed.value)
        result = get_list(
            session,
            ctypes.byref(needed),
            ctypes.byref(count),
            entries,
            ctypes.byref(reboot_reasons),
        )
        if result != 0:
            return ()

        holders: list[WindowsLockHolder] = []
        for index in range(count.value):
            entry = entries[index]
            pid = int(entry.Process.dwProcessId)
            holders.append(
                WindowsLockHolder(
                    pid=pid,
                    app_name=str(entry.strAppName),
                    service_name=str(entry.strServiceShortName),
                    image_path=_process_image_path(pid),
                    restartable=bool(entry.bRestartable),
                )
            )
        return tuple(holders)
    finally:
        end_session(session)


def _within(path: Path, root: Path) -> bool:
    try:
        path.resolve().relative_to(root.resolve())
        return True
    except (OSError, ValueError):
        return False


def recover_private_codex_lock(
    executable_path: Path,
    *,
    settle_seconds: float = 0.5,
) -> WindowsLockRecovery:
    """Terminate only stale processes whose image is inside this private runtime."""

    holders = list_windows_lock_holders(executable_path)
    release_root = (
        executable_path.parent.parent
        if executable_path.parent.name.casefold() == "bin"
        else executable_path.parent
    )
    terminated: list[int] = []
    errors: list[str] = []
    current_pid = os.getpid()

    for holder in holders:
        if holder.pid == current_pid or not holder.image_path:
            continue
        image = Path(holder.image_path)
        if not _within(image, release_root):
            continue
        try:
            completed = subprocess.run(
                ["taskkill", "/PID", str(holder.pid), "/T", "/F"],
                capture_output=True,
                text=True,
                shell=False,
                check=False,
                timeout=15,
            )
        except (OSError, subprocess.SubprocessError) as exc:
            errors.append(f"pid={holder.pid}: {exc}")
            continue
        if completed.returncode == 0:
            terminated.append(holder.pid)
        else:
            detail = (completed.stderr or completed.stdout).strip()
            errors.append(
                f"pid={holder.pid}: taskkill exit={completed.returncode}: {detail}"
            )

    if terminated and settle_seconds > 0:
        time.sleep(settle_seconds)
    return WindowsLockRecovery(
        holders=holders,
        terminated_pids=tuple(terminated),
        termination_errors=tuple(errors),
    )


def describe_launch_os_error(exc: BaseException) -> str:
    details: list[str] = [f"type={type(exc).__name__}", f"repr={exc!r}"]
    for name in ("winerror", "errno", "filename", "filename2"):
        value = getattr(exc, name, None)
        if value is not None:
            details.append(f"{name}={value!r}")
    return ", ".join(details)


__all__ = [
    "WindowsLockHolder",
    "WindowsLockRecovery",
    "describe_launch_os_error",
    "list_windows_lock_holders",
    "recover_private_codex_lock",
]
