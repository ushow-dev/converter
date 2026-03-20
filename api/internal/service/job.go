package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"app/api/internal/model"
	"app/api/internal/queue"
	"app/api/internal/repository"
)

var (
	unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._\-]`)
	titleYearRe = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)`)
)

// buildNormalizedName produces a filesystem-safe name matching the scanner format.
// Format: {slug}_{year}_[{tmdb_id}]
// slug = lowercase title, only letters and digits, spaces collapsed to underscores.
func buildNormalizedName(title, year, tmdbID string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			sb.WriteRune(r)
		}
	}
	slug := strings.TrimSpace(sb.String())
	slug = strings.Join(strings.Fields(slug), "_")
	if slug == "" {
		return title
	}
	parts := []string{slug}
	if year != "" {
		parts = append(parts, year)
	}
	name := strings.Join(parts, "_")
	if tmdbID != "" {
		name += fmt.Sprintf("_[%s]", tmdbID)
	}
	return name
}

// DuplicateError is returned when a movie with the given TMDB/IMDb ID already
// exists in the database with a completed (ready) asset.
type DuplicateError struct {
	MovieID int64
	Title   string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("movie already exists (id=%d)", e.MovieID)
}

// CreateJobRequest holds input for creating a new media job.
type CreateJobRequest struct {
	RequestID     string
	ContentType   string
	SourceType    string
	SourceRef     string
	IMDbID        string
	TMDBID        string
	Title         string
	Priority      model.JobPriority
	CorrelationID string
}

// JobService handles media job lifecycle.
type JobService struct {
	jobs       *repository.JobRepository
	movieRepo  *repository.MovieRepository
	queue      *queue.Client
	mediaRoot  string
	tmdbAPIKey string
}

// NewJobService creates a JobService.
func NewJobService(jobs *repository.JobRepository, movieRepo *repository.MovieRepository, q *queue.Client, mediaRoot, tmdbAPIKey string) *JobService {
	return &JobService{
		jobs:       jobs,
		movieRepo:  movieRepo,
		queue:      q,
		mediaRoot:  mediaRoot,
		tmdbAPIKey: tmdbAPIKey,
	}
}

// checkDuplicate looks up a movie by tmdbID or imdbID (in that order).
// Returns *DuplicateError if the movie already has a completed (ready) asset.
// Returns nil if not found or no ready asset yet — job creation proceeds normally.
// Lookup errors are silently ignored so they never block job creation.
func (s *JobService) checkDuplicate(ctx context.Context, tmdbID, imdbID string) error {
	var movie *model.Movie
	var err error

	switch {
	case tmdbID != "":
		movie, err = s.movieRepo.GetByTMDBID(ctx, tmdbID)
	case imdbID != "":
		movie, err = s.movieRepo.GetByIMDbID(ctx, imdbID)
	default:
		return nil
	}
	if errors.Is(err, repository.ErrNotFound) || err != nil {
		return nil // not found or lookup error — don't block
	}

	// movie.JobID is non-nil only when a ready asset exists (LEFT JOIN WHERE is_ready=true).
	if movie.JobID == nil {
		return nil // movie row exists but conversion not yet complete
	}

	title := ""
	if movie.Title != nil {
		title = *movie.Title
	}
	return &DuplicateError{MovieID: movie.ID, Title: title}
}

// CreateJob creates a media job (idempotent via request_id) and publishes it to the download queue.
func (s *JobService) CreateJob(ctx context.Context, req CreateJobRequest) (*model.Job, error) {
	if err := s.checkDuplicate(ctx, req.TMDBID, req.IMDbID); err != nil {
		return nil, err
	}

	jobID := generateJobID()
	now := time.Now().UTC()
	priority := req.Priority
	if priority == "" {
		priority = model.JobPriorityNormal
	}

	job := &model.Job{
		JobID:         jobID,
		ContentType:   req.ContentType,
		SourceType:    req.SourceType,
		SourceRef:     req.SourceRef,
		Priority:      priority,
		Status:        model.JobStatusQueued,
		CorrelationID: &req.CorrelationID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if req.RequestID != "" {
		job.RequestID = &req.RequestID
	}

	created, err := s.jobs.Create(ctx, job)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			// Idempotent: return the existing job without re-enqueuing.
			return created, nil
		}
		return nil, fmt.Errorf("create job: %w", err)
	}

	// Publish to download_queue.
	corrID := ""
	if job.CorrelationID != nil {
		corrID = *job.CorrelationID
	}
	reqID := ""
	if job.RequestID != nil {
		reqID = *job.RequestID
	}
	payload := model.DownloadPayload{
		SchemaVersion: "v1",
		JobID:         created.JobID,
		JobType:       "download",
		ContentType:   created.ContentType,
		CorrelationID: corrID,
		Attempt:       1,
		MaxAttempts:   5,
		CreatedAt:     now,
		Payload: model.DownloadJob{
			SourceType: created.SourceType,
			SourceRef:  created.SourceRef,
			IMDbID:     req.IMDbID,
			TMDBID:     req.TMDBID,
			Title:      req.Title,
			TargetDir:  fmt.Sprintf("/media/downloads/%s", created.JobID),
			Priority:   string(created.Priority),
			RequestID:  reqID,
		},
	}
	if err := s.queue.Enqueue(ctx, queue.DownloadQueue, payload); err != nil {
		// Queue failure is not fatal for job creation: the job is already persisted.
		// The worker can pick it up after a restart or via a recovery sweep.
		// Mark as created (not queued) so the state is accurate.
		_ = s.jobs.UpdateStatus(ctx, created.JobID, model.JobStatusCreated, nil, 0)
		return created, fmt.Errorf("enqueue download job: %w", err)
	}

	return created, nil
}

// CreateUploadJobRequest holds input for creating a job from a local file upload.
type CreateUploadJobRequest struct {
	RequestID     string
	Title         string
	IMDbID        string
	TMDBID        string
	CorrelationID string
}

// CreateUploadJob saves the uploaded file to /media/downloads/{jobID}/ and
// enqueues it directly to convert_queue (skipping the download worker).
func (s *JobService) CreateUploadJob(
	ctx context.Context,
	req CreateUploadJobRequest,
	file multipart.File,
	filename string,
) (*model.Job, error) {
	if err := s.checkDuplicate(ctx, req.TMDBID, req.IMDbID); err != nil {
		return nil, err
	}

	jobID := generateJobID()
	now := time.Now().UTC()

	// Sanitize filename: keep only safe characters.
	safe := unsafeChars.ReplaceAllString(filepath.Base(filename), "_")
	if safe == "" {
		safe = "video.mkv"
	}

	// Write the uploaded file to /media/downloads/{jobID}/{safe}.
	destDir := filepath.Join(s.mediaRoot, "downloads", jobID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create download dir: %w", err)
	}
	destPath := filepath.Join(destDir, safe)
	dst, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("create dest file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.RemoveAll(destDir)
		return nil, fmt.Errorf("write uploaded file: %w", err)
	}

	title := req.Title
	job := &model.Job{
		JobID:         jobID,
		ContentType:   "movie",
		SourceType:    model.SourceTypeUpload,
		SourceRef:     safe,
		Title:         &title,
		Priority:      model.JobPriorityNormal,
		Status:        model.JobStatusQueued,
		CorrelationID: &req.CorrelationID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if req.RequestID != "" {
		job.RequestID = &req.RequestID
	}

	created, err := s.jobs.Create(ctx, job)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return created, nil
		}
		_ = os.RemoveAll(destDir)
		return nil, fmt.Errorf("create job: %w", err)
	}

	// Push directly to convert_queue (no download stage needed).
	corrID := ""
	if job.CorrelationID != nil {
		corrID = *job.CorrelationID
	}
	payload := model.ConvertPayload{
		SchemaVersion: "v1",
		JobID:         created.JobID,
		JobType:       "convert",
		ContentType:   "movie",
		CorrelationID: corrID,
		Attempt:       1,
		MaxAttempts:   5,
		CreatedAt:     now,
		Payload: model.ConvertJob{
			InputPath:     destPath,
			OutputPath:    fmt.Sprintf("%s/temp/%s", s.mediaRoot, created.JobID),
			OutputProfile: "mp4_h264_aac_1080p",
			FinalDir:      fmt.Sprintf("%s/converted/movies", s.mediaRoot),
			IMDbID:        req.IMDbID,
			TMDBID:        req.TMDBID,
			Title:         req.Title,
		},
	}
	if err := s.queue.Enqueue(ctx, queue.ConvertQueue, payload); err != nil {
		_ = s.jobs.UpdateStatus(ctx, created.JobID, model.JobStatusCreated, nil, 0)
		return created, fmt.Errorf("enqueue convert job: %w", err)
	}

	return created, nil
}

// DeleteJob removes a job, its DB records, and its files from disk.
func (s *JobService) DeleteJob(ctx context.Context, jobID string) error {
	meta, err := s.jobs.Delete(ctx, jobID)
	if err != nil {
		return err
	}

	// Best-effort filesystem cleanup — ignore missing dirs.
	for _, sub := range []string{"downloads", "converted", "temp"} {
		_ = os.RemoveAll(filepath.Join(s.mediaRoot, sub, jobID))
	}
	if meta != nil && meta.StoragePath != nil {
		_ = os.RemoveAll(filepath.Dir(*meta.StoragePath))
	}
	return nil
}

// GetJob fetches a job by ID.
func (s *JobService) GetJob(ctx context.Context, jobID string) (*model.Job, error) {
	return s.jobs.GetByID(ctx, jobID)
}

// ListJobs lists jobs with optional status filter and cursor pagination.
func (s *JobService) ListJobs(
	ctx context.Context, status string, limit int, cursor string,
) ([]*model.Job, string, error) {
	return s.jobs.List(ctx, status, limit, cursor)
}

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

func generateJobID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("job_%x", b)
}
