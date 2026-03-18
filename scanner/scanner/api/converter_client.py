# scanner/scanner/api/converter_client.py
import logging
from typing import Optional, Tuple

import requests

logger = logging.getLogger(__name__)


class ConverterClient:
    """HTTP client for the converter API ingest endpoints."""

    def __init__(self, base_url: str, service_token: str) -> None:
        self._base = base_url.rstrip("/")
        self._session = requests.Session()
        self._session.headers.update({
            "X-Service-Token": service_token,
            "Content-Type": "application/json",
        })

    def register(
        self,
        source_path: str,
        source_filename: str,
        content_kind: str = "movie",
        normalized_name: Optional[str] = None,
        tmdb_id: Optional[str] = None,
        file_size_bytes: Optional[int] = None,
        quality_score: Optional[int] = None,
        is_upgrade_candidate: bool = False,
        duplicate_of_movie_id: Optional[int] = None,
        review_reason: Optional[str] = None,
        stable_since: Optional[str] = None,
    ) -> int:
        """Register a file with the converter API. Returns api_item_id."""
        payload: dict = {
            "source_path": source_path,
            "source_filename": source_filename,
            "content_kind": content_kind,
            "is_upgrade_candidate": is_upgrade_candidate,
        }
        for key, val in [
            ("normalized_name", normalized_name),
            ("tmdb_id", tmdb_id),
            ("file_size_bytes", file_size_bytes),
            ("quality_score", quality_score),
            ("duplicate_of_movie_id", duplicate_of_movie_id),
            ("review_reason", review_reason),
            ("stable_since", stable_since),
        ]:
            if val is not None:
                payload[key] = val

        resp = self._session.post(
            f"{self._base}/api/ingest/incoming/register",
            json=payload,
            timeout=15,
        )
        resp.raise_for_status()
        return resp.json()["id"]

    def get_status(self, api_item_id: int) -> Tuple[str, Optional[str]]:
        """Fetch current status. Returns (status, error_message). Raises on error."""
        resp = self._session.get(
            f"{self._base}/api/ingest/incoming/{api_item_id}",
            timeout=15,
        )
        resp.raise_for_status()
        data = resp.json()
        return data["status"], data.get("error_message")
