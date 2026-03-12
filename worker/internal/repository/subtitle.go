package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SubtitleRepository persists subtitle track records.
type SubtitleRepository struct {
	pool *pgxpool.Pool
}

// NewSubtitleRepository creates a SubtitleRepository.
func NewSubtitleRepository(pool *pgxpool.Pool) *SubtitleRepository {
	return &SubtitleRepository{pool: pool}
}

// Upsert inserts or replaces the subtitle row for (movie_id, language).
func (r *SubtitleRepository) Upsert(
	ctx context.Context,
	movieID int64,
	language, source, storagePath string,
	externalID *string,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO movie_subtitles (movie_id, language, source, storage_path, external_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (movie_id, language) DO UPDATE
		SET source       = EXCLUDED.source,
		    storage_path = EXCLUDED.storage_path,
		    external_id  = EXCLUDED.external_id,
		    updated_at   = NOW()
	`, movieID, language, source, storagePath, externalID)
	if err != nil {
		return fmt.Errorf("subtitle upsert: %w", err)
	}
	return nil
}
