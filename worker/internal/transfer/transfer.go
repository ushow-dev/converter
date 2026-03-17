package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

type Worker struct {
	q             *queue.Client
	movieRepo     *repository.MovieRepository
	storageLocID  int64
	rcloneRemote  string
	remotePath    string
}

func New(
	q *queue.Client,
	movieRepo *repository.MovieRepository,
	rcloneRemote string,
	remotePath string,
	storageLocID int64,
) *Worker {
	return &Worker{
		q:            q,
		movieRepo:    movieRepo,
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
	log := slog.With("movie_id", msg.Payload.MovieID, "storage_key", msg.Payload.StorageKey)

	localPath := msg.Payload.LocalPath
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Error("local path does not exist, skipping transfer", "path", localPath)
		return
	}

	// Destination: <remote>:<remotePath>/movies/<storageKey>/
	dest := fmt.Sprintf("%s:%s/movies/%s/",
		w.rcloneRemote,
		filepath.ToSlash(w.remotePath),
		msg.Payload.StorageKey,
	)

	log.Info("starting rclone transfer", "src", localPath, "dest", dest)
	start := time.Now()

	// rclone move: copies all files then deletes source files on success.
	cmd := exec.CommandContext(ctx, "rclone", "move",
		localPath+"/", // trailing slash: move contents, not folder itself
		dest,
		"--progress",
		"--stats-one-line",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Error("rclone transfer failed", "error", err, "duration_s", time.Since(start).Seconds())
		return
	}

	log.Info("rclone transfer complete", "duration_s", time.Since(start).Seconds())

	// Remove now-empty local directory.
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		log.Warn("could not remove local dir after transfer", "path", localPath, "error", err)
	}

	// Update DB: mark movie as residing on remote storage.
	if err := w.movieRepo.UpdateStorageLocation(ctx, msg.Payload.MovieID, w.storageLocID); err != nil {
		log.Error("update storage_location_id failed", "error", err)
		return
	}

	log.Info("transfer done, storage_location_id updated", "location_id", w.storageLocID)
}
