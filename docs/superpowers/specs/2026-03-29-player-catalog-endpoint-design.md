# Player Catalog Endpoint Design

## Summary

New API endpoint `GET /api/player/catalog` that returns TMDB IDs of all converted (ready) movies, with delta support via `since` timestamp parameter. Payload CMS polls this every 5 minutes to sync available movies.

## Endpoint Specification

### `GET /api/player/catalog`

**Auth:** `X-Player-Key` header (existing player auth group)

**Query Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `since` | RFC 3339 timestamp | no | Return only movies with `updated_at > since`. Omit for full list. |

**Response (200):**

```json
{
  "items": [
    {"tmdb_id": "16662"},
    {"tmdb_id": "278"}
  ],
  "count": 2
}
```

**Errors:**

| Code | Condition |
|---|---|
| 400 | Invalid `since` format |
| 401 | Missing or invalid `X-Player-Key` |

### SQL Query

```sql
SELECT DISTINCT m.tmdb_id
FROM movies m
JOIN media_assets a ON a.movie_id = m.id
WHERE a.is_ready = true
  AND m.tmdb_id IS NOT NULL
  AND ($1::timestamptz IS NULL OR m.updated_at > $1)
ORDER BY m.updated_at ASC
```

## Architecture

### Files to modify

- `api/internal/repository/movie.go` — new method `ListReadyTMDBIDs(ctx, since *time.Time) ([]string, error)`
- `api/internal/handler/player.go` — new method `GetCatalog(w, r)`
- `api/internal/server/server.go` — register `GET /catalog` in player auth group

### Data flow

```
Payload CMS (every 5 min)
  → GET /api/player/catalog?since=<last_poll_time>
  → X-Player-Key header
  → PlayerHandler.GetCatalog()
  → MovieRepository.ListReadyTMDBIDs(since)
  → SQL: movies JOIN media_assets WHERE is_ready AND tmdb_id IS NOT NULL
  → JSON response with tmdb_id list
```

### Design decisions

- **Player auth, not admin JWT** — CMS is an external service like the player embed
- **Timestamp-based delta, not cursor** — no pagination needed; CMS remembers last poll time
- **Objects not flat array** — `{"tmdb_id": "..."}` is extensible if more fields needed later
- **`updated_at ASC` ordering** — stable order for delta consumption
- **No pagination** — catalog size is expected to be small (hundreds, not thousands)
