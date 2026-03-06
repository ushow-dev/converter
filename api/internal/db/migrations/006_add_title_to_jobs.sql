-- Phase: add title column to media_jobs for direct storage (used by upload flow)
ALTER TABLE media_jobs ADD COLUMN IF NOT EXISTS title TEXT;
