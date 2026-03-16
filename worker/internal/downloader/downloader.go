package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/qbittorrent"
	"app/worker/internal/repository"
)

var errJobCanceled = errors.New("job canceled")

// Worker consumes download_queue and orchestrates torrent downloads.
type Worker struct {
	q         *queue.Client
	jobRepo   *repository.JobRepository
	qbt       *qbittorrent.Client
	mediaRoot string
}

// New creates a download Worker.
func New(
	q *queue.Client,
	jobRepo *repository.JobRepository,
	qbt *qbittorrent.Client,
	mediaRoot string,
) *Worker {
	return &Worker{q: q, jobRepo: jobRepo, qbt: qbt, mediaRoot: mediaRoot}
}

// Run starts the BLPOP consumer loop. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("download worker started")
	for {
		if ctx.Err() != nil {
			slog.Info("download worker stopped")
			return
		}
		raw, err := w.q.Pop(ctx, queue.DownloadQueue, 5*time.Second)
		if errors.Is(err, queue.ErrEmpty) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("download queue pop error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		w.process(ctx, raw)
	}
}

func (w *Worker) process(ctx context.Context, raw []byte) {
	var msg model.DownloadMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Error("unmarshal download message", "error", err)
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
			fmt.Sprintf("exceeded %d attempts", msg.MaxAttempts), false)
		return
	}

	// Distributed lock: prevent parallel processing of the same job.
	locked, err := w.q.AcquireLock(ctx, msg.JobID)
	if err != nil {
		log.Error("acquire lock", "error", err)
		return
	}
	if !locked {
		log.Info("job already locked, skipping")
		return
	}
	defer w.q.ReleaseLock(ctx, msg.JobID)

	stage := model.StageDownload
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusInProgress, &stage, 0); err != nil {
		log.Error("update status to in_progress", "error", err)
		return
	}

	log.Info("starting download", "source_ref", msg.Payload.SourceRef)

	// Add torrent to qBittorrent.
	targetDir := msg.Payload.TargetDir
	if targetDir == "" {
		targetDir = filepath.Join(w.mediaRoot, "downloads", msg.JobID)
	}
	if err := os.MkdirAll(targetDir, 0o777); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create target dir: "+err.Error(), true)
		return
	}
	_ = os.Chmod(targetDir, 0o777) // bypass umask so qBittorrent (uid=1000) can write

	hash, err := w.qbt.AddTorrent(ctx, msg.Payload.SourceRef, targetDir)
	if err != nil {
		w.failOrRequeue(ctx, msg, "QBITTORRENT_ERROR", err.Error(), true)
		return
	}
	log.Info("torrent added to qbittorrent", "hash", hash)

	// Poll until download completes or the job is deleted from DB.
	info, err := w.waitForDownloadOrCancel(ctx, msg.JobID, hash, func(pct int) {
		_ = w.jobRepo.UpdateProgress(ctx, msg.JobID, pct)
		log.Info("download progress", "pct", pct)
	})
	if err != nil {
		if errors.Is(err, errJobCanceled) {
			log.Info("job was deleted during download; canceled torrent")
			return
		}
		w.failOrRequeue(ctx, msg, "DOWNLOAD_ERROR", err.Error(), true)
		return
	}

	log.Info("download complete", "content_path", info.ContentPath)

	// Find the primary video file to convert.
	inputPath, err := findPrimaryVideoFile(info.ContentPath, targetDir)
	if err != nil {
		w.failJob(ctx, msg, "NO_VIDEO_FILE", err.Error(), false)
		return
	}
	log.Info("found video file for conversion", "input", inputPath)

	// Transition to convert stage.
	stageConvert := model.StageConvert
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusInProgress, &stageConvert, 0); err != nil {
		log.Error("update status to convert stage", "error", err)
	}

	// Enqueue convert job.
	// OutputPath is the temp dir where ffmpeg writes HLS files.
	// FinalDir is where the completed HLS tree is moved after conversion.
	outputPath := filepath.Join(w.mediaRoot, "temp", msg.JobID)
	finalDir := filepath.Join(w.mediaRoot, "converted", "movies", msg.JobID)
	convertMsg := model.ConvertMessage{
		SchemaVersion: "v1",
		JobID:         msg.JobID,
		JobType:       "convert",
		ContentType:   msg.ContentType,
		CorrelationID: msg.CorrelationID,
		Attempt:       1,
		MaxAttempts:   msg.MaxAttempts,
		CreatedAt:     time.Now().UTC(),
		Payload: model.ConvertJob{
			InputPath:     inputPath,
			OutputPath:    outputPath,
			OutputProfile: "hls_720_480_360",
			FinalDir:      finalDir,
			IMDbID:        msg.Payload.IMDbID,
			TMDBID:        msg.Payload.TMDBID,
			Title:         msg.Payload.Title,
		},
	}
	if err := w.q.Push(ctx, queue.ConvertQueue, convertMsg); err != nil {
		log.Error("enqueue convert job", "error", err)
		// Non-fatal: the convert worker can be manually re-triggered.
	}
	log.Info("convert job enqueued")
}

// failJob marks the job as permanently failed (no requeue).
func (w *Worker) failJob(ctx context.Context, msg model.DownloadMessage, code, message string, retryable bool) {
	slog.Error("download failed", "job_id", msg.JobID, "code", code, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, retryable)
}

// failOrRequeue marks the job as failed or re-enqueues it if attempts remain.
func (w *Worker) failOrRequeue(ctx context.Context, msg model.DownloadMessage, code, message string, retryable bool) {
	if !retryable || msg.Attempt >= msg.MaxAttempts {
		w.failJob(ctx, msg, code, message, false)
		return
	}
	slog.Warn("download error, will retry",
		"job_id", msg.JobID, "attempt", msg.Attempt, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, true)

	msg.Attempt++
	delay := backoffDelay(msg.Attempt)
	time.Sleep(delay)
	if err := w.q.Push(ctx, queue.DownloadQueue, msg); err != nil {
		slog.Error("requeue failed", "job_id", msg.JobID, "error", err)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// findPrimaryVideoFile returns the path to the largest video file in or at contentPath.
func findPrimaryVideoFile(contentPath, fallbackDir string) (string, error) {
	search := contentPath
	if search == "" {
		search = fallbackDir
	}

	info, err := os.Stat(search)
	if err != nil {
		// content_path might not yet be flushed; search target dir instead.
		search = fallbackDir
	} else if !info.IsDir() {
		// Single-file torrent: content_path is the file itself.
		if isVideoFile(search) {
			return search, nil
		}
	}

	// Directory: find the largest video file.
	var best string
	var bestSize int64
	_ = filepath.Walk(search, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if isVideoFile(path) && fi.Size() > bestSize {
			best = path
			bestSize = fi.Size()
		}
		return nil
	})
	if best == "" {
		return "", fmt.Errorf("no video file found in %q", search)
	}
	return best, nil
}

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".wmv": true, ".m4v": true, ".mpg": true, ".mpeg": true,
	".ts": true, ".m2ts": true, ".flv": true,
}

func isVideoFile(path string) bool {
	return videoExts[strings.ToLower(filepath.Ext(path))]
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

func (w *Worker) waitForDownloadOrCancel(
	ctx context.Context,
	jobID string,
	hash string,
	progressFn func(int),
) (*qbittorrent.TorrentInfo, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastProgress := -1
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			exists, err := w.jobRepo.Exists(ctx, jobID)
			if err != nil {
				slog.Warn("check job existence failed", "job_id", jobID, "error", err)
			} else if !exists {
				_ = w.qbt.DeleteTorrent(ctx, hash)
				return nil, errJobCanceled
			}

			info, err := w.qbt.GetTorrentInfo(ctx, hash)
			if err != nil {
				slog.Warn("qbittorrent poll error", "hash", hash, "error", err)
				continue
			}
			if info == nil {
				slog.Warn("torrent not yet visible in qbittorrent", "hash", hash)
				continue
			}
			if info.IsError() {
				return nil, fmt.Errorf("torrent error state: %s", info.State)
			}
			pct := int(info.Progress * 100)
			if pct != lastProgress {
				lastProgress = pct
				progressFn(pct)
			}
			if info.IsComplete() {
				return info, nil
			}
		}
	}
}
