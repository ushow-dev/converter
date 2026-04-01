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
	SourceTypeTorrent = "torrent"
	SourceTypeUpload  = "upload"
	SourceTypeHTTP    = "http"
)

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
	Title      string `json:"title"`
	TargetDir  string `json:"target_dir"`
	Priority   string `json:"priority"`
	RequestID  string `json:"request_id"`
}

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

// ConvertPayload is the message envelope pushed directly to convert_queue
// by the API for upload jobs (bypassing the download worker).
// JSON-compatible with worker/internal/model.ConvertMessage.
type ConvertPayload struct {
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

// ProxyConfig holds optional proxy settings for remote HTTP requests.
type ProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Type     string `json:"type"`     // "SOCKS5" or "HTTP"
	Username string `json:"username"`
	Password string `json:"password"`
}

// RemoteDownloadPayload is the message envelope pushed to remote_download_queue.
// JSON-compatible with worker/internal/model.RemoteDownloadMessage.
type RemoteDownloadPayload struct {
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
	SourceURL   string       `json:"source_url"`             // HTTP(S) URL of the video file
	Filename    string       `json:"filename"`               // sanitized filename to save as
	IMDbID      string       `json:"imdb_id"`
	TMDBID      string       `json:"tmdb_id"`
	Title       string       `json:"title"`
	StorageKey  string       `json:"storage_key,omitempty"`  // pre-built normalized key; forwarded to ConvertJob
	TargetDir   string       `json:"target_dir"`
	ProxyConfig *ProxyConfig `json:"proxy_config,omitempty"` // optional proxy for the download
}

// TransferPayload is the message pushed to transfer_queue after HLS conversion.
type TransferPayload struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	CorrelationID string      `json:"correlation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       TransferJob `json:"payload"`
}

// TransferJob holds details for a single rclone transfer operation.
type TransferJob struct {
	MovieID      int64  `json:"movie_id"`
	StorageKey   string `json:"storage_key"`   // relative folder name, e.g. "Inception (2010)"
	LocalPath    string `json:"local_path"`    // absolute local path to the movie folder
	ContentType  string `json:"content_type,omitempty"`
	EpisodeID    *int64 `json:"episode_id,omitempty"`
}
