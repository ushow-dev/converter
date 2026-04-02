package handler

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/model"
	"app/api/internal/repository"
)

// SeriesHandler handles /api/admin/series endpoints.
type SeriesHandler struct {
	seriesRepo   *repository.SeriesRepository
	mediaBaseURL string
}

// NewSeriesHandler creates a SeriesHandler.
func NewSeriesHandler(seriesRepo *repository.SeriesRepository, mediaBaseURL string) *SeriesHandler {
	return &SeriesHandler{seriesRepo: seriesRepo, mediaBaseURL: mediaBaseURL}
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

	series, seasons, episodeMap, err := h.seriesRepo.GetSeriesWithEpisodes(r.Context(), seriesID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to get series", false, cid)
		return
	}

	seasonItems := make([]map[string]any, 0, len(seasons))
	for _, s := range seasons {
		eps := episodeMap[s.ID]
		epItems := make([]map[string]any, 0, len(eps))
		for _, ep := range eps {
			item := map[string]any{
				"id":             ep.ID,
				"episode_number": ep.EpisodeNumber,
				"title":          ep.Title,
				"storage_key":    ep.StorageKey,
				"has_thumbnail":  ep.HasAsset && ep.ThumbnailPath != nil,
				"created_at":     ep.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			}
			if ep.HasAsset && ep.ThumbnailPath != nil && h.mediaBaseURL != "" {
				u := buildSeriesMediaURL(h.mediaBaseURL, series.StorageKey, s.SeasonNumber, ep.EpisodeNumber, "thumbnail.jpg")
				item["thumbnail_url"] = u
			}
			epItems = append(epItems, item)
		}

		seasonItems = append(seasonItems, map[string]any{
			"id":            s.ID,
			"season_number": s.SeasonNumber,
			"poster_url":    s.PosterURL,
			"episodes":      epItems,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"series":  series,
		"seasons": seasonItems,
	})
}

// DeleteEpisode handles DELETE /api/admin/episodes/{episodeId}.
func (h *SeriesHandler) DeleteEpisode(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	idStr := chi.URLParam(r, "episodeId")
	episodeID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid episode id", false, cid)
		return
	}
	if err := h.seriesRepo.DeleteEpisode(r.Context(), episodeID); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete episode", false, cid)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EpisodeThumbnail handles GET /api/admin/episodes/{episodeId}/thumbnail.
func (h *SeriesHandler) EpisodeThumbnail(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	episodeID, err := strconv.ParseInt(chi.URLParam(r, "episodeId"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid episode id", false, cid)
		return
	}
	asset, err := h.seriesRepo.GetEpisodeAsset(r.Context(), episodeID)
	if err != nil || asset.ThumbnailPath == nil {
		http.NotFound(w, r)
		return
	}
	// Validate path is under expected directory.
	clean := filepath.Clean(*asset.ThumbnailPath)
	if !strings.HasPrefix(clean, "/media/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, clean)
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
