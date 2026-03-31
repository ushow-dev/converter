#!/usr/bin/env python3
"""Re-resolve TMDB IDs for movies in the converter DB.

Sources of truth (in priority order):
  1. [tmdb-XXXXX] tag embedded in the source filename (from Radarr)
  2. TMDB search with title scoring (for files without tags)

Usage:
  # Dry run — show mismatches only
  python3 scripts/re-resolve-tmdb.py

  # Apply changes
  python3 scripts/re-resolve-tmdb.py --apply

Requires:
  pip install requests guessit

Environment:
  TMDB_API_KEY  — required for re-searching files without embedded tags
  SSH_KEY       — path to SSH key for API server (default: ~/.ssh/id_rsa_personal)
  DB_HOST       — API server IP (default: 178.104.100.36)
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path
from typing import Optional
from urllib.parse import unquote

# Add scanner module to path for reusing metadata functions
SCANNER_PATH = Path(__file__).resolve().parent.parent / "scanner"
sys.path.insert(0, str(SCANNER_PATH))

from scanner.services.metadata import tmdb_search, parse_filename, _title_score

SSH_KEY = os.environ.get("SSH_KEY", os.path.expanduser("~/.ssh/id_rsa_personal"))
DB_HOST = os.environ.get("DB_HOST", "178.104.100.36")
TMDB_API_KEY = os.environ.get("TMDB_API_KEY", "")

# Regex to extract [tmdb-XXXXX] and [imdb-ttXXXXXXX] from filenames
RE_TMDB_TAG = re.compile(r"\[tmdb-(\d+)\]")
RE_IMDB_TAG = re.compile(r"\[imdb-(tt\d+)\]")


def ssh_psql(sql: str) -> str:
    """Run a psql query on the API server via SSH."""
    cmd = [
        "ssh", "-i", SSH_KEY, f"root@{DB_HOST}",
        f'docker exec converter-postgres-1 psql -U app -d mediadb -t -A -F"|" -c "{sql}"',
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    if result.returncode != 0:
        print(f"ERROR: psql failed: {result.stderr}", file=sys.stderr)
        sys.exit(1)
    return result.stdout.strip()


def ssh_psql_exec(sql: str) -> str:
    """Run a modifying SQL statement on the API server via SSH."""
    # Escape single quotes for shell
    escaped = sql.replace("'", "'\\''")
    cmd = [
        "ssh", "-i", SSH_KEY, f"root@{DB_HOST}",
        f"docker exec converter-postgres-1 psql -U app -d mediadb -c '{escaped}'",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    if result.returncode != 0:
        print(f"ERROR: psql exec failed: {result.stderr}", file=sys.stderr)
        return ""
    return result.stdout.strip()


def fetch_movies() -> list[dict]:
    """Fetch all movies with their source filenames from the DB."""
    sql = (
        "SELECT DISTINCT ON (m.id) m.id, m.tmdb_id, m.imdb_id, m.title, j.source_ref "
        "FROM movies m "
        "JOIN media_assets a ON a.movie_id = m.id "
        "JOIN media_jobs j ON j.job_id = a.job_id "
        "WHERE a.is_ready = true "
        "ORDER BY m.id, j.created_at DESC"
    )
    raw = ssh_psql(sql)
    movies = []
    for line in raw.splitlines():
        if not line.strip():
            continue
        parts = line.split("|")
        if len(parts) < 5:
            continue
        movies.append({
            "id": int(parts[0]),
            "tmdb_id": parts[1] or None,
            "imdb_id": parts[2] or None,
            "title": parts[3] or None,
            "source_ref": parts[4],
        })
    return movies


def extract_filename(source_ref: str) -> str:
    """Extract the filename from a source_ref (URL or path)."""
    # URL-decode
    decoded = unquote(source_ref)
    # Get the last path component
    return decoded.rsplit("/", 1)[-1]


def extract_tags(filename: str) -> tuple[Optional[str], Optional[str]]:
    """Extract [tmdb-XXXXX] and [imdb-ttXXXXXXX] from filename."""
    tmdb_match = RE_TMDB_TAG.search(filename)
    imdb_match = RE_IMDB_TAG.search(filename)
    return (
        tmdb_match.group(1) if tmdb_match else None,
        imdb_match.group(1) if imdb_match else None,
    )


def resolve_tmdb(filename: str, api_key: str) -> tuple[Optional[str], Optional[str], str]:
    """Resolve TMDB ID for a filename.

    Returns (tmdb_id, imdb_id, source) where source is 'tag' or 'search'.
    """
    # Priority 1: embedded tags
    tag_tmdb, tag_imdb = extract_tags(filename)
    if tag_tmdb:
        return tag_tmdb, tag_imdb, "tag"

    # Priority 2: TMDB search with scoring
    if not api_key:
        return None, None, "no_api_key"

    parsed = parse_filename(filename)
    title = parsed["title"]
    year = parsed.get("year")

    if not title:
        return None, None, "no_title"

    result = tmdb_search(title, year, api_key)
    if not result:
        return None, None, "not_found"

    return result["tmdb_id"], result.get("imdb_id"), "search"


def main():
    parser = argparse.ArgumentParser(description="Re-resolve TMDB IDs for converter movies")
    parser.add_argument("--apply", action="store_true", help="Apply changes to DB (default: dry run)")
    args = parser.parse_args()

    if not TMDB_API_KEY:
        print("WARNING: TMDB_API_KEY not set — can only resolve from filename tags", file=sys.stderr)

    print("Fetching movies from DB...")
    movies = fetch_movies()
    print(f"Found {len(movies)} movies\n")

    mismatches = []
    tag_updates = []
    search_updates = []
    unchanged = 0
    skipped = 0

    for movie in movies:
        filename = extract_filename(movie["source_ref"])
        new_tmdb, new_imdb, source = resolve_tmdb(filename, TMDB_API_KEY)

        old_tmdb = movie["tmdb_id"]
        old_imdb = movie["imdb_id"]

        tmdb_changed = new_tmdb and new_tmdb != old_tmdb
        imdb_changed = new_imdb and new_imdb != old_imdb

        if not tmdb_changed and not imdb_changed:
            unchanged += 1
            continue

        if not new_tmdb:
            skipped += 1
            continue

        entry = {
            "id": movie["id"],
            "title": movie["title"],
            "filename": filename,
            "old_tmdb": old_tmdb,
            "new_tmdb": new_tmdb,
            "old_imdb": old_imdb,
            "new_imdb": new_imdb,
            "source": source,
        }
        mismatches.append(entry)
        if source == "tag":
            tag_updates.append(entry)
        else:
            search_updates.append(entry)

    # Report
    print("=" * 80)
    print(f"RESULTS: {len(movies)} movies, {unchanged} unchanged, {len(mismatches)} mismatches, {skipped} skipped")
    print("=" * 80)

    if tag_updates:
        print(f"\n--- FROM FILENAME TAGS (high confidence): {len(tag_updates)} ---")
        for e in tag_updates:
            print(f"  #{e['id']} {e['filename'][:60]}")
            print(f"    tmdb: {e['old_tmdb']} -> {e['new_tmdb']}")
            if e["imdb_changed"] if "imdb_changed" in e else (e["new_imdb"] and e["new_imdb"] != e["old_imdb"]):
                print(f"    imdb: {e['old_imdb']} -> {e['new_imdb']}")

    if search_updates:
        print(f"\n--- FROM TMDB SEARCH (verify manually): {len(search_updates)} ---")
        for e in search_updates:
            print(f"  #{e['id']} {e['filename'][:60]}")
            print(f"    tmdb: {e['old_tmdb']} -> {e['new_tmdb']}")

    if not mismatches:
        print("\nNo mismatches found. All TMDB IDs are correct.")
        return

    if not args.apply:
        print(f"\nDry run complete. Use --apply to update {len(mismatches)} movies.")
        return

    # Apply updates
    print(f"\nApplying {len(mismatches)} updates...")
    applied = 0
    for e in mismatches:
        new_tmdb = e["new_tmdb"] or ""
        new_imdb = e["new_imdb"] or ""
        title = e["title"] or ""

        sql = (
            f"UPDATE movies SET "
            f"tmdb_id = NULLIF('{new_tmdb}', ''), "
            f"imdb_id = COALESCE(NULLIF('{new_imdb}', ''), imdb_id), "
            f"updated_at = NOW() "
            f"WHERE id = {e['id']}"
        )
        result = ssh_psql_exec(sql)
        if "UPDATE 1" in result:
            applied += 1
            print(f"  OK  #{e['id']} tmdb {e['old_tmdb']} -> {e['new_tmdb']}")
        else:
            print(f"  FAIL #{e['id']}: {result}")

    print(f"\nDone. Updated {applied}/{len(mismatches)} movies.")


if __name__ == "__main__":
    main()
