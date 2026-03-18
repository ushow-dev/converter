# Модуль: Scanner Service (Python)

## Назначение

Автономный Python-сервис на storage-сервере, который автоматически индексирует папку `incoming/`, нормализует имена файлов, определяет дубли, регистрирует файлы в собственной БД и предоставляет HTTP API для IngestWorker. Перемещает готовые файлы в `library/movies/` после завершения копирования.

Код живёт в `scanner/` и разворачивается независимо от основного стека converter.

---

## Архитектура

Долгоживущий Python-процесс с тремя независимыми потоками. Общее состояние — PostgreSQL. Потоки общаются через БД и in-process `queue.Queue`.

```
scanner/
├── docker-compose.yml          # postgres + scanner service
├── .env.example
├── Dockerfile                  # python:3.12-slim + ffmpeg
├── pyproject.toml
└── scanner/
    ├── main.py                 # точка входа, запускает 3 потока
    ├── config.py               # все env vars (frozen dataclass)
    ├── db.py                   # connection pool, авто-миграции
    ├── migrations/
    │   ├── 001_initial.sql     # scanner_incoming_items + scanner_library_movies
    │   └── 002_add_claim_columns.sql # claimed_at/claim_expires_at для TTL claims
    ├── loops/
    │   ├── scan_loop.py        # сканирует incoming/ каждые 30с
    │   └── move_worker.py      # os.rename() в library/
    ├── services/
    │   ├── stability.py        # проверка стабильности файла
    │   ├── metadata.py         # GuessIt + TMDB lookup + normalized_name
    │   ├── quality.py          # ffprobe + quality_score
    │   └── duplicates.py       # логика дублей и апгрейдов
    └── api/
        └── server.py           # Flask HTTP API для IngestWorker (/api/v1/incoming/*)
```

### Потоки

| Поток | Интервал | Ответственность |
|---|---|---|
| `scan_loop` | 30с | Обход `incoming/`, stability check, metadata pipeline, регистрация в scanner DB |
| `api_server` | HTTP (Flask, порт SCANNER_API_PORT) | Принимает claim/progress/complete/fail от IngestWorker |
| `move_worker` | event-driven | `os.rename()` в `library/` + upsert в scanner_library_movies |

Потоки не делят состояние напрямую: `scan_loop` → `api_server` через PostgreSQL, `api_server` → `move_worker` через `queue.Queue`.

---

## Жизненный цикл файла

### Статусы

```
new → registered → claimed → copying → copied → completed → archived
new → review_duplicate      (полный дубль, файл переименован)
new → review_unknown_quality (ffprobe не сработал, есть existing)
new → skipped               (неподдерживаемый тип файла)
copying → failed
claimed → failed            (lease expired)
```

### Поток обработки

```
1. scan_loop обнаруживает файл → INSERT status=new
2. scan_loop ждёт стабильности (размер не меняется ≥ STABILITY_SEC)
3. scan_loop запускает metadata pipeline:
   a. guessit → title, year, release_type
   b. TMDB search → tmdb_id, canonical_title, poster_url
   c. Построить normalized_name (doctor_bakshi_2023_[881935])
   d. quality_label из release_type (HD/SD/NULL)
   e. ffprobe → quality_score (0..100)
4. Duplicate detection:
   - Нет совпадения в library → register
   - new_score ≥ existing_score + 8 → register (upgrade candidate)
   - Разница < 8 → review_duplicate (переименовать файл)
   - ffprobe провалился + есть existing → review_unknown_quality
5. INSERT scanner_incoming_items, status=registered (локально в scanner DB)
6. IngestWorker вызывает POST /api/v1/incoming/claim → получает item
7. IngestWorker: progress (copying) → rclone copy → progress (copied) → complete
8. При status=completed → move_queue
9. move_worker: os.rename(incoming/..., library/movies/{normalized_name}/)
10. UPSERT scanner_library_movies, status=archived
```

---

## Сервисные слои

### stability.py

Чистые функции без IO:

- `is_stable(current_size, last_seen_size, stable_since, now, stability_sec)` → `bool`
  Возвращает `True` если размер файла не менялся не менее `stability_sec` секунд.
- `update_stability(current_size, last_seen_size, stable_since, now)` → `dict`
  Возвращает обновлённые поля `{file_size_bytes, stable_since}`.

### quality.py

- `compute_quality_score(width, height, hdr, codec, bitrate_kbps)` → `int`
  Детерминированный score: `resolution_score + hdr_score + codec_score + bitrate_score`.

| Компонент | Значения |
|---|---|
| resolution_score | 2160p=60, 1440p=45, 1080p=35, 720p=20, SD=10 |
| hdr_score | DolbyVision=15, HDR10/HDR10+=10, HLG=5, SDR=0 |
| codec_score | AV1=10, HEVC=8, H264=5, other=2 |
| bitrate_score | 0..15 (линейно в рамках resolution tier) |

- `parse_ffprobe_output(json_str)` → `dict | None`
  Парсит вывод ffprobe, определяет HDR по `color_transfer` и `side_data_list`.
- `ffprobe_quality(file_path)` → `dict | None`
  Запускает `ffprobe` subprocess, возвращает `{codec, width, height, hdr, bitrate_kbps, quality_score}` или `None` при ошибке.

### metadata.py

- `parse_filename(filename)` → `{title, year, release_type}`
  Использует `guessit`.
- `build_normalized_name(title, year, tmdb_id)` → `str`
  Пример: `doctor_bakshi_2023_[881935]` или `doctor_bakshi_2023`.
- `quality_label_from_release_type(release_type)` → `"HD" | "SD" | None`
  WEBRip/BluRay/WEB-DL → HD; CAM/TS/TC/Screener → SD; иначе → None.
- `tmdb_search(title, year, api_key)` → `dict | None`
  Запрос к TMDB API. Возвращает `{tmdb_id, title, imdb_id, poster_url}` или `None`.
  Hardcoded `time.sleep(0.5)` после каждого запроса для соблюдения rate limit.

### duplicates.py

- `decide_action(existing_score, new_score, ffprobe_ok)` → `"register" | "review_duplicate" | "review_unknown_quality"`
  `UPGRADE_THRESHOLD = 8` — минимальная разница в очках для апгрейда.

### api/server.py

Flask HTTP API, принимает запросы от IngestWorker:

- `POST /api/v1/incoming/claim` — атомарно забирает доступные items (FOR UPDATE SKIP LOCKED), устанавливает `claimed_at`/`claim_expires_at`
- `POST /api/v1/incoming/<id>/progress` — обновляет статус (`copying`/`copied`) и прогресс
- `POST /api/v1/incoming/<id>/complete` — помечает item как `completed`, кладёт в move_queue
- `POST /api/v1/incoming/<id>/fail` — фиксирует ошибку, сбрасывает в `new` при `attempts < max_attempts`

Все endpoints защищены заголовком `X-Service-Token` (проверяется против `SERVICE_TOKEN`).

---

## База данных

Два основных хранилища (PostgreSQL):

**`scanner_incoming_items`** — операционная очередь файлов из `incoming/`.
Ключевые поля: `source_path`, `status`, `quality_score`, `api_item_id`, `normalized_name`, `tmdb_id`, `duplicate_of_library_movie_id`.

**`scanner_library_movies`** — каталог готовых фильмов в `library/`.
Ключевые поля: `normalized_name` (UNIQUE), `tmdb_id` (UNIQUE WHERE NOT NULL), `quality_score`, `library_relative_path`, `status` (ready/replaced/deprecated).

Миграции применяются автоматически при старте через `db.init()`.

---

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `INCOMING_DIR` | — | Путь к папке входящих файлов |
| `LIBRARY_DIR` | — | Путь к медиатеке |
| `SERVICE_TOKEN` | — | Токен авторизации (X-Service-Token), проверяется scanner |
| `SCANNER_API_PORT` | — | Порт Flask HTTP API сервера |
| `TMDB_API_KEY` | — | Ключ TMDB API |
| `DATABASE_URL` | — | DSN PostgreSQL |
| `SCAN_INTERVAL_SEC` | 30 | Интервал сканирования incoming/ |
| `STABILITY_SEC` | 30 | Время ожидания стабильности файла |

---

## Развёртывание

```bash
cd scanner/
cp .env.example .env
# Заполнить переменные в .env
docker compose up -d
```

Сервис использует bind mounts для доступа к файловой системе хоста. `incoming/` монтируется read-only, `library/` — read-write. Перемещение файлов осуществляется через `os.rename()` — работает мгновенно, т.к. `incoming/` и `library/` находятся на одном диске.

---

## Тестирование

40 unit-тестов без DB и без внешних HTTP запросов (mock TMDB + mock ffprobe + mock converter API):

```bash
cd scanner/
PYTHONPATH=. python3 -m pytest tests/ -v
```

| Файл | Что проверяет |
|---|---|
| `tests/test_stability.py` | Stability detection: stable/unstable, update_stability |
| `tests/test_quality.py` | quality_score для разных resolution/codec/HDR, parse_ffprobe_output |
| `tests/test_metadata.py` | GuessIt парсинг, normalized_name, TMDB fallback |
| `tests/test_duplicates.py` | Upgrade проходит (delta≥8), дубль блокируется, unknown_quality |
| `tests/test_api_server.py` | claim/progress/complete/fail через mock HTTP |

---

## Ограничения текущей версии

- Только фильмы (`content_kind=movie`); сериалы не поддерживаются
- Нет веб-интерфейса для управления (контроль через DB напрямую)
- Retry для TMDB rate limiting не реализован — hardcoded `sleep(0.5)`
- Нет heartbeat endpoint — используется polling модель
