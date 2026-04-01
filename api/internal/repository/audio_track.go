package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// AudioTrackRepository handles read access to audio tracks.
type AudioTrackRepository struct {
	pool *pgxpool.Pool
}

// NewAudioTrackRepository creates an AudioTrackRepository backed by pool.
func NewAudioTrackRepository(pool *pgxpool.Pool) *AudioTrackRepository {
	return &AudioTrackRepository{pool: pool}
}

// ListByAsset returns all audio tracks for the given asset, ordered by track_index.
func (r *AudioTrackRepository) ListByAsset(ctx context.Context, assetID, assetType string) ([]*model.AudioTrack, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, asset_id, asset_type, track_index, language, label, is_default
		FROM audio_tracks
		WHERE asset_id = $1
		  AND asset_type = $2
		ORDER BY track_index`, assetID, assetType)
	if err != nil {
		return nil, fmt.Errorf("list audio tracks: %w", err)
	}
	defer rows.Close()

	var tracks []*model.AudioTrack
	for rows.Next() {
		t := &model.AudioTrack{}
		if err := rows.Scan(&t.ID, &t.AssetID, &t.AssetType, &t.TrackIndex, &t.Language, &t.Label, &t.IsDefault); err != nil {
			return nil, fmt.Errorf("scan audio track: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}
