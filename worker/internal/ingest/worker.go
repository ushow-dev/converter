package ingest

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"
)

// Worker polls the converter API for claimed ingest items, copies them via rclone,
// then calls complete which creates the convert job server-side.
type Worker struct {
	client       *Client
	puller       *Puller
	mediaRoot    string
	claimTTLSec  int
	pollInterval time.Duration
}

func New(client *Client, puller *Puller, mediaRoot string, claimTTLSec int) *Worker {
	return &Worker{
		client:       client,
		puller:       puller,
		mediaRoot:    mediaRoot,
		claimTTLSec:  claimTTLSec,
		pollInterval: 10 * time.Second,
	}
}

// Run polls for items until ctx is cancelled. Each call to Run handles one item at a time.
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

	jobID, err := w.client.Complete(ctx, item.ID, localPath)
	if err != nil {
		log.Error("complete call failed", "error", err)
		if failErr := w.client.Fail(ctx, item.ID, err.Error()); failErr != nil {
			log.Warn("fail update failed", "error", failErr)
		}
		return
	}

	log.Info("ingest item processed", "job_id", jobID)
}
