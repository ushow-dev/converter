# Scanner HTTP API

FastAPI сервер, запущенный внутри scanner-процесса. Принимает запросы от Go IngestWorker для управления жизненным циклом входящих файлов.

**Base URL:** `http://scanner:8080` (внутри Docker Compose сети)
**Порт:** задаётся через `SCANNER_API_PORT` (default: `8080`)
**Аутентификация:** все endpoints требуют заголовок `X-Service-Token`

---

## Аутентификация

Все запросы должны содержать заголовок:

```
X-Service-Token: <SERVICE_TOKEN>
```

При отсутствии или неверном токене — `401 Unauthorized`.

---

## Endpoints

### POST /api/v1/incoming/claim

Атомарно забирает доступные (status=`registered`) items из очереди.
Использует `SELECT ... FOR UPDATE SKIP LOCKED` — безопасно для параллельных воркеров.

**Request body:**
```json
{
  "limit": 1,
  "claim_ttl_sec": 900
}
```

| Поле | Тип | Default | Описание |
|---|---|---|---|
| `limit` | int | `1` | Максимальное количество items (не более 10) |
| `claim_ttl_sec` | int | `900` | TTL claim в секундах (15 мин) |

**Response 200:**
```json
{
  "items": [
    {
      "id": 42,
      "source_path": "/mnt/storage/incoming/Dune.Part.Two.2024.2160p.BluRay.mkv",
      "source_filename": "Dune.Part.Two.2024.2160p.BluRay.mkv",
      "normalized_name": "dune_part_two_2024_[693134]",
      "tmdb_id": "693134",
      "content_kind": "movie"
    }
  ]
}
```

При пустой очереди возвращает `{"items": []}`.

**Побочный эффект:**
- `status` → `claimed`
- `claimed_at` → `NOW()`
- `claim_expires_at` → `NOW() + claim_ttl_sec`

---

### POST /api/v1/incoming/{item_id}/progress

Обновляет статус копирования для указанного item.

**Path parameters:**
- `item_id` — integer ID из поля `id` в ответе `/claim`

**Request body:**
```json
{ "status": "copying" }
```

| `status` | Когда вызывать |
|---|---|
| `copying` | Сразу после начала rclone copy |
| `copied` | После завершения rclone copy (до финального /complete) |

**Response:** `204 No Content`

**Ошибки:**
- `400` — если `status` не `copying` и не `copied`
- `401` — неверный токен

---

### POST /api/v1/incoming/{item_id}/complete

Помечает item как завершённый. Запускает перемещение файла в `library/`.

**Path parameters:**
- `item_id` — integer ID

**Request body:** пустой `{}` (или без body)

**Response 200:**
```json
{
  "id": 42,
  "job_id": "ingest-42"
}
```

**Побочный эффект:**
- `status` → `completed`
- item попадает во внутреннюю `move_queue`
- `move_worker` выполняет `os.rename()` → `library/movies/{normalized_name}/`
- После rename: `status` → `archived`, upsert в `scanner_library_movies`

**Ошибки:**
- `404` — item не найден
- `401` — неверный токен

---

### POST /api/v1/incoming/{item_id}/fail

Фиксирует ошибку обработки item.

**Path parameters:**
- `item_id` — integer ID

**Request body:**
```json
{ "error_message": "rclone: connection timeout after 300s" }
```

| Поле | Тип | Default | Описание |
|---|---|---|---|
| `error_message` | string | `""` | Описание ошибки для диагностики |

**Response:** `204 No Content`

**Побочный эффект:**
- `status` → `failed`
- `error_message` записывается в DB

**Ошибки:**
- `401` — неверный токен

---

## Типичный flow (IngestWorker)

```
1. POST /claim            → получить item (id=42, source_path=...)
2. POST /42/progress      body: {"status": "copying"}
3. rclone copy <source_path> <remote_dst>
4. POST /42/progress      body: {"status": "copied"}
5. CreateForIngest в локальной DB (converter)
6. Push convert_queue
7. POST /42/complete      → получить job_id
```

При ошибке на любом шаге (3 или 5):
```
POST /42/fail    body: {"error_message": "..."}
```

---

## Коды ответов

| Code | Описание |
|---|---|
| `200` | Успех с телом ответа (claim, complete) |
| `204` | Успех без тела (progress, fail) |
| `400` | Некорректный запрос (невалидный статус progress) |
| `401` | Неверный или отсутствующий X-Service-Token |
| `404` | Item не найден (complete) |
| `422` | Невалидное тело запроса (FastAPI Pydantic validation) |

---

## Конфигурация на стороне IngestWorker (Go)

| Env | Default | Описание |
|---|---|---|
| `SCANNER_API_URL` | `http://scanner:8080` | Base URL scanner API |
| `INGEST_SERVICE_TOKEN` | — | Должен совпадать с `SERVICE_TOKEN` scanner |
| `INGEST_CLAIM_TTL_SEC` | `900` | TTL claim при запросе |
| `INGEST_CONCURRENCY` | `1` | Параллельных claim/copy операций |
