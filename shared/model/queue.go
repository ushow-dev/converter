package model

import "time"

// ─── Download ────────────────────────────────────────────────────────────────

// DownloadMessage is the envelope pushed to download_queue.
// Unified name for api/internal/model.DownloadPayload and worker/internal/model.DownloadMessage.
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

// ─── Convert ─────────────────────────────────────────────────────────────────

// ConvertMessage is the envelope pushed to convert_queue.
// Unified name for api/internal/model.ConvertPayload and worker/internal/model.ConvertMessage.
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
	// StorageKey is the pre-built normalized storage key (slug_year_[tmdb]).
	// When set, the worker uses it directly and skips re-normalization.
	StorageKey    string `json:"storage_key,omitempty"`
	SeriesID      *int64 `json:"series_id,omitempty"`
	SeasonNumber  *int   `json:"season_number,omitempty"`
	EpisodeNumber *int   `json:"episode_number,omitempty"`
}

// ─── Remote Download ─────────────────────────────────────────────────────────

// RemoteDownloadMessage is the envelope pushed to remote_download_queue.
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
	StorageKey  string       `json:"storage_key,omitempty"` // pre-built normalized key; forwarded to ConvertJob
	TargetDir   string       `json:"target_dir"`
	ProxyConfig *ProxyConfig `json:"proxy_config,omitempty"`
}

// ─── Transfer ────────────────────────────────────────────────────────────────

// TransferMessage is the envelope pushed to transfer_queue after HLS conversion.
// Unified name for api/internal/model.TransferPayload and worker/internal/model.TransferMessage.
type TransferMessage struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	CorrelationID string      `json:"correlation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       TransferJob `json:"payload"`
}

// TransferJob is the inner payload for a transfer task.
type TransferJob struct {
	ContentID   int64  `json:"content_id"`
	StorageKey  string `json:"storage_key"`
	LocalPath   string `json:"local_path"`
	ContentType string `json:"content_type,omitempty"`
}

// ─── Cancel ──────────────────────────────────────────────────────────────────

// CancelMessage is the envelope pushed to cancel_queue.
type CancelMessage struct {
	SchemaVersion string    `json:"schema_version"`
	JobID         string    `json:"job_id"`
	CorrelationID string    `json:"correlation_id"`
	CreatedAt     time.Time `json:"created_at"`
	Payload       CancelJob `json:"payload"`
}

// CancelJob is the inner payload for a cancel task.
type CancelJob struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason,omitempty"`
}
