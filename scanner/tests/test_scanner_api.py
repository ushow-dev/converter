# scanner/tests/test_scanner_api.py
import queue
from unittest.mock import patch, MagicMock

import pytest

# A minimal fake Config to avoid needing env vars
class FakeConfig:
    service_token = "secret"
    api_port = 8080


def _make_app(move_q):
    from scanner.api.server import create_app
    cfg = FakeConfig()
    app = create_app(cfg, move_q)
    app.config["TESTING"] = True
    return app.test_client()


# ── Auth ─────────────────────────────────────────────────────────────────────

def test_claim_unauthorized():
    client = _make_app(queue.Queue())
    resp = client.post("/api/v1/incoming/claim", json={})
    assert resp.status_code == 401


def test_progress_unauthorized():
    client = _make_app(queue.Queue())
    resp = client.post("/api/v1/incoming/1/progress", json={"status": "copying"})
    assert resp.status_code == 401


# ── Claim ────────────────────────────────────────────────────────────────────

def test_claim_empty():
    """When no registered items exist, returns empty list."""
    mq = queue.Queue()
    client = _make_app(mq)
    with patch("scanner.api.server._claim_items", return_value=[]) as mock_claim:
        resp = client.post(
            "/api/v1/incoming/claim",
            json={"limit": 1, "claim_ttl_sec": 900},
            headers={"X-Service-Token": "secret"},
        )
    assert resp.status_code == 200
    assert resp.get_json() == {"items": []}
    mock_claim.assert_called_once_with(1, 900)


def test_claim_returns_items():
    """Returns claimed items."""
    item = {
        "id": 42,
        "source_path": "/incoming/film.mkv",
        "source_filename": "film.mkv",
        "normalized_name": "film_2024_[12345]",
        "tmdb_id": "12345",
        "content_kind": "movie",
    }
    mq = queue.Queue()
    client = _make_app(mq)
    with patch("scanner.api.server._claim_items", return_value=[item]):
        resp = client.post(
            "/api/v1/incoming/claim",
            json={"limit": 1, "claim_ttl_sec": 900},
            headers={"X-Service-Token": "secret"},
        )
    assert resp.status_code == 200
    data = resp.get_json()
    assert len(data["items"]) == 1
    assert data["items"][0]["id"] == 42


# ── Progress ─────────────────────────────────────────────────────────────────

def test_progress_copying():
    mq = queue.Queue()
    client = _make_app(mq)
    with patch("scanner.api.server._update_status") as mock_update:
        resp = client.post(
            "/api/v1/incoming/42/progress",
            json={"status": "copying"},
            headers={"X-Service-Token": "secret"},
        )
    assert resp.status_code == 204
    mock_update.assert_called_once_with(42, "copying")


def test_progress_invalid_status():
    mq = queue.Queue()
    client = _make_app(mq)
    resp = client.post(
        "/api/v1/incoming/42/progress",
        json={"status": "bad_status"},
        headers={"X-Service-Token": "secret"},
    )
    assert resp.status_code == 400


# ── Complete ─────────────────────────────────────────────────────────────────

def test_complete_enqueues_move():
    """complete endpoint updates status, puts item on move_queue, returns job_id."""
    mq = queue.Queue()
    client = _make_app(mq)
    item_info = {"source_path": "/incoming/film.mkv", "normalized_name": "film_2024_[12345]"}
    with patch("scanner.api.server._get_item_info", return_value=item_info), \
         patch("scanner.api.server._update_status") as mock_update:
        resp = client.post(
            "/api/v1/incoming/42/complete",
            json={},
            headers={"X-Service-Token": "secret"},
        )
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["id"] == 42
    assert data["job_id"] == "ingest-42"
    mock_update.assert_called_once_with(42, "completed")
    task = mq.get_nowait()
    assert task["item_id"] == 42
    assert task["source_path"] == "/incoming/film.mkv"


def test_complete_not_found():
    mq = queue.Queue()
    client = _make_app(mq)
    with patch("scanner.api.server._get_item_info", return_value=None):
        resp = client.post(
            "/api/v1/incoming/99/complete",
            json={},
            headers={"X-Service-Token": "secret"},
        )
    assert resp.status_code == 404


# ── Fail ─────────────────────────────────────────────────────────────────────

def test_fail_updates_status():
    mq = queue.Queue()
    client = _make_app(mq)
    with patch("scanner.api.server._update_status_with_error") as mock_fail:
        resp = client.post(
            "/api/v1/incoming/42/fail",
            json={"error_message": "rclone timeout"},
            headers={"X-Service-Token": "secret"},
        )
    assert resp.status_code == 204
    mock_fail.assert_called_once_with(42, "failed", "rclone timeout")
