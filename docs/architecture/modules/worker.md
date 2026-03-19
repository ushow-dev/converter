# Модуль: Worker Service (Go)

## Назначение

Асинхронно выполняет тяжёлые задачи: скачивание исходных файлов через qBittorrent, конвертацию в HLS, перенос на удалённый сервер, а также копирование файлов от ingest worker.

---

## Архитектура

Worker — Go-сервис (`app/worker`). Запускает несколько горутин, каждая обслуживает свою очередь через Redis BLPOP.

```
worker/
├── cmd/worker/main.go          # точка входа, регистрирует горутины
└── internal/
    ├── downloader/             # download_queue → qBittorrent → convert_queue
    ├── converter/              # convert_queue → ffmpeg HLS → completed
    ├── transfer/               # transfer_queue → rclone move → remote
    ├── ingest/                 # ingest worker: claim → rclone copy → complete
    │   ├── worker.go           # poll loop + processItem
    │   ├── client.go           # HTTP клиент к scanner API
    │   └── puller.go           # rclone copy по SFTP
    ├── ffmpeg/                 # ffmpeg profiles, progress parsing
    ├── qbittorrent/            # HTTP клиент qBittorrent
    └── repository/             # прямые запросы к PostgreSQL
```

---

## Горутины и очереди

| Горутина | Источник | Что делает |
|---|---|---|
| `Downloader` | `download_queue` (BLPOP) | Скачивает торрент через qBittorrent, перекладывает в `convert_queue` |
| `Converter` | `convert_queue` (BLPOP) | Запускает ffmpeg HLS, обновляет asset, опционально перекладывает в `transfer_queue` |
| `TransferWorker` | `transfer_queue` (BLPOP) | Переносит HLS-файлы на удалённый сервер через rclone |
| `IngestWorker` | polling scanner API | Забирает (claim) ingest items из scanner, копирует файлы через rclone SFTP, создаёт media_job локально и сообщает scanner о завершении |

---

## Download → Convert поток

```
POST /api/admin/jobs (или torrent)
  → download_queue (Redis RPUSH)
  → Downloader: qBittorrent добавляет торрент, ждёт скачивания
  → convert_queue (Redis RPUSH)
  → Converter: ffmpeg HLS (360/480/720p)
  → media_assets INSERT (is_ready=true)
  → media_jobs UPDATE status=completed
  → transfer_queue (если RCLONE_REMOTE задан)
```

Upload (прямая загрузка файла через `/api/admin/jobs/upload`) минует `download_queue` и попадает сразу в `convert_queue`.

Remote download (`/api/admin/jobs/remote-download`) использует отдельную `remote_download_queue`.

---

## Ingest поток (Block 2)

Ingest Worker обслуживает файлы, зарегистрированные Python scanner-сервисом:

```
scanner scan_loop → INSERT scanner_incoming_items (status=registered)
  → IngestWorker (polling, каждые 10с):
      POST /api/v1/incoming/claim → [item]  (scanner API)
      POST /api/v1/incoming/{id}/progress (status=copying)
      rclone copy (SFTP: storage-server → /media/downloads/ingest_{id}/)
      POST /api/v1/incoming/{id}/progress (status=copied)
      CREATE media_job локально (idempotent via request_id)
      RPUSH convert_queue (ConvertPayload)
      POST /api/v1/incoming/{id}/complete  (scanner API)
  → Converter: ffmpeg HLS как обычно
  → scanner move_worker: os.rename → library/movies/
```

Ingest Worker использует HTTP-клиент к scanner API с заголовком `X-Service-Token`.

---

## FFmpeg HLS-конвертация

Один проход ffmpeg создаёт три варианта (360/480/720p) через `filter_complex`. Подробнее — в [pipeline.md](../converter/pipeline.md).

Thumbnail: сначала пробует TMDB backdrop, при неудаче — извлекает кадр через ffmpeg.

---

## Transfer (перенос на удалённый сервер)

После конвертации, если задан `RCLONE_REMOTE`:

```
transfer_queue → TransferWorker → rclone move → remote:/storage/movies/<Title (Year)>/
  → movies.storage_location_id обновлён
  → плеер использует base_url из storage_locations
```

---

## Контракты очередей

**`DownloadPayload`** (`api/internal/model/`, `worker/internal/model/`):
```json
{ "job_id": "job_abc123", "source_ref": "...", "title": "...", "tmdb_id": "...", "request_id": "..." }
```

**`ConvertPayload`** (`api/internal/model/`, `worker/internal/model/`):
```json
{ "job_id": "job_abc123", "source_path": "/media/downloads/job_abc123/...", "title": "...", "tmdb_id": "...", "movie_storage_key": "Title (Year)" }
```

Форматы payload в `api/` и `worker/` должны оставаться синхронизированными — см. [contracts/worker.md](../../contracts/worker.md).

---

## Отказоустойчивость

- Redis NX-лок на `job_id` предотвращает параллельную обработку одной задачи
- `request_id` UNIQUE в БД — идемпотентность при повторных POST
- Exponential backoff при ошибках (5s → 10s → ... → 5m)
- Ingest Worker: при падении rclone вызывает `POST /api/v1/incoming/{id}/fail` к scanner
- При рестарте незавершённые задачи остаются в очереди (BLPOP не destructive до ACK)

---

## Конфигурация

| Переменная | Default | Описание |
|---|---|---|
| `DOWNLOAD_CONCURRENCY` | `2` | Параллельные загрузки торрентов |
| `CONVERT_CONCURRENCY` | `1` | Параллельные FFmpeg конвертации |
| `HTTP_DOWNLOAD_CONCURRENCY` | `3` | Параллельные HTTP-загрузки |
| `TRANSFER_CONCURRENCY` | `1` | Параллельных rclone transfer операций |
| `INGEST_CONCURRENCY` | `3` | Параллельных ingest горутин |
| `FFMPEG_THREADS` | `0` (auto) | Потоков FFmpeg на задание |
| `MEDIA_ROOT` | `/media` | Корень media-тома |
| `TMDB_API_KEY` | — | Для метаданных и постеров |
| `OPENSUBTITLES_API_KEY` | — | Для авто-субтитров (если не задан — отключено) |
| `SUBTITLE_LANGUAGES` | `en,bn,hi` | Языки субтитров через запятую |
| `RCLONE_REMOTE` | — | Имя rclone remote для переноса; если не задан — перенос отключён |
| `RCLONE_REMOTE_PATH` | `/storage` | Базовый путь на remote |
| `SCANNER_API_URL` | `http://scanner:8080` | URL scanner HTTP API для IngestWorker |
| `INGEST_SERVICE_TOKEN` | — | X-Service-Token для scanner API (если не задан — ingest отключён) |
| `INGEST_CLAIM_TTL_SEC` | `900` | TTL lease на ingest item (15 мин) |
| `INGEST_MAX_ATTEMPTS` | `3` | Макс. попыток до permanent failure |
| `INGEST_SOURCE_REMOTE` | `scanremote` | rclone remote для SFTP-доступа к серверу сканера |
| `INGEST_SOURCE_BASE_PATH` | `/incoming` | Путь на сервере сканера (symlink → `/mnt/storage/incoming`) |
| `RCLONE_CONFIG_SCANREMOTE_TYPE` | `sftp` | Тип rclone remote для сервера сканера |
| `RCLONE_CONFIG_SCANREMOTE_HOST` | — | IP/hostname сервера сканера |
| `RCLONE_CONFIG_SCANREMOTE_USER` | `root` | SSH-пользователь на сервере сканера |
| `RCLONE_CONFIG_SCANREMOTE_KEY_FILE` | `/secrets/scanner_rclone` | Путь к SSH-ключу внутри контейнера |
