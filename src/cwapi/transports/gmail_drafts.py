from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import threading

from .go_service import GoDraftTransportClient

_MAX_LIST_RESULTS = 5000
_MANAGED_CLIENT_LOCK = threading.RLock()
_MANAGED_GO_CLIENT: GoDraftTransportClient | None = None


@dataclass(frozen=True)
class GmailDraft:
    draft_id: str
    message_id: str
    subject: str
    body: str


class GmailDraftLookupError(RuntimeError):
    pass


def install_managed_go_client(client: GoDraftTransportClient) -> None:
    """Install the process-owned Go client used by normal Runner construction."""

    global _MANAGED_GO_CLIENT
    with _MANAGED_CLIENT_LOCK:
        if _MANAGED_GO_CLIENT is not None and _MANAGED_GO_CLIENT is not client:
            raise RuntimeError("当前进程已经安装另一个 Go Gmail Transport client。")
        _MANAGED_GO_CLIENT = client


def clear_managed_go_client(client: GoDraftTransportClient | None = None) -> None:
    global _MANAGED_GO_CLIENT
    with _MANAGED_CLIENT_LOCK:
        if client is None or _MANAGED_GO_CLIENT is client:
            _MANAGED_GO_CLIENT = None


def _current_managed_go_client() -> GoDraftTransportClient | None:
    with _MANAGED_CLIENT_LOCK:
        return _MANAGED_GO_CLIENT


class GmailDraftTransport:
    """Thin Python facade over the mandatory loopback Go Gmail Transport."""

    def __init__(
        self,
        *,
        account: str,
        credentials_path: Path,
        token_path: Path,
        go_client: GoDraftTransportClient | None = None,
    ) -> None:
        self.account = account
        self.credentials_path = credentials_path
        self.token_path = token_path
        managed = go_client or _current_managed_go_client()
        if managed is not None:
            self._go_client = managed
            return
        transport_url = os.environ.get("CWAPI_GO_TRANSPORT_URL", "").strip()
        if not transport_url:
            raise RuntimeError(
                "Go Gmail Transport 未启动：缺少当前进程的受管 Transport client。"
            )
        self._go_client = GoDraftTransportClient.from_environment(transport_url)

    @property
    def go_client(self) -> GoDraftTransportClient:
        return self._go_client

    def close(self) -> None:
        self._go_client.close()

    def authorize(self) -> None:
        self._go_client.authorize()

    def find_exact_draft_by_subject(self, subject: str) -> str | None:
        try:
            return self._go_client.find_exact_draft_by_subject(subject)
        except Exception as exc:
            raise GmailDraftLookupError(
                f"Gmail draft lookup failed through Go Transport: {exc}"
            ) from exc

    def list_drafts(
        self,
        *,
        query: str,
        max_results: int,
    ) -> list[GmailDraft]:
        bounded = max(1, min(int(max_results), _MAX_LIST_RESULTS))
        return [
            GmailDraft(
                draft_id=str(item.get("draft_id", "")),
                message_id=str(item.get("message_id", "")),
                subject=str(item.get("subject", "")),
                body=str(item.get("body", "")),
            )
            for item in self._go_client.list_drafts(
                query=query,
                max_results=bounded,
            )
        ]

    def get_draft(self, draft_id: str) -> GmailDraft:
        item = self._go_client.get_draft(draft_id)
        return GmailDraft(
            draft_id=str(item.get("draft_id", "")),
            message_id=str(item.get("message_id", "")),
            subject=str(item.get("subject", "")),
            body=str(item.get("body", "")),
        )

    def create_draft(self, *, subject: str, body: str) -> str:
        return self._go_client.create_draft(subject=subject, body=body)
