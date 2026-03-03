package model

import "time"

// ─── Job ─────────────────────────────────────────────────────────────────────

// JobStatus represents the lifecycle state of a media job.
type JobStatus string

const (
	JobStatusCreated    JobStatus = "created"
	JobStatusQueued     JobStatus = "queued"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// JobStage represents the active processing stage.
type JobStage string

const (
	JobStageDownload JobStage = "download"
	JobStageConvert  JobStage = "convert"
)

// JobPriority represents processing priority.
type JobPriority string

const (
	JobPriorityLow    JobPriority = "low"
	JobPriorityNormal JobPriority = "normal"
	JobPriorityHigh   JobPriority = "high"
)

// Job is the core domain entity representing a media processing task.
type Job struct {
	JobID           string      `json:"job_id"`
	ContentType     string      `json:"content_type"`
	SourceType      string      `json:"source_type"`
	SourceRef       string      `json:"source_ref"`
	Title           *string     `json:"title,omitempty"`          // from search_results JOIN
	ThumbnailPath   *string     `json:"thumbnail_path,omitempty"` // from media_assets JOIN
	Priority        JobPriority `json:"priority"`
	Status          JobStatus   `json:"status"`
	Stage           *JobStage   `json:"stage,omitempty"`
	ProgressPercent int         `json:"progress_percent"`
	ErrorCode       *string     `json:"error_code"`
	ErrorMessage    *string     `json:"error_message"`
	Retryable       *bool       `json:"retryable,omitempty"`
	RequestID       *string     `json:"-"`
	CorrelationID   *string     `json:"correlation_id,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// ─── Asset ───────────────────────────────────────────────────────────────────

// Asset represents a converted media file ready for playback.
type Asset struct {
	AssetID       string    `json:"asset_id"`
	JobID         string    `json:"job_id"`
	StoragePath   string    `json:"storage_path"`   // path to master.m3u8
	ThumbnailPath *string   `json:"thumbnail_path,omitempty"` // path to thumbnail.jpg
	DurationSec   *int      `json:"duration_sec,omitempty"`
	VideoCodec    *string   `json:"video_codec,omitempty"`
	AudioCodec    *string   `json:"audio_codec,omitempty"`
	IsReady       bool      `json:"is_ready"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ─── SearchResult ─────────────────────────────────────────────────────────────

// SearchResult is a normalised release entry returned from an indexer backend.
type SearchResult struct {
	ExternalID  string    `json:"external_id"`
	Title       string    `json:"title"`
	SourceType  string    `json:"source_type"`
	SourceRef   string    `json:"source_ref"`
	SizeBytes   int64     `json:"size_bytes"`
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	Indexer     string    `json:"indexer"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// ─── Queue payloads ──────────────────────────────────────────────────────────

// DownloadPayload is the message envelope pushed to download_queue.
type DownloadPayload struct {
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
	TargetDir  string `json:"target_dir"`
	Priority   string `json:"priority"`
	RequestID  string `json:"request_id"`
}
