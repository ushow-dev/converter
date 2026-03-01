-- Phase 2: Core API — initial schema
-- Idempotent: uses IF NOT EXISTS throughout.

-- ── media_jobs ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS media_jobs (
    job_id           TEXT        PRIMARY KEY,
    content_type     TEXT        NOT NULL DEFAULT 'movie',
    source_type      TEXT        NOT NULL,
    source_ref       TEXT        NOT NULL,
    priority         TEXT        NOT NULL DEFAULT 'normal',
    status           TEXT        NOT NULL DEFAULT 'created',
    stage            TEXT,
    progress_percent INTEGER     NOT NULL DEFAULT 0,
    error_code       TEXT,
    error_message    TEXT,
    retryable        BOOLEAN,
    request_id       TEXT        UNIQUE,          -- idempotency key
    correlation_id   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_media_jobs_status     ON media_jobs (status);
CREATE INDEX IF NOT EXISTS idx_media_jobs_created_at ON media_jobs (created_at DESC);

-- ── media_assets ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS media_assets (
    asset_id     TEXT        PRIMARY KEY,
    job_id       TEXT        NOT NULL REFERENCES media_jobs (job_id),
    storage_path TEXT        NOT NULL,
    duration_sec INTEGER,
    video_codec  TEXT,
    audio_codec  TEXT,
    is_ready     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_media_assets_job_id ON media_assets (job_id);

-- ── job_events ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS job_events (
    event_id   TEXT        PRIMARY KEY,
    job_id     TEXT        NOT NULL REFERENCES media_jobs (job_id),
    event_type TEXT        NOT NULL,
    payload    JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_job_events_job_id ON job_events (job_id);

-- ── search_results ────────────────────────────────────────────────────────────
-- Cache of releases returned by indexer backends.
CREATE TABLE IF NOT EXISTS search_results (
    external_id  TEXT        PRIMARY KEY,
    title        TEXT        NOT NULL,
    source_type  TEXT        NOT NULL,
    source_ref   TEXT        NOT NULL,
    size_bytes   BIGINT      NOT NULL DEFAULT 0,
    seeders      INTEGER     NOT NULL DEFAULT 0,
    leechers     INTEGER     NOT NULL DEFAULT 0,
    indexer      TEXT        NOT NULL DEFAULT '',
    content_type TEXT        NOT NULL DEFAULT 'movie',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
