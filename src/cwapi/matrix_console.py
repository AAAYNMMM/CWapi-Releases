from __future__ import annotations

import os
import shutil
from datetime import datetime
from pathlib import Path
from typing import Any, TextIO

from .observability import RunnerConsole


_MATRIX_CODES = {
    "ghost": "38;5;22",
    "dark": "38;5;28",
    "mid": "38;5;40",
    "bright": "38;5;46",
    "pale": "38;5;120",
    "white": "38;5;255",
    "gray": "38;5;244",
    "amber": "38;5;220",
    "red": "38;5;196",
}

_PANEL_CATEGORIES = frozenset({"START", "TASK", "RESULT", "STOP"})

_CATEGORY_GLYPHS = {
    "RECOVER": "↺",
    "WORKTREE": "◇",
    "STEP": "▸",
    "ACTIVE": "◌",
    "ARTIFACT": "⬢",
    "POLL": "↻",
    "CANCEL": "!",
    "CLEANUP": "⌁",
    "TRANSPORT": "↯",
    "OUT": "│",
    "ERR": "│",
    "WATCH": "!",
}

_ASCII_GLYPHS = {
    "↺": "~",
    "◇": "+",
    "▸": ">",
    "◌": ".",
    "⬢": "#",
    "↻": "~",
    "⌁": "~",
    "↯": "!",
    "│": "|",
    "✓": "+",
    "✕": "x",
}


class MatrixRunnerConsole(RunnerConsole):
    """Matrix-inspired terminal renderer backed by the in-memory Runner audit log."""

    def __init__(
        self,
        log_path: Path,
        *,
        stream: TextIO | None = None,
        max_bytes: int = 5 * 1024 * 1024,
        color: bool | None = None,
        console_width: int | None = None,
        theme: str | None = None,
    ) -> None:
        super().__init__(
            log_path,
            stream=stream,
            max_bytes=max_bytes,
            color=color,
            console_width=console_width,
        )
        requested = (theme or os.environ.get("CWAPI_LOG_THEME", "matrix")).strip().casefold()
        self.theme = "clean" if requested in {"clean", "classic"} else "matrix"

    def _console_lines(
        self,
        now: datetime,
        category: str,
        level: str,
        message: str,
        details: dict[str, Any] | None,
    ) -> list[str]:
        if self.theme == "clean":
            return super()._console_lines(now, category, level, message, details)
        if category == "START":
            return self._boot_panel(now, message, details or {})
        if category in _PANEL_CATEGORIES or level == "ERROR":
            return self._event_panel(now, category, level, message, details or {})
        return self._event_rail(now, category, level, message, details or {})

    def _boot_panel(
        self,
        now: datetime,
        message: str,
        details: dict[str, Any],
    ) -> list[str]:
        width = self._matrix_width()
        title = " CWAPI // LOCAL EXECUTION GRID "
        top = self._border("╔═[" + title + "]", "═", "╗", width)
        timecode = now.strftime("%H:%M:%S.%f")[:12]
        headline = self._fit(f"║  NODE LINK ESTABLISHED  ::  {timecode}  ::  {message}", width, "║")
        rows = self._format_detail_rows(details)
        lines = [
            self._tone(top, "bright", bold=True),
            self._tone(headline, "pale", bold=True),
            self._tone(self._fit("║  CHANNELS  [GMAIL] [SQLITE] [WORKTREE] [RESULT]  ::  SECURE", width, "║"), "mid"),
        ]
        for index, row in enumerate(rows, start=1):
            lines.append(
                self._tone(
                    self._fit(f"║  0x{index:02X}  {row}", width, "║"),
                    "mid" if index % 2 else "dark",
                )
            )
        lines.append(self._tone("╚" + "═" * (width - 2) + "╝", "bright", bold=True))
        return self._ascii_if_needed(lines)

    def _event_panel(
        self,
        now: datetime,
        category: str,
        level: str,
        message: str,
        details: dict[str, Any],
    ) -> list[str]:
        width = self._matrix_width()
        signal = self._signal(category, level, message, details)
        timecode = now.strftime("%H:%M:%S.%f")[:12]
        title = f"╭─[ {timecode} ]─[ {category}::{signal} ]"
        tone = self._level_tone(level, success=signal in {"ONLINE", "PASS", "DELIVERED"})
        lines = [self._tone(self._border(title, "─", "╮", width), tone, bold=True)]
        glyph = "✕" if level == "ERROR" else "!" if level == "WARN" else "▸"
        lines.append(self._tone(self._fit(f"│  {glyph}  {message}", width, "│"), tone, bold=level != "INFO"))
        rows = self._format_detail_rows(details)
        for index, row in enumerate(rows, start=1):
            lines.append(self._tone(self._fit(f"│  0x{index:02X}  {row}", width, "│"), "mid"))
        footer = f"╰─[ LINK::{self._footer_state(level, signal)} ]"
        lines.append(self._tone(self._border(footer, "─", "╯", width), tone))
        return self._ascii_if_needed(lines)

    def _event_rail(
        self,
        now: datetime,
        category: str,
        level: str,
        message: str,
        details: dict[str, Any],
    ) -> list[str]:
        timecode = now.strftime("%H:%M:%S.%f")[:12]
        signal = self._signal(category, level, message, details)
        glyph = self._glyph(category, level, signal)
        tone = self._level_tone(level, success=signal == "PASS")
        label = f"{category}::{signal}"
        headline = f"{timecode}  {glyph}  {label:<20} // {message}"
        lines = [self._tone(headline.rstrip(), tone, bold=signal == "PASS")]
        rows = self._format_detail_rows(details)
        for index, row in enumerate(rows, start=1):
            connector = "└─" if index == len(rows) else "├─"
            lines.append(self._tone(f"              {connector} 0x{index:02X}  {row}", "dark"))
        return self._ascii_if_needed(lines)

    def _matrix_width(self) -> int:
        detected = self.console_width or shutil.get_terminal_size(fallback=(120, 24)).columns
        return max(68, min(132, detected))

    @staticmethod
    def _border(prefix: str, fill: str, suffix: str, width: int) -> str:
        return prefix + fill * max(1, width - len(prefix) - len(suffix)) + suffix

    @staticmethod
    def _fit(prefix: str, width: int, suffix: str) -> str:
        available = max(1, width - len(suffix))
        text = prefix if len(prefix) <= available else prefix[: max(1, available - 1)] + "…"
        return text + " " * max(0, available - len(text)) + suffix

    def _signal(
        self,
        category: str,
        level: str,
        message: str,
        details: dict[str, Any],
    ) -> str:
        folded = message.casefold()
        if level == "ERROR":
            return "FAULT"
        if level == "WARN":
            return "ALERT"
        if category == "START":
            return "ONLINE"
        if category == "STOP":
            return "OFFLINE"
        if category == "TASK":
            execution = str(details.get("execution", "")).upper()
            return execution or ("CLAIM" if "认领" in message else "STATE")
        if category == "RESULT":
            if any(marker in folded for marker in ("completed", "uploaded", "delivered")):
                return "DELIVERED"
            return str(details.get("result", "STATE")).upper()
        if category == "STEP":
            return "PASS" if "completed" in folded else "STATE"
        if category in {"OUT", "ERR"}:
            return "STREAM"
        if category == "ACTIVE":
            return "PULSE"
        if category == "TRANSPORT":
            return "LINK"
        return "EVENT"

    @staticmethod
    def _footer_state(level: str, signal: str) -> str:
        if level == "ERROR":
            return "DEGRADED"
        if level == "WARN":
            return "ATTENTION"
        if signal == "OFFLINE":
            return "CLOSED"
        return "SECURE"

    def _glyph(self, category: str, level: str, signal: str) -> str:
        if level == "ERROR":
            return "✕"
        if level == "WARN":
            return "!"
        if signal == "PASS":
            return "✓"
        return _CATEGORY_GLYPHS.get(category, "▸")

    @staticmethod
    def _level_tone(level: str, *, success: bool = False) -> str:
        if level == "ERROR":
            return "red"
        if level == "WARN":
            return "amber"
        if success:
            return "bright"
        return "pale"

    def _tone(self, text: str, tone: str, *, bold: bool = False, dim: bool = False) -> str:
        if not self.color:
            return text
        codes: list[str] = []
        if bold:
            codes.append("1")
        if dim:
            codes.append("2")
        codes.append(_MATRIX_CODES.get(tone, _MATRIX_CODES["mid"]))
        return f"\x1b[{';'.join(codes)}m{text}\x1b[0m"

    def _ascii_if_needed(self, lines: list[str]) -> list[str]:
        if self._unicode:
            return lines
        replacements = {
            "╔": "+",
            "╗": "+",
            "╚": "+",
            "╝": "+",
            "╭": "+",
            "╮": "+",
            "╰": "+",
            "╯": "+",
            "═": "=",
            "─": "-",
            "║": "|",
            "│": "|",
            "├": "+",
            "└": "+",
            "…": "...",
        }
        replacements.update(_ASCII_GLYPHS)
        converted: list[str] = []
        for line in lines:
            for source, target in replacements.items():
                line = line.replace(source, target)
            converted.append(line)
        return converted
