package model

import "time"

// ─── Job status / stage ──────────────────────────────────────────────────────

const (
	StatusCreated    = "created"
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"

	StageDownload = "download"
	StageConvert  = "convert"
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
}

// ─── Asset ───────────────────────────────────────────────────────────────────

// Asset is the record created after successful conversion.
type Asset struct {
	AssetID     string
	JobID       string
	StoragePath string
	DurationSec *int
	VideoCodec  *string
	AudioCodec  *string
	IsReady     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
