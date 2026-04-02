package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// DeleteJob removes a job, its DB records, and its files from disk.
// It also signals the worker to cancel any in-flight processing for this job.
func (s *JobService) DeleteJob(ctx context.Context, jobID string) error {
	meta, err := s.jobs.Delete(ctx, jobID)
	if err != nil {
		return err
	}

	// Signal the worker to cancel any in-flight processing.
	// Best-effort: if Redis is unavailable the job is already gone from DB
	// and the worker will detect deletion via IsTerminal on its next DB check.
	_ = s.queue.Enqueue(ctx, queue.CancelQueue, jobID)

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

func generateJobID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("job_%x", b)
}
