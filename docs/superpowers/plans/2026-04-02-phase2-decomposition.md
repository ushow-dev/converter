# Phase 2: Large File Decomposition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split 4 oversized Go files (converter 761 lines, player handler 676, job service 498, browse handler 467) into focused components. Pure restructuring — no behavior changes.

**Architecture:** Each large file is split into 2-3 files by responsibility. Structs and methods move to new files in the same package. Public interfaces unchanged. All existing tests and callers remain valid.

**Tech Stack:** Go 1.23

**Spec:** `docs/superpowers/specs/2026-04-02-full-refactoring-design.md` — Phase 2

**Depends on:** Phase 1 (shared packages) should be completed first.

---

## Task 1: Split converter.go (761 → 3 files)

**Files:**
- Modify: `worker/internal/converter/converter.go` (keep pipeline orchestration)
- Create: `worker/internal/converter/tmdb.go` (TMDB metadata fetching)
- Create: `worker/internal/converter/archive.go` (scanner archive logic)

- [ ] **Step 1: Extract tmdb.go**

Move these functions from converter.go to a new `tmdb.go`:
- `tmdbMetadata` struct
- `fetchTMDBMetadata()` function
- `fetchTMDBTVMetadata()` function
- `downloadImage()` function

All functions are package-private — no import changes needed. Just move them to a new file in the same package.

- [ ] **Step 2: Extract archive.go**

Move these functions from converter.go to `archive.go`:
- `archiveToScanner()` method on Worker
- `parseQuality()` function

- [ ] **Step 3: Verify**

Run: `cd worker && go build ./...`
Expected: Build succeeds — same package, same visibility.

- [ ] **Step 4: Commit**

```bash
git add worker/internal/converter/
git commit -m "refactor(worker): split converter.go into tmdb.go and archive.go"
```

---

## Task 2: Split player.go (676 → 3 files)

**Files:**
- Modify: `api/internal/handler/player.go` (keep shared infra)
- Create: `api/internal/handler/player_movie.go` (movie endpoints)
- Create: `api/internal/handler/player_series.go` (series endpoints)

- [ ] **Step 1: Extract player_movie.go**

Move from player.go to `player_movie.go`:
- `GetMovie()` method
- `GetAsset()` method
- `GetJobStatus()` method
- `GetCatalog()` method
- `getMovieByIMDbID()`, `getMovieByTMDBID()` helpers
- `repositoryMovieView` struct
- `buildMovieMediaURL()` function (keep `buildMediaURL` in player.go)

- [ ] **Step 2: Extract player_series.go**

Move from player.go to `player_series.go`:
- `GetSeries()` method
- `GetEpisode()` method
- `buildEpisodePayload()`, `buildEpisodeAudioTracks()`, `buildEpisodeSubtitles()` helpers
- `repositoryEpisodeView` struct
- `buildSeriesMediaURL()` function

- [ ] **Step 3: Keep in player.go**

player.go retains:
- `PlayerHandler` struct and `NewPlayerHandler()`
- `resolveBaseURL()`, `maybeSignMediaURL()`, `buildMediaURL()`
- `buildAudioTracksPayload()` (shared helper)
- `mediaURLSigner` and `mediaSigningPath()` and `storagePathToPlaybackURL()`
- P2P metrics (PostP2PMetrics, P2PMetricsSnapshot, atomic counters)

- [ ] **Step 4: Verify**

Run: `cd api && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add api/internal/handler/player*.go
git commit -m "refactor(api): split player.go into movie and series handlers"
```

---

## Task 3: Split job.go service (498 → 3 files)

**Files:**
- Modify: `api/internal/service/job.go` (keep shared logic)
- Create: `api/internal/service/job_create.go` (torrent + upload job creation)
- Create: `api/internal/service/job_remote.go` (remote download job creation)

- [ ] **Step 1: Extract job_create.go**

Move from job.go:
- `CreateJobRequest` struct
- `CreateJob()` method (torrent job creation)
- `CreateUploadJob()` method
- File-handling helpers used by upload

- [ ] **Step 2: Extract job_remote.go**

Move from job.go:
- `CreateRemoteDownloadJob()` method
- `RemoteDownloadRequest` struct (if exists)
- Remote download-specific helpers

- [ ] **Step 3: Keep in job.go**

job.go retains:
- `JobService` struct and `NewJobService()`
- `GetJob()`, `ListJobs()`, `DeleteJob()` (read operations)
- `buildNormalizedName()` (shared helper)
- `checkDuplicate()` (shared helper)
- `DuplicateError` type

- [ ] **Step 4: Verify**

Run: `cd api && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add api/internal/service/job*.go
git commit -m "refactor(api): split job service into create and remote download modules"
```

---

## Task 4: Split browse.go (467 → 2 files)

**Files:**
- Modify: `api/internal/handler/browse.go` (keep handler)
- Create: `api/internal/handler/browse_parser.go` (parsing helpers)

- [ ] **Step 1: Extract browse_parser.go**

Move from browse.go:
- All regex-based parsing functions (findDirs, extractSize, etc.)
- URL manipulation helpers
- Any struct types used only for parsing

- [ ] **Step 2: Verify**

Run: `cd api && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add api/internal/handler/browse*.go
git commit -m "refactor(api): extract browse parsing helpers to browse_parser.go"
```

---

## Dependency Graph

```
Task 1 (converter split) — independent
Task 2 (player split) — independent
Task 3 (job service split) — independent
Task 4 (browse split) — independent
```

All 4 tasks are independent and can be executed in any order or in parallel.
