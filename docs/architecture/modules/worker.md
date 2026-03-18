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
{ "job_id": 1, "source_ref": "...", "title": "...", "tmdb_id": "...", "request_id": "..." }
```

**`ConvertPayload`** (`api/internal/model/`, `worker/internal/model/`):
```json
{ "job_id": 1, "input_path": "/media/downloads/1/...", "title": "...", "tmdb_id": "...", "request_id": "..." }
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

| Переменная | Описание |
|---|---|
| `DOWNLOAD_CONCURRENCY` | Параллельные загрузки (default: 1) |
| `CONVERT_CONCURRENCY` | Параллельные конвертации (default: 1) |
| `TMDB_API_KEY` | Для метаданных и постеров |
| `OPENSUBTITLES_API_KEY` | Для авто-субтитров |
| `RCLONE_REMOTE` | Имя rclone remote для переноса (`myserver:`); если не задан — перенос отключён |
| `SCANNER_API_URL` | URL scanner HTTP API для IngestWorker |
| `INGEST_SERVICE_TOKEN` | Токен для обращений к scanner `/api/v1/incoming/*` |
| `INGEST_CLAIM_TTL_SEC` | TTL lease на ingest item (default: 3600) |
| `MEDIA_ROOT` | Корень media-тома (`/media`) |
| `SFTP_HOST`, `SFTP_USER`, `SFTP_KEY_PATH` | SFTP-доступ к storage-серверу для rclone copy |
