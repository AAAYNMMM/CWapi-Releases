from __future__ import annotations

import os
import subprocess


def close_codex_app_server(client: object) -> None:
    """Close a Codex client and verify the managed Windows process tree exits.

    Cleanup is deliberately non-throwing because it runs from finally/atexit
    paths. Failures are retained in the client's stderr tail for the next
    diagnostic receipt instead of being silently discarded.
    """

    if getattr(client, "_closed", False):
        return
    setattr(client, "_closed", True)
    process = getattr(client, "_process", None)
    setattr(client, "_process", None)
    if process is None:
        return

    diagnostics: list[str] = []
    stdin = getattr(process, "stdin", None)
    try:
        if stdin is not None:
            stdin.close()
    except OSError as exc:
        diagnostics.append(f"close stdin failed: {exc}")

    try:
        running = process.poll() is None
    except Exception:
        running = True

    if running:
        if os.name == "nt":
            try:
                completed = subprocess.run(
                    ["taskkill", "/PID", str(process.pid), "/T", "/F"],
                    capture_output=True,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    shell=False,
                    timeout=15,
                    check=False,
                )
                if completed.returncode != 0:
                    detail = (completed.stderr or completed.stdout).strip()
                    diagnostics.append(
                        "taskkill failed "
                        f"pid={process.pid} exit={completed.returncode}: {detail[:1000]}"
                    )
            except Exception as exc:
                diagnostics.append(f"taskkill exception pid={process.pid}: {exc}")
        else:
            try:
                process.terminate()
            except Exception as exc:
                diagnostics.append(f"terminate failed pid={process.pid}: {exc}")

    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        diagnostics.append(f"process tree did not exit within 15s pid={process.pid}")
        try:
            process.kill()
            process.wait(timeout=15)
        except Exception as exc:
            diagnostics.append(f"final kill failed pid={process.pid}: {exc}")
    except Exception as exc:
        diagnostics.append(f"wait failed pid={process.pid}: {exc}")

    try:
        client._fail_all_pending()  # type: ignore[attr-defined]
    except Exception as exc:
        diagnostics.append(f"fail pending requests failed: {exc}")

    if diagnostics:
        tail = getattr(client, "_stderr_tail", None)
        if tail is not None:
            executable = getattr(client, "executable_path", "unknown")
            tail.append(
                "\n[CWapi Toolhost cleanup] "
                f"executable={executable}; "
                + " | ".join(diagnostics)
                + "\n"
            )


__all__ = ["close_codex_app_server"]
