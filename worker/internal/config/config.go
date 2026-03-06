package config

import (
	"fmt"
	"os"
	"strconv"
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
	DownloadConcurrency int
	ConvertConcurrency  int

	// Internal health server port
	HealthPort string

	// TMDB API key for backdrop download (optional)
	TMDBAPIKey string
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
		DownloadConcurrency: intEnv("DOWNLOAD_CONCURRENCY", 2),
		ConvertConcurrency:  intEnv("CONVERT_CONCURRENCY", 1),
		TMDBAPIKey:          getEnv("TMDB_API_KEY", ""),
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

func intEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
