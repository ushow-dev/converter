# Transfer Stage Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a `transfer` job stage with a real-time rclone progress bar on the queue page and job detail page.

**Architecture:** Extend the existing job pipeline so that when `RCLONE_REMOTE` is set, the converter transitions the job to `in_progress/stage=transfer` instead of `completed` after HLS. The transfer worker parses rclone stderr stats and updates job progress every ~2 seconds, then marks the job `completed` when done.

**Tech Stack:** Go 1.23 (pgx v5, log/slog, os/exec), Next.js 14 (TypeScript)

**Note:** This project has no test files. Skip all TDD steps — implement directly and verify by building.

**Spec:** `docs/superpowers/specs/2026-03-17-transfer-stage-design.md`

---

## Chunk 1: Backend Models and Repository

**Files:**
- Modify: `api/internal/model/model.go` (JobStage constants, line 21-24)
- Modify: `worker/internal/model/model.go` (stage constants, line 14-15)
- Modify: `worker/internal/repository/job.go` (add two new methods after line 43)

---

- [ ] **Step 1: Add `JobStageTransfer` to API model**

File: `api/internal/model/model.go`, lines 21-24. Change:

```go
const (
	JobStageDownload JobStage = "download"
	JobStageConvert  JobStage = "convert"
)
```

To:

```go
const (
	JobStageDownload JobStage = "download"
	JobStageConvert  JobStage = "convert"
	JobStageTransfer JobStage = "transfer"
)
```

- [ ] **Step 2: Add `StageTransfer` to worker model**

File: `worker/internal/model/model.go`, lines 14-15. Change:

```go
	StageDownload = "download"
	StageConvert  = "convert"
```

To:

```go
	StageDownload  = "download"
	StageConvert   = "convert"
	StageTransfer  = "transfer"
```

- [ ] **Step 3: Add `SetStageAndProgress` and `SetCompleted` to worker job repository**

File: `worker/internal/repository/job.go`. Add after the `UpdateProgress` method (after line 43):

```go
// SetStageAndProgress updates stage and progress_percent atomically.
// Used by the transfer worker to track rclone upload progress.
func (r *JobRepository) SetStageAndProgress(ctx context.Context, jobID, stage string, percent int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET stage = $2, progress_percent = $3, updated_at = NOW()
		WHERE job_id = $1`,
		jobID, stage, percent)
	if err != nil {
		return fmt.Errorf("set stage and progress %s: %w", jobID, err)
	}
	return nil
}

// SetCompleted marks a job as completed with progress_percent=100.
// Does NOT write stage — the stage column keeps its current value.
func (r *JobRepository) SetCompleted(ctx context.Context, jobID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET status = 'completed', progress_percent = 100, updated_at = NOW()
		WHERE job_id = $1`,
		jobID)
	if err != nil {
		return fmt.Errorf("set completed %s: %w", jobID, err)
	}
	return nil
}
```

- [ ] **Step 4: Build worker to verify no compile errors**

```bash
cd /Users/robospot/prj/cleaner/converter
docker build --target builder -t worker-test ./worker 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 5: Build API to verify no compile errors**

```bash
docker build --target builder -t api-test ./api 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/robospot/prj/cleaner/converter
git add api/internal/model/model.go worker/internal/model/model.go worker/internal/repository/job.go
git commit -m "feat(worker): add transfer stage constants and SetStageAndProgress/SetCompleted repo methods"
```

---

## Chunk 2: Converter — Conditional Job Completion

**Files:**
- Modify: `worker/internal/converter/converter.go` (lines 263-310)

The goal is:
1. Move subtitle fetch to *before* the job completion call (fix race with rclone)
2. When `transferEnabled`: transition job to `in_progress/stage=transfer/progress=0` instead of `completed`, then enqueue transfer
3. When `!transferEnabled`: keep existing `completed` behaviour

---

- [ ] **Step 1: Rewrite the completion + subtitle + transfer block**

File: `worker/internal/converter/converter.go`. Replace lines 263-310 (from `// Mark job as completed.` to the closing `}` of the transfer enqueue block):

```go
	// Best-effort cleanup of original downloaded torrent data on successful convert.
	downloadsDir := filepath.Join(w.mediaRoot, "downloads", msg.JobID)
	if err := os.RemoveAll(downloadsDir); err != nil {
		log.Error("cleanup downloads dir failed", "path", downloadsDir, "error", err)
	}

	log.Info("job completed", "asset_id", assetID, "master", masterPath)

	// ── Subtitle fetch (best-effort, non-fatal) ───────────────────────────────
	// Must run BEFORE transfer enqueue to avoid race: rclone move may start
	// while subtitle files are still being written to finalDir.
	if w.subtitleFetcher != nil && msg.Payload.TMDBID != "" {
		results := w.subtitleFetcher.FetchAndSave(ctx, msg.Payload.TMDBID, finalDir)
		for _, sub := range results {
			extID := &sub.ExternalID
			if sub.ExternalID == "" {
				extID = nil
			}
			if err := w.subtitleRepo.Upsert(ctx, movie.ID, sub.Language, "opensubtitles", sub.FilePath, extID); err != nil {
				log.Warn("subtitle upsert failed", "lang", sub.Language, "error", err)
			}
		}
		log.Info("subtitles fetched", "count", len(results))
	}

	// ── Mark job completed or hand off to transfer ────────────────────────────
	if w.transferEnabled {
		// Keep job in_progress; transfer worker will mark it completed.
		if err := w.jobRepo.SetStageAndProgress(ctx, msg.JobID, model.StageTransfer, 0); err != nil {
			log.Error("set transfer stage failed", "error", err)
		}

		tfMsg := model.TransferMessage{
			SchemaVersion: "1",
			JobID:         msg.JobID,
			CorrelationID: msg.CorrelationID,
			CreatedAt:     time.Now().UTC(),
			Payload: model.TransferJob{
				MovieID:    movie.ID,
				StorageKey: movie.StorageKey,
				LocalPath:  finalDir,
			},
		}
		if err := w.q.Push(ctx, queue.TransferQueue, tfMsg); err != nil {
			log.Error("enqueue transfer failed", "error", err)
			// Non-fatal: mark completed locally so the job doesn't stay stuck.
			if err2 := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err2 != nil {
				log.Error("fallback complete failed", "error", err2)
			}
		} else {
			log.Info("transfer job enqueued", "movie_id", movie.ID)
		}
	} else {
		// No transfer configured: mark job completed immediately.
		if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err != nil {
			log.Error("update status to completed", "error", err)
		}
		log.Info("job completed", "asset_id", assetID, "master", masterPath)
	}
```

- [ ] **Step 2: Build worker to verify no compile errors**

```bash
cd /Users/robospot/prj/cleaner/converter
docker build --target builder -t worker-test ./worker 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add worker/internal/converter/converter.go
git commit -m "feat(worker): transition job to transfer stage instead of completed when transfer enabled"
```

---

## Chunk 3: Transfer Worker — Progress Tracking

**Files:**
- Modify: `worker/internal/transfer/transfer.go` (full rewrite of struct + New + process)

Add `jobRepo` dependency and rewrite `process()` to parse rclone stderr for progress.

---

- [ ] **Step 1: Rewrite transfer.go**

Replace the full contents of `worker/internal/transfer/transfer.go`:

```go
package transfer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

var pctRe = regexp.MustCompile(`(\d+)%`)

type Worker struct {
	q             *queue.Client
	movieRepo     *repository.MovieRepository
	jobRepo       *repository.JobRepository
	storageLocID  int64
	rcloneRemote  string
	remotePath    string
}

func New(
	q *queue.Client,
	movieRepo *repository.MovieRepository,
	jobRepo *repository.JobRepository,
	rcloneRemote string,
	remotePath string,
	storageLocID int64,
) *Worker {
	return &Worker{
		q:            q,
		movieRepo:    movieRepo,
		jobRepo:      jobRepo,
		storageLocID: storageLocID,
		rcloneRemote: rcloneRemote,
		remotePath:   remotePath,
	}
}

func (w *Worker) Run(ctx context.Context) {
	slog.Info("transfer worker started", "remote", w.rcloneRemote)
	for {
		if ctx.Err() != nil {
			slog.Info("transfer worker stopped")
			return
		}
		raw, err := w.q.Pop(ctx, queue.TransferQueue, 5*time.Second)
		if errors.Is(err, queue.ErrEmpty) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("transfer queue pop error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		w.process(ctx, raw)
	}
}

func (w *Worker) process(ctx context.Context, raw []byte) {
	var msg model.TransferMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Error("unmarshal transfer message", "error", err)
		return
	}
	log := slog.With(
		"job_id", msg.JobID,
		"movie_id", msg.Payload.MovieID,
		"storage_key", msg.Payload.StorageKey,
	)

	localPath := msg.Payload.LocalPath
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Error("local path does not exist, skipping transfer", "path", localPath)
		if err2 := w.jobRepo.SetFailed(ctx, msg.JobID, "transfer_src_missing", "local path not found", false); err2 != nil {
			log.Error("set failed", "error", err2)
		}
		return
	}

	// Mark job as transfer stage with 0% progress before starting rclone.
	if err := w.jobRepo.SetStageAndProgress(ctx, msg.JobID, model.StageTransfer, 0); err != nil {
		log.Error("set transfer stage", "error", err)
	}

	dest := fmt.Sprintf("%s:%s/movies/%s/",
		w.rcloneRemote,
		filepath.ToSlash(w.remotePath),
		msg.Payload.StorageKey,
	)

	log.Info("starting rclone transfer", "src", localPath, "dest", dest)
	start := time.Now()

	// --stats 1s --stats-one-line: write one-line stats to stderr every second.
	// Do NOT use --progress: it writes ANSI escape codes incompatible with parsing.
	cmd := exec.CommandContext(ctx, "rclone", "move",
		localPath+"/",
		dest,
		"--stats", "1s",
		"--stats-one-line",
	)
	cmd.Stdout = os.Stdout

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error("rclone stderr pipe", "error", err)
		_ = w.jobRepo.SetFailed(ctx, msg.JobID, "transfer_pipe_error", err.Error(), false)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Error("rclone start", "error", err)
		_ = w.jobRepo.SetFailed(ctx, msg.JobID, "transfer_start_error", err.Error(), false)
		return
	}

	// Parse stderr for progress percentage and update DB every ~2 seconds.
	go func() {
		scanner := bufio.NewScanner(stderr)
		lastPct := -1
		lastUpdate := time.Now()
		for scanner.Scan() {
			line := scanner.Text()
			if m := pctRe.FindStringSubmatch(line); m != nil {
				pct, err := strconv.Atoi(m[1])
				if err != nil {
					continue
				}
				if pct != lastPct && time.Since(lastUpdate) >= 2*time.Second {
					lastPct = pct
					lastUpdate = time.Now()
					if err := w.jobRepo.SetStageAndProgress(ctx, msg.JobID, model.StageTransfer, pct); err != nil {
						log.Warn("progress update failed", "error", err)
					}
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		log.Error("rclone transfer failed", "error", err, "duration_s", time.Since(start).Seconds())
		_ = w.jobRepo.SetFailed(ctx, msg.JobID, "transfer_rclone_error", err.Error(), false)
		return
	}

	log.Info("rclone transfer complete", "duration_s", time.Since(start).Seconds())

	// Remove now-empty local directory.
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		log.Warn("could not remove local dir after transfer", "path", localPath, "error", err)
	}

	// Update movie storage location in DB.
	if err := w.movieRepo.UpdateStorageLocation(ctx, msg.Payload.MovieID, w.storageLocID); err != nil {
		log.Error("update storage_location_id failed", "error", err)
	}

	// Mark job completed. Stage stays "transfer" (SetCompleted does not write stage).
	if err := w.jobRepo.SetCompleted(ctx, msg.JobID); err != nil {
		log.Error("set completed failed", "error", err)
	}

	log.Info("transfer done", "location_id", w.storageLocID)
}
```

- [ ] **Step 2: Build worker to verify no compile errors**

```bash
cd /Users/robospot/prj/cleaner/converter
docker build --target builder -t worker-test ./worker 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add worker/internal/transfer/transfer.go
git commit -m "feat(worker): rewrite transfer worker with rclone progress tracking and job stage updates"
```

---

## Chunk 4: Wire jobRepo into Transfer Worker

**Files:**
- Modify: `worker/cmd/worker/main.go` (line 118-119)

---

- [ ] **Step 1: Pass jobRepo to transfer.New()**

File: `worker/cmd/worker/main.go`, lines 118-119. Change:

```go
		trWorker = transfer.New(redisClient, movieRepo,
			cfg.RcloneRemote, cfg.RcloneRemotePath, remoteStorageLocID)
```

To:

```go
		trWorker = transfer.New(redisClient, movieRepo, jobRepo,
			cfg.RcloneRemote, cfg.RcloneRemotePath, remoteStorageLocID)
```

- [ ] **Step 2: Build worker to verify no compile errors**

```bash
cd /Users/robospot/prj/cleaner/converter
docker build --target builder -t worker-test ./worker 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add worker/cmd/worker/main.go
git commit -m "feat(worker): inject jobRepo into transfer worker"
```

---

## Chunk 5: Frontend — Transfer Stage Label

**Files:**
- Modify: `frontend/src/types/index.ts` (line 3)
- Modify: `frontend/src/app/queue/page.tsx` (line 70)
- Modify: `frontend/src/app/jobs/[jobId]/page.tsx` (line 68)

---

- [ ] **Step 1: Add `'transfer'` to JobStage type**

File: `frontend/src/types/index.ts`, line 3. Change:

```ts
export type JobStage = 'download' | 'convert'
```

To:

```ts
export type JobStage = 'download' | 'convert' | 'transfer'
```

- [ ] **Step 2: Add `'Перенос'` label on queue page**

File: `frontend/src/app/queue/page.tsx`, line 70. Change:

```ts
	const stageName = job.stage === 'download' ? 'Скачивание' : job.stage === 'convert' ? 'Конвертация' : null
```

To:

```ts
	const stageName = job.stage === 'download' ? 'Скачивание' : job.stage === 'convert' ? 'Конвертация' : job.stage === 'transfer' ? 'Перенос' : null
```

- [ ] **Step 3: Add `'Перенос'` label on job detail page**

File: `frontend/src/app/jobs/[jobId]/page.tsx`, line 68. The current code shows `job.stage ?? '—'` raw. Change it to a translated label:

```tsx
<Row label="Stage" value={
    job.stage === 'download' ? 'Скачивание'
    : job.stage === 'convert' ? 'Конвертация'
    : job.stage === 'transfer' ? 'Перенос'
    : '—'
} />
```

- [ ] **Step 4: Build frontend to verify no TypeScript errors**

```bash
cd /Users/robospot/prj/cleaner/converter
docker build -t frontend-test ./frontend 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/app/queue/page.tsx frontend/src/app/jobs/[jobId]/page.tsx
git commit -m "feat(frontend): add transfer stage label to queue and job detail pages"
```

---

## Chunk 6: Deploy to Remote Server

- [ ] **Step 1: Push changes to remote server**

```bash
ssh -i /Users/robospot/.ssh/id_ed25519 root@178.104.53.215 "cd /opt/converter && git pull"
```

- [ ] **Step 2: Rebuild and restart all services**

```bash
ssh -i /Users/robospot/.ssh/id_ed25519 root@178.104.53.215 "cd /opt/converter && docker compose build && docker compose up -d"
```

- [ ] **Step 3: Verify worker started correctly**

```bash
ssh -i /Users/robospot/.ssh/id_ed25519 root@178.104.53.215 "cd /opt/converter && docker compose logs worker --tail=20"
```

Expected log lines:
```
transfer worker started  remote=myremote
```

- [ ] **Step 4: Update CHANGELOG.md**

Add to `CHANGELOG.md` under `## [Unreleased]`:

```markdown
### Added
- `worker/internal/model/model.go`: add `StageTransfer` constant
- `api/internal/model/model.go`: add `JobStageTransfer` constant
- `worker/internal/repository/job.go`: add `SetStageAndProgress` and `SetCompleted` methods
- `worker/internal/transfer/transfer.go`: rewrite transfer worker with rclone stderr progress parsing and job stage tracking
- `worker/internal/converter/converter.go`: transition job to `transfer` stage instead of `completed` when transfer is enabled; fix subtitle fetch ordering race

### Changed
- `frontend/src/types/index.ts`: add `'transfer'` to `JobStage` type
- `frontend/src/app/queue/page.tsx`: show "Перенос" label for transfer stage
- `frontend/src/app/jobs/[jobId]/page.tsx`: show "Перенос" label for transfer stage
```

- [ ] **Step 5: Commit CHANGELOG**

```bash
git add CHANGELOG.md
git commit -m "docs: update CHANGELOG for transfer stage feature"
git push
```
