package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"app/api/internal/model"
	"app/api/internal/queue"
	"app/api/internal/repository"
)

// IngestService handles the incoming media ingest lifecycle.
type IngestService struct {
	repo      *repository.IncomingRepository
	jobRepo   *repository.JobRepository
	queue     *queue.Client
	mediaRoot string
}

// NewIngestService creates an IngestService.
func NewIngestService(
	repo *repository.IncomingRepository,
	jobRepo *repository.JobRepository,
	q *queue.Client,
	mediaRoot string,
) *IngestService {
	return &IngestService{repo: repo, jobRepo: jobRepo, queue: q, mediaRoot: mediaRoot}
}

// Register upserts an incoming media item (idempotent by source_path).
func (s *IngestService) Register(ctx context.Context, req *model.RegisterIncomingRequest) (*model.IncomingItem, error) {
	if req.SourcePath == "" {
		return nil, fmt.Errorf("source_path is required")
	}
	if req.SourceFilename == "" {
		return nil, fmt.Errorf("source_filename is required")
	}
	if req.ContentKind == "" {
		req.ContentKind = "movie"
	}
	return s.repo.Register(ctx, req)
}

// Claim atomically claims up to limit 'new' items for processing.
func (s *IngestService) Claim(ctx context.Context, limit int, claimTTLSec int) ([]model.IncomingItem, error) {
	if limit <= 0 {
		limit = 3
	}
	if claimTTLSec <= 0 {
		claimTTLSec = 900
	}
	expiresAt := time.Now().Add(time.Duration(claimTTLSec) * time.Second)
	return s.repo.ClaimBatch(ctx, limit, expiresAt)
}

// Progress updates the status of a claimed item to "copying" or "copied".
func (s *IngestService) Progress(ctx context.Context, req *model.ProgressIncomingRequest) error {
	if req.ID == 0 {
		return fmt.Errorf("id is required")
	}
	if req.Status != "copying" && req.Status != "copied" {
		return fmt.Errorf("status must be \"copying\" or \"copied\"")
	}
	return s.repo.Progress(ctx, req)
}

// Fail marks a claimed item as failed or resets it to 'new' for retry.
func (s *IngestService) Fail(ctx context.Context, req *model.FailIncomingRequest, maxAttempts int) error {
	if req.ID == 0 {
		return fmt.Errorf("id is required")
	}
	return s.repo.Fail(ctx, req.ID, req.ErrorMessage, maxAttempts)
}

// Complete creates a media_job and pushes it to convert_queue, then marks
// the incoming item as completed. If the queue push fails, the item is left
// in 'copied' status so the worker can retry by calling Complete again.
func (s *IngestService) Complete(ctx context.Context, req *model.CompleteIncomingRequest) (*model.CompleteIncomingResponse, error) {
	if req.ID == 0 {
		return nil, fmt.Errorf("id is required")
	}
	if req.LocalPath == "" {
		return nil, fmt.Errorf("local_path is required")
	}

	item, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("get incoming item: %w", err)
	}

	// Deterministic job ID for idempotency.
	jobID := fmt.Sprintf("ingest-%d", req.ID)

	// Build the media_job row.
	job := &model.Job{
		JobID:       jobID,
		ContentType: item.ContentKind,
		SourceType:  "ingest",
		SourceRef:   item.SourcePath,
		Priority:    model.JobPriorityNormal,
		Status:      model.JobStatusQueued,
		RequestID:   &jobID,
	}
	if item.NormalizedName != nil {
		job.Title = item.NormalizedName
	}

	_, createErr := s.jobRepo.Create(ctx, job)
	if createErr != nil && !errors.Is(createErr, repository.ErrConflict) {
		return nil, fmt.Errorf("create job: %w", createErr)
	}

	// Build the convert_queue payload.
	outputPath := filepath.Join(s.mediaRoot, "converted", "movies")
	finalDir := fmt.Sprintf("ingest_%d", req.ID)
	if item.NormalizedName != nil {
		finalDir = *item.NormalizedName
	}
	tmdbID := ""
	if item.TMDBID != nil {
		tmdbID = *item.TMDBID
	}
	title := ""
	if item.NormalizedName != nil {
		title = *item.NormalizedName
	}

	payload := model.ConvertPayload{
		SchemaVersion: "1",
		JobID:         jobID,
		JobType:       "convert",
		ContentType:   item.ContentKind,
		CorrelationID: jobID,
		Attempt:       1,
		MaxAttempts:   3,
		CreatedAt:     time.Now(),
		Payload: model.ConvertJob{
			InputPath:     req.LocalPath,
			OutputPath:    outputPath,
			OutputProfile: "mp4_h264_aac_1080p",
			FinalDir:      finalDir,
			TMDBID:        tmdbID,
			Title:         title,
		},
	}

	if err := s.queue.Enqueue(ctx, queue.ConvertQueue, payload); err != nil {
		// Leave item in 'copied' status so the worker can retry Complete.
		return nil, fmt.Errorf("enqueue convert job: %w", err)
	}

	if err := s.repo.Complete(ctx, req.ID, jobID, req.LocalPath); err != nil {
		return nil, fmt.Errorf("mark incoming item complete: %w", err)
	}

	return &model.CompleteIncomingResponse{JobID: jobID}, nil
}
