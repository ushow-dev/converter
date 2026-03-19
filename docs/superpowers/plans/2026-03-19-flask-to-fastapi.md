# Flask → FastAPI Migration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Flask with FastAPI + Uvicorn in the scanner HTTP API, keeping all endpoints and behaviour identical.

**Architecture:** `scanner/scanner/api/server.py` is rewritten to use FastAPI with Pydantic request models and a closure-based auth dependency. Uvicorn replaces `app.run()`. Tests are rewritten for FastAPI's `TestClient` (starlette/httpx-based).

**Tech Stack:** `fastapi>=0.110`, `uvicorn[standard]>=0.29`, `httpx>=0.27` (test dep)

---

## Chunk 1: Dependencies and server rewrite

### Task 1: Update pyproject.toml

**Files:**
- Modify: `scanner/pyproject.toml`

- [ ] **Step 1: Open `scanner/pyproject.toml` and replace the Flask dependency**

Current `dependencies` list contains `"flask>=3.0"`. Replace it with:

```toml
[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"

[project]
name = "scanner"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "psycopg2-binary>=2.9",
    "guessit>=3.8",
    "requests>=2.31",
    "fastapi>=0.110",
    "uvicorn[standard]>=0.29",
]

[project.optional-dependencies]
dev = ["pytest>=8.0", "pytest-mock>=3.12", "httpx>=0.27"]

[tool.setuptools.packages.find]
where = ["."]
include = ["scanner*"]
```

Key changes: `flask>=3.0` → `fastapi>=0.110` + `uvicorn[standard]>=0.29`; `httpx>=0.27` added to dev deps (required by FastAPI's TestClient).

- [ ] **Step 2: Verify the file looks correct — no Flask references remain**

- [ ] **Step 3: Commit**

```bash
git add scanner/pyproject.toml
git commit -m "chore(scanner): replace flask with fastapi+uvicorn in dependencies"
```

---

### Task 2: Rewrite scanner/scanner/api/server.py

**Files:**
- Modify: `scanner/scanner/api/server.py`

The rewrite must preserve identical HTTP contracts:

| Endpoint | Method | Auth | Success | Error |
|---|---|---|---|---|
| `/api/v1/incoming/claim` | POST | X-Service-Token | 200 `{"items": [...]}` | 401 |
| `/api/v1/incoming/{id}/progress` | POST | X-Service-Token | 204 | 401, 400 |
| `/api/v1/incoming/{id}/complete` | POST | X-Service-Token | 200 `{"id":..,"job_id":..}` | 401, 404 |
| `/api/v1/incoming/{id}/fail` | POST | X-Service-Token | 204 | 401 |

- [ ] **Step 1: Rewrite `scanner/scanner/api/server.py`**

```python
# scanner/scanner/api/server.py
import logging
import queue
from typing import Annotated

import uvicorn
from fastapi import Depends, FastAPI, Header, HTTPException, Response
from pydantic import BaseModel

from scanner import db
from scanner.config import Config

logger = logging.getLogger(__name__)


# ── Pydantic request models ───────────────────────────────────────────────────

class ClaimRequest(BaseModel):
    limit: int = 1
    claim_ttl_sec: int = 900


class ProgressRequest(BaseModel):
    status: str


class FailRequest(BaseModel):
    error_message: str = ""


# ── App factory ───────────────────────────────────────────────────────────────

def create_app(cfg: Config, move_queue: queue.Queue) -> FastAPI:
    app = FastAPI()

    def _auth(x_service_token: Annotated[str, Header()] = "") -> None:
        if x_service_token != cfg.service_token:
            raise HTTPException(status_code=401, detail="unauthorized")

    AuthDep = Annotated[None, Depends(_auth)]

    @app.post("/api/v1/incoming/claim")
    def claim(_: AuthDep, body: ClaimRequest = ClaimRequest()):
        limit = min(body.limit, 10)
        items = _claim_items(limit, body.claim_ttl_sec)
        return {"items": items}

    @app.post("/api/v1/incoming/{item_id}/progress", status_code=204)
    def progress(item_id: int, _: AuthDep, body: ProgressRequest):
        if body.status not in ("copying", "copied"):
            raise HTTPException(status_code=400, detail="status must be copying or copied")
        _update_status(item_id, body.status)
        return Response(status_code=204)

    @app.post("/api/v1/incoming/{item_id}/complete")
    def complete(item_id: int, _: AuthDep):
        info = _get_item_info(item_id)
        if info is None:
            raise HTTPException(status_code=404, detail="not found")
        _update_status(item_id, "completed")
        move_queue.put({
            "item_id": item_id,
            "source_path": info["source_path"],
            "normalized_name": info["normalized_name"],
        })
        job_id = f"ingest-{item_id}"
        return {"id": item_id, "job_id": job_id}

    @app.post("/api/v1/incoming/{item_id}/fail", status_code=204)
    def fail(item_id: int, _: AuthDep, body: FailRequest = FailRequest()):
        _update_status_with_error(item_id, "failed", body.error_message)
        return Response(status_code=204)

    return app


# ── DB helpers (unchanged) ────────────────────────────────────────────────────

def _claim_items(limit: int, ttl_sec: int) -> list:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    WITH candidates AS (
                        SELECT id FROM scanner_incoming_items
                        WHERE status = 'registered'
                        ORDER BY created_at
                        LIMIT %s
                        FOR UPDATE SKIP LOCKED
                    )
                    UPDATE scanner_incoming_items
                    SET status = 'claimed',
                        claimed_at = NOW(),
                        claim_expires_at = NOW() + (%s * interval '1 second'),
                        updated_at = NOW()
                    WHERE id IN (SELECT id FROM candidates)
                    RETURNING id, source_path, source_filename, normalized_name, tmdb_id
                    """,
                    (limit, ttl_sec),
                )
                rows = cur.fetchall()
                return [
                    {
                        "id": r[0],
                        "source_path": r[1],
                        "source_filename": r[2],
                        "normalized_name": r[3],
                        "tmdb_id": r[4],
                        "content_kind": "movie",
                    }
                    for r in rows
                ]
    finally:
        db.put_conn(conn)


def _update_status(item_id: int, status: str) -> None:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_incoming_items SET status=%s, updated_at=NOW() WHERE id=%s",
                    (status, item_id),
                )
    finally:
        db.put_conn(conn)


def _update_status_with_error(item_id: int, status: str, error_msg: str) -> None:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_incoming_items SET status=%s, error_message=%s, updated_at=NOW() WHERE id=%s",
                    (status, error_msg, item_id),
                )
    finally:
        db.put_conn(conn)


def _get_item_info(item_id: int) -> dict | None:
    conn = db.get_conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT source_path, normalized_name FROM scanner_incoming_items WHERE id=%s",
                (item_id,),
            )
            row = cur.fetchone()
            if row is None:
                return None
            return {"source_path": row[0], "normalized_name": row[1]}
    finally:
        db.put_conn(conn)


def run(cfg: Config, move_queue: queue.Queue) -> None:
    """Start Uvicorn HTTP server. Call from a daemon thread.

    uvicorn.run() installs signal handlers via signal.signal(), which is only
    allowed from the main thread. We disable that by monkey-patching
    install_signal_handlers before calling server.run().
    """
    logger.info("scanner API server starting on port %d", cfg.api_port)
    app = create_app(cfg, move_queue)
    config = uvicorn.Config(app, host="0.0.0.0", port=cfg.api_port, log_level="info")
    server = uvicorn.Server(config)
    server.install_signal_handlers = lambda: None  # disabled: not on main thread
    server.run()
```

- [ ] **Step 2: Verify no Flask imports remain in the file**

```bash
grep -n flask scanner/scanner/api/server.py
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add scanner/scanner/api/server.py
git commit -m "feat(scanner): replace Flask with FastAPI in scanner API server"
```

---

## Chunk 2: Tests

### Task 3: Rewrite tests/test_scanner_api.py

FastAPI's `TestClient` (from `fastapi.testclient`) uses a `requests`-compatible interface. The test surface is identical to the Flask version but there are two differences:
- `_make_app` returns `TestClient(app)` not `app.test_client()`
- `resp.get_json()` → `resp.json()`
- Auth header value is passed as `headers={"x-service-token": "secret"}` (header names are lowercased by httpx)

**Files:**
- Modify: `scanner/tests/test_scanner_api.py`

- [ ] **Step 1: Rewrite `scanner/tests/test_scanner_api.py`**

```python
# scanner/tests/test_scanner_api.py
import queue
from unittest.mock import patch

from fastapi.testclient import TestClient


class FakeConfig:
    service_token = "secret"
    api_port = 8080


def _make_client(move_q: queue.Queue) -> TestClient:
    from scanner.api.server import create_app
    cfg = FakeConfig()
    app = create_app(cfg, move_q)
    return TestClient(app)


# ── Auth ──────────────────────────────────────────────────────────────────────

def test_claim_unauthorized():
    client = _make_client(queue.Queue())
    resp = client.post("/api/v1/incoming/claim", json={})
    assert resp.status_code == 401


def test_progress_unauthorized():
    client = _make_client(queue.Queue())
    resp = client.post("/api/v1/incoming/1/progress", json={"status": "copying"})
    assert resp.status_code == 401


# ── Claim ─────────────────────────────────────────────────────────────────────

def test_claim_empty():
    """When no registered items exist, returns empty list."""
    mq = queue.Queue()
    client = _make_client(mq)
    with patch("scanner.api.server._claim_items", return_value=[]) as mock_claim:
        resp = client.post(
            "/api/v1/incoming/claim",
            json={"limit": 1, "claim_ttl_sec": 900},
            headers={"x-service-token": "secret"},
        )
    assert resp.status_code == 200
    assert resp.json() == {"items": []}
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
    client = _make_client(mq)
    with patch("scanner.api.server._claim_items", return_value=[item]):
        resp = client.post(
            "/api/v1/incoming/claim",
            json={"limit": 1, "claim_ttl_sec": 900},
            headers={"x-service-token": "secret"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert len(data["items"]) == 1
    assert data["items"][0]["id"] == 42


# ── Progress ──────────────────────────────────────────────────────────────────

def test_progress_copying():
    mq = queue.Queue()
    client = _make_client(mq)
    with patch("scanner.api.server._update_status") as mock_update:
        resp = client.post(
            "/api/v1/incoming/42/progress",
            json={"status": "copying"},
            headers={"x-service-token": "secret"},
        )
    assert resp.status_code == 204
    mock_update.assert_called_once_with(42, "copying")


def test_progress_invalid_status():
    mq = queue.Queue()
    client = _make_client(mq)
    resp = client.post(
        "/api/v1/incoming/42/progress",
        json={"status": "bad_status"},
        headers={"x-service-token": "secret"},
    )
    assert resp.status_code == 400


# ── Complete ──────────────────────────────────────────────────────────────────

def test_complete_enqueues_move():
    """complete endpoint updates status, puts item on move_queue, returns job_id."""
    mq = queue.Queue()
    client = _make_client(mq)
    item_info = {"source_path": "/incoming/film.mkv", "normalized_name": "film_2024_[12345]"}
    with patch("scanner.api.server._get_item_info", return_value=item_info), \
         patch("scanner.api.server._update_status") as mock_update:
        resp = client.post(
            "/api/v1/incoming/42/complete",
            json={},
            headers={"x-service-token": "secret"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert data["id"] == 42
    assert data["job_id"] == "ingest-42"
    mock_update.assert_called_once_with(42, "completed")
    task = mq.get_nowait()
    assert task["item_id"] == 42
    assert task["source_path"] == "/incoming/film.mkv"


def test_complete_not_found():
    mq = queue.Queue()
    client = _make_client(mq)
    with patch("scanner.api.server._get_item_info", return_value=None):
        resp = client.post(
            "/api/v1/incoming/99/complete",
            json={},
            headers={"x-service-token": "secret"},
        )
    assert resp.status_code == 404


# ── Fail ──────────────────────────────────────────────────────────────────────

def test_fail_updates_status():
    mq = queue.Queue()
    client = _make_client(mq)
    with patch("scanner.api.server._update_status_with_error") as mock_fail:
        resp = client.post(
            "/api/v1/incoming/42/fail",
            json={"error_message": "rclone timeout"},
            headers={"x-service-token": "secret"},
        )
    assert resp.status_code == 204
    mock_fail.assert_called_once_with(42, "failed", "rclone timeout")
```

- [ ] **Step 2: Install dev deps and run tests**

```bash
cd scanner && pip install -e ".[dev]" -q && pytest tests/test_scanner_api.py -v
```

Expected: 9 tests passing, 0 failures.

- [ ] **Step 3: Run the full test suite to confirm no regressions**

```bash
pytest -v
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add scanner/tests/test_scanner_api.py
git commit -m "test(scanner): rewrite API tests for FastAPI TestClient"
```

---

## Chunk 3: Docs and server deployment

### Task 4: Update CHANGELOG and redeploy on server

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add entry under `## [Unreleased]` in `CHANGELOG.md`**

```markdown
### Changed
- `scanner/scanner/api/server.py`: replaced Flask with FastAPI + Uvicorn; identical HTTP contracts preserved
- `scanner/pyproject.toml`: replaced `flask>=3.0` with `fastapi>=0.110`, `uvicorn[standard]>=0.29`; added `httpx>=0.27` to dev deps
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: log Flask→FastAPI migration in CHANGELOG"
```

- [ ] **Step 3: Push to remote**

```bash
git push origin main
```

- [ ] **Step 4: Pull and rebuild on server**

The repo root on the server is `/opt/converter`; the scanner's `docker-compose.yml` is in `/opt/converter/scanner`.

```bash
ssh -i ~/.ssh/id_rsa_personal root@213.111.156.183 \
  "cd /opt/converter && git pull && cd scanner && docker compose build scanner && docker compose up -d scanner"
```

Expected: build succeeds, scanner container restarts, `docker logs scanner-scanner-1` shows `scanner API server starting on port 8080`.

- [ ] **Step 5: Smoke-test the API from the server**

```bash
ssh -i ~/.ssh/id_rsa_personal root@213.111.156.183 \
  'TOKEN=$(awk -F= "/^SERVICE_TOKEN/{print \$2}" /opt/converter/scanner/.env) && \
   curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/api/v1/incoming/claim \
     -H "Content-Type: application/json" \
     -H "X-Service-Token: $TOKEN" \
     -d "{\"limit\":1}"'
```

Expected output: `200`
