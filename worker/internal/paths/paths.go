// Package paths provides centralized media path resolution.
package paths

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Resolver builds filesystem and storage paths for media content.
type Resolver struct {
	mediaRoot string
}

// New creates a Resolver with the given media root (e.g. "/media").
func New(mediaRoot string) *Resolver {
	return &Resolver{mediaRoot: mediaRoot}
}

// MovieFinalDir returns the local path for converted movie HLS output.
// Example: /media/converted/movies/inception_2010_[16662]
func (r *Resolver) MovieFinalDir(storageKey string) string {
	return filepath.Join(r.mediaRoot, "converted", "movies", storageKey)
}

// EpisodeFinalDir returns the local path for converted episode HLS output.
// Example: /media/converted/series/devil_may_cry_2025_[235930]/s01/e02
func (r *Resolver) EpisodeFinalDir(seriesStorageKey string, season, episode int) string {
	return filepath.Join(r.mediaRoot, "converted", "series", seriesStorageKey,
		fmt.Sprintf("s%02d", season), fmt.Sprintf("e%02d", episode))
}

// MovieTransferKey returns the storage key for rclone transfer destination.
// Example: inception_2010_[16662]
func (r *Resolver) MovieTransferKey(storageKey string) string {
	return storageKey
}

// EpisodeTransferKey returns the storage key for rclone transfer destination.
// Example: devil_may_cry_2025_[235930]/s01/e02
func (r *Resolver) EpisodeTransferKey(seriesStorageKey string, season, episode int) string {
	return fmt.Sprintf("%s/s%02d/e%02d", seriesStorageKey, season, episode)
}

// TransferDest builds the full rclone destination path.
// contentDir is "movies" or "series", storageKey is the relative path within.
func (r *Resolver) TransferDest(contentType, storageKey string) string {
	if contentType == "episode" {
		return "series/" + storageKey
	}
	return "movies/" + storageKey
}

// DownloadsDir returns the path for raw downloads.
func (r *Resolver) DownloadsDir(jobID string) string {
	return filepath.Join(r.mediaRoot, "downloads", jobID)
}

// TempDir returns the FFmpeg working directory.
func (r *Resolver) TempDir(jobID string) string {
	return filepath.Join(r.mediaRoot, "temp", jobID)
}

// StripMasterPlaylist removes the trailing /master.m3u8 from a storage path.
func StripMasterPlaylist(storagePath string) string {
	const suffix = "/master.m3u8"
	if strings.HasSuffix(storagePath, suffix) {
		return storagePath[:len(storagePath)-len(suffix)]
	}
	return storagePath
}
