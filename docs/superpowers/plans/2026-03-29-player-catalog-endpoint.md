# Player Catalog Endpoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `GET /api/player/catalog` endpoint that returns TMDB IDs of all converted movies, with delta support via `since` timestamp.

**Architecture:** New repository method queries `movies JOIN media_assets` for ready movies with TMDB IDs. New handler method parses optional `since` query param and returns JSON array of objects. Route registered in existing player auth group.

**Tech Stack:** Go, chi router, pgx, PostgreSQL

---

## File Structure

| Action | File | Responsibility |
|---|---|---|
| Modify | `api/internal/repository/movie.go` | New `ListReadyTMDBIDs` method |
| Modify | `api/internal/handler/player.go` | New `GetCatalog` handler method |
| Modify | `api/internal/server/server.go` | Register `GET /catalog` route |
| Modify | `docs/contracts/api.md` | Document new endpoint |
| Modify | `CHANGELOG.md` | Record change |

---

### Task 1: Add repository method `ListReadyTMDBIDs`

**Files:**
- Modify: `api/internal/repository/movie.go`

- [ ] **Step 1: Add the `ListReadyTMDBIDs` method**

Add to `api/internal/repository/movie.go`, after the `List` method (after line 92):

```go
// ListReadyTMDBIDs returns tmdb_id values for movies that have at least one
// ready asset. When since is non-nil only movies updated after that timestamp
// are returned.
func (r *MovieRepository) ListReadyTMDBIDs(ctx context.Context, since *time.Time) ([]string, error) {
	const base = `
		SELECT DISTINCT m.tmdb_id
		FROM movies m
		JOIN media_assets a ON a.movie_id = m.id
		WHERE a.is_ready = true
		  AND m.tmdb_id IS NOT NULL`

	var (
		rows pgx.Rows
		err  error
	)
	if since != nil {
		rows, err = r.pool.Query(ctx, base+` AND m.updated_at > $1 ORDER BY m.updated_at ASC`, *since)
	} else {
		rows, err = r.pool.Query(ctx, base+` ORDER BY m.updated_at ASC`)
	}
	if err != nil {
		return nil, fmt.Errorf("list ready tmdb ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tmdb id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
```

Note: `time` is already imported in the file via other handler usage. If not, add `"time"` to imports.

- [ ] **Step 2: Verify it compiles**

Run: `cd api && go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add api/internal/repository/movie.go
git commit -m "feat(api): add ListReadyTMDBIDs repository method"
```

---

### Task 2: Add `GetCatalog` handler method

**Files:**
- Modify: `api/internal/handler/player.go`

- [ ] **Step 1: Add the `GetCatalog` method to `PlayerHandler`**

Add to `api/internal/handler/player.go`, after the `GetMovie` method (after line 172, before the `repositoryMovieView` struct):

```go
// GetCatalog handles GET /api/player/catalog?since=...
func (h *PlayerHandler) GetCatalog(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var since *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"invalid since parameter: expected RFC 3339 format", false, cid)
			return
		}
		since = &t
	}

	ids, err := h.movieRepo.ListReadyTMDBIDs(r.Context(), since)
	if err != nil {
		slog.Error("catalog query failed", "error", err, "correlation_id", cid)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to fetch catalog", false, cid)
		return
	}

	items := make([]map[string]string, len(ids))
	for i, id := range ids {
		items[i] = map[string]string{"tmdb_id": id}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}
```

All imports (`time`, `strings`, `slog`, `auth`) are already present in the file.

- [ ] **Step 2: Verify it compiles**

Run: `cd api && go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add api/internal/handler/player.go
git commit -m "feat(api): add GetCatalog player handler"
```

---

### Task 3: Register route in server

**Files:**
- Modify: `api/internal/server/server.go`

- [ ] **Step 1: Add the route**

In `api/internal/server/server.go`, inside the player auth group (after line 93, after the `GetJobStatus` registration):

```go
r.Get("/catalog", deps.PlayerHandler.GetCatalog)
```

The block should look like:

```go
r.Group(func(r chi.Router) {
    r.Use(auth.PlayerKeyMiddleware(deps.Cfg.PlayerAPIKey))
    r.Get("/movie", deps.PlayerHandler.GetMovie)
    r.Get("/assets/{assetID}", deps.PlayerHandler.GetAsset)
    r.Get("/jobs/{jobID}/status", deps.PlayerHandler.GetJobStatus)
    r.Get("/catalog", deps.PlayerHandler.GetCatalog)
})
```

- [ ] **Step 2: Verify it compiles**

Run: `cd api && go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add api/internal/server/server.go
git commit -m "feat(api): register GET /api/player/catalog route"
```

---

### Task 4: Update documentation and changelog

**Files:**
- Modify: `docs/contracts/api.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add endpoint to API contract**

Add a new section to `docs/contracts/api.md` under the Player API section:

```markdown
### GET /api/player/catalog

Returns TMDB IDs of all converted (ready) movies. Supports delta loading via `since` parameter.

**Auth:** `X-Player-Key` header

**Query Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `since` | RFC 3339 timestamp | no | Return only movies updated after this timestamp. Omit for full list. |

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

| Status | Code | Condition |
|---|---|---|
| 400 | VALIDATION_ERROR | Invalid `since` format |
| 401 | UNAUTHORIZED | Missing or invalid X-Player-Key |
```

- [ ] **Step 2: Add CHANGELOG entry**

Add under `## [Unreleased]`:

```markdown
### Added
- `api/internal/repository/movie.go`: `ListReadyTMDBIDs` method for querying converted movies by TMDB ID with delta support
- `api/internal/handler/player.go`: `GetCatalog` handler for `GET /api/player/catalog` endpoint
- `api/internal/server/server.go`: registered `/catalog` route in player auth group
```

- [ ] **Step 3: Commit**

```bash
git add docs/contracts/api.md CHANGELOG.md
git commit -m "docs(contracts): add GET /api/player/catalog endpoint"
```
