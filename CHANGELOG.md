# CHANGELOG

Все значимые изменения фиксируются здесь.
Формат основан на [Keep a Changelog](https://keepachangelog.com/).
Версионирование следует [Semantic Versioning](https://semver.org/).

> AI-ассистенты обязаны обновлять этот файл при каждом изменении кода.
> Инструкция: добавляй запись в секцию `[Unreleased]`, указывай тип (`Added/Changed/Fixed/Removed/Security`).

---

## [Unreleased]

### Added
- `worker/internal/repository/subtitle.go`: add `UpsertEpisodeSubtitle` method to persist subtitle tracks for series episodes into `episode_subtitles` table
- `worker/internal/converter/converter.go`: fetch and save subtitles for episodes via `subtitleFetcher.FetchAndSave` after HLS conversion completes, mirroring existing movie subtitle behaviour

### Changed
- `worker/internal/converter/converter.go`: replaced inline `filepath.Join` path construction with `PathResolver` calls (`MovieFinalDir`, `EpisodeFinalDir`, `MovieTransferKey`, `EpisodeTransferKey`, `DownloadsDir`); removed `transferStorageKey()` helper function
- `worker/cmd/worker/main.go`: create `paths.Resolver` and pass it to `converter.New()`
- `worker/internal/recovery/recovery.go`: replaced local `stripMasterPlaylist` helper with `paths.StripMasterPlaylist` from the centralized paths package; removed duplicate logic

### Added
- `worker/internal/paths/paths.go`: new `PathResolver` package providing centralized media path resolution — `MovieFinalDir`, `EpisodeFinalDir`, `MovieTransferKey`, `EpisodeTransferKey`, `TransferDest`, `DownloadsDir`, `TempDir`, and `StripMasterPlaylist` as single source of truth for all path construction in the worker

### Fixed
- `worker/internal/model/model.go`, `api/internal/model/model.go`: renamed `TransferJob.MovieID` to `ContentID` (JSON: `content_id`) and removed unused `EpisodeID` field — `ContentID` now correctly holds either a movie ID or an episode ID depending on `ContentType`
- `worker/internal/transfer/transfer.go`: skip `movieRepo.UpdateStorageLocation` for episodes (content_type=episode) to prevent updating a non-existent movies row; updated log field from `movie_id` to `content_id`
- `worker/internal/converter/converter.go`: updated TransferJob construction to use `ContentID` field
- `worker/internal/recovery/recovery.go`: updated `rebuildTransferPayload` to use `ContentID` for both movie and episode recovery paths
- `scanner/scanner/loops/scan_loop.py`: `_process_series_folder` now inserts episodes with `status='new'` instead of `status='registered'`, so the stability check in `_handle_stable_episode` must promote them — prevents partially-copied files from being ingested

### Security
- `api/internal/handler/series.go`: validate episode thumbnail path is under `/media/` before serving to prevent path traversal; use `filepath.Clean` and prefix check, return 404 on violation

### Added
- `api/internal/handler/series.go`: added `DeleteEpisode` handler (`DELETE /api/admin/episodes/{episodeId}`) and `EpisodeThumbnail` handler (`GET /api/admin/episodes/{episodeId}/thumbnail`); enriched `Get` response to include `has_thumbnail` and `created_at` per episode by querying `GetEpisodeAsset`
- `api/internal/repository/series.go`: added `DeleteEpisode` method — deletes episode row by primary key
- `api/internal/server/server.go`: registered `DELETE /episodes/{episodeId}` in JWT-protected group and `GET /episodes/{episodeId}/thumbnail` in JWT-query-or-header group
- `frontend/src/lib/api.ts`: added `deleteEpisode` and `episodeThumbnailSrc` API helpers
- `frontend/src/types/index.ts`: added optional `has_thumbnail` field to `Episode` interface
- `frontend/src/app/series/[id]/page.tsx`: added `PlayerModal` component for in-page episode playback; added thumbnail column to episode table; added per-episode delete button with confirmation; play button now opens modal instead of external link

### Fixed
- `worker/internal/converter/converter.go`: prefix episode `storage_key` with `series.StorageKey` to prevent UNIQUE constraint violations when multiple series share the same season/episode number (e.g. `devil_may_cry_2025_[235930]_s01e01`)
- `frontend/src/app/series/page.tsx`: added play button (links to player with `type=series`) and delete button per row; fetches `playerUrl` from `/api/app-config`; added TMDB external link icon button matching movies page pattern
- `frontend/src/app/series/[id]/page.tsx`: replaced broken `<dl>` grid metadata layout with flat flex row of inline label+value pairs; fixed invalid Tailwind class `w-22` → `w-[88px]` for poster placeholder; added play button to each episode row linking to player with season/episode params; fetches `playerUrl` from `/api/app-config`; passes `tmdbId` and `playerUrl` down through `SeasonSection` and `EpisodeRow`

### Added
- `player/src/app/SeriesPlayer.tsx`: new client component — handles series navigation with season/episode dropdowns, prev/next buttons, and single-episode embed mode; converts episode API data into `MovieResponse` format for `PlayerClient`
- `player/src/app/page.tsx`: added `type`, `s`, `e`, `nav` search params; routing logic dispatches to `SeriesPlayer` for `type=series` requests (full navigation or single-episode embed) and to `PlayerClient` for movies
- `player/src/app/globals.css`: added `.series-player-wrapper`, `.series-nav`, `.series-title`, `.series-selectors`, `.series-select`, `.series-ep-nav`, `.ep-nav-btn` styles for series navigation UI

- `player/src/app/PlayerClient.tsx`: added `AudioTrackInfo` interface, `audioTracks`/`selectedAudio` state, `applyAudioTrack` callback, and audio track selector UI in the settings panel — shows "Озвучка" section when HLS stream has more than one audio track
- `api/internal/model/series.go`: new model structs `Series`, `Season`, `Episode`, `EpisodeAsset`, `EpisodeSubtitle`, `AudioTrack`, and `ContentTypeSeries` constant — domain models for series/episode support
- `api/internal/model/model.go`: extended `ConvertJob` with `SeriesID`, `SeasonNumber`, `EpisodeNumber` fields; extended `TransferJob` with `ContentType` and `EpisodeID` fields — enables queue payloads to carry series context
- `api/internal/db/migrations/014_series_and_audio_tracks.sql`: new tables `series`, `seasons`, `episodes`, `episode_assets`, `episode_subtitles`, `audio_tracks`; extends `media_jobs` with `series_id`, `season_number`, `episode_number` columns — foundation for series/episode support
- `worker/internal/model/series.go`: new worker-side domain structs `Series`, `Season`, `Episode`, `EpisodeAsset`, `AudioTrack` mirroring API models (no JSON tags on domain structs)
- `worker/internal/model/model.go`: added `SeriesID`, `SeasonNumber`, `EpisodeNumber` to `ConvertJob`; added `ContentType`, `EpisodeID` to `TransferJob` to route series content through the worker pipeline
- `worker/internal/ffmpeg/runner.go`: `HLSResult` now carries `AudioTracks []AudioStreamInfo` — callers can persist per-track language/title metadata after encoding
- `worker/internal/repository/series.go`: new `SeriesRepository` with `UpsertSeries`, `UpsertSeason`, `UpsertEpisode`, `CreateEpisodeAsset`, `GetSeriesByID` — persists series catalog and episode records after conversion
- `worker/internal/repository/audio_track.go`: new `AudioTrackRepository` with `BulkInsert` — persists audio track metadata produced by multi-audio HLS encoding
- `api/internal/repository/series.go`: new `SeriesRepository` with `GetByTMDBID`, `GetByID`, `List` (cursor pagination), `ListSeasons`, `ListEpisodes`, `GetEpisodeBySE`, `GetEpisodeAsset`, `DeleteSeries` — read/write access to series catalog for the API service
- `api/internal/repository/audio_track.go`: new `AudioTrackRepository` with `ListByAsset` — retrieves audio tracks by asset ID and type for the API service
- `api/internal/repository/episode_subtitle.go`: new `EpisodeSubtitleRepository` with `ListByEpisodeID` — retrieves episode subtitles ordered by language for the API service

- `worker/internal/converter/converter.go`: added `seriesRepo` and `audioTrackRepo` fields to `Worker`; `New()` now accepts both repos; `process()` branches on `content_type="series"` to upsert season/episode and write HLS to `converted/series/{key}/sNN/eNN` path; episode assets are created via `SeriesRepository.CreateEpisodeAsset`; audio tracks from `HLSResult.AudioTracks` are bulk-inserted after asset creation via `AudioTrackRepository.BulkInsert`; `TransferJob` now carries `ContentType` and uses `contentID`/`filepath.Base(finalDir)` so transfer worker can handle both content types; added `nullableText` helper
- `worker/cmd/worker/main.go`: instantiate `audioTrackRepo` and pass `seriesRepo`+`audioTrackRepo` to `converter.New()`
- `worker/internal/ingest/client.go`: added `SeriesTMDBID`, `SeasonNumber`, `EpisodeNumber` fields to `IncomingItem` — scanner can now supply episode context for series content
- `worker/internal/ingest/worker.go`: added `seriesRepo` field and updated `New()` to accept `*repository.SeriesRepository`; `processItem` now upserts the series record and forwards `SeriesID`, `SeasonNumber`, `EpisodeNumber` in the `ConvertMessage` payload for `content_kind=episode` items
- `worker/cmd/worker/main.go`: instantiate `seriesRepo` and pass it to `ingest.New()`
- `api/internal/handler/player.go`: added `GetSeries` and `GetEpisode` handlers; added audio tracks to `GetMovie` response; added `buildSeriesMediaURL` helper and `buildEpisodePayload`/`buildEpisodeAudioTracks`/`buildEpisodeSubtitles` helpers; updated `mediaSigningPath` to support `series` content type
- `api/internal/repository/asset.go`: added `GetByMovieID` method to look up the ready asset for a movie by movie ID
- `api/cmd/api/main.go`: instantiate `seriesRepo`, `audioTrackRepo`, `epSubtitleRepo` and pass to `NewPlayerHandler`
- `api/internal/server/server.go`: register `GET /api/player/series` and `GET /api/player/episode` routes
- `scanner/scanner/migrations/005_series_support.sql`: adds `content_kind`, `series_tmdb_id`, `season_number`, `episode_number` columns to `scanner_incoming_items` — enables episode rows alongside movie rows
- `scanner/scanner/services/series_detect.py`: new service that walks a folder with `guessit` and returns a sorted list of episode dicts `{file_path, title, season, episode, year}` — used by scan_loop to detect TV series folders
- `scanner/scanner/services/metadata.py`: added `tmdb_tv_search()` for searching TMDB `/search/tv` endpoint — returns `tmdb_id`, `title`, `poster_url` for a series
- `scanner/scanner/loops/scan_loop.py`: `_scan_once()` now iterates top-level subdirectories and calls `_process_series_folder()` before the flat file walk; new `_process_series_folder()` detects episodes via `series_detect`, looks up TMDB TV, and inserts each episode as `status=registered` / `content_kind=episode`
- `scanner/scanner/api/server.py`: `/claim` response now returns `content_kind`, `series_tmdb_id`, `season_number`, `episode_number` fields from DB — IngestWorker can distinguish movies from episodes

- `frontend/src/types/index.ts`: added `Series`, `SeriesResponse`, `Season`, `Episode`, `SeriesDetailResponse` interfaces; extended `ContentType` to include `'series'`
- `frontend/src/components/Nav.tsx`: added "Сериалы" navigation link pointing to `/series`
- `frontend/src/lib/api.ts`: added `seriesUrl`, `getSeries`, `getSeriesDetail`, `deleteSeries` API helpers
- `frontend/src/app/series/page.tsx`: new series list page with cursor pagination and table view
- `frontend/src/app/series/[id]/page.tsx`: new series detail page with metadata header, collapsible season sections, and episode tables

- `docs/contracts/api.md`: added admin series endpoints (`GET /api/admin/series`, `GET /api/admin/series/{seriesId}`, `DELETE /api/admin/series/{seriesId}`) with request/response examples
- `REPO_MAP.md`: added entries for all new series-support files — handler, model, repository layers for both API and Worker; added `player/` dedicated section with `SeriesPlayer.tsx`; added frontend series pages
- `docs/decisions/ADR-011-separate-tables-for-series.md`: new ADR documenting the decision to use separate `series`/`seasons`/`episodes` tables instead of extending the polymorphic `movies` table
- `docs/decisions/README.md`: added ADR-011 to the index, updated next number to 012

### Changed
- `worker/internal/ffmpeg/runner.go`: `RunHLS` now maps all audio tracks from the source file into every HLS variant instead of hardcoding `0:a:0`; falls back to probeHasAudio and a single synthetic silence track when ProbeAudioStreams fails; builds `var_stream_map` dynamically so N audio × 3 video variants are correctly muxed; writes language/title metadata tags per audio stream

### Fixed
- `player/src/app/PlayerClient.tsx`: replace `reattachHlsAfterAd` (destroy+recreate hls.js on ad end) with `onAdStart`/`onAdEnd` using `detachMedia`/`attachMedia` — preserves the P2P engine and WebRTC connections across VAST ads
- `player/src/app/PlayerClient.tsx`: gate Fluid Player init on `hlsReady` state instead of `streamMode !== 'pending'` — eliminates race where Fluid Player could initialize before hls.js parsed the manifest, killing MSE/P2P playback
- `player/src/app/PlayerClient.tsx`: add `swarmId` derived from stream URL so peers watching the same movie join the same swarm — fixes P2P showing zero peer traffic
- `infra/nginx/api-server.conf`: add `X-Forwarded-For` header to wt-tracker proxy so tracker sees real peer IPs
- `scanner/scanner/services/metadata.py`: TMDB search now scores results by title similarity instead of blindly picking the first (most popular) result — fixes mismatches like "Fire" → "Avatar: Fire and Ash"

### Added
- `scripts/re-resolve-tmdb.py`: one-off script to re-resolve TMDB/IMDB IDs for all movies — extracts tags from filenames, re-searches TMDB with scoring, updates DB (dry-run by default)
- `api/internal/repository/movie.go`: `ListReadyTMDBIDs` method for querying converted movies by TMDB ID with delta support
- `api/internal/handler/player.go`: `GetCatalog` handler for `GET /api/player/catalog` endpoint
- `api/internal/server/server.go`: registered `/catalog` route in player auth group
- `player/src/app/PlayerClient.tsx`: P2P HLS streaming via p2p-media-loader — peers share `.ts` segments over WebRTC, reducing CDN/origin load
- `player/src/app/p2pMetrics.ts`: client-side P2P metrics collector — beacons HTTP/P2P byte counts and peer count to API every 30s
- `api/internal/handler/metrics.go`: Prometheus `/metrics` endpoint exposing P2P byte/segment counters
- `api/internal/handler/player.go`: `POST /api/player/p2p-metrics` endpoint for ingesting client P2P stats
- `docker-compose.api.yml`: `wt-tracker` service — WebTorrent tracker for P2P peer discovery
- `infra/wt-tracker/config.json`: wt-tracker configuration (port 8050, announce interval 120s)
- `infra/nginx/api-server.conf`: `tracker.pimor.online` server block with WSS proxy to wt-tracker
- `infra/grafana/provisioning/dashboards/p2p-overview.json`: Grafana dashboard — P2P ratio, bandwidth saved, active peers, segments by source
- `infra/prometheus/prometheus.yml`: API metrics scrape job for P2P counters
- `worker/internal/cancelregistry/registry.go`: thread-safe CancelRegistry — maps jobID → cancelFunc for per-job context cancellation
- `worker/cmd/worker/main.go`: cancel watcher goroutine BLPOPs from `cancel_queue` and calls `registry.Cancel(jobID)` to abort in-flight jobs
- `api/internal/queue/redis.go`, `worker/internal/queue/redis.go`: `CancelQueue = "cancel_queue"` constant

### Fixed
- `scanner/scanner/loops/scan_loop.py`: skip files prefixed with `._` (macOS SMB resource forks) and files under 1MB — prevents premature ffmpeg on empty or stub files
- `scanner/docker-compose.yml`: added `restart: unless-stopped` to postgres service — after server reboot postgres stayed stopped, scanner crash-looped unable to connect to DB
- `scanner/docker-compose.yml`: removed public port 5432 from postgres — DB is internal only, exposed port attracted scanner bots
- `worker/internal/repository/job.go`: `IsTerminal` now returns `(true, nil)` for deleted jobs (`pgx.ErrNoRows`) — queued jobs that were deleted before processing are skipped cleanly
- `frontend/src/app/movies/page.tsx`: limit title column to `max-w-[8rem]` on mobile with text truncation to reduce horizontal scroll
- `worker/internal/httpdownloader/downloader.go`: per-job context cancellation; `ReleaseLock` uses global ctx; cancelled downloads abort immediately without retry
- `worker/internal/converter/converter.go`: per-job context cancellation; `ReleaseLock` uses global ctx; cancelled ffmpeg processes are killed cleanly
- `api/internal/service/job.go`: `DeleteJob` pushes jobID to `cancel_queue` so the worker stops in-flight work immediately on deletion

### Changed
- `frontend/src/app/movies/page.tsx`: скрыты колонки ID, IMDb, год, дата, субтитры на мобиле (`hidden sm:table-cell`); TMDB и название остаются
- `frontend/src/app/queue/page.tsx`: скрыты колонки прогресс и дата на мобиле
- `frontend/src/app/search/page.tsx`: скрыта колонка Indexer на мобиле
- `frontend/src/components/Nav.tsx`: мобильная адаптация — ссылки перенесены во вторую строку на экранах < sm, горизонтальный скролл при нехватке места, отступы уменьшены на мобиле
- `frontend/src/app/movies/page.tsx`: таблица обёрнута в `overflow-x-auto` (вместо `overflow-hidden`), адаптивные отступы `px-3 py-4 sm:px-6 sm:py-8`, заголовок страницы с `flex-wrap`
- `frontend/src/app/queue/page.tsx`: таблица обёрнута в `overflow-x-auto`, адаптивные отступы, заголовок с `flex-wrap`
- `frontend/src/app/search/page.tsx`: убран `max-w-6xl` — таблица теперь во всю ширину страницы, адаптивные отступы
- `frontend/src/app/jobs/[jobId]/page.tsx`: адаптивные отступы `px-3 sm:px-6`
- `frontend/src/app/upload/page.tsx`: адаптивные отступы `px-3 sm:px-6`

### Fixed
- `api/internal/handler/browse.go`: browse корневой папки с большим количеством поддиректорий больше не вызывает 500/таймаут — реализована пагинация (offset/limit, по 100 за раз), результаты сортируются, добавлен 25-секундный таймаут на страницу
- `frontend/src/app/upload/page.tsx`: кнопка «Загрузить ещё (осталось N)» для догрузки следующей страницы директорий; счётчик «Показано X из Y»
- `frontend/src/lib/api.ts`: `browseRemoteUrl` принимает `offset`/`limit`, возвращает `BrowseResponse` вместо `RemoteMovie[]`
- `frontend/src/types/index.ts`: новый тип `BrowseResponse` (`items`, `total`, `has_more`)
- `worker/internal/transfer/transfer.go`: после `rclone move` локальная директория не удалялась — `rclone` перемещает файлы но оставляет пустые поддиректории (`720/`, `480/`, `360/`); исправлено `os.Remove` → `os.RemoveAll`
- `worker/internal/repository/movie.go`: storage key больше не корёжится при двойной нормализации — если `ConvertJob.StorageKey` задан, используется напрямую без вызова `buildStorageKey`
- `worker/internal/model/model.go`, `api/internal/model/model.go`: добавлено поле `StorageKey` в `ConvertJob` и `RemoteDownloadJob`
- `api/internal/service/job.go`: `CreateRemoteDownloadJob` теперь передаёт `StorageKey = normalizedName` в очередь
- `worker/internal/httpdownloader/downloader.go`: `StorageKey` пробрасывается из `RemoteDownloadMessage` в `ConvertMessage`
- `worker/internal/ingest/worker.go`: ingest worker ставит `StorageKey = normalized_name` из scanner

### Added
- `scanner/scanner/api/server.py`: новый endpoint `POST /api/v1/library/archive` — upsert в `scanner_library_movies` (`status=ready`); idempotent по `normalized_name`; парсит `quality_score`/`quality_label` из имени файла
- `worker/internal/ingest/client.go`: метод `Archive()` и тип `ArchiveRequest` для вызова нового scanner API endpoint
- `worker/internal/converter/converter.go`: после успешной конвертации non-ingest задания оригинальный файл копируется на scanner-сервер в `{ARCHIVE_DEST_PATH}/{storageKey}/` через rclone, регистрируется в `scanner_library_movies`, затем удаляется локально; при ошибке — просто удаляется локально
- `worker/internal/config/config.go`: новая переменная `ARCHIVE_DEST_PATH` (default `/library/movies`)

### Changed
- `worker/internal/converter/converter.go`: `Worker` расширен полями `scannerClient`, `ingestSourceRemote`, `archiveDestPath`; `New()` принимает три новых параметра
- `worker/cmd/worker/main.go`: инициализация `scannerClientForArchive` и передача в `converter.New()`; archive включается автоматически когда заданы `INGEST_SERVICE_TOKEN`, `SCANNER_API_URL`, `INGEST_SOURCE_REMOTE`
- `api/internal/handler/subtitles.go`: субтитры синхронизируются на storage-сервер через rclone после каждого Upload и Search; добавлены поля `storageRemote`, `storageRemotePath` и метод `syncSubtitlesToStorage()`
- `api/internal/config/config.go`: добавлены поля `StorageRemote`, `StorageRemotePath` (переменные `STORAGE_REMOTE`, `STORAGE_REMOTE_PATH`)
- `api/Dockerfile`: добавлен `rclone` в runtime Alpine image для синхронизации субтитров на storage-сервер
- `docker-compose.api.yml`: добавлены переменные окружения для rclone (`STORAGE_REMOTE`, `STORAGE_REMOTE_PATH`, `RCLONE_CONFIG_STORAGE_*`) и volume `./secrets:/secrets:ro`
- `docs/scanner/api.md`: добавлена документация нового endpoint `POST /api/v1/library/archive`

- `docs/architecture/target-production-architecture.md`: целевая продакшн-архитектура — многосерверная схема с CDN Edge в Азии для раздачи HLS в Бангладеш; описывает роли серверов, потоки данных, pull-caching, этапы перехода
- `infra/nginx/api-server.conf`: nginx конфиг для API-сервера (178.104.100.36) — admin.pimor.online, api.pimor.online, pimor.online
- `infra/nginx/storage-server.conf`: nginx конфиг для Storage-сервера (45.134.174.84) — media.pimor.online, player.pimor.online

### Changed
- `docs/architecture/deployment.md`: полностью переписан под многосерверную архитектуру — сводная таблица серверов, ролей, доменов, compose-стеков, nginx, потока данных
- `REPO_MAP.md`: добавлен `infra/nginx/`, `player/`, `docker-compose.worker.yml`, `.env.api.example`, `.env.worker.example`

### Removed
- `pimor.online.conf`: устаревший all-in-one nginx конфиг (старая архитектура, единый сервер) — заменён на `infra/nginx/`
- `ptrack.ink.conf`: устаревший nginx конфиг для домена ptrack.ink (старый домен)
- `docker-compose.api.yml`: compose-файл для API-сервера (postgres, redis, api, frontend) — без worker и torrent-сервисов
- `docker-compose.worker.yml`: compose-файл для сервера конвертации (worker, qbittorrent, prowlarr, flaresolverr) — подключается к внешним postgres и redis на API-сервере
- `.env.api.example`: переменные окружения для API-сервера
- `.env.worker.example`: переменные окружения для worker-сервера

### Changed
- `api/internal/service/job.go`: remote download jobs now store title in scanner-compatible normalized format `{slug}_{year}_[{tmdb_id}]` — matches incoming (scanner) job naming
- `worker/internal/repository/movie.go`: `buildStorageKey` rewritten to use scanner format instead of `Title(Year)` — storage directories now use lowercase slugs with underscores

### Removed
- `frontend/src/app/upload/page.tsx`: удалена вкладка «Локальная загрузка» — в разделённой архитектуре (API-сервер и Worker на разных машинах) файл загружается на диск API-сервера, но Worker не имеет к нему доступа; остался только «Удалённый каталог»
- `frontend/src/lib/api.ts`: удалены `uploadMovie`, `tmdbLookup`, `getUploadEndpoint`, `UPLOAD_PATH` — больше не используются

### Changed
- `api/internal/service/job.go`: remote downloads always use converter worker's `remote_download_queue`; scanner forwarding, `forwardToScanner`, and `isPrivateURL` removed — scanner is for ingest flow only
- `api/cmd/api/main.go`: `NewJobService` no longer receives `scannerAPIURL`/`serviceToken`
- `frontend/src/types/index.ts`: removed `'downloading'` from `DownloadItemState`
- `frontend/src/app/upload/page.tsx`: removed `downloading` transient state; download response always includes `job_id`

### Fixed
- `api/internal/service/job.go`: private/local IP URLs (10.x, 172.16-31.x, 192.168.x, 127.x) are now downloaded via converter worker's `remote_download_queue` instead of scanner server — scanner has no LAN access to these addresses
- `scanner/scanner/loops/download_worker.py`: switched from `urllib.request.urlretrieve` to `requests` with SOCKS5/HTTP proxy support; proxy_url fetched from DB per download

### Changed
- `scanner/scanner/api/server.py`: `POST /api/v1/downloads` now accepts optional `proxy_config` and stores it as `proxy_url` in DB
- `scanner/scanner/migrations/004_downloads_proxy.sql`: added `proxy_url` column to `scanner_downloads`
- `scanner/pyproject.toml`: added `requests[socks]` for SOCKS5 proxy support
- `api/internal/service/job.go`: `forwardToScanner` now passes `proxy_config` to scanner API when proxy is enabled

### Added
- `scanner/scanner/migrations/003_downloads_table.sql`: `scanner_downloads` table for tracking remote download tasks
- `scanner/scanner/loops/download_worker.py`: background thread that downloads queued URLs to `/incoming/` using `urllib.request`
- `scanner/scanner/api/server.py`: `POST /api/v1/downloads` endpoint — accepts URL + filename, creates download task in `scanner_downloads`
- `scanner/scanner/api/server.py`: `GET /api/v1/downloads` endpoint — returns last 100 download tasks ordered by created_at DESC
- `scanner/scanner/api/server.py`: `POST /api/v1/downloads/{id}/retry` endpoint — resets failed download back to queued
- `scanner/scanner/main.py`: added `download_worker` daemon thread
- `api/internal/handler/scanner.go`: `ScannerHandler` that proxies scanner download list and retry endpoints
- `api/internal/server/server.go`: `GET /api/admin/scanner/downloads` and `POST /api/admin/scanner/downloads/{id}/retry` routes
- `frontend/src/types/index.ts`: `ScannerDownload`, `ScannerDownloadStatus`, `ScannerDownloadsResponse` types
- `frontend/src/lib/api.ts`: `getScannerDownloads()` and `retryScannerDownload()` functions
- `frontend/src/app/queue/page.tsx`: scanner downloads section with status badges and retry button for failed items

### Changed
- `api/internal/service/job.go`: `CreateRemoteDownloadJob` forwards download to scanner API when `SCANNER_API_URL` is set, instead of enqueuing to `remote_download_queue`; added `scannerAPIURL` and `serviceToken` fields to `JobService` and updated `NewJobService` signature
- `api/internal/config/config.go`: added `ScannerAPIURL` and `IngestServiceToken` fields, read from `SCANNER_API_URL` and `INGEST_SERVICE_TOKEN` env vars
- `api/cmd/api/main.go`: pass `cfg.ScannerAPIURL` and `cfg.IngestServiceToken` to `NewJobService`
- `docker-compose.yml`: added `SCANNER_API_URL` and `INGEST_SERVICE_TOKEN` to `api` service environment; replaced `curl` with `wget` in frontend healthcheck (Next.js image has no curl)
- `frontend/src/app/upload/page.tsx`: handle absent `job_id` in remote download response — show "Скачивается…" state when scanner handles the download directly
- `frontend/src/types/index.ts`: added `'downloading'` to `DownloadItemState` union type
- `scanner/scanner/api/server.py`: `_claim_items` CTE now resets expired `claimed` items back to `registered` before selecting new candidates — prevents items from getting stuck after TTL expiry
- `scanner/scanner/loops/scan_loop.py`: `_scan_once` calls `_retry_failed_items()` each iteration — items with `status='failed'` and no `review_reason` are reset to `registered` after 30 min cooldown
- `.env.example`: increased `INGEST_CLAIM_TTL_SEC` default from 900 (15 min) to 7200 (2 hours) to cover large file rclone copies

### Changed
- `docker-compose.yml`: prowlarr, qbittorrent, flaresolverr moved to `profiles: [torrent]` — не стартуют при обычном `docker compose up`; для запуска: `docker compose --profile torrent up -d`
- `worker/internal/converter/converter.go`: source file is now deleted after conversion instead of being preserved in the final HLS directory — original is kept on scanner server in `/library`; removed `buildSourceFilename` and `normalizeFilenameSegment` helpers

### Fixed
- `scanner/scanner/loops/move_worker.py`: replaced `os.rename()` with `shutil.move()` and changed `/incoming` mount from `:ro` to `:rw` — `os.rename()` fails with `EXDEV` (errno 18) across different bind mounts; `shutil.move()` on the same physical disk (`/mnt/storage`) uses atomic rename; `:rw` required so scanner can delete source after move
- `scanner/docker-compose.yml`: changed `/incoming` mount from `:ro` to `:rw` — scanner needs write access to move processed files out of incoming
- `scanner/docker-compose.yml`: split `INCOMING_DIR`/`LIBRARY_DIR` into separate host (`INCOMING_HOST_DIR`/`LIBRARY_HOST_DIR`) and container vars — previously the same var was used for both docker-compose bind-mount source and the in-process path, causing the mount to point at the wrong host directory
- `docker-compose.yml`: added `RCLONE_CONFIG_SCANREMOTE_*` and all `INGEST_*` env vars to worker service; added `./secrets/scanner_rclone` mount — ingest worker was disabled because env vars were not passed into container

### Added
- `docker-compose.yml`: worker now mounts `./secrets/scanner_rclone:/secrets/scanner_rclone:ro` for SFTP access to scanner server
- `.env.example`: document `RCLONE_CONFIG_SCANREMOTE_*` vars for scanner SFTP remote
- `scanner/.env.example`: document `INCOMING_HOST_DIR`/`LIBRARY_HOST_DIR` (host paths) separate from `INCOMING_DIR`/`LIBRARY_DIR` (container paths)

### Changed
- `scanner/scanner/api/server.py`: replaced Flask with FastAPI + Uvicorn; identical HTTP contracts preserved
- `scanner/pyproject.toml`: replaced `flask>=3.0` with `fastapi>=0.110`, `uvicorn[standard]>=0.29`; added `httpx>=0.27` to dev deps
- `scanner/scanner/api/server.py`: scanner now exposes Flask HTTP API (`/api/v1/incoming/*`) instead of calling converter API
- `scanner/scanner/loops/scan_loop.py`: registers items directly to scanner DB (no external API call)
- `worker/internal/ingest/client.go`: IngestWorker now calls scanner HTTP API instead of converter API
- `worker/internal/ingest/worker.go`: worker creates media_job and enqueues convert job locally after rclone copy
- `worker/internal/config/config.go`: renamed `CONVERTER_API_URL` → `SCANNER_API_URL` for ingest worker

### Removed
- `api/internal/handler/ingest.go`: removed all `/api/ingest/incoming/*` endpoints from converter API
- `api/internal/service/ingest.go`: removed IngestService from converter API
- `api/internal/repository/incoming.go`: removed IncomingRepository from converter API
- `api/internal/model/incoming.go`: removed IncomingItem and related request/response models
- `scanner/scanner/loops/poll_loop.py`: removed poll loop (scanner no longer polls converter)
- `scanner/scanner/api/converter_client.py`: removed converter HTTP client from scanner

### Added

- `api/internal/db/migrations/013_drop_incoming_media_items.sql`: drop incoming_media_items from converter DB
- `scanner/scanner/migrations/002_add_claim_columns.sql`: add claimed_at/claim_expires_at to scanner_incoming_items
- `worker/internal/repository/job.go`: `CreateForIngest` method for idempotent job creation by ingest worker
- `docs/decisions/ADR-009-scanner-as-ingest-api-server.md`: documents ingest architecture inversion

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
- `api/internal/handler/ingest.go`, `api/internal/service/ingest.go`, `api/internal/server/server.go`: add `GET /api/ingest/incoming/{id}` for scanner poll_loop status checks
- `scanner/`: new Python scanner service — scans `incoming/`, GuessIt+TMDB normalization, ffprobe quality scoring, duplicate detection, registers files in converter API, moves to `library/movies/` after conversion
- `scanner/scanner/services/stability.py`: file stability detection (size unchanged for N seconds)
- `scanner/scanner/services/quality.py`: deterministic quality score (resolution+HDR+codec+bitrate, max ~100)
- `scanner/scanner/services/metadata.py`: GuessIt + TMDB + normalized_name generation
- `scanner/scanner/services/duplicates.py`: duplicate policy (upgrade threshold=8 points)
- `scanner/scanner/api/converter_client.py`: HTTP client to converter API (register + get_status)
- `scanner/scanner/loops/scan_loop.py`: scan_loop — walks incoming/, stability check, metadata pipeline, register or review
- `scanner/scanner/loops/poll_loop.py`: poll_loop — polls converter API every 60s, maps converter→local status
- `scanner/scanner/loops/move_worker.py`: move_worker — os.rename to library/movies/, upserts scanner_library_movies
- `scanner/docker-compose.yml`: autonomous deployment on storage server (postgres + scanner)
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

