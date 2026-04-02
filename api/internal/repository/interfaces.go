package repository

import (
	"context"
	"app/api/internal/model"
)

// MovieReader provides read access to movie records.
type MovieReader interface {
	GetByIMDbID(ctx context.Context, imdbID string) (*model.Movie, error)
	GetByTMDBID(ctx context.Context, tmdbID string) (*model.Movie, error)
	GetByID(ctx context.Context, id int64) (*model.Movie, error)
	List(ctx context.Context, limit int, cursor string) ([]*model.Movie, string, error)
}

// SeriesReader provides read access to series records.
type SeriesReader interface {
	GetByTMDBID(ctx context.Context, tmdbID string) (*model.Series, error)
	GetByID(ctx context.Context, id int64) (*model.Series, error)
	List(ctx context.Context, limit int, cursor string) ([]*model.Series, string, error)
}
