package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Pinger can check connectivity.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Start runs a minimal HTTP health server on the given port.
// Returns only when the listener fails; call in a goroutine.
func Start(port string, redis Pinger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		status := "ok"
		code := http.StatusOK
		checks := map[string]string{}

		if err := redis.Ping(ctx); err != nil {
			checks["redis"] = "unhealthy: " + err.Error()
			status = "degraded"
			code = http.StatusServiceUnavailable
		} else {
			checks["redis"] = "healthy"
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"checks": checks,
		})
	})

	srv := &http.Server{
		Addr:        net.JoinHostPort("", port),
		Handler:     mux,
		ReadTimeout: 5 * time.Second,
	}
	slog.Info("health server starting", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("health server error", "error", err)
	}
}
