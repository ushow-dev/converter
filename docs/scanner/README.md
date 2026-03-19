# Scanner Service

Автономный Python-сервис, развёртываемый на storage-сервере. Автоматически индексирует папку `incoming/`, нормализует имена файлов, проверяет дубликаты, регистрирует файлы в собственной БД и предоставляет HTTP API для Go IngestWorker. После завершения копирования перемещает файлы в `library/movies/`.

Исходный код: `scanner/`. Разворачивается отдельно от основного стека `converter` через `scanner/docker-compose.yml`.

---

## Архитектура

Один долгоживущий Python-процесс с тремя daemon-потоками. Общее состояние — PostgreSQL. Коммуникация между потоками — через БД и `queue.Queue`.

```
                       ┌─────────────────────────────────────────┐
                       │           Scanner Process               │
                       │                                         │
  incoming/ ──────────▶│  scan_loop          api_server          │◀── IngestWorker (Go)
                       │  (каждые 30с)       (FastAPI :8080)     │
                       │      │                   │              │
                       │      ▼                   ▼              │
                       │  PostgreSQL ◀──────── move_queue        │
                       │                           │             │
  library/ ◀───────────│               move_worker              │
                       │               (os.rename)               │
                       └─────────────────────────────────────────┘
```

### Потоки

| Поток | Режим | Ответственность |
|---|---|---|
| `scan_loop` | polling, 30с | Обход `incoming/`, stability check, metadata pipeline, дубликаты, регистрация в DB |
| `api_server` | event-driven (FastAPI + Uvicorn, порт `SCANNER_API_PORT`) | HTTP API для IngestWorker: claim / progress / complete / fail |
| `move_worker` | event-driven (queue.Queue) | `os.rename()` из `incoming/` в `library/movies/`, upsert в `scanner_library_movies` |

Взаимодействие:
- `scan_loop` → `api_server`: через таблицу `scanner_incoming_items` (статус `registered`)
- `api_server` → `move_worker`: через `queue.Queue` при вызове `/complete`

---

## Структура репозитория

```
scanner/
├── docker-compose.yml          # postgres:16-alpine + scanner (python:3.12-slim + ffmpeg)
├── .env.example
├── Dockerfile
├── pyproject.toml
└── scanner/
    ├── main.py                 # точка входа, запускает 3 потока + signal handling
    ├── config.py               # frozen dataclass, все env vars
    ├── db.py                   # ThreadedConnectionPool, авто-миграции
    ├── migrations/
    │   ├── 001_initial.sql     # scanner_incoming_items + scanner_library_movies
    │   └── 002_add_claim_columns.sql  # claimed_at / claim_expires_at (TTL claim)
    ├── loops/
    │   ├── scan_loop.py        # сканирует incoming/ каждые SCAN_INTERVAL_SEC
    │   └── move_worker.py      # os.rename → library/movies/{normalized_name}/
    ├── services/
    │   ├── stability.py        # проверка стабильности файла (размер не меняется)
    │   ├── metadata.py         # GuessIt + TMDB lookup + normalized_name
    │   ├── quality.py          # ffprobe → quality_score (0..100)
    │   └── duplicates.py       # решение: register / review_duplicate / review_unknown_quality
    ├── api/
    │   └── server.py           # FastAPI app factory + uvicorn.Server
    └── tests/
        ├── test_stability.py
        ├── test_quality.py
        ├── test_metadata.py
        ├── test_duplicates.py
        └── test_scanner_api.py
```

---

## Жизненный цикл файла

### Граф статусов

```
new
 ├──▶ registered      (стабильный файл прошёл metadata + duplicate check)
 │       └──▶ claimed (IngestWorker вызвал /claim)
 │               ├──▶ copying   (/progress status=copying)
 │               │       └──▶ copied    (/progress status=copied)
 │               │               └──▶ completed (/complete)
 │               │                       └──▶ archived  (move_worker переместил файл)
 │               └──▶ failed    (/fail или claim TTL истёк)
 ├──▶ review_duplicate       (дубль, файл переименован в REVIEW_DUPLICATE_...)
 ├──▶ review_unknown_quality (ffprobe не сработал + существует в library)
 └──▶ skipped                (неподдерживаемое расширение)
```

### Последовательность обработки

```
1. scan_loop обнаруживает видеофайл в incoming/
   → INSERT scanner_incoming_items (status=new)

2. scan_loop проверяет стабильность:
   → если размер файла не менялся ≥ STABILITY_SEC секунд — файл стабилен

3. scan_loop запускает metadata pipeline:
   a. guessit(filename)           → title, year, release_type
   b. TMDB search(title, year)    → tmdb_id, canonical_title
   c. build_normalized_name()     → "doctor_strange_2022_[614479]"
   d. ffprobe(file_path)          → quality_score (0..100)

4. Проверка дублей:
   a. Нет в scanner_library_movies по tmdb_id / normalized_name
      → action=register
   b. Есть, new_score ≥ existing_score + 8
      → action=register (upgrade candidate)
   c. Есть, разница < 8
      → action=review_duplicate (переименовать файл в incoming/)
   d. ffprobe провалился + есть в library
      → action=review_unknown_quality

5. При action=register:
   → UPDATE status=registered

6. IngestWorker (Go) вызывает POST /api/v1/incoming/claim
   → атомарный SELECT ... FOR UPDATE SKIP LOCKED
   → status=claimed, claimed_at=NOW(), claim_expires_at=NOW()+TTL

7. IngestWorker: rclone copy файла на converter-сервер
   → POST /progress (status=copying)
   → rclone copy ...
   → POST /progress (status=copied)

8. IngestWorker завершает обработку:
   → POST /complete → status=completed, item попадает в move_queue

9. move_worker выполняет os.rename:
   → incoming/{filename} → library/movies/{normalized_name}/{filename}
   → UPSERT scanner_library_movies (status=ready)
   → UPDATE scanner_incoming_items (status=archived)
```

---

## Сервисные компоненты

### stability.py

Чистые функции без I/O:

| Функция | Описание |
|---|---|
| `is_stable(current_size, last_seen_size, stable_since, now, stability_sec)` | `True` если размер не менялся ≥ `stability_sec` секунд |
| `update_stability(current_size, last_seen_size, stable_since, now)` | Возвращает `{file_size_bytes, stable_since}` с обновлёнными полями |

### quality.py

| Функция | Описание |
|---|---|
| `compute_quality_score(width, height, hdr, codec, bitrate_kbps)` | Детерминированный score 0..100 |
| `parse_ffprobe_output(json_str)` | Парсит JSON-вывод ffprobe, определяет HDR |
| `ffprobe_quality(file_path)` | Запускает subprocess ffprobe, возвращает `dict` или `None` |

Компоненты quality_score:

| Компонент | Значения |
|---|---|
| resolution | 2160p=60, 1440p=45, 1080p=35, 720p=20, SD=10 |
| hdr | DolbyVision=15, HDR10/HDR10+=10, HLG=5, SDR=0 |
| codec | AV1=10, HEVC=8, H264=5, other=2 |
| bitrate | 0..15 (линейно в рамках resolution tier) |

### metadata.py

| Функция | Описание |
|---|---|
| `parse_filename(filename)` | guessit → `{title, year, release_type}` |
| `build_normalized_name(title, year, tmdb_id)` | `"doctor_strange_2022_[614479]"` или `"doctor_strange_2022"` |
| `quality_label_from_release_type(release_type)` | WEBRip/BluRay/WEB-DL → `"HD"`; CAM/TS/TC → `"SD"`; иначе `None` |
| `tmdb_search(title, year, api_key)` | GET TMDB API → `{tmdb_id, title, imdb_id, poster_url}` или `None` |

### duplicates.py

| Функция | Описание |
|---|---|
| `decide_action(existing_score, new_score, ffprobe_ok)` | `"register"` / `"review_duplicate"` / `"review_unknown_quality"` |

Порог апгрейда: `UPGRADE_THRESHOLD = 8`. Если разница `new_score - existing_score ≥ 8` — регистрируется как upgrade candidate.

---

## Конфигурация

| Переменная | Обязательная | По умолчанию | Описание |
|---|---|---|---|
| `INCOMING_DIR` | ✓ | — | Абсолютный путь к папке входящих файлов |
| `LIBRARY_DIR` | ✓ | — | Абсолютный путь к медиатеке |
| `DATABASE_URL` | ✓ | — | PostgreSQL DSN (напр. `postgresql://user:pass@host:5432/db`) |
| `SERVICE_TOKEN` | ✓ | — | Секретный токен для X-Service-Token auth |
| `TMDB_API_KEY` | ✓ | — | API ключ TMDB (v3) |
| `SCANNER_API_PORT` | — | `8080` | Порт FastAPI HTTP сервера |
| `SCAN_INTERVAL_SEC` | — | `30` | Интервал сканирования incoming/ (секунды) |
| `STABILITY_SEC` | — | `30` | Время ожидания стабильности файла (секунды) |

---

## Поддерживаемые форматы

Видеофайлы с расширениями: `.mkv`, `.mp4`, `.avi`, `.mov`, `.ts`, `.m2ts`, `.wmv`

Текущие ограничения:
- Только `content_kind=movie`; сериалы не поддерживаются
- TMDB поиск: `sleep(0.5)` между запросами, retry не реализован
- Нет веб-интерфейса; управление через прямые запросы к DB
- Нет healthcheck endpoint

---

## Развёртывание

```bash
cd scanner/
cp .env.example .env
# заполнить переменные в .env
docker compose up -d
```

Сервис использует bind mounts. `incoming/` монтируется read-only, `library/` — read-write. `os.rename()` работает мгновенно, т.к. оба пути должны быть на одном диске (разные mount points потребуют `shutil.move`).

Миграции применяются автоматически при старте через `db.init()`.

---

## Тестирование

```bash
cd scanner/
pip install -e ".[dev]"
pytest tests/ -v
```

44 unit-теста без DB и без внешних HTTP запросов (mock TMDB + mock ffprobe):

| Файл | Что проверяет |
|---|---|
| `test_stability.py` | stable/unstable detection, update_stability |
| `test_quality.py` | quality_score для разных resolution/codec/HDR, parse_ffprobe_output |
| `test_metadata.py` | GuessIt парсинг, normalized_name, TMDB fallback |
| `test_duplicates.py` | upgrade проходит (delta≥8), дубль блокируется, unknown_quality |
| `test_scanner_api.py` | claim/progress/complete/fail через FastAPI TestClient |
