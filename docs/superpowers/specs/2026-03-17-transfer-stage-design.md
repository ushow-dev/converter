# Transfer Stage — Design Spec

**Date:** 2026-03-17
**Status:** Approved

## Problem

After HLS conversion completes, the job immediately transitions to `completed`. The async rclone transfer to remote storage is invisible — the user has no way to know whether files are still uploading or already on the remote server.

## Goal

Show a `transfer` stage with a real-time progress bar on both the queue page and the job detail page, using the existing job status/stage/progress_percent pipeline.

## Approach

Extend the existing job pipeline: when transfer is enabled, the converter does not close the job as `completed` after HLS. Instead it transitions the job to `in_progress + stage=transfer + progress=0`. The transfer worker drives progress updates and closes the job when done.

## Changes

### 1. API model (`api/internal/model/model.go`)

Add `JobStageTransfer JobStage = "transfer"` to the existing `JobStage` constants.

### 2. Worker model (`worker/internal/model/model.go`)

Add `StageTransfer = "transfer"` alongside the existing `StageDownload` and `StageConvert` constants.

### 3. Worker job repository (`worker/internal/repository/job.go`)

Add two new methods:

- `SetStageAndProgress(ctx context.Context, jobID string, stage string, percent int) error` — updates `stage`, `progress_percent`, and `updated_at` in a single query. Used for all transfer progress ticks. The existing `UpdateProgress(ctx, jobID string, progress int)` does not write `stage` and must not be used for the transfer stage.

- `SetCompleted(ctx context.Context, jobID string) error` — updates `status='completed'`, `progress_percent=100`, `updated_at=NOW()`. Does NOT write `stage` — the stage column is left at its current value (`transfer`). Do not use the existing `UpdateStatus` for this because `UpdateStatus` always writes `stage`, and passing nil would NULL it out.

Also ensure `SetFailed(ctx, jobID string, ...)` does NOT write `stage` — the transfer worker calls `SetStageAndProgress` with `stage=transfer` before starting rclone, so `stage` is already correct on the row when `SetFailed` runs. Do not change `SetFailed` to accept a stage parameter (that would break the converter failure path).

### 4. Worker — Converter (`worker/internal/converter/converter.go`)

**Subtitle fetch ordering fix (required):** Move the subtitle fetch block to *before* the transfer enqueue. Currently subtitles are fetched after the job is marked completed and after transfer is enqueued — this creates a race where the transfer worker may start `rclone move` while subtitle files are still being written to `finalDir`.

**Job completion change:** The existing `UpdateStatus(...completed...)` call (currently unconditional) must become a conditional branch:
- If `!w.transferEnabled`: mark job `completed` (existing behaviour, unchanged)
- If `w.transferEnabled`: call `SetStageAndProgress(ctx, jobID, StageTransfer, 0)` to mark `in_progress / stage=transfer / progress=0`, then enqueue the transfer message

The transfer message already includes `job_id` — no model changes needed.

### 5. Worker — Transfer (`worker/internal/transfer/transfer.go`)

**rclone invocation:** Replace existing flags with `--stats 1s --stats-one-line`. Do NOT include `--progress` — it writes ANSI escape codes to stderr which are incompatible with line-by-line parsing.

**Progress tracking:**
- Capture rclone stderr in a goroutine
- Parse lines using the rclone one-line stats format: `<timestamp> INFO  : ... X%, ...`
- Extract the percentage field directly (e.g. regex `(\d+)%`) — do not compute from transferred/total bytes
- Call `jobRepo.SetStageAndProgress(ctx, jobID, StageTransfer, percent)` every ~2 seconds (skip if same percent as last call)

**On success:** call `jobRepo.SetCompleted(ctx, jobID)` — this sets `status=completed`, `progress_percent=100`. Stage is already `transfer` on the row.

**On error:** call `jobRepo.SetFailed(ctx, jobID, errCode, errMsg)` — stage is already `transfer` on the row (set before rclone was started).

**`transfer.Worker` struct changes:** Add `jobRepo *repository.JobRepository` field to the `Worker` struct and update the `New()` constructor to accept and store it. The transfer worker must call `jobRepo.SetStageAndProgress`, `jobRepo.SetCompleted`, and `jobRepo.SetFailed` directly. Inject the existing `JobRepository` instance from `worker/cmd/worker/main.go`.

**No distributed lock:** a Redis NX lock on the transfer job is out of scope. Duplicate transfer messages (e.g. manual re-enqueue) may cause concurrent rclone runs on the same directory. Acceptable for now.

### 6. Frontend types (`frontend/src/types/index.ts`)

Add `'transfer'` to the `JobStage` union type.

### 7. Frontend queue page (`frontend/src/app/queue/page.tsx`)

Add `'transfer'` → `'Перенос'` to the `stageName` mapping. The existing progress bar and spinner already render for any `in_progress` job — no other changes needed.

### 8. Frontend job detail page (`frontend/src/app/jobs/[jobId]/page.tsx`)

Add `'transfer'` → `'Перенос'` to the stage label mapping.

**Note on subtitle section:** `SubtitleSection` renders only when `job.status === 'completed'`. With transfer enabled, the subtitle section will appear only after the transfer completes — not immediately after conversion. This is the intended behaviour.

### 9. Database

No migration required. The `stage` column in `media_jobs` is `TEXT` with no constraint — adding a new stage value requires no schema change.

## Data Flow

```
Converter completes HLS
  → fetch subtitles (must happen before transfer enqueue)
  → if transferEnabled:
      job: in_progress / stage=transfer / progress=0
      push TransferMessage{job_id, movie_id, storage_key, local_path}
  → else:
      job: completed

Transfer worker pops message
  → jobRepo.SetStageAndProgress(jobID, "transfer", 0)
  → rclone move --stats 1s --stats-one-line
  → parse stderr for percentage every ~2s → SetStageAndProgress(jobID, "transfer", pct)
  → on done:
      movie.storage_location_id = remote
      jobRepo.SetCompleted(jobID)  [progress=100, stage stays "transfer"]
  → on error:
      jobRepo.SetFailed(jobID, ...)  [stage stays "transfer"]
```

## Error Handling

- rclone exits non-zero → `SetFailed`, stage stays `transfer`
- Progress parsing fails → silently ignored, progress stays at last known value
- Transfer worker crash mid-transfer → job stays `in_progress` indefinitely (no automatic recovery — out of scope)

## Out of Scope

- Retry logic for failed transfers
- Partial progress resume
- Transfer cancellation
- Distributed lock on transfer worker
