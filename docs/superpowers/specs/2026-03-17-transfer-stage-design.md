# Transfer Stage — Design Spec

**Date:** 2026-03-17
**Status:** Approved

## Problem

After HLS conversion completes, the job immediately transitions to `completed`. The async rclone transfer to remote storage is invisible — the user has no way to know whether files are still uploading or already on the remote server.

## Goal

Show a `transfer` stage with a real-time progress bar on both the queue page and the job detail page, using the existing job status/stage/progress_percent pipeline.

## Approach

Extend the existing job pipeline: converter does not close the job as `completed` when transfer is enabled. Instead it transitions the job to `in_progress + stage=transfer`. The transfer worker drives progress updates and closes the job when done.

## Changes

### 1. API model (`api/internal/model/model.go`)

Add `JobStageTransfer JobStage = "transfer"` to the existing `JobStage` constants.

### 2. API repository (`api/internal/repository/job.go`)

Add method `UpdateProgress(ctx, jobID string, stage JobStage, percent int)` that updates `stage`, `progress_percent`, and `updated_at` on the jobs table.

### 3. Worker — Converter (`worker/internal/converter/converter.go`)

When `w.transferEnabled`:
- Instead of marking job `completed` after HLS, mark it `in_progress` with `stage=transfer`, `progress_percent=0`
- Then enqueue transfer message as before

When transfer is disabled, behaviour is unchanged (job marked `completed` after HLS).

### 4. Worker — Transfer (`worker/internal/transfer/transfer.go`)

- Run rclone with `--stats 1s --stats-one-line-type bits` flags
- Read stderr in a goroutine, parse lines matching `Transferred: X / Y` to compute percent
- Every ~2 seconds call `jobRepo.UpdateProgress(ctx, jobID, StageTransfer, percent)`
- On success: call `jobRepo.Complete(ctx, jobID)` with `stage=transfer, progress=100`
- On failure: call `jobRepo.Fail(ctx, jobID, errCode, errMsg)`
- `TransferMessage` must include `job_id` (already present in model)

### 5. Frontend types (`frontend/src/types/index.ts`)

Add `'transfer'` to `JobStage` union type.

### 6. Frontend queue page (`frontend/src/app/queue/page.tsx`)

Add `'transfer'` → `'Перенос'` to `stageName` mapping.

### 7. Frontend job detail page (`frontend/src/app/jobs/[jobId]/page.tsx`)

Add `'transfer'` → `'Перенос'` to stage label mapping.

## Data Flow

```
Converter completes HLS
  → job: in_progress / stage=transfer / progress=0
  → push TransferMessage{job_id, movie_id, storage_key, local_path}

Transfer worker pops message
  → rclone move --stats 1s --stats-one-line-type bits
  → parse stderr → update job progress every ~2s
  → on done: job completed / stage=transfer / progress=100
             movie.storage_location_id = remote
  → on error: job failed / stage=transfer
```

## Error Handling

- rclone exits non-zero → job marked `failed` with `stage=transfer`
- Progress parsing fails → silently ignored, progress stays at last known value
- Transfer worker crash mid-transfer → job stays `in_progress` indefinitely (existing retry/recovery behaviour unchanged)

## Out of Scope

- Retry logic for failed transfers
- Partial progress resume
- Transfer cancellation
