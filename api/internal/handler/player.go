package handler

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/repository"
	"app/api/internal/service"
)

// PlayerHandler handles /api/player/* endpoints.
type PlayerHandler struct {
	jobSvc         *service.JobService
	assetRepo      *repository.AssetRepository
	movieRepo      *repository.MovieRepository
	subtitleRepo   *repository.SubtitleRepository
	storageLocRepo *repository.StorageLocationRepository
	mediaBaseURL   string
	mediaSigner    *mediaURLSigner
}

// NewPlayerHandler creates a PlayerHandler.
func NewPlayerHandler(
	jobSvc *service.JobService,
	assetRepo *repository.AssetRepository,
	movieRepo *repository.MovieRepository,
	subtitleRepo *repository.SubtitleRepository,
	storageLocRepo *repository.StorageLocationRepository,
	mediaBaseURL string,
	mediaSigningKey string,
	mediaSigningTTL time.Duration,
) *PlayerHandler {
	return &PlayerHandler{
		jobSvc:         jobSvc,
		assetRepo:      assetRepo,
		movieRepo:      movieRepo,
		subtitleRepo:   subtitleRepo,
		storageLocRepo: storageLocRepo,
		mediaBaseURL:   mediaBaseURL,
		mediaSigner:    newMediaURLSigner(mediaSigningKey, mediaSigningTTL),
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

	baseURL := h.resolveBaseURL(r.Context(), movie.storageLocationID)

	// Build subtitle list.
	subtitleTracks := []map[string]string{}
	if subs, err := h.subtitleRepo.ListByMovieID(r.Context(), movie.id); err == nil {
		for _, sub := range subs {
			subtitleTracks = append(subtitleTracks, map[string]string{
				"language": sub.Language,
				"url":      h.maybeSignMediaURL(buildMovieMediaURL(baseURL, movie.storageKey, "subtitles/"+sub.Language+".vtt")),
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
				"hls": h.maybeSignMediaURL(buildMovieMediaURL(baseURL, movie.storageKey, "master.m3u8")),
			},
			"assets": map[string]any{
				"poster": h.maybeSignMediaURL(buildMovieMediaURL(baseURL, movie.storageKey, "thumbnail.jpg")),
			},
			"subtitles": subtitleTracks,
		},
		"meta": map[string]any{
			"version": "v1",
		},
	})
}

// GetCatalog handles GET /api/player/catalog?since=...
func (h *PlayerHandler) GetCatalog(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var since *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"invalid since parameter: expected RFC 3339 format", false, cid)
			return
		}
		since = &t
	}

	ids, err := h.movieRepo.ListReadyTMDBIDs(r.Context(), since)
	if err != nil {
		slog.Error("catalog query failed", "error", err, "correlation_id", cid)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch catalog", false, cid)
		return
	}

	items := make([]map[string]string, len(ids))
	for i, id := range ids {
		items[i] = map[string]string{"tmdb_id": id}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

type repositoryMovieView struct {
	id                int64
	storageKey        string
	imdbID            *string
	tmdbID            *string
	storageLocationID *int64
}

func (h *PlayerHandler) getMovieByIMDbID(r *http.Request, imdbID string) (*repositoryMovieView, error) {
	m, err := h.movieRepo.GetByIMDbID(r.Context(), imdbID)
	if err != nil {
		return nil, err
	}
	return &repositoryMovieView{id: m.ID, storageKey: m.StorageKey, imdbID: m.IMDbID, tmdbID: m.TMDBID, storageLocationID: m.StorageLocationID}, nil
}

func (h *PlayerHandler) getMovieByTMDBID(r *http.Request, tmdbID string) (*repositoryMovieView, error) {
	m, err := h.movieRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		return nil, err
	}
	return &repositoryMovieView{id: m.ID, storageKey: m.StorageKey, imdbID: m.IMDbID, tmdbID: m.TMDBID, storageLocationID: m.StorageLocationID}, nil
}

// resolveBaseURL returns the appropriate media base URL for a movie.
// If the movie is on a remote storage location with a configured base_url, use that.
// Falls back to the global MEDIA_BASE_URL (covers local movies and remote movies
// whose domain is not yet configured).
func (h *PlayerHandler) resolveBaseURL(ctx context.Context, storageLocationID *int64) string {
	if storageLocationID != nil && *storageLocationID > 1 {
		loc, err := h.storageLocRepo.GetByID(ctx, *storageLocationID)
		if err == nil && loc.BaseURL != "" {
			return loc.BaseURL
		}
		// base_url empty = domain not yet configured; fall through to local
	}
	return h.mediaBaseURL
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

// ── P2P metrics ───────────────────────────────────────────────────────────────

// Atomic counters for P2P metrics (no external dependency needed).
var (
	p2pHTTPBytes     atomic.Int64
	p2pP2PBytes      atomic.Int64
	p2pHTTPSegments  atomic.Int64
	p2pP2PSegments   atomic.Int64
	p2pPeersSnapshot atomic.Int64
)

// P2PMetricsSnapshot returns current P2P counters (for /metrics or monitoring).
func P2PMetricsSnapshot() map[string]int64 {
	return map[string]int64{
		"p2p_http_bytes_total":     p2pHTTPBytes.Load(),
		"p2p_p2p_bytes_total":      p2pP2PBytes.Load(),
		"p2p_http_segments_total":  p2pHTTPSegments.Load(),
		"p2p_p2p_segments_total":   p2pP2PSegments.Load(),
		"p2p_peers_last_snapshot":  p2pPeersSnapshot.Load(),
	}
}

type p2pMetricsPayload struct {
	StreamID    string `json:"stream_id"`
	HTTPBytes   int64  `json:"http_bytes"`
	P2PBytes    int64  `json:"p2p_bytes"`
	HTTPSegments int64 `json:"http_segments"`
	P2PSegments int64  `json:"p2p_segments"`
	Peers       int64  `json:"peers"`
	WindowSec   int    `json:"window_sec"`
}

// PostP2PMetrics handles POST /api/player/p2p-metrics.
func (h *PlayerHandler) PostP2PMetrics(w http.ResponseWriter, r *http.Request) {
	var payload p2pMetricsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	p2pHTTPBytes.Add(payload.HTTPBytes)
	p2pP2PBytes.Add(payload.P2PBytes)
	p2pHTTPSegments.Add(payload.HTTPSegments)
	p2pP2PSegments.Add(payload.P2PSegments)
	p2pPeersSnapshot.Store(payload.Peers)

	slog.Debug("p2p metrics",
		"stream_id", payload.StreamID,
		"http_bytes", payload.HTTPBytes,
		"p2p_bytes", payload.P2PBytes,
		"peers", payload.Peers,
	)

	w.WriteHeader(http.StatusNoContent)
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
