package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

// Worker polls the scanner API for claimed ingest items, copies them via rclone,
// creates the convert job locally, then notifies the scanner.
type Worker struct {
	client       *Client
	puller       *Puller
	jobRepo      *repository.JobRepository
	queueClient  *queue.Client
	mediaRoot    string
	claimTTLSec  int
	pollInterval time.Duration
}

// New creates an IngestWorker.
func New(
	client *Client,
	puller *Puller,
	jobRepo *repository.JobRepository,
	queueClient *queue.Client,
	mediaRoot string,
	claimTTLSec int,
) *Worker {
	return &Worker{
		client:       client,
		puller:       puller,
		jobRepo:      jobRepo,
		queueClient:  queueClient,
		mediaRoot:    mediaRoot,
		claimTTLSec:  claimTTLSec,
		pollInterval: 10 * time.Second,
	}
}

// Run polls for items until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("ingest worker goroutine started")
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items, err := w.client.Claim(ctx, w.claimTTLSec)
			if err != nil {
				slog.Warn("ingest claim failed", "error", err)
				continue
			}
			if len(items) == 0 {
				continue
			}
			w.processItem(ctx, items[0])
		}
	}
}

func (w *Worker) processItem(ctx context.Context, item IncomingItem) {
	log := slog.With("ingest_id", item.ID, "source", item.SourcePath)

	destDir := filepath.Join(w.mediaRoot, "downloads", "ingest_"+strconv.FormatInt(item.ID, 10))

	if err := w.client.Progress(ctx, item.ID, "copying"); err != nil {
		log.Warn("progress update failed", "error", err)
	}

	localPath, err := w.puller.Copy(ctx, item.SourcePath, destDir)
	if err != nil {
		log.Error("rclone copy failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	if err := w.client.Progress(ctx, item.ID, "copied"); err != nil {
		log.Warn("progress update failed", "error", err)
	}

	// Create media_job in converter DB (previously done by converter API's Complete endpoint).
	jobID := fmt.Sprintf("ingest-%d", item.ID)
	contentKind := item.ContentKind
	if contentKind == "" {
		contentKind = "movie"
	}
	title := ""
	if item.NormalizedName != nil {
		title = *item.NormalizedName
	}

	if err := w.jobRepo.CreateForIngest(ctx, jobID, item.SourcePath, title, contentKind); err != nil {
		log.Error("create ingest job failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	// Build and push convert_queue message.
	finalDir := fmt.Sprintf("ingest_%d", item.ID)
	if item.NormalizedName != nil {
		finalDir = *item.NormalizedName
	}
	tmdbID := ""
	if item.TMDBID != nil {
		tmdbID = *item.TMDBID
	}
	outputPath := filepath.Join(w.mediaRoot, "converted", "movies")

	msg := model.ConvertMessage{
		SchemaVersion: "1",
		JobID:         jobID,
		JobType:       "convert",
		ContentType:   contentKind,
		CorrelationID: jobID,
		Attempt:       1,
		MaxAttempts:   3,
		CreatedAt:     time.Now(),
		Payload: model.ConvertJob{
			InputPath:     localPath,
			OutputPath:    outputPath,
			OutputProfile: "mp4_h264_aac_1080p",
			FinalDir:      finalDir,
			TMDBID:        tmdbID,
			Title:         title,
		},
	}

	if err := w.queueClient.Push(ctx, queue.ConvertQueue, msg); err != nil {
		log.Error("enqueue convert job failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	// Notify scanner that processing is complete — triggers move to library.
	if err := w.client.Complete(ctx, item.ID); err != nil {
		// Job is already enqueued — log but don't fail. Scanner will eventually
		// expire the claim and show the item as stuck.
		log.Warn("scanner complete notification failed (job already queued)", "error", err, "job_id", jobID)
		return
	}

	log.Info("ingest item processed", "job_id", jobID)
}
