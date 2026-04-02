package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// SeriesRepository handles read/write access to the series catalog.
type SeriesRepository struct {
	pool *pgxpool.Pool
}

// NewSeriesRepository creates a SeriesRepository backed by pool.
func NewSeriesRepository(pool *pgxpool.Pool) *SeriesRepository {
	return &SeriesRepository{pool: pool}
}

// GetByTMDBID fetches a series row by tmdb_id. Returns ErrNotFound if absent.
func (r *SeriesRepository) GetByTMDBID(ctx context.Context, tmdbID string) (*model.Series, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		FROM series
		WHERE tmdb_id = $1
		LIMIT 1`, tmdbID)
	if err != nil {
		return nil, fmt.Errorf("query series by tmdb_id: %w", err)
	}
	defer rows.Close()

	result, err := scanSeriesRows(rows)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	return result[0], nil
}

// GetByID fetches a series row by its primary key. Returns ErrNotFound if absent.
func (r *SeriesRepository) GetByID(ctx context.Context, id int64) (*model.Series, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		FROM series
		WHERE id = $1
		LIMIT 1`, id)
	if err != nil {
		return nil, fmt.Errorf("query series by id: %w", err)
	}
	defer rows.Close()

	result, err := scanSeriesRows(rows)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	return result[0], nil
}

// List returns series ordered by creation time descending, with cursor pagination.
func (r *SeriesRepository) List(ctx context.Context, limit int, cursor string) ([]*model.Series, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	const base = `
		SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		FROM series`

	var rows pgx.Rows
	var err error
	if cursor != "" {
		rows, err = r.pool.Query(ctx,
			base+` WHERE created_at < $1::timestamptz ORDER BY created_at DESC LIMIT $2`,
			cursor, limit+1)
	} else {
		rows, err = r.pool.Query(ctx,
			base+` ORDER BY created_at DESC LIMIT $1`,
			limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list series: %w", err)
	}
	defer rows.Close()

	result, err := scanSeriesRows(rows)
	if err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(result) > limit {
		result = result[:limit]
		nextCursor = result[limit-1].CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z")
	}
	return result, nextCursor, nil
}

// ListSeasons returns all seasons for a series ordered by season_number.
func (r *SeriesRepository) ListSeasons(ctx context.Context, seriesID int64) ([]*model.Season, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, series_id, season_number, poster_url, created_at, updated_at
		FROM seasons
		WHERE series_id = $1
		ORDER BY season_number`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("list seasons: %w", err)
	}
	defer rows.Close()

	var seasons []*model.Season
	for rows.Next() {
		s := &model.Season{}
		if err := rows.Scan(&s.ID, &s.SeriesID, &s.SeasonNumber, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan season: %w", err)
		}
		seasons = append(seasons, s)
	}
	return seasons, rows.Err()
}

// ListEpisodes returns all episodes for a season ordered by episode_number.
func (r *SeriesRepository) ListEpisodes(ctx context.Context, seasonID int64) ([]*model.Episode, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, season_id, episode_number, title, storage_key, created_at, updated_at
		FROM episodes
		WHERE season_id = $1
		ORDER BY episode_number`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	defer rows.Close()

	var episodes []*model.Episode
	for rows.Next() {
		e := &model.Episode{}
		if err := rows.Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &e.Title, &e.StorageKey, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		episodes = append(episodes, e)
	}
	return episodes, rows.Err()
}

// GetEpisodeBySE finds an episode by series tmdb_id, season number and episode number.
// Returns ErrNotFound if no match exists.
func (r *SeriesRepository) GetEpisodeBySE(ctx context.Context, seriesTMDBID string, seasonNum, episodeNum int) (*model.Episode, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.season_id, e.episode_number, e.title, e.storage_key, e.created_at, e.updated_at
		FROM episodes e
		JOIN seasons sn ON sn.id = e.season_id
		JOIN series s ON s.id = sn.series_id
		WHERE s.tmdb_id = $1
		  AND sn.season_number = $2
		  AND e.episode_number = $3
		LIMIT 1`, seriesTMDBID, seasonNum, episodeNum)
	if err != nil {
		return nil, fmt.Errorf("query episode by S/E: %w", err)
	}
	defer rows.Close()

	var episodes []*model.Episode
	for rows.Next() {
		e := &model.Episode{}
		if err := rows.Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &e.Title, &e.StorageKey, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		episodes = append(episodes, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(episodes) == 0 {
		return nil, ErrNotFound
	}
	return episodes[0], nil
}

// GetEpisodeAsset returns the ready asset for an episode. Returns ErrNotFound if absent.
func (r *SeriesRepository) GetEpisodeAsset(ctx context.Context, episodeID int64) (*model.EpisodeAsset, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT asset_id, job_id, episode_id, storage_path, thumbnail_path,
		       duration_sec, video_codec, audio_codec, is_ready, created_at, updated_at
		FROM episode_assets
		WHERE episode_id = $1
		  AND is_ready = true
		LIMIT 1`, episodeID)

	a := &model.EpisodeAsset{}
	err := row.Scan(
		&a.AssetID, &a.JobID, &a.EpisodeID, &a.StoragePath, &a.ThumbnailPath,
		&a.DurationSec, &a.VideoCodec, &a.AudioCodec, &a.IsReady, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get episode asset: %w", err)
	}
	return a, nil
}

// DeleteEpisode deletes an episode by ID.
func (r *SeriesRepository) DeleteEpisode(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM episodes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete episode: %w", err)
	}
	return nil
}

// DeleteSeries deletes a series by ID. Cascades handle related rows.
func (r *SeriesRepository) DeleteSeries(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM series WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete series: %w", err)
	}
	return nil
}

// EpisodeDetail holds episode data with asset info from a JOIN query.
type EpisodeDetail struct {
	ID            int64
	SeasonID      int64
	EpisodeNumber int
	Title         *string
	StorageKey    string
	HasAsset      bool
	ThumbnailPath *string
	AssetID       *string
	CreatedAt     time.Time
}

// GetSeriesWithEpisodes fetches a series with all seasons, episodes, and asset readiness in one query.
func (r *SeriesRepository) GetSeriesWithEpisodes(ctx context.Context, seriesID int64) (
	*model.Series, []*model.Season, map[int64][]EpisodeDetail, error,
) {
	// First get the series itself.
	series, err := r.GetByID(ctx, seriesID)
	if err != nil {
		return nil, nil, nil, err
	}

	// Single query for all seasons, episodes, and their assets.
	rows, err := r.pool.Query(ctx, `
		SELECT sn.id, sn.series_id, sn.season_number, sn.poster_url, sn.created_at, sn.updated_at,
		       e.id, e.episode_number, e.title, e.storage_key, e.created_at,
		       ea.asset_id, ea.thumbnail_path, (ea.asset_id IS NOT NULL) AS has_asset
		FROM seasons sn
		LEFT JOIN episodes e ON e.season_id = sn.id
		LEFT JOIN episode_assets ea ON ea.episode_id = e.id AND ea.is_ready = true
		WHERE sn.series_id = $1
		ORDER BY sn.season_number, e.episode_number`,
		seriesID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get series episodes: %w", err)
	}
	defer rows.Close()

	seasonMap := map[int64]*model.Season{}
	var seasonOrder []int64
	episodeMap := map[int64][]EpisodeDetail{}

	for rows.Next() {
		var sn model.Season
		var epID *int64
		var epNum *int
		var epTitle *string
		var epKey *string
		var epCreated *time.Time
		var assetID *string
		var thumbPath *string
		var hasAsset bool

		if err := rows.Scan(
			&sn.ID, &sn.SeriesID, &sn.SeasonNumber, &sn.PosterURL, &sn.CreatedAt, &sn.UpdatedAt,
			&epID, &epNum, &epTitle, &epKey, &epCreated,
			&assetID, &thumbPath, &hasAsset,
		); err != nil {
			return nil, nil, nil, fmt.Errorf("scan series detail: %w", err)
		}

		if _, ok := seasonMap[sn.ID]; !ok {
			s := sn // copy
			seasonMap[sn.ID] = &s
			seasonOrder = append(seasonOrder, sn.ID)
		}

		if epID != nil {
			ed := EpisodeDetail{
				ID:            *epID,
				SeasonID:      sn.ID,
				EpisodeNumber: *epNum,
				Title:         epTitle,
				StorageKey:    *epKey,
				HasAsset:      hasAsset,
				ThumbnailPath: thumbPath,
				AssetID:       assetID,
				CreatedAt:     *epCreated,
			}
			episodeMap[sn.ID] = append(episodeMap[sn.ID], ed)
		}
	}

	seasons := make([]*model.Season, 0, len(seasonOrder))
	for _, id := range seasonOrder {
		seasons = append(seasons, seasonMap[id])
	}

	return series, seasons, episodeMap, rows.Err()
}

func scanSeriesRows(rows pgx.Rows) ([]*model.Series, error) {
	var result []*model.Series
	for rows.Next() {
		s := &model.Series{}
		if err := rows.Scan(
			&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan series: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
