package handler

import (
	"encoding/json"
	"net/http"
)

// respondJSON writes v as JSON with the given status code.
func respondJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}

// respondError writes a standard error envelope.
func respondError(w http.ResponseWriter, statusCode int, code, message string, retryable bool, correlationID string) {
	respondJSON(w, statusCode, map[string]any{
		"error": map[string]any{
			"code":           code,
			"message":        message,
			"retryable":      retryable,
			"correlation_id": correlationID,
		},
	})
}
