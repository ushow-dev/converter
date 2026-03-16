-- Migration 009: Move media_assets.storage_path from converted/<key>/... to converted/movies/<key>/...
-- Safe to re-run: REPLACE only touches rows that still have the old path prefix.

UPDATE media_assets
SET storage_path = REPLACE(storage_path, '/converted/', '/converted/movies/')
WHERE storage_path LIKE '%/converted/mov_%';

UPDATE media_assets
SET thumbnail_path = REPLACE(thumbnail_path, '/converted/', '/converted/movies/')
WHERE thumbnail_path IS NOT NULL
  AND thumbnail_path LIKE '%/converted/mov_%';

UPDATE movie_subtitles
SET storage_path = REPLACE(storage_path, '/converted/', '/converted/movies/')
WHERE storage_path LIKE '%/converted/mov_%';
