package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"

	"app/api/internal/model"
	"app/api/internal/queue"
	"app/api/internal/repository"
)

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
