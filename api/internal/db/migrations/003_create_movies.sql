-- Phase: movies catalog metadata for converted assets
CREATE TABLE IF NOT EXISTS movies (
    id         BIGSERIAL   PRIMARY KEY,
    imdb_id    TEXT        NOT NULL,
    tmdb_id    TEXT        NOT NULL,
    poster_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (imdb_id, tmdb_id)
);

CREATE INDEX IF NOT EXISTS idx_movies_imdb_id ON movies (imdb_id);
CREATE INDEX IF NOT EXISTS idx_movies_tmdb_id ON movies (tmdb_id);
