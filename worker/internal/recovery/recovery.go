// Package recovery handles cleanup of stale jobs after worker restart.
// It finds in_progress jobs whose locks expired (worker crashed) and
// re-queues them for processing.
package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
	"app/worker/internal/queue"
)

// staleJob represents an in_progress job found during startup recovery.
type staleJob struct {
	JobID       string  `json:"job_id"`
	ContentType string  `json:"content_type"`
	Stage       *string `json:"stage"`
	SourceRef   string  `json:"source_ref"`
	Title       string  `json:"title"`
}

// Run finds all in_progress jobs and re-queues them.
// Should be called once at worker startup, before consumer loops begin.
func Run(ctx context.Context, pool *pgxpool.Pool, q *queue.Client) {
	slog.Info("recovery: scanning for stale in_progress jobs")

	rows, err := pool.Query(ctx, `
		SELECT job_id, content_type, stage, source_ref, COALESCE(title, '')
		FROM media_jobs
		WHERE status = 'in_progress'
	`)
	if err != nil {
		slog.Error("recovery: query stale jobs", "error", err)
		return
	}
	defer rows.Close()

	var stale []staleJob
	for rows.Next() {
		var j staleJob
		if err := rows.Scan(&j.JobID, &j.ContentType, &j.Stage, &j.SourceRef, &j.Title); err != nil {
			slog.Error("recovery: scan row", "error", err)
			continue
		}
		stale = append(stale, j)
	}
	if err := rows.Err(); err != nil {
		slog.Error("recovery: rows error", "error", err)
	}

	if len(stale) == 0 {
		slog.Info("recovery: no stale jobs found")
		return
	}

	slog.Info("recovery: found stale jobs", "count", len(stale))

	for _, j := range stale {
		// Release any stale Redis locks.
		stage := "convert"
		if j.Stage != nil {
			stage = *j.Stage
		}
		lockKey := j.JobID + "_" + stage
		q.ReleaseLock(ctx, lockKey)

		// Reset job to queued.
		_, err := pool.Exec(ctx, `
			UPDATE media_jobs
			SET status = 'queued', stage = NULL, progress_percent = 0, updated_at = NOW()
			WHERE job_id = $1 AND status = 'in_progress'`,
			j.JobID)
		if err != nil {
			slog.Error("recovery: reset job", "job_id", j.JobID, "error", err)
			continue
		}

		// Re-push to the appropriate queue.
		var pushErr error
		switch stage {
		case "convert":
			msg := model.ConvertMessage{
				SchemaVersion: "1",
				JobID:         j.JobID,
				JobType:       "convert",
				ContentType:   j.ContentType,
				CorrelationID: j.JobID,
				Attempt:       1,
				MaxAttempts:   5,
				CreatedAt:     time.Now(),
				Payload: model.ConvertJob{
					InputPath: findInputPath(ctx, pool, j.JobID),
				},
			}
			pushErr = q.Push(ctx, queue.ConvertQueue, msg)

		case "transfer":
			msg := model.TransferMessage{
				SchemaVersion: "1",
				JobID:         j.JobID,
				CorrelationID: j.JobID,
				CreatedAt:     time.Now(),
				Payload:       rebuildTransferPayload(ctx, pool, j.JobID),
			}
			pushErr = q.Push(ctx, queue.TransferQueue, msg)

		case "download":
			// Download jobs are handled by qBittorrent state — just reset to queued.
			// The download worker will re-check torrent status on next poll.
			slog.Info("recovery: download job reset to queued (no re-push needed)", "job_id", j.JobID)
			continue

		default:
			slog.Warn("recovery: unknown stage, resetting to queued only", "job_id", j.JobID, "stage", stage)
			continue
		}

		if pushErr != nil {
			slog.Error("recovery: re-push failed", "job_id", j.JobID, "queue", stage, "error", pushErr)
		} else {
			slog.Info("recovery: job re-queued", "job_id", j.JobID, "stage", stage)
		}
	}

	slog.Info("recovery: done", "recovered", len(stale))
}

// findInputPath looks up the convert message input path from the job's download dir or temp dir.
// Falls back to empty string if not determinable — converter will fail gracefully.
func findInputPath(ctx context.Context, pool *pgxpool.Pool, jobID string) string {
	var sourceRef string
	_ = pool.QueryRow(ctx, "SELECT source_ref FROM media_jobs WHERE job_id = $1", jobID).Scan(&sourceRef)
	return sourceRef
}

// rebuildTransferPayload reconstructs a TransferJob from DB state.
func rebuildTransferPayload(ctx context.Context, pool *pgxpool.Pool, jobID string) model.TransferJob {
	var tj model.TransferJob

	// Try movie asset first.
	var movieID int64
	var storagePath string
	err := pool.QueryRow(ctx,
		"SELECT a.movie_id, a.storage_path FROM media_assets a WHERE a.job_id = $1 AND a.is_ready = true LIMIT 1",
		jobID).Scan(&movieID, &storagePath)
	if err == nil {
		var storageKey string
		_ = pool.QueryRow(ctx, "SELECT storage_key FROM movies WHERE id = $1", movieID).Scan(&storageKey)
		tj.MovieID = movieID
		tj.StorageKey = storageKey
		tj.LocalPath = storagePath[:len(storagePath)-len("/master.m3u8")] // strip filename
		tj.ContentType = "movie"
		return tj
	}

	// Try episode asset.
	var episodeID int64
	err = pool.QueryRow(ctx,
		"SELECT ea.episode_id, ea.storage_path FROM episode_assets ea WHERE ea.job_id = $1 AND ea.is_ready = true LIMIT 1",
		jobID).Scan(&episodeID, &storagePath)
	if err == nil {
		tj.MovieID = episodeID
		tj.LocalPath = storagePath[:len(storagePath)-len("/master.m3u8")]
		tj.ContentType = "episode"
		// Derive storage key from local path.
		tj.StorageKey = fmt.Sprintf("%d", episodeID)
		return tj
	}

	slog.Warn("recovery: could not rebuild transfer payload", "job_id", jobID)
	return tj
}

// RecoverStaleLocks cleans up any orphaned job locks that don't correspond
// to an in_progress job (e.g. from a previous crash where the DB was reset
// but Redis wasn't).
func RecoverStaleLocks(ctx context.Context, pool *pgxpool.Pool, q *queue.Client) {
	keys, err := q.ScanLocks(ctx, "job_lock:*")
	if err != nil {
		slog.Warn("recovery: could not scan locks", "error", err)
		return
	}
	for _, key := range keys {
		// Extract job_id from "job_lock:{jobID}_{stage}"
		raw := key[len("job_lock:"):]
		jobID := raw
		for i := len(raw) - 1; i >= 0; i-- {
			if raw[i] == '_' {
				jobID = raw[:i]
				break
			}
		}
		var status string
		err := pool.QueryRow(ctx, "SELECT status FROM media_jobs WHERE job_id = $1", jobID).Scan(&status)
		if err != nil || status != "in_progress" {
			q.ReleaseLock(ctx, raw)
			slog.Info("recovery: released orphan lock", "key", key, "job_status", status)
		}
	}
}
