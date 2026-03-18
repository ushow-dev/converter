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
