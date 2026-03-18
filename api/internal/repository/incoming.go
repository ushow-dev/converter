package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// IncomingRepository handles persistence of incoming_media_items.
type IncomingRepository struct {
	pool *pgxpool.Pool
}

// NewIncomingRepository creates an IncomingRepository backed by pool.
func NewIncomingRepository(pool *pgxpool.Pool) *IncomingRepository {
	return &IncomingRepository{pool: pool}
}

const incomingReturning = `
	id, source_path, source_filename, normalized_name, tmdb_id,
	content_kind, file_size_bytes, stable_since, status, attempts,
	claimed_at, claim_expires_at, quality_score, is_upgrade_candidate,
	duplicate_of_movie_id, review_reason, api_job_id, error_message,
	local_path, created_at, updated_at`

// Register upserts an incoming media item by source_path (idempotent).
// If req.Status is non-nil it is used for the insert; otherwise defaults to 'new'.
func (r *IncomingRepository) Register(ctx context.Context, req *model.RegisterIncomingRequest) (*model.IncomingItem, error) {
	status := model.IncomingStatusNew
	if req.Status != nil {
		status = model.IncomingStatus(*req.Status)
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO incoming_media_items
		    (source_path, source_filename, normalized_name, tmdb_id, content_kind,
		     file_size_bytes, stable_since, quality_score, is_upgrade_candidate,
		     duplicate_of_movie_id, review_reason, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (source_path) DO UPDATE SET
		    source_filename       = EXCLUDED.source_filename,
		    normalized_name       = COALESCE(EXCLUDED.normalized_name, incoming_media_items.normalized_name),
		    tmdb_id               = COALESCE(EXCLUDED.tmdb_id, incoming_media_items.tmdb_id),
		    file_size_bytes       = COALESCE(EXCLUDED.file_size_bytes, incoming_media_items.file_size_bytes),
		    stable_since          = COALESCE(EXCLUDED.stable_since, incoming_media_items.stable_since),
		    quality_score         = COALESCE(EXCLUDED.quality_score, incoming_media_items.quality_score),
		    is_upgrade_candidate  = EXCLUDED.is_upgrade_candidate,
		    duplicate_of_movie_id = COALESCE(EXCLUDED.duplicate_of_movie_id, incoming_media_items.duplicate_of_movie_id),
		    review_reason         = COALESCE(EXCLUDED.review_reason, incoming_media_items.review_reason),
		    updated_at            = NOW()
		RETURNING `+incomingReturning,
		req.SourcePath, req.SourceFilename, req.NormalizedName, req.TMDBID, req.ContentKind,
		req.FileSizeBytes, req.StableSince, req.QualityScore, req.IsUpgradeCandidate,
		req.DuplicateOfMovieID, req.ReviewReason, string(status),
	)

	item, err := scanIncomingItem(row)
	if err != nil {
		return nil, fmt.Errorf("register incoming item: %w", err)
	}
	return item, nil
}

// ClaimBatch atomically claims up to limit 'new' items, resetting any
// expired claims first. Returns the claimed items.
func (r *IncomingRepository) ClaimBatch(ctx context.Context, limit int, expiresAt time.Time) ([]model.IncomingItem, error) {
	rows, err := r.pool.Query(ctx, `
		WITH reset_expired AS (
		    UPDATE incoming_media_items
		    SET status = 'new',
		        claim_expires_at = NULL,
		        updated_at = NOW()
		    WHERE status = 'claimed' AND claim_expires_at < NOW()
		)
		UPDATE incoming_media_items
		SET status           = 'claimed',
		    claimed_at       = NOW(),
		    claim_expires_at = $2,
		    attempts         = attempts + 1,
		    updated_at       = NOW()
		WHERE id IN (
		    SELECT id FROM incoming_media_items
		    WHERE status = 'new'
		    ORDER BY stable_since ASC NULLS LAST, id ASC
		    LIMIT $1
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING `+incomingReturning,
		limit, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("claim batch: %w", err)
	}
	defer rows.Close()

	var items []model.IncomingItem
	for rows.Next() {
		item, err := scanIncomingRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed item: %w", err)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed rows: %w", err)
	}
	return items, nil
}

// GetByID fetches a single incoming item by its primary key.
// Returns ErrNotFound if no row exists.
func (r *IncomingRepository) GetByID(ctx context.Context, id int64) (*model.IncomingItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+incomingReturning+`
		FROM incoming_media_items WHERE id = $1`,
		id,
	)
	item, err := scanIncomingItem(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get incoming item by id: %w", err)
	}
	return item, nil
}

// Progress updates the status of a claimed/copying item.
func (r *IncomingRepository) Progress(ctx context.Context, req *model.ProgressIncomingRequest) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE incoming_media_items
		SET status = $2, updated_at = NOW()
		WHERE id = $1 AND status IN ('claimed', 'copying')`,
		req.ID, req.Status,
	)
	if err != nil {
		return fmt.Errorf("progress incoming item: %w", err)
	}
	return nil
}

// Fail marks a claimed item as failed or resets it to 'new' for retry.
// If attempts < maxAttempts the status is reset to 'new'; otherwise 'failed'.
func (r *IncomingRepository) Fail(ctx context.Context, id int64, errorMsg string, maxAttempts int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE incoming_media_items
		SET status           = CASE WHEN attempts < $3 THEN 'new' ELSE 'failed' END,
		    error_message    = $2,
		    claim_expires_at = NULL,
		    updated_at       = NOW()
		WHERE id = $1`,
		id, errorMsg, maxAttempts,
	)
	if err != nil {
		return fmt.Errorf("fail incoming item: %w", err)
	}
	return nil
}

// Complete marks an item as completed and records the resulting job ID and local path.
func (r *IncomingRepository) Complete(ctx context.Context, id int64, jobID string, localPath string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE incoming_media_items
		SET status     = 'completed',
		    api_job_id = $2,
		    local_path = $3,
		    updated_at = NOW()
		WHERE id = $1`,
		id, jobID, localPath,
	)
	if err != nil {
		return fmt.Errorf("complete incoming item: %w", err)
	}
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// scanIncomingItem scans a single-row result (pgx.Row) into an IncomingItem.
func scanIncomingItem(row pgx.Row) (*model.IncomingItem, error) {
	item := &model.IncomingItem{}
	var status string
	err := row.Scan(
		&item.ID,
		&item.SourcePath,
		&item.SourceFilename,
		&item.NormalizedName,
		&item.TMDBID,
		&item.ContentKind,
		&item.FileSizeBytes,
		&item.StableSince,
		&status,
		&item.Attempts,
		&item.ClaimedAt,
		&item.ClaimExpiresAt,
		&item.QualityScore,
		&item.IsUpgradeCandidate,
		&item.DuplicateOfMovieID,
		&item.ReviewReason,
		&item.APIJobID,
		&item.ErrorMessage,
		&item.LocalPath,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.Status = model.IncomingStatus(status)
	return item, nil
}

// scanIncomingRow scans one row from a pgx.Rows cursor into an IncomingItem.
// pgx.Rows.Scan has the same signature as pgx.Row.Scan so we can reuse
// the same field list; we just wrap rows to satisfy the pgx.Row interface.
func scanIncomingRow(rows pgx.Rows) (*model.IncomingItem, error) {
	return scanIncomingItem(rows)
}
