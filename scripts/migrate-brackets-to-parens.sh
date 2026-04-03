#!/bin/bash
# Migrate storage keys from [tmdb_id] to (tmdb_id) format.
# Run on each server that has data: API DB, Storage, Scanner.
#
# Usage:
#   ./scripts/migrate-brackets-to-parens.sh db-api      # Fix API PostgreSQL
#   ./scripts/migrate-brackets-to-parens.sh db-scanner   # Fix Scanner PostgreSQL
#   ./scripts/migrate-brackets-to-parens.sh fs-storage   # Rename folders on Storage server
#   ./scripts/migrate-brackets-to-parens.sh fs-scanner   # Rename folders on Scanner library

set -euo pipefail

case "${1:-}" in
  db-api)
    echo "=== Migrating API database ==="
    # movies.storage_key
    psql -U app -d mediadb -c "
      UPDATE movies SET storage_key = REPLACE(REPLACE(storage_key, '_[', '_('), ']', ')'), updated_at = NOW()
      WHERE storage_key LIKE '%[%]%';
    "
    # series.storage_key
    psql -U app -d mediadb -c "
      UPDATE series SET storage_key = REPLACE(REPLACE(storage_key, '_[', '_('), ']', ')'), updated_at = NOW()
      WHERE storage_key LIKE '%[%]%';
    "
    # episodes.storage_key
    psql -U app -d mediadb -c "
      UPDATE episodes SET storage_key = REPLACE(REPLACE(storage_key, '_[', '_('), ']', ')'), updated_at = NOW()
      WHERE storage_key LIKE '%[%]%';
    "
    # media_assets.storage_path and thumbnail_path
    psql -U app -d mediadb -c "
      UPDATE media_assets SET
        storage_path = REPLACE(REPLACE(storage_path, '_[', '_('), ']', ')'),
        thumbnail_path = REPLACE(REPLACE(thumbnail_path, '_[', '_('), ']', ')')
      WHERE storage_path LIKE '%[%]%';
    "
    # episode_assets.storage_path and thumbnail_path
    psql -U app -d mediadb -c "
      UPDATE episode_assets SET
        storage_path = REPLACE(REPLACE(storage_path, '_[', '_('), ']', ')'),
        thumbnail_path = REPLACE(REPLACE(thumbnail_path, '_[', '_('), ']', ')')
      WHERE storage_path LIKE '%[%]%';
    "
    echo "Done. Verify: SELECT storage_key FROM movies WHERE storage_key LIKE '%[%' LIMIT 5;"
    ;;

  db-scanner)
    echo "=== Migrating Scanner database ==="
    psql -U scanner -d scanner -c "
      UPDATE scanner_incoming_items SET
        normalized_name = REPLACE(REPLACE(normalized_name, '_[', '_('), ']', ')')
      WHERE normalized_name LIKE '%[%]%';
    "
    psql -U scanner -d scanner -c "
      UPDATE scanner_library_movies SET
        normalized_name = REPLACE(REPLACE(normalized_name, '_[', '_('), ']', ')'),
        library_relative_path = REPLACE(REPLACE(library_relative_path, '_[', '_('), ']', ')')
      WHERE normalized_name LIKE '%[%]%';
    "
    echo "Done."
    ;;

  fs-storage)
    echo "=== Renaming folders on Storage server ==="
    for dir in /storage/movies /storage/series; do
      [ -d "$dir" ] || continue
      find "$dir" -maxdepth 1 -type d -name '*\[*\]*' | while read old; do
        new=$(echo "$old" | sed 's/_\[/_(/' | sed 's/\]/)/g')
        if [ "$old" != "$new" ]; then
          echo "  $old -> $new"
          mv "$old" "$new"
        fi
      done
    done
    echo "Done."
    ;;

  fs-scanner)
    echo "=== Renaming folders on Scanner library ==="
    for dir in /mnt/storage/library/movies /mnt/storage/library/series; do
      [ -d "$dir" ] || continue
      find "$dir" -maxdepth 1 -type d -name '*\[*\]*' | while read old; do
        new=$(echo "$old" | sed 's/_\[/_(/' | sed 's/\]/)/g')
        if [ "$old" != "$new" ]; then
          echo "  $old -> $new"
          mv "$old" "$new"
        fi
      done
    done
    # Also rename nested series folders (s01/e01 dirs don't have brackets)
    echo "Done."
    ;;

  *)
    echo "Usage: $0 {db-api|db-scanner|fs-storage|fs-scanner}"
    exit 1
    ;;
esac
