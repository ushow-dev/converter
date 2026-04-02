package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

type StorageLocationRepository struct {
	pool *pgxpool.Pool
}

func NewStorageLocationRepository(pool *pgxpool.Pool) *StorageLocationRepository {
	return &StorageLocationRepository{pool: pool}
}

func (r *StorageLocationRepository) GetByID(ctx context.Context, id int64) (*model.StorageLocation, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, type, base_url FROM storage_locations WHERE id = $1`, id)
	loc := &model.StorageLocation{}
	if err := row.Scan(&loc.ID, &loc.Name, &loc.Type, &loc.BaseURL); err != nil {
		return nil, fmt.Errorf("get storage location: %w", err)
	}
	return loc, nil
}

// GetActiveRemoteID returns the ID of the first active non-local storage location.
// Returns an error if no such location exists.
func (r *StorageLocationRepository) GetActiveRemoteID(ctx context.Context) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM storage_locations WHERE is_active = true AND type != 'local' ORDER BY id LIMIT 1`,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get active remote storage location: %w", err)
	}
	return id, nil
}
