package converter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"app/worker/internal/ingest"
	"app/worker/internal/model"
)

// archiveToScanner copies the source file to scanner server's library via rclone and
// upserts it into scanner_library_movies. Deletes the local copy on success.
func (w *Worker) archiveToScanner(
	ctx context.Context,
	log *slog.Logger,
	src string,
	movie *model.Movie,
	tmdbID string,
	imdbID string,
	tmdbMeta *tmdbMetadata,
) error {
	// Normalize filename: {storageKey}{ext} (e.g. fanboy_2021_[801808].mp4)
	ext := filepath.Ext(filepath.Base(src))
	normalizedFilename := movie.StorageKey + ext

	// Get file size before any operations.
	var fileSizeBytes int64
	if info, err := os.Stat(src); err == nil {
		fileSizeBytes = info.Size()
	}

	// Remote destination: {remote}:{archiveDestPath}/{storageKey}/{normalizedFilename}
	remotePath := fmt.Sprintf("%s:%s/%s/%s", w.ingestSourceRemote, w.archiveDestPath, movie.StorageKey, normalizedFilename)
	args := []string{"copyto", src, remotePath, "--progress", "--stats-one-line", "--stats=5s"}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Info("rclone archive to scanner library", "src", src, "dest", remotePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone copy to scanner: %w", err)
	}

	// Delete local copy after successful transfer.
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		log.Warn("could not delete local source after archive", "error", err)
	} else {
		log.Info("source file archived and deleted locally", "src", src)
	}

	// Build archive request for scanner_library_movies.
	relPath := fmt.Sprintf("%s/%s", movie.StorageKey, normalizedFilename)
	// Prefer TMDB title (human-readable) over movie.Title which may be the storage key.
	title := movie.StorageKey // fallback
	if tmdbMeta != nil && tmdbMeta.Title != "" {
		title = tmdbMeta.Title
	} else if movie.Title != nil && *movie.Title != "" {
		title = *movie.Title
	}
	year := 0
	if movie.Year != nil {
		year = *movie.Year
	} else if tmdbMeta != nil {
		year = tmdbMeta.Year
	}
	qualScore, qualLabel := parseQuality(filepath.Base(src))

	archReq := ingest.ArchiveRequest{
		NormalizedName:      movie.StorageKey,
		LibraryRelativePath: relPath,
		Title:               title,
		TMDBID:              tmdbID,
		IMDbID:              imdbID,
		Year:                year,
		QualityScore:        qualScore,
		QualityLabel:        qualLabel,
		FileSizeBytes:       fileSizeBytes,
	}

	// Register in scanner DB (best-effort: failure does not abort the job).
	if _, err := w.scannerClient.Archive(ctx, archReq); err != nil {
		log.Warn("scanner library registration failed (file is on scanner)", "error", err)
	} else {
		log.Info("scanner library movie registered", "normalized_name", movie.StorageKey)
	}
	return nil
}

// parseQuality extracts a numeric quality score and label from a filename.
// Looks for common resolution markers: 2160p/4K → 2160, 1080p → 1080, 720p → 720, etc.
func parseQuality(filename string) (score int, label string) {
	lower := strings.ToLower(filename)
	switch {
	case strings.Contains(lower, "2160p") || strings.Contains(lower, "4k") || strings.Contains(lower, "uhd"):
		return 2160, "HD"
	case strings.Contains(lower, "1080p") || strings.Contains(lower, "1080i"):
		return 1080, "HD"
	case strings.Contains(lower, "720p") || strings.Contains(lower, "720i"):
		return 720, "HD"
	case strings.Contains(lower, "480p"):
		return 480, "SD"
	case strings.Contains(lower, "360p"):
		return 360, "SD"
	default:
		return 0, ""
	}
}
