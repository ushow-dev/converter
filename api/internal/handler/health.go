package handler

import (
	"context"
	"net/http"
	"time"
)

// Pinger is any dependency that can be health-checked.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves /health/live and /health/ready.
type HealthHandler struct {
	db    Pinger
	redis Pinger
}

// NewHealthHandler creates a HealthHandler.
func NewHealthHandler(db, redis Pinger) *HealthHandler {
	return &HealthHandler{db: db, redis: redis}
}

// Live handles GET /health/live — always 200, indicates process is up.
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /health/ready — checks downstream dependencies.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	allOK := true

	if err := h.db.Ping(ctx); err != nil {
		checks["postgres"] = "unhealthy: " + err.Error()
		allOK = false
	} else {
		checks["postgres"] = "healthy"
	}

	if err := h.redis.Ping(ctx); err != nil {
		checks["redis"] = "unhealthy: " + err.Error()
		allOK = false
	} else {
		checks["redis"] = "healthy"
	}

	statusCode := http.StatusOK
	status := "ready"
	if !allOK {
		statusCode = http.StatusServiceUnavailable
		status = "not ready"
	}
	respondJSON(w, statusCode, map[string]any{
		"status": status,
		"checks": checks,
	})
}
