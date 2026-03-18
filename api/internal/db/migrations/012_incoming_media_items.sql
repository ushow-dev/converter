-- 012_incoming_media_items.sql
-- Ingest queue: files registered by the scanner and claimed by the ingest worker.

CREATE TABLE IF NOT EXISTS incoming_media_items (
    id                      BIGSERIAL    PRIMARY KEY,
    source_path             TEXT         NOT NULL,
    source_filename         TEXT         NOT NULL,
    normalized_name         TEXT,
    tmdb_id                 TEXT,
    content_kind            TEXT         NOT NULL DEFAULT 'movie',
    file_size_bytes         BIGINT,
    stable_since            TIMESTAMPTZ,
    status                  TEXT         NOT NULL DEFAULT 'new',
    attempts                INT          NOT NULL DEFAULT 0,
    claimed_at              TIMESTAMPTZ,
    claim_expires_at        TIMESTAMPTZ,
    quality_score           INT,
    is_upgrade_candidate    BOOLEAN      NOT NULL DEFAULT FALSE,
    duplicate_of_movie_id   BIGINT       REFERENCES movies(id) ON DELETE SET NULL,
    review_reason           TEXT,
    api_job_id              TEXT,
    error_message           TEXT,
    local_path              TEXT,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT incoming_status_check CHECK (status IN (
        'new','claimed','copying','copied','completed',
        'failed','skipped','review_duplicate','review_unknown_quality','upgrade_candidate'
    ))
);

CREATE UNIQUE INDEX IF NOT EXISTS incoming_media_items_source_path_key
    ON incoming_media_items (source_path);

CREATE INDEX IF NOT EXISTS incoming_media_items_status_idx
    ON incoming_media_items (status, stable_since);

CREATE INDEX IF NOT EXISTS incoming_media_items_claim_expires_idx
    ON incoming_media_items (claim_expires_at)
    WHERE status = 'claimed';
