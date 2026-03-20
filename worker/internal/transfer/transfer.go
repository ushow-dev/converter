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
	q            *queue.Client
	movieRepo    *repository.MovieRepository
	jobRepo      *repository.JobRepository
	storageLocID int64
	rcloneRemote string
	remotePath   string
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

	// Re-assert transfer stage/progress in case of reprocessing.
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

	// Remove local directory (rclone move leaves empty subdirs behind).
	if err := os.RemoveAll(localPath); err != nil && !os.IsNotExist(err) {
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
