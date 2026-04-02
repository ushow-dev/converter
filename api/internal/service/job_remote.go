package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"app/api/internal/model"
	"app/api/internal/queue"
	"app/api/internal/repository"
)

// CreateRemoteDownloadJobRequest holds input for a remote HTTP download job.
type CreateRemoteDownloadJobRequest struct {
	SourceURL     string             // HTTP(S) URL of the video file
	Filename      string             // original filename (used to parse title/year)
	CorrelationID string
	ProxyConfig   *model.ProxyConfig // optional proxy for the download
}

// CreateRemoteDownloadJob creates a job for downloading a video from an HTTP URL.
// It parses the title and year from the filename, optionally searches TMDB for metadata,
// then persists the job and enqueues it for the HTTP download worker.
func (s *JobService) CreateRemoteDownloadJob(
	ctx context.Context,
	req CreateRemoteDownloadJobRequest,
) (*model.Job, string, error) {
	// Parse title and year from filename.
	base := strings.TrimSuffix(req.Filename, filepath.Ext(req.Filename))
	title, year := parseTitleYear(base)

	// TMDB search (best-effort).
	tmdbID := ""
	if s.tmdbAPIKey != "" && title != "" {
		tmdbID, _ = tmdbSearch(ctx, s.tmdbAPIKey, title, year)
	}

	// Duplicate check — runs after TMDB lookup so we have the tmdbID.
	if err := s.checkDuplicate(ctx, tmdbID, ""); err != nil {
		return nil, tmdbID, err
	}

	// Sanitize filename for storage.
	safe := unsafeChars.ReplaceAllString(filepath.Base(req.Filename), "_")
	if safe == "" {
		safe = "video.mkv"
	}

	// Normalized name: scanner-compatible format {slug}_{year}_[{tmdb_id}]
	normalizedName := buildNormalizedName(title, year, tmdbID)

	now := time.Now().UTC()

	jobID := generateJobID()

	job := &model.Job{
		JobID:         jobID,
		ContentType:   "movie",
		SourceType:    model.SourceTypeHTTP,
		SourceRef:     req.SourceURL,
		Title:         &normalizedName,
		Priority:      model.JobPriorityNormal,
		Status:        model.JobStatusQueued,
		CorrelationID: &req.CorrelationID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if tmdbID != "" {
		job.TMDBID = &tmdbID
	}

	created, err := s.jobs.Create(ctx, job)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return created, tmdbID, nil
		}
		return nil, "", fmt.Errorf("create job: %w", err)
	}

	corrID := req.CorrelationID
	payload := model.RemoteDownloadPayload{
		SchemaVersion: "v1",
		JobID:         created.JobID,
		JobType:       "remote_download",
		ContentType:   "movie",
		CorrelationID: corrID,
		Attempt:       1,
		MaxAttempts:   3,
		CreatedAt:     now,
		Payload: model.RemoteDownloadJob{
			SourceURL:   req.SourceURL,
			Filename:    safe,
			TMDBID:      tmdbID,
			Title:       normalizedName,
			StorageKey:  normalizedName,
			TargetDir:   fmt.Sprintf("%s/downloads/%s", s.mediaRoot, created.JobID),
			ProxyConfig: req.ProxyConfig,
		},
	}
	if err := s.queue.Enqueue(ctx, queue.RemoteDownloadQueue, payload); err != nil {
		_ = s.jobs.UpdateStatus(ctx, created.JobID, model.JobStatusCreated, nil, 0)
		return created, tmdbID, fmt.Errorf("enqueue remote download job: %w", err)
	}

	return created, tmdbID, nil
}

// parseTitleYear extracts a title and 4-digit year from a filename stem like
// "120 Bahadur (2025) Hindi 720p ..." → ("120 Bahadur", "2025").
func parseTitleYear(stem string) (title, year string) {
	m := titleYearRe.FindStringSubmatch(stem)
	if m != nil {
		return strings.TrimSpace(m[1]), m[2]
	}
	// Fallback: use the whole stem as title.
	return strings.TrimSpace(stem), ""
}

// tmdbSearch searches TMDB for the best match and returns the TMDB ID string.
func tmdbSearch(ctx context.Context, apiKey, title, year string) (string, error) {
	q := url.Values{}
	q.Set("query", title)
	q.Set("api_key", apiKey)
	if year != "" {
		q.Set("year", year)
	}
	searchURL := "https://api.themoviedb.org/3/search/movie?" + q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("TMDB search returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Results) == 0 {
		return "", fmt.Errorf("no TMDB results for %q", title)
	}
	return fmt.Sprintf("%d", result.Results[0].ID), nil
}
