# scanner/scanner/loops/poll_loop.py
import logging
import queue
import time

from scanner import db
from scanner.api.converter_client import ConverterClient
from scanner.config import Config

logger = logging.getLogger(__name__)

# Converter status → scanner local status
_STATUS_MAP = {
    "new":       "claimed",
    "claimed":   "claimed",
    "copying":   "copying",
    "copied":    "copied",
    "completed": "completed",
    "failed":    "failed",
}

ACTIVE_STATUSES = ("registered", "claimed", "copying", "copied")


def run(cfg: Config, client: ConverterClient, move_queue: queue.Queue) -> None:
    """Run poll loop forever. Call from a daemon thread."""
    logger.info("poll_loop started, interval=%ds", cfg.poll_interval_sec)
    while True:
        try:
            _poll_once(client, move_queue)
        except Exception:
            logger.exception("poll_loop iteration failed")
        time.sleep(cfg.poll_interval_sec)


def _poll_once(client: ConverterClient, move_queue: queue.Queue) -> None:
    items = _fetch_active_items()
    for item_id, api_item_id, source_path, normalized_name in items:
        try:
            conv_status, error_msg = client.get_status(api_item_id)
        except Exception as e:
            logger.warning("get_status failed for api_item_id=%d: %s", api_item_id, e)
            continue

        local_status = _STATUS_MAP.get(conv_status)
        if local_status is None:
            logger.warning("unknown converter status %r for item %d", conv_status, item_id)
            continue

        _update_item_status(item_id, local_status, error_msg)

        if local_status == "completed":
            logger.info("item %d completed, queuing move (source=%s)", item_id, source_path)
            move_queue.put({"item_id": item_id, "source_path": source_path, "normalized_name": normalized_name})


def _fetch_active_items() -> list:
    placeholders = ", ".join(["%s"] * len(ACTIVE_STATUSES))
    conn = db.get_conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                f"SELECT id, api_item_id, source_path, normalized_name FROM scanner_incoming_items WHERE status IN ({placeholders}) AND api_item_id IS NOT NULL",
                ACTIVE_STATUSES,
            )
            return cur.fetchall()
    finally:
        db.put_conn(conn)


def _update_item_status(item_id: int, status: str, error_msg) -> None:
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
