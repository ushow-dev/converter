package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/api/internal/auth"
	"app/api/internal/repository"
)

type repositoryEpisodeView struct {
	episodeID        int64
	seasonID         int64
	seasonNumber     int
	episodeNumber    int
	title            *string
	storageKey       string
	seriesStorageKey string
}

// GetSeries handles GET /api/player/series?tmdb_id=...
func (h *PlayerHandler) GetSeries(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	if tmdbID == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"tmdb_id is required", false, cid)
		return
	}

	series, err := h.seriesRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch series", false, cid)
		return
	}

	baseURL := h.mediaBaseURL

	seasons, err := h.seriesRepo.ListSeasons(r.Context(), series.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch seasons", false, cid)
		return
	}

	seasonsPayload := []map[string]any{}
	for _, season := range seasons {
		episodes, err := h.seriesRepo.ListEpisodes(r.Context(), season.ID)
		if err != nil {
			continue
		}

		episodesPayload := []map[string]any{}
		for _, ep := range episodes {
			epView := &repositoryEpisodeView{
				episodeID:        ep.ID,
				seasonID:         ep.SeasonID,
				seasonNumber:     season.SeasonNumber,
				episodeNumber:    ep.EpisodeNumber,
				title:            ep.Title,
				storageKey:       ep.StorageKey,
				seriesStorageKey: series.StorageKey,
			}
			episodesPayload = append(episodesPayload, h.buildEpisodePayload(r.Context(), epView, baseURL))
		}

		seasonsPayload = append(seasonsPayload, map[string]any{
			"season_number": season.SeasonNumber,
			"episodes":      episodesPayload,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"series": map[string]any{
				"id":      series.ID,
				"tmdb_id": series.TMDBID,
				"imdb_id": series.IMDbID,
				"title":   series.Title,
				"year":    series.Year,
			},
			"seasons": seasonsPayload,
		},
		"meta": map[string]any{
			"version": "v1",
		},
	})
}

// GetEpisode handles GET /api/player/episode?tmdb_id=...&s=1&e=1
func (h *PlayerHandler) GetEpisode(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	sParam := strings.TrimSpace(r.URL.Query().Get("s"))
	eParam := strings.TrimSpace(r.URL.Query().Get("e"))

	if tmdbID == "" || sParam == "" || eParam == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"tmdb_id, s (season), and e (episode) are required", false, cid)
		return
	}

	seasonNum, err := strconv.Atoi(sParam)
	if err != nil || seasonNum < 1 {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"s must be a positive integer", false, cid)
		return
	}
	episodeNum, err := strconv.Atoi(eParam)
	if err != nil || episodeNum < 1 {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"e must be a positive integer", false, cid)
		return
	}

	series, err := h.seriesRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch series", false, cid)
		return
	}

	ep, err := h.seriesRepo.GetEpisodeBySE(r.Context(), tmdbID, seasonNum, episodeNum)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "episode not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch episode", false, cid)
		return
	}

	baseURL := h.mediaBaseURL
	epView := &repositoryEpisodeView{
		episodeID:        ep.ID,
		seasonID:         ep.SeasonID,
		seasonNumber:     seasonNum,
		episodeNumber:    ep.EpisodeNumber,
		title:            ep.Title,
		storageKey:       ep.StorageKey,
		seriesStorageKey: series.StorageKey,
	}
	epPayload := h.buildEpisodePayload(r.Context(), epView, baseURL)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"series": map[string]any{
				"id":      series.ID,
				"tmdb_id": series.TMDBID,
			},
			"episode": epPayload,
		},
		"meta": map[string]any{
			"version": "v1",
		},
	})
}

// buildEpisodePayload builds the JSON payload for a single episode.
func (h *PlayerHandler) buildEpisodePayload(ctx context.Context, ep *repositoryEpisodeView, baseURL string) map[string]any {
	asset, err := h.seriesRepo.GetEpisodeAsset(ctx, ep.episodeID)
	if err != nil {
		return map[string]any{
			"episode_number": ep.episodeNumber,
			"title":          ep.title,
			"is_ready":       false,
		}
	}

	hlsURL := h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, ep.seriesStorageKey, ep.seasonNumber, ep.episodeNumber, "master.m3u8"))
	thumbURL := h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, ep.seriesStorageKey, ep.seasonNumber, ep.episodeNumber, "thumbnail.jpg"))
	audioTracks := h.buildEpisodeAudioTracks(ctx, asset.AssetID)
	subtitleTracks := h.buildEpisodeSubtitles(ctx, ep.episodeID, baseURL, ep.seriesStorageKey, ep.seasonNumber, ep.episodeNumber)

	payload := map[string]any{
		"episode_number": ep.episodeNumber,
		"title":          ep.title,
		"is_ready":       true,
		"playback": map[string]any{
			"hls": hlsURL,
		},
		"assets": map[string]any{
			"thumbnail": thumbURL,
		},
		"audio_tracks": audioTracks,
		"subtitles":    subtitleTracks,
	}
	return payload
}

// buildEpisodeAudioTracks fetches audio tracks for an episode asset.
func (h *PlayerHandler) buildEpisodeAudioTracks(ctx context.Context, assetID string) []map[string]any {
	tracks := h.buildAudioTracksPayload(ctx, assetID, "episode")
	if tracks == nil {
		return []map[string]any{}
	}
	return tracks
}

// buildEpisodeSubtitles fetches subtitles for an episode.
func (h *PlayerHandler) buildEpisodeSubtitles(ctx context.Context, episodeID int64, baseURL, seriesStorageKey string, seasonNum, episodeNum int) []map[string]string {
	subtitleTracks := []map[string]string{}
	if subs, err := h.epSubtitleRepo.ListByEpisodeID(ctx, episodeID); err == nil {
		for _, sub := range subs {
			subtitleTracks = append(subtitleTracks, map[string]string{
				"language": sub.Language,
				"url":      h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, seriesStorageKey, seasonNum, episodeNum, "subtitles/"+sub.Language+".vtt")),
			})
		}
	}
	return subtitleTracks
}

func buildSeriesMediaURL(baseURL, seriesStorageKey string, seasonNum, episodeNum int, fileName string) string {
	return buildMediaURL(baseURL, fmt.Sprintf("series/%s/s%02d/e%02d/%s", seriesStorageKey, seasonNum, episodeNum, fileName))
}
