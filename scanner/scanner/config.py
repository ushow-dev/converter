# scanner/scanner/config.py
import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    incoming_dir: str
    library_dir: str
    tmdb_api_key: str
    database_url: str
    service_token: str
    api_port: int
    scan_interval_sec: int
    stability_sec: int


def load() -> Config:
    return Config(
        incoming_dir=_require("INCOMING_DIR"),
        library_dir=_require("LIBRARY_DIR"),
        tmdb_api_key=_require("TMDB_API_KEY"),
        database_url=_require("DATABASE_URL"),
        service_token=_require("SERVICE_TOKEN"),
        api_port=int(os.environ.get("SCANNER_API_PORT", "8080")),
        scan_interval_sec=int(os.environ.get("SCAN_INTERVAL_SEC", "30")),
        stability_sec=int(os.environ.get("STABILITY_SEC", "30")),
    )


def _require(key: str) -> str:
    val = os.environ.get(key)
    if not val:
        raise RuntimeError(f"Required env var {key!r} is not set")
    return val
