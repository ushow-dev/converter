# Scanner Service — Design Spec

**Date:** 2026-03-18
**Status:** Approved

---

## Goal

Python-сервис на storage-сервере, который автоматически индексирует папку `incoming/`, нормализует имена через GuessIt + TMDB, определяет дубли через ffprobe quality scoring и регистрирует файлы в converter API. После завершения конвертации перемещает файлы в `library/movies/`.

## Architecture

Долгоживущий Python-процесс с тремя независимыми потоками. Общая база данных — PostgreSQL. Потоки общаются через DB и in-process `queue.Queue`.

```
scanner/
├── docker-compose.yml          # postgres + scanner service
├── .env.example
├── Dockerfile
├── pyproject.toml
├── scanner/
│   ├── main.py                 # точка входа, запускает 3 потока
│   ├── config.py               # все env vars
│   ├── db.py                   # connection pool, миграции
│   ├── loops/
│   │   ├── scan_loop.py        # сканирует incoming/ каждые 30с
│   │   ├── poll_loop.py        # опрашивает converter API каждые 60с
│   │   └── move_worker.py      # выполняет os.rename() в library/
│   ├── services/
│   │   ├── stability.py        # проверка стабильности файла
│   │   ├── metadata.py         # GuessIt + TMDB lookup
│   │   ├── quality.py          # ffprobe + quality_score расчёт
│   │   └── duplicates.py       # логика дублей и апгрейдов
│   ├── api/
│   │   └── converter_client.py # HTTP клиент к converter API
│   └── migrations/
│       └── 001_initial.sql
└── tests/
    ├── test_stability.py
    ├── test_metadata.py
    ├── test_quality.py
    ├── test_duplicates.py
    └── test_converter_client.py
```

### Потоки

| Поток | Интервал | Ответственность |
|---|---|---|
| `scan_loop` | 30с | Обход `incoming/`, stability check, metadata, register |
| `poll_loop` | 60с | Опрос converter API, постановка задач на move |
| `move_worker` | event-driven | `os.rename()` + upsert в library |

Потоки не разделяют состояние напрямую: `scan_loop` → `poll_loop` через PostgreSQL, `poll_loop` → `move_worker` через `queue.Queue`.

---

## Data Model

### `scanner_incoming_items`

Операционная очередь файлов из `incoming/`.

```sql
CREATE TABLE scanner_incoming_items (
    id                              BIGSERIAL PRIMARY KEY,
    source_path                     TEXT NOT NULL UNIQUE,   -- абсолютный путь в incoming/
    source_filename                 TEXT NOT NULL,
    file_size_bytes                 BIGINT,
    first_seen_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stable_since                    TIMESTAMPTZ,            -- когда размер перестал меняться
    status                          TEXT NOT NULL DEFAULT 'new',
    -- new | registered | claimed | copying | copied | completed | archived
    -- | failed | review_duplicate | review_unknown_quality | skipped
    review_reason                   TEXT,                   -- full_duplicate | unknown_quality | move_failed
    is_upgrade_candidate            BOOLEAN NOT NULL DEFAULT FALSE,
    quality_score                   INTEGER,                -- 0..100, NULL до ffprobe
    api_item_id                     BIGINT,                 -- id в converter incoming_media_items
    duplicate_of_library_movie_id   BIGINT,                 -- FK → scanner_library_movies.id (nullable)
    tmdb_id                         TEXT,
    normalized_name                 TEXT,                   -- doctor_bakshi_2023_[881935]
    title                           TEXT,
    year                            INTEGER,
    error_message                   TEXT,
    library_relative_path           TEXT,                   -- заполняется после move
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX ON scanner_incoming_items (status, stable_since);
CREATE INDEX ON scanner_incoming_items (duplicate_of_library_movie_id, status);
```

**Статусы (state flow):**
```
new → registered → claimed → copying → copied → completed → archived
new → review_duplicate
new → review_unknown_quality
new → skipped
copying → failed
claimed → failed  (lease expired)
```

- `new` → файл обнаружен, ожидает стабильности
- `registered` → зарегистрирован в converter API
- `claimed` → converter worker принял item
- `copying` → converter worker копирует файл по rclone
- `copied` → rclone завершён, поставлен в convert_queue
- `completed` → конвертация завершена, ожидает move в library
- `archived` → перемещён в library, финальный статус
- `failed` → ошибка на любом этапе
- `review_duplicate` → полный дубль, файл переименован
- `review_unknown_quality` → качество не определено, файл переименован
- `skipped` → неподдерживаемый тип файла

### `scanner_library_movies`

Каталог готовых фильмов в `library/`.

```sql
CREATE TABLE scanner_library_movies (
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

CREATE UNIQUE INDEX ON scanner_library_movies (tmdb_id) WHERE tmdb_id IS NOT NULL;
CREATE INDEX ON scanner_library_movies (status, updated_at DESC);
```

---

## Core Logic

### scan_loop

```
каждые SCAN_INTERVAL_SEC секунд:
  1. Рекурсивный обход INCOMING_DIR
  2. Для каждого видеофайла (.mkv, .mp4, .avi, .mov, .ts, .m2ts):
     - Если не в DB → INSERT status=new
     - Если в DB и status=new → UPDATE last_seen_at, file_size_bytes
  3. Для каждого status=new со стабильным размером (last_seen_at - stable_since >= STABILITY_SEC):
     - Запустить metadata pipeline
     - Принять решение по дублям
     - Если register → POST /api/ingest/incoming/register, UPDATE status=registered
     - Если review → переименовать файл, UPDATE status=review_*
```

### Metadata pipeline

```
1. guessit(filename) → title, year, release_type (WEBRip|CAM|TS|etc.)
2. TMDB search(title, year) → tmdb_id, canonical_title, imdb_id, poster_url
   - При ошибке TMDB: продолжить без tmdb_id (fallback на guessit)
3. normalized_name:
   - С TMDB:    doctor_bakshi_2023_[881935]
   - Без TMDB:  doctor_bakshi_2023
4. quality_label из release_type:
   - WEBRip / BluRay / WEB-DL → 'HD'
   - CAM / TS / TC / Screener → 'SD'
   - Иначе → NULL
5. ffprobe(file) → resolution, hdr, codec, bitrate → quality_score
```

### Quality scoring (детерминированный)

```
resolution_score: 2160p=60, 1440p=45, 1080p=35, 720p=20, SD=10
hdr_score:        DolbyVision=15, HDR10/HDR10+=10, SDR=0
codec_score:      AV1=10, HEVC=8, H264=5, other=2
bitrate_score:    0..15 (линейно в рамках resolution tier)

quality_score = resolution_score + hdr_score + codec_score + bitrate_score
```

### Duplicate detection

```
1. Поиск в scanner_library_movies по tmdb_id (приоритет) или normalized_name
2. Нет совпадения → register
3. Совпадение найдено:
   - ffprobe успешен И new_score >= existing_score + 8:
     → is_upgrade_candidate=true, register
   - ffprobe успешен И разница < 8:
     → status=review_duplicate
     → rename: REVIEW_DUPLICATE_{normalized_name}_{timestamp}{ext}
   - ffprobe провалился:
     → status=review_unknown_quality
     → rename: REVIEW_UNKNOWN_{normalized_name}_{timestamp}{ext}
```

### poll_loop

Опрашивает converter API для всех items находящихся в активной обработке (`registered`, `claimed`, `copying`, `copied`).

```
каждые POLL_INTERVAL_SEC секунд:
  1. SELECT все items WHERE status IN ('registered','claimed','copying','copied')
     AND api_item_id IS NOT NULL
  2. Для каждого: GET /api/ingest/incoming/{api_item_id} на converter API
     Примечание: response содержит только { id, status, error_message } —
     всё остальное (source_path, normalized_name) берётся из локальной DB
  3. Маппинг converter статуса → local статус:
     - converter 'new'|'claimed'  → local 'claimed'
     - converter 'copying'        → local 'copying'
     - converter 'copied'         → local 'copied'
     - converter 'completed'      → local 'completed'
                                  → положить item_id в move_queue
     - converter 'failed'         → local 'failed', error_message
  4. UPDATE scanner_incoming_items SET status=..., updated_at=NOW()
```

### move_worker

```
бесконечный цикл, блокируется на move_queue.get():
  1. Получить item из queue
  2. Сформировать target_dir: LIBRARY_DIR/movies/{NormalizedName}/
  3. os.makedirs(target_dir, exist_ok=True)
  4. os.rename(source_path, target_dir/filename)
  5. UPSERT в scanner_library_movies
  6. UPDATE scanner_incoming_items SET status='archived', library_relative_path=...
  7. При ошибке rename: UPDATE status='failed', review_reason='move_failed'
```

---

## Converter API Dependency

Scanner требует один новый endpoint на converter API (не реализован в текущей версии):

```
GET /api/ingest/incoming/{id}
Auth: ServiceToken
Response: { "id": 1, "status": "completed", "error_message": null }
```

Этот endpoint необходимо реализовать как часть Block 1 работы (маленькое добавление к существующему `api/internal/handler/ingest.go`).

---

## Configuration

Все параметры через env vars:

```bash
# Пути
INCOMING_DIR=/mnt/storage/incoming
LIBRARY_DIR=/mnt/storage/library

# Converter API
CONVERTER_API_URL=http://converter-server:8000
CONVERTER_SERVICE_TOKEN=changeme

# TMDB
TMDB_API_KEY=changeme

# База данных
DATABASE_URL=postgresql://scanner:scanner@postgres:5432/scanner

# Тюнинг
SCAN_INTERVAL_SEC=30
POLL_INTERVAL_SEC=60
STABILITY_SEC=30
```

---

## Deployment

**docker-compose.yml** — два сервиса:
- `postgres:16-alpine` — данные в named volume
- `scanner` — собственный образ, bind mounts для `INCOMING_DIR` и `LIBRARY_DIR`

**Dockerfile:** `python:3.12-slim` + `ffmpeg` (для ffprobe) + зависимости через pip.

**Зависимости:**
- `psycopg2-binary` — PostgreSQL
- `guessit` — парсинг имён файлов
- `requests` — HTTP клиент
- `pytest` — тесты

---

## Testing

| Тест | Что проверяет |
|---|---|
| `test_stability.py` | stability detection: файл без изменений ≥30с → stable |
| `test_metadata.py` | GuessIt парсинг, TMDB fallback без ключа, normalized_name генерация |
| `test_quality.py` | quality_score расчёт для разных resolution/codec/HDR комбинаций |
| `test_duplicates.py` | upgrade проходит (delta ≥8), duplicate блокируется, unknown_quality блокируется |
| `test_converter_client.py` | register/claim-poll/fail через mock HTTP — критический путь API интеграции |

Все unit-тесты — без DB и без внешних HTTP запросов (mock TMDB + mock ffprobe + mock converter API).

---

## Out of Scope

- Series/episodes (только `movie` на текущем этапе)
- Web UI для scanner (управление через DB напрямую или через converter frontend)
- Retry логика для TMDB rate limiting — hardcoded `time.sleep(0.5)` между запросами в `metadata.py` (не конфигурируется)
- Heartbeat endpoint (`POST /api/ingest/incoming/heartbeat`) — не используется при polling модели
