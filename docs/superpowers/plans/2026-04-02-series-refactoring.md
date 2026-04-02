# Series Architecture Refactoring Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify series support architecture — reduce duplication, centralize path logic, optimize queries, and make movies/episodes symmetric.

**Architecture:** Extract shared PathResolver for all path operations; optimize N+1 queries with JOINs; unify response builders in API handlers; add episode subtitle support in converter.

**Tech Stack:** Go 1.23, PostgreSQL 16, Python 3.12, Next.js/React

**Depends on:** `2026-04-02-series-bugfixes.md` (Plan A) must be completed first.

---

## File Structure

### New files

| File | Purpose |
|---|---|
| `worker/internal/paths/paths.go` | Centralized path resolution for movies and series |

### Modified files

| File | Changes |
|---|---|
| `worker/internal/converter/converter.go` | Use PathResolver, remove inline path logic |
| `worker/internal/transfer/transfer.go` | Use PathResolver |
| `worker/internal/recovery/recovery.go` | Use PathResolver |
| `worker/internal/converter/converter.go` | Remove `transferStorageKey()` helper |
| `api/internal/repository/series.go` | Add `GetSeriesWithEpisodes()` JOIN query |
| `api/internal/handler/series.go` | Use single JOIN query in Get handler |
| `api/internal/handler/player.go` | Extract shared episode payload builder, reduce duplication |
| `worker/internal/converter/converter.go` | Add episode subtitle fetching |
| `worker/internal/repository/subtitle.go` | Add `UpsertEpisodeSubtitle()` |

---

## Task 1: Create PathResolver

Centralize all path construction logic into one module. Currently paths are built in converter.go, transfer.go, recovery.go, and transferStorageKey() — all with slightly different approaches.

**Files:**
- Create: `worker/internal/paths/paths.go`

- [ ] **Step 1: Create paths module**

```go
// Package paths provides centralized media path resolution.
package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Resolver builds filesystem and storage paths for media content.
type Resolver struct {
	mediaRoot string
}

// New creates a Resolver with the given media root (e.g. "/media").
func New(mediaRoot string) *Resolver {
	return &Resolver{mediaRoot: mediaRoot}
}

// MovieFinalDir returns the local path for converted movie HLS output.
// Example: /media/converted/movies/inception_2010_[16662]
func (r *Resolver) MovieFinalDir(storageKey string) string {
	return filepath.Join(r.mediaRoot, "converted", "movies", storageKey)
}

// EpisodeFinalDir returns the local path for converted episode HLS output.
// Example: /media/converted/series/devil_may_cry_2025_[235930]/s01/e02
func (r *Resolver) EpisodeFinalDir(seriesStorageKey string, season, episode int) string {
	return filepath.Join(r.mediaRoot, "converted", "series", seriesStorageKey,
		fmt.Sprintf("s%02d", season), fmt.Sprintf("e%02d", episode))
}

// MovieTransferKey returns the rclone destination relative path for a movie.
// Example: movies/inception_2010_[16662]
func (r *Resolver) MovieTransferKey(storageKey string) string {
	return "movies/" + storageKey
}

// EpisodeTransferKey returns the rclone destination relative path for an episode.
// Example: series/devil_may_cry_2025_[235930]/s01/e02
func (r *Resolver) EpisodeTransferKey(seriesStorageKey string, season, episode int) string {
	return fmt.Sprintf("series/%s/s%02d/e%02d", seriesStorageKey, season, episode)
}

// TransferKeyFromFinalDir extracts the transfer key from a local final directory path.
// This is used when we need to derive the storage key from an already-built path.
func (r *Resolver) TransferKeyFromFinalDir(finalDir, contentType string) string {
	if contentType == "episode" {
		prefix := filepath.Join(r.mediaRoot, "converted", "series") + "/"
		if rel := strings.TrimPrefix(filepath.ToSlash(finalDir), filepath.ToSlash(prefix)); rel != filepath.ToSlash(finalDir) {
			return "series/" + rel
		}
	}
	return "movies/" + filepath.Base(finalDir)
}

// DownloadsDir returns the path for raw downloads.
func (r *Resolver) DownloadsDir(jobID string) string {
	return filepath.Join(r.mediaRoot, "downloads", jobID)
}

// TempDir returns the FFmpeg working directory.
func (r *Resolver) TempDir(jobID string) string {
	return filepath.Join(r.mediaRoot, "temp", jobID)
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd worker && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add worker/internal/paths/paths.go
git commit -m "refactor(worker): create centralized PathResolver for media paths"
```

---

## Task 2: Use PathResolver in Converter

Replace inline path construction in converter.go with PathResolver calls.

**Files:**
- Modify: `worker/internal/converter/converter.go`

- [ ] **Step 1: Add PathResolver to Worker struct**

Add `paths *paths.Resolver` field to the Worker struct. Add it to `New()` constructor. Remove `transferStorageKey()` helper function.

- [ ] **Step 2: Replace path construction**

Replace movie final dir (line ~295):
```go
// Before:
finalDir = filepath.Join(w.mediaRoot, "converted", "movies", movie.StorageKey)
// After:
finalDir = w.paths.MovieFinalDir(movie.StorageKey)
```

Replace episode final dir (lines ~283-284):
```go
// Before:
finalDir = filepath.Join(w.mediaRoot, "converted", "series", series.StorageKey,
    fmt.Sprintf("s%02d", seasonNum), fmt.Sprintf("e%02d", episodeNum))
// After:
finalDir = w.paths.EpisodeFinalDir(series.StorageKey, seasonNum, episodeNum)
```

Replace transfer storage key (line ~453):
```go
// Before:
StorageKey: transferStorageKey(finalDir, w.mediaRoot, contentType),
// After:
StorageKey: w.paths.TransferKeyFromFinalDir(finalDir, contentType),
```

Replace downloads dir (line ~410):
```go
// Before:
downloadsDir := filepath.Join(w.mediaRoot, "downloads", msg.JobID)
// After:
downloadsDir := w.paths.DownloadsDir(msg.JobID)
```

- [ ] **Step 3: Wire up in main.go**

In `worker/cmd/worker/main.go`, create PathResolver and pass to converter:
```go
pathResolver := paths.New(cfg.MediaRoot)
cvWorker := converter.New(..., pathResolver)
```

- [ ] **Step 4: Delete transferStorageKey()**

Remove the `transferStorageKey()` function from converter.go — replaced by PathResolver.

- [ ] **Step 5: Verify compilation and commit**

```bash
cd worker && go build ./...
git commit -am "refactor(worker): use PathResolver in converter"
```

---

## Task 3: Use PathResolver in Transfer and Recovery

**Files:**
- Modify: `worker/internal/transfer/transfer.go`
- Modify: `worker/internal/recovery/recovery.go`

- [ ] **Step 1: Update transfer worker**

Replace dest path construction (lines 100-109):
```go
// Before:
contentDir := "movies"
if msg.Payload.ContentType == "episode" {
    contentDir = "series"
}
dest := fmt.Sprintf("%s:%s/%s/%s/", ...)

// After — StorageKey already contains the full relative path (e.g. "series/key/s01/e02"):
dest := fmt.Sprintf("%s:%s/%s/",
    w.rcloneRemote,
    filepath.ToSlash(w.remotePath),
    msg.Payload.StorageKey,
)
```

Note: This works because converter now puts the full transfer key (including `movies/` or `series/` prefix) in StorageKey via `PathResolver.TransferKeyFromFinalDir()`.

- [ ] **Step 2: Update recovery**

Pass PathResolver to recovery.Run() or use the JOIN query from Task 3 of bugfixes plan (which already derives the correct storage key).

- [ ] **Step 3: Verify and commit**

```bash
cd worker && go build ./...
git commit -am "refactor(worker): use PathResolver in transfer and recovery"
```

---

## Task 4: Optimize N+1 Queries in Series API

Replace the loop of queries in `GET /api/admin/series/{id}` and `GET /api/player/series` with a single JOIN query.

**Files:**
- Modify: `api/internal/repository/series.go`
- Modify: `api/internal/handler/series.go`
- Modify: `api/internal/handler/player.go`

- [ ] **Step 1: Add GetSeriesDetail repository method**

In `api/internal/repository/series.go`, add:

```go
// SeriesDetail contains a series with all its seasons, episodes, and asset readiness.
type SeriesDetail struct {
	Series   *model.Series
	Seasons  []SeasonDetail
}

type SeasonDetail struct {
	Season   *model.Season
	Episodes []EpisodeDetail
}

type EpisodeDetail struct {
	Episode      *model.Episode
	HasAsset     bool
	ThumbnailPath *string
	AssetID      *string
}

// GetSeriesDetail fetches a series with all nested data in a single query.
func (r *SeriesRepository) GetSeriesDetail(ctx context.Context, seriesID int64) (*SeriesDetail, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.storage_key, s.tmdb_id, s.imdb_id, s.title, s.year, s.poster_url, s.created_at, s.updated_at,
		       sn.id, sn.season_number, sn.poster_url, sn.created_at, sn.updated_at,
		       e.id, e.episode_number, e.title, e.storage_key, e.created_at, e.updated_at,
		       ea.asset_id, ea.thumbnail_path, (ea.asset_id IS NOT NULL) AS has_asset
		FROM series s
		LEFT JOIN seasons sn ON sn.series_id = s.id
		LEFT JOIN episodes e ON e.season_id = sn.id
		LEFT JOIN episode_assets ea ON ea.episode_id = e.id AND ea.is_ready = true
		WHERE s.id = $1
		ORDER BY sn.season_number, e.episode_number`,
		seriesID)
	if err != nil {
		return nil, fmt.Errorf("get series detail: %w", err)
	}
	defer rows.Close()

	// Parse into nested structure...
	// (reconstruct SeriesDetail from flat rows, grouping by season)
}
```

This replaces 1 + N_seasons + N_episodes queries with 1 query.

- [ ] **Step 2: Update admin handler to use single query**

Replace the Get handler loop with:
```go
detail, err := h.seriesRepo.GetSeriesDetail(r.Context(), seriesID)
```

- [ ] **Step 3: Update player handler similarly**

Replace the seasons/episodes loop in GetSeries with a similar approach, adding audio track and subtitle fetching per episode (these still need separate queries but are fewer overall).

- [ ] **Step 4: Verify and commit**

```bash
cd api && go build ./...
git commit -am "perf(api): optimize series detail queries with single JOIN"
```

---

## Task 5: Add Episode Subtitle Fetching

Currently subtitles are only fetched for movies. Add episode subtitle support in the converter.

**Files:**
- Modify: `worker/internal/converter/converter.go`
- Modify: `worker/internal/repository/subtitle.go` (or create episode_subtitle.go)

- [ ] **Step 1: Add UpsertEpisodeSubtitle to worker repository**

```go
func (r *SubtitleRepository) UpsertEpisodeSubtitle(
	ctx context.Context, episodeID int64, language, source, storagePath string, externalID *string,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO episode_subtitles (episode_id, language, source, storage_path, external_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (episode_id, language) DO UPDATE SET
			storage_path = EXCLUDED.storage_path,
			external_id = EXCLUDED.external_id,
			updated_at = NOW()`,
		episodeID, language, source, storagePath, externalID)
	return err
}
```

- [ ] **Step 2: Add subtitle fetch for episodes in converter**

After the movie subtitle block (line ~437), add:
```go
// Episode subtitles (best-effort).
if contentType == "episode" && w.subtitleFetcher != nil && msg.Payload.TMDBID != "" {
	results := w.subtitleFetcher.FetchAndSave(jobCtx, msg.Payload.TMDBID, finalDir)
	for _, sub := range results {
		extID := &sub.ExternalID
		if sub.ExternalID == "" {
			extID = nil
		}
		if err := w.subtitleRepo.UpsertEpisodeSubtitle(jobCtx, contentID, sub.Language, "opensubtitles", sub.FilePath, extID); err != nil {
			log.Warn("episode subtitle upsert failed", "lang", sub.Language, "error", err)
		}
	}
	log.Info("episode subtitles fetched", "count", len(results))
}
```

- [ ] **Step 3: Verify and commit**

```bash
cd worker && go build ./...
git commit -am "feat(worker): add subtitle fetching for series episodes"
```

---

## Task 6: Extract Shared Response Builders in Player Handler

Reduce duplication between movie and series response building in player.go.

**Files:**
- Modify: `api/internal/handler/player.go`

- [ ] **Step 1: Extract buildAudioTracksPayload helper**

```go
func (h *PlayerHandler) buildAudioTracksPayload(ctx context.Context, assetID, assetType string) []map[string]any {
	tracks, err := h.audioTrackRepo.ListByAsset(ctx, assetID, assetType)
	if err != nil || len(tracks) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tracks))
	for i, t := range tracks {
		td := map[string]any{"index": t.TrackIndex, "is_default": t.IsDefault}
		if t.Language != nil { td["language"] = *t.Language }
		if t.Label != nil { td["label"] = *t.Label }
		result[i] = td
	}
	return result
}
```

- [ ] **Step 2: Extract buildSubtitlesPayload helper**

```go
func (h *PlayerHandler) buildSubtitlesPayload(ctx context.Context, subs []subtitleEntry, baseURL string) []map[string]string {
	result := make([]map[string]string, len(subs))
	for i, sub := range subs {
		result[i] = map[string]string{
			"language": sub.Language,
			"url":      h.maybeSignMediaURL(sub.URL),
		}
	}
	return result
}
```

- [ ] **Step 3: Use helpers in GetMovie and GetSeries**

Replace the inline audio track and subtitle building in both handlers with calls to the shared helpers.

- [ ] **Step 4: Unify URL builders**

Replace `buildMovieMediaURL` and `buildSeriesMediaURL` with a single `buildMediaURL`:

```go
func buildMediaURL(baseURL, relativePath string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return "/" + relativePath
	}
	return trimmed + "/" + relativePath
}
```

Callers pass the relative path:
- Movies: `buildMediaURL(baseURL, fmt.Sprintf("movies/%s/%s", storageKey, file))`
- Series: `buildMediaURL(baseURL, fmt.Sprintf("series/%s/s%02d/e%02d/%s", key, s, e, file))`

- [ ] **Step 5: Verify and commit**

```bash
cd api && go build ./...
git commit -am "refactor(api): extract shared response builders in player handler"
```

---

## Dependency Graph

```
Task 1 (PathResolver)
├── Task 2 (converter uses it)
└── Task 3 (transfer + recovery use it)

Task 4 (N+1 queries) ── independent
Task 5 (episode subtitles) ── independent
Task 6 (shared builders) ── independent
```

Tasks 1→2→3 are sequential. Tasks 4, 5, 6 are independent of each other and of Tasks 1-3.
