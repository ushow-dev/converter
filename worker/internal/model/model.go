package model

import "time"

// ─── Job status / stage ──────────────────────────────────────────────────────

const (
	StatusCreated    = "created"
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"

	StageDownload  = "download"
	StageConvert   = "convert"
	StageTransfer  = "transfer"
)

// ─── Queue message envelopes ─────────────────────────────────────────────────

// DownloadMessage is the envelope consumed from download_queue.
// JSON-compatible with api/internal/model.DownloadPayload.
type DownloadMessage struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	JobType       string      `json:"job_type"`
	ContentType   string      `json:"content_type"`
	CorrelationID string      `json:"correlation_id"`
	Attempt       int         `json:"attempt"`
	MaxAttempts   int         `json:"max_attempts"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       DownloadJob `json:"payload"`
}

// DownloadJob is the inner payload for a download task.
type DownloadJob struct {
	SourceType string `json:"source_type"`
	SourceRef  string `json:"source_ref"`
	IMDbID     string `json:"imdb_id"`
	TMDBID     string `json:"tmdb_id"`
	Title      string `json:"title"`
	TargetDir  string `json:"target_dir"`
	Priority   string `json:"priority"`
	RequestID  string `json:"request_id"`
}

// ConvertMessage is the envelope pushed to convert_queue by the download worker
// and consumed by the convert worker.
type ConvertMessage struct {
	SchemaVersion string     `json:"schema_version"`
	JobID         string     `json:"job_id"`
	JobType       string     `json:"job_type"`
	ContentType   string     `json:"content_type"`
	CorrelationID string     `json:"correlation_id"`
	Attempt       int        `json:"attempt"`
	MaxAttempts   int        `json:"max_attempts"`
	CreatedAt     time.Time  `json:"created_at"`
	Payload       ConvertJob `json:"payload"`
}

// ConvertJob is the inner payload for a convert task.
type ConvertJob struct {
	InputPath     string `json:"input_path"`
	OutputPath    string `json:"output_path"`
	OutputProfile string `json:"output_profile"`
	FinalDir      string `json:"final_dir"`
	IMDbID        string `json:"imdb_id"`
	TMDBID        string `json:"tmdb_id"`
	Title         string `json:"title"`
}

// ProxyConfig holds optional proxy settings for remote HTTP requests.
type ProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Type     string `json:"type"`     // "SOCKS5" or "HTTP"
	Username string `json:"username"`
	Password string `json:"password"`
}

// RemoteDownloadMessage is the envelope consumed from remote_download_queue.
type RemoteDownloadMessage struct {
	SchemaVersion string            `json:"schema_version"`
	JobID         string            `json:"job_id"`
	JobType       string            `json:"job_type"`
	ContentType   string            `json:"content_type"`
	CorrelationID string            `json:"correlation_id"`
	Attempt       int               `json:"attempt"`
	MaxAttempts   int               `json:"max_attempts"`
	CreatedAt     time.Time         `json:"created_at"`
	Payload       RemoteDownloadJob `json:"payload"`
}

// RemoteDownloadJob is the inner payload for an HTTP download task.
type RemoteDownloadJob struct {
	SourceURL   string       `json:"source_url"`
	Filename    string       `json:"filename"`
	IMDbID      string       `json:"imdb_id"`
	TMDBID      string       `json:"tmdb_id"`
	Title       string       `json:"title"`
	TargetDir   string       `json:"target_dir"`
	ProxyConfig *ProxyConfig `json:"proxy_config,omitempty"`
}

// ─── Asset ───────────────────────────────────────────────────────────────────

// Asset is the record created after successful conversion.
type Asset struct {
	AssetID       string
	JobID         string
	MovieID       *int64
	StoragePath   string  // path to master.m3u8
	ThumbnailPath *string // path to thumbnail.jpg, nil if unavailable
	DurationSec   *int
	VideoCodec    *string
	AudioCodec    *string
	IsReady       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Movie is the catalog record created after successful conversion.
type Movie struct {
	ID         int64
	StorageKey string
	IMDbID     *string
	TMDBID     *string
	Title      *string
	Year       *int
	PosterURL  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ─── Subtitle ─────────────────────────────────────────────────────────────────

// Subtitle represents a subtitle track stored for a movie.
type Subtitle struct {
	ID          int64
	MovieID     int64
	Language    string  // ISO 639-1, e.g. "ru", "en"
	Source      string  // "opensubtitles" | "upload"
	StoragePath string
	ExternalID  *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ─── Storage & Transfer ───────────────────────────────────────────────────────

// StorageLocation mirrors the api model for worker-side reads.
type StorageLocation struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
}

// TransferMessage is the BLPOP envelope for transfer_queue.
// JSON-compatible with api/internal/model.TransferPayload.
type TransferMessage struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	CorrelationID string      `json:"correlation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       TransferJob `json:"payload"`
}

// TransferJob is the inner payload for a transfer task.
type TransferJob struct {
	MovieID    int64  `json:"movie_id"`
	StorageKey string `json:"storage_key"`
	LocalPath  string `json:"local_path"`
}
