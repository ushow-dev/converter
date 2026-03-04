package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"app/api/internal/auth"
	"app/api/internal/config"
	"app/api/internal/handler"
)

// Dependencies bundles all wired-up components needed to build the router.
type Dependencies struct {
	Cfg           *config.Config
	HealthHandler *handler.HealthHandler
	AuthHandler   *handler.AuthHandler
	SearchHandler *handler.SearchHandler
	JobsHandler   *handler.JobsHandler
	PlayerHandler *handler.PlayerHandler
}

// New builds the chi router with all routes and middleware registered.
func New(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.RequestSize(4 * 1024 * 1024)) // 4 MB max body
	r.Use(auth.CorrelationIDMiddleware)
	r.Use(requestLogger)
	r.Use(chimiddleware.Recoverer)
	r.Use(corsMiddleware)

	// ── Health endpoints (no auth) ─────────────────────────────────────────────
	r.Get("/health/live", deps.HealthHandler.Live)
	r.Get("/health/ready", deps.HealthHandler.Ready)

	// ── Admin API ─────────────────────────────────────────────────────────────
	r.Route("/api/admin", func(r chi.Router) {
		// Public: login
		r.Post("/auth/login", deps.AuthHandler.Login)

		// Protected: require valid JWT
		r.Group(func(r chi.Router) {
			r.Use(auth.JWTMiddleware(deps.Cfg.JWTSecret))
			r.Get("/search", deps.SearchHandler.Search)
			r.Post("/jobs", deps.JobsHandler.Create)
			r.Get("/jobs", deps.JobsHandler.List)
			r.Get("/jobs/{jobID}", deps.JobsHandler.Get)
			r.Delete("/jobs/{jobID}", deps.JobsHandler.Delete)
		})

		// Thumbnail: JWT via header or ?token= query param (for <img src>)
		r.Group(func(r chi.Router) {
			r.Use(auth.JWTQueryOrHeaderMiddleware(deps.Cfg.JWTSecret))
			r.Get("/jobs/{jobID}/thumbnail", deps.JobsHandler.Thumbnail)
		})
	})

	// ── Player API ────────────────────────────────────────────────────────────
	r.Route("/api/player", func(r chi.Router) {
		r.Use(auth.PlayerKeyMiddleware(deps.Cfg.PlayerAPIKey))
		r.Get("/movie", deps.PlayerHandler.GetMovie)
		r.Get("/assets/{assetID}", deps.PlayerHandler.GetAsset)
		r.Get("/jobs/{jobID}/status", deps.PlayerHandler.GetJobStatus)
	})

	return r
}

// Start runs the HTTP server and blocks until ctx is cancelled.
func Start(ctx context.Context, cfg *config.Config, handler http.Handler) error {
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.Info("api server shutting down")
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}
}

// ─── middleware helpers ───────────────────────────────────────────────────────

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"correlation_id", auth.GetCorrelationID(r.Context()),
		)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Correlation-Id, X-Player-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
