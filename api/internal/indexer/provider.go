package indexer

import (
	"context"

	"app/api/internal/model"
)

// Provider is the abstraction layer for release search backends.
// Prowlarr is the concrete implementation for Phase 2.
// Future backends (e.g. Jackett, custom) implement this interface.
type Provider interface {
	// Search queries the indexer backend and returns normalised results.
	Search(ctx context.Context, query, contentType string, limit int) ([]model.SearchResult, error)
}
