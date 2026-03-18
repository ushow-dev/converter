# scanner/scanner/services/quality.py
import json
import logging
import subprocess
from typing import Optional

logger = logging.getLogger(__name__)

_BITRATE_CAPS = {"2160p": 80_000, "1440p": 40_000, "1080p": 20_000, "720p": 10_000, "sd": 4_000}
_RESOLUTION_SCORES = {"2160p": 60, "1440p": 45, "1080p": 35, "720p": 20, "sd": 10}
_HDR_SCORES = {"DOVI": 15, "HDR10+": 10, "HDR10": 10, "HLG": 5}
_CODEC_SCORES = {"av1": 10, "hevc": 8, "h265": 8, "h264": 5}


def _resolution_tier(width: int, height: int) -> str:
    if height >= 2160 or width >= 3840:
        return "2160p"
    if height >= 1440 or width >= 2560:
        return "1440p"
    if height >= 1080 or width >= 1920:
        return "1080p"
    if height >= 720 or width >= 1280:
        return "720p"
    return "sd"


def compute_quality_score(width: int, height: int, hdr: Optional[str], codec: str, bitrate_kbps: int) -> int:
    tier = _resolution_tier(width, height)
    res_score = _RESOLUTION_SCORES[tier]
    hdr_score = _HDR_SCORES.get(hdr or "", 0)
    codec_score = _CODEC_SCORES.get(codec.lower(), 2)
    cap = _BITRATE_CAPS[tier]
    bitrate_score = int(min(bitrate_kbps / cap, 1.0) * 15)
    return res_score + hdr_score + codec_score + bitrate_score


def parse_ffprobe_output(ffprobe_json: str) -> Optional[dict]:
    try:
        data = json.loads(ffprobe_json)
    except json.JSONDecodeError:
        return None

    video = next((s for s in data.get("streams", []) if s.get("codec_type") == "video"), None)
    if video is None:
        return None

    hdr = None
    color_transfer = video.get("color_transfer", "")
    side_data = video.get("side_data_list", [])
    if any(sd.get("side_data_type") == "DOVI configuration record" for sd in side_data):
        hdr = "DOVI"
    elif color_transfer in ("smpte2084", "arib-std-b67"):
        hdr = "HDR10"

    bit_rate = video.get("bit_rate", "")
    bitrate_kbps = int(bit_rate) // 1000 if str(bit_rate).isdigit() else 0

    return {
        "codec": video.get("codec_name", ""),
        "width": video.get("width", 0),
        "height": video.get("height", 0),
        "hdr": hdr,
        "bitrate_kbps": bitrate_kbps,
    }


def ffprobe_quality(file_path: str) -> Optional[dict]:
    """Run ffprobe and return quality info dict, or None on failure."""
    try:
        result = subprocess.run(
            ["ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", file_path],
            capture_output=True, text=True, timeout=30,
        )
        if result.returncode != 0:
            logger.warning("ffprobe failed for %s: %s", file_path, result.stderr)
            return None
    except (subprocess.TimeoutExpired, FileNotFoundError) as e:
        logger.warning("ffprobe error for %s: %s", file_path, e)
        return None

    parsed = parse_ffprobe_output(result.stdout)
    if parsed is None:
        return None

    score = compute_quality_score(
        width=parsed["width"], height=parsed["height"],
        hdr=parsed["hdr"], codec=parsed["codec"], bitrate_kbps=parsed["bitrate_kbps"],
    )
    return {**parsed, "quality_score": score}
