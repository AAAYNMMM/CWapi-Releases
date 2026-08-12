from __future__ import annotations

import hashlib
from typing import Any

from .canonical_json import canonical_json_bytes


def content_sha256(value: Any) -> str:
    return hashlib.sha256(canonical_json_bytes(value)).hexdigest()
