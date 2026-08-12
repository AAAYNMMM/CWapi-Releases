from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from typing import Any


class GoTransportError(RuntimeError):
    pass


class GoDraftTransportClient:
    def __init__(
        self,
        base_url: str,
        *,
        secret: str | None = None,
        timeout_seconds: float = 120.0,
    ) -> None:
        normalized = base_url.strip().rstrip("/")
        if not normalized.startswith(
            ("http://127.0.0.1:", "http://localhost:", "http://[::1]:")
        ):
            raise ValueError("Go transport must use a loopback HTTP address.")
        self.base_url = normalized
        self.secret = secret or ""
        self.timeout_seconds = timeout_seconds

    @classmethod
    def from_environment(cls, base_url: str) -> "GoDraftTransportClient":
        return cls(
            base_url,
            secret=os.environ.get("CWAPI_TRANSPORT_SECRET"),
        )

    def _request(
        self,
        path: str,
        payload: dict[str, Any] | None = None,
        *,
        method: str = "POST",
        timeout_seconds: float | None = None,
    ) -> dict[str, Any]:
        body = None
        headers = {"Accept": "application/json"}
        if payload is not None:
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = "application/json; charset=utf-8"
        if self.secret:
            headers["Authorization"] = f"Bearer {self.secret}"
        request = urllib.request.Request(
            f"{self.base_url}{path}",
            data=body,
            headers=headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(
                request,
                timeout=timeout_seconds or self.timeout_seconds,
            ) as response:
                raw = response.read()
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")[:1000]
            raise GoTransportError(
                f"Go transport HTTP {exc.code}: {detail}"
            ) from exc
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            raise GoTransportError(f"Go transport unavailable: {exc}") from exc
        try:
            decoded = json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise GoTransportError("Go transport returned invalid JSON.") from exc
        if not isinstance(decoded, dict):
            raise GoTransportError("Go transport returned a non-object response.")
        if decoded.get("error"):
            raise GoTransportError(str(decoded["error"]))
        return decoded

    def health(self) -> dict[str, Any]:
        return self._request(
            "/health",
            method="GET",
            timeout_seconds=5.0,
        )

    def events(self, *, after_id: int = 0) -> list[dict[str, Any]]:
        query = urllib.parse.urlencode({"after": max(0, int(after_id))})
        response = self._request(
            f"/v1/events?{query}",
            method="GET",
            timeout_seconds=5.0,
        )
        events = response.get("events", [])
        if not isinstance(events, list):
            raise GoTransportError("Go transport returned invalid events.")
        return [dict(item) for item in events if isinstance(item, dict)]

    def profile(self) -> dict[str, Any]:
        response = self._request(
            "/v1/profile",
            method="GET",
            timeout_seconds=30.0,
        )
        email = str(response.get("email_address") or "").strip()
        if not email or "@" not in email:
            raise GoTransportError("Go transport returned no Gmail account address.")
        response["email_address"] = email
        return response

    def authorize(self) -> None:
        response = self._request(
            "/v1/oauth/authorize",
            {},
            timeout_seconds=610.0,
        )
        if response.get("status") != "authorized":
            raise GoTransportError("Go transport did not complete OAuth authorization.")

    def list_drafts(self, *, query: str, max_results: int) -> list[dict[str, str]]:
        response = self._request(
            "/v1/drafts/list",
            {"query": query, "max_results": max_results},
        )
        drafts = response.get("drafts", [])
        if not isinstance(drafts, list):
            raise GoTransportError("Go transport returned invalid drafts.")
        return [dict(item) for item in drafts if isinstance(item, dict)]

    def get_draft(self, draft_id: str) -> dict[str, str]:
        return {
            key: str(value)
            for key, value in self._request(
                "/v1/drafts/get",
                {"draft_id": draft_id},
            ).items()
        }

    def find_exact_draft_by_subject(self, subject: str) -> str | None:
        value = self._request(
            "/v1/drafts/find",
            {"subject": subject},
        ).get("draft_id")
        return str(value) if value else None

    def create_draft(self, *, subject: str, body: str) -> str:
        value = self._request(
            "/v1/drafts/create",
            {"subject": subject, "body": body},
        ).get("draft_id")
        if not value:
            raise GoTransportError("Go transport returned no draft_id.")
        return str(value)

    def close(self) -> None:
        return None
