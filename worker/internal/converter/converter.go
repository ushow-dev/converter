package converter

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"app/worker/internal/ffmpeg"
	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

// Worker consumes convert_queue and orchestrates HLS conversions.
type Worker struct {
	q         *queue.Client
	jobRepo   *repository.JobRepository
	assetRepo *repository.AssetRepository
	movieRepo *repository.MovieRepository
	mediaRoot string
}

// New creates a convert Worker.
func New(
	q *queue.Client,
	jobRepo *repository.JobRepository,
	assetRepo *repository.AssetRepository,
	movieRepo *repository.MovieRepository,
	mediaRoot string,
) *Worker {
	return &Worker{
		q: q, jobRepo: jobRepo, assetRepo: assetRepo, movieRepo: movieRepo, mediaRoot: mediaRoot,
	}
}

// Run starts the BLPOP consumer loop. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("convert worker started")
	for {
		if ctx.Err() != nil {
			slog.Info("convert worker stopped")
			return
		}
		raw, err := w.q.Pop(ctx, queue.ConvertQueue, 5*time.Second)
		if errors.Is(err, queue.ErrEmpty) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("convert queue pop error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		w.process(ctx, raw)
	}
}

func (w *Worker) process(ctx context.Context, raw []byte) {
	var msg model.ConvertMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Error("unmarshal convert message", "error", err)
		return
	}
	log := slog.With("job_id", msg.JobID, "correlation_id", msg.CorrelationID)

	// Guard: skip already-terminal jobs.
	if terminal, err := w.jobRepo.IsTerminal(ctx, msg.JobID); err != nil || terminal {
		log.Info("skipping terminal job")
		return
	}

	// Guard: max attempts exceeded.
	if msg.Attempt > msg.MaxAttempts {
		log.Warn("max attempts exceeded", "attempt", msg.Attempt)
		_ = w.jobRepo.SetFailed(ctx, msg.JobID, "MAX_ATTEMPTS_EXCEEDED",
			fmt.Sprintf("exceeded %d convert attempts", msg.MaxAttempts), false)
		return
	}

	// Distributed lock.
	lockKey := msg.JobID + "_convert"
	locked, err := w.q.AcquireLock(ctx, lockKey)
	if err != nil {
		log.Error("acquire lock", "error", err)
		return
	}
	if !locked {
		log.Info("convert job already locked, skipping")
		return
	}
	defer w.q.ReleaseLock(ctx, lockKey)

	stage := model.StageConvert
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusInProgress, &stage, 0); err != nil {
		log.Error("update status to in_progress", "error", err)
		return
	}

	inputPath := msg.Payload.InputPath
	outputDir := msg.Payload.OutputPath // temp HLS working directory

	log.Info("starting HLS convert", "input", inputPath, "output_dir", outputDir)

	// Clean up any leftover from a previous attempt.
	_ = os.RemoveAll(outputDir)

	// Prepare temp output dir.
	if err := os.MkdirAll(outputDir, 0o777); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create output dir: "+err.Error(), false)
		return
	}
	_ = os.Chmod(outputDir, 0o777)

	// ── HLS encode ───────────────────────────────────────────────────────────
	start := time.Now()
	result, err := ffmpeg.RunHLS(ctx, inputPath, outputDir, 4, func(pct int) {
		_ = w.jobRepo.UpdateProgress(ctx, msg.JobID, pct)
		log.Info("convert progress", "pct", pct)
	})
	if err != nil {
		w.failOrRequeue(ctx, msg, "FFMPEG_ERROR", err.Error(), false)
		return
	}
	log.Info("HLS encode done", "duration_s", time.Since(start).Seconds())

	// ── Thumbnail ─────────────────────────────────────────────────────────────
	thumbSrc := outputDir + "/thumbnail.jpg"
	if err := ffmpeg.Thumbnail(ctx, inputPath, thumbSrc, 600); err != nil {
		// Non-fatal: log and continue without thumbnail.
		log.Warn("thumbnail extraction failed", "error", err)
		thumbSrc = ""
	}

	// ── Create movie row and derive final directory ───────────────────────────
	movie, err := w.movieRepo.Upsert(ctx, msg.Payload.IMDbID, msg.Payload.TMDBID, nil)
	if err != nil {
		w.failJob(ctx, msg, "DB_ERROR", "create movie record: "+err.Error(), false)
		return
	}
	finalDir := filepath.Join(w.mediaRoot, "converted", movie.StorageKey)

	// ── Move temp → final ─────────────────────────────────────────────────────
	// Remove stale final dir from a previous attempt if present.
	_ = os.RemoveAll(finalDir)
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o777); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create parent of final dir: "+err.Error(), false)
		return
	}
	if err := os.Rename(outputDir, finalDir); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "move to final dir: "+err.Error(), false)
		return
	}
	log.Info("HLS files moved to final dir", "path", finalDir)

	masterPath := filepath.Join(finalDir, "master.m3u8")
	var thumbFinalPath *string
	if thumbSrc != "" {
		// Thumbnail was written inside outputDir which was renamed to finalDir.
		p := filepath.Join(finalDir, "thumbnail.jpg")
		thumbFinalPath = &p
	}

	// ── Probe accurate duration from master.m3u8's first variant ─────────────
	durationSec := result.DurationSec
	if probed := ffmpeg.ProbeInfo(ctx, filepath.Join(finalDir, "720", "index.m3u8")); probed > 0 {
		durationSec = probed
	}

	// ── Create asset record ───────────────────────────────────────────────────
	now := time.Now().UTC()
	assetID := generateAssetID()
	videoCodec := "h264"
	audioCodec := "aac"

	asset := &model.Asset{
		AssetID:       assetID,
		JobID:         msg.JobID,
		MovieID:       &movie.ID,
		StoragePath:   masterPath,
		ThumbnailPath: thumbFinalPath,
		DurationSec:   &durationSec,
		VideoCodec:    &videoCodec,
		AudioCodec:    &audioCodec,
		IsReady:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := w.assetRepo.Create(ctx, asset); err != nil {
		log.Error("create asset record", "error", err)
		// Non-fatal.
	}

	// Mark job as completed.
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err != nil {
		log.Error("update status to completed", "error", err)
	}

	// Best-effort cleanup of original downloaded torrent data on successful convert.
	downloadsDir := filepath.Join(w.mediaRoot, "downloads", msg.JobID)
	if err := os.RemoveAll(downloadsDir); err != nil {
		log.Warn("cleanup downloads dir failed", "path", downloadsDir, "error", err)
	}

	log.Info("job completed", "asset_id", assetID, "master", masterPath)
}

// failJob marks the job as permanently failed.
func (w *Worker) failJob(ctx context.Context, msg model.ConvertMessage, code, message string, retryable bool) {
	slog.Error("convert failed", "job_id", msg.JobID, "code", code, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, retryable)
}

// failOrRequeue marks failed or re-enqueues if attempts remain.
func (w *Worker) failOrRequeue(ctx context.Context, msg model.ConvertMessage, code, message string, retryable bool) {
	if !retryable || msg.Attempt >= msg.MaxAttempts {
		w.failJob(ctx, msg, code, message, false)
		return
	}
	slog.Warn("convert error, will retry",
		"job_id", msg.JobID, "attempt", msg.Attempt, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, true)

	msg.Attempt++
	delay := backoffDelay(msg.Attempt)
	time.Sleep(delay)
	if err := w.q.Push(ctx, queue.ConvertQueue, msg); err != nil {
		slog.Error("requeue convert failed", "job_id", msg.JobID, "error", err)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func backoffDelay(attempt int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > 5*time.Minute {
			return 5 * time.Minute
		}
	}
	return d
}

func generateAssetID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("asset_%x", b)
}
