# CHANGELOG

Все значимые изменения фиксируются здесь.
Формат основан на [Keep a Changelog](https://keepachangelog.com/).
Версионирование следует [Semantic Versioning](https://semver.org/).

> AI-ассистенты обязаны обновлять этот файл при каждом изменении кода.
> Инструкция: добавляй запись в секцию `[Unreleased]`, указывай тип (`Added/Changed/Fixed/Removed/Security`).

---

## [Unreleased]

### Changed

- `worker/internal/repository/movie.go`: replaced random `mov_<hex>` storage key generation with human-readable `Title (Year)` format; added unique-constraint collision retry (up to 10 attempts with numeric suffix)



- `worker/internal/converter/converter.go`: HLS output now written to `media/converted/movies/{storageKey}/` instead of `media/converted/{storageKey}/`
- `worker/internal/downloader/downloader.go`: `FinalDir` hint in convert message updated to new `converted/movies/` prefix
- `api/internal/service/job.go`: upload job `FinalDir` updated to `converted/movies/` prefix
- `api/internal/handler/player.go`: `buildMovieMediaURL` uses new `/media/converted/movies/{key}/{file}` URL format; `mediaSigningPath` updated to bind token at depth 4 (`/media/converted/movies/{key}/`)
- `api/internal/handler/subtitles.go`: subtitle directory resolves to `converted/movies/{storageKey}/subtitles`
- `api/internal/db/migrations/009_update_storage_path_movies_subdir.sql`: backfills existing `media_assets.storage_path`, `media_assets.thumbnail_path`, and `movie_subtitles.storage_path` rows to the new path prefix

### Fixed

- `frontend/src/app/movies/page.tsx`: кнопка "смотреть" теперь корректно открывает плеер в модальном окне через iframe; URL плеера читается из runtime-переменной `PLAYER_URL` вместо build-time `NEXT_PUBLIC_PLAYER_URL`

### Added

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
- `docs/decisions/` — система Architecture Decision Records (ADR-001..006)
- `docs/roadmap/roadmap.md` — дорожная карта развития
- `scripts/new-adr.sh` — скрипт создания нового ADR

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

