package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
)

// ScannerHandler proxies scanner download endpoints to the scanner API.
type ScannerHandler struct {
	scannerAPIURL string
	serviceToken  string
}

// NewScannerHandler creates a ScannerHandler.
func NewScannerHandler(scannerAPIURL, serviceToken string) *ScannerHandler {
	return &ScannerHandler{
		scannerAPIURL: scannerAPIURL,
		serviceToken:  serviceToken,
	}
}

// ListDownloads proxies GET /api/admin/scanner/downloads → scanner GET /api/v1/downloads.
func (h *ScannerHandler) ListDownloads(w http.ResponseWriter, r *http.Request) {
	correlationID := auth.GetCorrelationID(r.Context())
	if h.scannerAPIURL == "" {
		respondJSON(w, http.StatusServiceUnavailable, errBody("SCANNER_UNAVAILABLE", "scanner API not configured", correlationID))
		return
	}
	body, status, err := h.proxyGet(r.Context(), "/api/v1/downloads")
	if err != nil {
		respondJSON(w, http.StatusBadGateway, errBody("SCANNER_ERROR", err.Error(), correlationID))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body) //nolint:errcheck
}

// RetryDownload proxies POST /api/admin/scanner/downloads/{id}/retry → scanner POST /api/v1/downloads/{id}/retry.
func (h *ScannerHandler) RetryDownload(w http.ResponseWriter, r *http.Request) {
	correlationID := auth.GetCorrelationID(r.Context())
	if h.scannerAPIURL == "" {
		respondJSON(w, http.StatusServiceUnavailable, errBody("SCANNER_UNAVAILABLE", "scanner API not configured", correlationID))
		return
	}
	idStr := chi.URLParam(r, "downloadID")
	if _, err := strconv.Atoi(idStr); err != nil {
		respondJSON(w, http.StatusBadRequest, errBody("INVALID_ID", "invalid download id", correlationID))
		return
	}
	status, err := h.proxyPost(r.Context(), fmt.Sprintf("/api/v1/downloads/%s/retry", idStr), nil)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, errBody("SCANNER_ERROR", err.Error(), correlationID))
		return
	}
	w.WriteHeader(status)
}

func (h *ScannerHandler) proxyGet(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.scannerAPIURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Service-Token", h.serviceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func (h *ScannerHandler) proxyPost(ctx context.Context, path string, payload []byte) (int, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.scannerAPIURL+path, bodyReader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Service-Token", h.serviceToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func errBody(code, msg, correlationID string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":           code,
			"message":        msg,
			"retryable":      false,
			"correlation_id": correlationID,
		},
	}
}
