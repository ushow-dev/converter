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

// respondIfDuplicate checks if err is a *service.DuplicateError and, if so,
// writes a 409 response with movie_id and title, then returns true.
func respondIfDuplicate(w http.ResponseWriter, err error, correlationID string) bool {
	var dupErr *service.DuplicateError
	if !errors.As(err, &dupErr) {
		return false
	}
	respondJSON(w, http.StatusConflict, map[string]any{
		"error": map[string]any{
			"code":           "DUPLICATE",
			"message":        "movie already exists",
			"retryable":      false,
			"correlation_id": correlationID,
			"movie_id":       dupErr.MovieID,
			"title":          dupErr.Title,
		},
	})
	return true
}

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
	IMDbID      string `json:"imdb_id"`
	TMDBID      string `json:"tmdb_id"`
	Title       string `json:"title"`
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
		IMDbID:        req.IMDbID,
		TMDBID:        req.TMDBID,
		Title:         req.Title,
		Priority:      priority,
		CorrelationID: cid,
	})
	if err != nil {
		if respondIfDuplicate(w, err, cid) {
			return
		}
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

// Upload handles POST /api/admin/jobs/upload (multipart form).
// Accepts a video file plus title/imdb_id/tmdb_id fields, saves the file to
// the downloads directory, and enqueues it directly for conversion.
func (h *JobsHandler) Upload(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	// Override the global body size limit for file uploads (up to 50 GB).
	r.Body = http.MaxBytesReader(w, r.Body, 50<<30)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"failed to parse multipart form", false, cid)
		return
	}

	title := r.FormValue("title")
	if title == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"title is required", false, cid)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"file is required", false, cid)
		return
	}
	defer file.Close()

	job, err := h.svc.CreateUploadJob(r.Context(), service.CreateUploadJobRequest{
		RequestID:     r.FormValue("request_id"),
		Title:         title,
		IMDbID:        r.FormValue("imdb_id"),
		TMDBID:        r.FormValue("tmdb_id"),
		CorrelationID: cid,
	}, file, header.Filename)
	if err != nil {
		if respondIfDuplicate(w, err, cid) {
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to create upload job", false, cid)
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
	if jobs == nil {
		jobs = []*model.Job{}
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

// RemoteDownload handles POST /api/admin/jobs/remote-download.
// Accepts a JSON body with the URL of a remote video file, parses the title/year
// from the filename, optionally searches TMDB, creates the job, and enqueues it
// for the HTTP download worker.
func (h *JobsHandler) RemoteDownload(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var req struct {
		URL         string             `json:"url"`
		Filename    string             `json:"filename"`
		ProxyConfig *model.ProxyConfig `json:"proxy_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", false, cid)
		return
	}
	if req.URL == "" || req.Filename == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "url and filename are required", false, cid)
		return
	}

	job, tmdbID, err := h.svc.CreateRemoteDownloadJob(r.Context(), service.CreateRemoteDownloadJobRequest{
		SourceURL:     req.URL,
		Filename:      req.Filename,
		CorrelationID: cid,
		ProxyConfig:   req.ProxyConfig,
	})
	if err != nil {
		if respondIfDuplicate(w, err, cid) {
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create remote download job", false, cid)
		return
	}

	title := ""
	if job.Title != nil {
		title = *job.Title
	}
	// job.JobID may be empty when the download was forwarded to the scanner API
	// (no converter job was created). The frontend handles empty job_id gracefully.
	respondJSON(w, http.StatusAccepted, map[string]any{
		"job_id":     job.JobID,
		"status":     string(job.Status),
		"title":      title,
		"tmdb_id":    tmdbID,
		"created_at": job.CreatedAt,
	})
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
