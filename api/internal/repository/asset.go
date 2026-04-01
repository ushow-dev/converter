package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// AssetRepository handles persistence of media_assets.
type AssetRepository struct {
	pool *pgxpool.Pool
}

// NewAssetRepository creates an AssetRepository backed by pool.
func NewAssetRepository(pool *pgxpool.Pool) *AssetRepository {
	return &AssetRepository{pool: pool}
}

// GetByID fetches an asset by its primary key.
func (r *AssetRepository) GetByID(ctx context.Context, assetID string) (*model.Asset, error) {
	a := &model.Asset{}
	err := r.pool.QueryRow(ctx, `
		SELECT asset_id, job_id, storage_path, thumbnail_path, duration_sec,
		       video_codec, audio_codec, is_ready, created_at, updated_at
		FROM media_assets WHERE asset_id = $1`, assetID).
		Scan(&a.AssetID, &a.JobID, &a.StoragePath, &a.ThumbnailPath, &a.DurationSec,
			&a.VideoCodec, &a.AudioCodec, &a.IsReady, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return a, nil
}

// GetByMovieID fetches the ready asset associated with a movie.
func (r *AssetRepository) GetByMovieID(ctx context.Context, movieID int64) (*model.Asset, error) {
	a := &model.Asset{}
	err := r.pool.QueryRow(ctx, `
		SELECT asset_id, job_id, storage_path, thumbnail_path, duration_sec,
		       video_codec, audio_codec, is_ready, created_at, updated_at
		FROM media_assets
		WHERE movie_id = $1
		  AND is_ready = true
		ORDER BY created_at DESC
		LIMIT 1`, movieID).
		Scan(&a.AssetID, &a.JobID, &a.StoragePath, &a.ThumbnailPath, &a.DurationSec,
			&a.VideoCodec, &a.AudioCodec, &a.IsReady, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return a, nil
}

// GetByJobID fetches the asset associated with a job (one-to-one for movie pipeline).
func (r *AssetRepository) GetByJobID(ctx context.Context, jobID string) (*model.Asset, error) {
	a := &model.Asset{}
	err := r.pool.QueryRow(ctx, `
		SELECT asset_id, job_id, storage_path, thumbnail_path, duration_sec,
		       video_codec, audio_codec, is_ready, created_at, updated_at
		FROM media_assets WHERE job_id = $1 LIMIT 1`, jobID).
		Scan(&a.AssetID, &a.JobID, &a.StoragePath, &a.ThumbnailPath, &a.DurationSec,
			&a.VideoCodec, &a.AudioCodec, &a.IsReady, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return a, nil
}

// Create inserts a new asset record.
func (r *AssetRepository) Create(ctx context.Context, a *model.Asset) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO media_assets
			(asset_id, job_id, storage_path, thumbnail_path, duration_sec, video_codec, audio_codec,
			 is_ready, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.AssetID, a.JobID, a.StoragePath, a.ThumbnailPath, a.DurationSec,
		a.VideoCodec, a.AudioCodec, a.IsReady, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert asset: %w", err)
	}
	return nil
}

// MarkReady sets is_ready = true for the given asset.
func (r *AssetRepository) MarkReady(ctx context.Context, assetID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE media_assets SET is_ready = TRUE, updated_at = NOW()
		WHERE asset_id = $1`, assetID)
	return err
}
