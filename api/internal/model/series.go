package model

import "time"

const ContentTypeSeries = "series"

type Series struct {
	ID         int64     `json:"id"`
	StorageKey string    `json:"storage_key"`
	TMDBID     *string   `json:"tmdb_id,omitempty"`
	IMDbID     *string   `json:"imdb_id,omitempty"`
	Title      string    `json:"title"`
	Year       *int      `json:"year,omitempty"`
	PosterURL  *string   `json:"poster_url,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Season struct {
	ID           int64     `json:"id"`
	SeriesID     int64     `json:"series_id"`
	SeasonNumber int       `json:"season_number"`
	PosterURL    *string   `json:"poster_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Episode struct {
	ID            int64     `json:"id"`
	SeasonID      int64     `json:"season_id"`
	EpisodeNumber int       `json:"episode_number"`
	Title         *string   `json:"title,omitempty"`
	StorageKey    string    `json:"storage_key"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type EpisodeAsset struct {
	AssetID       string    `json:"asset_id"`
	JobID         string    `json:"job_id"`
	EpisodeID     int64     `json:"episode_id"`
	StoragePath   string    `json:"storage_path"`
	ThumbnailPath *string   `json:"thumbnail_path,omitempty"`
	DurationSec   *int      `json:"duration_sec,omitempty"`
	VideoCodec    *string   `json:"video_codec,omitempty"`
	AudioCodec    *string   `json:"audio_codec,omitempty"`
	IsReady       bool      `json:"is_ready"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type EpisodeSubtitle struct {
	ID          int64     `json:"id"`
	EpisodeID   int64     `json:"episode_id"`
	Language    string    `json:"language"`
	Source      string    `json:"source"`
	StoragePath string    `json:"storage_path"`
	ExternalID  *string   `json:"external_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AudioTrack struct {
	ID         int64   `json:"id"`
	AssetID    string  `json:"asset_id"`
	AssetType  string  `json:"asset_type"`
	TrackIndex int     `json:"track_index"`
	Language   *string `json:"language,omitempty"`
	Label      *string `json:"label,omitempty"`
	IsDefault  bool    `json:"is_default"`
}
