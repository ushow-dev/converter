package handler

import (
	"errors"
	"net/http"
	"strconv"

	"app/api/internal/auth"
	"app/api/internal/indexer"
	"app/api/internal/service"
)

// SearchHandler handles /api/admin/search.
type SearchHandler struct {
	svc *service.SearchService
}

// NewSearchHandler creates a SearchHandler.
func NewSearchHandler(svc *service.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// Search handles GET /api/admin/search.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	q := r.URL.Query()

	query := q.Get("query")
	if query == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"query parameter is required", false, cid)
		return
	}

	contentType := q.Get("content_type")
	if contentType == "" {
		contentType = "movie"
	}
	if contentType != "movie" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"content_type must be 'movie'", false, cid)
		return
	}

	limit := 50
	if ls := q.Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil {
			limit = v
		}
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}

	results, err := h.svc.Search(r.Context(), query, contentType, limit)
	if err != nil {
		if errors.Is(err, indexer.ErrIndexerUnavailable) {
			respondError(w, http.StatusServiceUnavailable, "INDEXER_UNAVAILABLE",
				"search backend is temporarily unavailable", true, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"search failed", false, cid)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":          results,
		"total":          len(results),
		"correlation_id": cid,
	})
}
