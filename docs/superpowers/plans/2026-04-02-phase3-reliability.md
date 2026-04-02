# Phase 3: Reliability Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix magic numbers, add repository interfaces for testability, wrap cancel queue in envelope, centralize content type handling, fix Docker permissions.

**Architecture:** Targeted fixes to specific reliability gaps. Each task is independent and deployable on its own.

**Tech Stack:** Go 1.23, Docker

**Spec:** `docs/superpowers/specs/2026-04-02-full-refactoring-design.md` — Phase 3

---

## Task 1: Fix Magic Storage Location ID

The worker hardcodes `const remoteStorageLocID = int64(2)` tied to a specific migration. If storage location 2 is deleted or renumbered, transfer silently writes wrong data.

**Files:**
- Modify: `worker/cmd/worker/main.go`
- Modify: `worker/internal/repository/storage_location.go` (or create)

- [ ] **Step 1: Add GetActiveRemote to storage location repository**

In the worker's storage location repository (find or create `worker/internal/repository/storage_location.go`), add:

```go
// GetActiveRemoteID returns the ID of the first active remote storage location.
// Returns 0 if none found.
func (r *StorageLocationRepository) GetActiveRemoteID(ctx context.Context) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		"SELECT id FROM storage_locations WHERE is_active = true AND type != 'local' ORDER BY id LIMIT 1",
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
```

- [ ] **Step 2: Replace magic number in main.go**

Replace:
```go
const remoteStorageLocID = int64(2)
```

With:
```go
var remoteStorageLocID int64
if cfg.RcloneRemote != "" {
    storageLocRepo := repository.NewStorageLocationRepository(pool)
    id, err := storageLocRepo.GetActiveRemoteID(ctx)
    if err != nil || id == 0 {
        slog.Error("no active remote storage location found, transfer disabled")
    } else {
        remoteStorageLocID = id
        slog.Info("remote storage location resolved", "id", id)
    }
}
```

- [ ] **Step 3: Verify and commit**

```bash
cd worker && go build ./...
git commit -am "fix(worker): resolve storage location ID from database instead of hardcoding"
```

---

## Task 2: Add Repository Interfaces

Define interfaces for key repositories to enable unit testing with mocks.

**Files:**
- Create: `api/internal/repository/interfaces.go`
- Create: `worker/internal/repository/interfaces.go`

- [ ] **Step 1: Create API repository interfaces**

```go
// interfaces.go — repository interfaces for dependency injection and testing.
package repository

import (
	"context"

	"app/api/internal/model"
)

type MovieReader interface {
	GetByIMDbID(ctx context.Context, imdbID string) (*model.Movie, error)
	GetByTMDBID(ctx context.Context, tmdbID string) (*model.Movie, error)
	GetByID(ctx context.Context, id int64) (*model.Movie, error)
	List(ctx context.Context, limit int, cursor string) ([]*model.Movie, string, error)
}

type AssetReader interface {
	GetByID(ctx context.Context, assetID string) (*model.Asset, error)
	GetByJobID(ctx context.Context, jobID string) (*model.Asset, error)
	GetByMovieID(ctx context.Context, movieID int64) (*model.Asset, error)
}

type SeriesReader interface {
	GetByTMDBID(ctx context.Context, tmdbID string) (*model.Series, error)
	GetByID(ctx context.Context, id int64) (*model.Series, error)
	List(ctx context.Context, limit int, cursor string) ([]*model.Series, string, error)
	ListSeasons(ctx context.Context, seriesID int64) ([]*model.Season, error)
	ListEpisodes(ctx context.Context, seasonID int64) ([]*model.Episode, error)
}
```

- [ ] **Step 2: Create worker repository interfaces**

Similar pattern for worker-side repositories:
```go
package repository

import "context"

type JobWriter interface {
	UpdateStatus(ctx context.Context, jobID, status string, stage *string, progress int) error
	SetFailed(ctx context.Context, jobID, errorCode, errorMessage string, retryable bool) error
	SetCompleted(ctx context.Context, jobID string) error
	IsTerminal(ctx context.Context, jobID string) (bool, error)
}
```

- [ ] **Step 3: Verify and commit**

```bash
cd api && go build ./... && cd ../worker && go build ./...
git commit -am "refactor: add repository interfaces for testability"
```

Note: Handlers and services continue using concrete types for now. Interfaces are available for future test files to use as mock targets.

---

## Task 3: Wrap Cancel Queue in Envelope

Current cancel queue pushes raw string job IDs with no schema versioning.

**Files:**
- Modify: `api/internal/handler/jobs.go` (cancel producer)
- Modify: `worker/cmd/worker/main.go` (cancel consumer)

- [ ] **Step 1: Update cancel producer**

Find where cancel messages are pushed in api handler. Replace raw string push with CancelMessage struct (defined in shared/model/queue.go from Phase 1):

```go
cancelMsg := sharedmodel.CancelMessage{
    SchemaVersion: "1",
    JobID:         jobID,
    CreatedAt:     time.Now().UTC(),
}
```

- [ ] **Step 2: Update cancel consumer**

In worker/main.go cancel watcher loop, replace:
```go
var jobID string
if err := json.Unmarshal(raw, &jobID); err != nil { ... }
```

With:
```go
var msg sharedmodel.CancelMessage
if err := json.Unmarshal(raw, &msg); err != nil {
    // Backward compatibility: try raw string.
    var jobID string
    if err2 := json.Unmarshal(raw, &jobID); err2 != nil {
        slog.Error("unmarshal cancel message", "error", err, "raw", string(raw))
        continue
    }
    msg.JobID = jobID
}
slog.Info("cancelling job", "job_id", msg.JobID)
registry.Cancel(msg.JobID)
```

- [ ] **Step 3: Verify and commit**

```bash
cd api && go build ./... && cd ../worker && go build ./...
git commit -am "refactor: wrap cancel queue messages in schema-versioned envelope"
```

---

## Task 4: Clean Up mediaSigningPath Content Types

Remove obsolete "serials" and "tv" from media signing path. Only "movies" and "series" are used.

**Files:**
- Modify: `api/internal/handler/player.go` (or `player_media.go` after Phase 2)

- [ ] **Step 1: Simplify mediaSigningPath**

Find `mediaSigningPath()` function. Replace:
```go
(parts[0] == "movies" || parts[0] == "series" || parts[0] == "serials" || parts[0] == "tv")
```
With:
```go
(parts[0] == "movies" || parts[0] == "series")
```

- [ ] **Step 2: Verify and commit**

```bash
cd api && go build ./...
git commit -am "fix(api): remove obsolete content type aliases from media signing path"
```

---

## Task 5: Fix Docker Permissions

**Files:**
- Modify: `worker/cmd/worker/main.go`

- [ ] **Step 1: Change chmod from 777 to 755**

Find the media directory setup loop. Replace:
```go
if err := os.MkdirAll(dir, 0o777); err != nil {
    ...
}
_ = chmodR(dir, 0o777)
```

With:
```go
if err := os.MkdirAll(dir, 0o755); err != nil {
    ...
}
_ = chmodR(dir, 0o755)
```

- [ ] **Step 2: Verify and commit**

```bash
cd worker && go build ./...
git commit -am "fix(worker): use 755 directory permissions instead of 777"
```

---

## Dependency Graph

All 5 tasks are independent. Can be executed in any order.

Task 3 (cancel envelope) depends on Phase 1 being completed (CancelMessage struct in shared module). If Phase 1 is not done yet, define CancelMessage locally in worker model.
