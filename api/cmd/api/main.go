package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"app/api/internal/config"
	"app/api/internal/db"
	"app/api/internal/handler"
	"app/api/internal/indexer"
	"app/api/internal/queue"
	"app/api/internal/repository"
	"app/api/internal/server"
	"app/api/internal/service"
	"app/api/internal/subtitles"
)

func main() {
	// ── Structured JSON logging ────────────────────────────────────────────────
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ── Configuration ──────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── PostgreSQL ─────────────────────────────────────────────────────────────
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("postgres connected")

	// ── Redis ──────────────────────────────────────────────────────────────────
	redisClient, err := queue.New(cfg.RedisURL)
	if err != nil {
		slog.Error("connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	if err := redisClient.Ping(ctx); err != nil {
		slog.Error("ping redis", "error", err)
		os.Exit(1)
	}
	slog.Info("redis connected")

	// ── Repositories ───────────────────────────────────────────────────────────
	jobRepo := repository.NewJobRepository(pool)
	assetRepo := repository.NewAssetRepository(pool)
	movieRepo := repository.NewMovieRepository(pool)
	searchRepo := repository.NewSearchRepository(pool)
	subtitleRepo := repository.NewSubtitleRepository(pool)
	storageLocRepo := repository.NewStorageLocationRepository(pool)

	// ── Indexer backend ────────────────────────────────────────────────────────
	prowlarr := indexer.NewProwlarrClient(cfg.ProwlarrBaseURL, cfg.ProwlarrAPIKey)

	// ── Services ───────────────────────────────────────────────────────────────
	searchSvc := service.NewSearchService(prowlarr, searchRepo)
	jobSvc := service.NewJobService(jobRepo, movieRepo, redisClient, cfg.MediaRoot, cfg.TMDBAPIKey)

	// ── OpenSubtitles client (optional) ────────────────────────────────────────
	osClient := subtitles.NewClient(cfg.OpenSubtitlesAPIKey)

	// ── Handlers ───────────────────────────────────────────────────────────────
	// pool and redisClient both satisfy handler.Pinger via their Ping methods.
	healthH := handler.NewHealthHandler(pool, redisClient)
	authH := handler.NewAuthHandler(cfg)
	searchH := handler.NewSearchHandler(searchSvc)
	jobsH := handler.NewJobsHandler(jobSvc, assetRepo)
	moviesH := handler.NewMoviesHandler(cfg.TMDBAPIKey, movieRepo)
	playerH := handler.NewPlayerHandler(
		jobSvc,
		assetRepo,
		movieRepo,
		subtitleRepo,
		storageLocRepo,
		cfg.MediaBaseURL,
		cfg.MediaSigningKey,
		cfg.MediaSigningTTL,
	)
	subtitleH := handler.NewSubtitleHandler(movieRepo, subtitleRepo, osClient, cfg.MediaRoot, cfg.SubtitleLanguages)
	browseH := handler.NewBrowseHandler()
	scannerH := handler.NewScannerHandler(cfg.ScannerAPIURL, cfg.IngestServiceToken)

	// ── HTTP server ────────────────────────────────────────────────────────────
	h := server.New(server.Dependencies{
		Cfg:             cfg,
		HealthHandler:   healthH,
		AuthHandler:     authH,
		SearchHandler:   searchH,
		JobsHandler:     jobsH,
		MoviesHandler:   moviesH,
		PlayerHandler:   playerH,
		SubtitleHandler: subtitleH,
		BrowseHandler:   browseH,
		ScannerHandler:  scannerH,
	})

	if err := server.Start(ctx, cfg, h); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
