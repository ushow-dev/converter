package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/model"
	"app/api/internal/repository"
	"app/api/internal/subtitles"
)

// SubtitleHandler handles /api/admin/movies/{movieId}/subtitles endpoints.
type SubtitleHandler struct {
	movieRepo    *repository.MovieRepository
	subtitleRepo *repository.SubtitleRepository
	osClient     *subtitles.Client // nil if API key not configured
	mediaRoot    string
	languages    []string
}

// NewSubtitleHandler creates a SubtitleHandler.
func NewSubtitleHandler(
	movieRepo *repository.MovieRepository,
	subtitleRepo *repository.SubtitleRepository,
	osClient *subtitles.Client,
	mediaRoot string,
	languages []string,
) *SubtitleHandler {
	return &SubtitleHandler{
		movieRepo:    movieRepo,
		subtitleRepo: subtitleRepo,
		osClient:     osClient,
		mediaRoot:    mediaRoot,
		languages:    languages,
	}
}

// List handles GET /api/admin/movies/{movieId}/subtitles.
func (h *SubtitleHandler) List(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	movieID, err := parseMovieID(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PARAM", "invalid movie id", false, cid)
		return
	}

	subs, err := h.subtitleRepo.ListByMovieID(r.Context(), movieID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list subtitles", false, cid)
		return
	}
	if subs == nil {
		subs = []*model.Subtitle{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": subs})
}

// Upload handles POST /api/admin/movies/{movieId}/subtitles.
// Accepts multipart form with fields: language (string) and file (.vtt or .srt).
func (h *SubtitleHandler) Upload(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	movieID, err := parseMovieID(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PARAM", "invalid movie id", false, cid)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_FORM", "failed to parse form", false, cid)
		return
	}

	language := strings.TrimSpace(r.FormValue("language"))
	if language == "" {
		respondError(w, http.StatusBadRequest, "MISSING_PARAM", "language is required", false, cid)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "MISSING_FILE", "file is required", false, cid)
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "IO_ERROR", "failed to read file", false, cid)
		return
	}

	// Convert SRT to VTT if needed.
	var vttData []byte
	if strings.HasSuffix(strings.ToLower(header.Filename), ".srt") {
		vttData = subtitles.SRTtoVTT(raw)
	} else {
		vttData = raw
	}

	// Resolve movie's storage_key to build the output path.
	movie, err := h.movieRepo.GetByID(r.Context(), movieID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "movie not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch movie", false, cid)
		return
	}

	subtitleDir := filepath.Join(h.mediaRoot, "converted", "movies", movie.StorageKey, "subtitles")
	if err := os.MkdirAll(subtitleDir, 0o777); err != nil {
		respondError(w, http.StatusInternalServerError, "IO_ERROR", "failed to create subtitle dir", false, cid)
		return
	}

	filePath := filepath.Join(subtitleDir, language+".vtt")
	if err := os.WriteFile(filePath, vttData, 0o666); err != nil {
		respondError(w, http.StatusInternalServerError, "IO_ERROR", "failed to write subtitle file", false, cid)
		return
	}

	sub := &model.Subtitle{
		MovieID:     movieID,
		Language:    language,
		Source:      "upload",
		StoragePath: filePath,
	}
	if err := h.subtitleRepo.Upsert(r.Context(), sub); err != nil {
		respondError(w, http.StatusInternalServerError, "DB_ERROR", "failed to save subtitle", false, cid)
		return
	}

	subs, _ := h.subtitleRepo.ListByMovieID(r.Context(), movieID)
	if subs == nil {
		subs = []*model.Subtitle{}
	}
	respondJSON(w, http.StatusCreated, map[string]any{"items": subs})
}

// Search handles POST /api/admin/movies/{movieId}/subtitles/search.
// Triggers an OpenSubtitles search and saves found subtitles.
func (h *SubtitleHandler) Search(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	movieID, err := parseMovieID(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PARAM", "invalid movie id", false, cid)
		return
	}

	if h.osClient == nil {
		respondError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED",
			"OPENSUBTITLES_API_KEY is not configured", false, cid)
		return
	}

	movie, err := h.movieRepo.GetByID(r.Context(), movieID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "movie not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch movie", false, cid)
		return
	}

	if movie.TMDBID == nil || *movie.TMDBID == "" {
		respondError(w, http.StatusUnprocessableEntity, "MISSING_TMDB_ID",
			"movie has no tmdb_id; cannot search subtitles", false, cid)
		return
	}

	results, err := h.osClient.Search(r.Context(), *movie.TMDBID, h.languages)
	if err != nil {
		respondError(w, http.StatusBadGateway, "OPENSUBTITLES_ERROR",
			fmt.Sprintf("subtitle search failed: %s", err), true, cid)
		return
	}

	subtitleDir := filepath.Join(h.mediaRoot, "converted", "movies", movie.StorageKey, "subtitles")
	if err := os.MkdirAll(subtitleDir, 0o777); err != nil {
		respondError(w, http.StatusInternalServerError, "IO_ERROR", "failed to create subtitle dir", false, cid)
		return
	}

	for _, res := range results {
		downloadURL, err := h.osClient.DownloadURL(r.Context(), res.FileID)
		if err != nil {
			continue // non-fatal per language
		}
		raw, err := h.osClient.FetchRaw(r.Context(), downloadURL)
		if err != nil {
			continue
		}
		vttData := subtitles.SRTtoVTT(raw)
		filePath := filepath.Join(subtitleDir, res.Language+".vtt")
		if err := os.WriteFile(filePath, vttData, 0o666); err != nil {
			continue
		}
		extID := strconv.Itoa(res.FileID)
		sub := &model.Subtitle{
			MovieID:     movieID,
			Language:    res.Language,
			Source:      "opensubtitles",
			StoragePath: filePath,
			ExternalID:  &extID,
		}
		_ = h.subtitleRepo.Upsert(r.Context(), sub)
	}

	subs, _ := h.subtitleRepo.ListByMovieID(r.Context(), movieID)
	if subs == nil {
		subs = []*model.Subtitle{}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items": subs,
		"found": len(results),
	})
}

func parseMovieID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "movieId"), 10, 64)
}
