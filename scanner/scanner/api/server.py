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


class DownloadRequest(BaseModel):
    url: str
    filename: str


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

    @app.post("/api/v1/downloads", status_code=201)
    def create_download(_: AuthDep, body: DownloadRequest):
        if not body.url or not body.filename:
            raise HTTPException(status_code=400, detail="url and filename are required")
        item_id = _create_download(body.url, body.filename)
        return {"id": item_id}

    @app.get("/api/v1/downloads")
    def list_downloads(_: AuthDep):
        items = _list_downloads()
        return {"items": items}

    @app.post("/api/v1/downloads/{download_id}/retry", status_code=204)
    def retry_download(download_id: int, _: AuthDep):
        updated = _retry_download(download_id)
        if not updated:
            raise HTTPException(status_code=404, detail="not found or not retryable")
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
                    WITH expired AS (
                        UPDATE scanner_incoming_items
                        SET status = 'registered',
                            claimed_at = NULL,
                            claim_expires_at = NULL,
                            updated_at = NOW()
                        WHERE status = 'claimed' AND claim_expires_at < NOW()
                        RETURNING id
                    ),
                    candidates AS (
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


def _list_downloads() -> list:
    conn = db.get_conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT id, url, filename, status, error_message, created_at, updated_at
                FROM scanner_downloads
                ORDER BY created_at DESC
                LIMIT 100
                """,
            )
            rows = cur.fetchall()
            return [
                {
                    "id": r[0],
                    "url": r[1],
                    "filename": r[2],
                    "status": r[3],
                    "error_message": r[4],
                    "created_at": r[5].isoformat() if r[5] else None,
                    "updated_at": r[6].isoformat() if r[6] else None,
                }
                for r in rows
            ]
    finally:
        db.put_conn(conn)


def _retry_download(download_id: int) -> bool:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    UPDATE scanner_downloads
                    SET status = 'queued', error_message = NULL, updated_at = NOW()
                    WHERE id = %s AND status = 'failed'
                    """,
                    (download_id,),
                )
                return cur.rowcount > 0
    finally:
        db.put_conn(conn)


def _create_download(url: str, filename: str) -> int:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "INSERT INTO scanner_downloads (url, filename) VALUES (%s, %s) RETURNING id",
                    (url, filename),
                )
                return cur.fetchone()[0]
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
