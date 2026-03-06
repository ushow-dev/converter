package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

// AssetRepository creates asset records after successful conversion.
type AssetRepository struct {
	pool *pgxpool.Pool
}

// NewAssetRepository creates an AssetRepository.
func NewAssetRepository(pool *pgxpool.Pool) *AssetRepository {
	return &AssetRepository{pool: pool}
}

// Create inserts a new asset record.
func (r *AssetRepository) Create(ctx context.Context, a *model.Asset) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO media_assets
			(asset_id, job_id, movie_id, storage_path, thumbnail_path, duration_sec, video_codec, audio_codec,
			 is_ready, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (asset_id) DO NOTHING`,
		a.AssetID, a.JobID, a.MovieID, a.StoragePath, a.ThumbnailPath, a.DurationSec,
		a.VideoCodec, a.AudioCodec, a.IsReady, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert asset %s: %w", a.AssetID, err)
	}
	return nil
}
