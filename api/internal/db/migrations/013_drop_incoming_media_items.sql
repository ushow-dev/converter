-- 013_drop_incoming_media_items.sql
-- Remove the ingest queue table from the converter DB.
-- The scanner service now owns its own scanner_incoming_items table.

DROP INDEX IF EXISTS incoming_media_items_claim_expires_idx;
DROP INDEX IF EXISTS incoming_media_items_status_idx;
DROP INDEX IF EXISTS incoming_media_items_source_path_key;
DROP TABLE IF EXISTS incoming_media_items;
