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

IngestWorker обращается к **scanner API** (отдельный Python-сервис на storage-сервере), забирает зарегистрированные файлы и копирует их на converter-сервер для дальнейшей обработки.

**Activation:** Активируется только если оба параметра `INGEST_SERVICE_TOKEN` и `INGEST_SOURCE_REMOTE` непустые. При отсутствии любого из них горутины ingest не запускаются.

**Poll interval:** ~10 секунд (HTTP polling, не Redis-driven).

**Concurrency:** `INGEST_CONCURRENCY` (default: **3**). Каждая горутина независимо claim'ит и обрабатывает один item за тик.

### Claim–copy–complete cycle

1. `POST /api/v1/incoming/claim` → scanner API, `limit=1`, `claim_ttl_sec=INGEST_CLAIM_TTL_SEC`
2. Если items пустой — sleep и retry.
3. `POST /api/v1/incoming/{id}/progress` → `{"status": "copying"}`
4. `rclone copy {INGEST_SOURCE_REMOTE}:{INGEST_SOURCE_BASE_PATH}/{source_path} /media/downloads/ingest_{id}/`
5. `POST /api/v1/incoming/{id}/progress` → `{"status": "copied"}`
6. Создать `media_job` локально в converter DB (idempotent через `ON CONFLICT (request_id) DO NOTHING`)
7. `RPUSH convert_queue {ConvertPayload}` — поставить в очередь конвертации
8. `POST /api/v1/incoming/{id}/complete` → уведомить scanner об успехе
9. При любой ошибке: `POST /api/v1/incoming/{id}/fail` → `{"error_message": "..."}`

**Важно:** converter-воркер сам создаёт media_job и push'ит convert_queue (шаги 6–7), а не scanner и не converter API. Scanner только перемещает файл в library/ после вызова `/complete`.

### Local storage layout

Скопированные файлы попадают в:
```
/media/downloads/ingest_{id}/{source_filename}
```

### Required configuration

| Variable | Default | Purpose |
|---|---|---|
| `INGEST_SERVICE_TOKEN` | — | X-Service-Token для scanner API |
| `INGEST_SOURCE_REMOTE` | — | Имя rclone remote (напр. `mynas`) |
| `INGEST_SOURCE_BASE_PATH` | `/incoming` | Базовый путь на remote |
| `SCANNER_API_URL` | `http://scanner:8080` | URL scanner HTTP API |
| `INGEST_CONCURRENCY` | `3` | Параллельных горутин |
| `INGEST_CLAIM_TTL_SEC` | `900` | TTL lease в секундах (15 мин) |
| `INGEST_MAX_ATTEMPTS` | `3` | Макс. попыток до permanent failure |

### Lease expiry recovery

При падении воркера в mid-copy item остаётся в статусе `claimed` с истёкшим `claim_expires_at`. При следующем `/claim` scanner возвращает только items со статусом `registered` — истёкшие leases должны сбрасываться через отдельный background job (в текущей реализации не автоматизировано).

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
