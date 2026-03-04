package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// MovieRepository handles read access to movies catalog.
type MovieRepository struct {
	pool *pgxpool.Pool
}

// NewMovieRepository creates a MovieRepository backed by pool.
func NewMovieRepository(pool *pgxpool.Pool) *MovieRepository {
	return &MovieRepository{pool: pool}
}

// GetByIMDbID fetches a movie row by imdb_id.
func (r *MovieRepository) GetByIMDbID(ctx context.Context, imdbID string) (*model.Movie, error) {
	return r.getOne(ctx, `SELECT id, imdb_id, tmdb_id, poster_url, created_at, updated_at FROM movies WHERE imdb_id = $1 LIMIT 1`, imdbID)
}

// GetByTMDBID fetches a movie row by tmdb_id.
func (r *MovieRepository) GetByTMDBID(ctx context.Context, tmdbID string) (*model.Movie, error) {
	return r.getOne(ctx, `SELECT id, imdb_id, tmdb_id, poster_url, created_at, updated_at FROM movies WHERE tmdb_id = $1 LIMIT 1`, tmdbID)
}

func (r *MovieRepository) getOne(ctx context.Context, query, id string) (*model.Movie, error) {
	m := &model.Movie{}
	err := r.pool.QueryRow(ctx, query, id).
		Scan(&m.ID, &m.IMDbID, &m.TMDBID, &m.PosterURL, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return m, nil
}
