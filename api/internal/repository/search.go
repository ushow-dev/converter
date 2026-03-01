package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// SearchRepository persists normalised search results from indexer backends.
type SearchRepository struct {
	pool *pgxpool.Pool
}

// NewSearchRepository creates a SearchRepository backed by pool.
func NewSearchRepository(pool *pgxpool.Pool) *SearchRepository {
	return &SearchRepository{pool: pool}
}

// UpsertBatch saves results, updating existing records on conflict.
func (r *SearchRepository) UpsertBatch(ctx context.Context, results []model.SearchResult) error {
	for _, sr := range results {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO search_results
				(external_id, title, source_type, source_ref,
				 size_bytes, seeders, leechers, indexer, content_type, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (external_id) DO UPDATE SET
				title        = EXCLUDED.title,
				source_ref   = EXCLUDED.source_ref,
				size_bytes   = EXCLUDED.size_bytes,
				seeders      = EXCLUDED.seeders,
				leechers     = EXCLUDED.leechers,
				indexer      = EXCLUDED.indexer`,
			sr.ExternalID, sr.Title, sr.SourceType, sr.SourceRef,
			sr.SizeBytes, sr.Seeders, sr.Leechers, sr.Indexer,
			sr.ContentType, sr.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("upsert search result %s: %w", sr.ExternalID, err)
		}
	}
	return nil
}
