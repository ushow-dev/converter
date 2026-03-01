package service

import (
	"context"
	"errors"
	"log/slog"

	"app/api/internal/indexer"
	"app/api/internal/model"
	"app/api/internal/repository"
)

// SearchService coordinates release search via an indexer backend.
type SearchService struct {
	provider indexer.Provider
	repo     *repository.SearchRepository
}

// NewSearchService creates a SearchService.
func NewSearchService(provider indexer.Provider, repo *repository.SearchRepository) *SearchService {
	return &SearchService{provider: provider, repo: repo}
}

// Search fetches releases from the indexer, saves them to DB, and returns results.
// If the indexer is unavailable it returns ErrIndexerUnavailable.
func (s *SearchService) Search(
	ctx context.Context, query, contentType string, limit int,
) ([]model.SearchResult, error) {
	results, err := s.provider.Search(ctx, query, contentType, limit)
	if err != nil {
		if errors.Is(err, indexer.ErrIndexerUnavailable) {
			return nil, err
		}
		slog.Error("search provider error", "error", err)
		return nil, indexer.ErrIndexerUnavailable
	}

	// Persist in background — search results are a cache, failure is non-fatal.
	if len(results) > 0 {
		if err := s.repo.UpsertBatch(ctx, results); err != nil {
			slog.Warn("failed to persist search results", "error", err)
		}
	}

	return results, nil
}
