# Job Cancellation Design

**Date:** 2026-03-21
**Status:** Approved

## Problem

When a job is deleted via `DELETE /api/admin/jobs/{jobID}`, the API removes the DB record but the worker goroutine continues running — holding a concurrency slot and consuming resources. New queued jobs cannot start until the slot is freed.

## Goal

When a job is deleted from the admin UI, the worker must immediately stop the active download (HTTP) or conversion (ffmpeg) for that job and free the concurrency slot.

**Out of scope:**
- Transfer worker (rclone) — not cancelled
- Torrent downloader (`download_queue`) — already handles deletion: `waitForDownloadOrCancel` polls `Exists()` every 5 seconds and exits naturally when the job is gone from DB
- Ingest worker — not cancelled

---

## Design

### Signal mechanism: Redis cancel queue

API pushes the jobID to a new Redis list `cancel_queue` (RPUSH) when `DeleteJob` is called. Wire format: JSON-marshalled string — e.g. `"job_01abc123"` (with quotes). This is the output of `json.Marshal(jobID)` and is consistent with how both `api/internal/queue` and `worker/internal/queue` clients work — both call `json.Marshal(v)` before push. The consumer does `json.Unmarshal(raw, &jobID)`.

No new infrastructure. No pub/sub. No `PushRaw`. Consistent with existing patterns.

### Fix IsTerminal for missing rows

`worker/internal/repository/job.go:IsTerminal` currently returns `(false, err)` when the job row does not exist (`pgx.ErrNoRows`). Workers guard with `if err != nil || terminal { return }`, so deleted jobs are skipped — but via a side-effect of the error path, with a misleading log "skipping terminal job".

As part of this change, fix `IsTerminal` to explicitly return `(true, nil)` on `pgx.ErrNoRows` — "a deleted job is treated as terminal." This makes the intent explicit and the log message accurate.

### CancelRegistry

New package: `worker/internal/cancelregistry`

```go
type Registry struct {
    mu      sync.Mutex
    cancels map[string]context.CancelFunc
}

func (r *Registry) Register(jobID string, cancel context.CancelFunc)
func (r *Registry) Cancel(jobID string)    // no-op if jobID not registered
func (r *Registry) Unregister(jobID string)
```

Single shared instance created in `main.go`, passed to workers that need it.

### Per-job context in workers

Both `httpdownloader.Worker` and `converter.Worker` accept a `*cancelregistry.Registry`.

In each `process()` method, before doing any work:

```go
jobCtx, jobCancel := context.WithCancel(ctx)   // ctx = global SIGTERM context
registry.Register(msg.JobID, jobCancel)
defer registry.Unregister(msg.JobID)
defer jobCancel()
```

All downstream calls use `jobCtx`. Since `http.NewRequestWithContext` and `exec.CommandContext` already respect context cancellation, no changes are needed in the HTTP client or ffmpeg runner.

**Lock release:** `ReleaseLock` must be called with the global `ctx` (not `jobCtx`), because `jobCtx` may already be cancelled at defer time. If called with a cancelled context, the Redis `DEL` fails silently and the lock persists for up to 1 hour (the TTL). Fix:

```go
defer w.q.ReleaseLock(ctx, msg.JobID)  // ctx = global, not jobCtx
```

### Cancel watcher goroutine

New goroutine in `main.go`:

```go
go func() {
    for {
        if ctx.Err() != nil { return }
        raw, err := redisClient.Pop(ctx, queue.CancelQueue, 5*time.Second)
        if errors.Is(err, queue.ErrEmpty) { continue }
        if err != nil { ... continue }
        var jobID string
        if err := json.Unmarshal(raw, &jobID); err != nil { continue }
        registry.Cancel(jobID)
    }
}()
```

Uses the existing `queue.Client.Pop`. `CancelQueue = "cancel_queue"` added as a constant to both `api/internal/queue` and `worker/internal/queue`.

### API side

In `api/internal/service/job.go`, `JobService` gains a queue dependency (already has it for other operations). After deleting from DB:

```go
_ = s.queue.Enqueue(ctx, queue.CancelQueue, jobID)  // best-effort, error ignored
```

Push is best-effort — if Redis is unavailable, the job is still deleted from DB; the worker will detect the missing record via `IsTerminal` on its next status update.

### Behaviour on cancellation

| Stage | What happens |
|---|---|
| HTTP download (active) | `io.Copy` returns context error → partial file removed by existing error handler → no retry, no `SetFailed` (job gone from DB) |
| ffmpeg conversion (active) | `exec.CommandContext` kills ffmpeg → existing cleanup removes temp dir |
| Job queued but not yet picked up | Worker pops it → `IsTerminal` returns `(true, nil)` (fixed) → skipped cleanly |
| Cancel signal after job completes | `registry.Cancel` is a no-op (jobID already unregistered) |

### Files changed

| File | Change |
|---|---|
| `worker/internal/cancelregistry/registry.go` | New package |
| `worker/internal/queue/redis.go` | Add `CancelQueue` constant |
| `worker/internal/repository/job.go` | Fix `IsTerminal` to return `(true, nil)` on `pgx.ErrNoRows` |
| `worker/internal/httpdownloader/downloader.go` | Accept registry; per-job context; fix `ReleaseLock` to use global ctx |
| `worker/internal/converter/converter.go` | Accept registry; per-job context; fix `ReleaseLock` to use global ctx |
| `worker/cmd/worker/main.go` | Create registry; start cancel watcher goroutine; wire registry into workers |
| `api/internal/queue/queue.go` | Add `CancelQueue` constant |
| `api/internal/service/job.go` | Push jobID to `cancel_queue` after delete |
