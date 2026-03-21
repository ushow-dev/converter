# Job Cancellation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a job is deleted via the admin UI, immediately stop the active HTTP download or ffmpeg conversion in the worker and free the concurrency slot.

**Architecture:** API pushes the jobID as a JSON string to Redis `cancel_queue` on delete. A dedicated watcher goroutine in the worker BLPOPs from `cancel_queue` and calls `registry.Cancel(jobID)`. Each active job runs with a per-job derived context; cancelling that context aborts the in-flight HTTP request or kills the ffmpeg process via existing context-aware APIs.

**Tech Stack:** Go 1.22, Redis (go-redis/v9), pgx/v5, standard library `context`, `sync`

> **Note:** Go is not installed locally — all build and test verification uses Docker: `docker compose build worker` (on converter server) or via the worker Dockerfile. Unit tests for pure Go packages (no DB/Redis) can be run inside a temporary container.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `worker/internal/cancelregistry/registry.go` | Create | Thread-safe map of jobID → cancelFunc |
| `worker/internal/queue/redis.go` | Modify | Add `CancelQueue` constant |
| `worker/internal/repository/job.go` | Modify | Fix `IsTerminal` to return `(true, nil)` on missing row |
| `worker/internal/httpdownloader/downloader.go` | Modify | Accept registry; per-job context; fix ReleaseLock |
| `worker/internal/converter/converter.go` | Modify | Accept registry; per-job context; fix ReleaseLock |
| `worker/cmd/worker/main.go` | Modify | Create registry; start cancel watcher goroutine; wire workers |
| `api/internal/queue/redis.go` | Modify | Add `CancelQueue` constant |
| `api/internal/service/job.go` | Modify | Push jobID to cancel_queue after DB delete |
| `CHANGELOG.md` | Modify | Document the change |

---

## Chunk 1: Foundation — CancelRegistry + queue constant + IsTerminal fix

### Task 1: Create CancelRegistry package

**Files:**
- Create: `worker/internal/cancelregistry/registry.go`

- [ ] **Step 1: Create the file**

```go
// Package cancelregistry tracks per-job cancel functions so that an external
// signal (e.g. from a Redis cancel queue) can abort an in-flight job.
package cancelregistry

import (
	"context"
	"sync"
)

// Registry is a thread-safe map of job ID → context.CancelFunc.
// Register a cancel func before starting work; call Cancel to abort it;
// call Unregister when the job finishes (success or failure).
type Registry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{cancels: make(map[string]context.CancelFunc)}
}

// Register stores cancel for the given jobID, replacing any previous entry.
func (r *Registry) Register(jobID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[jobID] = cancel
}

// Cancel calls the cancel func for jobID if one is registered; no-op otherwise.
func (r *Registry) Cancel(jobID string) {
	r.mu.Lock()
	cancel, ok := r.cancels[jobID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

// Unregister removes the entry for jobID from the registry.
func (r *Registry) Unregister(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, jobID)
}
```

- [ ] **Step 2: Commit**

```bash
git add worker/internal/cancelregistry/registry.go
git commit -m "feat(worker): add CancelRegistry for per-job context cancellation"
```

---

### Task 2: Add CancelQueue constant to both queue packages

**Files:**
- Modify: `worker/internal/queue/redis.go`
- Modify: `api/internal/queue/redis.go`

- [ ] **Step 1: Add constant to worker queue**

In `worker/internal/queue/redis.go`, in the `const` block, add:
```go
CancelQueue = "cancel_queue"
```

Full const block becomes:
```go
const (
	DownloadQueue       = "download_queue"
	ConvertQueue        = "convert_queue"
	RemoteDownloadQueue = "remote_download_queue"
	TransferQueue       = "transfer_queue"
	CancelQueue         = "cancel_queue"

	lockTTL = time.Hour
)
```

- [ ] **Step 2: Add constant to API queue**

In `api/internal/queue/redis.go`, in the `const` block, add:
```go
CancelQueue = "cancel_queue"
```

Full const block becomes:
```go
const (
	DownloadQueue       = "download_queue"
	ConvertQueue        = "convert_queue"
	RemoteDownloadQueue = "remote_download_queue"
	TransferQueue       = "transfer_queue"
	CancelQueue         = "cancel_queue"
)
```

- [ ] **Step 3: Commit**

```bash
git add worker/internal/queue/redis.go api/internal/queue/redis.go
git commit -m "feat(queue): add CancelQueue constant to api and worker queue packages"
```

---

### Task 3: Fix IsTerminal to treat missing row as terminal

**Files:**
- Modify: `worker/internal/repository/job.go` (lines 89–100)

- [ ] **Step 1: Update IsTerminal**

Replace the current implementation:

```go
// IsTerminal returns true if the job is already in a terminal state
// (completed or failed). Used to guard against duplicate processing.
func (r *JobRepository) IsTerminal(ctx context.Context, jobID string) (bool, error) {
	var status string
	err := r.pool.QueryRow(ctx,
		"SELECT status FROM media_jobs WHERE job_id = $1", jobID).
		Scan(&status)
	if err != nil {
		return false, fmt.Errorf("get status %s: %w", jobID, err)
	}
	return status == "completed" || status == "failed", nil
}
```

With:

```go
// IsTerminal returns true if the job is already in a terminal state
// (completed or failed), or if the job no longer exists in the DB
// (treated as terminal — the job was deleted while queued).
func (r *JobRepository) IsTerminal(ctx context.Context, jobID string) (bool, error) {
	var status string
	err := r.pool.QueryRow(ctx,
		"SELECT status FROM media_jobs WHERE job_id = $1", jobID).
		Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("get status %s: %w", jobID, err)
	}
	return status == "completed" || status == "failed", nil
}
```

- [ ] **Step 2: Add required imports**

Add to the import block in `worker/internal/repository/job.go`:
```go
"errors"
"github.com/jackc/pgx/v5"
```

(Check if `errors` is already imported; if so, only add the pgx import.)

- [ ] **Step 3: Commit**

```bash
git add worker/internal/repository/job.go
git commit -m "fix(worker): IsTerminal returns true for deleted jobs (pgx.ErrNoRows)"
```

---

## Chunk 2: Worker wiring — httpdownloader + converter + main

### Task 4: Wire CancelRegistry into httpdownloader

**Files:**
- Modify: `worker/internal/httpdownloader/downloader.go`

The current `Worker` struct has fields: `q`, `jobRepo`, `mediaRoot`.
The current `process(ctx, raw)` uses `ctx` (global) directly for all operations.
`ReleaseLock` is called via `defer w.q.ReleaseLock(ctx, msg.JobID)` — this `ctx` will remain global (correct).

- [ ] **Step 1: Add registry field to Worker struct**

Change the struct and constructor:

```go
type Worker struct {
	q         *queue.Client
	jobRepo   *repository.JobRepository
	mediaRoot string
	registry  *cancelregistry.Registry
}

func New(q *queue.Client, jobRepo *repository.JobRepository, mediaRoot string, registry *cancelregistry.Registry) *Worker {
	return &Worker{
		q:        q,
		jobRepo:  jobRepo,
		mediaRoot: mediaRoot,
		registry: registry,
	}
}
```

- [ ] **Step 2: Add import for cancelregistry**

Add to the import block:
```go
"app/worker/internal/cancelregistry"
```

- [ ] **Step 3: Update process() to use per-job context**

At the top of `process()`, after the lock is acquired (after `defer w.q.ReleaseLock(ctx, msg.JobID)`), add:

```go
// Per-job cancellable context. The global ctx (SIGTERM) is the parent.
// ReleaseLock above captures the global ctx intentionally — so that
// lock release still works even when jobCtx is cancelled.
jobCtx, jobCancel := context.WithCancel(ctx)
w.registry.Register(msg.JobID, jobCancel)
defer func() {
    jobCancel()
    w.registry.Unregister(msg.JobID)
}()
```

Then replace every subsequent use of `ctx` in `process()` (after lock acquisition) with `jobCtx`. Specifically:
- `w.jobRepo.UpdateStatus(ctx, ...)` → `w.jobRepo.UpdateStatus(jobCtx, ...)`
- `w.downloadWithProgress(ctx, ...)` → `w.downloadWithProgress(jobCtx, ...)`
- `w.jobRepo.UpdateStatus(ctx, ...)` (convert stage transition) → `w.jobRepo.UpdateStatus(jobCtx, ...)`
- `w.q.Push(ctx, ...)` (enqueue convert) → `w.q.Push(jobCtx, ...)`

Note: `w.q.ReleaseLock(ctx, msg.JobID)` stays as global `ctx` — already set up before per-job context creation.

- [ ] **Step 4: Verify cancellation is clean**

When `jobCtx` is cancelled mid-download, `downloadWithProgress` returns a context error. The existing error handler in `process()` already checks `if ctx.Err() != nil { return }` — update this check to use `jobCtx.Err()`:

```go
if err := w.downloadWithProgress(jobCtx, client, msg.JobID, msg.Payload.SourceURL, destPath, log); err != nil {
    if jobCtx.Err() != nil {
        // Cancelled (job deleted) — don't retry, job is gone from DB.
        log.Info("download cancelled", "job_id", msg.JobID)
        _ = os.Remove(destPath)
        return
    }
    w.failOrRequeue(ctx, msg, "DOWNLOAD_ERROR", err.Error(), true)
    return
}
```

- [ ] **Step 5: Commit**

```bash
git add worker/internal/httpdownloader/downloader.go
git commit -m "feat(worker): wire CancelRegistry into httpdownloader for per-job cancellation"
```

---

### Task 5: Wire CancelRegistry into converter

**Files:**
- Modify: `worker/internal/converter/converter.go`

The converter's lock key is `msg.JobID + "_convert"`. The lock is released via `defer w.q.ReleaseLock(ctx, lockKey)` — this should stay as global `ctx`.

- [ ] **Step 1: Add registry field to Worker struct**

Add to the struct (after existing fields):
```go
registry *cancelregistry.Registry
```

- [ ] **Step 2: Add registry parameter to New()**

`New()` currently has a long parameter list. Add `registry *cancelregistry.Registry` as the last parameter before the closing `)`:

```go
func New(
    q *queue.Client,
    jobRepo *repository.JobRepository,
    assetRepo *repository.AssetRepository,
    movieRepo *repository.MovieRepository,
    subtitleFetcher *subtitles.Fetcher,
    subtitleRepo *repository.SubtitleRepository,
    mediaRoot string,
    tmdbAPIKey string,
    ffmpegThreads int,
    transferEnabled bool,
    scannerClient *ingest.Client,
    ingestSourceRemote string,
    archiveDestPath string,
    registry *cancelregistry.Registry,
) *Worker {
    return &Worker{
        q: q, jobRepo: jobRepo, assetRepo: assetRepo, movieRepo: movieRepo,
        subtitleFetcher: subtitleFetcher, subtitleRepo: subtitleRepo,
        mediaRoot: mediaRoot, tmdbAPIKey: tmdbAPIKey, ffmpegThreads: ffmpegThreads,
        transferEnabled:    transferEnabled,
        scannerClient:      scannerClient,
        ingestSourceRemote: ingestSourceRemote,
        archiveDestPath:    archiveDestPath,
        registry:           registry,
    }
}
```

- [ ] **Step 3: Add import for cancelregistry**

Add to the import block:
```go
"app/worker/internal/cancelregistry"
```

- [ ] **Step 4: Update process() to use per-job context**

In `process()`, after `defer w.q.ReleaseLock(ctx, lockKey)` (line ~130), add:

```go
// Per-job cancellable context. ReleaseLock above captures global ctx intentionally.
jobCtx, jobCancel := context.WithCancel(ctx)
w.registry.Register(msg.JobID, jobCancel)
defer func() {
    jobCancel()
    w.registry.Unregister(msg.JobID)
}()
```

Then replace all subsequent uses of `ctx` in `process()` with `jobCtx`. Key callsites:
- `w.jobRepo.UpdateStatus(ctx, ...)` (in_progress) → `jobCtx`
- `ffmpeg.RunHLS(ctx, ...)` → `jobCtx`
- `ffmpeg.Thumbnail(ctx, ...)` → `jobCtx`
- `fetchTMDBMetadata(ctx, ...)` → `jobCtx`
- All DB writes (asset, movie upsert, etc.) → `jobCtx`
- `w.jobRepo.UpdateProgress(ctx, ...)` inside progress callback → `jobCtx`
- Archive/scanner calls → `jobCtx` (cancellable if job deleted mid-archive; acceptable)

Also add a cancellation check after `ffmpeg.RunHLS`:

```go
result, err := ffmpeg.RunHLS(jobCtx, inputPath, outputDir, 4, w.ffmpegThreads, func(pct int) {
    _ = w.jobRepo.UpdateProgress(jobCtx, msg.JobID, pct)
    log.Info("convert progress", "pct", pct)
})
if err != nil {
    if jobCtx.Err() != nil {
        log.Info("convert cancelled", "job_id", msg.JobID)
        _ = os.RemoveAll(outputDir)
        return
    }
    w.failOrRequeue(ctx, msg, "FFMPEG_ERROR", err.Error(), false)
    return
}
```

Note: `w.failOrRequeue(ctx, ...)`, `w.failJob(ctx, ...)`, and the final completion `UpdateStatus` calls (e.g. `StatusCompleted`, `stage = 100`) keep global `ctx` — these write to DB and should succeed even if `jobCtx` was cancelled.

- [ ] **Step 5: Commit**

```bash
git add worker/internal/converter/converter.go
git commit -m "feat(worker): wire CancelRegistry into converter for per-job cancellation"
```

---

### Task 6: Create cancel watcher goroutine and wire everything in main.go

**Files:**
- Modify: `worker/cmd/worker/main.go`

- [ ] **Step 1: Add missing imports**

The current `main.go` import block is missing the following — all must be added:
```go
"app/worker/internal/cancelregistry"
"encoding/json"
"errors"
"time"
```

(`time` is needed for `5*time.Second` and `time.Sleep` in the cancel watcher; `encoding/json` and `errors` for Unmarshal and `errors.Is`; `cancelregistry` for the new registry.)

- [ ] **Step 2: Create the registry and update worker constructors**

After Redis and Postgres setup (around line 96), add:

```go
// ── Cancel registry ────────────────────────────────────────────────────────
registry := cancelregistry.New()
```

Update `httpdownloader.New(...)` call (line ~129):
```go
httpDlWorker := httpdownloader.New(redisClient, jobRepo, cfg.MediaRoot, registry)
```

Update `converter.New(...)` call (line ~125), adding `registry` as the last argument:
```go
cvWorker := converter.New(redisClient, jobRepo, assetRepo, movieRepo,
    subtitleFetcher, subtitleRepo, cfg.MediaRoot, cfg.TMDBAPIKey, cfg.FFmpegThreads,
    cfg.RcloneRemote != "", scannerClientForArchive,
    cfg.IngestSourceRemote, cfg.ArchiveDestPath, registry)
```

- [ ] **Step 3: Start cancel watcher goroutine**

Place this block after `var wg sync.WaitGroup` (line ~146) and after the health server goroutine `go health.Start(...)` — both must already be in scope. In practice, insert this immediately before the "Download worker(s)" loop:

```go
// ── Cancel watcher ─────────────────────────────────────────────────────────
// Reads job IDs from cancel_queue and aborts in-flight jobs via the registry.
wg.Add(1)
go func() {
    defer wg.Done()
    slog.Info("cancel watcher started")
    for {
        if ctx.Err() != nil {
            slog.Info("cancel watcher stopped")
            return
        }
        raw, err := redisClient.Pop(ctx, queue.CancelQueue, 5*time.Second)
        if errors.Is(err, queue.ErrEmpty) {
            continue
        }
        if err != nil {
            if ctx.Err() != nil {
                return
            }
            slog.Error("cancel queue pop error", "error", err)
            time.Sleep(time.Second)
            continue
        }
        var jobID string
        if err := json.Unmarshal(raw, &jobID); err != nil {
            slog.Error("unmarshal cancel message", "error", err, "raw", string(raw))
            continue
        }
        slog.Info("cancelling job", "job_id", jobID)
        registry.Cancel(jobID)
    }
}()
```

- [ ] **Step 4: Verify the build compiles**

```bash
docker compose -f docker-compose.worker.yml build worker 2>&1 | tail -20
```

Expected: build succeeds (no compile errors).

- [ ] **Step 5: Commit**

```bash
git add worker/cmd/worker/main.go
git commit -m "feat(worker): start cancel watcher goroutine; wire CancelRegistry into workers"
```

---

## Chunk 3: API side + deploy

### Task 7: Push to cancel_queue when job is deleted

**Files:**
- Modify: `api/internal/service/job.go`

- [ ] **Step 1: Update DeleteJob to push cancel signal**

The current `DeleteJob` (line 314) does DB delete + file cleanup. Add a best-effort push after the DB delete succeeds:

```go
func (s *JobService) DeleteJob(ctx context.Context, jobID string) error {
	meta, err := s.jobs.Delete(ctx, jobID)
	if err != nil {
		return err
	}

	// Signal the worker to cancel any in-flight processing for this job.
	// Best-effort: if Redis is unavailable, the job is already gone from DB
	// and the worker will detect deletion via IsTerminal on its next DB check.
	_ = s.queue.Enqueue(ctx, queue.CancelQueue, jobID)

	// Best-effort filesystem cleanup — ignore missing dirs.
	for _, sub := range []string{"downloads", "converted", "temp"} {
		_ = os.RemoveAll(filepath.Join(s.mediaRoot, sub, jobID))
	}
	if meta != nil && meta.StoragePath != nil {
		_ = os.RemoveAll(filepath.Dir(*meta.StoragePath))
	}
	return nil
}
```

- [ ] **Step 2: Verify the API build compiles**

```bash
docker compose -f docker-compose.api.yml build api 2>&1 | tail -20
```

Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add api/internal/service/job.go
git commit -m "feat(api): push cancel signal to cancel_queue when job is deleted"
```

---

### Task 8: Update CHANGELOG and deploy

- [ ] **Step 1: Update CHANGELOG.md**

Add under `## [Unreleased]` → `### Added`:

```markdown
### Added
- `worker/internal/cancelregistry/registry.go`: new CancelRegistry — thread-safe map of jobID → cancelFunc for per-job context cancellation
- `worker/cmd/worker/main.go`: cancel watcher goroutine BLPOPs from `cancel_queue` and calls `registry.Cancel(jobID)`
- `api/internal/queue/redis.go`, `worker/internal/queue/redis.go`: `CancelQueue = "cancel_queue"` constant

### Fixed
- `worker/internal/repository/job.go`: `IsTerminal` now returns `(true, nil)` for deleted jobs (`pgx.ErrNoRows`) instead of a misleading error
- `worker/internal/httpdownloader/downloader.go`: per-job context cancellation; `ReleaseLock` uses global ctx; cancelled downloads abort cleanly without retry
- `worker/internal/converter/converter.go`: per-job context cancellation; `ReleaseLock` uses global ctx; cancelled ffmpeg processes are killed cleanly
- `api/internal/service/job.go`: `DeleteJob` pushes jobID to `cancel_queue` so the worker stops in-flight work immediately
```

- [ ] **Step 2: Commit changelog**

```bash
git add CHANGELOG.md
git commit -m "docs(docs): update CHANGELOG for job cancellation feature"
```

- [ ] **Step 3: Push to origin**

```bash
git push origin main
```

- [ ] **Step 4: Deploy API**

```bash
ssh -i ~/.ssh/id_rsa_personal root@178.104.100.36 \
  'cd /opt/converter && git pull origin main && \
   docker compose -f docker-compose.api.yml build api && \
   docker compose -f docker-compose.api.yml up -d api'
```

Expected: API container recreated successfully.

- [ ] **Step 5: Deploy Worker**

```bash
ssh -i ~/.ssh/id_ed25519 root@178.104.53.215 \
  'cd /opt/converter && git pull origin main && \
   docker compose -f docker-compose.worker.yml build worker && \
   docker compose -f docker-compose.worker.yml up -d worker'
```

Expected: Worker container recreated successfully.

- [ ] **Step 6: Smoke test**

Start a remote download job in the admin UI. While it is downloading, delete it. Check worker logs:

```bash
ssh -i ~/.ssh/id_ed25519 root@178.104.53.215 \
  'docker logs converter-worker-1 --tail 30 2>&1'
```

Expected: log line `"msg":"download cancelled"` or `"msg":"cancelling job"` for the deleted job ID. New queued jobs should start immediately after.
