# Series Critical Bugfixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix data corruption bugs in series support — episode storage key collisions, transfer worker breaking movie records, and recovery producing invalid payloads.

**Architecture:** Targeted fixes to existing code without restructuring. Each task fixes one specific bug identified in code review.

**Tech Stack:** Go 1.23, PostgreSQL 16

---

## File Structure

### Modified files

| File | Changes |
|---|---|
| `worker/internal/converter/converter.go:277` | Prefix episode storage_key with series key |
| `worker/internal/transfer/transfer.go:173-176` | Skip movie storage location update for episodes |
| `worker/internal/recovery/recovery.go:124-160` | Fix episode transfer payload reconstruction |
| `worker/internal/model/model.go:178-184` | Rename `MovieID` to `ContentID` in TransferJob |
| `api/internal/model/model.go:247-251` | Same rename in API model |
| `scanner/scanner/loops/scan_loop.py:206-269` | Add stability check for series folder episodes |
| `api/internal/handler/series.go:167-178` | Validate thumbnail path before serving |

---

## Task 1: Fix Episode Storage Key Collision

The episode `storage_key` is currently `s01e01` which is NOT globally unique. When a second series has S01E01, the UNIQUE constraint on `episodes.storage_key` will fail.

**Files:**
- Modify: `worker/internal/converter/converter.go:277`

- [ ] **Step 1: Fix episode storage key**

In `converter.go`, find line 277:
```go
epStorageKey := fmt.Sprintf("s%02de%02d", seasonNum, episodeNum)
```

Replace with:
```go
epStorageKey := fmt.Sprintf("%s_s%02de%02d", series.StorageKey, seasonNum, episodeNum)
```

This makes the storage key globally unique: `devil_may_cry_2025_[235930]_s01e01`.

- [ ] **Step 2: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Fix existing data in DB**

Run on API server:
```sql
UPDATE episodes e
SET storage_key = s.storage_key || '_' || e.storage_key
FROM seasons sn
JOIN series s ON s.id = sn.series_id
WHERE sn.id = e.season_id
  AND e.storage_key NOT LIKE '%\_%s%';
```

- [ ] **Step 4: Commit**

```bash
git add worker/internal/converter/converter.go
git commit -m "fix(worker): prefix episode storage_key with series key for uniqueness"
```

---

## Task 2: Fix Transfer Worker for Episodes

Transfer worker calls `movieRepo.UpdateStorageLocation(movieID)` for ALL content types, including episodes. For episodes, `MovieID` field actually contains `episodeID`, so it tries to update a non-existent movie record.

**Files:**
- Modify: `worker/internal/transfer/transfer.go:173-176`
- Modify: `worker/internal/model/model.go:178-184` (worker)
- Modify: `api/internal/model/model.go:247-251` (API)

- [ ] **Step 1: Rename MovieID to ContentID in TransferJob**

In `worker/internal/model/model.go`, replace:
```go
type TransferJob struct {
	MovieID     int64  `json:"movie_id"`
	StorageKey  string `json:"storage_key"`
	LocalPath   string `json:"local_path"`
	ContentType string `json:"content_type,omitempty"`
	EpisodeID   *int64 `json:"episode_id,omitempty"`
}
```

With:
```go
type TransferJob struct {
	ContentID   int64  `json:"content_id"`
	StorageKey  string `json:"storage_key"`
	LocalPath   string `json:"local_path"`
	ContentType string `json:"content_type,omitempty"`
}
```

Do the same in `api/internal/model/model.go`.

- [ ] **Step 2: Update converter to use ContentID**

In `converter.go`, find:
```go
Payload: model.TransferJob{
    MovieID:     contentID,
```

Replace `MovieID` with `ContentID`.

- [ ] **Step 3: Fix transfer worker to branch on content type**

In `transfer.go`, replace lines 173-176:
```go
// Update movie storage location in DB.
if err := w.movieRepo.UpdateStorageLocation(ctx, msg.Payload.MovieID, w.storageLocID); err != nil {
    log.Error("update storage_location_id failed", "error", err)
}
```

With:
```go
// Update storage location in DB — only for movies (episodes don't have storage_location_id).
if msg.Payload.ContentType != "episode" {
    if err := w.movieRepo.UpdateStorageLocation(ctx, msg.Payload.ContentID, w.storageLocID); err != nil {
        log.Error("update storage_location_id failed", "error", err)
    }
}
```

- [ ] **Step 4: Update all references to MovieID in transfer.go**

Find all `msg.Payload.MovieID` in `transfer.go` and replace with `msg.Payload.ContentID`. This includes log messages:
```go
log.Info("starting rclone transfer", "src", localPath, "dest", dest)
```

- [ ] **Step 5: Verify compilation**

Run: `cd worker && go build ./...` and `cd api && go build ./...`
Expected: Both build successfully.

- [ ] **Step 6: Commit**

```bash
git add worker/internal/model/model.go worker/internal/transfer/transfer.go worker/internal/converter/converter.go api/internal/model/model.go
git commit -m "fix(worker): rename TransferJob.MovieID to ContentID and skip storage location update for episodes"
```

---

## Task 3: Fix Recovery Episode Transfer Payload

`rebuildTransferPayload` sets `MovieID = episodeID` and `StorageKey = fmt.Sprintf("%d", episodeID)` for episodes, which is wrong on both counts.

**Files:**
- Modify: `worker/internal/recovery/recovery.go:124-160`

- [ ] **Step 1: Fix rebuildTransferPayload for episodes**

Replace the entire `rebuildTransferPayload` function:

```go
func rebuildTransferPayload(ctx context.Context, pool *pgxpool.Pool, jobID string) model.TransferJob {
	var tj model.TransferJob

	// Try movie asset first.
	var movieID int64
	var storagePath string
	err := pool.QueryRow(ctx,
		"SELECT a.movie_id, a.storage_path FROM media_assets a WHERE a.job_id = $1 AND a.is_ready = true LIMIT 1",
		jobID).Scan(&movieID, &storagePath)
	if err == nil {
		var storageKey string
		_ = pool.QueryRow(ctx, "SELECT storage_key FROM movies WHERE id = $1", movieID).Scan(&storageKey)
		tj.ContentID = movieID
		tj.StorageKey = storageKey
		tj.LocalPath = stripMasterPlaylist(storagePath)
		tj.ContentType = "movie"
		return tj
	}

	// Try episode asset.
	var episodeID int64
	err = pool.QueryRow(ctx,
		"SELECT ea.episode_id, ea.storage_path FROM episode_assets ea WHERE ea.job_id = $1 AND ea.is_ready = true LIMIT 1",
		jobID).Scan(&episodeID, &storagePath)
	if err == nil {
		// Derive series storage key via JOIN.
		var seriesStorageKey string
		var seasonNum, episodeNum int
		_ = pool.QueryRow(ctx, `
			SELECT s.storage_key, sn.season_number, e.episode_number
			FROM episodes e
			JOIN seasons sn ON sn.id = e.season_id
			JOIN series s ON s.id = sn.series_id
			WHERE e.id = $1`, episodeID,
		).Scan(&seriesStorageKey, &seasonNum, &episodeNum)

		tj.ContentID = episodeID
		tj.StorageKey = fmt.Sprintf("%s/s%02d/e%02d", seriesStorageKey, seasonNum, episodeNum)
		tj.LocalPath = stripMasterPlaylist(storagePath)
		tj.ContentType = "episode"
		return tj
	}

	slog.Warn("recovery: could not rebuild transfer payload", "job_id", jobID)
	return tj
}

func stripMasterPlaylist(storagePath string) string {
	const suffix = "/master.m3u8"
	if len(storagePath) > len(suffix) {
		return storagePath[:len(storagePath)-len(suffix)]
	}
	return storagePath
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add worker/internal/recovery/recovery.go
git commit -m "fix(worker): reconstruct proper episode storage key in recovery transfer payload"
```

---

## Task 4: Add Stability Check for Series Folder Episodes

Scanner `_process_series_folder` registers episodes immediately without checking if files are still being written. This can cause partially-copied files to be ingested.

**Files:**
- Modify: `scanner/scanner/loops/scan_loop.py:206-269`

- [ ] **Step 1: Add stability check to _process_series_folder**

In `_process_series_folder`, after the `file_path.stat().st_size` check, add a stability gate. Instead of inserting directly as `registered`, insert as `new` first, then let the regular `_process_file` flow handle stability and registration.

Replace the INSERT in `_process_series_folder` (lines 235-257) with:

```python
        conn = db.get_conn()
        try:
            with conn:
                with conn.cursor() as cur:
                    cur.execute(
                        "SELECT id, status FROM scanner_incoming_items WHERE source_path = %s",
                        (str(file_path),),
                    )
                    row = cur.fetchone()
                    if row is not None:
                        continue  # already registered, skip
                    cur.execute(
                        """
                        INSERT INTO scanner_incoming_items
                            (source_path, source_filename, file_size_bytes, status,
                             content_kind, normalized_name, tmdb_id,
                             series_tmdb_id, season_number, episode_number)
                        VALUES (%s, %s, %s, 'new', 'episode', %s, %s, %s, %s, %s)
                        """,
                        (
                            str(file_path),
                            file_path.name,
                            current_size,
                            ep_normalized,
                            series_tmdb_id,
                            series_tmdb_id,
                            ep["season"],
                            ep["episode"],
                        ),
                    )
                    logger.info(
                        "series episode detected: %s S%02dE%02d (pending stability check)",
                        file_path.name, ep["season"], ep["episode"],
                    )
        except Exception:
            logger.exception("failed to register episode %s", file_path)
        finally:
            db.put_conn(conn)
```

- [ ] **Step 2: Update _handle_stable_episode to promote new → registered**

The existing `_handle_stable_episode` already does `UPDATE ... WHERE status='new'` which will promote these items. But it also sets `normalized_name` and `content_kind` — verify it doesn't overwrite the values already set by `_process_series_folder`.

Check that `_handle_stable_episode` uses `UPDATE ... SET ... WHERE source_path=%s AND status='new'` — if the episode was already registered with correct metadata by `_process_series_folder`, this UPDATE just changes status to `registered`. The normalized_name and tmdb_id will be re-computed but should produce the same values.

No code change needed here if `_handle_stable_episode` already handles this correctly.

- [ ] **Step 3: Commit**

```bash
git add scanner/scanner/loops/scan_loop.py
git commit -m "fix(scanner): add stability check for series folder episodes"
```

---

## Task 5: Validate Thumbnail Path Before Serving

`EpisodeThumbnail` serves files from DB-stored paths without validation. Defense-in-depth: ensure path is under expected media root.

**Files:**
- Modify: `api/internal/handler/series.go:167-178`

- [ ] **Step 1: Add path validation**

Replace the `EpisodeThumbnail` handler body:

```go
func (h *SeriesHandler) EpisodeThumbnail(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	episodeID, err := strconv.ParseInt(chi.URLParam(r, "episodeId"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid episode id", false, cid)
		return
	}
	asset, err := h.seriesRepo.GetEpisodeAsset(r.Context(), episodeID)
	if err != nil || asset.ThumbnailPath == nil {
		http.NotFound(w, r)
		return
	}
	// Validate path is under expected directory.
	clean := filepath.Clean(*asset.ThumbnailPath)
	if !strings.HasPrefix(clean, "/media/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, clean)
}
```

Add `"path/filepath"` and `"strings"` to imports if not already present.

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add api/internal/handler/series.go
git commit -m "fix(api): validate episode thumbnail path before serving"
```

---

## Dependency Graph

```
Task 1 (episode storage key) ── independent
Task 2 (transfer ContentID)  ── independent
Task 3 (recovery payload)    ── depends on Task 2 (uses ContentID)
Task 4 (stability check)     ── independent
Task 5 (path validation)     ── independent
```

Tasks 1, 2, 4, 5 can be executed in parallel. Task 3 must follow Task 2.
