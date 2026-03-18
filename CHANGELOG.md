# CHANGELOG

Все значимые изменения фиксируются здесь.
Формат основан на [Keep a Changelog](https://keepachangelog.com/).
Версионирование следует [Semantic Versioning](https://semver.org/).

> AI-ассистенты обязаны обновлять этот файл при каждом изменении кода.
> Инструкция: добавляй запись в секцию `[Unreleased]`, указывай тип (`Added/Changed/Fixed/Removed/Security`).

---

## [Unreleased]

### Changed
- `worker/internal/converter/converter.go`: `buildSourceFilename` wraps TMDB ID in brackets — format is now `title_year_[tmdbID].ext`; if no TMDB ID, bracket suffix is omitted
- `worker/internal/repository/movie.go`: `buildStorageKey` now uses underscores instead of spaces and `Title(Year)` format without space before parenthesis

### Added
- `api/internal/repository/incoming.go`: add `IncomingRepository` with atomic batch claim (expired-lease reset CTE, `FOR UPDATE SKIP LOCKED`), idempotent `Register` upsert, `GetByID`, `Progress`, `Fail` (retry vs. dead-letter), and `Complete` methods
- `worker/internal/model/model.go`: add `StageTransfer` constant
- `api/internal/model/model.go`: add `JobStageTransfer` constant
- `worker/internal/repository/job.go`: add `SetStageAndProgress` and `SetCompleted` methods for transfer stage tracking
- `worker/internal/transfer/transfer.go`: rewrite transfer worker with rclone stderr progress parsing and job stage updates
- `frontend/src/types/index.ts`: add `'transfer'` to `JobStage` type
- `frontend/src/app/queue/page.tsx`: show "Перенос" label for transfer stage with progress bar
- `frontend/src/app/jobs/[jobId]/page.tsx`: show "Перенос" label for transfer stage

### Changed
- `worker/internal/converter/converter.go`: transition job to `transfer` stage instead of `completed` when transfer is enabled; fix subtitle fetch ordering race with rclone
- `worker/cmd/worker/main.go`: inject `jobRepo` into transfer worker constructor

### Added
- `api/internal/db/migrations/010_storage_locations.sql`: storage_locations table and movies.storage_location_id FK
- `api/internal/db/migrations/011_seed_remote_storage_location.sql`: seed remote storage location row
- `worker/internal/transfer/transfer.go`: TransferWorker — rclone-based post-conversion file transfer
- `api/internal/repository/storage_location.go`: StorageLocationRepository (api)
- `worker/internal/repository/storage_location.go`: StorageLocationRepository (worker)
- `frontend/src/app/api/app-config/route.ts`: server-side Route Handler с `dynamic = 'force-dynamic'`, возвращает `playerUrl` из env для клиентских компонентов без бейка в бандл
- `CLAUDE.md` — инструкции и правила для AI-ассистентов
- `REPO_MAP.md` — карта директорий проекта
- `ARCHITECTURE.md` — краткий обзор системной архитектуры
- `Makefile` — единая точка входа для команд разработки (`make help`)
- `.githooks/commit-msg` — валидатор формата Conventional Commits
- `.githooks/pre-commit` — go vet + проверка на секреты
- `docs/architecture/system-overview.md` — общий обзор системы
- `docs/architecture/services.md` — описание каждого сервиса
- `docs/architecture/data-flow.md` — потоки данных между сервисами
- `docs/architecture/deployment.md` — развёртывание и инфраструктура
- `docs/contracts/api.md` — HTTP API контракты
- `docs/contracts/worker.md` — контракты очередей воркера
- `docs/converter/pipeline.md` — FFmpeg HLS pipeline
- `docs/player/player-architecture.md` — архитектура плеера
- `docs/admin/admin-overview.md` — Admin UI обзор
- `docs/roadmap/technical-debt.md` — технический долг и приоритеты
- `docs/contributing/conventional-commits.md` — стандарт оформления коммитов
- `docs/decisions/` — система Architecture Decision Records (ADR-001..007)
- `docs/roadmap/roadmap.md` — дорожная карта развития
- `scripts/new-adr.sh` — скрипт создания нового ADR

### Changed

- `worker/internal/repository/movie.go`: storage_key now uses "Title (Year)" format instead of random hex; added unique-constraint collision retry (up to 10 attempts with numeric suffix)
- `worker/internal/converter/converter.go`: HLS output now written to `media/converted/movies/{storageKey}/` instead of `media/converted/{storageKey}/`; enqueue transfer_queue message after successful HLS conversion
- `worker/internal/downloader/downloader.go`: `FinalDir` hint in convert message updated to new `converted/movies/` prefix
- `api/internal/service/job.go`: upload job `FinalDir` updated to `converted/movies/` prefix
- `api/internal/handler/player.go`: media base URL resolved per-movie from storage_location; falls back to MEDIA_BASE_URL when base_url is empty
- `api/internal/handler/subtitles.go`: subtitle directory resolves to `converted/movies/{storageKey}/subtitles`
- `api/internal/db/migrations/009_update_storage_path_movies_subdir.sql`: backfills existing `media_assets.storage_path`, `media_assets.thumbnail_path`, and `movie_subtitles.storage_path` rows to the new path prefix
- `worker/Dockerfile`: rclone installed

### Fixed

- `frontend/src/app/movies/page.tsx`: кнопка "смотреть" теперь корректно открывает плеер в модальном окне через iframe; URL плеера читается из runtime-переменной `PLAYER_URL` вместо build-time `NEXT_PUBLIC_PLAYER_URL`

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

