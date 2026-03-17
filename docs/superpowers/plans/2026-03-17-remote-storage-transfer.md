# Remote Storage Transfer Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After HLS conversion completes, automatically transfer the movie folder to a remote server via rclone (SFTP today, S3-compatible later), then serve media URLs dynamically based on which storage location holds the file.

**Architecture:** Add a `storage_locations` table to track configured backends. After conversion, the worker pushes a message to a new `transfer_queue`. A new `TransferWorker` runs rclone and updates `movies.storage_location_id` on success. The player API reads the storage location's `base_url` at request time to build correct HLS URLs. `base_url` can be empty until a domain/proxy is configured — the player falls back to local `MEDIA_BASE_URL` in that case.

**Tech Stack:** Go 1.23, PostgreSQL 16, Redis BLPOP, rclone (added to worker Docker image), pgx v5

---

## Remote server — already configured

Server `45.134.174.84` is ready:
- **User:** `mediarw` (no password, key-only)
- **SSH private key:** `converter/secrets/mediarw_rclone` (in `.gitignore`)
- **Storage path:** `/storage/movies/`

**rclone env vars** (add to `.env`):
```
RCLONE_CONFIG_MYREMOTE_TYPE=sftp
RCLONE_CONFIG_MYREMOTE_HOST=45.134.174.84
RCLONE_CONFIG_MYREMOTE_USER=mediarw
RCLONE_CONFIG_MYREMOTE_KEY_FILE=/secrets/mediarw_rclone
RCLONE_REMOTE=myremote
RCLONE_REMOTE_PATH=/storage
```

**`base_url`** — leave empty until proxy/domain is ready. When ready:
```sql
UPDATE storage_locations SET base_url = 'https://media.yourdomain.com' WHERE name = 'remote-sftp';
```

Transfer is automatic when `RCLONE_REMOTE` is set. Unset = files stay local.

---

## File Map

**New files:**
- `api/internal/db/migrations/010_storage_locations.sql` — new table + FK column
- `api/internal/db/migrations/011_seed_remote_storage_location.sql` — seed remote row (empty base_url)
- `worker/internal/transfer/transfer.go` — TransferWorker (rclone + DB update)

**Modified files:**
- `worker/internal/repository/movie.go` — replace `generateStorageKey()` with `buildStorageKey(title, year)`, add `UpdateStorageLocation()`
- `api/internal/model/model.go` — add `StorageLocation` struct, `TransferPayload/Job`, `StorageLocationID` to `Movie`
- `worker/internal/model/model.go` — add `TransferMessage/Job`, `StorageLocation`
- `api/internal/repository/movie.go` — add `UpdateStorageLocation()`, scan `storage_location_id`
- `api/internal/repository/` — new `storage_location.go` (read repo)
- `worker/internal/repository/` — new `storage_location.go` (read repo for worker)
- `api/internal/queue/queue.go` — add `TransferQueue` constant
- `worker/internal/queue/queue.go` — add `TransferQueue` constant
- `worker/internal/converter/converter.go` — push to transfer_queue after completion
- `worker/internal/config/config.go` — add `RcloneRemote`, `RcloneRemotePath`, `TransferConcurrency`
- `worker/cmd/worker/main.go` — register TransferWorker goroutines
- `api/internal/handler/player.go` — use per-movie `base_url` from storage_location
- `worker/Dockerfile` — install rclone + openssh-client
- `docker-compose.yml` — mount `./secrets/mediarw_rclone:/secrets/mediarw_rclone:ro` in worker
- `.env.example` — document new env vars
- `docs/contracts/worker.md` — add TransferPayload schema
- `docs/converter/pipeline.md` — add transfer stage
- `CHANGELOG.md` — entry for this feature
- `docs/decisions/README.md` — update next ADR number

**New docs:**
- `docs/decisions/ADR-007-remote-storage-rclone.md`

---

## Chunk 0: storage_key format — Title (Year)

> This chunk is independent and can be implemented and deployed before the rest.

### Task 1: Change storage_key generation to "Title (Year)" format

**Files:**
- Modify: `worker/internal/repository/movie.go`

Currently `generateStorageKey()` produces `mov_<random>`. New format: `The Matrix (1999)`.
Existing movies keep their old keys — no migration needed.

- [ ] **Step 1: Write `buildStorageKey` function**

Replace `generateStorageKey()` with:

```go
// buildStorageKey builds a human-readable, filesystem-safe folder name.
// Format: "Title (Year)" or "Title" if year is unknown, "untitled_<hex>" if title is empty.
func buildStorageKey(title string, year *int) string {
	sanitized := strings.Map(func(r rune) rune {
		// Drop chars invalid in folder names across Linux/macOS/rclone remotes
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return -1
		}
		return r
	}, strings.TrimSpace(title))
	sanitized = strings.Join(strings.Fields(sanitized), " ") // collapse whitespace

	if sanitized == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		return fmt.Sprintf("untitled_%x", b)
	}

	if year != nil && *year > 0 {
		return fmt.Sprintf("%s (%d)", sanitized, *year)
	}
	return sanitized
}
```

Remove the old `generateStorageKey()` function.

- [ ] **Step 2: Handle unique constraint collisions in `Upsert`**

The `storage_key` column has a UNIQUE constraint. Two movies with identical title+year would collide (rare but possible). Change the INSERT in `Upsert` to retry with a suffix:

```go
// Try "Title (Year)", then "Title (Year) 2", "Title (Year) 3", etc.
baseKey := buildStorageKey(title, year)
var m *model.Movie
for attempt := 1; attempt <= 10; attempt++ {
	key := baseKey
	if attempt > 1 {
		key = fmt.Sprintf("%s %d", baseKey, attempt)
	}
	m = &model.Movie{}
	err = tx.QueryRow(ctx, `
		INSERT INTO movies (storage_key, imdb_id, tmdb_id, title, year, poster_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, storage_key, imdb_id, tmdb_id, title, year, poster_url, created_at, updated_at`,
		key, imdb, tmdb, ttl, year, posterURL,
	).Scan(&m.ID, &m.StorageKey, &m.IMDbID, &m.TMDBID, &m.Title, &m.Year, &m.PosterURL, &m.CreatedAt, &m.UpdatedAt)
	if err == nil {
		break
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "storage_key") {
		continue // key collision — try suffix
	}
	return nil, fmt.Errorf("insert movie: %w", err)
}
if m == nil || m.ID == 0 {
	return nil, fmt.Errorf("insert movie: exhausted key attempts for %q", baseKey)
}
```

Add import: `"github.com/jackc/pgx/v5/pgconn"`

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```
Expected: no errors

- [ ] **Step 4: Manual test** — trigger a conversion job, verify the `movies` table gets a row with key like `Inception (2010)` instead of `mov_abc123`

- [ ] **Step 5: Commit**

```bash
git add worker/internal/repository/movie.go
git commit -m "feat(worker): use 'Title (Year)' format for movie storage_key folders"
```

---

## Chunk 1: Database and models

### Task 2: DB migration — storage_locations table

**Files:**
- Create: `api/internal/db/migrations/010_storage_locations.sql`

- [ ] **Step 1: Write the migration**

```sql
-- Migration 010: remote storage locations + link movies to storage location

CREATE TABLE IF NOT EXISTS storage_locations (
    id         BIGSERIAL   PRIMARY KEY,
    name       TEXT        NOT NULL UNIQUE,
    type       TEXT        NOT NULL,           -- "sftp" | "s3" | "local"
    base_url   TEXT        NOT NULL DEFAULT '', -- HTTP base for player URLs; empty = not yet configured
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reserve id=1 for "local" (existing movies with NULL storage_location_id use local MEDIA_BASE_URL)
INSERT INTO storage_locations (id, name, type, base_url)
VALUES (1, 'local', 'local', '')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE movies
    ADD COLUMN IF NOT EXISTS storage_location_id BIGINT
    REFERENCES storage_locations(id) ON DELETE SET NULL;

-- NULL means local (default)
```

- [ ] **Step 2: Verify migration is idempotent** — re-run it manually, expect no errors

- [ ] **Step 3: Restart API service to apply migration**

```bash
docker compose restart api
docker compose logs api | grep -E "migration|error"
```

- [ ] **Step 4: Commit**

```bash
git add api/internal/db/migrations/010_storage_locations.sql
git commit -m "feat(db): add storage_locations table and movies.storage_location_id"
```

---

### Task 3: Model updates — api/internal/model/model.go

**Files:**
- Modify: `api/internal/model/model.go`

- [ ] **Step 1: Add `StorageLocation` struct**

Add after the `Movie` struct:
```go
// StorageLocation represents a configured media storage backend.
type StorageLocation struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`     // "sftp" | "s3" | "local"
	BaseURL   string    `json:"base_url"` // empty = domain not yet configured
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}
```

- [ ] **Step 2: Add `StorageLocationID` to `Movie` struct**

```go
StorageLocationID *int64 `json:"storage_location_id,omitempty"`
```

- [ ] **Step 3: Add `TransferPayload` and `TransferJob`**

```go
// TransferPayload is the message pushed to transfer_queue after HLS conversion.
type TransferPayload struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	CorrelationID string      `json:"correlation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       TransferJob `json:"payload"`
}

// TransferJob holds details for a single rclone transfer operation.
type TransferJob struct {
	MovieID    int64  `json:"movie_id"`
	StorageKey string `json:"storage_key"` // relative folder name, e.g. "Inception (2010)"
	LocalPath  string `json:"local_path"`  // absolute local path to the movie folder
}
```

- [ ] **Step 4: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/api && go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add api/internal/model/model.go
git commit -m "feat(api): add StorageLocation model and TransferPayload"
```

---

### Task 4: Worker model updates — worker/internal/model/model.go

**Files:**
- Modify: `worker/internal/model/model.go`

- [ ] **Step 1: Read the file to find existing structs**

- [ ] **Step 2: Add `StorageLocation`, `TransferMessage`, `TransferJob`**

```go
// StorageLocation mirrors the api model for worker-side reads.
type StorageLocation struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
}

// TransferMessage is the BLPOP envelope for transfer_queue.
// JSON-compatible with api/internal/model.TransferPayload.
type TransferMessage struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	CorrelationID string      `json:"correlation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       TransferJob `json:"payload"`
}

// TransferJob is the inner payload for a transfer task.
type TransferJob struct {
	MovieID    int64  `json:"movie_id"`
	StorageKey string `json:"storage_key"`
	LocalPath  string `json:"local_path"`
}
```

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add worker/internal/model/model.go
git commit -m "feat(worker): add StorageLocation and TransferMessage models"
```

---

## Chunk 2: Queue constants, config, repositories

### Task 5: Add TransferQueue constant

**Files:**
- Modify: `api/internal/queue/queue.go`
- Modify: `worker/internal/queue/queue.go`

- [ ] **Step 1: Read both files to find where queue name constants are defined**

- [ ] **Step 2: Add constant to api queue package**

```go
const TransferQueue = "transfer_queue"
```

- [ ] **Step 3: Add same constant to worker queue package**

- [ ] **Step 4: Build both**

```bash
cd /Users/robospot/prj/cleaner/converter/api && go build ./... && \
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add api/internal/queue/ worker/internal/queue/
git commit -m "feat(queue): add transfer_queue constant to api and worker"
```

---

### Task 6: Worker config — rclone settings

**Files:**
- Modify: `worker/internal/config/config.go`

- [ ] **Step 1: Add fields to `Config` struct**

```go
// rclone transfer settings
RcloneRemote       string // name of rclone remote, e.g. "myremote" — empty disables transfer
RcloneRemotePath   string // base path on remote, e.g. "/storage"
TransferConcurrency int
```

- [ ] **Step 2: Add to `Load()` function**

```go
RcloneRemote:        getEnv("RCLONE_REMOTE", ""),
RcloneRemotePath:    getEnv("RCLONE_REMOTE_PATH", "/storage"),
TransferConcurrency: intEnv("TRANSFER_CONCURRENCY", 1),
```

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 4: Update `.env.example`** — add section:

```
# Remote storage transfer (optional — leave RCLONE_REMOTE empty to keep files local)
RCLONE_REMOTE=
RCLONE_REMOTE_PATH=/storage
TRANSFER_CONCURRENCY=1
```

- [ ] **Step 5: Commit**

```bash
git add worker/internal/config/config.go .env.example
git commit -m "feat(config): add rclone remote transfer config to worker"
```

---

### Task 7: StorageLocation repository (API)

**Files:**
- Create: `api/internal/repository/storage_location.go`
- Modify: `api/internal/repository/movie.go`

- [ ] **Step 1: Create `api/internal/repository/storage_location.go`**

```go
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/api/internal/model"
)

// StorageLocationRepository reads the storage_locations table.
type StorageLocationRepository struct {
	pool *pgxpool.Pool
}

func NewStorageLocationRepository(pool *pgxpool.Pool) *StorageLocationRepository {
	return &StorageLocationRepository{pool: pool}
}

// GetByID returns a storage location by id.
func (r *StorageLocationRepository) GetByID(ctx context.Context, id int64) (*model.StorageLocation, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, type, base_url, is_active, created_at
		 FROM storage_locations WHERE id = $1`, id)
	loc := &model.StorageLocation{}
	if err := row.Scan(&loc.ID, &loc.Name, &loc.Type, &loc.BaseURL, &loc.IsActive, &loc.CreatedAt); err != nil {
		return nil, fmt.Errorf("get storage location: %w", err)
	}
	return loc, nil
}
```

- [ ] **Step 2: Add `UpdateStorageLocation` to `api/internal/repository/movie.go`**

```go
func (r *MovieRepository) UpdateStorageLocation(ctx context.Context, movieID, locationID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE movies SET storage_location_id = $2, updated_at = NOW() WHERE id = $1`,
		movieID, locationID)
	return err
}
```

Also update all SELECT queries in the file to include `m.storage_location_id`, and add `&m.StorageLocationID` to `scanMovieRows`.

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/api && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add api/internal/repository/storage_location.go api/internal/repository/movie.go
git commit -m "feat(api): add StorageLocationRepository and movie.UpdateStorageLocation"
```

---

### Task 8: StorageLocation repository (Worker)

**Files:**
- Create: `worker/internal/repository/storage_location.go`
- Modify: `worker/internal/repository/movie.go`

- [ ] **Step 1: Create `worker/internal/repository/storage_location.go`**

```go
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/worker/internal/model"
)

type StorageLocationRepository struct {
	pool *pgxpool.Pool
}

func NewStorageLocationRepository(pool *pgxpool.Pool) *StorageLocationRepository {
	return &StorageLocationRepository{pool: pool}
}

func (r *StorageLocationRepository) GetByID(ctx context.Context, id int64) (*model.StorageLocation, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, type, base_url FROM storage_locations WHERE id = $1`, id)
	loc := &model.StorageLocation{}
	if err := row.Scan(&loc.ID, &loc.Name, &loc.Type, &loc.BaseURL); err != nil {
		return nil, fmt.Errorf("get storage location: %w", err)
	}
	return loc, nil
}
```

- [ ] **Step 2: Add `UpdateStorageLocation` to `worker/internal/repository/movie.go`**

```go
func (r *MovieRepository) UpdateStorageLocation(ctx context.Context, movieID, locationID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE movies SET storage_location_id = $2, updated_at = NOW() WHERE id = $1`,
		movieID, locationID)
	return err
}
```

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add worker/internal/repository/storage_location.go worker/internal/repository/movie.go
git commit -m "feat(worker): add StorageLocationRepository and movie.UpdateStorageLocation"
```

---

## Chunk 3: Transfer worker

### Task 9: Create transfer worker

**Files:**
- Create: `worker/internal/transfer/transfer.go`

- [ ] **Step 1: Write the transfer worker**

```go
package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"app/worker/internal/model"
	"app/worker/internal/queue"
	"app/worker/internal/repository"
)

type Worker struct {
	q             *queue.Client
	movieRepo     *repository.MovieRepository
	storageLocID  int64
	rcloneRemote  string
	remotePath    string
}

func New(
	q *queue.Client,
	movieRepo *repository.MovieRepository,
	rcloneRemote string,
	remotePath string,
	storageLocID int64,
) *Worker {
	return &Worker{
		q:            q,
		movieRepo:    movieRepo,
		storageLocID: storageLocID,
		rcloneRemote: rcloneRemote,
		remotePath:   remotePath,
	}
}

func (w *Worker) Run(ctx context.Context) {
	slog.Info("transfer worker started", "remote", w.rcloneRemote)
	for {
		if ctx.Err() != nil {
			slog.Info("transfer worker stopped")
			return
		}
		raw, err := w.q.Pop(ctx, queue.TransferQueue, 5*time.Second)
		if errors.Is(err, queue.ErrEmpty) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("transfer queue pop error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		w.process(ctx, raw)
	}
}

func (w *Worker) process(ctx context.Context, raw []byte) {
	var msg model.TransferMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Error("unmarshal transfer message", "error", err)
		return
	}
	log := slog.With("movie_id", msg.Payload.MovieID, "storage_key", msg.Payload.StorageKey)

	localPath := msg.Payload.LocalPath
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Error("local path does not exist, skipping transfer", "path", localPath)
		return
	}

	// Destination: <remote>:<remotePath>/movies/<storageKey>/
	dest := fmt.Sprintf("%s:%s/movies/%s/",
		w.rcloneRemote,
		filepath.ToSlash(w.remotePath),
		msg.Payload.StorageKey,
	)

	log.Info("starting rclone transfer", "src", localPath, "dest", dest)
	start := time.Now()

	// rclone move: copies all files then deletes source files on success.
	cmd := exec.CommandContext(ctx, "rclone", "move",
		localPath+"/", // trailing slash: move contents, not folder itself
		dest,
		"--progress",
		"--stats-one-line",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Error("rclone transfer failed", "error", err, "duration_s", time.Since(start).Seconds())
		return
	}

	log.Info("rclone transfer complete", "duration_s", time.Since(start).Seconds())

	// Remove now-empty local directory.
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		log.Warn("could not remove local dir after transfer", "path", localPath, "error", err)
	}

	// Update DB: mark movie as residing on remote storage.
	if err := w.movieRepo.UpdateStorageLocation(ctx, msg.Payload.MovieID, w.storageLocID); err != nil {
		log.Error("update storage_location_id failed", "error", err)
		return
	}

	log.Info("transfer done, storage_location_id updated", "location_id", w.storageLocID)
}
```

- [ ] **Step 2: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add worker/internal/transfer/transfer.go
git commit -m "feat(worker): add TransferWorker for rclone remote upload"
```

---

### Task 10: Converter pushes to transfer_queue

**Files:**
- Modify: `worker/internal/converter/converter.go`

- [ ] **Step 1: Add `transferEnabled bool` field to `Worker` struct and `New()` signature**

- [ ] **Step 2: After the subtitle fetch block (end of `process`), add**

```go
// ── Enqueue transfer (if remote is configured) ────────────────────────────
if w.transferEnabled {
	tfMsg := model.TransferMessage{
		SchemaVersion: "1",
		JobID:         msg.JobID,
		CorrelationID: msg.CorrelationID,
		CreatedAt:     time.Now().UTC(),
		Payload: model.TransferJob{
			MovieID:    movie.ID,
			StorageKey: movie.StorageKey,
			LocalPath:  finalDir,
		},
	}
	if err := w.q.Push(ctx, queue.TransferQueue, tfMsg); err != nil {
		log.Error("enqueue transfer failed", "error", err)
		// Non-fatal: film is still available locally.
	} else {
		log.Info("transfer job enqueued", "movie_id", movie.ID)
	}
}
```

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add worker/internal/converter/converter.go
git commit -m "feat(worker): enqueue transfer_queue message after HLS conversion completes"
```

---

### Task 11: Register TransferWorker in main.go

**Files:**
- Modify: `worker/cmd/worker/main.go`

- [ ] **Step 1: Add import for `transfer` package**

- [ ] **Step 2: Conditionally create and register TransferWorker**

After the `cvWorker` setup, add:

```go
// Transfer worker (optional: only when RCLONE_REMOTE is set)
const remoteStorageLocID = int64(2) // matches id from migration 011
var trWorker *transfer.Worker
if cfg.RcloneRemote != "" {
	trWorker = transfer.New(redisClient, movieRepo,
		cfg.RcloneRemote, cfg.RcloneRemotePath, remoteStorageLocID)
	slog.Info("transfer worker enabled", "remote", cfg.RcloneRemote)
} else {
	slog.Info("transfer worker disabled (RCLONE_REMOTE not set)")
}
```

Register goroutines after the existing worker loops:

```go
if trWorker != nil {
	for i := 0; i < cfg.TransferConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trWorker.Run(ctx)
		}()
	}
}
```

Also pass `cfg.RcloneRemote != ""` as `transferEnabled` when constructing `cvWorker`.

- [ ] **Step 3: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/worker && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add worker/cmd/worker/main.go
git commit -m "feat(worker): register TransferWorker goroutines in main"
```

---

## Chunk 4: Player API dynamic URLs + storage location seeding

### Task 12: Seed remote storage_location row

**Files:**
- Create: `api/internal/db/migrations/011_seed_remote_storage_location.sql`

`base_url` is intentionally empty — fill it in when the proxy/domain is ready with:
```sql
UPDATE storage_locations SET base_url = 'https://media.yourdomain.com' WHERE name = 'remote-sftp';
```

```sql
-- Migration 011: seed remote storage location
-- base_url is empty until a domain/proxy is configured.
-- Update it later: UPDATE storage_locations SET base_url='https://...' WHERE name='remote-sftp';

INSERT INTO storage_locations (name, type, base_url, is_active)
VALUES ('remote-sftp', 'sftp', '', true)
ON CONFLICT (name) DO NOTHING;
```

After applying, verify the auto-assigned `id`:
```sql
SELECT id, name FROM storage_locations;
```
If it differs from `2`, update the `remoteStorageLocID` constant in `main.go` (Task 11).

- [ ] **Step 1: Create the file**

- [ ] **Step 2: Apply migration and confirm id=2**

- [ ] **Step 3: Commit**

```bash
git add api/internal/db/migrations/011_seed_remote_storage_location.sql
git commit -m "feat(db): seed remote storage_location row with empty base_url"
```

---

### Task 13: Player handler — dynamic base URL per movie

**Files:**
- Modify: `api/internal/handler/player.go`

The player handler currently uses a single global `h.mediaBaseURL`. When a movie has `storage_location_id` set and that location has a non-empty `base_url`, use it. If `base_url` is empty (domain not yet configured), fall back to local `MEDIA_BASE_URL`.

- [ ] **Step 1: Add `StorageLocationRepository` field to `PlayerHandler`**

```go
storageLocRepo *repository.StorageLocationRepository
```

Update `NewPlayerHandler` signature and constructor accordingly.

- [ ] **Step 2: Add `resolveBaseURL` helper**

```go
// resolveBaseURL returns the appropriate media base URL for a movie.
// If the movie is on a remote storage location with a configured base_url, use that.
// Falls back to the global MEDIA_BASE_URL (covers local movies and remote movies
// whose domain is not yet configured).
func (h *PlayerHandler) resolveBaseURL(ctx context.Context, storageLocationID *int64) string {
	if storageLocationID != nil && *storageLocationID > 1 {
		loc, err := h.storageLocRepo.GetByID(ctx, *storageLocationID)
		if err == nil && loc.BaseURL != "" {
			return loc.BaseURL
		}
		// base_url empty = domain not yet configured; fall through to local
	}
	return h.mediaBaseURL
}
```

- [ ] **Step 3: Add `storageLocationID *int64` to `repositoryMovieView`**

```go
type repositoryMovieView struct {
	id                int64
	storageKey        string
	imdbID            *string
	tmdbID            *string
	storageLocationID *int64  // add this
}
```

Update `getMovieByIMDbID` and `getMovieByTMDBID` to populate it from `m.StorageLocationID`.

- [ ] **Step 4: Update `GetMovie` to call `resolveBaseURL`**

Replace all occurrences of `h.mediaBaseURL` in `GetMovie` with:
```go
baseURL := h.resolveBaseURL(r.Context(), movie.storageLocationID)
```
Then use `baseURL` instead of `h.mediaBaseURL` in `buildMovieMediaURL` calls.

- [ ] **Step 5: Update `NewPlayerHandler` call in `api/cmd/api/main.go`** to pass the storage location repo

- [ ] **Step 6: Build check**

```bash
cd /Users/robospot/prj/cleaner/converter/api && go build ./...
```

- [ ] **Step 7: Manual smoke test**

  a. Movie with `storage_location_id = NULL` → player URL uses `MEDIA_BASE_URL`

  b. Set `UPDATE movies SET storage_location_id = 2 WHERE id = <any>` → call player endpoint → URL still uses `MEDIA_BASE_URL` (because `base_url` is empty)

  c. Set `UPDATE storage_locations SET base_url = 'https://test.example.com' WHERE id = 2` → call again → URL now uses `https://test.example.com`

- [ ] **Step 8: Commit**

```bash
git add api/internal/handler/player.go api/cmd/api/main.go
git commit -m "feat(api): resolve media base URL from storage_location per movie"
```

---

## Chunk 5: Docker, docs, ADR

### Task 14: Install rclone and wire SSH key into worker

**Files:**
- Modify: `worker/Dockerfile`
- Modify: `docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Read `worker/Dockerfile`** to identify the runtime stage (Alpine or Debian)

- [ ] **Step 2: Add rclone installation** in the runtime stage

For Alpine:
```dockerfile
RUN apk add --no-cache rclone openssh-client
```
For Debian/Ubuntu:
```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends rclone openssh-client && rm -rf /var/lib/apt/lists/*
```

- [ ] **Step 3: Add SSH key volume mount to `docker-compose.yml`** in the `worker` service

```yaml
worker:
  volumes:
    - ./secrets/mediarw_rclone:/secrets/mediarw_rclone:ro
```

- [ ] **Step 4: Add rclone env vars to `.env.example`** (replace the existing transfer section added in Task 6):

```
# Remote storage transfer (optional — leave RCLONE_REMOTE empty to keep files local)
RCLONE_CONFIG_MYREMOTE_TYPE=sftp
RCLONE_CONFIG_MYREMOTE_HOST=45.134.174.84
RCLONE_CONFIG_MYREMOTE_USER=mediarw
RCLONE_CONFIG_MYREMOTE_KEY_FILE=/secrets/mediarw_rclone
RCLONE_REMOTE=myremote
RCLONE_REMOTE_PATH=/storage
TRANSFER_CONCURRENCY=1
```

- [ ] **Step 5: Rebuild worker image**

```bash
docker compose build worker
```

- [ ] **Step 6: Verify rclone can reach the remote**

```bash
docker compose run --rm worker rclone lsd myremote:/storage
```
Expected: shows `movies` directory

- [ ] **Step 7: Commit**

```bash
git add worker/Dockerfile docker-compose.yml .env.example
git commit -m "build(worker): install rclone, mount SSH key for mediarw@45.134.174.84"
```

---

### Task 15: ADR and documentation

**Files:**
- Create: `docs/decisions/ADR-007-remote-storage-rclone.md`
- Modify: `docs/decisions/README.md`
- Modify: `docs/converter/pipeline.md`
- Modify: `docs/contracts/worker.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Create ADR-007**

```bash
cd /Users/robospot/prj/cleaner/converter && ./scripts/new-adr.sh "remote-storage-rclone"
```

Fill in sections:
- **Context:** Media files accumulate on the processing server; need to move completed HLS to a larger remote server. Remote server has no web server initially — will be set up later behind a proxy.
- **Decision:** Use rclone with `transfer_queue` consumer. Storage backends tracked in `storage_locations` table. `base_url` can be empty until proxy is ready; player falls back to local. Movie folder names use `Title (Year)` format for human readability.
- **Consequences:** Films stay local if `RCLONE_REMOTE` is unset. Future S3 support requires only adding a new `storage_locations` row and rclone remote config. Changing domain = one SQL UPDATE.

- [ ] **Step 2: Update `docs/decisions/README.md`** — add ADR-007 row, update next number to 008

- [ ] **Step 3: Update `docs/converter/pipeline.md`** — add transfer stage:

```
convert → [HLS ready in /media/converted/movies/<Title (Year)>/]
        → transfer_queue → rclone move → remote:/storage/movies/<Title (Year)>/
        → movies.storage_location_id updated
```

- [ ] **Step 4: Update `docs/contracts/worker.md`** — add `TransferMessage` schema

- [ ] **Step 5: Update `CHANGELOG.md`** under `## [Unreleased]`:

```markdown
### Added
- `api/internal/db/migrations/010_storage_locations.sql`: storage_locations table and movies.storage_location_id FK
- `api/internal/db/migrations/011_seed_remote_storage_location.sql`: seed remote storage location row
- `worker/internal/transfer/transfer.go`: TransferWorker — rclone-based post-conversion file transfer
- `api/internal/repository/storage_location.go`: StorageLocationRepository (api)
- `worker/internal/repository/storage_location.go`: StorageLocationRepository (worker)

### Changed
- `worker/internal/repository/movie.go`: storage_key now uses "Title (Year)" format instead of random hex
- `worker/internal/converter/converter.go`: enqueue transfer_queue message after successful HLS conversion
- `api/internal/handler/player.go`: media base URL resolved per-movie from storage_location; falls back to MEDIA_BASE_URL when base_url is empty
- `worker/Dockerfile`: rclone installed
```

- [ ] **Step 6: Commit**

```bash
git add docs/ CHANGELOG.md
git commit -m "docs: add ADR-007, update pipeline and contracts for remote storage transfer"
```

---

## Final checklist

- [ ] Chunk 0 deployed: new movies get `Inception (2010)` style folders
- [ ] Migration 010 applied: `storage_locations` table with local row (id=1)
- [ ] Migration 011 applied: remote row exists (id=2), `base_url` empty for now
- [ ] Worker image rebuilt with rclone
- [ ] rclone config available in worker container (`RCLONE_REMOTE` + credentials)
- [ ] End-to-end test: convert job → transfer_queue receives message → rclone transfers → `movies.storage_location_id = 2`
- [ ] Player URL test: movie with `storage_location_id = 2` and empty `base_url` → falls back to `MEDIA_BASE_URL` ✓
- [ ] When domain is ready: `UPDATE storage_locations SET base_url='https://...' WHERE id=2` → player URLs update automatically ✓
- [ ] Local-only fallback: `RCLONE_REMOTE` unset → no transfer, everything works as before ✓
