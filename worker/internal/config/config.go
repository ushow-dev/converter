package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration for the worker service.
type Config struct {
	// Redis
	RedisURL string

	// PostgreSQL
	DatabaseURL string

	// qBittorrent web UI
	QBittorrentBaseURL string
	QBittorrentUser    string
	QBittorrentPass    string

	// Media filesystem root (shared volume mounted at /media)
	MediaRoot string

	// Worker concurrency
	DownloadConcurrency     int
	ConvertConcurrency      int
	HTTPDownloadConcurrency int

	// FFmpeg thread limit per job (0 = auto/all cores).
	// Set to cpu_count/ConvertConcurrency when running parallel conversions,
	// e.g. FFMPEG_THREADS=8 with CONVERT_CONCURRENCY=2 on a 16-core host.
	FFmpegThreads int

	// Internal health server port
	HealthPort string

	// TMDB API key for backdrop download (optional)
	TMDBAPIKey string

	// OpenSubtitles.com API key (optional; subtitle search disabled if empty)
	OpenSubtitlesAPIKey string

	// Languages to fetch subtitles for, e.g. ["ru","en"]
	SubtitleLanguages []string

	// rclone transfer settings
	RcloneRemote       string // name of rclone remote, e.g. "myremote" — empty disables transfer
	RcloneRemotePath   string // base path on remote, e.g. "/storage"
	TransferConcurrency int

	// Ingest worker settings
	ConverterAPIURL      string
	IngestServiceToken   string
	IngestConcurrency    int
	IngestClaimTTLSec    int
	IngestMaxAttempts    int
	IngestSourceRemote   string
	IngestSourceBasePath string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		RedisURL:            mustEnv("REDIS_URL"),
		DatabaseURL:         mustEnv("DATABASE_URL"),
		QBittorrentBaseURL:  getEnv("QBITTORRENT_BASE_URL", "http://qbittorrent:8080"),
		QBittorrentUser:     getEnv("QBITTORRENT_USER", "admin"),
		QBittorrentPass:     getEnv("QBITTORRENT_PASSWORD", "adminadmin"),
		MediaRoot:           getEnv("MEDIA_ROOT", "/media"),
		HealthPort:          getEnv("WORKER_HEALTH_PORT", "8001"),
		DownloadConcurrency:     intEnv("DOWNLOAD_CONCURRENCY", 2),
		ConvertConcurrency:      intEnv("CONVERT_CONCURRENCY", 1),
		HTTPDownloadConcurrency: intEnv("HTTP_DOWNLOAD_CONCURRENCY", 3),
		FFmpegThreads:           intEnv("FFMPEG_THREADS", 0),
		TMDBAPIKey:          getEnv("TMDB_API_KEY", ""),
		OpenSubtitlesAPIKey: getEnv("OPENSUBTITLES_API_KEY", ""),
		SubtitleLanguages:   parseCSV(getEnv("SUBTITLE_LANGUAGES", "en,bn,hi")),
		RcloneRemote:        getEnv("RCLONE_REMOTE", ""),
		RcloneRemotePath:    getEnv("RCLONE_REMOTE_PATH", "/storage"),
		TransferConcurrency: intEnv("TRANSFER_CONCURRENCY", 1),
		ConverterAPIURL:      getEnv("CONVERTER_API_URL", "http://api:8000"),
		IngestServiceToken:   getEnv("INGEST_SERVICE_TOKEN", ""),
		IngestConcurrency:    intEnv("INGEST_CONCURRENCY", 3),
		IngestClaimTTLSec:    intEnv("INGEST_CLAIM_TTL_SEC", 900),
		IngestMaxAttempts:    intEnv("INGEST_MAX_ATTEMPTS", 3),
		IngestSourceRemote:   getEnv("INGEST_SOURCE_REMOTE", ""),
		IngestSourceBasePath: getEnv("INGEST_SOURCE_BASE_PATH", "/incoming"),
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env %q is not set", key))
	}
	return v
}

func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func intEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
