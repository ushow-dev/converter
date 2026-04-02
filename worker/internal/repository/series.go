package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

// SeriesRepository persists series, season, episode and episode asset records.
type SeriesRepository struct {
	pool *pgxpool.Pool
}

// NewSeriesRepository creates a SeriesRepository.
func NewSeriesRepository(pool *pgxpool.Pool) *SeriesRepository {
	return &SeriesRepository{pool: pool}
}

// UpsertSeries finds a series by tmdb_id, updates it if found, inserts it otherwise.
func (r *SeriesRepository) UpsertSeries(
	ctx context.Context,
	tmdbID, imdbID, title string,
	year *int,
	posterURL *string,
	storageKey string,
) (*model.Series, error) {
	tmdb := nullableText(tmdbID)
	imdb := nullableText(imdbID)

	// Try to find existing series by tmdb_id first.
	if tmdb != nil {
		s := &model.Series{}
		err := r.pool.QueryRow(ctx, `
			SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
			FROM series WHERE tmdb_id = $1 LIMIT 1`, *tmdb,
		).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
		if err != nil && err != pgx.ErrNoRows {
			return nil, fmt.Errorf("find series by tmdb_id: %w", err)
		}
		if err == nil {
			// Found — update fields that are not yet populated.
			if _, err := r.pool.Exec(ctx, `
				UPDATE series
				SET imdb_id    = COALESCE(imdb_id, $2),
				    title      = COALESCE(title, $3),
				    year       = COALESCE(year, $4),
				    poster_url = COALESCE(poster_url, $5),
				    updated_at = NOW()
				WHERE id = $1`,
				s.ID, imdb, nullableText(title), year, posterURL); err != nil {
				return nil, fmt.Errorf("update series %d: %w", s.ID, err)
			}
			return s, nil
		}
	}

	// Insert new series.
	key := storageKey
	if key == "" {
		key = buildSeriesStorageKey(title, year, tmdb)
	}
	s := &model.Series{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO series (storage_key, tmdb_id, imdb_id, title, year, poster_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (storage_key) DO UPDATE
		    SET tmdb_id    = COALESCE(series.tmdb_id, EXCLUDED.tmdb_id),
		        imdb_id    = COALESCE(series.imdb_id, EXCLUDED.imdb_id),
		        title      = COALESCE(series.title, EXCLUDED.title),
		        year       = COALESCE(series.year, EXCLUDED.year),
		        poster_url = COALESCE(series.poster_url, EXCLUDED.poster_url),
		        updated_at = NOW()
		RETURNING id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at`,
		key, tmdb, imdb, nullableText(title), year, posterURL,
	).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert series: %w", err)
	}
	return s, nil
}

// UpsertSeason inserts or updates a season record for the given series.
func (r *SeriesRepository) UpsertSeason(
	ctx context.Context,
	seriesID int64,
	seasonNumber int,
) (*model.Season, error) {
	s := &model.Season{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO seasons (series_id, season_number)
		VALUES ($1, $2)
		ON CONFLICT (series_id, season_number) DO UPDATE
		    SET updated_at = NOW()
		RETURNING id, series_id, season_number`,
		seriesID, seasonNumber,
	).Scan(&s.ID, &s.SeriesID, &s.SeasonNumber)
	if err != nil {
		return nil, fmt.Errorf("upsert season (series=%d season=%d): %w", seriesID, seasonNumber, err)
	}
	return s, nil
}

// UpsertEpisode inserts or updates an episode record within a season.
func (r *SeriesRepository) UpsertEpisode(
	ctx context.Context,
	seasonID int64,
	episodeNumber int,
	title *string,
	storageKey string,
) (*model.Episode, error) {
	e := &model.Episode{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO episodes (season_id, episode_number, title, storage_key)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (season_id, episode_number) DO UPDATE
		    SET title       = COALESCE(episodes.title, EXCLUDED.title),
		        storage_key = EXCLUDED.storage_key,
		        updated_at  = NOW()
		RETURNING id, season_id, episode_number, title, storage_key`,
		seasonID, episodeNumber, title, storageKey,
	).Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &e.Title, &e.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("upsert episode (season=%d ep=%d): %w", seasonID, episodeNumber, err)
	}
	return e, nil
}

// CreateEpisodeAsset inserts a new episode asset record.
func (r *SeriesRepository) CreateEpisodeAsset(ctx context.Context, a *model.EpisodeAsset) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO episode_assets
		    (asset_id, job_id, episode_id, storage_path, thumbnail_path, duration_sec,
		     video_codec, audio_codec, is_ready)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		a.AssetID, a.JobID, a.EpisodeID, a.StoragePath, a.ThumbnailPath,
		a.DurationSec, a.VideoCodec, a.AudioCodec, a.IsReady,
	)
	if err != nil {
		return fmt.Errorf("insert episode asset (episode=%d): %w", a.EpisodeID, err)
	}
	return nil
}

// GetSeriesByID returns a single series by primary key.
func (r *SeriesRepository) GetSeriesByID(ctx context.Context, id int64) (*model.Series, error) {
	s := &model.Series{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		FROM series WHERE id = $1`, id,
	).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get series %d: %w", id, err)
	}
	return s, nil
}

// UpdateSeriesMeta updates title, year, and poster_url for a series.
func (r *SeriesRepository) UpdateSeriesMeta(ctx context.Context, id int64, title string, year *int, posterURL *string) {
	_, _ = r.pool.Exec(ctx, `
		UPDATE series
		SET title      = COALESCE(NULLIF($2, ''), title),
		    year       = COALESCE($3, year),
		    poster_url = COALESCE($4, poster_url),
		    updated_at = NOW()
		WHERE id = $1`, id, title, year, posterURL)
}

// buildSeriesStorageKey builds a filesystem-safe folder name for a series.
// Format matches movie convention: {slug}_{year}_[{tmdb_id}].
func buildSeriesStorageKey(title string, year *int, tmdbID *string) string {
	return buildStorageKey(title, year, tmdbID)
}
