package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// EpisodeSubtitleRepository handles read access to episode subtitles.
type EpisodeSubtitleRepository struct {
	pool *pgxpool.Pool
}

// NewEpisodeSubtitleRepository creates an EpisodeSubtitleRepository backed by pool.
func NewEpisodeSubtitleRepository(pool *pgxpool.Pool) *EpisodeSubtitleRepository {
	return &EpisodeSubtitleRepository{pool: pool}
}

// ListByEpisodeID returns all subtitles for the given episode, ordered by language.
func (r *EpisodeSubtitleRepository) ListByEpisodeID(ctx context.Context, episodeID int64) ([]*model.EpisodeSubtitle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, episode_id, language, source, storage_path, external_id, created_at, updated_at
		FROM episode_subtitles
		WHERE episode_id = $1
		ORDER BY language`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("list episode subtitles: %w", err)
	}
	defer rows.Close()

	var subtitles []*model.EpisodeSubtitle
	for rows.Next() {
		s := &model.EpisodeSubtitle{}
		if err := rows.Scan(&s.ID, &s.EpisodeID, &s.Language, &s.Source, &s.StoragePath, &s.ExternalID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan episode subtitle: %w", err)
		}
		subtitles = append(subtitles, s)
	}
	return subtitles, rows.Err()
}
