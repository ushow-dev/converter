# Full Project Refactoring Design

**Date:** 2026-04-02
**Status:** Draft
**Scope:** All services — API, Worker, Scanner, Frontend, Player

---

## Overview

Comprehensive refactoring of the entire codebase to reduce duplication, decompose large files, improve reliability, and clean up frontend/player. Organized into 4 independent phases that can be deployed separately.

---

## Phase 1: Shared Packages (Foundation)

### Problem
Models and constants are duplicated between `api/internal/model/` and `worker/internal/model/`. Queue payload structs are defined nearly identically in both services. Error handling is inconsistent — no shared error types.

### Solution
Create `shared/` packages at the root level, importable by both api and worker Go modules.

#### 1.1 Shared Models

Move to `shared/model/`:
- `queue.go` — all queue payload structs: DownloadPayload, ConvertMessage, RemoteDownloadMessage, TransferMessage and their inner Job types
- `status.go` — JobStatus, JobStage, JobPriority constants
- `proxy.go` — ProxyConfig struct
- `constants.go` — ContentTypeMovie, ContentTypeEpisode, SourceType constants

Both `api/internal/model/model.go` and `worker/internal/model/model.go` remove duplicated definitions and import from shared.

Domain-specific models stay in their services:
- `api/internal/model/` keeps Movie, Series, Asset, Subtitle (with JSON tags)
- `worker/internal/model/` keeps Movie, Series, Asset (without JSON tags, different field set)

#### 1.2 Shared Errors

Create `shared/errors/errors.go`:
- `ErrNotFound` — entity not found
- `ErrConflict` — duplicate/conflict
- `ErrValidation` — validation failed
- Wrapping helpers: `Wrap(err, msg)`, `IsNotFound(err)`

API and worker repositories use these instead of per-package sentinels.

#### 1.3 Go Module Structure

Since api/ and worker/ are separate Go modules, shared/ becomes a third module:
```
shared/
  go.mod        (module app/shared)
  model/
  errors/
```

Both api/go.mod and worker/go.mod add `replace app/shared => ../shared`.

---

## Phase 2: Large File Decomposition

### Problem
Several files exceed 300-700 lines with multiple responsibilities mixed together.

### Solution
Split each large file into focused components. No behavior changes — pure restructuring.

#### 2.1 converter.go (761 lines → 3 files)

| New file | Responsibility | Approx lines |
|---|---|---|
| `converter.go` | Worker struct, Run() loop, process() pipeline orchestration | ~300 |
| `tmdb.go` | fetchTMDBMetadata, fetchTMDBTVMetadata, downloadImage, tmdbMetadata struct | ~120 |
| `archive.go` | archiveToScanner, parseQuality | ~100 |

Helpers stay in converter.go: backoffDelay, generateAssetID, nullableText, failJob, failOrRequeue.

#### 2.2 player.go (676 lines → 3 files)

| New file | Responsibility | Approx lines |
|---|---|---|
| `player_movie.go` | GetMovie, GetAsset, GetJobStatus, GetCatalog | ~200 |
| `player_series.go` | GetSeries, GetEpisode, buildEpisodePayload, repositoryEpisodeView | ~200 |
| `player_media.go` | PlayerHandler struct, NewPlayerHandler, media signing, URL builders, P2P metrics, shared helpers (buildAudioTracksPayload, buildMediaURL, maybeSignMediaURL) | ~250 |

#### 2.3 job.go service (498 lines → 3 files)

| New file | Responsibility | Approx lines |
|---|---|---|
| `job.go` | JobService struct, shared helpers (buildNormalizedName, checkDuplicate, DuplicateError) | ~100 |
| `job_create.go` | CreateJob (torrent), CreateUploadJob | ~200 |
| `job_remote.go` | CreateRemoteDownloadJob, remote download helpers | ~150 |

#### 2.4 browse.go (467 lines → 2 files)

| New file | Responsibility | Approx lines |
|---|---|---|
| `browse.go` | BrowseHandler, Browse endpoint | ~200 |
| `browse_parser.go` | findDirs, extractSize, regex helpers, URL parsing | ~200 |

---

## Phase 3: Reliability

### Problem
Magic numbers, no repository interfaces, inconsistent queue envelopes, overly permissive Docker permissions.

### Solution

#### 3.1 Fix Magic Storage Location ID

Current: `const remoteStorageLocID = int64(2)` hardcoded in worker/main.go.

Fix: Query database at startup to find active remote storage location:
```go
loc, err := storageLocRepo.GetActiveRemote(ctx)
if err != nil {
    slog.Warn("no remote storage location found, transfer disabled")
}
```

#### 3.2 Repository Interfaces

Define interfaces in each service for testability:
```go
// api/internal/repository/interfaces.go
type MovieReader interface {
    GetByIMDbID(ctx context.Context, id string) (*model.Movie, error)
    GetByTMDBID(ctx context.Context, id string) (*model.Movie, error)
    List(ctx context.Context, limit int, cursor string) ([]*model.Movie, string, error)
}
```

Handlers and services depend on interfaces, not concrete types. This enables unit testing with mocks.

#### 3.3 Cancel Queue Envelope

Current: raw string job ID pushed to cancel_queue.
Fix: Wrap in standard envelope:
```json
{
  "schema_version": "1",
  "job_id": "job_abc123",
  "created_at": "2026-04-02T..."
}
```

#### 3.4 Centralize Content Type Constants

Current: string literals "movie", "episode", "series" scattered across code.
Fix: Constants in shared/model/constants.go, used everywhere:
```go
const (
    ContentTypeMovie   = "movie"
    ContentTypeEpisode = "episode"
)
```

Remove "serials" and "tv" from mediaSigningPath — use only "movies" and "series".

#### 3.5 Docker Permissions

Current: `chmod -R 777 /media` in worker startup.
Fix: Use `0o755` for directories, `0o644` for files. Run qBittorrent container with specific UID matching media dir ownership.

---

## Phase 4: Frontend + Player + Scanner

### Problem
Large component files, duplicated patterns, PlayerClient uses MovieResponse shim for episodes.

### Solution

#### 4.1 Player — Generic PlaybackData

Replace MovieResponse with a generic interface:

```typescript
interface PlaybackData {
  hls: string
  poster?: string
  subtitles?: { language: string; url: string }[]
}
```

PlayerClient accepts `PlaybackData` instead of `MovieResponse`. SeriesPlayer and page.tsx construct PlaybackData directly from their API responses — no more `episodeToMovieResponse()` shim with fake `movie: { id: 0, imdb_id: '' }`.

#### 4.2 Player — Extract hooks and constants

| New file | Content |
|---|---|
| `player/src/app/useHlsPlayer.ts` | HLS init, Fluid Player setup, quality/audio state, ad handling — extracted from PlayerClient |
| `player/src/app/PlayerControls.tsx` | Quality selector, audio selector, settings menu UI |
| `player/src/app/constants.ts` | HLS_CONFIG, SUBTITLE_LABELS, normalizeLanguageCode, subtitleLabel |

PlayerClient becomes a thin wrapper: renders video element, mounts controls, delegates to useHlsPlayer hook.

#### 4.3 Player — Unified page.tsx fetch

Replace separate `fetchMovieData()` and `fetchSeriesData()` with single `fetchPlaybackData(params)` that routes based on `type` parameter.

#### 4.4 Frontend — Extract components

| Page | Extract to |
|---|---|
| `movies/page.tsx` (473 lines) | `MovieTable.tsx` (table + rows), `MoviePlayerModal.tsx` (modal), `SubtitleCell.tsx` (already exists partially) |
| `series/[id]/page.tsx` (448 lines) | `SeasonAccordion.tsx`, `EpisodeTable.tsx`, `SeriesHeader.tsx` |

#### 4.5 Frontend — Pagination hook

Create `frontend/src/hooks/usePaginatedList.ts`:
```typescript
function usePaginatedList<T>(fetchUrl: (limit: number, cursor?: string) => string) {
  // Returns: items, loading, error, loadMore, hasMore
}
```

Used by both movies and series list pages — eliminates duplicated pagination state management.

#### 4.6 Scanner — Constants and data layer

- Move `VIDEO_EXTENSIONS` to `scanner/scanner/constants.py` — single source
- Extract DB queries from `server.py` into `scanner/scanner/data/queries.py`
- Keep server.py as thin FastAPI routing layer

---

## Phase Dependencies

```
Phase 1 (shared packages) — foundation, do first
    ↓
Phase 2 (file decomposition) — uses shared imports
    ↓
Phase 3 (reliability) — uses interfaces from decomposed files

Phase 4 (frontend/player/scanner) — fully independent, can run in parallel with 2 or 3
```

## Decisions

| Decision | Rationale |
|---|---|
| Separate shared/ Go module | Both api/ and worker/ need same types; go.mod replace directive keeps it simple |
| Split by responsibility, not layer | `tmdb.go` groups all TMDB logic vs splitting by handler/service/repo |
| Interfaces only in Phase 3 | Phase 2 splits files first, then Phase 3 adds interfaces to clean boundaries |
| PlaybackData replaces MovieResponse | Eliminates semantic dishonesty (episodes pretending to be movies) |
| No unified Asset table in this refactoring | Architectural change too large; defer to separate spec |
