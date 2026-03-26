# Описание сервисов

## API Service (`converter/api/`)

**Язык:** Go 1.23
**Порт:** 8000
**Образ:** Multistage Dockerfile (builder: golang:1.23-alpine, runtime: alpine:3.21)

### Ответственность
- Аутентификация и авторизация (JWT + bcrypt)
- Управление заданиями (CRUD, статусы, пагинация)
- Поиск через Prowlarr (с кэшированием и circuit breaker)
- Каталог фильмов и субтитров
- Player API (данные для воспроизведения, подписывание URL)
- Применение DB-миграций при старте

### Ключевые зависимости (Go)
| Пакет | Назначение |
|---|---|
| `go-chi/chi/v5` | HTTP роутер |
| `golang-jwt/jwt/v5` | JWT токены |
| `jackc/pgx/v5` | PostgreSQL (pgxpool) |
| `redis/go-redis/v9` | Redis клиент |
| `sony/gobreaker` | Circuit breaker для Prowlarr |
| `golang.org/x/crypto` | bcrypt |

### API роуты
```
POST   /api/admin/auth/login
GET    /api/admin/search
POST   /api/admin/jobs
POST   /api/admin/jobs/upload
POST   /api/admin/jobs/remote-download
GET    /api/admin/jobs
GET    /api/admin/jobs/{jobID}
DELETE /api/admin/jobs/{jobID}
GET    /api/admin/jobs/{jobID}/thumbnail
GET    /api/admin/movies
PATCH  /api/admin/movies/{movieId}
DELETE /api/admin/movies/{movieId}
GET    /api/admin/movies/{movieId}/thumbnail
GET    /api/admin/movies/tmdb/{tmdbId}
GET    /api/admin/movies/tmdb/search
GET    /api/admin/movies/{movieId}/subtitles
POST   /api/admin/movies/{movieId}/subtitles
POST   /api/admin/movies/{movieId}/subtitles/search
GET    /api/player/movie
GET    /api/player/assets/{assetID}
GET    /api/player/jobs/{jobID}/status
POST   /api/player/p2p-metrics
GET    /metrics
GET    /health/live
GET    /health/ready
```

---

## Worker Service (`converter/worker/`)

**Язык:** Go 1.23
**Порт:** 8001 (только health check)
**Образ:** Multistage Dockerfile

### Ответственность
- Потребление очередей Redis (BLPOP)
- Управление загрузками торрентов через qBittorrent API
- HTTP-загрузка удалённых файлов (с поддержкой proxy)
- FFmpeg HLS конвертация (360p/480p/720p)
- Создание записей movie и asset в БД
- Авто-получение субтитров (OpenSubtitles)
- Загрузка постера с TMDB

### Горутины при старте
```
main.go
  ├── for i in DOWNLOAD_CONCURRENCY:      go downloadWorker()
  ├── for i in CONVERT_CONCURRENCY:       go convertWorker()
  ├── for i in HTTP_DOWNLOAD_CONCURRENCY: go httpDownloadWorker()
  ├── for i in TRANSFER_CONCURRENCY:      go transferWorker()   # только если RCLONE_REMOTE задан
  ├── for i in INGEST_CONCURRENCY:        go ingestWorker()     # только если INGEST_SERVICE_TOKEN + INGEST_SOURCE_REMOTE заданы
  └── go healthServer(:8001)
```

### Concurrency defaults
| Параметр | Default |
|---|---|
| `DOWNLOAD_CONCURRENCY` | 2 |
| `CONVERT_CONCURRENCY` | 1 |
| `HTTP_DOWNLOAD_CONCURRENCY` | 3 |
| `TRANSFER_CONCURRENCY` | 1 |
| `INGEST_CONCURRENCY` | 3 |
| FFmpeg thread limit | 0 (auto) |

---

## Frontend Service (`converter/frontend/`)

**Технология:** Next.js 14.2 / React 18
**Порт:** 3000
**Стилизация:** Tailwind CSS 3.4

### Ответственность
- Admin UI для управления заданиями, поиском, каталогом
- Аутентификация (JWT в localStorage)
- Real-time обновления через SWR polling
- HLS воспроизведение через hls.js

### Маршруты страниц
| Маршрут | Назначение |
|---|---|
| `/` | Auth guard (→ /login или /movies) |
| `/login` | Форма входа |
| `/search` | Поиск торрентов |
| `/upload` | Загрузка файла / HTTP-загрузка |
| `/queue` | Список заданий |
| `/jobs/[jobId]` | Детали задания |
| `/movies` | Каталог фильмов |

---

## PostgreSQL (Postgres 16 Alpine)

**Порт:** 5432 (внутренний)
**Данные:** Docker volume `postgres_data`

### Таблицы
| Таблица | Назначение |
|---|---|
| `media_jobs` | Задания (статус, stage, прогресс) |
| `media_assets` | Результаты HLS конвертации |
| `movies` | Каталог фильмов (IMDb/TMDB) |
| `movie_subtitles` | Субтитры на язык |
| `search_results` | Кэш Prowlarr релизов |
| `job_events` | Аудит-лог (JSONB) |
| `storage_locations` | Хранилища файлов (local/remote); плеер выбирает base_url по `storage_location_id` |

Миграции: `api/internal/db/migrations/` (001–013), применяются автоматически при старте API.

---

## Redis (Redis 7 Alpine)

**Порт:** 6379 (внутренний)
**Персистентность:** AOF (`appendonly yes`)

### Использование
- Очереди заданий (list, RPUSH/BLPOP)
- Distributed locks (SET NX, 1 час TTL)
- (Потенциально) кэширование поисковых результатов

---

## qBittorrent (LinuxServer)

**Порт:** 8080 (WebUI, внешний)
**Credentials:** из `.env` (QBITTORRENT_USER / QBITTORRENT_PASSWORD)

Worker взаимодействует с qBittorrent через HTTP API (логин → добавить торрент → опрос статуса).

---

## Prowlarr (LinuxServer)

**Порт:** 9696 (внешний, для ручной настройки)
**API Key:** из `.env` (PROWLARR_API_KEY)

Агрегатор торрент-индексаторов. Worker не использует Prowlarr напрямую — только API.

---

## FlaresolveRR

**Порт:** 8191 (внутренний)

Обходит Cloudflare при запросах Prowlarr к защищённым индексаторам.
