# Ingest Architecture Inversion Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Invert the ingest architecture so the Scanner (Python) is an HTTP server and the IngestWorker (Go) polls it — the scanner has zero knowledge of the converter.

**Architecture:** The Scanner exposes a Flask HTTP API (`/api/v1/incoming/*`) with claim/progress/complete/fail endpoints; the IngestWorker polls the scanner instead of the converter API. After rclone copy, the IngestWorker creates the `media_job` and pushes to `convert_queue` locally (previously done by the converter's Complete endpoint). All ingest-related code is removed from the converter API service.

**Tech Stack:** Python Flask (scanner HTTP server), Go net/http (worker client), psycopg2 FOR UPDATE SKIP LOCKED (atomic claim), Redis RPUSH (convert_queue)

---

## Chunk 1: Scanner Python — become a server

### Task 1: Update scanner config + env

**Files:**
- Modify: `scanner/scanner/config.py`
- Modify: `scanner/.env.example`

**Context:**
- Remove `converter_api_url` and `converter_service_token` (scanner no longer calls converter)
- Add `api_port` (HTTP server port, default 8080) and `service_token` (to authenticate incoming requests from IngestWorker)
- The `service_token` that IngestWorker sends must match this value

- [ ] **Step 1: Replace config.py**

```python
# scanner/scanner/config.py
import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    incoming_dir: str
    library_dir: str
    tmdb_api_key: str
    database_url: str
    service_token: str
    api_port: int
    scan_interval_sec: int
    stability_sec: int


def load() -> Config:
    return Config(
        incoming_dir=_require("INCOMING_DIR"),
        library_dir=_require("LIBRARY_DIR"),
        tmdb_api_key=_require("TMDB_API_KEY"),
        database_url=_require("DATABASE_URL"),
        service_token=_require("SERVICE_TOKEN"),
        api_port=int(os.environ.get("SCANNER_API_PORT", "8080")),
        scan_interval_sec=int(os.environ.get("SCAN_INTERVAL_SEC", "30")),
        stability_sec=int(os.environ.get("STABILITY_SEC", "30")),
    )


def _require(key: str) -> str:
    val = os.environ.get(key)
    if not val:
        raise RuntimeError(f"Required env var {key!r} is not set")
    return val
```

- [ ] **Step 2: Replace .env.example**

```ini
# scanner/.env.example

# Paths (bind-mounted into container)
INCOMING_DIR=/mnt/storage/incoming
LIBRARY_DIR=/mnt/storage/library

# Scanner HTTP API
SCANNER_API_PORT=8080
SERVICE_TOKEN=changeme

# TMDB (https://www.themoviedb.org/settings/api)
TMDB_API_KEY=changeme

# Database
DATABASE_URL=postgresql://scanner:scanner@postgres:5432/scanner

# Tuning (seconds)
SCAN_INTERVAL_SEC=30
STABILITY_SEC=30
```

- [ ] **Step 3: Verify tests still pass (config no longer uses removed fields)**

```bash
cd scanner/
PYTHONPATH=. python3 -m pytest tests/ -v 2>&1 | head -40
```

Expected: some tests fail (scan_loop tests that pass client) — that's OK, those are fixed in Task 3. Config-only tests (test_stability, test_quality, test_metadata, test_duplicates) should pass.

- [ ] **Step 4: Commit**

```bash
git add scanner/scanner/config.py scanner/.env.example
git commit -m "refactor(scanner): remove converter client config, add HTTP server config"
```

---

### Task 2: Add scanner DB migration for claim columns

**Files:**
- Create: `scanner/scanner/migrations/002_add_claim_columns.sql`

**Context:**
- The scanner DB needs `claimed_at` and `claim_expires_at` on `scanner_incoming_items` so the HTTP API can implement TTL-based claiming (FOR UPDATE SKIP LOCKED pattern)
- Migration is applied automatically by `db.init()` on startup

- [ ] **Step 1: Create migration**

```sql
-- scanner/scanner/migrations/002_add_claim_columns.sql
-- Add claim tracking columns for the scanner HTTP API.

ALTER TABLE scanner_incoming_items
    ADD COLUMN IF NOT EXISTS claimed_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_incoming_claim_expires
    ON scanner_incoming_items (claim_expires_at)
    WHERE status = 'claimed';
```

- [ ] **Step 2: Verify migration runner picks it up (by inspection)**

Open `scanner/scanner/db.py` and confirm `_run_migrations()` iterates all `.sql` files in the `migrations/` directory in sorted order. The new file `002_add_claim_columns.sql` will be picked up automatically.

- [ ] **Step 3: Commit**

```bash
git add scanner/scanner/migrations/002_add_claim_columns.sql
git commit -m "feat(scanner): add claim_at/claim_expires_at columns to scanner_incoming_items"
```

---

### Task 3: Update scan_loop — remove converter client dependency

**Files:**
- Modify: `scanner/scanner/loops/scan_loop.py`

**Context:**
- Current: `run(cfg, client)` calls `client.register()` to talk to converter API, stores `api_item_id`
- New: `run(cfg)` — no external API call. When a file is stable and passes duplicate check, directly set `status='registered'` in DB. `api_item_id` stays NULL (column remains for schema compatibility).
- Remove `client` parameter from `run`, `_scan_once`, `_process_file`, `_handle_stable_file`, `_do_register`

- [ ] **Step 1: Write the updated scan_loop.py**

```python
# scanner/scanner/loops/scan_loop.py
import logging
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from scanner import db
from scanner.config import Config
from scanner.services import duplicates, metadata, quality, stability

logger = logging.getLogger(__name__)

VIDEO_EXTENSIONS = {".mkv", ".mp4", ".avi", ".mov", ".ts", ".m2ts", ".wmv"}


def run(cfg: Config) -> None:
    """Run scan loop forever. Call from a daemon thread."""
    logger.info("scan_loop started, interval=%ds", cfg.scan_interval_sec)
    while True:
        try:
            _scan_once(cfg)
        except Exception:
            logger.exception("scan_loop iteration failed")
        time.sleep(cfg.scan_interval_sec)


def _scan_once(cfg: Config) -> None:
    now = datetime.now(timezone.utc)
    for file_path in _walk_video_files(Path(cfg.incoming_dir)):
        try:
            _process_file(cfg, file_path, now)
        except Exception:
            logger.exception("error processing file %s", file_path)


def _walk_video_files(root: Path):
    for dirpath, _, filenames in os.walk(root):
        for fname in filenames:
            if Path(fname).suffix.lower() in VIDEO_EXTENSIONS:
                yield Path(dirpath) / fname


def _process_file(cfg: Config, file_path: Path, now: datetime) -> None:
    try:
        current_size = file_path.stat().st_size
    except OSError:
        return  # file disappeared

    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT id, status, file_size_bytes, stable_since FROM scanner_incoming_items WHERE source_path = %s",
                    (str(file_path),),
                )
                row = cur.fetchone()

                if row is None:
                    cur.execute(
                        "INSERT INTO scanner_incoming_items (source_path, source_filename, file_size_bytes, status) VALUES (%s, %s, %s, 'new')",
                        (str(file_path), file_path.name, current_size),
                    )
                    return

                item_id, status, last_size, stable_since = row
                if status != "new":
                    return

                upd = stability.update_stability(
                    current_size=current_size,
                    last_seen_size=last_size,
                    stable_since=stable_since,
                    now=now,
                )
                cur.execute(
                    "UPDATE scanner_incoming_items SET file_size_bytes=%s, stable_since=%s, last_seen_at=%s, updated_at=NOW() WHERE id=%s",
                    (upd["file_size_bytes"], upd["stable_since"], now, item_id),
                )

                if not stability.is_stable(
                    current_size=current_size,
                    last_seen_size=last_size,
                    stable_since=upd["stable_since"],
                    now=now,
                    stability_sec=cfg.stability_sec,
                ):
                    return
    finally:
        db.put_conn(conn)

    _handle_stable_file(cfg, file_path, current_size)


def _handle_stable_file(cfg: Config, file_path: Path, file_size: int) -> None:
    parsed = metadata.parse_filename(file_path.name)
    title = parsed["title"]
    year = parsed.get("year")

    tmdb_result = metadata.tmdb_search(title, year, cfg.tmdb_api_key)
    tmdb_id = tmdb_result["tmdb_id"] if tmdb_result else None
    canonical_title = tmdb_result["title"] if tmdb_result else title

    normalized_name = metadata.build_normalized_name(canonical_title, year, tmdb_id)

    quality_result = quality.ffprobe_quality(str(file_path))
    quality_score = quality_result["quality_score"] if quality_result else None
    ffprobe_ok = quality_result is not None

    existing_score = _get_existing_score(normalized_name, tmdb_id)
    action = duplicates.decide_action(
        existing_score=existing_score,
        new_score=quality_score,
        ffprobe_ok=ffprobe_ok,
    )

    if action == "register":
        _do_register(
            file_path=file_path,
            normalized_name=normalized_name,
            tmdb_id=tmdb_id,
            file_size=file_size,
            quality_score=quality_score,
            is_upgrade_candidate=(existing_score is not None),
        )
    else:
        import datetime as _dt
        ts = _dt.datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S")
        prefix = "REVIEW_DUPLICATE" if action == "review_duplicate" else "REVIEW_UNKNOWN"
        new_name = f"{prefix}_{normalized_name}_{ts}{file_path.suffix}"
        new_path = file_path.parent / new_name
        try:
            file_path.rename(new_path)
        except OSError as e:
            logger.error("rename failed for %s: %s", file_path, e)
            return
        _update_status(str(file_path), action, review_reason=action.removeprefix("review_"))


def _get_existing_score(normalized_name: str, tmdb_id: Optional[str]) -> Optional[int]:
    conn = db.get_conn()
    try:
        with conn.cursor() as cur:
            if tmdb_id:
                cur.execute("SELECT quality_score FROM scanner_library_movies WHERE tmdb_id = %s LIMIT 1", (tmdb_id,))
            else:
                cur.execute("SELECT quality_score FROM scanner_library_movies WHERE normalized_name = %s LIMIT 1", (normalized_name,))
            row = cur.fetchone()
            return row[0] if row else None
    finally:
        db.put_conn(conn)


def _do_register(file_path, normalized_name, tmdb_id, file_size, quality_score, is_upgrade_candidate):
    """Mark file as registered in scanner DB — ready to be claimed by IngestWorker."""
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_incoming_items SET status='registered', normalized_name=%s, tmdb_id=%s, quality_score=%s, is_upgrade_candidate=%s, updated_at=NOW() WHERE source_path=%s AND status='new'",
                    (normalized_name, tmdb_id, quality_score, is_upgrade_candidate, str(file_path)),
                )
    finally:
        db.put_conn(conn)


def _update_status(source_path: str, status: str, review_reason: Optional[str] = None) -> None:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_incoming_items SET status=%s, review_reason=%s, updated_at=NOW() WHERE source_path=%s",
                    (status, review_reason, source_path),
                )
    finally:
        db.put_conn(conn)
```

- [ ] **Step 2: Verify tests pass**

```bash
cd scanner/
PYTHONPATH=. python3 -m pytest tests/test_stability.py tests/test_quality.py tests/test_metadata.py tests/test_duplicates.py -v
```

Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add scanner/scanner/loops/scan_loop.py
git commit -m "refactor(scanner): scan_loop registers items directly to DB, removes converter client dependency"
```

---

### Task 4: Add scanner HTTP API server (Flask)

**Files:**
- Create: `scanner/scanner/api/server.py`
- Modify: `scanner/pyproject.toml` (add flask dependency)

**Context:**
- This replaces `poll_loop.py` as the mechanism through which the IngestWorker gets updates
- Runs as a Flask server in a daemon thread
- Endpoints:
  - `POST /api/v1/incoming/claim` — atomically claims N items (status: `registered` → `claimed`) using `FOR UPDATE SKIP LOCKED`
  - `POST /api/v1/incoming/<id>/progress` — updates status (`copying` or `copied`)
  - `POST /api/v1/incoming/<id>/complete` — marks `completed`, puts item on `move_queue`
  - `POST /api/v1/incoming/<id>/fail` — marks `failed`
- Auth: `X-Service-Token` header must match `cfg.service_token`

- [ ] **Step 1: Write the failing test first**

Create `scanner/tests/test_scanner_api.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd scanner/
PYTHONPATH=. python3 -m pytest tests/test_scanner_api.py -v 2>&1 | head -20
```

Expected: ImportError or ModuleNotFoundError — `scanner.api.server` does not exist yet

- [ ] **Step 3: Add flask to pyproject.toml**

```toml
# scanner/pyproject.toml
[project]
name = "scanner"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "psycopg2-binary>=2.9",
    "guessit>=3.8",
    "requests>=2.31",
    "flask>=3.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0",
    "pytest-mock>=3.12",
]
```

- [ ] **Step 4: Install flask**

```bash
pip3 install flask>=3.0
```

Expected: Successfully installed flask and its dependencies

- [ ] **Step 5: Write scanner/scanner/api/server.py**

```python
# scanner/scanner/api/server.py
import logging
import queue

from flask import Flask, request, jsonify

from scanner import db
from scanner.config import Config

logger = logging.getLogger(__name__)


def create_app(cfg: Config, move_queue: queue.Queue) -> Flask:
    app = Flask(__name__)

    def _check_token():
        token = request.headers.get("X-Service-Token", "")
        if token != cfg.service_token:
            return jsonify({"error": "unauthorized"}), 401
        return None

    @app.post("/api/v1/incoming/claim")
    def claim():
        err = _check_token()
        if err:
            return err
        data = request.get_json(silent=True) or {}
        limit = min(int(data.get("limit", 1)), 10)
        ttl_sec = int(data.get("claim_ttl_sec", 900))
        items = _claim_items(limit, ttl_sec)
        return jsonify({"items": items})

    @app.post("/api/v1/incoming/<int:item_id>/progress")
    def progress(item_id):
        err = _check_token()
        if err:
            return err
        data = request.get_json(silent=True) or {}
        status = data.get("status")
        if status not in ("copying", "copied"):
            return jsonify({"error": "status must be copying or copied"}), 400
        _update_status(item_id, status)
        return "", 204

    @app.post("/api/v1/incoming/<int:item_id>/complete")
    def complete(item_id):
        err = _check_token()
        if err:
            return err
        info = _get_item_info(item_id)
        if info is None:
            return jsonify({"error": "not found"}), 404
        _update_status(item_id, "completed")
        move_queue.put({
            "item_id": item_id,
            "source_path": info["source_path"],
            "normalized_name": info["normalized_name"],
        })
        job_id = f"ingest-{item_id}"
        return jsonify({"id": item_id, "job_id": job_id})

    @app.post("/api/v1/incoming/<int:item_id>/fail")
    def fail(item_id):
        err = _check_token()
        if err:
            return err
        data = request.get_json(silent=True) or {}
        error_msg = data.get("error_message", "")
        _update_status_with_error(item_id, "failed", error_msg)
        return "", 204

    return app


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
    """Start Flask HTTP server. Call from a daemon thread."""
    logger.info("scanner API server starting on port %d", cfg.api_port)
    app = create_app(cfg, move_queue)
    app.run(host="0.0.0.0", port=cfg.api_port, threaded=True, use_reloader=False)
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd scanner/
PYTHONPATH=. python3 -m pytest tests/test_scanner_api.py -v
```

Expected: all 9 tests pass

- [ ] **Step 7: Commit**

```bash
git add scanner/scanner/api/server.py scanner/tests/test_scanner_api.py scanner/pyproject.toml
git commit -m "feat(scanner): add Flask HTTP API server with claim/progress/complete/fail endpoints"
```

---

### Task 5: Remove poll_loop and converter_client; update main.py

**Files:**
- Delete: `scanner/scanner/loops/poll_loop.py`
- Delete: `scanner/scanner/api/converter_client.py`
- Delete: `scanner/tests/test_converter_client.py`
- Modify: `scanner/scanner/main.py`
- Modify: `scanner/scanner/docker-compose.yml`

**Context:**
- `poll_loop` polled the converter API to update item statuses — replaced by the HTTP server's endpoints
- `converter_client` was the HTTP client to converter — scanner no longer needs it
- `main.py` replaces the `poll_loop` thread with a `api_server` thread; removes ConverterClient instantiation
- `docker-compose.yml` must expose the scanner API port

- [ ] **Step 1: Delete removed files**

```bash
rm scanner/scanner/loops/poll_loop.py
rm scanner/scanner/api/converter_client.py
rm scanner/tests/test_converter_client.py
```

- [ ] **Step 2: Write the new main.py**

```python
# scanner/scanner/main.py
import logging
import queue
import signal
import sys
import threading

from scanner import db
from scanner.api import server as api_server
from scanner.config import load as load_config
from scanner.loops import move_worker, scan_loop

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)


def main() -> None:
    cfg = load_config()
    db.init(cfg.database_url)
    mq: queue.Queue = queue.Queue()

    threads = [
        threading.Thread(target=scan_loop.run, args=(cfg,), name="scan_loop", daemon=True),
        threading.Thread(target=api_server.run, args=(cfg, mq), name="api_server", daemon=True),
        threading.Thread(target=move_worker.run, args=(cfg, mq), name="move_worker", daemon=True),
    ]

    for t in threads:
        t.start()
        logger.info("started thread %s", t.name)

    stop = threading.Event()

    def _handle_signal(signum, frame):  # noqa: ANN001
        logger.info("received signal %d, shutting down", signum)
        stop.set()

    signal.signal(signal.SIGINT, _handle_signal)
    signal.signal(signal.SIGTERM, _handle_signal)

    stop.wait()
    logger.info("scanner stopped")
    sys.exit(0)


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Update docker-compose.yml to expose scanner API port**

Open `scanner/docker-compose.yml` and add a `ports` section to the `scanner` service:

```yaml
    ports:
      - "${SCANNER_API_PORT:-8080}:8080"
```

Place it after the `environment` section and before `volumes`.

- [ ] **Step 4: Run all scanner tests**

```bash
cd scanner/
PYTHONPATH=. python3 -m pytest tests/ -v
```

Expected: 40+ tests pass (test_converter_client.py is deleted, test_scanner_api.py has 9 tests)

- [ ] **Step 5: Commit**

```bash
git add scanner/scanner/main.py scanner/scanner/docker-compose.yml
git rm scanner/scanner/loops/poll_loop.py scanner/scanner/api/converter_client.py scanner/tests/test_converter_client.py
git commit -m "refactor(scanner): replace poll_loop with HTTP API server, remove converter client"
```

---

## Chunk 2: Converter API — remove all ingest code

### Task 6: Remove ingest handler, service, repository, models and routes

**Files:**
- Delete: `api/internal/handler/ingest.go`
- Delete: `api/internal/service/ingest.go`
- Delete: `api/internal/repository/incoming.go`
- Delete: `api/internal/model/incoming.go`
- Modify: `api/internal/server/server.go` — remove `IngestHandler` from Dependencies and route block
- Modify: `api/cmd/api/main.go` — remove ingest wiring
- Modify: `api/internal/config/config.go` — remove `IngestServiceToken` and `IngestMaxAttempts`

**Context:**
- The converter API no longer owns ingest state. The scanner owns it.
- The `ingest_incoming_items` table in converter DB will be dropped in Task 7.
- Removing `IngestHandler` from Dependencies requires removing the field and the route block in `server.go`, and removing the wiring in `main.go`.

- [ ] **Step 1: Delete ingest files**

```bash
rm api/internal/handler/ingest.go
rm api/internal/service/ingest.go
rm api/internal/repository/incoming.go
rm api/internal/model/incoming.go
```

- [ ] **Step 2: Update api/internal/server/server.go — remove IngestHandler**

Remove from `Dependencies` struct:
```go
// DELETE this line:
IngestHandler   *handler.IngestHandler
```

Remove from `New()` function the entire ingest route block:
```go
// DELETE this entire block:
// ── Ingest API (service-to-service) ──────────────────────────────────────
r.Route("/api/ingest", func(r chi.Router) {
    r.Use(auth.ServiceTokenMiddleware(deps.Cfg.IngestServiceToken))
    r.Post("/incoming/register", deps.IngestHandler.Register)
    r.Post("/incoming/claim",    deps.IngestHandler.Claim)
    r.Post("/incoming/progress", deps.IngestHandler.Progress)
    r.Post("/incoming/fail",     deps.IngestHandler.Fail)
    r.Post("/incoming/complete", deps.IngestHandler.Complete)
    r.Get("/incoming/{id}",      deps.IngestHandler.GetByID)
})
```

- [ ] **Step 3: Update api/cmd/api/main.go — remove ingest wiring**

Remove these 3 lines:
```go
incomingRepo := repository.NewIncomingRepository(pool)
ingestSvc    := service.NewIngestService(incomingRepo, jobRepo, redisClient, cfg.MediaRoot)
ingestH      := handler.NewIngestHandler(ingestSvc, cfg.IngestMaxAttempts)
```

Remove `IngestHandler: ingestH,` from the `server.New(server.Dependencies{...})` call.

- [ ] **Step 4: Update api/internal/config/config.go — remove ingest fields**

Remove `IngestServiceToken` and `IngestMaxAttempts` from the Config struct and from the `Load()` function.

The current lines to remove from the struct:
```go
// Ingest
IngestServiceToken string
IngestMaxAttempts  int
```

And from Load():
```go
IngestServiceToken:  getEnv("INGEST_SERVICE_TOKEN", ""),
IngestMaxAttempts:   intEnv("INGEST_MAX_ATTEMPTS", 3),
```

- [ ] **Step 5: Build to verify no compile errors**

```bash
docker compose build api 2>&1 | tail -20
```

Expected: build succeeds (no references to deleted types)

- [ ] **Step 6: Commit**

```bash
git rm api/internal/handler/ingest.go api/internal/service/ingest.go api/internal/repository/incoming.go api/internal/model/incoming.go
git add api/internal/server/server.go api/cmd/api/main.go api/internal/config/config.go
git commit -m "feat(api): remove ingest API endpoints — scanner now owns ingest state"
```

---

### Task 7: Add migration to drop incoming_media_items from converter DB

**Files:**
- Create: `api/internal/db/migrations/013_drop_incoming_media_items.sql`

**Context:**
- Migration 012 created `incoming_media_items` in the converter's PostgreSQL DB
- This table is no longer used; the scanner has its own `scanner_incoming_items` table
- Must use `DROP TABLE IF EXISTS` (idempotent)

- [ ] **Step 1: Create migration**

```sql
-- api/internal/db/migrations/013_drop_incoming_media_items.sql
-- Remove the ingest queue table from the converter DB.
-- The scanner service now owns its own scanner_incoming_items table.

DROP INDEX IF EXISTS incoming_media_items_claim_expires_idx;
DROP INDEX IF EXISTS incoming_media_items_status_idx;
DROP INDEX IF EXISTS incoming_media_items_source_path_key;
DROP TABLE IF EXISTS incoming_media_items;
```

- [ ] **Step 2: Verify migration file is in place**

```bash
ls api/internal/db/migrations/ | sort
```

Expected: `013_drop_incoming_media_items.sql` appears last

- [ ] **Step 3: Rebuild API to pick up migration**

```bash
docker compose build api 2>&1 | tail -5
```

Expected: build succeeds

- [ ] **Step 4: Commit**

```bash
git add api/internal/db/migrations/013_drop_incoming_media_items.sql
git commit -m "feat(db): drop incoming_media_items table — ingest state moved to scanner service"
```

---

## Chunk 3: IngestWorker Go — redirect to Scanner API

### Task 8: Rewrite worker/internal/ingest/client.go for scanner API

**Files:**
- Modify: `worker/internal/ingest/client.go`

**Context:**
- Old: called `/api/ingest/incoming/{claim,progress,fail,complete}` on the converter API
- New: calls `/api/v1/incoming/{claim,<id>/progress,<id>/fail,<id>/complete}` on the scanner API
- `Complete` now takes only `(ctx, id)` — no `localPath` — and returns `error` (not `(string, error)`)
  because the worker creates the job locally and doesn't need a job_id back from scanner
- `Progress` and `Fail` use path params (not body IDs)

- [ ] **Step 1: Write the new client.go**

```go
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// IncomingItem — fields the worker needs from the scanner API response.
type IncomingItem struct {
	ID             int64   `json:"id"`
	SourcePath     string  `json:"source_path"`
	SourceFilename string  `json:"source_filename"`
	ContentKind    string  `json:"content_kind"`
	NormalizedName *string `json:"normalized_name,omitempty"`
	TMDBID         *string `json:"tmdb_id,omitempty"`
}

// Client is an HTTP client for the scanner ingest API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates a Client that talks to the scanner API at baseURL.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Claim claims up to 1 item from the scanner API.
func (c *Client) Claim(ctx context.Context, claimTTLSec int) ([]IncomingItem, error) {
	body, _ := json.Marshal(map[string]int{"limit": 1, "claim_ttl_sec": claimTTLSec})
	var resp struct {
		Items []IncomingItem `json:"items"`
	}
	if err := c.post(ctx, "/api/v1/incoming/claim", body, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// Progress reports copying progress for an item.
func (c *Client) Progress(ctx context.Context, id int64, status string) error {
	body, _ := json.Marshal(map[string]string{"status": status})
	return c.post(ctx, fmt.Sprintf("/api/v1/incoming/%d/progress", id), body, nil)
}

// Fail reports a failure for an item.
func (c *Client) Fail(ctx context.Context, id int64, msg string) error {
	body, _ := json.Marshal(map[string]string{"error_message": msg})
	return c.post(ctx, fmt.Sprintf("/api/v1/incoming/%d/fail", id), body, nil)
}

// Complete marks an item as completed in the scanner.
func (c *Client) Complete(ctx context.Context, id int64) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/incoming/%d/complete", id), []byte("{}"), nil)
}

func (c *Client) post(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: status %d", path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
```

- [ ] **Step 2: Build worker to verify client.go compiles**

```bash
docker compose build worker 2>&1 | grep -E "error|Error|DONE|Step" | head -20
```

Expected: compile errors in worker.go because `Complete` signature changed — that's OK, fixed in Task 9

- [ ] **Step 3: Commit**

```bash
git add worker/internal/ingest/client.go
git commit -m "refactor(worker): rewrite ingest client to call scanner API"
```

---

### Task 9: Add CreateForIngest to worker job repository

**Files:**
- Modify: `worker/internal/repository/job.go`

**Context:**
- The converter API's `Complete` endpoint used to create the `media_job` and push to `convert_queue`
- Now the IngestWorker does this directly
- `CreateForIngest` inserts into `media_jobs` with `ON CONFLICT (request_id) DO NOTHING` for idempotency
- `jobID` is also used as `request_id` (e.g. `"ingest-42"`)

- [ ] **Step 1: Add CreateForIngest method at the end of job.go**

Append to `worker/internal/repository/job.go`:

```go
// CreateForIngest inserts a media_job for an ingest item.
// Uses ON CONFLICT (request_id) DO NOTHING for idempotency — safe to call twice.
func (r *JobRepository) CreateForIngest(ctx context.Context, jobID, sourcePath, title, contentKind string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO media_jobs
			(job_id, content_type, source_type, source_ref, priority, status, request_id, title)
		VALUES ($1, $2, 'ingest', $3, 'normal', 'queued', $4, $5)
		ON CONFLICT (request_id) DO NOTHING`,
		jobID, contentKind, sourcePath, jobID, title)
	if err != nil {
		return fmt.Errorf("create ingest job %s: %w", jobID, err)
	}
	return nil
}
```

- [ ] **Step 2: Build to verify it compiles**

```bash
docker compose build worker 2>&1 | grep -E "^#|error|Error" | head -20
```

Expected: compile error still in worker.go (Complete call) — OK, fixed next

- [ ] **Step 3: Commit**

```bash
git add worker/internal/repository/job.go
git commit -m "feat(worker): add CreateForIngest to job repository"
```

---

### Task 10: Update worker.go — create job locally, call scanner complete

**Files:**
- Modify: `worker/internal/ingest/worker.go`

**Context:**
- `New()` now accepts `jobRepo *repository.JobRepository` and `queueClient *queue.Client`
- After rclone copy, the worker:
  1. Calls `jobRepo.CreateForIngest` to insert `media_job` (idempotent)
  2. Pushes `model.ConvertMessage` to `queue.ConvertQueue`
  3. Calls `client.Complete(ctx, item.ID)` to notify scanner (triggers move)
- `client.Complete` now returns `error` only (not `(string, error)`)
- On queue push failure: call `client.Fail` (job insert may have succeeded — this is acceptable; retrying Complete will hit ON CONFLICT DO NOTHING)

- [ ] **Step 1: Write the new worker.go**

```go
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

// Worker polls the scanner API for claimed ingest items, copies them via rclone,
// creates the convert job locally, then notifies the scanner.
type Worker struct {
	client       *Client
	puller       *Puller
	jobRepo      *repository.JobRepository
	queueClient  *queue.Client
	mediaRoot    string
	claimTTLSec  int
	pollInterval time.Duration
}

// New creates an IngestWorker.
func New(
	client *Client,
	puller *Puller,
	jobRepo *repository.JobRepository,
	queueClient *queue.Client,
	mediaRoot string,
	claimTTLSec int,
) *Worker {
	return &Worker{
		client:       client,
		puller:       puller,
		jobRepo:      jobRepo,
		queueClient:  queueClient,
		mediaRoot:    mediaRoot,
		claimTTLSec:  claimTTLSec,
		pollInterval: 10 * time.Second,
	}
}

// Run polls for items until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("ingest worker goroutine started")
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items, err := w.client.Claim(ctx, w.claimTTLSec)
			if err != nil {
				slog.Warn("ingest claim failed", "error", err)
				continue
			}
			if len(items) == 0 {
				continue
			}
			w.processItem(ctx, items[0])
		}
	}
}

func (w *Worker) processItem(ctx context.Context, item IncomingItem) {
	log := slog.With("ingest_id", item.ID, "source", item.SourcePath)

	destDir := filepath.Join(w.mediaRoot, "downloads", "ingest_"+strconv.FormatInt(item.ID, 10))

	if err := w.client.Progress(ctx, item.ID, "copying"); err != nil {
		log.Warn("progress update failed", "error", err)
	}

	localPath, err := w.puller.Copy(ctx, item.SourcePath, destDir)
	if err != nil {
		log.Error("rclone copy failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	if err := w.client.Progress(ctx, item.ID, "copied"); err != nil {
		log.Warn("progress update failed", "error", err)
	}

	// Create media_job in converter DB (previously done by converter API's Complete endpoint).
	jobID := fmt.Sprintf("ingest-%d", item.ID)
	contentKind := item.ContentKind
	if contentKind == "" {
		contentKind = "movie"
	}
	title := ""
	if item.NormalizedName != nil {
		title = *item.NormalizedName
	}

	if err := w.jobRepo.CreateForIngest(ctx, jobID, item.SourcePath, title, contentKind); err != nil {
		log.Error("create ingest job failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	// Build and push convert_queue message.
	finalDir := fmt.Sprintf("ingest_%d", item.ID)
	if item.NormalizedName != nil {
		finalDir = *item.NormalizedName
	}
	tmdbID := ""
	if item.TMDBID != nil {
		tmdbID = *item.TMDBID
	}
	outputPath := filepath.Join(w.mediaRoot, "converted", "movies")

	msg := model.ConvertMessage{
		SchemaVersion: "1",
		JobID:         jobID,
		JobType:       "convert",
		ContentType:   contentKind,
		CorrelationID: jobID,
		Attempt:       1,
		MaxAttempts:   3,
		CreatedAt:     time.Now(),
		Payload: model.ConvertJob{
			InputPath:     localPath,
			OutputPath:    outputPath,
			OutputProfile: "mp4_h264_aac_1080p",
			FinalDir:      finalDir,
			TMDBID:        tmdbID,
			Title:         title,
		},
	}

	if err := w.queueClient.Push(ctx, queue.ConvertQueue, msg); err != nil {
		log.Error("enqueue convert job failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	// Notify scanner that processing is complete — triggers move to library.
	if err := w.client.Complete(ctx, item.ID); err != nil {
		// Job is already enqueued — log but don't fail. Scanner will eventually
		// expire the claim and show the item as stuck.
		log.Warn("scanner complete notification failed (job already queued)", "error", err, "job_id", jobID)
		return
	}

	log.Info("ingest item processed", "job_id", jobID)
}
```

- [ ] **Step 2: Build to verify worker.go compiles**

```bash
docker compose build worker 2>&1 | grep -E "^#|error|Error" | head -20
```

Expected: compile error in `main.go` — `ingest.New` call now has wrong signature — fixed in Task 11

- [ ] **Step 3: Commit**

```bash
git add worker/internal/ingest/worker.go
git commit -m "refactor(worker): ingest worker creates convert job locally, notifies scanner on complete"
```

---

### Task 11: Update worker config + main.go

**Files:**
- Modify: `worker/internal/config/config.go`
- Modify: `worker/cmd/worker/main.go`

**Context:**
- Rename `ConverterAPIURL` → `ScannerAPIURL` in config (env var: `CONVERTER_API_URL` → `SCANNER_API_URL`)
- Update `main.go` to pass `jobRepo` and `redisClient` to `ingest.New()`

- [ ] **Step 1: Update config.go**

In `worker/internal/config/config.go`:

Replace in struct:
```go
// OLD:
ConverterAPIURL      string
// NEW:
ScannerAPIURL        string
```

Replace in Load():
```go
// OLD:
ConverterAPIURL:      getEnv("CONVERTER_API_URL", "http://api:8000"),
// NEW:
ScannerAPIURL:        getEnv("SCANNER_API_URL", "http://scanner:8080"),
```

- [ ] **Step 2: Update main.go ingest wiring**

In `worker/cmd/worker/main.go`, find:

```go
if cfg.IngestServiceToken != "" && cfg.IngestSourceRemote != "" {
    ingestClient := ingest.NewClient(cfg.ConverterAPIURL, cfg.IngestServiceToken)
    ingestPuller := ingest.NewPuller(cfg.IngestSourceRemote, cfg.IngestSourceBasePath)
    ingestWkr := ingest.New(ingestClient, ingestPuller, cfg.MediaRoot, cfg.IngestClaimTTLSec)
```

Replace with:

```go
if cfg.IngestServiceToken != "" && cfg.IngestSourceRemote != "" {
    ingestClient := ingest.NewClient(cfg.ScannerAPIURL, cfg.IngestServiceToken)
    ingestPuller := ingest.NewPuller(cfg.IngestSourceRemote, cfg.IngestSourceBasePath)
    ingestWkr := ingest.New(ingestClient, ingestPuller, jobRepo, redisClient, cfg.MediaRoot, cfg.IngestClaimTTLSec)
```

- [ ] **Step 3: Build worker — must succeed with no errors**

```bash
docker compose build worker 2>&1 | tail -10
```

Expected: build succeeds

- [ ] **Step 4: Build API — must also succeed**

```bash
docker compose build api 2>&1 | tail -10
```

Expected: build succeeds

- [ ] **Step 5: Commit**

```bash
git add worker/internal/config/config.go worker/cmd/worker/main.go
git commit -m "refactor(worker): point ingest worker at scanner API (SCANNER_API_URL), wire jobRepo + redisClient"
```

---

## Chunk 4: Documentation + housekeeping

### Task 12: Update docs, CHANGELOG, contracts, ADR

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/contracts/api.md` — remove ingest endpoints section
- Modify: `docs/architecture/modules/scanner.md` — update to reflect new HTTP server architecture
- Modify: `docs/architecture/modules/worker.md` — update IngestWorker description
- Create: `docs/decisions/ADR-009-scanner-as-ingest-api-server.md`
- Modify: `docs/decisions/README.md`

**Context:**
- ADR documents the reversal of the ingest contract: scanner becomes server, worker becomes client
- This is a significant architectural decision that is hard to reverse

- [ ] **Step 1: Add CHANGELOG entries**

Under `## [Unreleased]` in `CHANGELOG.md`:

```markdown
### Changed
- `scanner/scanner/api/server.py`: scanner now exposes Flask HTTP API (`/api/v1/incoming/*`) instead of calling converter API
- `scanner/scanner/loops/scan_loop.py`: registers items directly to scanner DB (no external API call)
- `worker/internal/ingest/client.go`: IngestWorker now calls scanner HTTP API instead of converter API
- `worker/internal/ingest/worker.go`: worker creates media_job and enqueues convert job locally after rclone copy
- `worker/internal/config/config.go`: renamed `CONVERTER_API_URL` → `SCANNER_API_URL` for ingest worker

### Removed
- `api/internal/handler/ingest.go`: removed all `/api/ingest/incoming/*` endpoints from converter API
- `api/internal/service/ingest.go`: removed IngestService from converter API
- `api/internal/repository/incoming.go`: removed IncomingRepository from converter API
- `api/internal/model/incoming.go`: removed IncomingItem and related request/response models
- `scanner/scanner/loops/poll_loop.py`: removed poll loop (scanner no longer polls converter)
- `scanner/scanner/api/converter_client.py`: removed converter HTTP client from scanner

### Added
- `api/internal/db/migrations/013_drop_incoming_media_items.sql`: drop incoming_media_items from converter DB
- `scanner/scanner/migrations/002_add_claim_columns.sql`: add claimed_at/claim_expires_at to scanner_incoming_items
- `worker/internal/repository/job.go`: `CreateForIngest` method for idempotent job creation by ingest worker
- `docs/decisions/ADR-009-scanner-as-ingest-api-server.md`: documents ingest architecture inversion
```

- [ ] **Step 2: Remove ingest section from docs/contracts/api.md**

Remove the entire `/api/ingest/incoming/*` section (register, claim, progress, fail, complete, GET by id).

Add a note:
```markdown
## Ingest API

The ingest API has been removed from the converter. The Scanner service now
owns ingest state and exposes its own HTTP API. See `docs/architecture/modules/scanner.md`.
```

- [ ] **Step 3: Update scanner.md — reflect HTTP server architecture**

In `docs/architecture/modules/scanner.md`, update:

1. **Назначение** section — remove mention of calling converter API, add that scanner exposes HTTP API
2. **Архитектура** tree — replace `api/converter_client.py` with `api/server.py`; remove `loops/poll_loop.py`
3. **Потоки** table — replace `poll_loop` row with `api_server` row:
   | `api_server` | HTTP (Flask, порт `SCANNER_API_PORT`) | Принимает claim/progress/complete/fail от IngestWorker |
4. **Жизненный цикл файла / Поток обработки** — update step 5+ to reflect scanner API instead of converter API
5. **Конфигурация** table — replace `CONVERTER_API_URL`/`CONVERTER_SERVICE_TOKEN` with `SERVICE_TOKEN`/`SCANNER_API_PORT`
6. **Зависимости от converter API** section — remove entirely (scanner has no dependencies on converter)

- [ ] **Step 4: Update worker.md — IngestWorker description**

In `docs/architecture/modules/worker.md`, update the IngestWorker row in the goroutines table:

```
| `IngestWorker` | polling scanner API | Забирает (claim) ingest items из Scanner API, копирует файлы через rclone SFTP, создаёт convert job локально, уведомляет scanner о завершении |
```

Update the Ingest поток diagram:

```
IngestWorker (polling, каждые 10с):
    POST /api/v1/incoming/claim → [item]           (scanner API)
    POST /api/v1/incoming/{id}/progress (copying)  (scanner API)
    rclone copy (SFTP: storage-server → /media/downloads/ingest_{id}/)
    POST /api/v1/incoming/{id}/progress (copied)   (scanner API)
    CREATE media_job + PUSH convert_queue           (local DB + Redis)
    POST /api/v1/incoming/{id}/complete             (scanner API)
→ Converter: ffmpeg HLS как обычно
→ scanner move_worker: os.rename → library/movies/
```

Update Конфигурация table: rename `CONVERTER_API_URL` → `SCANNER_API_URL`.

- [ ] **Step 5: Create ADR-009**

```bash
./scripts/new-adr.sh "scanner as ingest API server"
```

Fill in the ADR:

```markdown
# ADR-009: Scanner as Ingest API Server

**Status:** accepted
**Date:** 2026-03-18

## Context

Originally the scanner called the converter API to register files and poll status
(`POST /api/ingest/incoming/register`, `GET /api/ingest/incoming/{id}`).
The IngestWorker (Go) called the converter API for claim/progress/complete/fail.

This created bidirectional coupling: scanner knew about converter, and converter
owned ingest state.

## Decision

Invert the relationship. The **Scanner** is now the HTTP server — it owns all ingest
state and exposes `/api/v1/incoming/{claim,<id>/progress,<id>/complete,<id>/fail}`.
The **IngestWorker** (Go) polls the scanner instead of the converter.

The IngestWorker creates the `media_job` and pushes to `convert_queue` locally
(previously done by the converter's Complete endpoint).

The converter API no longer has any ingest-related code.

## Consequences

- Scanner has zero knowledge of converter — simpler, more deployable independently
- IngestWorker needs `SCANNER_API_URL` instead of `CONVERTER_API_URL`
- `incoming_media_items` table dropped from converter DB (migration 013)
- Scanner gets `claimed_at`/`claim_expires_at` columns (migration 002) for TTL claims
- The `INGEST_SERVICE_TOKEN` is now verified by the scanner (not the converter)
```

- [ ] **Step 6: Add ADR to README table**

In `docs/decisions/README.md`, add:

```markdown
| [ADR-009](ADR-009-scanner-as-ingest-api-server.md) | Scanner как HTTP-сервер для ingest (инверсия) | accepted |
```

- [ ] **Step 7: Commit**

```bash
git add CHANGELOG.md docs/contracts/api.md docs/architecture/modules/scanner.md docs/architecture/modules/worker.md docs/decisions/ADR-009-scanner-as-ingest-api-server.md docs/decisions/README.md
git commit -m "docs: update scanner/worker docs and contracts for ingest architecture inversion (ADR-009)"
```
