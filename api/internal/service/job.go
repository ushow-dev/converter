package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"time"

	"app/api/internal/model"
	"app/api/internal/queue"
	"app/api/internal/repository"
)

// CreateJobRequest holds input for creating a new media job.
type CreateJobRequest struct {
	RequestID   string
	ContentType string
	SourceType  string
	SourceRef   string
	Priority    model.JobPriority
	CorrelationID string
}

// JobService handles media job lifecycle.
type JobService struct {
	jobs      *repository.JobRepository
	queue     *queue.Client
	mediaRoot string
}

// NewJobService creates a JobService.
func NewJobService(jobs *repository.JobRepository, q *queue.Client, mediaRoot string) *JobService {
	return &JobService{jobs: jobs, queue: q, mediaRoot: mediaRoot}
}

// CreateJob creates a media job (idempotent via request_id) and publishes it to the download queue.
func (s *JobService) CreateJob(ctx context.Context, req CreateJobRequest) (*model.Job, error) {
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

// DeleteJob removes a job, its DB records, and its files from disk.
func (s *JobService) DeleteJob(ctx context.Context, jobID string) error {
	// Best-effort filesystem cleanup — ignore missing dirs.
	for _, sub := range []string{"downloads", "converted", "temp"} {
		_ = os.RemoveAll(fmt.Sprintf("%s/%s/%s", s.mediaRoot, sub, jobID))
	}
	return s.jobs.Delete(ctx, jobID)
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
