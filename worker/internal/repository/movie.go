package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

// MovieRepository persists movie metadata records.
type MovieRepository struct {
	pool *pgxpool.Pool
}

// NewMovieRepository creates a MovieRepository.
func NewMovieRepository(pool *pgxpool.Pool) *MovieRepository {
	return &MovieRepository{pool: pool}
}

// Upsert inserts or updates movie metadata and always returns a stable storage key.
func (r *MovieRepository) Upsert(
	ctx context.Context, imdbID, tmdbID, title string, posterURL *string,
) (*model.Movie, error) {
	imdb := nullableText(imdbID)
	tmdb := nullableText(tmdbID)
	ttl := nullableText(title)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin movie upsert tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	existing, err := r.findByExternalID(ctx, tx, imdb, tmdb)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE movies
			SET imdb_id    = COALESCE(imdb_id, $2),
			    tmdb_id    = COALESCE(tmdb_id, $3),
			    title      = COALESCE(title, $4),
			    poster_url = COALESCE(poster_url, $5),
			    updated_at = NOW()
			WHERE id = $1`,
			existing.ID, imdb, tmdb, ttl, posterURL); err != nil {
			return nil, fmt.Errorf("update movie %d: %w", existing.ID, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit movie update: %w", err)
		}
		return existing, nil
	}

	m := &model.Movie{}
	if err := tx.QueryRow(ctx, `
		INSERT INTO movies (storage_key, imdb_id, tmdb_id, title, poster_url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, storage_key, imdb_id, tmdb_id, title, year, poster_url, created_at, updated_at`,
		generateStorageKey(), imdb, tmdb, ttl, posterURL,
	).Scan(&m.ID, &m.StorageKey, &m.IMDbID, &m.TMDBID, &m.Title, &m.Year, &m.PosterURL, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert movie: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit movie insert: %w", err)
	}
	return m, nil
}

func (r *MovieRepository) findByExternalID(
	ctx context.Context,
	tx pgx.Tx,
	imdbID *string,
	tmdbID *string,
) (*model.Movie, error) {
	if imdbID != nil {
		m, err := fetchMovieBy(ctx, tx, "imdb_id", *imdbID)
		if err != nil {
			return nil, err
		}
		if m != nil {
			return m, nil
		}
	}
	if tmdbID != nil {
		m, err := fetchMovieBy(ctx, tx, "tmdb_id", *tmdbID)
		if err != nil {
			return nil, err
		}
		if m != nil {
			return m, nil
		}
	}
	return nil, nil
}

func fetchMovieBy(ctx context.Context, tx pgx.Tx, field, value string) (*model.Movie, error) {
	query := fmt.Sprintf(`
		SELECT id, storage_key, imdb_id, tmdb_id, title, year, poster_url, created_at, updated_at
		FROM movies WHERE %s = $1 LIMIT 1`, field)

	m := &model.Movie{}
	err := tx.QueryRow(ctx, query, value).
		Scan(&m.ID, &m.StorageKey, &m.IMDbID, &m.TMDBID, &m.Title, &m.Year, &m.PosterURL, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch movie by %s: %w", field, err)
	}
	return m, nil
}

func generateStorageKey() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("mov_%x", b)
}

func nullableText(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
