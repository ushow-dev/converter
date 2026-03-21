package httpdownloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"app/worker/internal/cancelregistry"
	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
	"golang.org/x/net/proxy"
)

// Worker consumes remote_download_queue and downloads video files via HTTP.
// On completion it pushes the job to convert_queue.
type Worker struct {
	q         *queue.Client
	jobRepo   *repository.JobRepository
	mediaRoot string
	registry  *cancelregistry.Registry
}

// New creates an HTTP download Worker.
func New(q *queue.Client, jobRepo *repository.JobRepository, mediaRoot string, registry *cancelregistry.Registry) *Worker {
	return &Worker{
		q:         q,
		jobRepo:   jobRepo,
		mediaRoot: mediaRoot,
		registry:  registry,
	}
}

// Run starts the BLPOP consumer loop. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("http download worker started")
	for {
		if ctx.Err() != nil {
			slog.Info("http download worker stopped")
			return
		}
		raw, err := w.q.Pop(ctx, queue.RemoteDownloadQueue, 5*time.Second)
		if errors.Is(err, queue.ErrEmpty) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("remote download queue pop error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		w.process(ctx, raw)
	}
}

func (w *Worker) process(ctx context.Context, raw []byte) {
	var msg model.RemoteDownloadMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Error("unmarshal remote download message", "error", err)
		return
	}
	log := slog.With("job_id", msg.JobID, "correlation_id", msg.CorrelationID)

	if terminal, err := w.jobRepo.IsTerminal(ctx, msg.JobID); err != nil || terminal {
		log.Info("skipping terminal job")
		return
	}

	if msg.Attempt > msg.MaxAttempts {
		log.Warn("max attempts exceeded", "attempt", msg.Attempt)
		_ = w.jobRepo.SetFailed(ctx, msg.JobID, "MAX_ATTEMPTS_EXCEEDED",
			fmt.Sprintf("exceeded %d attempts", msg.MaxAttempts), false)
		return
	}

	locked, err := w.q.AcquireLock(ctx, msg.JobID)
	if err != nil {
		log.Error("acquire lock", "error", err)
		return
	}
	if !locked {
		log.Info("job already locked, skipping")
		return
	}
	defer w.q.ReleaseLock(ctx, msg.JobID)

	// Per-job cancellable context. ReleaseLock above captures global ctx intentionally
	// so the lock is released even when jobCtx is cancelled.
	jobCtx, jobCancel := context.WithCancel(ctx)
	w.registry.Register(msg.JobID, jobCancel)
	defer func() {
		jobCancel()
		w.registry.Unregister(msg.JobID)
	}()

	stage := model.StageDownload
	if err := w.jobRepo.UpdateStatus(jobCtx, msg.JobID, model.StatusInProgress, &stage, 0); err != nil {
		log.Error("update status to in_progress", "error", err)
		return
	}

	targetDir := msg.Payload.TargetDir
	if targetDir == "" {
		targetDir = filepath.Join(w.mediaRoot, "downloads", msg.JobID)
	}
	if err := os.MkdirAll(targetDir, 0o777); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create target dir: "+err.Error(), false)
		return
	}

	filename := msg.Payload.Filename
	if filename == "" {
		filename = "video.mkv"
	}
	destPath := filepath.Join(targetDir, filename)

	log.Info("starting HTTP download", "url", msg.Payload.SourceURL, "dest", destPath)

	client := buildHTTPClient(msg.Payload.ProxyConfig)
	if err := w.downloadWithProgress(jobCtx, client, msg.JobID, msg.Payload.SourceURL, destPath, log); err != nil {
		if jobCtx.Err() != nil {
			// Cancelled (job deleted) — abort cleanly without retry.
			log.Info("download cancelled", "job_id", msg.JobID)
			_ = os.Remove(destPath)
			return
		}
		w.failOrRequeue(ctx, msg, "DOWNLOAD_ERROR", err.Error(), true)
		return
	}

	log.Info("download complete", "path", destPath)

	// Transition to convert stage.
	stageConvert := model.StageConvert
	if err := w.jobRepo.UpdateStatus(jobCtx, msg.JobID, model.StatusInProgress, &stageConvert, 0); err != nil {
		log.Error("update status to convert stage", "error", err)
	}

	outputPath := filepath.Join(w.mediaRoot, "temp", msg.JobID)
	convertMsg := model.ConvertMessage{
		SchemaVersion: "v1",
		JobID:         msg.JobID,
		JobType:       "convert",
		ContentType:   msg.ContentType,
		CorrelationID: msg.CorrelationID,
		Attempt:       1,
		MaxAttempts:   msg.MaxAttempts,
		CreatedAt:     time.Now().UTC(),
		Payload: model.ConvertJob{
			InputPath:     destPath,
			OutputPath:    outputPath,
			OutputProfile: "mp4_h264_aac_1080p",
			FinalDir:      filepath.Join(w.mediaRoot, "converted"),
			IMDbID:        msg.Payload.IMDbID,
			TMDBID:        msg.Payload.TMDBID,
			Title:         msg.Payload.Title,
			StorageKey:    msg.Payload.StorageKey,
		},
	}
	if err := w.q.Push(jobCtx, queue.ConvertQueue, convertMsg); err != nil {
		log.Error("enqueue convert job", "error", err)
	}
	log.Info("convert job enqueued")
}

// buildHTTPClient returns an *http.Client configured to use the given proxy settings.
// If cfg is nil, disabled, or has no host, it returns a plain client with no global timeout.
func buildHTTPClient(cfg *model.ProxyConfig) *http.Client {
	if cfg == nil || !cfg.Enabled || cfg.Host == "" {
		return &http.Client{} // no global timeout — downloads can take a long time
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	switch strings.ToUpper(cfg.Type) {
	case "SOCKS5":
		var auth *proxy.Auth
		if cfg.Username != "" {
			auth = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
		if err != nil {
			break
		}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			return &http.Client{
				Transport: &http.Transport{DialContext: cd.DialContext},
			}
		}
	case "HTTP":
		var userInfo *url.Userinfo
		if cfg.Username != "" {
			userInfo = url.UserPassword(cfg.Username, cfg.Password)
		}
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   addr,
			User:   userInfo,
		}
		return &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}
	}

	return &http.Client{}
}

// downloadWithProgress streams a remote URL to destPath, reporting progress to the DB.
func (w *Worker) downloadWithProgress(
	ctx context.Context,
	client *http.Client,
	jobID, sourceURL, destPath string,
	log *slog.Logger,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d from server", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dest file: %w", err)
	}
	defer f.Close()

	total := resp.ContentLength // -1 if unknown
	pr := &progressReader{
		r:          resp.Body,
		total:      total,
		lastReport: -1,
		onProgress: func(pct int) {
			_ = w.jobRepo.UpdateProgress(ctx, jobID, pct)
			log.Info("download progress", "pct", pct)
		},
	}

	if _, err := io.Copy(f, pr); err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// progressReader wraps an io.Reader to track download progress.
type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	lastReport int
	onProgress func(int)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.read += int64(n)
		if p.total > 0 {
			pct := int(float64(p.read) / float64(p.total) * 100)
			if pct != p.lastReport && pct%2 == 0 { // report every 2%
				p.lastReport = pct
				p.onProgress(pct)
			}
		}
	}
	return n, err
}

func (w *Worker) failJob(ctx context.Context, msg model.RemoteDownloadMessage, code, message string, retryable bool) {
	slog.Error("http download failed", "job_id", msg.JobID, "code", code, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, retryable)
}

func (w *Worker) failOrRequeue(ctx context.Context, msg model.RemoteDownloadMessage, code, message string, retryable bool) {
	if !retryable || msg.Attempt >= msg.MaxAttempts {
		w.failJob(ctx, msg, code, message, false)
		return
	}
	slog.Warn("http download error, will retry",
		"job_id", msg.JobID, "attempt", msg.Attempt, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, true)

	msg.Attempt++
	delay := backoffDelay(msg.Attempt)
	time.Sleep(delay)
	if err := w.q.Push(ctx, queue.RemoteDownloadQueue, msg); err != nil {
		slog.Error("requeue failed", "job_id", msg.JobID, "error", err)
	}
}

func backoffDelay(attempt int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > 5*time.Minute {
			return 5 * time.Minute
		}
	}
	return d
}
