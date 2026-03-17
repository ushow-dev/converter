-- Migration 010: remote storage locations + link movies to storage location

CREATE TABLE IF NOT EXISTS storage_locations (
    id         BIGSERIAL   PRIMARY KEY,
    name       TEXT        NOT NULL UNIQUE,
    type       TEXT        NOT NULL,           -- "sftp" | "s3" | "local"
    base_url   TEXT        NOT NULL DEFAULT '', -- HTTP base for player URLs; empty = not yet configured
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reserve id=1 for "local" (existing movies with NULL storage_location_id use local MEDIA_BASE_URL)
INSERT INTO storage_locations (id, name, type, base_url)
VALUES (1, 'local', 'local', '')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE movies
    ADD COLUMN IF NOT EXISTS storage_location_id BIGINT
    REFERENCES storage_locations(id) ON DELETE SET NULL;

-- NULL means local (default)
