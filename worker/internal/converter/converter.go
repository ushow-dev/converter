package converter

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"app/worker/internal/ffmpeg"
	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

// Worker consumes convert_queue and orchestrates ffmpeg conversions.
type Worker struct {
	q         *queue.Client
	jobRepo   *repository.JobRepository
	assetRepo *repository.AssetRepository
}

// New creates a convert Worker.
func New(
	q *queue.Client,
	jobRepo *repository.JobRepository,
	assetRepo *repository.AssetRepository,
) *Worker {
	return &Worker{q: q, jobRepo: jobRepo, assetRepo: assetRepo}
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

	log.Info("starting convert",
		"input", msg.Payload.InputPath, "profile", msg.Payload.OutputProfile)

	// Prepare output directory.
	outputDir := filepath.Dir(msg.Payload.OutputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create output dir: "+err.Error(), false)
		return
	}

	// Run ffmpeg.
	start := time.Now()
	result, err := ffmpeg.Run(ctx, msg.Payload.InputPath, msg.Payload.OutputPath,
		msg.Payload.OutputProfile, func(pct int) {
			_ = w.jobRepo.UpdateProgress(ctx, msg.JobID, pct)
			log.Info("convert progress", "pct", pct)
		})
	if err != nil {
		w.failOrRequeue(ctx, msg, "FFMPEG_ERROR", err.Error(), false)
		return
	}
	log.Info("ffmpeg finished", "duration_s", time.Since(start).Seconds())

	// Move output → final_dir.
	if err := os.MkdirAll(msg.Payload.FinalDir, 0o755); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create final dir: "+err.Error(), false)
		return
	}
	finalPath := filepath.Join(msg.Payload.FinalDir, "output.mp4")
	if err := moveFile(msg.Payload.OutputPath, finalPath); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "move to final dir: "+err.Error(), false)
		return
	}
	log.Info("converted file moved to final dir", "path", finalPath)

	// Create asset record.
	now := time.Now().UTC()
	assetID := generateAssetID()
	videoCodec := result.VideoCodec
	audioCodec := result.AudioCodec
	durationSec := result.DurationSec

	// Prefer ffprobe for accurate duration.
	if probed := ffmpeg.ProbeInfo(ctx, finalPath); probed > 0 {
		durationSec = probed
	}

	asset := &model.Asset{
		AssetID:     assetID,
		JobID:       msg.JobID,
		StoragePath: finalPath,
		DurationSec: &durationSec,
		VideoCodec:  &videoCodec,
		AudioCodec:  &audioCodec,
		IsReady:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := w.assetRepo.Create(ctx, asset); err != nil {
		log.Error("create asset record", "error", err)
		// Non-fatal: job can still be marked completed; asset can be repaired.
	}

	// Mark job as completed.
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err != nil {
		log.Error("update status to completed", "error", err)
	}
	log.Info("job completed", "asset_id", assetID, "path", finalPath)
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

// moveFile moves src to dst, falling back to copy+delete for cross-device moves.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device move: copy then delete source.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Remove(src)
}

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
