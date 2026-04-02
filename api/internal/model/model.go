package model

import (
	"time"

	sharedmodel "app/shared/model"
)

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
	JobStageTransfer JobStage = "transfer"
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
	MovieID         *int64      `json:"movie_id,omitempty"`       // resolved via media_assets.movie_id -> movies
	IMDbID          *string     `json:"imdb_id,omitempty"`
	TMDBID          *string     `json:"tmdb_id,omitempty"`
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

// ─── Movie ───────────────────────────────────────────────────────────────────

// Movie represents catalog metadata used to resolve player links.
type Movie struct {
	ID                int64     `json:"id"`
	StorageKey        string    `json:"storage_key"`
	IMDbID            *string   `json:"imdb_id,omitempty"`
	TMDBID            *string   `json:"tmdb_id,omitempty"`
	Title             *string   `json:"title,omitempty"`
	Year              *int      `json:"year,omitempty"`
	PosterURL         *string   `json:"poster_url,omitempty"`
	ThumbnailURL      *string   `json:"thumbnail_url,omitempty"`
	HasThumbnail      bool      `json:"has_thumbnail"`
	JobID             *string   `json:"job_id,omitempty"` // from media_assets, used for delete
	StorageLocationID *int64    `json:"storage_location_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ─── StorageLocation ──────────────────────────────────────────────────────────

// StorageLocation represents a configured media storage backend.
type StorageLocation struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`     // "sftp" | "s3" | "local"
	BaseURL   string    `json:"base_url"` // empty = domain not yet configured
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
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

// ─── Source types ─────────────────────────────────────────────────────────────

const (
	SourceTypeTorrent = sharedmodel.SourceTypeTorrent
	SourceTypeUpload  = sharedmodel.SourceTypeUpload
	SourceTypeHTTP    = sharedmodel.SourceTypeHTTP
)

// ─── Subtitle ─────────────────────────────────────────────────────────────────

// Subtitle represents a subtitle track stored for a movie.
type Subtitle struct {
	ID          int64     `json:"id"`
	MovieID     int64     `json:"movie_id"`
	Language    string    `json:"language"`     // ISO 639-1, e.g. "ru", "en"
	Source      string    `json:"source"`       // "opensubtitles" | "upload"
	StoragePath string    `json:"storage_path"` // absolute path on disk
	ExternalID  *string   `json:"external_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ── Queue type aliases (definitions in app/shared/model) ────────────────────

type DownloadPayload = sharedmodel.DownloadMessage
type DownloadJob = sharedmodel.DownloadJob
type ConvertPayload = sharedmodel.ConvertMessage
type ConvertJob = sharedmodel.ConvertJob
type RemoteDownloadPayload = sharedmodel.RemoteDownloadMessage
type RemoteDownloadJob = sharedmodel.RemoteDownloadJob
type TransferPayload = sharedmodel.TransferMessage
type TransferJob = sharedmodel.TransferJob
type ProxyConfig = sharedmodel.ProxyConfig
