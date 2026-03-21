# Job Cancellation Design

**Date:** 2026-03-21
**Status:** Approved

## Problem

When a job is deleted via `DELETE /api/admin/jobs/{jobID}`, the API removes the DB record but the worker goroutine continues running — holding a concurrency slot and consuming resources. New queued jobs cannot start until the slot is freed.

## Goal

When a job is deleted from the admin UI, the worker must immediately stop the active download (HTTP) or conversion (ffmpeg) for that job and free the concurrency slot.

Transfer stage (rclone) is explicitly out of scope — transfers are not cancelled.

---

## Design

### Signal mechanism: Redis cancel queue

API pushes the jobID as a plain string to a new Redis list `cancel_queue` (RPUSH) when `DeleteJob` is called. This follows the existing BLPOP pattern used by all other queues.

No new infrastructure. No pub/sub. Consistent with the rest of the codebase.

### CancelRegistry

New package: `worker/internal/cancelregistry`

```go
type Registry struct {
    mu      sync.Mutex
    cancels map[string]context.CancelFunc
}

func (r *Registry) Register(jobID string, cancel context.CancelFunc)
func (r *Registry) Cancel(jobID string)   // calls cancel func if present, no-op otherwise
func (r *Registry) Unregister(jobID string)
```

Single shared instance created in `main.go`, passed to workers that need it.

### Per-job context in workers

Both `httpdownloader.Worker` and `converter.Worker` accept a `*cancelregistry.Registry`.

In each `process()` method, before doing any work:

```go
jobCtx, cancel := context.WithCancel(ctx)
registry.Register(msg.JobID, cancel)
defer registry.Unregister(msg.JobID)
defer cancel()
```

All downstream calls use `jobCtx` instead of `ctx`. Since `http.NewRequestWithContext` and `exec.CommandContext` already respect context cancellation, no further changes are needed in the HTTP client or ffmpeg runner.

### Cancel watcher goroutine

New goroutine in `main.go`:

```go
go func() {
    for {
        raw, err := redisClient.Pop(ctx, queue.CancelQueue, 5*time.Second)
        if errors.Is(err, queue.ErrEmpty) { continue }
        if err != nil { ... continue }
        registry.Cancel(string(raw)) // raw is plain jobID, not JSON
    }
}()
```

Uses the existing `queue.Client.Pop` method. `CancelQueue = "cancel_queue"` added as a constant.

### API side

In `api/internal/service/job.go`, `DeleteJob` gains a queue dependency and pushes to cancel_queue after deleting from DB:

```go
_ = s.queue.Enqueue(ctx, queue.CancelQueue, jobID)
```

The push is best-effort (error ignored) — if Redis is unavailable, the job is still deleted from DB and the worker will naturally detect the missing record on its next status update attempt.

### Behaviour on cancellation

| Stage | What happens |
|---|---|
| HTTP download | `io.Copy` returns context error → partial file already removed by existing error handler → no retry, no `SetFailed` (job gone from DB) |
| ffmpeg conversion | `exec.CommandContext` kills the process → existing cleanup removes temp dir → no retry |
| Job already queued (not yet picked up) | Worker pops it, calls `IsTerminal`/DB lookup → not found → skips (already works today) |
| Cancel signal arrives after job completes | `registry.Cancel` is a no-op (jobID already unregistered) |

### Files changed

| File | Change |
|---|---|
| `worker/internal/cancelregistry/registry.go` | New package |
| `worker/internal/queue/redis.go` | Add `CancelQueue` constant; add `PushRaw` for plain string push |
| `worker/internal/httpdownloader/downloader.go` | Accept registry, per-job context |
| `worker/internal/converter/converter.go` | Accept registry, per-job context |
| `worker/cmd/worker/main.go` | Create registry, start cancel watcher goroutine, wire registry into workers |
| `api/internal/service/job.go` | Push jobID to cancel_queue after delete |
| `api/internal/queue/queue.go` | Add `CancelQueue` constant |

### What is NOT changed

- Transfer worker — rclone transfers are not cancellable (out of scope)
- Queue message format — cancel_queue carries plain jobID strings, not JSON envelopes
- No new API endpoints
- No new DB migrations
