-- Phase: strict current schema (no legacy compatibility layer)

-- Remove legacy rows that cannot be linked to a movie explicitly.
DELETE FROM media_assets WHERE movie_id IS NULL;

-- Current implementation requires explicit asset -> movie relation.
ALTER TABLE media_assets ALTER COLUMN movie_id SET NOT NULL;

-- One conversion job produces at most one playable asset.
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_assets_job_id_unique
    ON media_assets (job_id);
