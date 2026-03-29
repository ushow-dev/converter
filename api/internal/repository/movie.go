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
	return r.getOne(ctx, `
		SELECT m.id, m.storage_key, m.imdb_id, m.tmdb_id, m.title, m.year, m.poster_url,
		       (a.thumbnail_path IS NOT NULL) AS has_thumbnail,
		       a.job_id,
		       m.storage_location_id,
		       m.created_at, m.updated_at
		FROM movies m
		LEFT JOIN media_assets a ON a.movie_id = m.id AND a.is_ready = true
		WHERE m.imdb_id = $1 LIMIT 1`, imdbID)
}

// GetByTMDBID fetches a movie row by tmdb_id.
func (r *MovieRepository) GetByTMDBID(ctx context.Context, tmdbID string) (*model.Movie, error) {
	return r.getOne(ctx, `
		SELECT m.id, m.storage_key, m.imdb_id, m.tmdb_id, m.title, m.year, m.poster_url,
		       (a.thumbnail_path IS NOT NULL) AS has_thumbnail,
		       a.job_id,
		       m.storage_location_id,
		       m.created_at, m.updated_at
		FROM movies m
		LEFT JOIN media_assets a ON a.movie_id = m.id AND a.is_ready = true
		WHERE m.tmdb_id = $1 LIMIT 1`, tmdbID)
}

// List returns movies ordered by creation time descending, with cursor pagination.
func (r *MovieRepository) List(ctx context.Context, limit int, cursor string) ([]*model.Movie, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	const base = `
		SELECT m.id, m.storage_key, m.imdb_id, m.tmdb_id, m.title, m.year, m.poster_url,
		       (a.thumbnail_path IS NOT NULL) AS has_thumbnail,
		       a.job_id,
		       m.storage_location_id,
		       m.created_at, m.updated_at
		FROM movies m
		LEFT JOIN media_assets a ON a.movie_id = m.id AND a.is_ready = true`

	var rows pgx.Rows
	var err error
	if cursor != "" {
		rows, err = r.pool.Query(ctx,
			base+` WHERE m.created_at < $1::timestamptz ORDER BY m.created_at DESC LIMIT $2`,
			cursor, limit+1)
	} else {
		rows, err = r.pool.Query(ctx,
			base+` ORDER BY m.created_at DESC LIMIT $1`,
			limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list movies: %w", err)
	}
	defer rows.Close()

	movies, err := scanMovieRows(rows)
	if err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(movies) > limit {
		movies = movies[:limit]
		nextCursor = movies[limit-1].CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z")
	}
	return movies, nextCursor, nil
}

// ListReadyTMDBIDs returns tmdb_id values for movies that have at least one
// ready asset. When since is non-nil only movies updated after that timestamp
// are returned.
func (r *MovieRepository) ListReadyTMDBIDs(ctx context.Context, since *time.Time) ([]string, error) {
	const base = `
		SELECT DISTINCT m.tmdb_id
		FROM movies m
		JOIN media_assets a ON a.movie_id = m.id
		WHERE a.is_ready = true
		  AND m.tmdb_id IS NOT NULL`

	var (
		rows pgx.Rows
		err  error
	)
	if since != nil {
		rows, err = r.pool.Query(ctx, base+` AND m.updated_at > $1 ORDER BY m.updated_at ASC`, *since)
	} else {
		rows, err = r.pool.Query(ctx, base+` ORDER BY m.updated_at ASC`)
	}
	if err != nil {
		return nil, fmt.Errorf("list ready tmdb ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tmdb id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateMeta updates imdb_id, tmdb_id and title for a movie.
// Empty string clears the corresponding field (stores NULL).
func (r *MovieRepository) UpdateMeta(ctx context.Context, movieID int64, imdbID, tmdbID, title string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE movies
		SET imdb_id = NULLIF($2, ''), tmdb_id = NULLIF($3, ''), title = NULLIF($4, ''), updated_at = NOW()
		WHERE id = $1`,
		movieID, imdbID, tmdbID, title)
	return err
}

// UpdateStorageLocation sets the storage_location_id for a movie.
func (r *MovieRepository) UpdateStorageLocation(ctx context.Context, movieID, locationID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE movies SET storage_location_id = $2, updated_at = NOW() WHERE id = $1`,
		movieID, locationID)
	return err
}

// GetByID fetches a movie row by its primary key.
func (r *MovieRepository) GetByID(ctx context.Context, id int64) (*model.Movie, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.storage_key, m.imdb_id, m.tmdb_id, m.title, m.year, m.poster_url,
		       (a.thumbnail_path IS NOT NULL) AS has_thumbnail,
		       a.job_id,
		       m.storage_location_id,
		       m.created_at, m.updated_at
		FROM movies m
		LEFT JOIN media_assets a ON a.movie_id = m.id AND a.is_ready = true
		WHERE m.id = $1 LIMIT 1`, id)
	if err != nil {
		return nil, fmt.Errorf("query movie by id: %w", err)
	}
	defer rows.Close()

	movies, err := scanMovieRows(rows)
	if err != nil {
		return nil, err
	}
	if len(movies) == 0 {
		return nil, ErrNotFound
	}
	return movies[0], nil
}

// ThumbnailPath returns the filesystem path to the thumbnail for a movie, or ErrNotFound.
func (r *MovieRepository) ThumbnailPath(ctx context.Context, movieID int64) (string, error) {
	var path string
	err := r.pool.QueryRow(ctx,
		`SELECT a.thumbnail_path FROM media_assets a WHERE a.movie_id = $1 AND a.thumbnail_path IS NOT NULL LIMIT 1`,
		movieID,
	).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) || path == "" {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get thumbnail path: %w", err)
	}
	return path, nil
}

func (r *MovieRepository) getOne(ctx context.Context, query, id string) (*model.Movie, error) {
	rows, err := r.pool.Query(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("query movie: %w", err)
	}
	defer rows.Close()

	movies, err := scanMovieRows(rows)
	if err != nil {
		return nil, err
	}
	if len(movies) == 0 {
		return nil, ErrNotFound
	}
	return movies[0], nil
}

func scanMovieRows(rows pgx.Rows) ([]*model.Movie, error) {
	var movies []*model.Movie
	for rows.Next() {
		m := &model.Movie{}
		if err := rows.Scan(
			&m.ID, &m.StorageKey, &m.IMDbID, &m.TMDBID, &m.Title, &m.Year, &m.PosterURL,
			&m.HasThumbnail, &m.JobID,
			&m.StorageLocationID,
			&m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan movie: %w", err)
		}
		movies = append(movies, m)
	}
	return movies, rows.Err()
}
