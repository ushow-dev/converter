package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"app/worker/internal/config"
	"app/worker/internal/converter"
	"app/worker/internal/db"
	"app/worker/internal/downloader"
	"app/worker/internal/ffmpeg"
	"app/worker/internal/health"
	"app/worker/internal/qbittorrent"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

func main() {
	// ── Structured JSON logging ────────────────────────────────────────────────
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ── Config ─────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if !ffmpeg.Installed() {
		slog.Warn("ffmpeg not found in PATH; convert jobs will fail")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	// ── PostgreSQL ─────────────────────────────────────────────────────────────
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("postgres connected")

	// ── qBittorrent ────────────────────────────────────────────────────────────
	qbt := qbittorrent.New(cfg.QBittorrentBaseURL, cfg.QBittorrentUser, cfg.QBittorrentPass)
	if err := qbt.Login(ctx); err != nil {
		slog.Warn("initial qbittorrent login failed — will retry per-job", "error", err)
	} else {
		slog.Info("qbittorrent authenticated")
	}

	// ── Repositories ───────────────────────────────────────────────────────────
	jobRepo := repository.NewJobRepository(pool)
	assetRepo := repository.NewAssetRepository(pool)

	// ── Pipeline workers ───────────────────────────────────────────────────────
	dlWorker := downloader.New(redisClient, jobRepo, qbt, cfg.MediaRoot)
	cvWorker := converter.New(redisClient, jobRepo, assetRepo)

	// ── Health server ──────────────────────────────────────────────────────────
	go health.Start(cfg.HealthPort, redisClient)

	// ── Run consumers ──────────────────────────────────────────────────────────
	var wg sync.WaitGroup

	// Download worker(s)
	for i := 0; i < cfg.DownloadConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dlWorker.Run(ctx)
		}()
	}

	// Convert worker(s)
	for i := 0; i < cfg.ConvertConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cvWorker.Run(ctx)
		}()
	}

	slog.Info("worker running",
		"download_concurrency", cfg.DownloadConcurrency,
		"convert_concurrency", cfg.ConvertConcurrency,
	)

	wg.Wait()
	slog.Info("worker shutdown complete")
}
