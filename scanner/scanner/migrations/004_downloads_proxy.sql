-- Add proxy_url column to scanner_downloads for per-download proxy support.
ALTER TABLE scanner_downloads ADD COLUMN IF NOT EXISTS proxy_url TEXT;
