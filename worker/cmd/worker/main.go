package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"app/worker/internal/cancelregistry"
	"app/worker/internal/config"
	"app/worker/internal/converter"
	"app/worker/internal/recovery"
	"app/worker/internal/db"
	"app/worker/internal/downloader"
	"app/worker/internal/ffmpeg"
	"app/worker/internal/health"
	"app/worker/internal/httpdownloader"
	"app/worker/internal/ingest"
	"app/worker/internal/paths"
	"app/worker/internal/qbittorrent"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
	"app/worker/internal/subtitles"
	"app/worker/internal/transfer"
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

	// ── Media directories ───────────────────────────────────────────────────────
	// Worker runs as root; ensure subdirs exist and are world-writable so
	// qBittorrent (uid=1000) can write downloads without permission errors.
	// chmod is applied recursively so existing job subdirs are also fixed.
	for _, dir := range []string{
		cfg.MediaRoot + "/downloads",
		cfg.MediaRoot + "/converted",
		cfg.MediaRoot + "/temp",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			slog.Warn("could not create media dir", "dir", dir, "error", err)
			continue
		}
		_ = chmodR(dir, 0o755)
	}
	slog.Info("media dirs ready", "root", cfg.MediaRoot)

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
	movieRepo := repository.NewMovieRepository(pool)
	subtitleRepo := repository.NewSubtitleRepository(pool)
	seriesRepo := repository.NewSeriesRepository(pool)
	audioTrackRepo := repository.NewAudioTrackRepository(pool)
	storageLocRepo := repository.NewStorageLocationRepository(pool)

	// ── Subtitle fetcher (optional) ────────────────────────────────────────────
	var subtitleFetcher *subtitles.Fetcher
	if cfg.OpenSubtitlesAPIKey != "" {
		subtitleFetcher = subtitles.NewFetcher(cfg.OpenSubtitlesAPIKey, cfg.SubtitleLanguages)
		slog.Info("subtitle fetcher enabled", "languages", cfg.SubtitleLanguages)
	} else {
		slog.Info("subtitle fetcher disabled (OPENSUBTITLES_API_KEY not set)")
	}

	// ── Cancel registry ────────────────────────────────────────────────────────
	registry := cancelregistry.New()

	// ── Pipeline workers ───────────────────────────────────────────────────────
	dlWorker := downloader.New(redisClient, jobRepo, qbt, cfg.MediaRoot)

	// Scanner client for archive-to-scanner: reuse ingest credentials.
	// Archive is enabled when INGEST_SERVICE_TOKEN, INGEST_SOURCE_REMOTE, and
	// INGEST_SOURCE_BASE_PATH are all set.
	var scannerClientForArchive *ingest.Client
	if cfg.IngestServiceToken != "" && cfg.ScannerAPIURL != "" && cfg.IngestSourceRemote != "" {
		scannerClientForArchive = ingest.NewClient(cfg.ScannerAPIURL, cfg.IngestServiceToken)
		slog.Info("archive-to-scanner enabled",
			"remote", cfg.IngestSourceRemote, "base", cfg.IngestSourceBasePath)
	} else {
		slog.Info("archive-to-scanner disabled (INGEST_SERVICE_TOKEN/INGEST_SOURCE_REMOTE not set)")
	}

	pathResolver := paths.New(cfg.MediaRoot)
	cvWorker := converter.New(redisClient, jobRepo, assetRepo, movieRepo,
		subtitleFetcher, subtitleRepo, seriesRepo, audioTrackRepo,
		cfg.MediaRoot, cfg.TMDBAPIKey, cfg.FFmpegThreads,
		cfg.RcloneRemote != "", scannerClientForArchive,
		cfg.IngestSourceRemote, cfg.ArchiveDestPath, registry, pathResolver)
	httpDlWorker := httpdownloader.New(redisClient, jobRepo, cfg.MediaRoot, registry)

	// Transfer worker (optional: only when RCLONE_REMOTE is set)
	var trWorker *transfer.Worker
	if cfg.RcloneRemote != "" {
		remoteStorageLocID, err := storageLocRepo.GetActiveRemoteID(ctx)
		if err != nil {
			slog.Warn("transfer worker disabled: no active remote storage location found", "error", err)
		} else {
			trWorker = transfer.New(redisClient, movieRepo, jobRepo,
				cfg.RcloneRemote, cfg.RcloneRemotePath, remoteStorageLocID)
			slog.Info("transfer worker enabled", "remote", cfg.RcloneRemote, "storage_location_id", remoteStorageLocID)
		}
	} else {
		slog.Info("transfer worker disabled (RCLONE_REMOTE not set)")
	}

	// ── Recover stale jobs from previous run ──────────────────────────────────
	recovery.RecoverStaleLocks(ctx, pool, redisClient)
	recovery.Run(ctx, pool, redisClient)

	// ── Health server ──────────────────────────────────────────────────────────
	go health.Start(cfg.HealthPort, redisClient)

	// ── Run consumers ──────────────────────────────────────────────────────────
	var wg sync.WaitGroup

	// Cancel watcher: reads job IDs from cancel_queue and aborts in-flight jobs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("cancel watcher started")
		for {
			if ctx.Err() != nil {
				slog.Info("cancel watcher stopped")
				return
			}
			raw, err := redisClient.Pop(ctx, queue.CancelQueue, 5*time.Second)
			if errors.Is(err, queue.ErrEmpty) {
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("cancel queue pop error", "error", err)
				time.Sleep(time.Second)
				continue
			}
			var jobID string
			if err := json.Unmarshal(raw, &jobID); err != nil {
				slog.Error("unmarshal cancel message", "error", err, "raw", string(raw))
				continue
			}
			slog.Info("cancelling job", "job_id", jobID)
			registry.Cancel(jobID)
		}
	}()

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

	// HTTP download worker(s)
	for i := 0; i < cfg.HTTPDownloadConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			httpDlWorker.Run(ctx)
		}()
	}

	if trWorker != nil {
		for i := 0; i < cfg.TransferConcurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				trWorker.Run(ctx)
			}()
		}
	}

	// Ingest worker (optional: only when INGEST_SERVICE_TOKEN and INGEST_SOURCE_REMOTE are set)
	if cfg.IngestServiceToken != "" && cfg.IngestSourceRemote != "" {
		ingestClient := ingest.NewClient(cfg.ScannerAPIURL, cfg.IngestServiceToken)
		ingestPuller := ingest.NewPuller(cfg.IngestSourceRemote, cfg.IngestSourceBasePath)
		ingestWkr := ingest.New(ingestClient, ingestPuller, jobRepo, seriesRepo, redisClient, cfg.MediaRoot, cfg.IngestClaimTTLSec)
		for i := 0; i < cfg.IngestConcurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ingestWkr.Run(ctx)
			}()
		}
		slog.Info("ingest worker enabled", "concurrency", cfg.IngestConcurrency)
	} else {
		slog.Info("ingest worker disabled (INGEST_SERVICE_TOKEN or INGEST_SOURCE_REMOTE not set)")
	}

	slog.Info("worker running",
		"download_concurrency", cfg.DownloadConcurrency,
		"convert_concurrency", cfg.ConvertConcurrency,
		"http_download_concurrency", cfg.HTTPDownloadConcurrency,
	)

	wg.Wait()
	slog.Info("worker shutdown complete")
}

// chmodR recursively sets permissions on dir and all its contents.
func chmodR(dir string, mode fs.FileMode) error {
	return filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		_ = os.Chmod(path, mode)
		return nil
	})
}
