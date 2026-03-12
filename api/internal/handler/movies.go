package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/model"
	"app/api/internal/repository"
)

// MoviesHandler handles /api/admin/movies endpoints.
type MoviesHandler struct {
	tmdbAPIKey string
	movieRepo  *repository.MovieRepository
}

// NewMoviesHandler creates a MoviesHandler.
func NewMoviesHandler(tmdbAPIKey string, movieRepo *repository.MovieRepository) *MoviesHandler {
	return &MoviesHandler{tmdbAPIKey: tmdbAPIKey, movieRepo: movieRepo}
}

type tmdbMovieResponse struct {
	Title       string `json:"title"`
	IMDbID      string `json:"imdb_id"`
	PosterPath  string `json:"poster_path"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"release_date"`
}

// TMDBLookup handles GET /api/admin/movies/tmdb/{tmdbId}.
// Fetches movie metadata from TMDB and returns title + imdb_id.
func (h *MoviesHandler) TMDBLookup(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	tmdbID := chi.URLParam(r, "tmdbId")

	if h.tmdbAPIKey == "" {
		respondError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED",
			"TMDB API key is not configured", false, cid)
		return
	}

	url := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s", tmdbID, h.tmdbAPIKey)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to build TMDB request", false, cid)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		respondError(w, http.StatusBadGateway, "TMDB_ERROR",
			"failed to reach TMDB", true, cid)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		respondError(w, http.StatusNotFound, "NOT_FOUND",
			"movie not found on TMDB", false, cid)
		return
	}
	if resp.StatusCode != http.StatusOK {
		respondError(w, http.StatusBadGateway, "TMDB_ERROR",
			fmt.Sprintf("TMDB returned status %d", resp.StatusCode), true, cid)
		return
	}

	var tmdb tmdbMovieResponse
	if err := json.NewDecoder(resp.Body).Decode(&tmdb); err != nil {
		respondError(w, http.StatusBadGateway, "TMDB_ERROR",
			"failed to parse TMDB response", false, cid)
		return
	}

	posterURL := ""
	if tmdb.PosterPath != "" {
		posterURL = "https://image.tmdb.org/t/p/w342" + tmdb.PosterPath
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"title":        tmdb.Title,
		"imdb_id":      tmdb.IMDbID,
		"poster_url":   posterURL,
		"overview":     tmdb.Overview,
		"release_date": tmdb.ReleaseDate,
	})
}

type tmdbSearchResponse struct {
	Results []struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		PosterPath  string `json:"poster_path"`
	} `json:"results"`
}

// TMDBSearch handles GET /api/admin/movies/tmdb/search?q=title&year=2025.
// Returns the best TMDB match for a title, used by the remote-download flow.
func (h *MoviesHandler) TMDBSearch(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	q := r.URL.Query().Get("q")
	if q == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "q is required", false, cid)
		return
	}
	if h.tmdbAPIKey == "" {
		respondError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED", "TMDB API key is not configured", false, cid)
		return
	}

	params := url.Values{}
	params.Set("query", q)
	params.Set("api_key", h.tmdbAPIKey)
	if year := r.URL.Query().Get("year"); year != "" {
		params.Set("year", year)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.themoviedb.org/3/search/movie?"+params.Encode(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build TMDB request", false, cid)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		respondError(w, http.StatusBadGateway, "TMDB_ERROR", "failed to reach TMDB", true, cid)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respondError(w, http.StatusBadGateway, "TMDB_ERROR",
			fmt.Sprintf("TMDB returned status %d", resp.StatusCode), true, cid)
		return
	}

	var tmdb tmdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&tmdb); err != nil {
		respondError(w, http.StatusBadGateway, "TMDB_ERROR", "failed to parse TMDB response", false, cid)
		return
	}
	if len(tmdb.Results) == 0 {
		respondJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}

	best := tmdb.Results[0]
	posterURL := ""
	if best.PosterPath != "" {
		posterURL = "https://image.tmdb.org/t/p/w342" + best.PosterPath
	}
	year := ""
	if len(best.ReleaseDate) >= 4 {
		year = best.ReleaseDate[:4]
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"found":      true,
		"tmdb_id":    fmt.Sprintf("%d", best.ID),
		"title":      best.Title,
		"year":       year,
		"poster_url": posterURL,
	})
}

// UpdateIDs handles PATCH /api/admin/movies/{movieId}.
// Updates imdb_id, tmdb_id, and/or title. Empty string clears the field.
func (h *MoviesHandler) UpdateIDs(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	idStr := chi.URLParam(r, "movieId")
	movieID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid movie id", false, cid)
		return
	}

	var req struct {
		IMDbID string `json:"imdb_id"`
		TMDBID string `json:"tmdb_id"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", false, cid)
		return
	}

	if err := h.movieRepo.UpdateMeta(r.Context(), movieID, req.IMDbID, req.TMDBID, req.Title); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update movie", false, cid)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /api/admin/movies.
func (h *MoviesHandler) List(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	q := r.URL.Query()

	cursor := q.Get("cursor")
	limit := 100
	if ls := q.Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil {
			limit = v
		}
	}

	movies, nextCursor, err := h.movieRepo.List(r.Context(), limit, cursor)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to list movies", false, cid)
		return
	}
	if movies == nil {
		movies = []*model.Movie{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":       movies,
		"next_cursor": nextCursor,
	})
}

// Thumbnail handles GET /api/admin/movies/{movieId}/thumbnail.
// Serves the JPEG thumbnail. Accepts JWT via Authorization header or ?token= query parameter.
func (h *MoviesHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	idStr := chi.URLParam(r, "movieId")
	movieID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"invalid movie id", false, cid)
		return
	}

	path, err := h.movieRepo.ThumbnailPath(r.Context(), movieID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND",
				"no thumbnail for this movie", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch thumbnail", false, cid)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}
