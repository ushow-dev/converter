package handler

import (
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/repository"
	"app/api/internal/service"
)

// PlayerHandler handles /api/player/* endpoints.
type PlayerHandler struct {
	jobSvc       *service.JobService
	assetRepo    *repository.AssetRepository
	movieRepo    *repository.MovieRepository
	subtitleRepo *repository.SubtitleRepository
	mediaBaseURL string
	mediaSigner  *mediaURLSigner
}

// NewPlayerHandler creates a PlayerHandler.
func NewPlayerHandler(
	jobSvc *service.JobService,
	assetRepo *repository.AssetRepository,
	movieRepo *repository.MovieRepository,
	subtitleRepo *repository.SubtitleRepository,
	mediaBaseURL string,
	mediaSigningKey string,
	mediaSigningTTL time.Duration,
) *PlayerHandler {
	return &PlayerHandler{
		jobSvc:       jobSvc,
		assetRepo:    assetRepo,
		movieRepo:    movieRepo,
		subtitleRepo: subtitleRepo,
		mediaBaseURL: mediaBaseURL,
		mediaSigner:  newMediaURLSigner(mediaSigningKey, mediaSigningTTL),
	}
}

// GetAsset handles GET /api/player/assets/{assetID}.
func (h *PlayerHandler) GetAsset(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	assetID := chi.URLParam(r, "assetID")

	asset, err := h.assetRepo.GetByID(r.Context(), assetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND",
				"asset not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch asset", false, cid)
		return
	}

	// Build playback info from actual storage_path to avoid divergence between
	// physical storage layout and API URL shape.
	playbackURL := h.maybeSignMediaURL(storagePathToPlaybackURL(asset.StoragePath))

	respondJSON(w, http.StatusOK, map[string]any{
		"asset_id":     asset.AssetID,
		"job_id":       asset.JobID,
		"content_type": "movie",
		"is_ready":     asset.IsReady,
		"playback": map[string]any{
			"mode": "url",
			"url":  playbackURL,
		},
		"media_info": map[string]any{
			"duration_sec": asset.DurationSec,
			"video_codec":  asset.VideoCodec,
			"audio_codec":  asset.AudioCodec,
		},
		"updated_at": asset.UpdatedAt,
	})
}

func storagePathToPlaybackURL(storagePath string) string {
	p := filepath.ToSlash(filepath.Clean(storagePath))
	if p == "." || p == "/" {
		return "/media"
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

// GetMovie handles GET /api/player/movie?imdb_id=...|tmdb_id=...
func (h *PlayerHandler) GetMovie(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	imdbID := strings.TrimSpace(r.URL.Query().Get("imdb_id"))
	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	if (imdbID == "" && tmdbID == "") || (imdbID != "" && tmdbID != "") {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"exactly one of imdb_id or tmdb_id must be provided", false, cid)
		return
	}

	var (
		movie *repositoryMovieView
		err   error
	)
	if imdbID != "" {
		movie, err = h.getMovieByIMDbID(r, imdbID)
	} else {
		movie, err = h.getMovieByTMDBID(r, tmdbID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "movie not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch movie", false, cid)
		return
	}

	// Build subtitle list.
	subtitleTracks := []map[string]string{}
	if subs, err := h.subtitleRepo.ListByMovieID(r.Context(), movie.id); err == nil {
		for _, sub := range subs {
			subtitleTracks = append(subtitleTracks, map[string]string{
				"language": sub.Language,
				"url":      h.maybeSignMediaURL(buildMovieMediaURL(h.mediaBaseURL, movie.storageKey, "subtitles/"+sub.Language+".vtt")),
			})
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"movie": map[string]any{
				"id":      movie.id,
				"imdb_id": movie.imdbID,
				"tmdb_id": movie.tmdbID,
			},
			"playback": map[string]any{
				"hls": h.maybeSignMediaURL(buildMovieMediaURL(h.mediaBaseURL, movie.storageKey, "master.m3u8")),
			},
			"assets": map[string]any{
				"poster": h.maybeSignMediaURL(buildMovieMediaURL(h.mediaBaseURL, movie.storageKey, "thumbnail.jpg")),
			},
			"subtitles": subtitleTracks,
		},
		"meta": map[string]any{
			"version": "v1",
		},
	})
}

type repositoryMovieView struct {
	id         int64
	storageKey string
	imdbID     *string
	tmdbID     *string
}

func (h *PlayerHandler) getMovieByIMDbID(r *http.Request, imdbID string) (*repositoryMovieView, error) {
	m, err := h.movieRepo.GetByIMDbID(r.Context(), imdbID)
	if err != nil {
		return nil, err
	}
	return &repositoryMovieView{id: m.ID, storageKey: m.StorageKey, imdbID: m.IMDbID, tmdbID: m.TMDBID}, nil
}

func (h *PlayerHandler) getMovieByTMDBID(r *http.Request, tmdbID string) (*repositoryMovieView, error) {
	m, err := h.movieRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		return nil, err
	}
	return &repositoryMovieView{id: m.ID, storageKey: m.StorageKey, imdbID: m.IMDbID, tmdbID: m.TMDBID}, nil
}

func buildMovieMediaURL(baseURL, storageKey, fileName string) string {
	relative := fmt.Sprintf("/movies/%s/%s", storageKey, fileName)
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return relative
	}
	return trimmed + relative
}

func (h *PlayerHandler) maybeSignMediaURL(rawURL string) string {
	if h.mediaSigner == nil {
		return rawURL
	}
	signedURL, err := h.mediaSigner.Sign(rawURL, time.Now().UTC())
	if err != nil {
		slog.Warn("failed to sign media url", "url", rawURL, "error", err)
		return rawURL
	}
	return signedURL
}

type mediaURLSigner struct {
	secret string
	ttl    time.Duration
}

func newMediaURLSigner(secret string, ttl time.Duration) *mediaURLSigner {
	secret = strings.TrimSpace(secret)
	if secret == "" || ttl <= 0 {
		return nil
	}
	return &mediaURLSigner{secret: secret, ttl: ttl}
}

func (s *mediaURLSigner) Sign(rawURL string, now time.Time) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse media url: %w", err)
	}

	if u.Path == "" {
		return "", fmt.Errorf("media url has empty path")
	}

	expires := now.Add(s.ttl).Unix()
	tokenPath := mediaSigningPath(u.Path)
	tokenBytes := md5.Sum([]byte(strconv.FormatInt(expires, 10) + tokenPath + s.secret))
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])

	query := u.Query()
	query.Set("st", token)
	query.Set("e", strconv.FormatInt(expires, 10))
	u.RawQuery = query.Encode()

	return u.String(), nil
}

func mediaSigningPath(path string) string {
	normalized := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}

	parts := strings.Split(strings.TrimPrefix(normalized, "/"), "/")
	// For HLS requests under /<content_type>/<storage_key>/..., bind token to
	// the storage key directory so nested playlists/segments share one signature.
	// Supported content types: movies, serials, tv.
	if len(parts) >= 2 &&
		(parts[0] == "movies" || parts[0] == "serials" || parts[0] == "tv") &&
		parts[1] != "" &&
		(parts[len(parts)-1] == "master.m3u8" ||
			strings.HasSuffix(parts[len(parts)-1], ".m3u8") ||
			strings.HasSuffix(parts[len(parts)-1], ".ts")) {
		return "/" + parts[0] + "/" + parts[1] + "/"
	}

	return normalized
}

// GetJobStatus handles GET /api/player/jobs/{jobID}/status.
func (h *PlayerHandler) GetJobStatus(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	jobID := chi.URLParam(r, "jobID")

	job, err := h.jobSvc.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND",
				"job not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch job", false, cid)
		return
	}

	isReady := job.Status == "completed"
	var assetID *string
	if isReady {
		if asset, err := h.assetRepo.GetByJobID(r.Context(), jobID); err == nil {
			assetID = &asset.AssetID
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"job_id":     job.JobID,
		"status":     string(job.Status),
		"is_ready":   isReady,
		"asset_id":   assetID,
		"updated_at": job.UpdatedAt,
	})
}
