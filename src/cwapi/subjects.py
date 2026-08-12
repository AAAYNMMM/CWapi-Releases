from __future__ import annotations

import re
from dataclasses import dataclass


_SUBJECT_RE = re.compile(
    r"^\[CWapi/1\]\[(?P<message_type>[A-Z_]+)\]"
    r"\[(?P<status>[A-Z_]+)\]\[(?P<entity_id>[A-Za-z0-9._:-]+)\]$"
)


@dataclass(frozen=True)
class ParsedSubject:
    message_type: str
    status: str
    entity_id: str


def parse_subject(subject: str) -> ParsedSubject | None:
    match = _SUBJECT_RE.fullmatch(subject.strip())
    if not match:
        return None
    return ParsedSubject(**match.groupdict())


def build_subject(message_type: str, status: str, entity_id: str) -> str:
    return f"[CWapi/1][{message_type}][{status}][{entity_id}]"
