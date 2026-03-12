package subtitles

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// SubtitleResult is the outcome of a successful subtitle download for one language.
type SubtitleResult struct {
	Language   string
	FilePath   string // absolute path to the .vtt file on disk
	ExternalID string // OpenSubtitles file_id as string
}

// Fetcher orchestrates searching, downloading, and saving subtitles.
type Fetcher struct {
	client    *Client
	languages []string
}

// NewFetcher creates a Fetcher. languages should be ISO 639-1 codes, e.g. ["ru","en"].
func NewFetcher(apiKey string, languages []string) *Fetcher {
	return &Fetcher{
		client:    NewClient(apiKey),
		languages: languages,
	}
}

// FetchAndSave searches OpenSubtitles for the given tmdbID, downloads each found
// subtitle, converts SRT→VTT, and writes files to {outputDir}/subtitles/{lang}.vtt.
// Per-language errors are logged and skipped; partial results are returned.
func (f *Fetcher) FetchAndSave(ctx context.Context, tmdbID, outputDir string) []SubtitleResult {
	results, err := f.client.Search(ctx, tmdbID, f.languages)
	if err != nil {
		slog.Warn("subtitle search failed", "tmdb_id", tmdbID, "error", err)
		return nil
	}
	if len(results) == 0 {
		slog.Info("no subtitles found", "tmdb_id", tmdbID, "languages", f.languages)
		return nil
	}

	subtitleDir := filepath.Join(outputDir, "subtitles")
	if err := os.MkdirAll(subtitleDir, 0o777); err != nil {
		slog.Warn("subtitle dir create failed", "dir", subtitleDir, "error", err)
		return nil
	}

	var saved []SubtitleResult
	for _, result := range results {
		filePath, err := f.downloadOne(ctx, result, subtitleDir)
		if err != nil {
			slog.Warn("subtitle download failed", "lang", result.Language, "error", err)
			continue
		}
		saved = append(saved, SubtitleResult{
			Language:   result.Language,
			FilePath:   filePath,
			ExternalID: strconv.Itoa(result.FileID),
		})
	}
	return saved
}

func (f *Fetcher) downloadOne(ctx context.Context, result Result, subtitleDir string) (string, error) {
	downloadURL, err := f.client.DownloadURL(ctx, result.FileID)
	if err != nil {
		return "", fmt.Errorf("get download url: %w", err)
	}

	raw, err := f.client.FetchRaw(ctx, downloadURL)
	if err != nil {
		return "", fmt.Errorf("fetch raw: %w", err)
	}

	vtt := SRTtoVTT(raw)

	filePath := filepath.Join(subtitleDir, result.Language+".vtt")
	if err := os.WriteFile(filePath, vtt, 0o666); err != nil {
		return "", fmt.Errorf("write vtt: %w", err)
	}
	return filePath, nil
}
