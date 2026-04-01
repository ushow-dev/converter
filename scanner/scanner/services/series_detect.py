import logging
import os
from pathlib import Path
from typing import Optional

import guessit

logger = logging.getLogger(__name__)

VIDEO_EXTENSIONS = {".mkv", ".mp4", ".avi", ".mov", ".ts", ".m2ts", ".wmv"}


def detect_series_folder(folder_path: Path) -> Optional[list[dict]]:
    """Scan a folder for TV series episodes.
    Returns a list of episode dicts if series content detected, None otherwise.
    Each dict: {file_path, title, season, episode, year}
    """
    episodes = []
    for dirpath, _, filenames in os.walk(folder_path):
        for fname in filenames:
            if fname.startswith("._"):
                continue
            if Path(fname).suffix.lower() not in VIDEO_EXTENSIONS:
                continue
            file_path = Path(dirpath) / fname
            info = guessit.guessit(fname)
            if info.get("type") != "episode":
                continue
            season = info.get("season")
            episode_num = info.get("episode")
            if season is None or episode_num is None:
                continue
            episodes.append({
                "file_path": file_path,
                "title": str(info.get("title", folder_path.name)),
                "season": int(season),
                "episode": int(episode_num),
                "year": info.get("year"),
            })
    if not episodes:
        return None
    episodes.sort(key=lambda e: (e["season"], e["episode"]))
    return episodes
