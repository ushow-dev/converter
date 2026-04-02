package model

import (
	"time"

	sharedmodel "app/shared/model"
)

// ── Queue type aliases (definitions in app/shared/model) ────────────────────

type DownloadMessage = sharedmodel.DownloadMessage
type DownloadJob = sharedmodel.DownloadJob
type ConvertMessage = sharedmodel.ConvertMessage
type ConvertJob = sharedmodel.ConvertJob
type RemoteDownloadMessage = sharedmodel.RemoteDownloadMessage
type RemoteDownloadJob = sharedmodel.RemoteDownloadJob
type TransferMessage = sharedmodel.TransferMessage
type TransferJob = sharedmodel.TransferJob
type ProxyConfig = sharedmodel.ProxyConfig

// ── Status/stage constant aliases ───────────────────────────────────────────

const (
	StatusCreated    = string(sharedmodel.StatusCreated)
	StatusQueued     = string(sharedmodel.StatusQueued)
	StatusInProgress = string(sharedmodel.StatusInProgress)
	StatusCompleted  = string(sharedmodel.StatusCompleted)
	StatusFailed     = string(sharedmodel.StatusFailed)

	StageDownload = string(sharedmodel.StageDownload)
	StageConvert  = string(sharedmodel.StageConvert)
	StageTransfer = string(sharedmodel.StageTransfer)
)

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
