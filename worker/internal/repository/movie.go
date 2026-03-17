package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	ctx context.Context, imdbID, tmdbID, title string, year *int, posterURL *string,
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
			    year       = COALESCE(year, $5),
			    poster_url = COALESCE(poster_url, $6),
			    updated_at = NOW()
			WHERE id = $1`,
			existing.ID, imdb, tmdb, ttl, year, posterURL); err != nil {
			return nil, fmt.Errorf("update movie %d: %w", existing.ID, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit movie update: %w", err)
		}
		return existing, nil
	}

	// Try "Title (Year)", then "Title (Year) 2", "Title (Year) 3", etc.
	baseKey := buildStorageKey(title, year)
	var m *model.Movie
	for attempt := 1; attempt <= 10; attempt++ {
		key := baseKey
		if attempt > 1 {
			key = fmt.Sprintf("%s %d", baseKey, attempt)
		}
		m = &model.Movie{}
		err = tx.QueryRow(ctx, `
			INSERT INTO movies (storage_key, imdb_id, tmdb_id, title, year, poster_url)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, storage_key, imdb_id, tmdb_id, title, year, poster_url, created_at, updated_at`,
			key, imdb, tmdb, ttl, year, posterURL,
		).Scan(&m.ID, &m.StorageKey, &m.IMDbID, &m.TMDBID, &m.Title, &m.Year, &m.PosterURL, &m.CreatedAt, &m.UpdatedAt)
		if err == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "storage_key") {
			continue // key collision — try suffix
		}
		return nil, fmt.Errorf("insert movie: %w", err)
	}
	if m.ID == 0 {
		return nil, fmt.Errorf("insert movie: exhausted key attempts for %q: %w", baseKey, err)
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

// buildStorageKey builds a human-readable, filesystem-safe folder name.
// Format: "Title(Year)" or "Title" if year is unknown, "untitled_<hex>" if title is empty.
// Spaces are replaced with underscores for clean remote paths.
func buildStorageKey(title string, year *int) string {
	sanitized := strings.Map(func(r rune) rune {
		// Drop chars invalid in folder names across Linux/macOS/rclone remotes
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return -1
		}
		return r
	}, strings.TrimSpace(title))
	sanitized = strings.Join(strings.Fields(sanitized), "_") // collapse whitespace into underscores

	if sanitized == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		return fmt.Sprintf("untitled_%x", b)
	}

	if year != nil && *year > 0 {
		return fmt.Sprintf("%s(%d)", sanitized, *year)
	}
	return sanitized
}

// UpdateStorageLocation updates the storage location for a movie.
func (r *MovieRepository) UpdateStorageLocation(ctx context.Context, movieID, locationID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE movies SET storage_location_id = $2, updated_at = NOW() WHERE id = $1`,
		movieID, locationID)
	return err
}

func nullableText(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
