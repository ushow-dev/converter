package model

import "time"

type Series struct {
	ID         int64
	StorageKey string
	TMDBID     *string
	IMDbID     *string
	Title      string
	Year       *int
	PosterURL  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Season struct {
	ID           int64
	SeriesID     int64
	SeasonNumber int
}

type Episode struct {
	ID            int64
	SeasonID      int64
	EpisodeNumber int
	Title         *string
	StorageKey    string
}

type EpisodeAsset struct {
	AssetID       string
	JobID         string
	EpisodeID     int64
	StoragePath   string
	ThumbnailPath *string
	DurationSec   *int
	VideoCodec    *string
	AudioCodec    *string
	IsReady       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AudioTrack struct {
	AssetID    string
	AssetType  string
	TrackIndex int
	Language   *string
	Label      *string
	IsDefault  bool
}
