package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// StorageLocationRepository reads the storage_locations table.
type StorageLocationRepository struct {
	pool *pgxpool.Pool
}

func NewStorageLocationRepository(pool *pgxpool.Pool) *StorageLocationRepository {
	return &StorageLocationRepository{pool: pool}
}

// GetByID returns a storage location by id.
func (r *StorageLocationRepository) GetByID(ctx context.Context, id int64) (*model.StorageLocation, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, type, base_url, is_active, created_at
		 FROM storage_locations WHERE id = $1`, id)
	loc := &model.StorageLocation{}
	if err := row.Scan(&loc.ID, &loc.Name, &loc.Type, &loc.BaseURL, &loc.IsActive, &loc.CreatedAt); err != nil {
		return nil, fmt.Errorf("get storage location: %w", err)
	}
	return loc, nil
}
