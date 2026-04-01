package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

// AudioTrackRepository persists audio track records.
type AudioTrackRepository struct {
	pool *pgxpool.Pool
}

// NewAudioTrackRepository creates an AudioTrackRepository.
func NewAudioTrackRepository(pool *pgxpool.Pool) *AudioTrackRepository {
	return &AudioTrackRepository{pool: pool}
}

// BulkInsert inserts a batch of audio tracks, ignoring duplicates.
func (r *AudioTrackRepository) BulkInsert(ctx context.Context, tracks []model.AudioTrack) error {
	for _, t := range tracks {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO audio_tracks (asset_id, asset_type, track_index, language, label, is_default)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING`,
			t.AssetID, t.AssetType, t.TrackIndex, t.Language, t.Label, t.IsDefault,
		)
		if err != nil {
			return fmt.Errorf("insert audio track (asset=%s index=%d): %w", t.AssetID, t.TrackIndex, err)
		}
	}
	return nil
}
