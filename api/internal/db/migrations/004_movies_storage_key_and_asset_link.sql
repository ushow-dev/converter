-- Phase: decouple storage from numeric movie id and allow optional external IDs

-- movies: stable storage key + nullable external IDs
ALTER TABLE movies ADD COLUMN IF NOT EXISTS storage_key TEXT;

UPDATE movies
SET storage_key = id::text
WHERE storage_key IS NULL OR storage_key = '';

ALTER TABLE movies ALTER COLUMN storage_key SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE table_schema = 'public'
          AND table_name = 'movies'
          AND constraint_name = 'movies_storage_key_key'
    ) THEN
        ALTER TABLE movies ADD CONSTRAINT movies_storage_key_key UNIQUE (storage_key);
    END IF;
END $$;

ALTER TABLE movies ALTER COLUMN imdb_id DROP NOT NULL;
ALTER TABLE movies ALTER COLUMN tmdb_id DROP NOT NULL;

ALTER TABLE movies DROP CONSTRAINT IF EXISTS movies_imdb_id_tmdb_id_key;
DROP INDEX IF EXISTS movies_imdb_id_tmdb_id_key;
DROP INDEX IF EXISTS idx_movies_imdb_id;
DROP INDEX IF EXISTS idx_movies_tmdb_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_movies_imdb_id_not_null
    ON movies (imdb_id) WHERE imdb_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_movies_tmdb_id_not_null
    ON movies (tmdb_id) WHERE tmdb_id IS NOT NULL;

-- media_assets: explicit FK to movies to avoid deriving relation from path
ALTER TABLE media_assets ADD COLUMN IF NOT EXISTS movie_id BIGINT;

UPDATE media_assets a
SET movie_id = m.id
FROM movies m
WHERE a.movie_id IS NULL
  AND a.storage_path ~ '/converted/[0-9]+/'
  AND m.id = substring(a.storage_path FROM '/converted/([0-9]+)/')::bigint;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE table_schema = 'public'
          AND table_name = 'media_assets'
          AND constraint_name = 'fk_media_assets_movie'
    ) THEN
        ALTER TABLE media_assets
            ADD CONSTRAINT fk_media_assets_movie
            FOREIGN KEY (movie_id) REFERENCES movies (id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_media_assets_movie_id ON media_assets (movie_id);
