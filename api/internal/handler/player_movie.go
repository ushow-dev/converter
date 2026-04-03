package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"log/slog"

	"app/api/internal/auth"
	"app/api/internal/repository"
)

// GetAsset handles GET /api/player/assets/{assetID}.
func (h *PlayerHandler) GetAsset(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	assetID := chi.URLParam(r, "assetID")

	asset, err := h.assetRepo.GetByID(r.Context(), assetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND",
				"asset not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch asset", false, cid)
		return
	}

	// Build playback info from actual storage_path to avoid divergence between
	// physical storage layout and API URL shape.
	playbackURL := h.maybeSignMediaURL(storagePathToPlaybackURL(asset.StoragePath))

	respondJSON(w, http.StatusOK, map[string]any{
		"asset_id":     asset.AssetID,
		"job_id":       asset.JobID,
		"content_type": "movie",
		"is_ready":     asset.IsReady,
		"playback": map[string]any{
			"mode": "url",
			"url":  playbackURL,
		},
		"media_info": map[string]any{
			"duration_sec": asset.DurationSec,
			"video_codec":  asset.VideoCodec,
			"audio_codec":  asset.AudioCodec,
		},
		"updated_at": asset.UpdatedAt,
	})
}

// GetMovie handles GET /api/player/movie?imdb_id=...|tmdb_id=...
func (h *PlayerHandler) GetMovie(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	imdbID := strings.TrimSpace(r.URL.Query().Get("imdb_id"))
	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	if (imdbID == "" && tmdbID == "") || (imdbID != "" && tmdbID != "") {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"exactly one of imdb_id or tmdb_id must be provided", false, cid)
		return
	}

	var (
		movie *repositoryMovieView
		err   error
	)
	if imdbID != "" {
		movie, err = h.getMovieByIMDbID(r, imdbID)
	} else {
		movie, err = h.getMovieByTMDBID(r, tmdbID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "movie not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch movie", false, cid)
		return
	}

	baseURL := h.resolveBaseURL(r.Context(), movie.storageLocationID)

	// Build subtitle list.
	subtitleTracks := []map[string]string{}
	if subs, err := h.subtitleRepo.ListByMovieID(r.Context(), movie.id); err == nil {
		for _, sub := range subs {
			subtitleTracks = append(subtitleTracks, map[string]string{
				"language": sub.Language,
				"url":      h.maybeSignMediaURL(buildMovieMediaURL(baseURL, movie.storageKey, "subtitles/"+sub.Language+".vtt")),
			})
		}
	}

	// Build audio track list.
	var audioTracks []map[string]any
	if asset, err := h.assetRepo.GetByMovieID(r.Context(), movie.id); err == nil {
		audioTracks = h.buildAudioTracksPayload(r.Context(), asset.AssetID, "movie")
	}
	if audioTracks == nil {
		audioTracks = []map[string]any{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"movie": map[string]any{
				"id":      movie.id,
				"imdb_id": movie.imdbID,
				"tmdb_id": movie.tmdbID,
			},
			"playback": map[string]any{
				"hls": h.maybeSignMediaURL(buildMovieMediaURL(baseURL, movie.storageKey, "master.m3u8")),
			},
			"assets": map[string]any{
				"poster": h.maybeSignMediaURL(buildMovieMediaURL(baseURL, movie.storageKey, "thumbnail.jpg")),
			},
			"subtitles":    subtitleTracks,
			"audio_tracks": audioTracks,
		},
		"meta": map[string]any{
			"version": "v1",
		},
	})
}

// GetCatalog handles GET /api/player/catalog?since=...
// Returns both movies (type: "movie") and series (type: "tv").
func (h *PlayerHandler) GetCatalog(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var since *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"invalid since parameter: expected RFC 3339 format", false, cid)
			return
		}
		since = &t
	}

	// Fetch movies.
	movieIDs, err := h.movieRepo.ListReadyTMDBIDs(r.Context(), since)
	if err != nil {
		slog.Error("catalog movie query failed", "error", err, "correlation_id", cid)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch catalog", false, cid)
		return
	}

	// Fetch series.
	seriesIDs, err := h.seriesRepo.ListReadyTMDBIDs(r.Context(), since)
	if err != nil {
		slog.Error("catalog series query failed", "error", err, "correlation_id", cid)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch catalog", false, cid)
		return
	}

	items := make([]map[string]string, 0, len(movieIDs)+len(seriesIDs))
	for _, id := range movieIDs {
		items = append(items, map[string]string{"tmdb_id": id, "type": "movie"})
	}
	for _, id := range seriesIDs {
		items = append(items, map[string]string{"tmdb_id": id, "type": "tv"})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

// GetJobStatus handles GET /api/player/jobs/{jobID}/status.
func (h *PlayerHandler) GetJobStatus(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	jobID := chi.URLParam(r, "jobID")

	job, err := h.jobSvc.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND",
				"job not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch job", false, cid)
		return
	}

	isReady := job.Status == "completed"
	var assetID *string
	if isReady {
		if asset, err := h.assetRepo.GetByJobID(r.Context(), jobID); err == nil {
			assetID = &asset.AssetID
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"job_id":     job.JobID,
		"status":     string(job.Status),
		"is_ready":   isReady,
		"asset_id":   assetID,
		"updated_at": job.UpdatedAt,
	})
}

type repositoryMovieView struct {
	id                int64
	storageKey        string
	imdbID            *string
	tmdbID            *string
	storageLocationID *int64
}

func (h *PlayerHandler) getMovieByIMDbID(r *http.Request, imdbID string) (*repositoryMovieView, error) {
	m, err := h.movieRepo.GetByIMDbID(r.Context(), imdbID)
	if err != nil {
		return nil, err
	}
	return &repositoryMovieView{id: m.ID, storageKey: m.StorageKey, imdbID: m.IMDbID, tmdbID: m.TMDBID, storageLocationID: m.StorageLocationID}, nil
}

func (h *PlayerHandler) getMovieByTMDBID(r *http.Request, tmdbID string) (*repositoryMovieView, error) {
	m, err := h.movieRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		return nil, err
	}
	return &repositoryMovieView{id: m.ID, storageKey: m.StorageKey, imdbID: m.IMDbID, tmdbID: m.TMDBID, storageLocationID: m.StorageLocationID}, nil
}

func buildMovieMediaURL(baseURL, storageKey, fileName string) string {
	return buildMediaURL(baseURL, fmt.Sprintf("movies/%s/%s", storageKey, fileName))
}
