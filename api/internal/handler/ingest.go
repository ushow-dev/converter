package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"app/api/internal/model"
	"app/api/internal/repository"
	"app/api/internal/service"
)

// IngestHandler handles /api/ingest/incoming/* endpoints.
type IngestHandler struct {
	svc         *service.IngestService
	maxAttempts int
}

// NewIngestHandler creates an IngestHandler.
func NewIngestHandler(svc *service.IngestService, maxAttempts int) *IngestHandler {
	return &IngestHandler{svc: svc, maxAttempts: maxAttempts}
}

// Register handles POST /api/ingest/incoming/register.
func (h *IngestHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req model.RegisterIncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	item, err := h.svc.Register(r.Context(), &req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, item)
}

// Claim handles POST /api/ingest/incoming/claim.
func (h *IngestHandler) Claim(w http.ResponseWriter, r *http.Request) {
	var req model.ClaimIncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	items, err := h.svc.Claim(r.Context(), req.Limit, req.ClaimTTLSec)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if items == nil {
		items = []model.IncomingItem{}
	}

	respondJSON(w, http.StatusOK, model.ClaimIncomingResponse{Items: items})
}

// Progress handles POST /api/ingest/incoming/progress.
func (h *IngestHandler) Progress(w http.ResponseWriter, r *http.Request) {
	var req model.ProgressIncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := h.svc.Progress(r.Context(), &req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Fail handles POST /api/ingest/incoming/fail.
func (h *IngestHandler) Fail(w http.ResponseWriter, r *http.Request) {
	var req model.FailIncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := h.svc.Fail(r.Context(), &req, h.maxAttempts); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Complete handles POST /api/ingest/incoming/complete.
func (h *IngestHandler) Complete(w http.ResponseWriter, r *http.Request) {
	var req model.CompleteIncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	resp, err := h.svc.Complete(r.Context(), &req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, resp)
}

// GetByID handles GET /api/ingest/incoming/{id}.
func (h *IngestHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	item, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, item)
}
