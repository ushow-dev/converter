CREATE TABLE IF NOT EXISTS movie_subtitles (
    id           BIGSERIAL    PRIMARY KEY,
    movie_id     BIGINT       NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    language     TEXT         NOT NULL,
    source       TEXT         NOT NULL,
    storage_path TEXT         NOT NULL,
    external_id  TEXT,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (movie_id, language)
);

CREATE INDEX IF NOT EXISTS idx_movie_subtitles_movie_id ON movie_subtitles(movie_id);
