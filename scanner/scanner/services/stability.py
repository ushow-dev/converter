# scanner/scanner/services/stability.py
from datetime import datetime
from typing import Optional


def is_stable(
    current_size: int,
    last_seen_size: Optional[int],
    stable_since: Optional[datetime],
    now: datetime,
    stability_sec: int,
) -> bool:
    """Return True if the file size has not changed for at least stability_sec seconds."""
    if last_seen_size is None or stable_since is None:
        return False
    if current_size != last_seen_size:
        return False
    elapsed = (now - stable_since).total_seconds()
    return elapsed >= stability_sec


def update_stability(
    current_size: int,
    last_seen_size: Optional[int],
    stable_since: Optional[datetime],
    now: datetime,
) -> dict:
    """Return updated stability fields based on whether the size changed."""
    if last_seen_size is not None and current_size == last_seen_size:
        return {
            "file_size_bytes": current_size,
            "stable_since": stable_since if stable_since is not None else now,
        }
    return {
        "file_size_bytes": current_size,
        "stable_since": None,
    }
