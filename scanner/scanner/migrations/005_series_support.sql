ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS content_kind TEXT NOT NULL DEFAULT 'movie';
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS series_tmdb_id TEXT;
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS season_number INT;
ALTER TABLE scanner_incoming_items ADD COLUMN IF NOT EXISTS episode_number INT;
