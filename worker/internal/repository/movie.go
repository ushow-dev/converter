package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MovieRepository persists movie metadata records.
type MovieRepository struct {
	pool *pgxpool.Pool
}

// NewMovieRepository creates a MovieRepository.
func NewMovieRepository(pool *pgxpool.Pool) *MovieRepository {
	return &MovieRepository{pool: pool}
}

// Upsert inserts a movie row or returns existing id on unique conflict.
func (r *MovieRepository) Upsert(
	ctx context.Context, imdbID, tmdbID string, posterURL *string,
) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO movies (imdb_id, tmdb_id, poster_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (imdb_id, tmdb_id)
		DO UPDATE SET
			poster_url = COALESCE(movies.poster_url, EXCLUDED.poster_url),
			updated_at = NOW()
		RETURNING id`,
		imdbID, tmdbID, posterURL,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert movie (%s, %s): %w", imdbID, tmdbID, err)
	}
	return id, nil
}
