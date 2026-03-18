-- scanner/scanner/migrations/001_initial.sql

CREATE TABLE IF NOT EXISTS scanner_incoming_items (
    id                              BIGSERIAL PRIMARY KEY,
    source_path                     TEXT NOT NULL UNIQUE,
    source_filename                 TEXT NOT NULL,
    file_size_bytes                 BIGINT,
    first_seen_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stable_since                    TIMESTAMPTZ,
    status                          TEXT NOT NULL DEFAULT 'new',
    -- new|registered|claimed|copying|copied|completed|archived
    -- |failed|review_duplicate|review_unknown_quality|skipped
    review_reason                   TEXT,
    is_upgrade_candidate            BOOLEAN NOT NULL DEFAULT FALSE,
    quality_score                   INTEGER,
    api_item_id                     BIGINT,
    duplicate_of_library_movie_id   BIGINT,
    tmdb_id                         TEXT,
    normalized_name                 TEXT,
    title                           TEXT,
    year                            INTEGER,
    error_message                   TEXT,
    library_relative_path           TEXT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_incoming_status_stable
    ON scanner_incoming_items (status, stable_since);
CREATE INDEX IF NOT EXISTS idx_incoming_dup
    ON scanner_incoming_items (duplicate_of_library_movie_id, status);

CREATE TABLE IF NOT EXISTS scanner_library_movies (
    id                      BIGSERIAL PRIMARY KEY,
    content_kind            TEXT NOT NULL DEFAULT 'movie',
    title                   TEXT NOT NULL,
    title_original          TEXT,
    normalized_name         TEXT NOT NULL UNIQUE,
    year                    INTEGER,
    tmdb_id                 TEXT,
    imdb_id                 TEXT,
    poster_url              TEXT,
    quality_score           INTEGER NOT NULL,
    quality_label           TEXT CHECK (quality_label IS NULL OR quality_label IN ('HD', 'SD')),
    library_relative_path   TEXT NOT NULL,
    file_size_bytes         BIGINT,
    status                  TEXT NOT NULL DEFAULT 'ready',
    -- ready | replaced | deprecated
    source_item_id          BIGINT REFERENCES scanner_incoming_items(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_library_tmdb
    ON scanner_library_movies (tmdb_id) WHERE tmdb_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_library_status
    ON scanner_library_movies (status, updated_at DESC);
