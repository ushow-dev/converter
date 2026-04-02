package handler

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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
	seriesRepo     *repository.SeriesRepository
	audioTrackRepo *repository.AudioTrackRepository
	epSubtitleRepo *repository.EpisodeSubtitleRepository
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
	seriesRepo *repository.SeriesRepository,
	audioTrackRepo *repository.AudioTrackRepository,
	epSubtitleRepo *repository.EpisodeSubtitleRepository,
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
		seriesRepo:     seriesRepo,
		audioTrackRepo: audioTrackRepo,
		epSubtitleRepo: epSubtitleRepo,
		mediaBaseURL:   mediaBaseURL,
		mediaSigner:    newMediaURLSigner(mediaSigningKey, mediaSigningTTL),
	}
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

// buildMediaURL joins baseURL with a relative path, handling empty base URL.
func buildMediaURL(baseURL, relativePath string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return "/" + relativePath
	}
	return trimmed + "/" + relativePath
}

// buildAudioTracksPayload fetches and formats audio tracks for any asset type.
func (h *PlayerHandler) buildAudioTracksPayload(ctx context.Context, assetID, assetType string) []map[string]any {
	tracks, err := h.audioTrackRepo.ListByAsset(ctx, assetID, assetType)
	if err != nil || len(tracks) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tracks))
	for i, t := range tracks {
		td := map[string]any{"index": t.TrackIndex, "is_default": t.IsDefault}
		if t.Language != nil {
			td["language"] = *t.Language
		}
		if t.Label != nil {
			td["label"] = *t.Label
		}
		result[i] = td
	}
	return result
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
	// Supported content types: movies, series.
	if len(parts) >= 2 &&
		(parts[0] == "movies" || parts[0] == "series") &&
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
		"p2p_http_bytes_total":    p2pHTTPBytes.Load(),
		"p2p_p2p_bytes_total":     p2pP2PBytes.Load(),
		"p2p_http_segments_total": p2pHTTPSegments.Load(),
		"p2p_p2p_segments_total":  p2pP2PSegments.Load(),
		"p2p_peers_last_snapshot": p2pPeersSnapshot.Load(),
	}
}

type p2pMetricsPayload struct {
	StreamID     string `json:"stream_id"`
	HTTPBytes    int64  `json:"http_bytes"`
	P2PBytes     int64  `json:"p2p_bytes"`
	HTTPSegments int64  `json:"http_segments"`
	P2PSegments  int64  `json:"p2p_segments"`
	Peers        int64  `json:"peers"`
	WindowSec    int    `json:"window_sec"`
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
