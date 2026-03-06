package config

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// HTTP server
	Port string

	// PostgreSQL
	DatabaseURL string

	// Redis
	RedisURL string

	// JWT
	JWTSecret    string
	JWTExpiry    time.Duration
	PlayerAPIKey string

	// Admin credentials
	AdminEmail        string
	AdminPasswordHash string // bcrypt hash

	// Prowlarr
	ProwlarrBaseURL string
	ProwlarrAPIKey  string

	// TMDB
	TMDBAPIKey string

	// Worker
	WorkerHealthPort string

	// Media
	MediaRoot       string
	MediaBaseURL    string
	MediaSigningKey string
	MediaSigningTTL time.Duration

	// App
	Environment string
}

// Load reads environment variables and returns a populated Config.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:             getEnv("PORT", "8000"),
		DatabaseURL:      mustEnv("DATABASE_URL"),
		RedisURL:         getEnv("REDIS_URL", "redis://redis:6379"),
		JWTSecret:        mustEnv("JWT_SECRET"),
		JWTExpiry:        24 * time.Hour,
		PlayerAPIKey:     mustEnv("PLAYER_API_KEY"),
		AdminEmail:       getEnv("ADMIN_EMAIL", "admin@example.com"),
		ProwlarrBaseURL:  getEnv("PROWLARR_BASE_URL", "http://prowlarr:9696"),
		ProwlarrAPIKey:   getEnv("PROWLARR_API_KEY", ""),
		TMDBAPIKey:       getEnv("TMDB_API_KEY", ""),
		WorkerHealthPort: getEnv("WORKER_HEALTH_PORT", "8001"),
		MediaRoot:        getEnv("MEDIA_ROOT", "/media"),
		MediaBaseURL:     getEnv("MEDIA_BASE_URL", ""),
		MediaSigningKey:  getEnv("MEDIA_SIGNING_KEY", ""),
		MediaSigningTTL:  getEnvDuration("MEDIA_SIGNING_TTL", 2*time.Minute),
		Environment:      getEnv("APP_ENV", "development"),
	}

	// Resolve admin password: prefer pre-hashed value, fall back to plaintext
	// (plaintext is converted to bcrypt hash at startup — never stored).
	if hash := getEnv("ADMIN_PASSWORD_HASH", ""); hash != "" {
		cfg.AdminPasswordHash = hash
	} else {
		plain := getEnv("ADMIN_PASSWORD", "")
		if plain == "" {
			return nil, fmt.Errorf("ADMIN_PASSWORD_HASH or ADMIN_PASSWORD must be set")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash admin password: %w", err)
		}
		cfg.AdminPasswordHash = string(h)
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		panic(fmt.Sprintf("invalid duration for %q: %v", key, err))
	}
	return d
}
