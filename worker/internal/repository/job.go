package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobRepository provides write-only access to job state used by the worker.
type JobRepository struct {
	pool *pgxpool.Pool
}

// NewJobRepository creates a JobRepository.
func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

// UpdateStatus sets status, stage, and progress_percent for a job.
func (r *JobRepository) UpdateStatus(
	ctx context.Context, jobID, status string, stage *string, progress int,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET status = $2, stage = $3, progress_percent = $4, updated_at = NOW()
		WHERE job_id = $1`,
		jobID, status, stage, progress)
	if err != nil {
		return fmt.Errorf("update status %s: %w", jobID, err)
	}
	return nil
}

// UpdateProgress updates only progress_percent without changing status/stage.
func (r *JobRepository) UpdateProgress(ctx context.Context, jobID string, progress int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET progress_percent = $2, updated_at = NOW()
		WHERE job_id = $1`,
		jobID, progress)
	return err
}

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

// SetFailed marks a job as failed with error details.
func (r *JobRepository) SetFailed(
	ctx context.Context, jobID, errorCode, errorMessage string, retryable bool,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET status = 'failed', error_code = $2, error_message = $3,
		    retryable = $4, updated_at = NOW()
		WHERE job_id = $1`,
		jobID, errorCode, errorMessage, retryable)
	if err != nil {
		return fmt.Errorf("set failed %s: %w", jobID, err)
	}
	return nil
}

// IsTerminal returns true if the job is already in a terminal state
// (completed or failed). Used to guard against duplicate processing.
func (r *JobRepository) IsTerminal(ctx context.Context, jobID string) (bool, error) {
	var status string
	err := r.pool.QueryRow(ctx,
		"SELECT status FROM media_jobs WHERE job_id = $1", jobID).
		Scan(&status)
	if err != nil {
		return false, fmt.Errorf("get status %s: %w", jobID, err)
	}
	return status == "completed" || status == "failed", nil
}

// Exists reports whether media_jobs contains the given job_id.
func (r *JobRepository) Exists(ctx context.Context, jobID string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM media_jobs WHERE job_id = $1)", jobID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check exists %s: %w", jobID, err)
	}
	return exists, nil
}
