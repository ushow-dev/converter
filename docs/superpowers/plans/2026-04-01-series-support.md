# Series Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add end-to-end TV series support with multi-audio tracks across the entire pipeline: scanner → worker → API → player.

**Architecture:** Separate DB tables for series/seasons/episodes alongside existing movies. Multi-audio applies universally (movies + series). FFmpeg maps all audio streams into HLS variants. Player gets two modes: standalone navigation and embed.

**Tech Stack:** Go 1.23, PostgreSQL 16, Python 3.12 (scanner), Next.js/React (player + frontend), FFmpeg, hls.js, GuessIt

**Spec:** `docs/superpowers/specs/2026-04-01-series-support-design.md`

---

## File Structure

### New files

| File | Purpose |
|---|---|
| `api/internal/db/migrations/014_series_and_audio_tracks.sql` | DB schema for series, seasons, episodes, audio_tracks |
| `api/internal/model/series.go` | Go structs: Series, Season, Episode, EpisodeAsset, EpisodeSubtitle, AudioTrack |
| `api/internal/repository/series.go` | Series/Season/Episode CRUD |
| `api/internal/repository/episode.go` | Episode-specific queries |
| `api/internal/repository/audio_track.go` | AudioTrack CRUD |
| `api/internal/handler/series.go` | Admin series handler |
| `api/internal/handler/series_player.go` | Player series/episode endpoints |
| `worker/internal/model/series.go` | Worker-side series structs |
| `worker/internal/ffmpeg/probe.go` | Audio stream probing (extracted from runner.go) |
| `worker/internal/repository/series.go` | Worker-side series/episode DB ops |
| `worker/internal/repository/audio_track.go` | Worker-side audio track DB ops |
| `scanner/scanner/migrations/005_series_support.sql` | Scanner DB: content_kind, season/episode columns |
| `scanner/scanner/services/series_detect.py` | Series folder detection + episode parsing |
| `player/src/app/seriesPage.tsx` | Server component: series data fetcher |
| `player/src/app/SeriesPlayer.tsx` | Client component: series navigation + audio selector |
| `frontend/src/app/series/page.tsx` | Admin series list page |
| `frontend/src/app/series/[id]/page.tsx` | Admin series detail page |
| `frontend/src/components/SeriesTable.tsx` | Series list table component |

### Modified files

| File | Changes |
|---|---|
| `api/internal/model/model.go` | Add series fields to ConvertJob, TransferJob; add ContentTypeSeries const |
| `api/internal/server/server.go` | Register series admin + player routes |
| `api/internal/handler/player.go` | Add audio_tracks to GetMovie response; add `buildSeriesMediaURL` |
| `worker/internal/model/model.go` | Add series fields to ConvertJob, TransferJob |
| `worker/internal/ffmpeg/runner.go` | Multi-audio mapping, return AudioTrackInfo in HLSResult |
| `worker/internal/converter/converter.go` | Series output path branch, save audio tracks + episode assets |
| `worker/internal/ingest/client.go` | Add series fields to IncomingItem |
| `worker/internal/ingest/worker.go` | Series upsert logic in processItem |
| `scanner/scanner/loops/scan_loop.py` | Detect series folders, register episodes |
| `scanner/scanner/services/metadata.py` | Add `tmdb_tv_search()` function |
| `scanner/scanner/api/server.py` | Update claim response with series fields |
| `player/src/app/page.tsx` | Route to series or movie based on `type` param |
| `player/src/app/PlayerClient.tsx` | Add audio track selector UI |
| `frontend/src/types/index.ts` | Add Series, Season, Episode types; update ContentType |
| `frontend/src/components/Nav.tsx` | Add "Сериалы" nav item |

---

## Task 1: Database Migration — Series Tables + Audio Tracks

**Files:**
- Create: `api/internal/db/migrations/014_series_and_audio_tracks.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 014_series_and_audio_tracks.sql

-- Series container
CREATE TABLE IF NOT EXISTS series (
  id BIGSERIAL PRIMARY KEY,
  storage_key TEXT UNIQUE NOT NULL,
  tmdb_id TEXT,
  imdb_id TEXT,
  title TEXT NOT NULL,
  year INT,
  poster_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_series_tmdb_id ON series (tmdb_id) WHERE tmdb_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_series_imdb_id ON series (imdb_id) WHERE imdb_id IS NOT NULL;

-- Season grouping
CREATE TABLE IF NOT EXISTS seasons (
  id BIGSERIAL PRIMARY KEY,
  series_id BIGINT NOT NULL REFERENCES series ON DELETE CASCADE,
  season_number INT NOT NULL,
  poster_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(series_id, season_number)
);

-- Individual episode
CREATE TABLE IF NOT EXISTS episodes (
  id BIGSERIAL PRIMARY KEY,
  season_id BIGINT NOT NULL REFERENCES seasons ON DELETE CASCADE,
  episode_number INT NOT NULL,
  title TEXT,
  storage_key TEXT UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(season_id, episode_number)
);

-- HLS assets per episode
CREATE TABLE IF NOT EXISTS episode_assets (
  asset_id TEXT PRIMARY KEY,
  job_id TEXT REFERENCES media_jobs(job_id),
  episode_id BIGINT NOT NULL REFERENCES episodes ON DELETE CASCADE,
  storage_path TEXT NOT NULL,
  thumbnail_path TEXT,
  duration_sec INT,
  video_codec TEXT,
  audio_codec TEXT,
  is_ready BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_episode_assets_episode_id ON episode_assets (episode_id);

-- Subtitles per episode
CREATE TABLE IF NOT EXISTS episode_subtitles (
  id BIGSERIAL PRIMARY KEY,
  episode_id BIGINT NOT NULL REFERENCES episodes ON DELETE CASCADE,
  language TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'opensubtitles',
  storage_path TEXT NOT NULL,
  external_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(episode_id, language)
);

-- Audio tracks (shared for movies and episodes)
CREATE TABLE IF NOT EXISTS audio_tracks (
  id BIGSERIAL PRIMARY KEY,
  asset_id TEXT NOT NULL,
  asset_type TEXT NOT NULL,
  track_index INT NOT NULL,
  language TEXT,
  label TEXT,
  is_default BOOLEAN DEFAULT false,
  UNIQUE(asset_id, asset_type, track_index)
);

CREATE INDEX IF NOT EXISTS idx_audio_tracks_asset ON audio_tracks (asset_id, asset_type);

-- Extend media_jobs with series context
ALTER TABLE media_jobs ADD COLUMN IF NOT EXISTS series_id BIGINT REFERENCES series;
ALTER TABLE media_jobs ADD COLUMN IF NOT EXISTS season_number INT;
ALTER TABLE media_jobs ADD COLUMN IF NOT EXISTS episode_number INT;
```

- [ ] **Step 2: Verify migration applies cleanly**

Run: `docker compose -f docker-compose.yml up -d postgres && sleep 2 && docker compose -f docker-compose.yml restart api`

Expected: API starts without migration errors in logs.

- [ ] **Step 3: Commit**

```bash
git add api/internal/db/migrations/014_series_and_audio_tracks.sql
git commit -m "feat(db): add series, seasons, episodes, audio_tracks tables"
```

---

## Task 2: API Models — Series + Audio Track Structs

**Files:**
- Create: `api/internal/model/series.go`
- Modify: `api/internal/model/model.go`

- [ ] **Step 1: Create series model file**

```go
// api/internal/model/series.go
package model

import "time"

const ContentTypeSeries = "series"

// Series represents a TV series container.
type Series struct {
	ID         int64     `json:"id"`
	StorageKey string    `json:"storage_key"`
	TMDBID     *string   `json:"tmdb_id,omitempty"`
	IMDbID     *string   `json:"imdb_id,omitempty"`
	Title      string    `json:"title"`
	Year       *int      `json:"year,omitempty"`
	PosterURL  *string   `json:"poster_url,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Season represents a season within a series.
type Season struct {
	ID           int64     `json:"id"`
	SeriesID     int64     `json:"series_id"`
	SeasonNumber int       `json:"season_number"`
	PosterURL    *string   `json:"poster_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Episode represents a single episode within a season.
type Episode struct {
	ID            int64     `json:"id"`
	SeasonID      int64     `json:"season_id"`
	EpisodeNumber int       `json:"episode_number"`
	Title         *string   `json:"title,omitempty"`
	StorageKey    string    `json:"storage_key"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// EpisodeAsset represents a converted HLS asset for an episode.
type EpisodeAsset struct {
	AssetID       string    `json:"asset_id"`
	JobID         string    `json:"job_id"`
	EpisodeID     int64     `json:"episode_id"`
	StoragePath   string    `json:"storage_path"`
	ThumbnailPath *string   `json:"thumbnail_path,omitempty"`
	DurationSec   *int      `json:"duration_sec,omitempty"`
	VideoCodec    *string   `json:"video_codec,omitempty"`
	AudioCodec    *string   `json:"audio_codec,omitempty"`
	IsReady       bool      `json:"is_ready"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// EpisodeSubtitle represents a subtitle track for an episode.
type EpisodeSubtitle struct {
	ID          int64     `json:"id"`
	EpisodeID   int64     `json:"episode_id"`
	Language    string    `json:"language"`
	Source      string    `json:"source"`
	StoragePath string    `json:"storage_path"`
	ExternalID  *string   `json:"external_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AudioTrack represents an audio track in an HLS asset.
type AudioTrack struct {
	ID         int64   `json:"id"`
	AssetID    string  `json:"asset_id"`
	AssetType  string  `json:"asset_type"` // "movie" | "episode"
	TrackIndex int     `json:"track_index"`
	Language   *string `json:"language,omitempty"`
	Label      *string `json:"label,omitempty"`
	IsDefault  bool    `json:"is_default"`
}
```

- [ ] **Step 2: Add series fields to ConvertJob in model.go**

In `api/internal/model/model.go`, add to `ConvertJob` struct (after `StorageKey` field at line 198):

```go
	StorageKey string `json:"storage_key,omitempty"`
	// Series-specific fields (nil for movies).
	SeriesID      *int64 `json:"series_id,omitempty"`
	SeasonNumber  *int   `json:"season_number,omitempty"`
	EpisodeNumber *int   `json:"episode_number,omitempty"`
```

And add to `TransferJob` struct (after `LocalPath` field at line 251):

```go
	LocalPath    string `json:"local_path"`
	ContentType  string `json:"content_type,omitempty"`  // "movie" | "series"
	EpisodeID    *int64 `json:"episode_id,omitempty"`
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add api/internal/model/series.go api/internal/model/model.go
git commit -m "feat(api): add series, episode, audio track model structs"
```

---

## Task 3: Worker Models — Mirror Series Structs

**Files:**
- Create: `worker/internal/model/series.go`
- Modify: `worker/internal/model/model.go`

- [ ] **Step 1: Create worker series model file**

```go
// worker/internal/model/series.go
package model

import "time"

// Series is the catalog record for a TV series (worker-side mirror).
type Series struct {
	ID         int64
	StorageKey string
	TMDBID     *string
	IMDbID     *string
	Title      string
	Year       *int
	PosterURL  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Season within a series.
type Season struct {
	ID           int64
	SeriesID     int64
	SeasonNumber int
}

// Episode within a season.
type Episode struct {
	ID            int64
	SeasonID      int64
	EpisodeNumber int
	Title         *string
	StorageKey    string
}

// EpisodeAsset is the record created after successful episode conversion.
type EpisodeAsset struct {
	AssetID       string
	JobID         string
	EpisodeID     int64
	StoragePath   string
	ThumbnailPath *string
	DurationSec   *int
	VideoCodec    *string
	AudioCodec    *string
	IsReady       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AudioTrack holds metadata for a single audio stream in an HLS asset.
type AudioTrack struct {
	AssetID    string
	AssetType  string // "movie" | "episode"
	TrackIndex int
	Language   *string
	Label      *string
	IsDefault  bool
}
```

- [ ] **Step 2: Add series fields to worker ConvertJob**

In `worker/internal/model/model.go`, add to `ConvertJob` struct (after `StorageKey` at line 72):

```go
	StorageKey    string `json:"storage_key,omitempty"`
	SeriesID      *int64 `json:"series_id,omitempty"`
	SeasonNumber  *int   `json:"season_number,omitempty"`
	EpisodeNumber *int   `json:"episode_number,omitempty"`
```

And add to `TransferJob` struct (after `LocalPath` at line 178):

```go
	LocalPath   string `json:"local_path"`
	ContentType string `json:"content_type,omitempty"`
	EpisodeID   *int64 `json:"episode_id,omitempty"`
```

- [ ] **Step 3: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add worker/internal/model/series.go worker/internal/model/model.go
git commit -m "feat(worker): add series model structs and extend queue payloads"
```

---

## Task 4: FFmpeg — Probe Audio Tracks

**Files:**
- Create: `worker/internal/ffmpeg/probe.go`

- [ ] **Step 1: Create audio probe module**

```go
// worker/internal/ffmpeg/probe.go
package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// AudioStreamInfo holds metadata for a single audio stream found by ffprobe.
type AudioStreamInfo struct {
	Index    int    // absolute stream index in the file
	Language string // ISO 639-1/2 from tags, e.g. "eng", "rus"; empty if unset
	Title    string // free-form title tag, e.g. "DUB", "Original"
}

// ProbeAudioStreams returns metadata for every audio stream in the file.
func ProbeAudioStreams(ctx context.Context, inputPath string) ([]AudioStreamInfo, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a",
		"-show_entries", "stream=index:stream_tags=language,title",
		"-of", "json",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe audio streams: %w", err)
	}

	var result struct {
		Streams []struct {
			Index int `json:"index"`
			Tags  struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse ffprobe audio output: %w", err)
	}

	streams := make([]AudioStreamInfo, len(result.Streams))
	for i, s := range result.Streams {
		streams[i] = AudioStreamInfo{
			Index:    s.Index,
			Language: s.Tags.Language,
			Title:    s.Tags.Title,
		}
	}
	return streams, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add worker/internal/ffmpeg/probe.go
git commit -m "feat(ffmpeg): add multi-audio stream probing via ffprobe"
```

---

## Task 5: FFmpeg — Multi-Audio HLS Encoding

**Files:**
- Modify: `worker/internal/ffmpeg/runner.go`

- [ ] **Step 1: Update HLSResult to include audio track info**

In `runner.go`, replace the `HLSResult` struct (lines 18-21):

```go
// HLSResult holds the outcome of a successful HLS conversion.
type HLSResult struct {
	DurationSec int
	HasAudio    bool
	AudioTracks []AudioStreamInfo // populated when multi-audio detected
}
```

- [ ] **Step 2: Replace single-audio mapping with multi-audio logic**

Replace the body of `RunHLS` from line 63 (`hasAudio := ...`) through line 147 (end of args) with:

```go
	audioStreams, probeErr := ProbeAudioStreams(ctx, inputPath)
	hasAudio := len(audioStreams) > 0
	if probeErr != nil {
		slog.Warn("could not probe audio streams, checking fallback", "error", probeErr)
		hasAudio = probeHasAudio(ctx, inputPath)
		if hasAudio {
			audioStreams = []AudioStreamInfo{{Index: 0, Language: "und", Title: ""}}
		}
	}

	segS := strconv.Itoa(segDur)
	gopS := strconv.Itoa(gop)

	slog.Info("HLS encode params",
		"target_fps", targetFPS, "gop", gop,
		"seg_dur", segDur, "has_audio", hasAudio,
		"audio_tracks", len(audioStreams))

	filterComplex := "[0:v]split=3[v720][v480][v360];" +
		"[v720]scale=-2:720:flags=bicubic[v720o];" +
		"[v480]scale=-2:480:flags=bicubic[v480o];" +
		"[v360]scale=-2:360:flags=bicubic[v360o]"

	// audio source index: 0 = original file; 1 = synthetic silence generator.
	aSrc := "0"
	var args []string
	if threads > 0 {
		args = append(args, "-threads", strconv.Itoa(threads))
	}
	args = append(args, "-hide_banner", "-y", "-i", inputPath)
	if !hasAudio {
		args = append(args,
			"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
		aSrc = "1"
		audioStreams = []AudioStreamInfo{{Index: 0, Language: "und", Title: "Silence"}}
	}

	args = append(args,
		"-map_metadata", "-1",
		"-map_chapters", "-1",
		"-filter_complex", filterComplex,
	)

	numAudio := len(audioStreams)

	// Helper to add one video variant with all audio tracks.
	addVariant := func(videoMap, videoIdx string, bitrate, maxrate, bufsize string) {
		args = append(args, "-map", videoMap)
		for ai := 0; ai < numAudio; ai++ {
			args = append(args, "-map", fmt.Sprintf("%s:a:%d", aSrc, ai))
		}

		args = append(args,
			"-c:v:"+videoIdx, "libx264", "-preset", "fast",
			"-profile:v:"+videoIdx, "high", "-level:v:"+videoIdx, "4.0",
			"-pix_fmt:v:"+videoIdx, "yuv420p", "-sc_threshold:v:"+videoIdx, "0",
			"-x264-params:v:"+videoIdx, "rc-lookahead=30",
			"-b:v:"+videoIdx, bitrate, "-maxrate:v:"+videoIdx, maxrate, "-bufsize:v:"+videoIdx, bufsize,
			"-g:v:"+videoIdx, gopS, "-keyint_min:v:"+videoIdx, gopS,
		)

		// Encode each audio stream for this variant.
		for ai := 0; ai < numAudio; ai++ {
			vi, _ := strconv.Atoi(videoIdx)
			aIdx := strconv.Itoa(vi*numAudio + ai)
			args = append(args,
				"-c:a:"+aIdx, "aac", "-b:a:"+aIdx, "80k",
				"-ar:a:"+aIdx, "48000", "-ac:a:"+aIdx, "2",
			)
		}
	}

	addVariant("[v720o]", "0", "1050k", "1155k", "2300k")
	addVariant("[v480o]", "1", "700k", "770k", "1500k")
	addVariant("[v360o]", "2", "320k", "352k", "700k")

	// Set audio stream metadata (language tags).
	globalAudioIdx := 0
	for vi := 0; vi < 3; vi++ {
		for ai := 0; ai < numAudio; ai++ {
			if audioStreams[ai].Language != "" {
				args = append(args,
					fmt.Sprintf("-metadata:s:a:%d", globalAudioIdx),
					"language="+audioStreams[ai].Language,
				)
			}
			if audioStreams[ai].Title != "" {
				args = append(args,
					fmt.Sprintf("-metadata:s:a:%d", globalAudioIdx),
					"title="+audioStreams[ai].Title,
				)
			}
			globalAudioIdx++
		}
	}

	if !hasAudio {
		args = append(args, "-shortest")
	}

	// Build -var_stream_map: "v:0,a:0,a:1,name:720 v:1,a:2,a:3,name:480 ..."
	var varStreamParts []string
	for vi := 0; vi < 3; vi++ {
		part := fmt.Sprintf("v:%d", vi)
		for ai := 0; ai < numAudio; ai++ {
			part += fmt.Sprintf(",a:%d", vi*numAudio+ai)
		}
		names := []string{"720", "480", "360"}
		part += ",name:" + names[vi]
		varStreamParts = append(varStreamParts, part)
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", segS,
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments",
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", strings.Join(varStreamParts, " "),
		"-hls_segment_filename", filepath.Join(outputDir, "%v", "seg%03d.ts"),
		filepath.Join(outputDir, "%v", "index.m3u8"),
	)
```

- [ ] **Step 3: Update the return value at the end of RunHLS**

Replace lines 180-183:

```go
	return &HLSResult{
		DurationSec: int(totalSec),
		HasAudio:    hasAudio,
		AudioTracks: audioStreams,
	}, nil
```

- [ ] **Step 4: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add worker/internal/ffmpeg/runner.go
git commit -m "feat(ffmpeg): support multi-audio track HLS encoding"
```

---

## Task 6: Worker Repositories — Series + Audio Tracks

**Files:**
- Create: `worker/internal/repository/series.go`
- Create: `worker/internal/repository/audio_track.go`

- [ ] **Step 1: Create series repository**

```go
// worker/internal/repository/series.go
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

// SeriesRepository persists series, season, and episode records.
type SeriesRepository struct {
	pool *pgxpool.Pool
}

// NewSeriesRepository creates a SeriesRepository.
func NewSeriesRepository(pool *pgxpool.Pool) *SeriesRepository {
	return &SeriesRepository{pool: pool}
}

// UpsertSeries inserts or updates a series record. Returns the series with its ID.
func (r *SeriesRepository) UpsertSeries(
	ctx context.Context, tmdbID, imdbID, title string, year *int, posterURL *string, storageKey string,
) (*model.Series, error) {
	tmdb := nullableText(tmdbID)
	imdb := nullableText(imdbID)

	// Try find by tmdb_id first.
	if tmdb != nil {
		var s model.Series
		err := r.pool.QueryRow(ctx,
			`SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
			 FROM series WHERE tmdb_id = $1 LIMIT 1`, *tmdb,
		).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
		if err == nil {
			// Update missing fields.
			_, _ = r.pool.Exec(ctx,
				`UPDATE series SET imdb_id = COALESCE(imdb_id, $2), title = COALESCE(title, $3),
				 year = COALESCE(year, $4), poster_url = COALESCE(poster_url, $5), updated_at = NOW()
				 WHERE id = $1`, s.ID, imdb, title, year, posterURL)
			return &s, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("find series by tmdb: %w", err)
		}
	}

	// Insert new series.
	if storageKey == "" {
		storageKey = buildSeriesStorageKey(title, year, tmdb)
	}
	var s model.Series
	err := r.pool.QueryRow(ctx,
		`INSERT INTO series (storage_key, tmdb_id, imdb_id, title, year, poster_url)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (storage_key) DO UPDATE SET updated_at = NOW()
		 RETURNING id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at`,
		storageKey, tmdb, imdb, title, year, posterURL,
	).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert series: %w", err)
	}
	return &s, nil
}

// UpsertSeason ensures a season record exists.
func (r *SeriesRepository) UpsertSeason(ctx context.Context, seriesID int64, seasonNumber int) (*model.Season, error) {
	var s model.Season
	err := r.pool.QueryRow(ctx,
		`INSERT INTO seasons (series_id, season_number)
		 VALUES ($1, $2)
		 ON CONFLICT (series_id, season_number) DO UPDATE SET updated_at = NOW()
		 RETURNING id, series_id, season_number`,
		seriesID, seasonNumber,
	).Scan(&s.ID, &s.SeriesID, &s.SeasonNumber)
	if err != nil {
		return nil, fmt.Errorf("upsert season: %w", err)
	}
	return &s, nil
}

// UpsertEpisode ensures an episode record exists.
func (r *SeriesRepository) UpsertEpisode(
	ctx context.Context, seasonID int64, episodeNumber int, title *string, storageKey string,
) (*model.Episode, error) {
	var e model.Episode
	err := r.pool.QueryRow(ctx,
		`INSERT INTO episodes (season_id, episode_number, title, storage_key)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (season_id, episode_number) DO UPDATE SET title = COALESCE(episodes.title, EXCLUDED.title), updated_at = NOW()
		 RETURNING id, season_id, episode_number, title, storage_key`,
		seasonID, episodeNumber, title, storageKey,
	).Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &e.Title, &e.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("upsert episode: %w", err)
	}
	return &e, nil
}

// CreateEpisodeAsset inserts an episode asset record.
func (r *SeriesRepository) CreateEpisodeAsset(ctx context.Context, a *model.EpisodeAsset) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO episode_assets (asset_id, job_id, episode_id, storage_path, thumbnail_path, duration_sec, video_codec, audio_codec, is_ready, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		a.AssetID, a.JobID, a.EpisodeID, a.StoragePath, a.ThumbnailPath,
		a.DurationSec, a.VideoCodec, a.AudioCodec, a.IsReady, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func buildSeriesStorageKey(title string, year *int, tmdbID *string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			sb.WriteRune(r)
		}
	}
	slug := strings.TrimSpace(sb.String())
	slug = strings.Join(strings.Fields(slug), "_")
	if slug == "" {
		slug = "untitled"
	}
	parts := []string{slug}
	if year != nil && *year > 0 {
		parts = append(parts, fmt.Sprintf("%d", *year))
	}
	key := strings.Join(parts, "_")
	if tmdbID != nil && *tmdbID != "" {
		key += fmt.Sprintf("_[%s]", *tmdbID)
	}
	return key
}
```

- [ ] **Step 2: Create audio track repository**

```go
// worker/internal/repository/audio_track.go
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

// AudioTrackRepository persists audio track metadata.
type AudioTrackRepository struct {
	pool *pgxpool.Pool
}

// NewAudioTrackRepository creates an AudioTrackRepository.
func NewAudioTrackRepository(pool *pgxpool.Pool) *AudioTrackRepository {
	return &AudioTrackRepository{pool: pool}
}

// BulkInsert inserts multiple audio tracks for an asset.
func (r *AudioTrackRepository) BulkInsert(ctx context.Context, tracks []model.AudioTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	for _, t := range tracks {
		_, err := r.pool.Exec(ctx,
			`INSERT INTO audio_tracks (asset_id, asset_type, track_index, language, label, is_default)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (asset_id, asset_type, track_index) DO NOTHING`,
			t.AssetID, t.AssetType, t.TrackIndex, t.Language, t.Label, t.IsDefault,
		)
		if err != nil {
			return fmt.Errorf("insert audio track %d: %w", t.TrackIndex, err)
		}
	}
	return nil
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add worker/internal/repository/series.go worker/internal/repository/audio_track.go
git commit -m "feat(worker): add series and audio track repositories"
```

---

## Task 7: Worker Converter — Series Branch + Audio Track Saving

**Files:**
- Modify: `worker/internal/converter/converter.go`

This is the largest modification. The converter needs to:
1. Branch on `content_type` to determine output path and asset table
2. Save audio tracks after conversion
3. Create episode assets for series content

- [ ] **Step 1: Add seriesRepo and audioTrackRepo to Worker struct**

Add fields to the `Worker` struct (after line 35 `subtitleRepo`):

```go
	seriesRepo       *repository.SeriesRepository
	audioTrackRepo   *repository.AudioTrackRepository
```

Update the `New()` constructor to accept and store these new repos (add parameters and assignments).

- [ ] **Step 2: Add series output path logic to process()**

After the movie upsert block (line 228), add a series branch. Replace lines 211-228 with:

```go
	// ── Create content record and derive final directory ─────────────────────
	var finalDir string
	var contentID int64     // movie.ID or episode.ID
	var contentType string  // "movie" or "episode"

	if msg.ContentType == "series" && msg.Payload.SeriesID != nil {
		// Series path: upsert episode, use series storage layout.
		season, err := w.seriesRepo.UpsertSeason(jobCtx, *msg.Payload.SeriesID, *msg.Payload.SeasonNumber)
		if err != nil {
			w.failJob(ctx, msg, "DB_ERROR", "upsert season: "+err.Error(), false)
			return
		}
		var epTitle *string
		if msg.Payload.Title != "" {
			epTitle = &msg.Payload.Title
		}
		episode, err := w.seriesRepo.UpsertEpisode(jobCtx, season.ID, *msg.Payload.EpisodeNumber, epTitle, msg.Payload.StorageKey)
		if err != nil {
			w.failJob(ctx, msg, "DB_ERROR", "upsert episode: "+err.Error(), false)
			return
		}
		// /media/converted/series/{series_storage_key}/s{NN}/e{NN}
		seriesStorageKey := msg.Payload.StorageKey
		// Strip episode suffix to get series key: "show_2020_[123]_s01e01" → find series by ID
		var seriesKey string
		if s, err := w.seriesRepo.GetSeriesByID(jobCtx, *msg.Payload.SeriesID); err == nil {
			seriesKey = s.StorageKey
		} else {
			seriesKey = seriesStorageKey
		}
		finalDir = filepath.Join(w.mediaRoot, "converted", "series", seriesKey,
			fmt.Sprintf("s%02d", *msg.Payload.SeasonNumber),
			fmt.Sprintf("e%02d", *msg.Payload.EpisodeNumber))
		contentID = episode.ID
		contentType = "episode"
	} else {
		// Movie path (existing logic).
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
		movie, err := w.movieRepo.Upsert(jobCtx, msg.Payload.IMDbID, msg.Payload.TMDBID, msg.Payload.Title, upsertYear, upsertPoster, msg.Payload.StorageKey)
		if err != nil {
			w.failJob(ctx, msg, "DB_ERROR", "create movie record: "+err.Error(), false)
			return
		}
		finalDir = filepath.Join(w.mediaRoot, "converted", "movies", movie.StorageKey)
		contentID = movie.ID
		contentType = "movie"
	}
```

- [ ] **Step 3: Create episode asset or movie asset based on content type**

Replace the asset creation block (lines 279-301) with:

```go
	// ── Create asset record ───────────────────────────────────────────────────
	now := time.Now().UTC()
	assetID := generateAssetID()
	videoCodec := "h264"
	audioCodec := "aac"

	if contentType == "episode" {
		epAsset := &model.EpisodeAsset{
			AssetID:       assetID,
			JobID:         msg.JobID,
			EpisodeID:     contentID,
			StoragePath:   masterPath,
			ThumbnailPath: thumbFinalPath,
			DurationSec:   &durationSec,
			VideoCodec:    &videoCodec,
			AudioCodec:    &audioCodec,
			IsReady:       true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := w.seriesRepo.CreateEpisodeAsset(jobCtx, epAsset); err != nil {
			log.Error("create episode asset record", "error", err)
		}
	} else {
		asset := &model.Asset{
			AssetID:       assetID,
			JobID:         msg.JobID,
			MovieID:       &contentID,
			StoragePath:   masterPath,
			ThumbnailPath: thumbFinalPath,
			DurationSec:   &durationSec,
			VideoCodec:    &videoCodec,
			AudioCodec:    &audioCodec,
			IsReady:       true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := w.assetRepo.Create(jobCtx, asset); err != nil {
			log.Error("create asset record", "error", err)
		}
	}

	// ── Save audio track metadata ─────────────────────────────────────────────
	if len(result.AudioTracks) > 0 {
		var tracks []model.AudioTrack
		for i, at := range result.AudioTracks {
			lang := nullableText(at.Language)
			label := nullableText(at.Title)
			tracks = append(tracks, model.AudioTrack{
				AssetID:    assetID,
				AssetType:  contentType,
				TrackIndex: i,
				Language:   lang,
				Label:      label,
				IsDefault:  i == 0,
			})
		}
		if err := w.audioTrackRepo.BulkInsert(jobCtx, tracks); err != nil {
			log.Warn("save audio tracks failed", "error", err)
		}
	}
```

- [ ] **Step 4: Add GetSeriesByID to series repository**

In `worker/internal/repository/series.go`, add:

```go
// GetSeriesByID fetches a series by primary key.
func (r *SeriesRepository) GetSeriesByID(ctx context.Context, id int64) (*model.Series, error) {
	var s model.Series
	err := r.pool.QueryRow(ctx,
		`SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		 FROM series WHERE id = $1`, id,
	).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get series by id: %w", err)
	}
	return &s, nil
}
```

- [ ] **Step 5: Add nullableText helper to converter.go** (or reuse from repository)

At the end of `converter.go`:

```go
func nullableText(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
```

- [ ] **Step 6: Update transfer message for series**

In the transfer section (~line 335), update `TransferJob` to pass content type:

```go
		tfMsg := model.TransferMessage{
			SchemaVersion: "1",
			JobID:         msg.JobID,
			CorrelationID: msg.CorrelationID,
			CreatedAt:     time.Now().UTC(),
			Payload: model.TransferJob{
				MovieID:     contentID,
				StorageKey:  filepath.Base(finalDir),
				LocalPath:   finalDir,
				ContentType: msg.ContentType,
			},
		}
```

- [ ] **Step 7: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 8: Commit**

```bash
git add worker/internal/converter/converter.go worker/internal/repository/series.go
git commit -m "feat(worker): add series branch in converter and audio track saving"
```

---

## Task 8: Ingest Worker — Series Support

**Files:**
- Modify: `worker/internal/ingest/client.go`
- Modify: `worker/internal/ingest/worker.go`

- [ ] **Step 1: Add series fields to IncomingItem**

In `client.go`, extend the `IncomingItem` struct (after line 19):

```go
type IncomingItem struct {
	ID             int64   `json:"id"`
	SourcePath     string  `json:"source_path"`
	SourceFilename string  `json:"source_filename"`
	ContentKind    string  `json:"content_kind"`
	NormalizedName *string `json:"normalized_name,omitempty"`
	TMDBID         *string `json:"tmdb_id,omitempty"`
	SeriesTMDBID   *string `json:"series_tmdb_id,omitempty"`
	SeasonNumber   *int    `json:"season_number,omitempty"`
	EpisodeNumber  *int    `json:"episode_number,omitempty"`
}
```

- [ ] **Step 2: Add series upsert logic to processItem**

In `worker.go`, after the `jobRepo.CreateForIngest` call (line 104), add series handling:

```go
	// For episode content, upsert series/season/episode in converter DB.
	var seriesID *int64
	if contentKind == "episode" && item.SeriesTMDBID != nil {
		series, err := w.seriesRepo.UpsertSeries(ctx, *item.SeriesTMDBID, "", title, nil, nil, "")
		if err != nil {
			log.Error("upsert series for ingest failed", "error", err)
			// Continue anyway — seriesID will be nil.
		} else {
			seriesID = &series.ID
		}
	}
```

Add `seriesRepo *repository.SeriesRepository` field to the `Worker` struct and `New()` constructor.

- [ ] **Step 3: Pass series fields in ConvertMessage**

Update the `ConvertMessage` payload construction (lines 123-141):

```go
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
			StorageKey:    title,
			SeriesID:      seriesID,
			SeasonNumber:  item.SeasonNumber,
			EpisodeNumber: item.EpisodeNumber,
		},
	}
```

- [ ] **Step 4: Verify compilation**

Run: `cd worker && go build ./...`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add worker/internal/ingest/client.go worker/internal/ingest/worker.go
git commit -m "feat(worker): add series support to ingest worker"
```

---

## Task 9: API Repositories — Series + Audio Tracks

**Files:**
- Create: `api/internal/repository/series.go`
- Create: `api/internal/repository/audio_track.go`

- [ ] **Step 1: Create API series repository**

```go
// api/internal/repository/series.go
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// SeriesRepository handles series, season, and episode queries.
type SeriesRepository struct {
	pool *pgxpool.Pool
}

// NewSeriesRepository creates a SeriesRepository.
func NewSeriesRepository(pool *pgxpool.Pool) *SeriesRepository {
	return &SeriesRepository{pool: pool}
}

// GetByTMDBID fetches a series by its TMDB ID.
func (r *SeriesRepository) GetByTMDBID(ctx context.Context, tmdbID string) (*model.Series, error) {
	var s model.Series
	err := r.pool.QueryRow(ctx,
		`SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		 FROM series WHERE tmdb_id = $1 LIMIT 1`, tmdbID,
	).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get series by tmdb: %w", err)
	}
	return &s, nil
}

// GetByID fetches a series by primary key.
func (r *SeriesRepository) GetByID(ctx context.Context, id int64) (*model.Series, error) {
	var s model.Series
	err := r.pool.QueryRow(ctx,
		`SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at
		 FROM series WHERE id = $1`, id,
	).Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get series by id: %w", err)
	}
	return &s, nil
}

// List returns series ordered by creation time descending with cursor pagination.
func (r *SeriesRepository) List(ctx context.Context, limit int, cursor string) ([]*model.Series, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	const base = `SELECT id, storage_key, tmdb_id, imdb_id, title, year, poster_url, created_at, updated_at FROM series`

	var rows pgx.Rows
	var err error
	if cursor != "" {
		rows, err = r.pool.Query(ctx, base+` WHERE created_at < $1::timestamptz ORDER BY created_at DESC LIMIT $2`, cursor, limit+1)
	} else {
		rows, err = r.pool.Query(ctx, base+` ORDER BY created_at DESC LIMIT $1`, limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list series: %w", err)
	}
	defer rows.Close()

	var items []*model.Series
	for rows.Next() {
		s := &model.Series{}
		if err := rows.Scan(&s.ID, &s.StorageKey, &s.TMDBID, &s.IMDbID, &s.Title, &s.Year, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, "", fmt.Errorf("scan series: %w", err)
		}
		items = append(items, s)
	}
	var nextCursor string
	if len(items) > limit {
		items = items[:limit]
		nextCursor = items[limit-1].CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return items, nextCursor, nil
}

// ListSeasons returns all seasons for a series.
func (r *SeriesRepository) ListSeasons(ctx context.Context, seriesID int64) ([]*model.Season, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, series_id, season_number, poster_url, created_at, updated_at
		 FROM seasons WHERE series_id = $1 ORDER BY season_number`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("list seasons: %w", err)
	}
	defer rows.Close()

	var items []*model.Season
	for rows.Next() {
		s := &model.Season{}
		if err := rows.Scan(&s.ID, &s.SeriesID, &s.SeasonNumber, &s.PosterURL, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan season: %w", err)
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// ListEpisodes returns all episodes for a season.
func (r *SeriesRepository) ListEpisodes(ctx context.Context, seasonID int64) ([]*model.Episode, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, season_id, episode_number, title, storage_key, created_at, updated_at
		 FROM episodes WHERE season_id = $1 ORDER BY episode_number`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	defer rows.Close()

	var items []*model.Episode
	for rows.Next() {
		e := &model.Episode{}
		if err := rows.Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &e.Title, &e.StorageKey, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// GetEpisodeBySE fetches an episode by series tmdb_id + season + episode number.
func (r *SeriesRepository) GetEpisodeBySE(ctx context.Context, seriesTMDBID string, seasonNum, episodeNum int) (*model.Episode, error) {
	var e model.Episode
	err := r.pool.QueryRow(ctx,
		`SELECT ep.id, ep.season_id, ep.episode_number, ep.title, ep.storage_key, ep.created_at, ep.updated_at
		 FROM episodes ep
		 JOIN seasons se ON se.id = ep.season_id
		 JOIN series sr ON sr.id = se.series_id
		 WHERE sr.tmdb_id = $1 AND se.season_number = $2 AND ep.episode_number = $3`,
		seriesTMDBID, seasonNum, episodeNum,
	).Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &e.Title, &e.StorageKey, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get episode: %w", err)
	}
	return &e, nil
}

// GetEpisodeAsset fetches the ready asset for an episode.
func (r *SeriesRepository) GetEpisodeAsset(ctx context.Context, episodeID int64) (*model.EpisodeAsset, error) {
	var a model.EpisodeAsset
	err := r.pool.QueryRow(ctx,
		`SELECT asset_id, job_id, episode_id, storage_path, thumbnail_path, duration_sec, video_codec, audio_codec, is_ready, created_at, updated_at
		 FROM episode_assets WHERE episode_id = $1 AND is_ready = true LIMIT 1`, episodeID,
	).Scan(&a.AssetID, &a.JobID, &a.EpisodeID, &a.StoragePath, &a.ThumbnailPath, &a.DurationSec, &a.VideoCodec, &a.AudioCodec, &a.IsReady, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get episode asset: %w", err)
	}
	return &a, nil
}

// DeleteSeries deletes a series and all cascaded data.
func (r *SeriesRepository) DeleteSeries(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM series WHERE id = $1`, id)
	return err
}
```

- [ ] **Step 2: Create API audio track repository**

```go
// api/internal/repository/audio_track.go
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// AudioTrackRepository reads audio track metadata.
type AudioTrackRepository struct {
	pool *pgxpool.Pool
}

// NewAudioTrackRepository creates an AudioTrackRepository.
func NewAudioTrackRepository(pool *pgxpool.Pool) *AudioTrackRepository {
	return &AudioTrackRepository{pool: pool}
}

// ListByAsset returns audio tracks for a given asset.
func (r *AudioTrackRepository) ListByAsset(ctx context.Context, assetID, assetType string) ([]*model.AudioTrack, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, asset_id, asset_type, track_index, language, label, is_default
		 FROM audio_tracks WHERE asset_id = $1 AND asset_type = $2 ORDER BY track_index`,
		assetID, assetType)
	if err != nil {
		return nil, fmt.Errorf("list audio tracks: %w", err)
	}
	defer rows.Close()

	var tracks []*model.AudioTrack
	for rows.Next() {
		t := &model.AudioTrack{}
		if err := rows.Scan(&t.ID, &t.AssetID, &t.AssetType, &t.TrackIndex, &t.Language, &t.Label, &t.IsDefault); err != nil {
			return nil, fmt.Errorf("scan audio track: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}
```

- [ ] **Step 3: Create episode subtitle repository**

```go
// api/internal/repository/episode_subtitle.go
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// EpisodeSubtitleRepository handles episode subtitle queries.
type EpisodeSubtitleRepository struct {
	pool *pgxpool.Pool
}

// NewEpisodeSubtitleRepository creates an EpisodeSubtitleRepository.
func NewEpisodeSubtitleRepository(pool *pgxpool.Pool) *EpisodeSubtitleRepository {
	return &EpisodeSubtitleRepository{pool: pool}
}

// ListByEpisodeID returns subtitles for a given episode.
func (r *EpisodeSubtitleRepository) ListByEpisodeID(ctx context.Context, episodeID int64) ([]*model.EpisodeSubtitle, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, episode_id, language, source, storage_path, external_id, created_at, updated_at
		 FROM episode_subtitles WHERE episode_id = $1 ORDER BY language`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("list episode subtitles: %w", err)
	}
	defer rows.Close()

	var subs []*model.EpisodeSubtitle
	for rows.Next() {
		s := &model.EpisodeSubtitle{}
		if err := rows.Scan(&s.ID, &s.EpisodeID, &s.Language, &s.Source, &s.StoragePath, &s.ExternalID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan episode subtitle: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add api/internal/repository/series.go api/internal/repository/audio_track.go api/internal/repository/episode_subtitle.go
git commit -m "feat(api): add series, audio track, and episode subtitle repositories"
```

---

## Task 10: API Player Handler — Series + Audio Tracks

**Files:**
- Modify: `api/internal/handler/player.go`

- [ ] **Step 1: Add series and audio track repos to PlayerHandler**

Add to struct fields (after `subtitleRepo` at line 31):

```go
	seriesRepo      *repository.SeriesRepository
	audioTrackRepo  *repository.AudioTrackRepository
	epSubtitleRepo  *repository.EpisodeSubtitleRepository
```

Update `NewPlayerHandler` to accept and store these.

- [ ] **Step 2: Add audio tracks to GetMovie response**

In `GetMovie()`, after building `subtitleTracks` (line 151), add:

```go
	// Build audio track list.
	audioTracks := []map[string]any{}
	if asset, err := h.assetRepo.GetByMovieID(r.Context(), movie.id); err == nil {
		if tracks, err := h.audioTrackRepo.ListByAsset(r.Context(), asset.AssetID, "movie"); err == nil {
			for _, t := range tracks {
				track := map[string]any{
					"index":      t.TrackIndex,
					"is_default": t.IsDefault,
				}
				if t.Language != nil {
					track["language"] = *t.Language
				}
				if t.Label != nil {
					track["label"] = *t.Label
				}
				audioTracks = append(audioTracks, track)
			}
		}
	}
```

And include `"audio_tracks": audioTracks` in the response JSON alongside `"subtitles"`.

- [ ] **Step 3: Add GetSeries handler**

```go
// GetSeries handles GET /api/player/series?tmdb_id=...
func (h *PlayerHandler) GetSeries(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	if tmdbID == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "tmdb_id is required", false, cid)
		return
	}

	series, err := h.seriesRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch series", false, cid)
		return
	}

	baseURL := h.resolveBaseURL(r.Context(), nil)
	seasons, _ := h.seriesRepo.ListSeasons(r.Context(), series.ID)

	seasonsData := []map[string]any{}
	for _, season := range seasons {
		episodes, _ := h.seriesRepo.ListEpisodes(r.Context(), season.ID)
		episodesData := []map[string]any{}

		for _, ep := range episodes {
			epData := map[string]any{
				"episode_number": ep.EpisodeNumber,
				"title":          ep.Title,
			}

			asset, err := h.seriesRepo.GetEpisodeAsset(r.Context(), ep.ID)
			if err == nil {
				epData["playback"] = map[string]string{
					"hls": h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, series.StorageKey, season.SeasonNumber, ep.EpisodeNumber, "master.m3u8")),
				}
				epData["assets"] = map[string]string{
					"thumbnail": h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, series.StorageKey, season.SeasonNumber, ep.EpisodeNumber, "thumbnail.jpg")),
				}

				// Audio tracks
				if tracks, err := h.audioTrackRepo.ListByAsset(r.Context(), asset.AssetID, "episode"); err == nil && len(tracks) > 0 {
					var audioData []map[string]any
					for _, t := range tracks {
						td := map[string]any{"index": t.TrackIndex, "is_default": t.IsDefault}
						if t.Language != nil { td["language"] = *t.Language }
						if t.Label != nil { td["label"] = *t.Label }
						audioData = append(audioData, td)
					}
					epData["audio_tracks"] = audioData
				}
			}

			// Subtitles
			if subs, err := h.epSubtitleRepo.ListByEpisodeID(r.Context(), ep.ID); err == nil && len(subs) > 0 {
				var subData []map[string]string
				for _, sub := range subs {
					subData = append(subData, map[string]string{
						"language": sub.Language,
						"url":     h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, series.StorageKey, season.SeasonNumber, ep.EpisodeNumber, "subtitles/"+sub.Language+".vtt")),
					})
				}
				epData["subtitles"] = subData
			}

			episodesData = append(episodesData, epData)
		}

		seasonData := map[string]any{
			"season_number": season.SeasonNumber,
			"poster_url":    season.PosterURL,
			"episodes":      episodesData,
		}
		seasonsData = append(seasonsData, seasonData)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"series": map[string]any{
				"id":         series.ID,
				"tmdb_id":    series.TMDBID,
				"title":      series.Title,
				"year":       series.Year,
				"poster_url": series.PosterURL,
			},
			"seasons": seasonsData,
		},
		"meta": map[string]any{"version": "v1"},
	})
}
```

- [ ] **Step 4: Add GetEpisode handler**

```go
// GetEpisode handles GET /api/player/episode?tmdb_id=...&s=1&e=1
func (h *PlayerHandler) GetEpisode(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	sNum, _ := strconv.Atoi(r.URL.Query().Get("s"))
	eNum, _ := strconv.Atoi(r.URL.Query().Get("e"))

	if tmdbID == "" || sNum <= 0 || eNum <= 0 {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "tmdb_id, s, and e are required", false, cid)
		return
	}

	series, err := h.seriesRepo.GetByTMDBID(r.Context(), tmdbID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch series", false, cid)
		return
	}

	episode, err := h.seriesRepo.GetEpisodeBySE(r.Context(), tmdbID, sNum, eNum)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "episode not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch episode", false, cid)
		return
	}

	baseURL := h.resolveBaseURL(r.Context(), nil)

	epData := map[string]any{
		"episode_number": episode.EpisodeNumber,
		"season_number":  sNum,
		"title":          episode.Title,
		"series": map[string]any{
			"tmdb_id": series.TMDBID,
			"title":   series.Title,
		},
	}

	asset, err := h.seriesRepo.GetEpisodeAsset(r.Context(), episode.ID)
	if err == nil {
		epData["playback"] = map[string]string{
			"hls": h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, series.StorageKey, sNum, eNum, "master.m3u8")),
		}
		epData["assets"] = map[string]string{
			"thumbnail": h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, series.StorageKey, sNum, eNum, "thumbnail.jpg")),
		}
		if tracks, err := h.audioTrackRepo.ListByAsset(r.Context(), asset.AssetID, "episode"); err == nil && len(tracks) > 0 {
			var audioData []map[string]any
			for _, t := range tracks {
				td := map[string]any{"index": t.TrackIndex, "is_default": t.IsDefault}
				if t.Language != nil { td["language"] = *t.Language }
				if t.Label != nil { td["label"] = *t.Label }
				audioData = append(audioData, td)
			}
			epData["audio_tracks"] = audioData
		}
	}

	if subs, err := h.epSubtitleRepo.ListByEpisodeID(r.Context(), episode.ID); err == nil && len(subs) > 0 {
		var subData []map[string]string
		for _, sub := range subs {
			subData = append(subData, map[string]string{
				"language": sub.Language,
				"url":     h.maybeSignMediaURL(buildSeriesMediaURL(baseURL, series.StorageKey, sNum, eNum, "subtitles/"+sub.Language+".vtt")),
			})
		}
		epData["subtitles"] = subData
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": epData,
		"meta": map[string]any{"version": "v1"},
	})
}
```

- [ ] **Step 5: Add URL builder helper**

```go
func buildSeriesMediaURL(baseURL, seriesStorageKey string, seasonNum, episodeNum int, fileName string) string {
	relative := fmt.Sprintf("/series/%s/s%02d/e%02d/%s", seriesStorageKey, seasonNum, episodeNum, fileName)
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return relative
	}
	return trimmed + relative
}
```

- [ ] **Step 6: Update mediaSigningPath for series**

In `mediaSigningPath()`, update the condition at line 315 to include `"series"`:

```go
	if len(parts) >= 2 &&
		(parts[0] == "movies" || parts[0] == "series" || parts[0] == "serials" || parts[0] == "tv") &&
```

- [ ] **Step 7: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 8: Commit**

```bash
git add api/internal/handler/player.go
git commit -m "feat(api): add series and episode player endpoints with audio tracks"
```

---

## Task 11: API Admin Handler — Series CRUD

**Files:**
- Create: `api/internal/handler/series.go`

- [ ] **Step 1: Create series admin handler**

```go
// api/internal/handler/series.go
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"app/api/internal/auth"
	"app/api/internal/repository"
)

// SeriesHandler handles /api/admin/series/* endpoints.
type SeriesHandler struct {
	seriesRepo *repository.SeriesRepository
}

// NewSeriesHandler creates a SeriesHandler.
func NewSeriesHandler(seriesRepo *repository.SeriesRepository) *SeriesHandler {
	return &SeriesHandler{seriesRepo: seriesRepo}
}

// List handles GET /api/admin/series
func (h *SeriesHandler) List(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	cursor := r.URL.Query().Get("cursor")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	items, nextCursor, err := h.seriesRepo.List(r.Context(), limit, cursor)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list series", false, cid)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"next_cursor": nextCursor,
	})
}

// Get handles GET /api/admin/series/{seriesId}
func (h *SeriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "seriesId"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid series ID", false, cid)
		return
	}

	series, err := h.seriesRepo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "series not found", false, cid)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch series", false, cid)
		return
	}

	seasons, _ := h.seriesRepo.ListSeasons(r.Context(), id)

	seasonsData := []map[string]any{}
	for _, season := range seasons {
		episodes, _ := h.seriesRepo.ListEpisodes(r.Context(), season.ID)
		seasonsData = append(seasonsData, map[string]any{
			"id":            season.ID,
			"season_number": season.SeasonNumber,
			"poster_url":    season.PosterURL,
			"episodes":      episodes,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"series":  series,
		"seasons": seasonsData,
	})
}

// Delete handles DELETE /api/admin/series/{seriesId}
func (h *SeriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "seriesId"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid series ID", false, cid)
		return
	}

	if err := h.seriesRepo.DeleteSeries(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete series", false, cid)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add api/internal/handler/series.go
git commit -m "feat(api): add admin series CRUD handler"
```

---

## Task 12: API Server — Register Series Routes

**Files:**
- Modify: `api/internal/server/server.go`

- [ ] **Step 1: Add SeriesHandler to Dependencies**

```go
type Dependencies struct {
	// ... existing fields ...
	SeriesHandler  *handler.SeriesHandler
}
```

- [ ] **Step 2: Add admin series routes**

Inside the protected JWT group (after line 73), add:

```go
			// Series
			r.Get("/series", deps.SeriesHandler.List)
			r.Get("/series/{seriesId}", deps.SeriesHandler.Get)
			r.Delete("/series/{seriesId}", deps.SeriesHandler.Delete)
```

- [ ] **Step 3: Add player series routes**

Inside the player API group (after line 94), add:

```go
			r.Get("/series", deps.PlayerHandler.GetSeries)
			r.Get("/episode", deps.PlayerHandler.GetEpisode)
```

- [ ] **Step 4: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add api/internal/server/server.go
git commit -m "feat(api): register series admin and player routes"
```

---

## Task 13: Scanner — Series Detection

**Files:**
- Create: `scanner/scanner/migrations/005_series_support.sql`
- Create: `scanner/scanner/services/series_detect.py`
- Modify: `scanner/scanner/loops/scan_loop.py`
- Modify: `scanner/scanner/services/metadata.py`

- [ ] **Step 1: Scanner DB migration**

```sql
-- 005_series_support.sql
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS content_kind TEXT NOT NULL DEFAULT 'movie';
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS series_tmdb_id TEXT;
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS season_number INT;
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS episode_number INT;
```

- [ ] **Step 2: Create series detection service**

```python
# scanner/scanner/services/series_detect.py
import logging
import os
from pathlib import Path
from typing import Optional

import guessit

logger = logging.getLogger(__name__)

VIDEO_EXTENSIONS = {".mkv", ".mp4", ".avi", ".mov", ".ts", ".m2ts", ".wmv"}


def detect_series_folder(folder_path: Path) -> Optional[list[dict]]:
    """Scan a folder for TV series episodes.

    Returns a list of episode dicts if series content detected, None otherwise.
    Each dict: {file_path, title, season, episode, year}
    """
    episodes = []
    for dirpath, _, filenames in os.walk(folder_path):
        for fname in filenames:
            if fname.startswith("._"):
                continue
            if Path(fname).suffix.lower() not in VIDEO_EXTENSIONS:
                continue

            file_path = Path(dirpath) / fname
            info = guessit.guessit(fname)

            if info.get("type") != "episode":
                continue

            season = info.get("season")
            episode_num = info.get("episode")

            if season is None or episode_num is None:
                continue

            episodes.append({
                "file_path": file_path,
                "title": str(info.get("title", folder_path.name)),
                "season": int(season),
                "episode": int(episode_num),
                "year": info.get("year"),
            })

    if not episodes:
        return None

    # Sort by season, episode
    episodes.sort(key=lambda e: (e["season"], e["episode"]))
    return episodes
```

- [ ] **Step 3: Add TMDB TV search to metadata service**

In `scanner/scanner/services/metadata.py`, add after the existing `tmdb_search` function:

```python
def tmdb_tv_search(title: str, year: Optional[int], api_key: str) -> Optional[dict]:
    """Search TMDB for TV series."""
    try:
        params = {"api_key": api_key, "query": title, "language": "en-US"}
        if year:
            params["first_air_date_year"] = year
        resp = requests.get(f"{_TMDB_BASE}/search/tv", params=params, timeout=10)
        resp.raise_for_status()
        results = resp.json().get("results", [])
        if not results:
            return None

        best = results[0]
        poster_url = f"{_TMDB_IMAGE_BASE}{best['poster_path']}" if best.get("poster_path") else None
        return {
            "tmdb_id": str(best["id"]),
            "title": best.get("name", title),
            "poster_url": poster_url,
        }
    except requests.RequestException as e:
        logger.warning("TMDB TV search failed for %r: %s", title, e)
        return None
    finally:
        time.sleep(0.5)
```

- [ ] **Step 4: Update scan_loop to detect series folders**

In `scan_loop.py`, modify `_scan_once` to detect folders with episodes. Add before the `_walk_video_files` loop:

```python
from scanner.services import series_detect

def _scan_once(cfg: Config) -> None:
    _retry_failed_items()
    now = datetime.now(timezone.utc)

    # Check for series folders (top-level directories in incoming).
    incoming = Path(cfg.incoming_dir)
    for entry in incoming.iterdir():
        if entry.is_dir():
            try:
                _process_series_folder(cfg, entry, now)
            except Exception:
                logger.exception("error processing series folder %s", entry)

    # Existing: scan individual video files.
    for file_path in _walk_video_files(Path(cfg.incoming_dir)):
        try:
            _process_file(cfg, file_path, now)
        except Exception:
            logger.exception("error processing file %s", file_path)
```

Add `_process_series_folder` function:

```python
def _process_series_folder(cfg: Config, folder_path: Path, now: datetime) -> None:
    """Detect and register episodes from a series folder."""
    episodes = series_detect.detect_series_folder(folder_path)
    if not episodes:
        return  # Not a series folder, individual files handled by _process_file

    # Use first episode to determine series metadata.
    series_title = episodes[0]["title"]
    series_year = episodes[0].get("year")

    tmdb_result = metadata.tmdb_tv_search(series_title, series_year, cfg.tmdb_api_key)
    series_tmdb_id = tmdb_result["tmdb_id"] if tmdb_result else None
    canonical_title = tmdb_result["title"] if tmdb_result else series_title

    for ep in episodes:
        file_path = ep["file_path"]
        season_num = ep["season"]
        episode_num = ep["episode"]

        normalized = metadata.build_normalized_name(canonical_title, series_year, series_tmdb_id)
        ep_normalized = f"{normalized}_s{season_num:02d}e{episode_num:02d}"

        conn = db.get_conn()
        try:
            with conn:
                with conn.cursor() as cur:
                    cur.execute(
                        "SELECT id FROM scanner_incoming_items WHERE source_path = %s",
                        (str(file_path),),
                    )
                    if cur.fetchone():
                        continue  # Already registered

                    cur.execute(
                        """INSERT INTO scanner_incoming_items
                           (source_path, source_filename, file_size_bytes, status, content_kind,
                            normalized_name, tmdb_id, series_tmdb_id, season_number, episode_number)
                           VALUES (%s, %s, %s, 'registered', 'episode', %s, %s, %s, %s, %s)""",
                        (str(file_path), file_path.name, file_path.stat().st_size,
                         ep_normalized, series_tmdb_id, series_tmdb_id, season_num, episode_num),
                    )
        finally:
            db.put_conn(conn)

        logger.info("registered episode: %s S%02dE%02d", canonical_title, season_num, episode_num)
```

- [ ] **Step 5: Update scanner claim API to return series fields**

In `scanner/scanner/api/server.py`, update the claim query to include the new columns. Find the SELECT statement for claim and add `content_kind, series_tmdb_id, season_number, episode_number` to the response.

- [ ] **Step 6: Commit**

```bash
git add scanner/scanner/migrations/005_series_support.sql scanner/scanner/services/series_detect.py scanner/scanner/loops/scan_loop.py scanner/scanner/services/metadata.py scanner/scanner/api/server.py
git commit -m "feat(scanner): add series folder detection and episode registration"
```

---

## Task 14: Player — Audio Track Selector

**Files:**
- Modify: `player/src/app/PlayerClient.tsx`

- [ ] **Step 1: Add audio track state and interface**

After the `QualityLevel` interface (line 20), add:

```typescript
interface AudioTrackInfo {
  index: number
  language?: string
  label?: string
  is_default: boolean
}
```

Inside `PlayerClient`, add state:

```typescript
const [audioTracks, setAudioTracks] = useState<AudioTrackInfo[]>([])
const [selectedAudio, setSelectedAudio] = useState<number>(0)
const [showAudioMenu, setShowAudioMenu] = useState(false)
```

- [ ] **Step 2: Populate audio tracks from hls.js MANIFEST_PARSED**

Inside the `MANIFEST_PARSED` handler (after line 175), add:

```typescript
        // Extract audio tracks from hls.js
        const hlsAudioTracks = (hls.audioTracks || []).map(
          (track: any, idx: number) => ({
            index: idx,
            language: track.lang || undefined,
            label: track.name || track.lang || `Track ${idx + 1}`,
            is_default: idx === 0,
          })
        )
        setAudioTracks(hlsAudioTracks)
```

- [ ] **Step 3: Add audio track switcher callback**

```typescript
  const applyAudioTrack = useCallback((index: number) => {
    setSelectedAudio(index)
    setShowAudioMenu(false)
    if (hlsRef.current) {
      hlsRef.current.audioTrack = index
    }
  }, [])
```

- [ ] **Step 4: Add audio menu UI in the settings panel**

Inside the `settings-menu` div (after the quality buttons, around line 515), add:

```tsx
            {audioTracks.length > 1 && (
              <>
                <div className="settings-section-label">Озвучка</div>
                {audioTracks.map((t) => (
                  <button
                    key={t.index}
                    type="button"
                    className={`quality-item${selectedAudio === t.index ? ' is-active' : ''}`}
                    onClick={(e) => { e.stopPropagation(); applyAudioTrack(t.index) }}
                  >
                    {t.label || SUBTITLE_LABELS[t.language ?? ''] || `Track ${t.index + 1}`}
                  </button>
                ))}
              </>
            )}
```

- [ ] **Step 5: Update click-outside handler to also close audio menu**

```typescript
    const handleClick = (e: MouseEvent) => {
      if (quickbarRef.current && !quickbarRef.current.contains(e.target as Node)) {
        setShowQualityMenu(false)
        setShowAudioMenu(false)
      }
    }
```

- [ ] **Step 6: Verify build**

Run: `cd player && npm run build`
Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
git add player/src/app/PlayerClient.tsx
git commit -m "feat(player): add audio track selector UI"
```

---

## Task 15: Player — Series Navigation Mode

**Files:**
- Modify: `player/src/app/page.tsx`
- Create: `player/src/app/SeriesPlayer.tsx`

- [ ] **Step 1: Update page.tsx to handle series routing**

```tsx
import PlayerClient, { type MovieResponse } from './PlayerClient'
import SeriesPlayer from './SeriesPlayer'

const API_URL = process.env.API_URL ?? 'http://localhost:8000'
const PLAYER_KEY = process.env.PLAYER_KEY ?? ''

type PageProps = {
  searchParams: { imdb_id?: string; tmdb_id?: string; type?: string; s?: string; e?: string; nav?: string }
}

async function fetchMovieData(imdbId?: string, tmdbId?: string): Promise<{ data?: MovieResponse; error?: string }> {
  if (!imdbId && !tmdbId) {
    return { error: 'No movie ID provided. Use ?imdb_id= or ?tmdb_id= query param.' }
  }
  const params = new URLSearchParams()
  if (imdbId) params.set('imdb_id', imdbId)
  else if (tmdbId) params.set('tmdb_id', tmdbId)

  try {
    const res = await fetch(`${API_URL}/api/player/movie?${params.toString()}`, {
      headers: { 'X-Player-Key': PLAYER_KEY },
      cache: 'no-store',
    })
    if (!res.ok) return { error: `API error ${res.status}` }
    return { data: (await res.json()) as MovieResponse }
  } catch {
    return { error: 'Failed to reach API' }
  }
}

async function fetchSeriesData(tmdbId: string, s?: string, e?: string) {
  try {
    const endpoint = s && e
      ? `${API_URL}/api/player/episode?tmdb_id=${tmdbId}&s=${s}&e=${e}`
      : `${API_URL}/api/player/series?tmdb_id=${tmdbId}`
    const res = await fetch(endpoint, {
      headers: { 'X-Player-Key': PLAYER_KEY },
      cache: 'no-store',
    })
    if (!res.ok) return { error: `API error ${res.status}` }
    return { data: await res.json() }
  } catch {
    return { error: 'Failed to reach API' }
  }
}

export default async function Page({ searchParams }: PageProps) {
  if (searchParams.type === 'series' && searchParams.tmdb_id) {
    const { data, error } = await fetchSeriesData(searchParams.tmdb_id, searchParams.s, searchParams.e)
    if (error) return <div className="player-status">{error}</div>
    if (!data) return <div className="player-status">No data</div>
    const hideNav = searchParams.nav === '0' || !!(searchParams.s && searchParams.e)
    return <SeriesPlayer initialData={data} hideNavigation={hideNav} />
  }

  const { data, error } = await fetchMovieData(searchParams.imdb_id, searchParams.tmdb_id)
  if (error) return <div className="player-status">{error}</div>
  if (!data) return <div className="player-status">No data</div>
  return <PlayerClient initialData={data} />
}
```

- [ ] **Step 2: Create SeriesPlayer component**

This is a large component. Create `player/src/app/SeriesPlayer.tsx` with:
- Season dropdown selector
- Episode list within selected season
- Reuse the existing PlayerClient for the actual video playback
- Prev/Next episode buttons
- Pass the selected episode's HLS URL to the video player

The component receives the full series data (seasons + episodes) and renders navigation UI around the player.

Key structure:
```tsx
'use client'

import { useState, useCallback } from 'react'
import PlayerClient, { type MovieResponse } from './PlayerClient'

interface SeriesData {
  data: {
    series: { id: number; tmdb_id: string; title: string; year?: number; poster_url?: string }
    seasons: Array<{
      season_number: number
      poster_url?: string
      episodes: Array<{
        episode_number: number
        title?: string
        playback?: { hls: string }
        assets?: { thumbnail: string }
        subtitles?: Array<{ language: string; url: string }>
        audio_tracks?: Array<{ index: number; language?: string; label?: string; is_default: boolean }>
      }>
    }>
  }
}

interface EpisodeData {
  data: {
    episode_number: number
    season_number: number
    title?: string
    series: { tmdb_id: string; title: string }
    playback?: { hls: string }
    assets?: { thumbnail: string }
    subtitles?: Array<{ language: string; url: string }>
    audio_tracks?: Array<{ index: number; language?: string; label?: string; is_default: boolean }>
  }
}

export default function SeriesPlayer({
  initialData,
  hideNavigation = false,
}: {
  initialData: SeriesData | EpisodeData
  hideNavigation?: boolean
}) {
  const isSingleEpisode = 'episode_number' in (initialData as any).data

  if (isSingleEpisode || hideNavigation) {
    const ep = (initialData as EpisodeData).data
    if (!ep.playback?.hls) return <div className="player-status">Episode not ready</div>
    const movieResponse: MovieResponse = {
      data: {
        movie: { id: 0, imdb_id: '', tmdb_id: ep.series.tmdb_id },
        playback: { hls: ep.playback.hls },
        assets: { poster: ep.assets?.thumbnail ?? '' },
        subtitles: ep.subtitles,
      },
      meta: { version: 'v1' },
    }
    return <PlayerClient initialData={movieResponse} />
  }

  return <SeriesNavigator data={(initialData as SeriesData).data} />
}

function SeriesNavigator({ data }: { data: SeriesData['data'] }) {
  const [selectedSeason, setSelectedSeason] = useState(data.seasons[0]?.season_number ?? 1)
  const [selectedEpisode, setSelectedEpisode] = useState(1)

  const season = data.seasons.find((s) => s.season_number === selectedSeason)
  const episode = season?.episodes.find((e) => e.episode_number === selectedEpisode)

  const goToEpisode = useCallback((s: number, e: number) => {
    setSelectedSeason(s)
    setSelectedEpisode(e)
  }, [])

  const prevEpisode = useCallback(() => {
    if (!season) return
    const idx = season.episodes.findIndex((e) => e.episode_number === selectedEpisode)
    if (idx > 0) {
      setSelectedEpisode(season.episodes[idx - 1].episode_number)
    } else {
      // Go to prev season last episode
      const sIdx = data.seasons.findIndex((s) => s.season_number === selectedSeason)
      if (sIdx > 0) {
        const prevS = data.seasons[sIdx - 1]
        setSelectedSeason(prevS.season_number)
        setSelectedEpisode(prevS.episodes[prevS.episodes.length - 1]?.episode_number ?? 1)
      }
    }
  }, [data.seasons, season, selectedSeason, selectedEpisode])

  const nextEpisode = useCallback(() => {
    if (!season) return
    const idx = season.episodes.findIndex((e) => e.episode_number === selectedEpisode)
    if (idx < season.episodes.length - 1) {
      setSelectedEpisode(season.episodes[idx + 1].episode_number)
    } else {
      const sIdx = data.seasons.findIndex((s) => s.season_number === selectedSeason)
      if (sIdx < data.seasons.length - 1) {
        const nextS = data.seasons[sIdx + 1]
        setSelectedSeason(nextS.season_number)
        setSelectedEpisode(nextS.episodes[0]?.episode_number ?? 1)
      }
    }
  }, [data.seasons, season, selectedSeason, selectedEpisode])

  if (!episode?.playback?.hls) {
    return <div className="player-status">Episode not ready</div>
  }

  const movieResponse: MovieResponse = {
    data: {
      movie: { id: 0, imdb_id: '', tmdb_id: data.series.tmdb_id ?? '' },
      playback: { hls: episode.playback.hls },
      assets: { poster: episode.assets?.thumbnail ?? '' },
      subtitles: episode.subtitles,
    },
    meta: { version: 'v1' },
  }

  return (
    <div className="series-player-wrapper">
      <div className="series-nav">
        <div className="series-title">{data.series.title}</div>
        <div className="series-selectors">
          <select
            value={selectedSeason}
            onChange={(e) => goToEpisode(Number(e.target.value), 1)}
            className="series-select"
          >
            {data.seasons.map((s) => (
              <option key={s.season_number} value={s.season_number}>
                Сезон {s.season_number}
              </option>
            ))}
          </select>
          <select
            value={selectedEpisode}
            onChange={(e) => setSelectedEpisode(Number(e.target.value))}
            className="series-select"
          >
            {season?.episodes.map((ep) => (
              <option key={ep.episode_number} value={ep.episode_number}>
                {ep.episode_number}. {ep.title || `Серия ${ep.episode_number}`}
              </option>
            ))}
          </select>
        </div>
        <div className="series-ep-nav">
          <button type="button" onClick={prevEpisode} className="ep-nav-btn">← Пред.</button>
          <button type="button" onClick={nextEpisode} className="ep-nav-btn">След. →</button>
        </div>
      </div>
      <PlayerClient key={`s${selectedSeason}e${selectedEpisode}`} initialData={movieResponse} />
    </div>
  )
}
```

- [ ] **Step 3: Add series CSS**

In `player/src/app/globals.css`, add:

```css
.series-player-wrapper { width: 100%; }
.series-nav { display: flex; align-items: center; gap: 12px; padding: 8px 0; flex-wrap: wrap; }
.series-title { font-weight: 600; font-size: 14px; color: #fff; }
.series-selectors { display: flex; gap: 8px; }
.series-select { background: #1a1a2e; color: #fff; border: 1px solid #333; border-radius: 4px; padding: 4px 8px; font-size: 13px; }
.series-ep-nav { display: flex; gap: 4px; margin-left: auto; }
.ep-nav-btn { background: #1a1a2e; color: #ccc; border: 1px solid #333; border-radius: 4px; padding: 4px 12px; font-size: 12px; cursor: pointer; }
.ep-nav-btn:hover { background: #2a2a4e; color: #fff; }
```

- [ ] **Step 4: Verify build**

Run: `cd player && npm run build`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add player/src/app/page.tsx player/src/app/SeriesPlayer.tsx player/src/app/globals.css
git commit -m "feat(player): add series navigation mode with season/episode selectors"
```

---

## Task 16: Frontend — Series Types + Nav

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/components/Nav.tsx`

- [ ] **Step 1: Update ContentType and add series interfaces**

In `frontend/src/types/index.ts`:

Change line 4:
```typescript
export type ContentType = 'movie' | 'series'
```

Add at the end of file:

```typescript
export interface Series {
  id: number
  storage_key: string
  tmdb_id?: string
  imdb_id?: string
  title: string
  year?: number
  poster_url?: string
  created_at: string
  updated_at: string
}

export interface SeriesResponse {
  items: Series[]
  next_cursor: string | null
}

export interface Season {
  id: number
  series_id: number
  season_number: number
  poster_url?: string
}

export interface Episode {
  id: number
  season_id: number
  episode_number: number
  title?: string
  storage_key: string
  created_at: string
  updated_at: string
}

export interface SeriesDetailResponse {
  series: Series
  seasons: Array<{
    id: number
    season_number: number
    poster_url?: string
    episodes: Episode[]
  }>
}
```

- [ ] **Step 2: Add "Сериалы" to Nav**

Read `frontend/src/components/Nav.tsx`, find the navigation items, and add a link to `/series` next to the existing `/movies` link.

- [ ] **Step 3: Verify build**

Run: `cd frontend && npm run build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/components/Nav.tsx
git commit -m "feat(frontend): add series types and navigation item"
```

---

## Task 17: Frontend — Series List Page

**Files:**
- Create: `frontend/src/app/series/page.tsx`

- [ ] **Step 1: Create series list page**

This page should mirror the existing `movies/page.tsx` pattern:
- Fetch series from `GET /api/admin/series`
- Display in a table: poster, title, year, tmdb_id
- Cursor pagination
- Link to series detail page

Follow the existing code patterns from `movies/page.tsx` exactly, but fetch from `/api/admin/series` and use `Series` type.

- [ ] **Step 2: Create series detail page**

Create `frontend/src/app/series/[id]/page.tsx`:
- Fetch from `GET /api/admin/series/{id}`
- Show series metadata
- Accordion of seasons → episode list with conversion status
- Delete series button

- [ ] **Step 3: Verify build**

Run: `cd frontend && npm run build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/app/series/
git commit -m "feat(frontend): add series list and detail pages"
```

---

## Task 18: Wire Up — Worker Main + API Main

**Files:**
- Modify: `worker/cmd/worker/main.go` — add series and audio track repos, pass to converter
- Modify: `api/cmd/api/main.go` — add series repos, create handlers, pass to Dependencies

- [ ] **Step 1: Update worker main**

Find where repositories are created and the converter `New()` is called. Add:

```go
seriesRepo := repository.NewSeriesRepository(pool)
audioTrackRepo := repository.NewAudioTrackRepository(pool)
```

Pass them to `converter.New(...)` and `ingest.New(...)`.

- [ ] **Step 2: Update API main**

Find where handlers are created and Dependencies struct is built. Add:

```go
seriesRepo := repository.NewSeriesRepository(pool)
audioTrackRepo := repository.NewAudioTrackRepository(pool)
epSubtitleRepo := repository.NewEpisodeSubtitleRepository(pool)
seriesHandler := handler.NewSeriesHandler(seriesRepo)
```

Update `NewPlayerHandler` call to pass the new repos.
Add `SeriesHandler` to the `Dependencies` struct.

- [ ] **Step 3: Verify both services compile**

Run: `cd api && go build ./cmd/api/ && cd ../worker && go build ./cmd/worker/`
Expected: Both build successfully.

- [ ] **Step 4: Commit**

```bash
git add api/cmd/api/main.go worker/cmd/worker/main.go
git commit -m "feat(api,worker): wire up series and audio track dependencies"
```

---

## Task 19: Documentation Updates

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/contracts/api.md`
- Modify: `docs/architecture/database-schema.md`
- Modify: `REPO_MAP.md`
- Create: ADR for series architecture decision

- [ ] **Step 1: Update CHANGELOG.md**

Add under `## [Unreleased]`:

```markdown
### Added
- `api/internal/db/migrations/014_series_and_audio_tracks.sql`: series, seasons, episodes, episode_assets, episode_subtitles, audio_tracks tables
- `api/internal/model/series.go`: Series, Season, Episode, AudioTrack model structs
- `api/internal/repository/series.go`: Series CRUD repository
- `api/internal/repository/audio_track.go`: Audio track repository
- `api/internal/repository/episode_subtitle.go`: Episode subtitle repository
- `api/internal/handler/series.go`: Admin series CRUD endpoints
- `api/internal/handler/player.go`: Player series/episode endpoints, audio tracks in movie response
- `worker/internal/model/series.go`: Worker-side series model structs
- `worker/internal/ffmpeg/probe.go`: Multi-audio stream probing
- `worker/internal/repository/series.go`: Worker series/episode upsert
- `worker/internal/repository/audio_track.go`: Worker audio track storage
- `scanner/scanner/migrations/005_series_support.sql`: Scanner series columns
- `scanner/scanner/services/series_detect.py`: Series folder detection via GuessIt
- `player/src/app/SeriesPlayer.tsx`: Series navigation player component
- `frontend/src/app/series/page.tsx`: Admin series list page
- `frontend/src/app/series/[id]/page.tsx`: Admin series detail page

### Changed
- `worker/internal/ffmpeg/runner.go`: Multi-audio HLS encoding (all audio tracks preserved)
- `worker/internal/converter/converter.go`: Series output path branch, audio track saving
- `worker/internal/ingest/worker.go`: Series ingest support
- `scanner/scanner/loops/scan_loop.py`: Series folder detection
- `scanner/scanner/services/metadata.py`: TMDB TV search
- `player/src/app/PlayerClient.tsx`: Audio track selector UI
- `player/src/app/page.tsx`: Series/movie routing by type param
- `frontend/src/types/index.ts`: Series types, ContentType updated
- `frontend/src/components/Nav.tsx`: Series navigation item
```

- [ ] **Step 2: Update API contracts**

Update `docs/contracts/api.md` with the new admin and player endpoints from Task 11 and Task 10.

- [ ] **Step 3: Create ADR**

Run: `./scripts/new-adr.sh "Separate tables for series support"`

Fill in: decision to use separate series/seasons/episodes tables rather than polymorphic movies table. Rationale: clean model, no nullable sprawl, doesn't break existing movie code.

- [ ] **Step 4: Update REPO_MAP.md**

Add new files and directories.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md docs/ REPO_MAP.md
git commit -m "docs: update changelog, contracts, ADR, and repo map for series support"
```

---

## Dependency Graph

```
Task 1 (DB migration)
├── Task 2 (API models) ─── Task 9 (API repos) ─── Task 10 (Player handler) ──┐
│                                                    Task 11 (Admin handler) ───┤
│                                                                               ├── Task 12 (Routes)
├── Task 3 (Worker models) ── Task 4 (Probe) ── Task 5 (FFmpeg multi-audio) ──┤
│                              Task 6 (Worker repos) ── Task 7 (Converter) ────┤
│                                                       Task 8 (Ingest) ───────┘
│
├── Task 13 (Scanner) ── independent
├── Task 14 (Player audio UI) ── depends on Task 5
├── Task 15 (Player series UI) ── depends on Task 10
├── Task 16-17 (Frontend) ── depends on Task 11, 12
├── Task 18 (Wiring) ── depends on all above
└── Task 19 (Docs) ── last
```

**Parallel work possible:**
- Tasks 2 + 3 (API models + Worker models)
- Tasks 4 + 9 (FFmpeg probe + API repos)
- Tasks 13 + 14 + 16 (Scanner + Player audio + Frontend types)
