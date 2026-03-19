# Scanner Service (Block 1) Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Реализовать Python scanner-сервис, который сканирует `incoming/`, нормализует имена через GuessIt+TMDB, определяет дубли по ffprobe quality score и регистрирует файлы в converter API; после конвертации переносит файлы в `library/movies/`.

**Architecture:** Долгоживущий Python-процесс с тремя потоками (scan_loop / poll_loop / move_worker), PostgreSQL для состояния, polling converter API для отслеживания статуса. Код живёт в `scanner/` — автономное приложение со своим docker-compose.

**Tech Stack:** Python 3.12, psycopg2-binary, guessit, requests, pytest; ffprobe из пакета ffmpeg; postgres:16-alpine.

---

## File Map

### Converter API (изменения в существующем коде)

| Файл | Действие | Что делает |
|---|---|---|
| `api/internal/service/ingest.go` | Modify | Добавить метод `GetByID` |
| `api/internal/handler/ingest.go` | Modify | Добавить handler `GetByID` |
| `api/internal/server/server.go` | Modify | Зарегистрировать `GET /api/ingest/incoming/{id}` |

### Scanner (новые файлы)

| Файл | Действие | Что делает |
|---|---|---|
| `scanner/pyproject.toml` | Create | Зависимости пакета |
| `scanner/Dockerfile` | Create | python:3.12-slim + ffmpeg |
| `scanner/docker-compose.yml` | Create | postgres + scanner сервисы |
| `scanner/.env.example` | Create | Все env vars с комментариями |
| `scanner/scanner/__init__.py` | Create | Пустой |
| `scanner/scanner/config.py` | Create | Чтение env vars, датаклассы |
| `scanner/scanner/db.py` | Create | psycopg2 pool, авто-миграции |
| `scanner/scanner/migrations/001_initial.sql` | Create | scanner_incoming_items + scanner_library_movies |
| `scanner/scanner/services/__init__.py` | Create | Пустой |
| `scanner/scanner/services/stability.py` | Create | is_stable() |
| `scanner/scanner/services/quality.py` | Create | ffprobe_quality() + quality_score() |
| `scanner/scanner/services/metadata.py` | Create | guessit parse + TMDB search + normalized_name |
| `scanner/scanner/services/duplicates.py` | Create | decide_action() |
| `scanner/scanner/api/__init__.py` | Create | Пустой |
| `scanner/scanner/api/converter_client.py` | Create | register() + get_status() |
| `scanner/scanner/loops/__init__.py` | Create | Пустой |
| `scanner/scanner/loops/scan_loop.py` | Create | Основной scan loop |
| `scanner/scanner/loops/poll_loop.py` | Create | Poll converter API |
| `scanner/scanner/loops/move_worker.py` | Create | os.rename + upsert library |
| `scanner/scanner/main.py` | Create | Точка входа, 3 потока |
| `scanner/tests/__init__.py` | Create | Пустой |
| `scanner/tests/test_stability.py` | Create | Тесты stability |
| `scanner/tests/test_quality.py` | Create | Тесты quality scoring |
| `scanner/tests/test_metadata.py` | Create | Тесты metadata pipeline |
| `scanner/tests/test_duplicates.py` | Create | Тесты duplicate detection |
| `scanner/tests/test_converter_client.py` | Create | Тесты HTTP клиента |

---

## Chunk 1: Converter API — GET /api/ingest/incoming/{id}

### Task 1: Добавить GetByID в сервис и handler

**Context:** Repository уже имеет `GetByID` (строка 118 в `api/internal/repository/incoming.go`). Нужно пробросить через service и handler. chi.URLParam извлекает `{id}` из URL.

**Files:**
- Modify: `api/internal/service/ingest.go`
- Modify: `api/internal/handler/ingest.go`
- Modify: `api/internal/server/server.go`

- [ ] **Step 1: Добавить GetByID в IngestService**

В `api/internal/service/ingest.go` добавить после метода `Complete`:

```go
// GetByID fetches a single incoming item by ID.
func (s *IngestService) GetByID(ctx context.Context, id int64) (*model.IncomingItem, error) {
	return s.repo.GetByID(ctx, id)
}
```

- [ ] **Step 2: Добавить GetByID handler**

В `api/internal/handler/ingest.go` добавить импорты `"errors"`, `"strconv"` и `"github.com/go-chi/chi/v5"`, `"app/api/internal/repository"`, затем добавить метод после `Complete`:

```go
// GetByID handles GET /api/ingest/incoming/{id}.
func (h *IngestHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	item, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, item)
}
```

- [ ] **Step 3: Зарегистрировать маршрут**

В `api/internal/server/server.go` в блоке `/api/ingest` добавить строку после `r.Post("/incoming/complete", ...)`:

```go
r.Get("/incoming/{id}", deps.IngestHandler.GetByID)
```

- [ ] **Step 4: Убедиться что компилируется**

```bash
docker compose build api 2>&1 | tail -5
```

Ожидаем: `Successfully built` без ошибок.

- [ ] **Step 5: Commit**

```bash
git add api/internal/service/ingest.go \
        api/internal/handler/ingest.go \
        api/internal/server/server.go
git commit -m "feat(api): add GET /api/ingest/incoming/{id} for scanner polling"
```

---

## Chunk 2: Scanner — Scaffold и конфигурация

### Task 2: Структура проекта, зависимости, Docker

**Files:**
- Create: `scanner/pyproject.toml`
- Create: `scanner/Dockerfile`
- Create: `scanner/docker-compose.yml`
- Create: `scanner/.env.example`
- Create: все `__init__.py`

- [ ] **Step 1: Создать pyproject.toml**

```toml
# scanner/pyproject.toml
[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.backends.legacy:build"

[project]
name = "scanner"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "psycopg2-binary>=2.9",
    "guessit>=3.8",
    "requests>=2.31",
]

[project.optional-dependencies]
dev = ["pytest>=8.0", "pytest-mock>=3.12"]

[tool.setuptools.packages.find]
where = ["."]
include = ["scanner*"]
```

- [ ] **Step 2: Создать Dockerfile**

```dockerfile
# scanner/Dockerfile
FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY pyproject.toml .
RUN pip install --no-cache-dir -e .

COPY . .

CMD ["python", "-m", "scanner.main"]
```

- [ ] **Step 3: Создать docker-compose.yml**

```yaml
# scanner/docker-compose.yml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: scanner
      POSTGRES_PASSWORD: scanner
      POSTGRES_DB: scanner
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U scanner"]
      interval: 5s
      timeout: 3s
      retries: 10

  scanner:
    build: .
    env_file: .env
    volumes:
      - ${INCOMING_DIR}:/incoming:ro
      - ${LIBRARY_DIR}:/library
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped

volumes:
  pgdata:
```

- [ ] **Step 4: Создать .env.example**

```bash
# scanner/.env.example

# Пути (bind-mounted в контейнер)
INCOMING_DIR=/mnt/storage/incoming
LIBRARY_DIR=/mnt/storage/library

# Converter API
CONVERTER_API_URL=http://converter-server:8000
CONVERTER_SERVICE_TOKEN=changeme

# TMDB (https://www.themoviedb.org/settings/api)
TMDB_API_KEY=changeme

# База данных
DATABASE_URL=postgresql://scanner:scanner@postgres:5432/scanner

# Тюнинг (секунды)
SCAN_INTERVAL_SEC=30
POLL_INTERVAL_SEC=60
STABILITY_SEC=30
```

- [ ] **Step 5: Создать __init__.py файлы**

```bash
mkdir -p scanner/scanner/services scanner/scanner/api scanner/scanner/loops scanner/scanner/migrations scanner/tests
touch scanner/scanner/__init__.py
touch scanner/scanner/services/__init__.py
touch scanner/scanner/api/__init__.py
touch scanner/scanner/loops/__init__.py
touch scanner/tests/__init__.py
```

- [ ] **Step 6: Commit**

```bash
git add scanner/
git commit -m "feat(scanner): scaffold project structure, Dockerfile, docker-compose"
```

---

### Task 3: config.py

**Files:**
- Create: `scanner/scanner/config.py`

- [ ] **Step 1: Написать config.py**

```python
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
```

- [ ] **Step 2: Commit**

```bash
git add scanner/scanner/config.py
git commit -m "feat(scanner): add config loader from env vars"
```

---

### Task 4: DB — миграция и connection pool

**Files:**
- Create: `scanner/scanner/migrations/001_initial.sql`
- Create: `scanner/scanner/db.py`

- [ ] **Step 1: Создать 001_initial.sql**

```sql
-- scanner/scanner/migrations/001_initial.sql

CREATE TABLE IF NOT EXISTS scanner_incoming_items (
    id                              BIGSERIAL PRIMARY KEY,
    source_path                     TEXT NOT NULL UNIQUE,
    source_filename                 TEXT NOT NULL,
    file_size_bytes                 BIGINT,
    first_seen_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stable_since                    TIMESTAMPTZ,
    status                          TEXT NOT NULL DEFAULT 'new',
    -- new|registered|claimed|copying|copied|completed|archived
    -- |failed|review_duplicate|review_unknown_quality|skipped
    review_reason                   TEXT,
    is_upgrade_candidate            BOOLEAN NOT NULL DEFAULT FALSE,
    quality_score                   INTEGER,
    api_item_id                     BIGINT,
    duplicate_of_library_movie_id   BIGINT,
    tmdb_id                         TEXT,
    normalized_name                 TEXT,
    title                           TEXT,
    year                            INTEGER,
    error_message                   TEXT,
    library_relative_path           TEXT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_incoming_status_stable
    ON scanner_incoming_items (status, stable_since);
CREATE INDEX IF NOT EXISTS idx_incoming_dup
    ON scanner_incoming_items (duplicate_of_library_movie_id, status);

CREATE TABLE IF NOT EXISTS scanner_library_movies (
    id                      BIGSERIAL PRIMARY KEY,
    content_kind            TEXT NOT NULL DEFAULT 'movie',
    title                   TEXT NOT NULL,
    title_original          TEXT,
    normalized_name         TEXT NOT NULL UNIQUE,
    year                    INTEGER,
    tmdb_id                 TEXT,
    imdb_id                 TEXT,
    poster_url              TEXT,
    quality_score           INTEGER NOT NULL,
    quality_label           TEXT CHECK (quality_label IS NULL OR quality_label IN ('HD', 'SD')),
    library_relative_path   TEXT NOT NULL,
    file_size_bytes         BIGINT,
    status                  TEXT NOT NULL DEFAULT 'ready',
    -- ready | replaced | deprecated
    source_item_id          BIGINT REFERENCES scanner_incoming_items(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_library_tmdb
    ON scanner_library_movies (tmdb_id) WHERE tmdb_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_library_status
    ON scanner_library_movies (status, updated_at DESC);
```

- [ ] **Step 2: Написать db.py**

```python
# scanner/scanner/db.py
import logging
from pathlib import Path

import psycopg2
import psycopg2.pool

logger = logging.getLogger(__name__)

_pool: psycopg2.pool.ThreadedConnectionPool | None = None


def init(database_url: str, minconn: int = 1, maxconn: int = 5) -> None:
    """Initialise the connection pool and run migrations."""
    global _pool
    _pool = psycopg2.pool.ThreadedConnectionPool(minconn, maxconn, dsn=database_url)
    _run_migrations()


def get_conn() -> psycopg2.extensions.connection:
    """Borrow a connection from the pool. Caller must call put_conn() afterwards."""
    if _pool is None:
        raise RuntimeError("DB pool not initialised — call db.init() first")
    return _pool.getconn()


def put_conn(conn: psycopg2.extensions.connection) -> None:
    """Return a connection to the pool."""
    if _pool is not None:
        _pool.putconn(conn)


def _run_migrations() -> None:
    migrations_dir = Path(__file__).parent / "migrations"
    sql_files = sorted(migrations_dir.glob("*.sql"))

    conn = get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute("""
                    CREATE TABLE IF NOT EXISTS schema_migrations (
                        version TEXT PRIMARY KEY,
                        applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                """)
                for path in sql_files:
                    version = path.stem
                    cur.execute(
                        "SELECT 1 FROM schema_migrations WHERE version = %s",
                        (version,),
                    )
                    if cur.fetchone():
                        continue
                    logger.info("applying migration %s", version)
                    cur.execute(path.read_text())
                    cur.execute(
                        "INSERT INTO schema_migrations (version) VALUES (%s)",
                        (version,),
                    )
    finally:
        put_conn(conn)
```

- [ ] **Step 3: Commit**

```bash
git add scanner/scanner/migrations/ scanner/scanner/db.py
git commit -m "feat(scanner): add DB migration runner and connection pool"
```

---

## Chunk 3: Scanner — Сервисные слои (TDD)

### Task 5: services/stability.py

**Files:**
- Create: `scanner/tests/test_stability.py`
- Create: `scanner/scanner/services/stability.py`

- [ ] **Step 1: Написать тест**

```python
# scanner/tests/test_stability.py
from datetime import datetime, timedelta, timezone
from scanner.services.stability import is_stable, update_stability

UTC = timezone.utc


def _now():
    return datetime.now(UTC)


def test_new_file_is_not_stable():
    now = _now()
    assert not is_stable(
        current_size=1000,
        last_seen_size=None,
        stable_since=None,
        now=now,
        stability_sec=30,
    )


def test_size_changed_resets_stability():
    now = _now()
    stable_since = now - timedelta(seconds=60)
    assert not is_stable(
        current_size=2000,
        last_seen_size=1000,
        stable_since=stable_since,
        now=now,
        stability_sec=30,
    )


def test_stable_after_threshold():
    now = _now()
    stable_since = now - timedelta(seconds=31)
    assert is_stable(
        current_size=1000,
        last_seen_size=1000,
        stable_since=stable_since,
        now=now,
        stability_sec=30,
    )


def test_not_stable_before_threshold():
    now = _now()
    stable_since = now - timedelta(seconds=10)
    assert not is_stable(
        current_size=1000,
        last_seen_size=1000,
        stable_since=stable_since,
        now=now,
        stability_sec=30,
    )


def test_update_stability_size_changed_clears_stable_since():
    now = _now()
    result = update_stability(
        current_size=2000,
        last_seen_size=1000,
        stable_since=now - timedelta(seconds=60),
        now=now,
    )
    assert result["stable_since"] is None
    assert result["file_size_bytes"] == 2000


def test_update_stability_size_same_sets_stable_since():
    now = _now()
    result = update_stability(
        current_size=1000,
        last_seen_size=1000,
        stable_since=None,
        now=now,
    )
    assert result["stable_since"] == now
    assert result["file_size_bytes"] == 1000


def test_update_stability_keeps_existing_stable_since():
    now = _now()
    old_stable = now - timedelta(seconds=60)
    result = update_stability(
        current_size=1000,
        last_seen_size=1000,
        stable_since=old_stable,
        now=now,
    )
    assert result["stable_since"] == old_stable
```

- [ ] **Step 2: Убедиться что тест падает**

```bash
cd scanner && pip install -e ".[dev]" -q && pytest tests/test_stability.py -v 2>&1 | head -20
```

Ожидаем: `ImportError` или `ModuleNotFoundError`.

- [ ] **Step 3: Написать реализацию**

```python
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
```

- [ ] **Step 4: Проверить что тесты проходят**

```bash
cd scanner && pytest tests/test_stability.py -v
```

Ожидаем: `7 passed`.

- [ ] **Step 5: Commit**

```bash
git add scanner/scanner/services/stability.py scanner/tests/test_stability.py
git commit -m "feat(scanner): add stability detection service"
```

---

### Task 6: services/quality.py

**Files:**
- Create: `scanner/tests/test_quality.py`
- Create: `scanner/scanner/services/quality.py`

- [ ] **Step 1: Написать тест**

```python
# scanner/tests/test_quality.py
import json
from scanner.services.quality import compute_quality_score, parse_ffprobe_output


def test_1080p_h264_sdr():
    score = compute_quality_score(width=1920, height=1080, hdr=None, codec="h264", bitrate_kbps=8000)
    assert 40 <= score <= 50


def test_2160p_hevc_hdr10_high_bitrate():
    score = compute_quality_score(width=3840, height=2160, hdr="HDR10", codec="hevc", bitrate_kbps=40000)
    assert score >= 78


def test_720p_h264_sdr():
    score = compute_quality_score(width=1280, height=720, hdr=None, codec="h264", bitrate_kbps=3000)
    assert 25 <= score <= 40


def test_dolby_vision_beats_hdr10():
    score_dv = compute_quality_score(width=3840, height=2160, hdr="DOVI", codec="hevc", bitrate_kbps=40000)
    score_hdr10 = compute_quality_score(width=3840, height=2160, hdr="HDR10", codec="hevc", bitrate_kbps=40000)
    assert score_dv > score_hdr10


def test_av1_beats_hevc_same_res():
    score_av1 = compute_quality_score(width=1920, height=1080, hdr=None, codec="av1", bitrate_kbps=8000)
    score_hevc = compute_quality_score(width=1920, height=1080, hdr=None, codec="hevc", bitrate_kbps=8000)
    assert score_av1 > score_hevc


def test_parse_ffprobe_output_h264_1080p():
    ffprobe_json = json.dumps({
        "streams": [{
            "codec_type": "video",
            "codec_name": "h264",
            "width": 1920,
            "height": 1080,
            "bit_rate": "8000000",
            "color_transfer": "bt709",
            "side_data_list": [],
        }]
    })
    result = parse_ffprobe_output(ffprobe_json)
    assert result is not None
    assert result["codec"] == "h264"
    assert result["width"] == 1920
    assert result["hdr"] is None


def test_parse_ffprobe_output_no_video_stream():
    ffprobe_json = json.dumps({"streams": [{"codec_type": "audio"}]})
    assert parse_ffprobe_output(ffprobe_json) is None
```

- [ ] **Step 2: Убедиться что тест падает**

```bash
cd scanner && pytest tests/test_quality.py -v 2>&1 | head -10
```

- [ ] **Step 3: Написать реализацию**

```python
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
```

- [ ] **Step 4: Проверить что тесты проходят**

```bash
cd scanner && pytest tests/test_quality.py -v
```

Ожидаем: `7 passed`.

- [ ] **Step 5: Commit**

```bash
git add scanner/scanner/services/quality.py scanner/tests/test_quality.py
git commit -m "feat(scanner): add ffprobe quality scoring service"
```

---

### Task 7: services/metadata.py

**Files:**
- Create: `scanner/tests/test_metadata.py`
- Create: `scanner/scanner/services/metadata.py`

- [ ] **Step 1: Написать тест**

```python
# scanner/tests/test_metadata.py
from unittest.mock import patch, Mock
import requests as _requests
from scanner.services.metadata import (
    parse_filename,
    build_normalized_name,
    quality_label_from_release_type,
    tmdb_search,
)


def test_parse_filename_with_year():
    result = parse_filename("Doctor.Bakshi.2023.1080p.WEBRip.mkv")
    assert result["title"].lower() == "doctor bakshi"
    assert result["year"] == 2023


def test_parse_filename_without_year():
    result = parse_filename("SomeMovie.mkv")
    assert result["title"]
    assert result["year"] is None


def test_build_normalized_name_with_tmdb():
    assert build_normalized_name("Doctor Bakshi", 2023, "881935") == "doctor_bakshi_2023_[881935]"


def test_build_normalized_name_without_tmdb():
    assert build_normalized_name("Doctor Bakshi", 2023, None) == "doctor_bakshi_2023"


def test_build_normalized_name_without_year():
    assert build_normalized_name("Doctor Bakshi", None, None) == "doctor_bakshi"


def test_quality_label_webrip():
    assert quality_label_from_release_type("WEBRip") == "HD"


def test_quality_label_bluray():
    assert quality_label_from_release_type("Blu-ray") == "HD"


def test_quality_label_cam():
    assert quality_label_from_release_type("CAM") == "SD"


def test_quality_label_ts():
    assert quality_label_from_release_type("TS") == "SD"


def test_quality_label_unknown():
    assert quality_label_from_release_type("UNKNOWN") is None


def test_quality_label_none():
    assert quality_label_from_release_type(None) is None


def test_tmdb_search_success():
    mock_resp = Mock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "results": [{"id": 881935, "title": "Doctor Bakshi", "release_date": "2023-01-01", "poster_path": "/poster.jpg"}]
    }
    with patch("requests.get", return_value=mock_resp):
        result = tmdb_search("Doctor Bakshi", 2023, "fake_key")
    assert result["tmdb_id"] == "881935"
    assert result["title"] == "Doctor Bakshi"


def test_tmdb_search_no_results():
    mock_resp = Mock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {"results": []}
    with patch("requests.get", return_value=mock_resp):
        assert tmdb_search("UnknownMovie", 2099, "fake_key") is None


def test_tmdb_search_network_error():
    with patch("requests.get", side_effect=_requests.RequestException("timeout")):
        assert tmdb_search("Doctor Bakshi", 2023, "fake_key") is None
```

- [ ] **Step 2: Убедиться что тест падает**

```bash
cd scanner && pytest tests/test_metadata.py -v 2>&1 | head -10
```

- [ ] **Step 3: Написать реализацию**

```python
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
```

- [ ] **Step 4: Проверить что тесты проходят**

```bash
cd scanner && pytest tests/test_metadata.py -v
```

Ожидаем: `13 passed`.

- [ ] **Step 5: Commit**

```bash
git add scanner/scanner/services/metadata.py scanner/tests/test_metadata.py
git commit -m "feat(scanner): add metadata service (guessit + TMDB + normalized_name)"
```

---

### Task 8: services/duplicates.py

**Files:**
- Create: `scanner/tests/test_duplicates.py`
- Create: `scanner/scanner/services/duplicates.py`

- [ ] **Step 1: Написать тест**

```python
# scanner/tests/test_duplicates.py
from scanner.services.duplicates import decide_action, UPGRADE_THRESHOLD


def test_no_existing_movie_register():
    assert decide_action(existing_score=None, new_score=50, ffprobe_ok=True) == "register"


def test_upgrade_candidate_above_threshold():
    assert decide_action(existing_score=40, new_score=50, ffprobe_ok=True) == "register"


def test_duplicate_below_threshold():
    assert decide_action(existing_score=45, new_score=50, ffprobe_ok=True) == "review_duplicate"


def test_duplicate_exact_threshold_is_upgrade():
    assert decide_action(existing_score=40, new_score=48, ffprobe_ok=True) == "register"


def test_unknown_quality_when_ffprobe_fails_with_existing():
    assert decide_action(existing_score=40, new_score=None, ffprobe_ok=False) == "review_unknown_quality"


def test_register_when_ffprobe_fails_no_existing():
    assert decide_action(existing_score=None, new_score=None, ffprobe_ok=False) == "register"


def test_upgrade_threshold_constant():
    assert UPGRADE_THRESHOLD == 8
```

- [ ] **Step 2: Убедиться что тест падает**

```bash
cd scanner && pytest tests/test_duplicates.py -v 2>&1 | head -10
```

- [ ] **Step 3: Написать реализацию**

```python
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
      "register"               — новый файл или апгрейд
      "review_duplicate"       — слишком близко по качеству
      "review_unknown_quality" — ffprobe провалился при наличии existing
    """
    if existing_score is None:
        return "register"
    if not ffprobe_ok:
        return "review_unknown_quality"
    if new_score is not None and new_score >= existing_score + UPGRADE_THRESHOLD:
        return "register"
    return "review_duplicate"
```

- [ ] **Step 4: Проверить что тесты проходят**

```bash
cd scanner && pytest tests/test_duplicates.py -v
```

Ожидаем: `7 passed`.

- [ ] **Step 5: Commit**

```bash
git add scanner/scanner/services/duplicates.py scanner/tests/test_duplicates.py
git commit -m "feat(scanner): add duplicate detection service"
```

---

### Task 9: api/converter_client.py

**Files:**
- Create: `scanner/tests/test_converter_client.py`
- Create: `scanner/scanner/api/converter_client.py`

- [ ] **Step 1: Написать тест**

```python
# scanner/tests/test_converter_client.py
from unittest.mock import patch, Mock
import requests as _requests
from scanner.api.converter_client import ConverterClient

BASE_URL = "http://converter:8000"
TOKEN = "secret"


def _client():
    return ConverterClient(base_url=BASE_URL, service_token=TOKEN)


def test_register_success():
    mock_resp = Mock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {"id": 42, "status": "new"}
    mock_resp.raise_for_status = Mock()
    with patch("requests.Session.post", return_value=mock_resp) as mock_post:
        item_id = _client().register(
            source_path="/incoming/Movie.mkv",
            source_filename="Movie.mkv",
            content_kind="movie",
        )
    assert item_id == 42


def test_register_raises_on_http_error():
    mock_resp = Mock()
    mock_resp.raise_for_status.side_effect = _requests.HTTPError("500")
    with patch("requests.Session.post", return_value=mock_resp):
        try:
            _client().register(source_path="/incoming/Movie.mkv", source_filename="Movie.mkv", content_kind="movie")
            assert False, "should raise"
        except _requests.HTTPError:
            pass


def test_get_status_completed():
    mock_resp = Mock()
    mock_resp.raise_for_status = Mock()
    mock_resp.json.return_value = {"id": 42, "status": "completed", "error_message": None}
    with patch("requests.Session.get", return_value=mock_resp):
        status, error = _client().get_status(42)
    assert status == "completed"
    assert error is None


def test_get_status_failed():
    mock_resp = Mock()
    mock_resp.raise_for_status = Mock()
    mock_resp.json.return_value = {"id": 42, "status": "failed", "error_message": "rclone error"}
    with patch("requests.Session.get", return_value=mock_resp):
        status, error = _client().get_status(42)
    assert status == "failed"
    assert error == "rclone error"


def test_get_status_network_error_raises():
    with patch("requests.Session.get", side_effect=_requests.RequestException("timeout")):
        try:
            _client().get_status(42)
            assert False, "should raise"
        except _requests.RequestException:
            pass
```

- [ ] **Step 2: Убедиться что тест падает**

```bash
cd scanner && pytest tests/test_converter_client.py -v 2>&1 | head -10
```

- [ ] **Step 3: Написать реализацию**

```python
# scanner/scanner/api/converter_client.py
import logging
from typing import Optional, Tuple

import requests

logger = logging.getLogger(__name__)


class ConverterClient:
    """HTTP client for the converter API ingest endpoints."""

    def __init__(self, base_url: str, service_token: str) -> None:
        self._base = base_url.rstrip("/")
        self._session = requests.Session()
        self._session.headers.update({
            "X-Service-Token": service_token,
            "Content-Type": "application/json",
        })

    def register(
        self,
        source_path: str,
        source_filename: str,
        content_kind: str = "movie",
        normalized_name: Optional[str] = None,
        tmdb_id: Optional[str] = None,
        file_size_bytes: Optional[int] = None,
        quality_score: Optional[int] = None,
        is_upgrade_candidate: bool = False,
        duplicate_of_movie_id: Optional[int] = None,
        review_reason: Optional[str] = None,
        stable_since: Optional[str] = None,
    ) -> int:
        """Register a file with the converter API. Returns api_item_id."""
        payload: dict = {
            "source_path": source_path,
            "source_filename": source_filename,
            "content_kind": content_kind,
            "is_upgrade_candidate": is_upgrade_candidate,
        }
        for key, val in [
            ("normalized_name", normalized_name),
            ("tmdb_id", tmdb_id),
            ("file_size_bytes", file_size_bytes),
            ("quality_score", quality_score),
            ("duplicate_of_movie_id", duplicate_of_movie_id),
            ("review_reason", review_reason),
            ("stable_since", stable_since),
        ]:
            if val is not None:
                payload[key] = val

        resp = self._session.post(
            f"{self._base}/api/ingest/incoming/register",
            json=payload,
            timeout=15,
        )
        resp.raise_for_status()
        return resp.json()["id"]

    def get_status(self, api_item_id: int) -> Tuple[str, Optional[str]]:
        """Fetch current status. Returns (status, error_message). Raises on error."""
        resp = self._session.get(
            f"{self._base}/api/ingest/incoming/{api_item_id}",
            timeout=15,
        )
        resp.raise_for_status()
        data = resp.json()
        return data["status"], data.get("error_message")
```

- [ ] **Step 4: Проверить что тесты проходят**

```bash
cd scanner && pytest tests/test_converter_client.py -v
```

Ожидаем: `5 passed`.

- [ ] **Step 5: Проверить все тесты вместе**

```bash
cd scanner && pytest tests/ -v
```

Ожидаем: `32 passed`, `0 failed`.

- [ ] **Step 6: Commit**

```bash
git add scanner/scanner/api/converter_client.py scanner/tests/test_converter_client.py
git commit -m "feat(scanner): add converter API client"
```

---

## Chunk 4: Scanner — Loops и main

### Task 10: loops/scan_loop.py

**Files:**
- Create: `scanner/scanner/loops/scan_loop.py`

- [ ] **Step 1: Написать scan_loop.py**

```python
# scanner/scanner/loops/scan_loop.py
import logging
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from scanner import db
from scanner.api.converter_client import ConverterClient
from scanner.config import Config
from scanner.services import duplicates, metadata, quality, stability

logger = logging.getLogger(__name__)

VIDEO_EXTENSIONS = {".mkv", ".mp4", ".avi", ".mov", ".ts", ".m2ts", ".wmv"}


def run(cfg: Config, client: ConverterClient) -> None:
    """Run scan loop forever. Call from a daemon thread."""
    logger.info("scan_loop started, interval=%ds", cfg.scan_interval_sec)
    while True:
        try:
            _scan_once(cfg, client)
        except Exception:
            logger.exception("scan_loop iteration failed")
        time.sleep(cfg.scan_interval_sec)


def _scan_once(cfg: Config, client: ConverterClient) -> None:
    now = datetime.now(timezone.utc)
    for file_path in _walk_video_files(Path(cfg.incoming_dir)):
        try:
            _process_file(cfg, client, file_path, now)
        except Exception:
            logger.exception("error processing file %s", file_path)


def _walk_video_files(root: Path):
    for dirpath, _, filenames in os.walk(root):
        for fname in filenames:
            if Path(fname).suffix.lower() in VIDEO_EXTENSIONS:
                yield Path(dirpath) / fname


def _process_file(cfg: Config, client: ConverterClient, file_path: Path, now: datetime) -> None:
    try:
        current_size = file_path.stat().st_size
    except OSError:
        return  # file disappeared

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

    _handle_stable_file(cfg, client, file_path, current_size)


def _handle_stable_file(cfg: Config, client: ConverterClient, file_path: Path, file_size: int) -> None:
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
            client=client,
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


def _do_register(client, file_path, normalized_name, tmdb_id, file_size, quality_score, is_upgrade_candidate):
    try:
        api_item_id = client.register(
            source_path=str(file_path),
            source_filename=file_path.name,
            content_kind="movie",
            normalized_name=normalized_name,
            tmdb_id=tmdb_id,
            file_size_bytes=file_size,
            quality_score=quality_score,
            is_upgrade_candidate=is_upgrade_candidate,
        )
    except Exception as e:
        logger.error("register failed for %s: %s", file_path, e)
        return

    conn = db.get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE scanner_incoming_items SET status='registered', api_item_id=%s, normalized_name=%s, tmdb_id=%s, quality_score=%s, is_upgrade_candidate=%s, updated_at=NOW() WHERE source_path=%s AND status='new'",
                    (api_item_id, normalized_name, tmdb_id, quality_score, is_upgrade_candidate, str(file_path)),
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
```

- [ ] **Step 2: Commit**

```bash
git add scanner/scanner/loops/scan_loop.py
git commit -m "feat(scanner): add scan_loop"
```

---

### Task 11: loops/poll_loop.py

**Files:**
- Create: `scanner/scanner/loops/poll_loop.py`

- [ ] **Step 1: Написать poll_loop.py**

```python
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
```

- [ ] **Step 2: Commit**

```bash
git add scanner/scanner/loops/poll_loop.py
git commit -m "feat(scanner): add poll_loop for converter status tracking"
```

---

### Task 12: loops/move_worker.py

**Files:**
- Create: `scanner/scanner/loops/move_worker.py`

- [ ] **Step 1: Написать move_worker.py**

```python
# scanner/scanner/loops/move_worker.py
import logging
import os
import queue
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
        os.rename(source_path, target_path)
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
```

- [ ] **Step 2: Commit**

```bash
git add scanner/scanner/loops/move_worker.py
git commit -m "feat(scanner): add move_worker for library transfer"
```

---

### Task 13: main.py — точка входа

**Files:**
- Create: `scanner/scanner/main.py`

- [ ] **Step 1: Написать main.py**

```python
# scanner/scanner/main.py
import logging
import queue
import signal
import sys
import threading

from scanner import db
from scanner.api.converter_client import ConverterClient
from scanner.config import load as load_config
from scanner.loops import move_worker, poll_loop, scan_loop

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)


def main() -> None:
    cfg = load_config()
    db.init(cfg.database_url)
    client = ConverterClient(
        base_url=cfg.converter_api_url,
        service_token=cfg.converter_service_token,
    )
    mq: queue.Queue = queue.Queue()

    threads = [
        threading.Thread(target=scan_loop.run, args=(cfg, client), name="scan_loop", daemon=True),
        threading.Thread(target=poll_loop.run, args=(cfg, client, mq), name="poll_loop", daemon=True),
        threading.Thread(target=move_worker.run, args=(cfg, mq), name="move_worker", daemon=True),
    ]

    for t in threads:
        t.start()
        logger.info("started thread %s", t.name)

    stop = threading.Event()

    def _handle_signal(signum, frame):  # noqa: ANN001
        logger.info("received signal %d, shutting down", signum)
        stop.set()

    signal.signal(signal.SIGINT, _handle_signal)
    signal.signal(signal.SIGTERM, _handle_signal)

    stop.wait()
    logger.info("scanner stopped")
    sys.exit(0)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Проверить что все тесты проходят**

```bash
cd scanner && pytest tests/ -v
```

Ожидаем: `32 passed`, `0 failed`.

- [ ] **Step 3: Commit**

```bash
git add scanner/scanner/main.py
git commit -m "feat(scanner): add main entrypoint with 3 daemon threads"
```

---

## Chunk 5: Docs и CHANGELOG

### Task 14: Обновить документацию

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/contracts/api.md`

- [ ] **Step 1: Обновить CHANGELOG.md**

Добавить в `## [Unreleased]`:

```markdown
### Added
- `api/internal/handler/ingest.go`, `api/internal/service/ingest.go`: добавлен `GET /api/ingest/incoming/{id}` для polling со стороны scanner
- `scanner/`: новый Python scanner-сервис — сканирует `incoming/`, GuessIt+TMDB нормализация, ffprobe quality scoring, duplicate detection, регистрация в converter API, перенос в `library/movies/` после конвертации
- `scanner/scanner/services/stability.py`: детектирование стабильности файла
- `scanner/scanner/services/quality.py`: детерминированный quality score (resolution+HDR+codec+bitrate)
- `scanner/scanner/services/metadata.py`: GuessIt + TMDB + normalized_name
- `scanner/scanner/services/duplicates.py`: политика дублей (upgrade threshold=8)
- `scanner/scanner/api/converter_client.py`: HTTP клиент к converter API
- `scanner/scanner/loops/`: scan_loop, poll_loop, move_worker
- `scanner/docker-compose.yml`: автономное развёртывание на storage-сервере
```

- [ ] **Step 2: Обновить docs/contracts/api.md**

Добавить в секцию Ingest API:

```markdown
### GET /api/ingest/incoming/{id}

Auth: `X-Service-Token`

Returns the current status of an incoming media item. Used by the scanner to poll processing state without a push callback.

**Response 200:**
```json
{
  "id": 42,
  "status": "completed",
  "error_message": null
}
```

**Response 404:** item not found
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md docs/contracts/api.md
git commit -m "docs: update CHANGELOG and API contracts for scanner Block 1"
```
