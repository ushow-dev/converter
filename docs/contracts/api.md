# API Контракты

> **ВАЖНО:** Изменение этих контрактов требует одновременного обновления Frontend типов в `frontend/src/types/index.ts`.
> При изменениях обновляйте этот файл.

## Аутентификация

### Схема 1: Admin JWT
```
Header: Authorization: Bearer <token>
       OR
Query:  ?token=<token>
```
Применяется для всех `/api/admin/*` endpoints.

### Схема 2: Player API Key
```
Header: X-Player-Key: <PLAYER_API_KEY>
```
Применяется для всех `/api/player/*` endpoints.

---

## Admin Endpoints

### POST /api/admin/auth/login
**Назначение:** Аутентификация администратора

**Request:**
```json
{
  "email": "string",
  "password": "string"
}
```

**Response 200:**
```json
{
  "token": "string (JWT)"
}
```

---

### GET /api/admin/search
**Назначение:** Поиск торрентов через Prowlarr

**Query params:**
- `q` (required) — поисковый запрос

**Response 200:**
```json
{
  "results": [
    {
      "id": "string",
      "title": "string",
      "size": "number (bytes)",
      "seeders": "number",
      "leechers": "number",
      "magnet_link": "string",
      "indexer": "string",
      "published_at": "string (ISO 8601)"
    }
  ]
}
```

---

### POST /api/admin/jobs
**Назначение:** Создать задание на загрузку торрента

**Request:**
```json
{
  "request_id": "string (UUID, для идемпотентности)",
  "title": "string",
  "year": "number (optional)",
  "imdb_id": "string (optional, например tt1234567)",
  "tmdb_id": "number (optional)",
  "magnet_link": "string"
}
```

**Response 201:**
```json
{
  "job_id": "string",
  "status": "queued"
}
```

---

### POST /api/admin/jobs/upload
**Назначение:** Загрузить файл напрямую (multipart/form-data)
**Максимальный размер:** 50 ГБ

**Form fields:**
- `file` (required) — видео файл
- `title` (required) — название
- `year` (optional) — год
- `imdb_id` (optional)
- `tmdb_id` (optional)
- `request_id` (optional) — UUID для идемпотентности

**Response 201:**
```json
{
  "job_id": "string",
  "status": "queued"
}
```

---

### POST /api/admin/jobs/remote-download
**Назначение:** Загрузить файл по HTTP URL

**Request:**
```json
{
  "url": "string (HTTP/HTTPS URL)",
  "title": "string (optional, берётся из URL если не задан)",
  "year": "number (optional)",
  "imdb_id": "string (optional)",
  "tmdb_id": "number (optional)",
  "request_id": "string (optional)"
}
```

**Response 201:**
```json
{
  "job_id": "string",
  "status": "queued"
}
```

---

### GET /api/admin/jobs
**Назначение:** Список заданий с курсорной пагинацией

**Query params:**
- `cursor` (optional) — job_id последнего элемента предыдущей страницы
- `limit` (optional, default: 20)
- `status` (optional) — фильтр по статусу

**Response 200:**
```json
{
  "jobs": [
    {
      "job_id": "string",
      "status": "created|queued|in_progress|completed|failed",
      "stage": "download|convert (null если не начато)",
      "progress_percent": "number (0-100)",
      "title": "string",
      "created_at": "string (ISO 8601)",
      "updated_at": "string (ISO 8601)",
      "error_message": "string (null если нет ошибки)"
    }
  ],
  "next_cursor": "string (null если последняя страница)"
}
```

---

### GET /api/admin/jobs/{jobID}
**Response 200:**
```json
{
  "job_id": "string",
  "status": "string",
  "stage": "string",
  "progress_percent": "number",
  "title": "string",
  "imdb_id": "string",
  "tmdb_id": "number",
  "created_at": "string",
  "updated_at": "string",
  "error_message": "string",
  "asset": {
    "asset_id": "string",
    "hls_url": "string",
    "thumbnail_url": "string"
  }
}
```

---

### DELETE /api/admin/jobs/{jobID}
**Назначение:** Удалить задание и все связанные файлы на диске

**Response 204:** (no content)

---

### GET /api/admin/jobs/{jobID}/thumbnail
**Назначение:** Получить миниатюру задания
**Auth:** JWT в query param `?token=...` (для `<img src>` тегов)

**Response 200:** image/jpeg

---

### GET /api/admin/movies
**Query params:**
- `cursor` (optional)
- `limit` (optional, default: 20)

**Response 200:**
```json
{
  "movies": [
    {
      "id": "number",
      "title": "string",
      "year": "number",
      "imdb_id": "string",
      "tmdb_id": "number",
      "poster_url": "string",
      "created_at": "string"
    }
  ],
  "next_cursor": "string"
}
```

---

### PATCH /api/admin/movies/{movieId}
**Request:**
```json
{
  "imdb_id": "string (optional)",
  "tmdb_id": "number (optional)",
  "title": "string (optional)"
}
```

**Response 200:** обновлённый объект movie

---

### DELETE /api/admin/movies/{movieId}
**Response 204**

---

### GET /api/admin/movies/tmdb/{tmdbId}
**Response 200:** TMDB metadata объект

---

### GET /api/admin/movies/tmdb/search
**Query:** `q=string`
**Response 200:** список TMDB результатов

---

### GET /api/admin/movies/{movieId}/subtitles
**Response 200:**
```json
{
  "subtitles": [
    {
      "id": "number",
      "language": "string (ISO 639-1)",
      "source": "opensubtitles|upload",
      "created_at": "string"
    }
  ]
}
```

---

### POST /api/admin/movies/{movieId}/subtitles
**Назначение:** Ручная загрузка субтитров (multipart)

**Form fields:**
- `file` — .srt или .vtt файл
- `language` — ISO 639-1 код

**Response 201:** subtitle объект

---

### POST /api/admin/movies/{movieId}/subtitles/search
**Назначение:** Авто-поиск и загрузка через OpenSubtitles

**Request:**
```json
{
  "language": "string (ISO 639-1, например 'en')"
}
```

**Response 200:** subtitle объект

---

## Player Endpoints

### GET /api/player/movie
**Query params:** `imdb_id=tt...` OR `tmdb_id=number`

**Response 200:**
```json
{
  "movie_id": "number",
  "title": "string",
  "year": "number",
  "hls_url": "string (подписанный если MEDIA_SIGNING_KEY задан)",
  "thumbnail_url": "string",
  "subtitles": [
    {
      "language": "string",
      "url": "string"
    }
  ]
}
```

---

### GET /api/player/assets/{assetID}
**Response 200:** asset с HLS URL

---

### GET /api/player/jobs/{jobID}/status
**Назначение:** Polling статуса конвертации для плеера

**Response 200:**
```json
{
  "status": "in_progress|completed|failed",
  "progress_percent": "number",
  "hls_url": "string (null пока не completed)"
}
```

---

## Ingest API

The ingest API has been removed from the converter. The Scanner service now
owns ingest state and exposes its own HTTP API. See `docs/architecture/modules/scanner.md`.

## Стандартные ошибки

```json
{
  "error": "string (описание ошибки)",
  "code": "string (machine-readable код, optional)"
}
```

| HTTP код | Значение |
|---|---|
| 400 | Bad Request (невалидный ввод) |
| 401 | Unauthorized (нет/неверный токен) |
| 403 | Forbidden (недостаточно прав) |
| 404 | Not Found |
| 409 | Conflict (дубликат request_id) |
| 500 | Internal Server Error |
