# scanner/scanner/loops/scan_loop.py
import logging
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import guessit

from scanner import db
from scanner.config import Config
from scanner.services import duplicates, metadata, quality, series_detect, stability

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
    _retry_failed_items()
    now = datetime.now(timezone.utc)

    # Scan top-level subdirectories for series folders before walking individual files
    incoming_root = Path(cfg.incoming_dir)
    for entry in sorted(incoming_root.iterdir()):
        if not entry.is_dir():
            continue
        try:
            _process_series_folder(cfg, entry, now)
        except Exception:
            logger.exception("error processing series folder %s", entry)

    for file_path in _walk_video_files(incoming_root):
        try:
            _process_file(cfg, file_path, now)
        except Exception:
            logger.exception("error processing file %s", file_path)


MIN_FILE_SIZE_BYTES = 1024 * 1024  # 1 MB — ignore stubs and resource forks


def _walk_video_files(root: Path):
    for dirpath, _, filenames in os.walk(root):
        for fname in filenames:
            if fname.startswith("._"):
                continue  # macOS resource fork files
            if Path(fname).suffix.lower() in VIDEO_EXTENSIONS:
                yield Path(dirpath) / fname


def _process_file(cfg: Config, file_path: Path, now: datetime) -> None:
    try:
        current_size = file_path.stat().st_size
    except OSError:
        return  # file disappeared

    if current_size < MIN_FILE_SIZE_BYTES:
        return  # file not yet written or too small to be a real video

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
    # Check if this is a single episode file (e.g. Show.S02E04.mkv dropped into incoming/).
    info = guessit.guessit(file_path.name)
    if info.get("type") == "episode" and info.get("season") is not None and info.get("episode") is not None:
        _handle_stable_episode(cfg, file_path, file_size, info)
        return

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


def _handle_stable_episode(cfg: Config, file_path: Path, file_size: int, info: dict) -> None:
    """Register a single episode file (e.g. Show.S02E04.mkv) as content_kind='episode'."""
    title = str(info.get("title", ""))
    year = info.get("year")
    season_num = int(info["season"])
    episode_num = int(info["episode"])

    tmdb_result = metadata.tmdb_tv_search(title, year, cfg.tmdb_api_key)
    series_tmdb_id = tmdb_result["tmdb_id"] if tmdb_result else None
    canonical_title = tmdb_result["title"] if tmdb_result else title

    normalized_name = metadata.build_normalized_name(canonical_title, year, series_tmdb_id)
    ep_normalized = f"{normalized_name}_s{season_num:02d}e{episode_num:02d}"

    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    """UPDATE scanner_incoming_items
                       SET status='registered', normalized_name=%s, tmdb_id=%s,
                           content_kind='episode', series_tmdb_id=%s,
                           season_number=%s, episode_number=%s, updated_at=NOW()
                       WHERE source_path=%s AND status='new'""",
                    (ep_normalized, series_tmdb_id, series_tmdb_id,
                     season_num, episode_num, str(file_path)),
                )
    finally:
        db.put_conn(conn)

    logger.info("single episode registered: %s S%02dE%02d (series_tmdb_id=%s)",
                canonical_title, season_num, episode_num, series_tmdb_id)


def _process_series_folder(cfg: Config, folder_path: Path, now: datetime) -> None:
    """Detect series episodes in a folder and register each as a scanner_incoming_items row."""
    episodes = series_detect.detect_series_folder(folder_path)
    if not episodes:
        return

    # Use first episode's title/year for TMDB TV lookup
    first = episodes[0]
    tmdb_result = metadata.tmdb_tv_search(first["title"], first.get("year"), cfg.tmdb_api_key)
    series_tmdb_id = tmdb_result["tmdb_id"] if tmdb_result else None

    canonical_title = tmdb_result["title"] if tmdb_result else first["title"]

    for ep in episodes:
        file_path = ep["file_path"]
        try:
            current_size = file_path.stat().st_size
        except OSError:
            continue
        if current_size < MIN_FILE_SIZE_BYTES:
            continue

        ep_year = ep.get("year") or first.get("year")
        ep_normalized = metadata.build_normalized_name(canonical_title, ep_year, series_tmdb_id)
        ep_normalized = f"{ep_normalized}_s{ep['season']:02d}e{ep['episode']:02d}"

        conn = db.get_conn()
        try:
            with conn:
                with conn.cursor() as cur:
                    cur.execute(
                        "SELECT id, status FROM scanner_incoming_items WHERE source_path = %s",
                        (str(file_path),),
                    )
                    row = cur.fetchone()
                    if row is not None:
                        continue  # already registered, skip
                    cur.execute(
                        """
                        INSERT INTO scanner_incoming_items
                            (source_path, source_filename, file_size_bytes, status,
                             content_kind, normalized_name, tmdb_id,
                             series_tmdb_id, season_number, episode_number)
                        VALUES (%s, %s, %s, 'registered', 'episode', %s, %s, %s, %s, %s)
                        """,
                        (
                            str(file_path),
                            file_path.name,
                            current_size,
                            ep_normalized,
                            series_tmdb_id,
                            series_tmdb_id,
                            ep["season"],
                            ep["episode"],
                        ),
                    )
                    logger.info(
                        "series episode registered: %s S%02dE%02d (series_tmdb_id=%s)",
                        file_path.name, ep["season"], ep["episode"], series_tmdb_id,
                    )
        except Exception:
            logger.exception("failed to register episode %s", file_path)
        finally:
            db.put_conn(conn)


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


def _retry_failed_items() -> None:
    """Reset ingest-failed items (no review_reason) back to registered after 30 min cooldown."""
    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    UPDATE scanner_incoming_items
                    SET status = 'registered', updated_at = NOW()
                    WHERE status = 'failed'
                      AND review_reason IS NULL
                      AND updated_at < NOW() - interval '30 minutes'
                    """
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
