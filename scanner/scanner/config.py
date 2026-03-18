# scanner/scanner/config.py
import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    incoming_dir: str
    library_dir: str
    converter_api_url: str
    converter_service_token: str
    tmdb_api_key: str
    database_url: str
    scan_interval_sec: int
    poll_interval_sec: int
    stability_sec: int


def load() -> Config:
    return Config(
        incoming_dir=_require("INCOMING_DIR"),
        library_dir=_require("LIBRARY_DIR"),
        converter_api_url=_require("CONVERTER_API_URL"),
        converter_service_token=_require("CONVERTER_SERVICE_TOKEN"),
        tmdb_api_key=_require("TMDB_API_KEY"),
        database_url=_require("DATABASE_URL"),
        scan_interval_sec=int(os.environ.get("SCAN_INTERVAL_SEC", "30")),
        poll_interval_sec=int(os.environ.get("POLL_INTERVAL_SEC", "60")),
        stability_sec=int(os.environ.get("STABILITY_SEC", "30")),
    )


def _require(key: str) -> str:
    val = os.environ.get(key)
    if not val:
        raise RuntimeError(f"Required env var {key!r} is not set")
    return val
