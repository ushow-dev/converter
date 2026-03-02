package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/model"
	"app/api/internal/repository"
	"app/api/internal/service"
)

// JobsHandler handles /api/admin/jobs endpoints.
type JobsHandler struct {
	svc       *service.JobService
	assetRepo *repository.AssetRepository
}

// NewJobsHandler creates a JobsHandler.
func NewJobsHandler(svc *service.JobService, assetRepo *repository.AssetRepository) *JobsHandler {
	return &JobsHandler{svc: svc, assetRepo: assetRepo}
}

type createJobRequest struct {
	RequestID   string `json:"request_id"`
	ContentType string `json:"content_type"`
	SourceType  string `json:"source_type"`
	SourceRef   string `json:"source_ref"`
	Priority    string `json:"priority"`
}

// Create handles POST /api/admin/jobs.
func (h *JobsHandler) Create(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"invalid JSON body", false, cid)
		return
	}

	if req.ContentType == "" || req.SourceType == "" || req.SourceRef == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"content_type, source_type, source_ref are required", false, cid)
		return
	}
	if req.ContentType != "movie" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"content_type must be 'movie'", false, cid)
		return
	}
	if req.SourceType != "torrent" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"source_type must be 'torrent'", false, cid)
		return
	}

	priority := model.JobPriority(req.Priority)
	if priority == "" {
		priority = model.JobPriorityNormal
	}

	job, err := h.svc.CreateJob(r.Context(), service.CreateJobRequest{
		RequestID:     req.RequestID,
		ContentType:   req.ContentType,
		SourceType:    req.SourceType,
		SourceRef:     req.SourceRef,
		Priority:      priority,
		CorrelationID: cid,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to create job", false, cid)
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]any{
		"job_id":     job.JobID,
		"status":     string(job.Status),
		"created_at": job.CreatedAt,
	})
}

// Get handles GET /api/admin/jobs/{jobID}.
func (h *JobsHandler) Get(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	jobID := chi.URLParam(r, "jobID")

	job, err := h.svc.GetJob(r.Context(), jobID)
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

	respondJSON(w, http.StatusOK, job)
}

// List handles GET /api/admin/jobs.
func (h *JobsHandler) List(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	q := r.URL.Query()

	status := q.Get("status")
	cursor := q.Get("cursor")
	limit := 50
	if ls := q.Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil {
			limit = v
		}
	}

	jobs, nextCursor, err := h.svc.ListJobs(r.Context(), status, limit, cursor)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to list jobs", false, cid)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":          jobs,
		"next_cursor":    nextCursor,
		"correlation_id": cid,
	})
}

// Delete handles DELETE /api/admin/jobs/{jobID}.
func (h *JobsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	jobID := chi.URLParam(r, "jobID")

	if err := h.svc.DeleteJob(r.Context(), jobID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "job not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete job", false, cid)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Thumbnail handles GET /api/admin/jobs/{jobID}/thumbnail.
// Serves the JPEG thumbnail for a completed job. Accepts JWT via Authorization
// header or ?token= query parameter (needed for use in <img src>).
func (h *JobsHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	jobID := chi.URLParam(r, "jobID")

	asset, err := h.assetRepo.GetByJobID(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND",
				"no asset for this job", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch asset", false, cid)
		return
	}
	if asset.ThumbnailPath == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND",
			"no thumbnail for this job", false, cid)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, *asset.ThumbnailPath)
}
