# Worker Queue Контракты

> **ВАЖНО:** Структуры payload в API (`api/internal/model/`) и Worker (`worker/internal/model/`) должны быть идентичны.
> При изменении любого payload обновляйте обе стороны одновременно.

## Очереди Redis

| Имя очереди | Продюсер | Консьюмер | Описание |
|---|---|---|---|
| `download_queue` | API `/api/admin/jobs` | Worker downloader | Торрент-загрузка |
| `convert_queue` | API (upload) / Worker (после download) | Worker converter | FFmpeg конвертация |
| `remote_download_queue` | API `/api/admin/jobs/remote-download` | Worker httpdownloader | HTTP-загрузка |
| `transfer_queue` | Worker converter (после HLS) | Worker transfer | Перенос HLS на удалённый сервер через rclone |

Механизм: `RPUSH` (producer) + `BLPOP` (consumer, blocking, timeout 5s).

---

## Payload схемы

### DownloadPayload (download_queue)

```json
{
  "job_id": "string",
  "magnet_link": "string",
  "title": "string",
  "year": "number (optional, 0 если не задан)",
  "imdb_id": "string (optional)",
  "tmdb_id": "number (optional)",
  "attempt": "number (текущая попытка, начиная с 1)"
}
```

**Поведение при ошибке:**
- Воркер повторяет до `max_attempts` (default: 5)
- При исчерпании попыток: `UPDATE media_jobs SET status=failed`
- Backoff: 500ms → 1s → 2s (exponential)

---

### ConvertPayload (convert_queue)

```json
{
  "job_id": "string",
  "source_path": "string (абсолютный путь к файлу, например /media/downloads/{jobID}/{file})",
  "title": "string",
  "year": "number (optional)",
  "imdb_id": "string (optional)",
  "tmdb_id": "number (optional)",
  "movie_storage_key": "string (например mov_80baaede8740795c, уникальный ключ хранения)"
}
```

**Действия воркера:**
1. `UPDATE media_jobs SET status=in_progress, stage=convert`
2. Запустить FFmpeg (360p/480p/720p HLS → `/media/converted/{movie_storage_key}/`)
3. Извлечь thumbnail
4. `INSERT INTO movies` (upsert по imdb_id+tmdb_id)
5. `INSERT INTO media_assets` (is_ready=true)
6. Авто-получить субтитры (если OpenSubtitles настроен)
7. Загрузить постер с TMDB
8. `UPDATE media_jobs SET status=completed`

**При ошибке:**
- `UPDATE media_jobs SET status=failed, error_message=...`
- Удалить временные файлы `/media/temp/{jobID}/`

---

### RemoteDownloadPayload (remote_download_queue)

```json
{
  "job_id": "string",
  "url": "string (HTTP/HTTPS URL файла)",
  "title": "string (optional)",
  "year": "number (optional)",
  "imdb_id": "string (optional)",
  "tmdb_id": "number (optional)",
  "proxy": {
    "type": "socks5|http (optional)",
    "address": "string (optional)"
  }
}
```

**Действия воркера:**
1. `UPDATE media_jobs SET status=in_progress, stage=download`
2. HTTP GET с optional proxy → `/media/downloads/{jobID}/`
3. На успех: `RPUSH convert_queue {ConvertPayload}`

---

### TransferMessage (transfer_queue)

Сообщение конверсионного воркера после успешной HLS-конвертации. Обрабатывается `TransferWorker`.

```json
{
  "schema_version": "string (версия схемы, например \"1\")",
  "job_id": "string",
  "correlation_id": "string (для трассировки, обычно совпадает с job_id)",
  "created_at": "time (RFC3339)",
  "payload": {
    "movie_id": "number (int64, ID фильма в таблице movies)",
    "storage_key": "string (папка фильма, формат \"Title (Year)\")",
    "local_path": "string (абсолютный путь к HLS-директории, например /media/converted/movies/Title (Year)/)"
  }
}
```

**Действия воркера:**
1. Запустить `rclone move {local_path} {RCLONE_REMOTE}/storage/movies/{storage_key}/`
2. `UPDATE movies SET storage_location_id = <remote_location_id> WHERE id = movie_id`
3. Плеер автоматически начнёт использовать `base_url` из `storage_locations`

**Если `RCLONE_REMOTE` не задан:**
- Сообщение не отправляется конвертером
- Файлы остаются в `/media/converted/movies/{storage_key}/`

---

## Ingest Worker

The ingest worker polls the API for newly registered incoming files and copies them to local disk before handing off to the conversion pipeline.

**Activation:** Gated on both `INGEST_SERVICE_TOKEN` and `INGEST_SOURCE_REMOTE` being non-empty. If either is unset, the ingest worker goroutines are not started.

**Poll interval:** 10 seconds (`BLPOP`-style, but HTTP-based — not Redis-driven).

**Concurrency:** Controlled by `INGEST_CONCURRENCY` (default: 1). Each goroutine independently claims and processes one item per tick.

### Claim–copy–complete cycle

1. Call `POST /api/ingest/incoming/claim` with `limit=1` and `claim_ttl_sec=INGEST_CLAIM_TTL_SEC`.
2. If no items returned, sleep and retry.
3. Call `POST /api/ingest/incoming/progress` → status `copying`.
4. Run `rclone copy {INGEST_SOURCE_REMOTE}:{INGEST_SOURCE_BASE_PATH}/{source_path} /media/downloads/ingest_{id}/`.
5. Call `POST /api/ingest/incoming/progress` → status `copied`, progress 100.
6. Call `POST /api/ingest/incoming/complete` with `local_path=/media/downloads/ingest_{id}/{source_filename}`.
   - The API creates the `media_job` and pushes `ConvertPayload` to `convert_queue`.
7. On any error: call `POST /api/ingest/incoming/fail` with the error message.

### Local storage layout

Copied files land in:
```
/media/downloads/ingest_{id}/{source_filename}
```

This path is passed to `complete` as `local_path`. The converter then treats it as a standard convert job.

### Required configuration

| Variable | Purpose |
|---|---|
| `INGEST_SERVICE_TOKEN` | Authenticates calls to `/api/ingest/*` |
| `INGEST_SOURCE_REMOTE` | rclone remote name (e.g. `mynas`) |
| `INGEST_SOURCE_BASE_PATH` | Base path on the remote (e.g. `incoming`) |
| `CONVERTER_API_URL` | Base URL of the API service (e.g. `http://api:8000`) |
| `INGEST_CONCURRENCY` | Number of parallel ingest goroutines (default: 1) |
| `INGEST_CLAIM_TTL_SEC` | Lease duration in seconds (default: 3600) |
| `INGEST_MAX_ATTEMPTS` | Max retry attempts before permanent failure (default: 3) |

### Lease expiry recovery

If the ingest worker crashes mid-copy, the item remains in `claiming` status with an expired lease. On the next `claim` call the API resets all expired leases back to `new`, making those items available for re-claim.

---

## Статусы задания (media_jobs.status)

```
created     — задание создано в БД, ещё не в очереди
queued      — добавлено в Redis очередь, ожидает воркера
in_progress — воркер обрабатывает (см. stage)
completed   — успешно завершено, asset создан
failed      — завершено с ошибкой
```

## Stages задания (media_jobs.stage)

```
download    — воркер скачивает файл (торрент или HTTP)
convert     — воркер запускает FFmpeg
transfer    — воркер переносит HLS на удалённый сервер через rclone
```

## Distributed Locking

Перед обработкой задания воркер устанавливает Redis lock:
```
SET job_lock:{job_id} 1 NX EX 3600
```

Если lock уже существует — задание пропускается (другой воркер обрабатывает).

---

## Изменение контрактов (процедура)

1. Определить, какая очередь затронута
2. Обновить struct в `api/internal/model/`
3. Обновить struct в `worker/internal/model/`
4. Проверить обратную совместимость (старые сообщения в очереди)
5. При breaking change — очистить очередь перед деплоем
6. Обновить этот файл
