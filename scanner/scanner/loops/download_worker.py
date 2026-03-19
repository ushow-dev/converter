import logging
import time
from pathlib import Path

import requests

from scanner import db
from scanner.config import Config

logger = logging.getLogger(__name__)

_CHUNK_SIZE = 1024 * 1024  # 1 MB


def run(cfg: Config) -> None:
    """Run download worker forever. Call from a daemon thread."""
    logger.info("download_worker started, incoming_dir=%s", cfg.incoming_dir)
    while True:
        try:
            _process_pending(cfg)
        except Exception:
            logger.exception("download_worker: unhandled error")
        time.sleep(10)


def _process_pending(cfg: Config) -> None:
    row = _fetch_queued()
    if row is None:
        return
    item_id, url, filename, proxy_url = row
    dest = Path(cfg.incoming_dir) / filename
    logger.info("download_worker: downloading id=%d url=%s to %s (proxy=%s)", item_id, url, dest, proxy_url or "none")
    _set_status(item_id, "downloading", None)
    try:
        _download(url, dest, proxy_url)
        _set_status(item_id, "done", None)
        logger.info("download_worker: finished id=%d -> %s", item_id, dest)
    except Exception as exc:
        error_msg = str(exc)
        logger.error("download_worker: failed id=%d: %s", item_id, error_msg)
        _set_status(item_id, "failed", error_msg)


def _download(url: str, dest: Path, proxy_url: str | None) -> None:
    proxies = None
    if proxy_url:
        proxies = {"http": proxy_url, "https": proxy_url}

    with requests.get(url, proxies=proxies, stream=True, timeout=30) as resp:
        resp.raise_for_status()
        dest.parent.mkdir(parents=True, exist_ok=True)
        with open(dest, "wb") as f:
            for chunk in resp.iter_content(chunk_size=_CHUNK_SIZE):
                if chunk:
                    f.write(chunk)


def _fetch_queued() -> tuple[int, str, str, str | None] | None:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    SELECT id, url, filename, proxy_url FROM scanner_downloads
                    WHERE status = 'queued'
                    ORDER BY created_at
                    LIMIT 1
                    FOR UPDATE SKIP LOCKED
                    """,
                )
                row = cur.fetchone()
                if row is None:
                    return None
                return (row[0], row[1], row[2], row[3])
    finally:
        db.put_conn(conn)


def _set_status(item_id: int, status: str, error_message: str | None) -> None:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_downloads SET status=%s, error_message=%s, updated_at=NOW() WHERE id=%s",
                    (status, error_message, item_id),
                )
    finally:
        db.put_conn(conn)
