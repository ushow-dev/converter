package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// SubtitleRepository handles persistence of movie_subtitles.
type SubtitleRepository struct {
	pool *pgxpool.Pool
}

// NewSubtitleRepository creates a SubtitleRepository backed by pool.
func NewSubtitleRepository(pool *pgxpool.Pool) *SubtitleRepository {
	return &SubtitleRepository{pool: pool}
}

// ListByMovieID returns all subtitle tracks for a movie ordered by language.
func (r *SubtitleRepository) ListByMovieID(ctx context.Context, movieID int64) ([]*model.Subtitle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, movie_id, language, source, storage_path, external_id, created_at, updated_at
		FROM movie_subtitles
		WHERE movie_id = $1
		ORDER BY language`, movieID)
	if err != nil {
		return nil, fmt.Errorf("list subtitles: %w", err)
	}
	defer rows.Close()

	var subs []*model.Subtitle
	for rows.Next() {
		s := &model.Subtitle{}
		if err := rows.Scan(&s.ID, &s.MovieID, &s.Language, &s.Source,
			&s.StoragePath, &s.ExternalID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan subtitle: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// Upsert inserts or replaces the subtitle row for (movie_id, language).
func (r *SubtitleRepository) Upsert(ctx context.Context, s *model.Subtitle) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO movie_subtitles (movie_id, language, source, storage_path, external_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (movie_id, language) DO UPDATE
		SET source       = EXCLUDED.source,
		    storage_path = EXCLUDED.storage_path,
		    external_id  = EXCLUDED.external_id,
		    updated_at   = NOW()
	`, s.MovieID, s.Language, s.Source, s.StoragePath, s.ExternalID)
	if err != nil {
		return fmt.Errorf("subtitle upsert: %w", err)
	}
	return nil
}
