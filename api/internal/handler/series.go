package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/model"
	"app/api/internal/repository"
)

// SeriesHandler handles /api/admin/series endpoints.
type SeriesHandler struct {
	seriesRepo *repository.SeriesRepository
}

// NewSeriesHandler creates a SeriesHandler.
func NewSeriesHandler(seriesRepo *repository.SeriesRepository) *SeriesHandler {
	return &SeriesHandler{seriesRepo: seriesRepo}
}

// List handles GET /api/admin/series.
func (h *SeriesHandler) List(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	q := r.URL.Query()

	cursor := q.Get("cursor")
	limit := 100
	if ls := q.Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil {
			limit = v
		}
	}

	series, nextCursor, err := h.seriesRepo.List(r.Context(), limit, cursor)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to list series", false, cid)
		return
	}
	if series == nil {
		series = []*model.Series{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":       series,
		"next_cursor": nextCursor,
	})
}

// Get handles GET /api/admin/series/{seriesId}.
func (h *SeriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	idStr := chi.URLParam(r, "seriesId")
	seriesID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid series id", false, cid)
		return
	}

	series, err := h.seriesRepo.GetByID(r.Context(), seriesID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to get series", false, cid)
		return
	}

	dbSeasons, err := h.seriesRepo.ListSeasons(r.Context(), seriesID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to list seasons", false, cid)
		return
	}

	type seasonItem struct {
		ID           int64            `json:"id"`
		SeasonNumber int              `json:"season_number"`
		Episodes     []*model.Episode `json:"episodes"`
	}

	seasonItems := make([]seasonItem, 0, len(dbSeasons))
	for _, s := range dbSeasons {
		episodes, err := h.seriesRepo.ListEpisodes(r.Context(), s.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
				"failed to list episodes", false, cid)
			return
		}
		if episodes == nil {
			episodes = []*model.Episode{}
		}

		seasonItems = append(seasonItems, seasonItem{
			ID:           s.ID,
			SeasonNumber: s.SeasonNumber,
			Episodes:     episodes,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"series":  series,
		"seasons": seasonItems,
	})
}

// Delete handles DELETE /api/admin/series/{seriesId}.
func (h *SeriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	idStr := chi.URLParam(r, "seriesId")
	seriesID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid series id", false, cid)
		return
	}

	if err := h.seriesRepo.DeleteSeries(r.Context(), seriesID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to delete series", false, cid)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
