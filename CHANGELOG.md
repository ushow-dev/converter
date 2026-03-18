# CHANGELOG

Все значимые изменения фиксируются здесь.
Формат основан на [Keep a Changelog](https://keepachangelog.com/).
Версионирование следует [Semantic Versioning](https://semver.org/).

> AI-ассистенты обязаны обновлять этот файл при каждом изменении кода.
> Инструкция: добавляй запись в секцию `[Unreleased]`, указывай тип (`Added/Changed/Fixed/Removed/Security`).

---

## [Unreleased]

### Added

- `api/internal/db/migrations/012_incoming_media_items.sql`: `incoming_media_items` table with status machine (`new → claiming → copying → copied → completed / failed`), lease expiry, quality and duplicate fields
- `api/internal/model/incoming.go`: `IncomingItem` model, status constants, and 7 request/response types for the ingest API
- `api/internal/auth/service_token.go`: `ServiceTokenMiddleware` — `X-Service-Token` header auth for service-to-service endpoints
- `api/internal/repository/incoming.go`: `IncomingRepository` — atomic batch claim with `FOR UPDATE SKIP LOCKED` and expired-lease reset in a single CTE; idempotent `Register` upsert; `Fail` with retry-vs-dead-letter logic
- `api/internal/service/ingest.go`: `IngestService` — `Complete` creates a deterministic `media_job` (idempotent via `request_id`) and pushes `ConvertPayload` to `convert_queue`
- `api/internal/handler/ingest.go`: `IngestHandler` with 5 service-token-protected endpoints: `Register`, `Claim`, `Progress`, `Fail`, `Complete`
- `api/internal/server/server.go`: register `/api/ingest/*` route group protected by `ServiceTokenMiddleware`
- `api/cmd/api/main.go`: wire `IncomingRepository`, `IngestService`, and `IngestHandler` into the dependency graph
- `worker/internal/ingest/client.go`: HTTP client for the ingest API (`Claim`, `Progress`, `Fail`, `Complete`)
- `worker/internal/ingest/puller.go`: rclone-based `Puller` that copies a single source file from a remote to local disk
- `worker/internal/ingest/worker.go`: poll-loop worker — claims items from the API, copies via rclone, calls `complete` to create the convert job server-side
- `worker/internal/config/config.go`: 7 new ingest config fields: `ConverterAPIURL`, `IngestServiceToken`, `IngestConcurrency`, `IngestClaimTTLSec`, `IngestMaxAttempts`, `IngestSourceRemote`, `IngestSourceBasePath`
- `worker/cmd/worker/main.go`: wire ingest worker goroutines; gated on `INGEST_SERVICE_TOKEN` and `INGEST_SOURCE_REMOTE` being set
- `.env.example`: document ingest worker environment variables
- `worker/internal/model/model.go`: `StageTransfer` constant
- `api/internal/model/model.go`: `JobStageTransfer` constant
- `worker/internal/repository/job.go`: `SetStageAndProgress` and `SetCompleted` methods for transfer stage tracking
- `worker/internal/transfer/transfer.go`: `TransferWorker` — rclone-based post-conversion file transfer with stderr progress parsing and job stage updates
- `frontend/src/types/index.ts`: add `'transfer'` to `JobStage` type
- `frontend/src/app/queue/page.tsx`: show "Перенос" label for transfer stage with progress bar
- `frontend/src/app/jobs/[jobId]/page.tsx`: show "Перенос" label for transfer stage
- `api/internal/db/migrations/010_storage_locations.sql`: `storage_locations` table and `movies.storage_location_id` FK
- `api/internal/db/migrations/011_seed_remote_storage_location.sql`: seed remote storage location row
- `api/internal/repository/storage_location.go`: `StorageLocationRepository` (api)
- `worker/internal/repository/storage_location.go`: `StorageLocationRepository` (worker)
- `frontend/src/app/api/app-config/route.ts`: server-side Route Handler with `dynamic = 'force-dynamic'`, returns `playerUrl` from env for client components without build-time baking
- `CLAUDE.md`: instructions and rules for AI assistants
- `REPO_MAP.md`: directory map of the project
- `ARCHITECTURE.md`: brief system architecture overview
- `Makefile`: single entry point for dev commands (`make help`)
- `.githooks/commit-msg`: Conventional Commits format validator
- `.githooks/pre-commit`: go vet + secrets check
- `docs/architecture/system-overview.md`, `services.md`, `data-flow.md`, `deployment.md`: architecture documentation
- `docs/contracts/api.md`: HTTP API contracts
- `docs/contracts/worker.md`: worker queue contracts
- `docs/converter/pipeline.md`: FFmpeg HLS pipeline
- `docs/decisions/ADR-001` through `ADR-008`: Architecture Decision Records
- `docs/decisions/ADR-008-incoming-scanner-api-driven-ingest-split.md`: ADR for API-driven ingest split between scanner and converter
- `scripts/new-adr.sh`: script for creating new ADRs

### Changed

- `worker/internal/converter/converter.go`: transition job to `transfer` stage instead of `completed` when transfer is enabled; `buildSourceFilename` wraps TMDB ID in brackets (`title_year_[tmdbID].ext`); omits bracket suffix if no TMDB ID
- `worker/internal/repository/movie.go`: `buildStorageKey` uses underscores and `Title(Year)` format (no space before parenthesis); unique-constraint collision retry up to 10 attempts with numeric suffix
- `worker/cmd/worker/main.go`: inject `jobRepo` into transfer worker constructor
- `worker/internal/downloader/downloader.go`: `FinalDir` hint in convert message updated to `converted/movies/` prefix
- `api/internal/service/job.go`: upload job `FinalDir` updated to `converted/movies/` prefix
- `api/internal/handler/player.go`: media base URL resolved per-movie from storage_location; falls back to `MEDIA_BASE_URL` when `base_url` is empty
- `api/internal/handler/subtitles.go`: subtitle directory resolves to `converted/movies/{storageKey}/subtitles`
- `api/internal/db/migrations/009_update_storage_path_movies_subdir.sql`: backfills existing `media_assets.storage_path`, `media_assets.thumbnail_path`, and `movie_subtitles.storage_path` rows to new path prefix
- `worker/Dockerfile`: rclone installed

### Fixed

- `frontend/src/app/movies/page.tsx`: "смотреть" button now correctly opens the player in a modal via iframe; player URL read from runtime `PLAYER_URL` variable instead of build-time `NEXT_PUBLIC_PLAYER_URL`

---

## [0.1.0] — базовая версия системы

> Зафиксировано при аудите 2026-03-16. Функциональность существовала до начала документирования.

### Added

- API сервис (Go, chi v5) — admin и player эндпоинты
- Worker сервис (Go) — загрузка торрентов + HLS конвертация FFmpeg
- Frontend (Next.js 14) — Admin UI
- PostgreSQL схема: `media_jobs`, `media_assets`, `movies`, `movie_subtitles`, `search_results`, `job_events`
- Redis очереди: `download_queue`, `convert_queue`, `remote_download_queue`
- Docker Compose стек: postgres, redis, api, worker, frontend, qbittorrent, prowlarr, flaresolverr
- JWT аутентификация для admin, API Key для player
- Мульти-разрешение HLS: 360p / 480p / 720p
- Автоматические субтитры через OpenSubtitles API
- Метаданные фильмов через TMDB API
- Поиск торрентов через Prowlarr с circuit breaker
- Курсорная пагинация для jobs и movies
- Идемпотентное создание заданий через `request_id`
- MD5 подписывание медиа-URL (опциональное, nginx secure_link)
- DB-миграции: автоприменение при старте API
- Distributed locking через Redis NX

