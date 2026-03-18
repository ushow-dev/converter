# scanner/scanner/services/duplicates.py
from typing import Optional

UPGRADE_THRESHOLD = 8


def decide_action(
    existing_score: Optional[int],
    new_score: Optional[int],
    ffprobe_ok: bool,
) -> str:
    """
    Returns:
      "register"               — new file or upgrade
      "review_duplicate"       — too close in quality
      "review_unknown_quality" — ffprobe failed with existing
    """
    if existing_score is None:
        return "register"
    if not ffprobe_ok:
        return "review_unknown_quality"
    if new_score is not None and new_score >= existing_score + UPGRADE_THRESHOLD:
        return "register"
    return "review_duplicate"
