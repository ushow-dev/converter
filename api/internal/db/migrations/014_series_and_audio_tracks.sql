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
