-- Phase: HLS conversion — add thumbnail_path to media_assets
ALTER TABLE media_assets ADD COLUMN IF NOT EXISTS thumbnail_path TEXT;
