package model

import "time"

// ─── IncomingStatus ───────────────────────────────────────────────────────────

// IncomingStatus represents the lifecycle state of an incoming media item.
type IncomingStatus string

const (
	IncomingStatusNew                  IncomingStatus = "new"
	IncomingStatusClaimed              IncomingStatus = "claimed"
	IncomingStatusCopying              IncomingStatus = "copying"
	IncomingStatusCopied               IncomingStatus = "copied"
	IncomingStatusCompleted            IncomingStatus = "completed"
	IncomingStatusFailed               IncomingStatus = "failed"
	IncomingStatusSkipped              IncomingStatus = "skipped"
	IncomingStatusReviewDuplicate      IncomingStatus = "review_duplicate"
	IncomingStatusReviewUnknownQuality IncomingStatus = "review_unknown_quality"
	IncomingStatusUpgradeCandidate     IncomingStatus = "upgrade_candidate"
)

// ─── IncomingItem ─────────────────────────────────────────────────────────────

// IncomingItem represents a media file awaiting ingest from an external source.
type IncomingItem struct {
	ID                 int64          `json:"id"`
	SourcePath         string         `json:"source_path"`
	SourceFilename     string         `json:"source_filename"`
	NormalizedName     *string        `json:"normalized_name,omitempty"`
	TMDBID             *string        `json:"tmdb_id,omitempty"`
	ContentKind        string         `json:"content_kind"`
	FileSizeBytes      *int64         `json:"file_size_bytes,omitempty"`
	StableSince        *time.Time     `json:"stable_since,omitempty"`
	Status             IncomingStatus `json:"status"`
	Attempts           int            `json:"attempts"`
	ClaimedAt          *time.Time     `json:"claimed_at,omitempty"`
	ClaimExpiresAt     *time.Time     `json:"claim_expires_at,omitempty"`
	QualityScore       *int           `json:"quality_score,omitempty"`
	IsUpgradeCandidate bool           `json:"is_upgrade_candidate"`
	DuplicateOfMovieID *int64         `json:"duplicate_of_movie_id,omitempty"`
	ReviewReason       *string        `json:"review_reason,omitempty"`
	APIJobID           *string        `json:"api_job_id,omitempty"`
	ErrorMessage       *string        `json:"error_message,omitempty"`
	LocalPath          *string        `json:"local_path,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// ─── Request/Response Types ───────────────────────────────────────────────────

// RegisterIncomingRequest is the body for POST /api/ingest/incoming/register.
type RegisterIncomingRequest struct {
	SourcePath         string  `json:"source_path"`
	SourceFilename     string  `json:"source_filename"`
	NormalizedName     *string `json:"normalized_name,omitempty"`
	TMDBID             *string `json:"tmdb_id,omitempty"`
	ContentKind        string  `json:"content_kind"`
	FileSizeBytes      *int64  `json:"file_size_bytes,omitempty"`
	StableSince        *time.Time `json:"stable_since,omitempty"`
	QualityScore       *int    `json:"quality_score,omitempty"`
	IsUpgradeCandidate bool    `json:"is_upgrade_candidate"`
	DuplicateOfMovieID *int64  `json:"duplicate_of_movie_id,omitempty"`
	ReviewReason       *string `json:"review_reason,omitempty"`
	Status             *string `json:"status,omitempty"` // allows scanner to set review_duplicate etc.
}

// ClaimIncomingRequest is the body for POST /api/ingest/incoming/claim.
type ClaimIncomingRequest struct {
	Limit       int `json:"limit"`
	ClaimTTLSec int `json:"claim_ttl_sec"`
}

// ClaimIncomingResponse is the response from POST /api/ingest/incoming/claim.
type ClaimIncomingResponse struct {
	Items []IncomingItem `json:"items"`
}

// ProgressIncomingRequest is the body for POST /api/ingest/incoming/progress.
type ProgressIncomingRequest struct {
	ID              int64  `json:"id"`
	ProgressPercent int    `json:"progress_percent"`
	Status          string `json:"status"` // "copying" | "copied"
}

// FailIncomingRequest is the body for POST /api/ingest/incoming/fail.
type FailIncomingRequest struct {
	ID           int64  `json:"id"`
	ErrorMessage string `json:"error_message"`
}

// CompleteIncomingRequest is the body for POST /api/ingest/incoming/complete.
// The API (not the worker) creates the media_job and pushes to convert_queue.
type CompleteIncomingRequest struct {
	ID        int64  `json:"id"`
	LocalPath string `json:"local_path"` // absolute local path of copied file
}

// CompleteIncomingResponse is the response from POST /api/ingest/incoming/complete.
type CompleteIncomingResponse struct {
	JobID string `json:"job_id"`
}
