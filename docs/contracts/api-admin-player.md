# API Contracts: Admin and Player

Версия контракта: `v1`  
Scope: `movie` pipeline only (series-ready fields присутствуют только как extension points).

## Общие правила

- Формат: `application/json; charset=utf-8`.
- Auth:
  - `Admin API` — JWT (`Authorization: Bearer <token>`).
  - `Player API` — service token или внутренний ключ (`X-Player-Key`), по env-конфигурации.
- Correlation:
  - Входящий заголовок: `X-Correlation-Id` (опционально).
  - Если не передан, API генерирует и возвращает его в ответе.
- Время: ISO-8601 UTC (`2026-03-01T10:00:00Z`).

## Error Model (единый)

```json
{
  "error": {
    "code": "INDEXER_UNAVAILABLE",
    "message": "Search backend is temporarily unavailable",
    "retryable": true,
    "correlation_id": "1f4d52f3-8b0f-4f98-a9c9-2a406f7eb3ed"
  }
}
```

Коды:

- `VALIDATION_ERROR` (`400`)
- `UNAUTHORIZED` (`401`)
- `FORBIDDEN` (`403`)
- `NOT_FOUND` (`404`)
- `CONFLICT` (`409`)
- `RATE_LIMITED` (`429`)
- `INDEXER_UNAVAILABLE` (`503`)
- `INTERNAL_ERROR` (`500`)

## Admin API

### `POST /api/admin/auth/login`

Назначение: получить JWT для admin UI.

Request:

```json
{
  "email": "admin@example.com",
  "password": "secret"
}
```

Response `200`:

```json
{
  "access_token": "jwt-token",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

### `GET /api/admin/search`

Назначение: поиск релизов через `IndexerProvider` (backend `Prowlarr`).

Query params:

- `query` (string, required)
- `content_type` (enum: `movie`, required)
- `limit` (int, optional, default `50`, max `100`)

Response `200`:

```json
{
  "items": [
    {
      "external_id": "prowlarr:idx:abc123",
      "title": "Movie.2025.1080p.WEB-DL",
      "source_type": "torrent",
      "source_ref": "magnet:?xt=urn:btih:...",
      "size_bytes": 7340032000,
      "seeders": 124,
      "leechers": 12,
      "indexer": "SomeIndexer",
      "content_type": "movie"
    }
  ],
  "total": 1,
  "correlation_id": "c8d78757-476c-4972-b3e7-94eb7df677c6"
}
```

### `POST /api/admin/jobs`

Назначение: создать задачу скачивания/обработки.

Request:

```json
{
  "request_id": "7c7f7f1a-09cc-4f6e-ae9f-a8e0e23cc1b3",
  "content_type": "movie",
  "source_type": "torrent",
  "source_ref": "magnet:?xt=urn:btih:...",
  "priority": "normal"
}
```

Response `202`:

```json
{
  "job_id": "job_01HZYJ9A",
  "status": "queued",
  "created_at": "2026-03-01T10:00:00Z"
}
```

### `GET /api/admin/jobs/{job_id}`

Response `200`:

```json
{
  "job_id": "job_01HZYJ9A",
  "content_type": "movie",
  "status": "in_progress",
  "stage": "download",
  "progress_percent": 42,
  "error_code": null,
  "error_message": null,
  "updated_at": "2026-03-01T10:05:00Z"
}
```

### `GET /api/admin/jobs`

Query params:

- `status` (optional)
- `limit` (optional, default `50`)
- `cursor` (optional)

Response `200`: список объектов как в `GET /api/admin/jobs/{job_id}` + `next_cursor`.

## Player API

### `GET /api/player/assets/{asset_id}`

Назначение: получить данные готового артефакта для воспроизведения.

Response `200`:

```json
{
  "asset_id": "asset_01HZYK3P",
  "job_id": "job_01HZYJ9A",
  "content_type": "movie",
  "is_ready": true,
  "playback": {
    "mode": "url",
    "url": "https://media.local/stream/asset_01HZYK3P.m3u8"
  },
  "media_info": {
    "duration_sec": 7320,
    "video_codec": "h264",
    "audio_codec": "aac"
  },
  "updated_at": "2026-03-01T10:15:00Z"
}
```

### `GET /api/player/jobs/{job_id}/status`

Назначение: lightweight endpoint для проверки готовности.

Response `200`:

```json
{
  "job_id": "job_01HZYJ9A",
  "status": "completed",
  "is_ready": true,
  "asset_id": "asset_01HZYK3P",
  "updated_at": "2026-03-01T10:15:00Z"
}
```

## Критерии совместимости v1

- Изменение обязательных полей — только через новую версию контракта.
- Добавление необязательных полей допускается без изменения версии.
- `content_type` обязателен везде для future compatibility с `series`.
