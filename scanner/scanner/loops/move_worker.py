# scanner/scanner/loops/move_worker.py
import logging
import queue
import shutil
from pathlib import Path

from scanner import db
from scanner.config import Config

logger = logging.getLogger(__name__)


def run(cfg: Config, move_queue: queue.Queue) -> None:
    """Run move worker forever. Blocks on queue. Call from a daemon thread."""
    logger.info("move_worker started, library_dir=%s", cfg.library_dir)
    while True:
        try:
            task = move_queue.get(timeout=5)
        except queue.Empty:
            continue
        try:
            _handle_move(cfg, task)
        except Exception:
            logger.exception("move_worker: unhandled error for task %r", task)
        finally:
            move_queue.task_done()


def _handle_move(cfg: Config, task: dict) -> None:
    item_id: int = task["item_id"]
    source_path = Path(task["source_path"])
    normalized_name: str = task.get("normalized_name") or source_path.stem

    target_dir = Path(cfg.library_dir) / "movies" / normalized_name
    target_path = target_dir / source_path.name
    relative_path = str(Path("movies") / normalized_name / source_path.name)

    item_info = _fetch_item(item_id)
    if item_info is None:
        logger.error("move_worker: item %d not found in DB", item_id)
        return

    try:
        target_dir.mkdir(parents=True, exist_ok=True)
        shutil.move(str(source_path), target_path)
    except OSError as e:
        logger.error("move failed %s → %s: %s", source_path, target_path, e)
        _mark_failed(item_id, "move_failed")
        return

    logger.info("moved %s → %s", source_path, target_path)

    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                title = item_info.get("title") or normalized_name
                cur.execute(
                    """
                    INSERT INTO scanner_library_movies
                        (title, title_original, normalized_name, year, tmdb_id,
                         quality_score, library_relative_path, file_size_bytes, status, source_item_id)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, 'ready', %s)
                    ON CONFLICT (normalized_name) DO UPDATE SET
                        quality_score         = EXCLUDED.quality_score,
                        library_relative_path = EXCLUDED.library_relative_path,
                        file_size_bytes       = EXCLUDED.file_size_bytes,
                        status                = 'ready',
                        updated_at            = NOW()
                    """,
                    (
                        title, item_info.get("title"), normalized_name,
                        item_info.get("year"), item_info.get("tmdb_id"),
                        item_info.get("quality_score", 0), relative_path,
                        item_info.get("file_size_bytes"), item_id,
                    ),
                )
                cur.execute(
                    "UPDATE scanner_incoming_items SET status='archived', library_relative_path=%s, updated_at=NOW() WHERE id=%s",
                    (relative_path, item_id),
                )
    finally:
        db.put_conn(conn)


def _fetch_item(item_id: int) -> dict | None:
    conn = db.get_conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT title, year, tmdb_id, quality_score, file_size_bytes FROM scanner_incoming_items WHERE id=%s",
                (item_id,),
            )
            row = cur.fetchone()
            if row is None:
                return None
            return {"title": row[0], "year": row[1], "tmdb_id": row[2], "quality_score": row[3], "file_size_bytes": row[4]}
    finally:
        db.put_conn(conn)


def _mark_failed(item_id: int, review_reason: str) -> None:
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_incoming_items SET status='failed', review_reason=%s, updated_at=NOW() WHERE id=%s",
                    (review_reason, item_id),
                )
    finally:
        db.put_conn(conn)
