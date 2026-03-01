package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on duplicate key violations (idempotency).
var ErrConflict = errors.New("conflict")

// JobRepository handles persistence of media_jobs.
type JobRepository struct {
	pool *pgxpool.Pool
}

// NewJobRepository creates a JobRepository backed by pool.
func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

// Create inserts a new job. On unique-constraint violation (request_id) it
// returns the existing job + ErrConflict so the caller can implement idempotency.
func (r *JobRepository) Create(ctx context.Context, job *model.Job) (*model.Job, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO media_jobs
			(job_id, content_type, source_type, source_ref, priority, status,
			 request_id, correlation_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		job.JobID, job.ContentType, job.SourceType, job.SourceRef,
		string(job.Priority), string(job.Status),
		job.RequestID, job.CorrelationID,
		job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && job.RequestID != nil {
			// Unique violation on request_id — return existing job.
			existing, fetchErr := r.GetByRequestID(ctx, *job.RequestID)
			if fetchErr != nil {
				return nil, fmt.Errorf("fetch existing job: %w", fetchErr)
			}
			return existing, ErrConflict
		}
		return nil, fmt.Errorf("insert job: %w", err)
	}
	return job, nil
}

// GetByID fetches a job by its primary key.
func (r *JobRepository) GetByID(ctx context.Context, jobID string) (*model.Job, error) {
	return r.scanJob(ctx, `
		SELECT job_id, content_type, source_type, source_ref, priority, status,
		       stage, progress_percent, error_code, error_message, retryable,
		       request_id, correlation_id, created_at, updated_at
		FROM media_jobs WHERE job_id = $1`, jobID)
}

// GetByRequestID fetches a job by its idempotency key.
func (r *JobRepository) GetByRequestID(ctx context.Context, requestID string) (*model.Job, error) {
	return r.scanJob(ctx, `
		SELECT job_id, content_type, source_type, source_ref, priority, status,
		       stage, progress_percent, error_code, error_message, retryable,
		       request_id, correlation_id, created_at, updated_at
		FROM media_jobs WHERE request_id = $1`, requestID)
}

// List returns jobs with optional status filter and cursor-based pagination.
// cursor is the created_at of the last seen record (exclusive).
func (r *JobRepository) List(
	ctx context.Context, status string, limit int, cursor string,
) ([]*model.Job, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var rows pgx.Rows
	var err error

	if status != "" && cursor != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT job_id, content_type, source_type, source_ref, priority, status,
			       stage, progress_percent, error_code, error_message, retryable,
			       request_id, correlation_id, created_at, updated_at
			FROM media_jobs
			WHERE status = $1 AND created_at < $2::timestamptz
			ORDER BY created_at DESC LIMIT $3`,
			status, cursor, limit+1)
	} else if status != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT job_id, content_type, source_type, source_ref, priority, status,
			       stage, progress_percent, error_code, error_message, retryable,
			       request_id, correlation_id, created_at, updated_at
			FROM media_jobs
			WHERE status = $1
			ORDER BY created_at DESC LIMIT $2`,
			status, limit+1)
	} else if cursor != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT job_id, content_type, source_type, source_ref, priority, status,
			       stage, progress_percent, error_code, error_message, retryable,
			       request_id, correlation_id, created_at, updated_at
			FROM media_jobs
			WHERE created_at < $1::timestamptz
			ORDER BY created_at DESC LIMIT $2`,
			cursor, limit+1)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT job_id, content_type, source_type, source_ref, priority, status,
			       stage, progress_percent, error_code, error_message, retryable,
			       request_id, correlation_id, created_at, updated_at
			FROM media_jobs
			ORDER BY created_at DESC LIMIT $1`,
			limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs, err := scanRows(rows)
	if err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(jobs) > limit {
		jobs = jobs[:limit]
		nextCursor = jobs[limit-1].CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z")
	}
	return jobs, nextCursor, nil
}

// UpdateStatus changes the job status (and optional stage / progress).
func (r *JobRepository) UpdateStatus(
	ctx context.Context, jobID string, status model.JobStatus,
	stage *model.JobStage, progress int,
) error {
	var stageStr *string
	if stage != nil {
		s := string(*stage)
		stageStr = &s
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET status = $2, stage = $3, progress_percent = $4, updated_at = NOW()
		WHERE job_id = $1`,
		jobID, string(status), stageStr, progress)
	return err
}

// SetFailed marks a job as failed with an error code and message.
func (r *JobRepository) SetFailed(
	ctx context.Context, jobID, errorCode, errorMessage string, retryable bool,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_jobs
		SET status = 'failed', error_code = $2, error_message = $3,
		    retryable = $4, updated_at = NOW()
		WHERE job_id = $1`,
		jobID, errorCode, errorMessage, retryable)
	return err
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (r *JobRepository) scanJob(ctx context.Context, query string, arg any) (*model.Job, error) {
	rows, err := r.pool.Query(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	jobs, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, ErrNotFound
	}
	return jobs[0], nil
}

func scanRows(rows pgx.Rows) ([]*model.Job, error) {
	var jobs []*model.Job
	for rows.Next() {
		j := &model.Job{}
		var stage *string
		var priority, status string
		err := rows.Scan(
			&j.JobID, &j.ContentType, &j.SourceType, &j.SourceRef,
			&priority, &status,
			&stage, &j.ProgressPercent,
			&j.ErrorCode, &j.ErrorMessage, &j.Retryable,
			&j.RequestID, &j.CorrelationID,
			&j.CreatedAt, &j.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		j.Priority = model.JobPriority(priority)
		j.Status = model.JobStatus(status)
		if stage != nil {
			s := model.JobStage(*stage)
			j.Stage = &s
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
