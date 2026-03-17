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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"app/worker/internal/ffmpeg"
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
	ffmpegThreads   int // 0 = auto
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
) *Worker {
	return &Worker{
		q: q, jobRepo: jobRepo, assetRepo: assetRepo, movieRepo: movieRepo,
		subtitleFetcher: subtitleFetcher, subtitleRepo: subtitleRepo,
		mediaRoot: mediaRoot, tmdbAPIKey: tmdbAPIKey, ffmpegThreads: ffmpegThreads,
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

	// ── Preserve original source file inside temp output dir ─────────────────
	// Moving it here (before the Rename below) carries it atomically to finalDir.
	// RemoveAll(downloadsDir) later won't touch it since it's already moved out.
	if src := msg.Payload.InputPath; src != "" {
		destName := buildSourceFilename(movie, msg.Payload.Title, msg.Payload.TMDBID, filepath.Ext(src))
		if err := os.Rename(src, filepath.Join(outputDir, destName)); err != nil {
			log.Warn("could not preserve source file", "src", src, "error", err)
		} else {
			log.Info("source file staged for final dir", "name", destName)
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

	// Mark job as completed.
	if err := w.jobRepo.UpdateStatus(ctx, msg.JobID, model.StatusCompleted, &stage, 100); err != nil {
		log.Error("update status to completed", "error", err)
	}

	// Best-effort cleanup of original downloaded torrent data on successful convert.
	downloadsDir := filepath.Join(w.mediaRoot, "downloads", msg.JobID)
	if err := os.RemoveAll(downloadsDir); err != nil {
		log.Warn("cleanup downloads dir failed", "path", downloadsDir, "error", err)
	}

	log.Info("job completed", "asset_id", assetID, "master", masterPath)

	// ── Subtitle fetch (best-effort, non-fatal) ───────────────────────────────
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

// ─── source file naming ───────────────────────────────────────────────────────

var reYear = regexp.MustCompile(`\((\d{4})\)`)

// buildSourceFilename constructs a clean, filesystem-safe name for the preserved
// original video file: title_year_tmdbID.ext  (e.g. inception_2010_tmdb27205.mkv).
func buildSourceFilename(movie *model.Movie, fallbackTitle, fallbackTMDB, ext string) string {
	title := fallbackTitle
	if movie.Title != nil && *movie.Title != "" {
		title = *movie.Title
	}

	tmdbID := fallbackTMDB
	if movie.TMDBID != nil && *movie.TMDBID != "" {
		tmdbID = *movie.TMDBID
	}

	// Try year from DB; if absent, parse from title string (e.g. "Inception (2010)").
	var year int
	if movie.Year != nil {
		year = *movie.Year
	} else if m := reYear.FindStringSubmatch(title); m != nil {
		if y, err := strconv.Atoi(m[1]); err == nil {
			year = y
		}
		// Strip "(year)" from the title so it doesn't appear twice.
		title = strings.TrimSpace(reYear.ReplaceAllString(title, ""))
	}

	norm := normalizeFilenameSegment(title)
	if norm == "" {
		norm = "source"
	}

	var name string
	switch {
	case year > 0 && tmdbID != "":
		name = fmt.Sprintf("%s_%d_%s", norm, year, tmdbID)
	case year > 0:
		name = fmt.Sprintf("%s_%d", norm, year)
	case tmdbID != "":
		name = fmt.Sprintf("%s_%s", norm, tmdbID)
	default:
		name = norm
	}

	return name + ext
}

// normalizeFilenameSegment lowercases s and replaces any run of non-alphanumeric
// characters with a single underscore, trimming leading/trailing underscores.
func normalizeFilenameSegment(s string) string {
	var b strings.Builder
	inSep := true // start true to trim leading separators
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			inSep = false
		} else if !inSep {
			b.WriteByte('_')
			inSep = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}
