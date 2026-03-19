CREATE TABLE IF NOT EXISTS scanner_downloads (
    id              SERIAL PRIMARY KEY,
    url             TEXT NOT NULL,
    filename        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'queued', -- queued, downloading, done, failed
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
