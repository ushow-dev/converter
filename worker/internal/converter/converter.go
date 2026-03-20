package converter

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"app/worker/internal/ffmpeg"
	"app/worker/internal/ingest"
	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
	"app/worker/internal/subtitles"
)

// Worker consumes convert_queue and orchestrates HLS conversions.
type Worker struct {
	q               *queue.Client
	jobRepo         *repository.JobRepository
	assetRepo       *repository.AssetRepository
	movieRepo       *repository.MovieRepository
	subtitleFetcher *subtitles.Fetcher // nil if OpenSubtitles not configured
	subtitleRepo    *repository.SubtitleRepository
	mediaRoot       string
	tmdbAPIKey      string
	ffmpegThreads   int  // 0 = auto
	transferEnabled bool // true if remote transfer is configured
	// Archive-to-scanner: copy original file to scanner server after conversion.
	// Enabled only for non-ingest jobs when scannerClient and ingestSourceRemote are set.
	scannerClient      *ingest.Client // nil if archive disabled
	ingestSourceRemote string         // rclone remote name for scanner SFTP
	archiveDestPath    string         // destination path on scanner, e.g. /library/movies
}

// New creates a convert Worker.
func New(
	q *queue.Client,
	jobRepo *repository.JobRepository,
	assetRepo *repository.AssetRepository,
	movieRepo *repository.MovieRepository,
	subtitleFetcher *subtitles.Fetcher,
	subtitleRepo *repository.SubtitleRepository,
	mediaRoot string,
	tmdbAPIKey string,
	ffmpegThreads int,
	transferEnabled bool,
	scannerClient *ingest.Client,
	ingestSourceRemote string,
	archiveDestPath string,
) *Worker {
	return &Worker{
		q: q, jobRepo: jobRepo, assetRepo: assetRepo, movieRepo: movieRepo,
		subtitleFetcher: subtitleFetcher, subtitleRepo: subtitleRepo,
		mediaRoot: mediaRoot, tmdbAPIKey: tmdbAPIKey, ffmpegThreads: ffmpegThreads,
		transferEnabled:    transferEnabled,
		scannerClient:      scannerClient,
		ingestSourceRemote: ingestSourceRemote,
		archiveDestPath:    archiveDestPath,
	}
}

// Run starts the BLPOP consumer loop. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("convert worker started")
	for {
		if ctx.Err() != nil {
			slog.Info("convert worker stopped")
			return
		}
		raw, err := w.q.Pop(ctx, queue.ConvertQueue, 5*time.Second)
		if errors.Is(err, queue.ErrEmpty) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("convert queue pop error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		w.process(ctx, raw)
	}
}

func (w *Worker) process(ctx context.Context, raw []byte) {
	var msg model.ConvertMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Error("unmarshal convert message", "error", err)
		return
	}
	log := slog.With("job_id", msg.JobID, "correlation_id", msg.CorrelationID)

	// Guard: skip already-terminal jobs.
	if terminal, err := w.jobRepo.IsTerminal(ctx, msg.JobID); err != nil || terminal {
		log.Info("skipping terminal job")
		return
	}

	// Guard: max attempts exceeded.
	if msg.Attempt > msg.MaxAttempts {
		log.Warn("max attempts exceeded", "attempt", msg.Attempt)
		_ = w.jobRepo.SetFailed(ctx, msg.JobID, "MAX_ATTEMPTS_EXCEEDED",
			fmt.Sprintf("exceeded %d convert attempts", msg.MaxAttempts), false)
		return
	}

	// Distributed lock.
	lockKey := msg.JobID + "_convert"
	locked, err := w.q.AcquireLock(ctx, lockKey)
	if err != nil {
		log.Error("acquire lock", "error", err)
		return
	}
	if !locked {
		log.Info("convert job already locked, skipping")
		return
	}
	defer w.q.ReleaseLock(ctx, lockKey)

	stage := model.StageConvert
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusInProgress, &stage, 0); err != nil {
		log.Error("update status to in_progress", "error", err)
		return
	}

	inputPath := msg.Payload.InputPath
	outputDir := msg.Payload.OutputPath // temp HLS working directory

	log.Info("starting HLS convert", "input", inputPath, "output_dir", outputDir)

	// Clean up any leftover from a previous attempt.
	_ = os.RemoveAll(outputDir)

	// Prepare temp output dir.
	if err := os.MkdirAll(outputDir, 0o777); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create output dir: "+err.Error(), false)
		return
	}
	_ = os.Chmod(outputDir, 0o777)

	// ── HLS encode ───────────────────────────────────────────────────────────
	start := time.Now()
	result, err := ffmpeg.RunHLS(ctx, inputPath, outputDir, 4, w.ffmpegThreads, func(pct int) {
		_ = w.jobRepo.UpdateProgress(ctx, msg.JobID, pct)
		log.Info("convert progress", "pct", pct)
	})
	if err != nil {
		w.failOrRequeue(ctx, msg, "FFMPEG_ERROR", err.Error(), false)
		return
	}
	log.Info("HLS encode done", "duration_s", time.Since(start).Seconds())

	// ── Thumbnail ─────────────────────────────────────────────────────────────
	thumbSrc := outputDir + "/thumbnail.jpg"
	if err := ffmpeg.Thumbnail(ctx, inputPath, thumbSrc, 600); err != nil {
		// Non-fatal: log and continue without thumbnail.
		log.Warn("thumbnail extraction failed", "error", err)
		thumbSrc = ""
	}

	// ── Fetch TMDB metadata (backdrop + year + poster) ───────────────────────
	var tmdbMeta *tmdbMetadata
	if w.tmdbAPIKey != "" && msg.Payload.TMDBID != "" {
		meta, err := fetchTMDBMetadata(ctx, w.tmdbAPIKey, msg.Payload.TMDBID)
		if err != nil {
			log.Warn("TMDB metadata fetch failed", "error", err)
		} else {
			tmdbMeta = meta
			if meta.BackdropPath != "" {
				backdropDest := outputDir + "/thumbnail.jpg"
				if err := downloadImage(ctx, "https://image.tmdb.org/t/p/w1280"+meta.BackdropPath, backdropDest); err != nil {
					log.Warn("TMDB backdrop download failed, keeping ffmpeg thumbnail", "error", err)
				} else {
					log.Info("TMDB backdrop saved", "tmdb_id", msg.Payload.TMDBID)
					thumbSrc = backdropDest
				}
			}
		}
	}

	// ── Create movie row and derive final directory ───────────────────────────
	var upsertYear *int
	var upsertPoster *string
	if tmdbMeta != nil {
		if tmdbMeta.Year > 0 {
			upsertYear = &tmdbMeta.Year
		}
		if tmdbMeta.PosterPath != "" {
			p := "https://image.tmdb.org/t/p/w500" + tmdbMeta.PosterPath
			upsertPoster = &p
		}
	}
	movie, err := w.movieRepo.Upsert(ctx, msg.Payload.IMDbID, msg.Payload.TMDBID, msg.Payload.Title, upsertYear, upsertPoster)
	if err != nil {
		w.failJob(ctx, msg, "DB_ERROR", "create movie record: "+err.Error(), false)
		return
	}
	finalDir := filepath.Join(w.mediaRoot, "converted", "movies", movie.StorageKey)

	// ── Archive or delete original source file ───────────────────────────────
	// For ingest jobs the source is already on the scanner server — just delete local copy.
	// For remote/torrent jobs, copy to scanner first then delete local copy.
	if src := msg.Payload.InputPath; src != "" {
		isIngestJob := strings.HasPrefix(msg.JobID, "ingest-")
		if !isIngestJob && w.scannerClient != nil && w.ingestSourceRemote != "" {
			if err := w.archiveToScanner(ctx, log, src, movie, msg.Payload.TMDBID, msg.Payload.IMDbID, tmdbMeta); err != nil {
				log.Warn("archive to scanner failed, deleting locally instead", "error", err)
				if err2 := os.Remove(src); err2 != nil && !os.IsNotExist(err2) {
					log.Warn("could not delete source file", "src", src, "error", err2)
				}
			}
			// archiveToScanner deletes the local file on success.
		} else {
			if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
				log.Warn("could not delete source file", "src", src, "error", err)
			} else {
				log.Info("source file deleted", "src", src)
			}
		}
	}

	// ── Move temp → final ─────────────────────────────────────────────────────
	// Remove stale final dir from a previous attempt if present.
	_ = os.RemoveAll(finalDir)
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o777); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "create parent of final dir: "+err.Error(), false)
		return
	}
	if err := os.Rename(outputDir, finalDir); err != nil {
		w.failJob(ctx, msg, "IO_ERROR", "move to final dir: "+err.Error(), false)
		return
	}
	log.Info("HLS files moved to final dir", "path", finalDir)

	masterPath := filepath.Join(finalDir, "master.m3u8")
	var thumbFinalPath *string
	if thumbSrc != "" {
		// Thumbnail was written inside outputDir which was renamed to finalDir.
		p := filepath.Join(finalDir, "thumbnail.jpg")
		thumbFinalPath = &p
	}

	// ── Probe accurate duration from master.m3u8's first variant ─────────────
	durationSec := result.DurationSec
	if probed := ffmpeg.ProbeInfo(ctx, filepath.Join(finalDir, "720", "index.m3u8")); probed > 0 {
		durationSec = probed
	}

	// ── Create asset record ───────────────────────────────────────────────────
	now := time.Now().UTC()
	assetID := generateAssetID()
	videoCodec := "h264"
	audioCodec := "aac"

	asset := &model.Asset{
		AssetID:       assetID,
		JobID:         msg.JobID,
		MovieID:       &movie.ID,
		StoragePath:   masterPath,
		ThumbnailPath: thumbFinalPath,
		DurationSec:   &durationSec,
		VideoCodec:    &videoCodec,
		AudioCodec:    &audioCodec,
		IsReady:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := w.assetRepo.Create(ctx, asset); err != nil {
		log.Error("create asset record", "error", err)
		// Non-fatal.
	}

	// Best-effort cleanup of original downloaded torrent data on successful convert.
	downloadsDir := filepath.Join(w.mediaRoot, "downloads", msg.JobID)
	if err := os.RemoveAll(downloadsDir); err != nil {
		log.Error("cleanup downloads dir failed", "path", downloadsDir, "error", err)
	}

	log.Info("job completed", "asset_id", assetID, "master", masterPath)

	// ── Subtitle fetch (best-effort, non-fatal) ───────────────────────────────
	// Must run BEFORE transfer enqueue to avoid race: rclone move may start
	// while subtitle files are still being written to finalDir.
	if w.subtitleFetcher != nil && msg.Payload.TMDBID != "" {
		results := w.subtitleFetcher.FetchAndSave(ctx, msg.Payload.TMDBID, finalDir)
		for _, sub := range results {
			extID := &sub.ExternalID
			if sub.ExternalID == "" {
				extID = nil
			}
			if err := w.subtitleRepo.Upsert(ctx, movie.ID, sub.Language, "opensubtitles", sub.FilePath, extID); err != nil {
				log.Warn("subtitle upsert failed", "lang", sub.Language, "error", err)
			}
		}
		log.Info("subtitles fetched", "count", len(results))
	}

	// ── Mark job completed or hand off to transfer ────────────────────────────
	if w.transferEnabled {
		// Keep job in_progress; transfer worker will mark it completed.
		if err := w.jobRepo.SetStageAndProgress(ctx, msg.JobID, model.StageTransfer, 0); err != nil {
			log.Error("set transfer stage failed", "error", err)
		}

		tfMsg := model.TransferMessage{
			SchemaVersion: "1",
			JobID:         msg.JobID,
			CorrelationID: msg.CorrelationID,
			CreatedAt:     time.Now().UTC(),
			Payload: model.TransferJob{
				MovieID:    movie.ID,
				StorageKey: movie.StorageKey,
				LocalPath:  finalDir,
			},
		}
		if err := w.q.Push(ctx, queue.TransferQueue, tfMsg); err != nil {
			log.Error("enqueue transfer failed", "error", err)
			// Non-fatal: mark completed locally so the job doesn't stay stuck.
			if err2 := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err2 != nil {
				log.Error("fallback complete failed", "error", err2)
			}
		} else {
			log.Info("transfer job enqueued", "movie_id", movie.ID)
		}
	} else {
		// No transfer configured: mark job completed immediately.
		if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err != nil {
			log.Error("update status to completed", "error", err)
		}
	}
}

// archiveToScanner copies the source file to scanner server's library via rclone and
// upserts it into scanner_library_movies. Deletes the local copy on success.
func (w *Worker) archiveToScanner(
	ctx context.Context,
	log *slog.Logger,
	src string,
	movie *model.Movie,
	tmdbID string,
	imdbID string,
	tmdbMeta *tmdbMetadata,
) error {
	filename := filepath.Base(src)

	// Get file size before any operations.
	var fileSizeBytes int64
	if info, err := os.Stat(src); err == nil {
		fileSizeBytes = info.Size()
	}

	// Remote destination: {remote}:{archiveDestPath}/{storageKey}/
	remoteDir := fmt.Sprintf("%s:%s/%s", w.ingestSourceRemote, w.archiveDestPath, movie.StorageKey)
	args := []string{"copy", src, remoteDir, "--progress", "--stats-one-line", "--stats=5s"}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Info("rclone archive to scanner library", "src", src, "dest", remoteDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone copy to scanner: %w", err)
	}

	// Delete local copy after successful transfer.
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		log.Warn("could not delete local source after archive", "error", err)
	} else {
		log.Info("source file archived and deleted locally", "src", src)
	}

	// Build archive request for scanner_library_movies.
	relPath := fmt.Sprintf("%s/%s", movie.StorageKey, filename)
	title := movie.StorageKey // fallback
	if movie.Title != nil && *movie.Title != "" {
		title = *movie.Title
	} else if tmdbMeta != nil && tmdbMeta.Title != "" {
		title = tmdbMeta.Title
	}
	year := 0
	if movie.Year != nil {
		year = *movie.Year
	} else if tmdbMeta != nil {
		year = tmdbMeta.Year
	}
	qualScore, qualLabel := parseQuality(filename)

	archReq := ingest.ArchiveRequest{
		NormalizedName:      movie.StorageKey,
		LibraryRelativePath: relPath,
		Title:               title,
		TMDBID:              tmdbID,
		IMDbID:              imdbID,
		Year:                year,
		QualityScore:        qualScore,
		QualityLabel:        qualLabel,
		FileSizeBytes:       fileSizeBytes,
	}

	// Register in scanner DB (best-effort: failure does not abort the job).
	if _, err := w.scannerClient.Archive(ctx, archReq); err != nil {
		log.Warn("scanner library registration failed (file is on scanner)", "error", err)
	} else {
		log.Info("scanner library movie registered", "normalized_name", movie.StorageKey)
	}
	return nil
}

// parseQuality extracts a numeric quality score and label from a filename.
// Looks for common resolution markers: 2160p/4K → 2160, 1080p → 1080, 720p → 720, etc.
func parseQuality(filename string) (score int, label string) {
	lower := strings.ToLower(filename)
	switch {
	case strings.Contains(lower, "2160p") || strings.Contains(lower, "4k") || strings.Contains(lower, "uhd"):
		return 2160, "HD"
	case strings.Contains(lower, "1080p") || strings.Contains(lower, "1080i"):
		return 1080, "HD"
	case strings.Contains(lower, "720p") || strings.Contains(lower, "720i"):
		return 720, "HD"
	case strings.Contains(lower, "480p"):
		return 480, "SD"
	case strings.Contains(lower, "360p"):
		return 360, "SD"
	default:
		return 0, ""
	}
}

// failJob marks the job as permanently failed.
func (w *Worker) failJob(ctx context.Context, msg model.ConvertMessage, code, message string, retryable bool) {
	slog.Error("convert failed", "job_id", msg.JobID, "code", code, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, retryable)
}

// failOrRequeue marks failed or re-enqueues if attempts remain.
func (w *Worker) failOrRequeue(ctx context.Context, msg model.ConvertMessage, code, message string, retryable bool) {
	if !retryable || msg.Attempt >= msg.MaxAttempts {
		w.failJob(ctx, msg, code, message, false)
		return
	}
	slog.Warn("convert error, will retry",
		"job_id", msg.JobID, "attempt", msg.Attempt, "error", message)
	_ = w.jobRepo.SetFailed(ctx, msg.JobID, code, message, true)

	msg.Attempt++
	delay := backoffDelay(msg.Attempt)
	time.Sleep(delay)
	if err := w.q.Push(ctx, queue.ConvertQueue, msg); err != nil {
		slog.Error("requeue convert failed", "job_id", msg.JobID, "error", err)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

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

func generateAssetID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("asset_%x", b)
}

type tmdbMetadata struct {
	Year         int
	Title        string
	BackdropPath string
	PosterPath   string
}

// fetchTMDBMetadata fetches movie details from TMDB and returns metadata including
// year, title, backdrop_path, and poster_path.
func fetchTMDBMetadata(ctx context.Context, apiKey, tmdbID string) (*tmdbMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s", tmdbID, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB details returned HTTP %d", resp.StatusCode)
	}

	var details struct {
		Title        string `json:"title"`
		ReleaseDate  string `json:"release_date"`
		BackdropPath string `json:"backdrop_path"`
		PosterPath   string `json:"poster_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}

	meta := &tmdbMetadata{
		Title:        details.Title,
		BackdropPath: details.BackdropPath,
		PosterPath:   details.PosterPath,
	}
	if len(details.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(details.ReleaseDate[:4]); err == nil {
			meta.Year = y
		}
	}
	return meta, nil
}

// downloadImage downloads an image from url and saves it to destPath.
func downloadImage(ctx context.Context, url, destPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

