# scanner/scanner/services/metadata.py
import logging
import re
import time
from typing import Optional

import guessit
import requests

logger = logging.getLogger(__name__)

VIDEO_EXTENSIONS = {".mkv", ".mp4", ".avi", ".mov", ".ts", ".m2ts", ".wmv"}

_HD_RELEASE_TYPES = {"webrip", "web-dl", "webdl", "web dl", "bluray", "blu-ray", "blu ray", "hdtv", "hdrip", "hd"}
_SD_RELEASE_TYPES = {"cam", "ts", "tc", "screener", "scr", "dvdscr", "r5"}

_TMDB_BASE = "https://api.themoviedb.org/3"
_TMDB_IMAGE_BASE = "https://image.tmdb.org/t/p/w500"


def parse_filename(filename: str) -> dict:
    info = guessit.guessit(filename)
    return {
        "title": str(info.get("title", "")),
        "year": info.get("year"),
        "release_type": str(info.get("release_group", info.get("source", ""))) or None,
    }


def build_normalized_name(title: str, year: Optional[int], tmdb_id: Optional[str]) -> str:
    slug = re.sub(r"[^\w\s]", "", title.lower()).strip()
    slug = re.sub(r"\s+", "_", slug)
    parts = [slug]
    if year:
        parts.append(str(year))
    name = "_".join(parts)
    if tmdb_id:
        name += f"_[{tmdb_id}]"
    return name


def quality_label_from_release_type(release_type: Optional[str]) -> Optional[str]:
    if not release_type:
        return None
    rt = release_type.lower()
    if any(hd in rt for hd in _HD_RELEASE_TYPES):
        return "HD"
    if any(sd in rt for sd in _SD_RELEASE_TYPES):
        return "SD"
    return None


def tmdb_search(title: str, year: Optional[int], api_key: str) -> Optional[dict]:
    try:
        params = {"api_key": api_key, "query": title, "language": "en-US"}
        if year:
            params["year"] = year
        resp = requests.get(f"{_TMDB_BASE}/search/movie", params=params, timeout=10)
        resp.raise_for_status()
        results = resp.json().get("results", [])
        if not results:
            return None
        best = results[0]
        poster_url = f"{_TMDB_IMAGE_BASE}{best['poster_path']}" if best.get("poster_path") else None
        return {
            "tmdb_id": str(best["id"]),
            "title": best.get("title", title),
            "imdb_id": best.get("imdb_id"),
            "poster_url": poster_url,
        }
    except requests.RequestException as e:
        logger.warning("TMDB search failed for %r: %s", title, e)
        return None
    finally:
        time.sleep(0.5)
