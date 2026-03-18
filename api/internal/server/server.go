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
	Cfg             *config.Config
	HealthHandler   *handler.HealthHandler
	AuthHandler     *handler.AuthHandler
	SearchHandler   *handler.SearchHandler
	JobsHandler     *handler.JobsHandler
	MoviesHandler   *handler.MoviesHandler
	PlayerHandler   *handler.PlayerHandler
	SubtitleHandler *handler.SubtitleHandler
	BrowseHandler   *handler.BrowseHandler
	IngestHandler   *handler.IngestHandler
}

// New builds the chi router with all routes and middleware registered.
func New(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(chimiddleware.RealIP)
	// Note: no global RequestSize limit — the upload handler sets its own via
	// http.MaxBytesReader; all other handlers are naturally small JSON payloads.
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
			r.Post("/jobs/upload", deps.JobsHandler.Upload)
			r.Get("/jobs", deps.JobsHandler.List)
			r.Get("/jobs/{jobID}", deps.JobsHandler.Get)
			r.Delete("/jobs/{jobID}", deps.JobsHandler.Delete)
			r.Get("/movies", deps.MoviesHandler.List)
			r.Patch("/movies/{movieId}", deps.MoviesHandler.UpdateIDs)
			r.Get("/movies/tmdb/{tmdbId}", deps.MoviesHandler.TMDBLookup)
			r.Get("/movies/{movieId}/subtitles", deps.SubtitleHandler.List)
			r.Post("/movies/{movieId}/subtitles", deps.SubtitleHandler.Upload)
			r.Post("/movies/{movieId}/subtitles/search", deps.SubtitleHandler.Search)
			r.Post("/remote-browse", deps.BrowseHandler.Browse)
			r.Post("/jobs/remote-download", deps.JobsHandler.RemoteDownload)
			r.Get("/movies/tmdb/search", deps.MoviesHandler.TMDBSearch)
		})

		// Thumbnail: JWT via header or ?token= query param (for <img src>)
		r.Group(func(r chi.Router) {
			r.Use(auth.JWTQueryOrHeaderMiddleware(deps.Cfg.JWTSecret))
			r.Get("/jobs/{jobID}/thumbnail", deps.JobsHandler.Thumbnail)
			r.Get("/movies/{movieId}/thumbnail", deps.MoviesHandler.Thumbnail)
		})
	})

	// ── Player API ────────────────────────────────────────────────────────────
	r.Route("/api/player", func(r chi.Router) {
		r.Use(auth.PlayerKeyMiddleware(deps.Cfg.PlayerAPIKey))
		r.Get("/movie", deps.PlayerHandler.GetMovie)
		r.Get("/assets/{assetID}", deps.PlayerHandler.GetAsset)
		r.Get("/jobs/{jobID}/status", deps.PlayerHandler.GetJobStatus)
	})

	// ── Ingest API (service-to-service) ──────────────────────────────────────
	r.Route("/api/ingest", func(r chi.Router) {
		r.Use(auth.ServiceTokenMiddleware(deps.Cfg.IngestServiceToken))
		r.Post("/incoming/register", deps.IngestHandler.Register)
		r.Post("/incoming/claim",    deps.IngestHandler.Claim)
		r.Post("/incoming/progress", deps.IngestHandler.Progress)
		r.Post("/incoming/fail",     deps.IngestHandler.Fail)
		r.Post("/incoming/complete", deps.IngestHandler.Complete)
	})

	return r
}

// Start runs the HTTP server and blocks until ctx is cancelled.
func Start(ctx context.Context, cfg *config.Config, handler http.Handler) error {
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second, // header only; body has no deadline (needed for large uploads)
		WriteTimeout:      0,                // no write deadline — upload response only sent after full receive
		IdleTimeout:       60 * time.Second,
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Correlation-Id, X-Player-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
