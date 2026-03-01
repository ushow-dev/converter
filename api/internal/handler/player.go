package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/repository"
	"app/api/internal/service"
)

// PlayerHandler handles /api/player/* endpoints.
type PlayerHandler struct {
	jobSvc   *service.JobService
	assetRepo *repository.AssetRepository
}

// NewPlayerHandler creates a PlayerHandler.
func NewPlayerHandler(jobSvc *service.JobService, assetRepo *repository.AssetRepository) *PlayerHandler {
	return &PlayerHandler{jobSvc: jobSvc, assetRepo: assetRepo}
}

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

	// Build playback info (URL mode: serve via nginx or direct file path).
	playbackURL := fmt.Sprintf("/media/converted/%s", asset.AssetID)

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
