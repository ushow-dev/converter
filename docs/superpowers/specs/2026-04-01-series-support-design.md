# Series Support Design

**Date:** 2026-04-01
**Status:** Draft
**Scope:** Scanner, Worker, API, Player, Frontend — end-to-end TV series support + multi-audio for all content

---

## Overview

Add TV series support to the media processing pipeline. Series are ingested as folders containing episodes organized by seasons. Each episode is converted individually (one job per episode). The player supports full series navigation (season/episode selection) and per-episode audio track switching.

Multi-audio support applies to both movies and series — all audio tracks from the source file are preserved during conversion.

---

## 1. Database Schema

### New Tables

```sql
-- Series container
CREATE TABLE series (
  id BIGSERIAL PRIMARY KEY,
  storage_key TEXT UNIQUE NOT NULL,        -- "breaking_bad_2008_[1396]"
  tmdb_id TEXT,
  imdb_id TEXT,
  title TEXT NOT NULL,
  year INT,
  poster_url TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Season grouping
CREATE TABLE seasons (
  id BIGSERIAL PRIMARY KEY,
  series_id BIGINT NOT NULL REFERENCES series ON DELETE CASCADE,
  season_number INT NOT NULL,
  poster_url TEXT,
  UNIQUE(series_id, season_number)
);

-- Individual episode
CREATE TABLE episodes (
  id BIGSERIAL PRIMARY KEY,
  season_id BIGINT NOT NULL REFERENCES seasons ON DELETE CASCADE,
  episode_number INT NOT NULL,
  title TEXT,
  storage_key TEXT UNIQUE NOT NULL,        -- "breaking_bad_2008_[1396]_s01e01"
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(season_id, episode_number)
);

-- HLS assets per episode
CREATE TABLE episode_assets (
  asset_id TEXT PRIMARY KEY,
  job_id TEXT REFERENCES media_jobs,
  episode_id BIGINT NOT NULL REFERENCES episodes ON DELETE CASCADE,
  storage_path TEXT NOT NULL,
  duration_sec INT,
  video_codec TEXT,
  audio_codec TEXT,
  is_ready BOOLEAN DEFAULT false
);

-- Subtitles per episode
CREATE TABLE episode_subtitles (
  id BIGSERIAL PRIMARY KEY,
  episode_id BIGINT NOT NULL REFERENCES episodes ON DELETE CASCADE,
  language TEXT NOT NULL,
  source TEXT DEFAULT 'opensubtitles',
  storage_path TEXT NOT NULL,
  UNIQUE(episode_id, language)
);

-- Audio tracks (shared for movies and episodes)
CREATE TABLE audio_tracks (
  id BIGSERIAL PRIMARY KEY,
  asset_id TEXT NOT NULL,                  -- FK to media_assets.asset_id OR episode_assets.asset_id
  asset_type TEXT NOT NULL,                -- 'movie' | 'episode'
  track_index INT NOT NULL,                -- 0, 1, 2...
  language TEXT,                           -- ISO 639-1
  label TEXT,                              -- human-readable from stream metadata
  is_default BOOLEAN DEFAULT false,
  UNIQUE(asset_id, asset_type, track_index)
);
```

### Changes to Existing Tables

```sql
-- media_jobs: add series context
ALTER TABLE media_jobs ADD COLUMN content_type TEXT DEFAULT 'movie';  -- 'movie' | 'series'
ALTER TABLE media_jobs ADD COLUMN series_id BIGINT REFERENCES series;
ALTER TABLE media_jobs ADD COLUMN season_number INT;
ALTER TABLE media_jobs ADD COLUMN episode_number INT;
```

---

## 2. Media Storage Layout

```
/media/converted/
├── movies/                              -- unchanged
│   └── {storageKey}/
│       ├── master.m3u8
│       ├── 720/, 480/, 360/
│       ├── thumbnail.jpg
│       └── subtitles/
│
└── series/
    └── {series_storage_key}/            -- "breaking_bad_2008_[1396]"
        ├── poster.jpg
        ├── s01/
        │   ├── poster.jpg              -- season poster (optional)
        │   ├── e01/
        │   │   ├── master.m3u8
        │   │   ├── 720/, 480/, 360/
        │   │   ├── thumbnail.jpg
        │   │   └── subtitles/
        │   ├── e02/
        │   │   └── ...
        │   └── ...
        ├── s02/
        │   └── ...
        └── ...
```

---

## 3. FFmpeg Pipeline — Multi-Audio

### Current (single audio)

```
-map [v720] -map 0:a:0
-map [v480] -map 0:a:0
-map [v360] -map 0:a:0
-var_stream_map "v:0,a:0 v:1,a:1 v:2,a:2"
```

### New (N audio tracks)

1. **Probe**: ffprobe enumerates all audio streams with language/title metadata
2. **Map**: for each video variant, map all audio streams

```
# Example: 2 audio tracks × 3 video = 6 streams
-map [v720] -map 0:a:0 -map 0:a:1
-map [v480] -map 0:a:0 -map 0:a:1
-map [v360] -map 0:a:0 -map 0:a:1
-var_stream_map "v:0,a:0,a:1 v:1,a:2,a:3 v:2,a:4,a:5"
```

3. **Metadata**: `-metadata:s:a:N language=ru` for each audio stream
4. **Result**: master.m3u8 contains `#EXT-X-MEDIA:TYPE=AUDIO` entries per language
5. **Post-convert**: save track metadata to `audio_tracks` table

This applies to both movies and series — content_type only affects the output path.

---

## 4. Scanner — Series Detection

### Detection Logic

1. `incoming/` scan finds a directory with video files
2. GuessIt parses filenames — if `type: "episode"` detected → series
3. Supports both folder structures:
   - **Strict hierarchy**: `Show/Season N/Show.S01E01.mkv`
   - **Flat**: `Show/Show.S01E01.mkv` (season from filename)
4. TMDB TV search by parsed title → fetch series metadata, episode titles, posters

### Scanner DB Changes

```sql
ALTER TABLE scanner_incoming_items ADD COLUMN content_kind TEXT DEFAULT 'movie';  -- 'movie' | 'episode'
ALTER TABLE scanner_incoming_items ADD COLUMN series_tmdb_id TEXT;
ALTER TABLE scanner_incoming_items ADD COLUMN season_number INT;
ALTER TABLE scanner_incoming_items ADD COLUMN episode_number INT;
```

### Processing

- One `scanner_incoming_items` row per episode (content_kind = 'episode')
- `normalized_name` = `"breaking_bad_2008_[1396]_s01e01"` (unique per episode)
- Quality score computed per episode file

### Claim / Ingest

IngestWorker on claim:
1. Upsert `series` by tmdb_id
2. Upsert `season` by (series_id, season_number)
3. Create `episode` record
4. Create `media_job` with content_type='series', series_id, season_number, episode_number
5. Push to `convert_queue` with series fields in payload
6. Output path: `/media/converted/series/{storage_key}/s{NN}/e{NN}/`

---

## 5. Queue Payloads

### ConvertMessage (v2)

```json
{
  "schema_version": "v2",
  "job_id": "job_...",
  "job_type": "convert",
  "content_type": "series",
  "payload": {
    "input_path": "/media/downloads/job_.../Breaking.Bad.S01E01.mkv",
    "output_path": "/media/temp/job_...",
    "output_profile": "mp4_h264_aac_1080p",
    "final_dir": "/media/converted/series/breaking_bad_2008_[1396]/s01/e01",
    "storage_key": "breaking_bad_2008_[1396]_s01e01",
    "series_id": 1,
    "season_number": 1,
    "episode_number": 1,
    "tmdb_id": "1396",
    "imdb_id": "tt0903747"
  }
}
```

New fields (`series_id`, `season_number`, `episode_number`) are nullable. For movies they remain null. Worker determines output table by `content_type`.

TransferMessage gets the same additional fields.

Backward compatibility: worker supports both v1 (movies) and v2 (with optional series fields).

---

## 6. API Endpoints

### Admin API

```
# Series CRUD
GET    /api/admin/series                                  — list (cursor pagination)
POST   /api/admin/series                                  — create manually
GET    /api/admin/series/{seriesId}                       — details + seasons
PATCH  /api/admin/series/{seriesId}                       — update metadata
DELETE /api/admin/series/{seriesId}                       — delete + all files

# Seasons
GET    /api/admin/series/{seriesId}/seasons               — list seasons
GET    /api/admin/series/{seriesId}/seasons/{seasonNum}    — episodes in season

# Episodes
GET    /api/admin/episodes/{episodeId}                    — episode details
DELETE /api/admin/episodes/{episodeId}                    — delete + files

# Episode subtitles
GET    /api/admin/episodes/{episodeId}/subtitles
POST   /api/admin/episodes/{episodeId}/subtitles
POST   /api/admin/episodes/{episodeId}/subtitles/search

# Folder-based ingest
POST   /api/admin/series/scan-folder                     — scan paths, return found episodes
POST   /api/admin/series/ingest                          — start conversion for selected episodes

# TMDB TV search
GET    /api/admin/series/tmdb/search                     — search by title
GET    /api/admin/series/tmdb/{tmdbId}                   — series metadata
```

### Player API

```
# Full navigation (mode A — default)
GET    /api/player/series?tmdb_id=1396                   — series + all seasons + episodes

# Specific episode (mode B — embed)
GET    /api/player/episode?tmdb_id=1396&s=1&e=1          — single episode for playback
```

### Player Series Response Format

```json
{
  "data": {
    "series": { "id": 1, "tmdb_id": "1396", "title": "Breaking Bad", "year": 2008, "poster_url": "..." },
    "seasons": [
      {
        "season_number": 1,
        "poster_url": "...",
        "episodes": [
          {
            "episode_number": 1,
            "title": "Pilot",
            "playback": { "hls": "/media/series/.../s01/e01/master.m3u8" },
            "assets": { "thumbnail": "/media/series/.../s01/e01/thumbnail.jpg" },
            "subtitles": [
              { "language": "en", "url": "/media/series/.../s01/e01/subtitles/en.vtt" }
            ],
            "audio_tracks": [
              { "index": 0, "language": "en", "label": "English", "is_default": true },
              { "index": 1, "language": "ru", "label": "Русский" }
            ]
          }
        ]
      }
    ]
  },
  "meta": { "version": "v1" }
}
```

---

## 7. Player UI

### Two Modes

**Mode A (standalone)** — URL: `/watch?tmdb_id=1396&type=series`
- Loads full series via `GET /api/player/series?tmdb_id=1396`
- Season selector dropdown + episode list
- Prev/next episode buttons
- Audio track selector

**Mode B (embed)** — URL: `/watch?tmdb_id=1396&type=series&s=1&e=3`
- Loads single episode via `GET /api/player/episode?tmdb_id=1396&s=1&e=3`
- Season/episode navigation hidden
- Audio track selector still available
- Optional: `&nav=0` hides prev/next buttons

### URL Parameter `type`

- `type=movie` or omitted — current behavior
- `type=series` — series mode

### New UI Elements

1. **Season/episode selector** — dropdown or side panel, mode A only
2. **Audio track selector** — custom dropdown next to quality menu, uses `hls.audioTracks` / `hls.audioTrack = index`. Works for both movies and series.
3. **Prev/next buttons** — overlay on video controls

---

## 8. Admin Frontend

### New Pages

1. **Series list** (`/series`) — table like `/movies`: poster, title, year, season/episode count, conversion status
2. **Series detail** (`/series/{id}`) — metadata + accordion of seasons → episode list with per-episode conversion progress
3. **Add series** — modal/page:
   - TMDB search (like movies)
   - Folder selection (file browser for source paths)
   - Preview of detected episodes (table: season, episode, filename, status)
   - "Start conversion" button — creates jobs for selected episodes

### Navigation

Add "Series" item to sidebar next to "Movies". Extend `ContentType` type: `'movie' | 'series'`.

### No Separate Episode Page

Episode management (subtitles, deletion, status) is within the series detail page. No dedicated route needed.

---

## 9. Decisions

| Decision | Rationale |
|---|---|
| Separate tables (not polymorphic) | Clean model, no nullable field sprawl, doesn't touch existing movie code |
| One job per episode | Natural fit for existing pipeline, parallel conversion, granular progress |
| All audio tracks preserved | User requested; FFmpeg + hls.js support it natively |
| Audio tracks table shared (asset_type discriminator) | Avoids duplicating table for movies vs episodes |
| TMDB for series metadata | Already integrated, good TV support |
| Two player modes (standalone/embed) | Covers both self-hosted and third-party embed use cases |
| GuessIt for episode parsing | Already used by scanner, handles S01E01 patterns well |
| schema_version v2 with backward compat | Non-breaking rollout, existing movie jobs unaffected |
