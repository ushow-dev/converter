# Incoming Ingest Worker (Block 2) — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `incoming_media_items` ingestion layer — API endpoints for scanner registration and worker claim/progress/complete, plus an ingest-puller worker that claims items, rclone-copies source files, and hands them off to the convert queue via the API.

**Architecture:** Scanner registers files via `POST /api/ingest/incoming/register`. Ingest worker polls `POST /api/ingest/incoming/claim`, copies the file with rclone, then calls `POST /api/ingest/incoming/complete` with the local path — the API creates the `media_job` and pushes to `convert_queue` (worker never touches the queue directly). All state is API-only.

**Tech Stack:** Go 1.22, chi router, pgx v5, rclone CLI (already used by transfer worker)

---

## Scope

Block 1 (Python scanner, storage server) is a **separate repository**. This plan covers Block 2 only — changes to this converter repo.

---

## Critical Design Decisions (from spec critique)

| Decision | Rationale |
|----------|-----------|
| Worker calls API `complete(id, local_path)` → API creates `media_job` + pushes convert_queue → returns `{job_id}` | Worker cannot push directly — converter worker expects `media_jobs` row to exist for the job_id |
| `ClaimBatch` atomically resets expired leases before claiming (single CTE) | Avoids need for a separate expire-leases endpoint or background goroutine |
| Claim size = `INGEST_CONCURRENCY` (no separate `claim_limit`) | Prevents claimed-but-idle items expiring before processing |
| `INGEST_SERVICE_TOKEN` + `X-Service-Token` header | Simple, follows existing player key pattern |
| `maxAttempts` from config, threaded through Service → Handler | No magic constants |
| No `heartbeat` endpoint (YAGNI) | Spec says "at necessity" — implement when needed |

---

## File Map

### New files — API

| File | Responsibility |
|------|---------------|
| `api/internal/db/migrations/012_incoming_media_items.sql` | Table + indexes |
| `api/internal/model/incoming.go` | `IncomingItem` struct + status constants + request/response types |
| `api/internal/repository/incoming.go` | `Register` (upsert), `ClaimBatch` (with expired-lease reset), `Progress`, `Fail`, `Complete` |
| `api/internal/service/ingest.go` | Validate + delegate; `Complete` creates `media_job` + pushes convert_queue |
| `api/internal/handler/ingest.go` | 5 HTTP handlers: register, claim, progress, fail, complete |
| `api/internal/auth/service_token.go` | `ServiceTokenMiddleware(token string)` |

### Modified files — API

| File | Change |
|------|--------|
| `api/internal/config/config.go` | Add `IngestServiceToken`, `IngestMaxAttempts int` |
| `api/internal/server/server.go` | Add `IngestHandler *handler.IngestHandler` to `Dependencies`; register `/api/ingest/*` routes |
| `api/cmd/api/main.go` | Wire `IncomingRepository` → `IngestService` → `IngestHandler` |

### New files — Worker

| File | Responsibility |
|------|---------------|
| `worker/internal/ingest/client.go` | Typed HTTP client: `Claim`, `Progress`, `Fail`, `Complete` |
| `worker/internal/ingest/puller.go` | `rclone copy` wrapper |
| `worker/internal/ingest/worker.go` | Poll loop: claim → copy → complete/fail |

### Modified files — Worker

| File | Change |
|------|--------|
| `worker/internal/config/config.go` | Add `ConverterAPIURL`, `IngestServiceToken`, `IngestConcurrency`, `IngestClaimTTLSec`, `IngestMaxAttempts`, `IngestSourceRemote`, `IngestSourceBasePath` |
| `worker/cmd/worker/main.go` | Instantiate and run ingest worker (gated on `IngestServiceToken != ""`) |

### Supporting

| File | Change |
|------|--------|
| `.env.example` | Document new ingest env vars |
| `CHANGELOG.md` | Entries under `[Unreleased]` |
| `docs/contracts/api.md` | 5 new endpoints |
| `docs/contracts/worker.md` | Ingest worker section |
| `docs/roadmap/roadmap.md` | Mark ingest work |

---

## Task 1: DB Migration

**Files:**
- Create: `api/internal/db/migrations/012_incoming_media_items.sql`

- [ ] **Write migration**

```sql
-- 012_incoming_media_items.sql

CREATE TABLE IF NOT EXISTS incoming_media_items (
    id                      BIGSERIAL    PRIMARY KEY,
    source_path             TEXT         NOT NULL,
    source_filename         TEXT         NOT NULL,
    normalized_name         TEXT,
    tmdb_id                 TEXT,
    content_kind            TEXT         NOT NULL DEFAULT 'movie',
    file_size_bytes         BIGINT,
    stable_since            TIMESTAMPTZ,
    status                  TEXT         NOT NULL DEFAULT 'new',
    attempts                INT          NOT NULL DEFAULT 0,
    claimed_at              TIMESTAMPTZ,
    claim_expires_at        TIMESTAMPTZ,
    quality_score           INT,
    is_upgrade_candidate    BOOLEAN      NOT NULL DEFAULT FALSE,
    duplicate_of_movie_id   BIGINT       REFERENCES movies(id) ON DELETE SET NULL,
    review_reason           TEXT,
    api_job_id              TEXT,
    error_message           TEXT,
    local_path              TEXT,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT incoming_status_check CHECK (status IN (
        'new','claimed','copying','copied','completed',
        'failed','skipped','review_duplicate','review_unknown_quality','upgrade_candidate'
    ))
);

CREATE UNIQUE INDEX IF NOT EXISTS incoming_media_items_source_path_key
    ON incoming_media_items (source_path);

CREATE INDEX IF NOT EXISTS incoming_media_items_status_idx
    ON incoming_media_items (status, stable_since);

CREATE INDEX IF NOT EXISTS incoming_media_items_claim_expires_idx
    ON incoming_media_items (claim_expires_at)
    WHERE status = 'claimed';
```

- [ ] **Verify: restart API, check `\d incoming_media_items`**

- [ ] **Commit**
```bash
git add api/internal/db/migrations/012_incoming_media_items.sql
git commit -m "feat(db): add incoming_media_items table for scanner ingest queue"
```

---

## Task 2: API Model

**Files:**
- Create: `api/internal/model/incoming.go`

- [ ] **Write model** — `IncomingItem`, status constants, `RegisterIncomingRequest`, `ClaimIncomingRequest/Response`, `ProgressIncomingRequest`, `FailIncomingRequest`, `CompleteIncomingRequest/Response`

`IncomingItem` fields mirror the table exactly (all nullable DB columns use pointer types).

`CompleteIncomingRequest` takes `{ID int64, LocalPath string}` — not a job_id, because the API creates the job.
`CompleteIncomingResponse` returns `{JobID string}`.

- [ ] **Commit**
```bash
git add api/internal/model/incoming.go
git commit -m "feat(api): add IncomingItem model and ingest request/response types"
```

---

## Task 3: ServiceToken Auth Middleware

**Files:**
- Create: `api/internal/auth/service_token.go`

Pattern: identical to `PlayerKeyMiddleware` in `api/internal/auth/auth.go` — read and follow that pattern exactly.

- [ ] **Write failing test**

```go
// api/internal/auth/service_token_test.go
func TestServiceTokenMiddleware_Valid(t *testing.T) { /* 200 when header matches */ }
func TestServiceTokenMiddleware_Invalid(t *testing.T) { /* 401 when header wrong */ }
func TestServiceTokenMiddleware_Missing(t *testing.T) { /* 401 when header absent */ }
```

- [ ] **Run test — expect FAIL**
```bash
cd api && go test ./internal/auth/... -run TestServiceToken -v
```

- [ ] **Implement**
```go
// api/internal/auth/service_token.go
package auth

import "net/http"

func ServiceTokenMiddleware(token string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if r.Header.Get("X-Service-Token") != token {
                http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

- [ ] **Run test — expect PASS**

- [ ] **Add config fields** — in `api/internal/config/config.go`:
  - `IngestServiceToken string` to struct
  - `IngestMaxAttempts  int`
  - In `Load()`: `IngestServiceToken: getEnv("INGEST_SERVICE_TOKEN", "")` and `IngestMaxAttempts: intEnv("INGEST_MAX_ATTEMPTS", 3)`

Note: `intEnv` doesn't exist in API config — implement inline or mirror from worker config.

- [ ] **Commit**
```bash
git add api/internal/auth/service_token.go api/internal/auth/service_token_test.go api/internal/config/config.go
git commit -m "feat(api): add ServiceTokenMiddleware and ingest config fields"
```

---

## Task 4: Ingest Repository

**Files:**
- Create: `api/internal/repository/incoming.go`

Key SQL design decisions:
- `Register`: `INSERT ... ON CONFLICT (source_path) DO UPDATE` — idempotent, only update non-null fields
- `ClaimBatch`: single CTE — first reset expired leases, then claim new ones:
  ```sql
  WITH reset_expired AS (
      UPDATE incoming_media_items
      SET status='new', claim_expires_at=NULL, updated_at=NOW()
      WHERE status='claimed' AND claim_expires_at < NOW()
  )
  UPDATE incoming_media_items
  SET status='claimed', claimed_at=NOW(), claim_expires_at=$2, attempts=attempts+1, updated_at=NOW()
  WHERE id IN (
      SELECT id FROM incoming_media_items
      WHERE status='new'
      ORDER BY stable_since ASC NULLS LAST, id ASC
      LIMIT $1
      FOR UPDATE SKIP LOCKED
  )
  RETURNING ...
  ```
  Pass `expiresAt := time.Now().Add(ttl)` as `$2` (TIMESTAMPTZ, not string interval).

- `Progress`: updates status to 'copying' or 'copied' when current status is in ('claimed','copying')
- `Fail`: resets to 'new' if `attempts < maxAttempts`, else 'failed'; clears `claim_expires_at`
- `Complete`: sets status='completed', api_job_id, local_path, updated_at

Use idiomatic pgx scanning — `pgx.Row` and `pgx.Rows` directly, no interface tricks.

- [ ] **Write tests** (integration, require real DATABASE_URL):
  - `TestRegister_Idempotent` — second register returns same ID
  - `TestClaimBatch_ClaimsNewItems` — claims N items, sets status=claimed
  - `TestClaimBatch_SkipsAlreadyClaimed` — does not double-claim
  - `TestClaimBatch_ResetsExpiredLease` — expired claimed item is re-claimable
  - `TestFail_ResetsToNewBelowMax` — attempts < max → status='new'
  - `TestFail_SetsFailedAtMax` — attempts >= max → status='failed'
  - `TestComplete_SetsJobID` — status='completed', api_job_id set

- [ ] **Run tests — expect FAIL**
```bash
cd api && DATABASE_URL="postgres://..." go test ./internal/repository/... -run TestRegister -v
```

- [ ] **Implement `incoming.go`**

- [ ] **Run tests — expect PASS**

- [ ] **Commit**
```bash
git add api/internal/repository/incoming.go api/internal/repository/incoming_test.go
git commit -m "feat(api): add IncomingRepository with idempotent register and atomic claim"
```

---

## Task 5: Ingest Service

**Files:**
- Create: `api/internal/service/ingest.go`

The service needs:
- `IncomingRepository` — for all state updates
- `JobRepository` — to create a `media_job` row during `Complete`
- `queue.Client` — to push `ConvertPayload` to `convert_queue` during `Complete`
- `config.Config` — for `MediaRoot`, `IngestMaxAttempts`

`Complete` flow:
1. Validate `id` and `local_path`
2. Build `outputPath = cfg.MediaRoot + "/converted/movies"`
3. Generate `jobID = fmt.Sprintf("ingest-%d", req.ID)` — deterministic, idempotent
4. Upsert `media_job` with `source_type="ingest"`, `request_id=jobID`, `status=queued`
5. Push `ConvertPayload` to `convert_queue` (same shape as `model.ConvertPayload` in api/internal/model)
6. Call `repo.Complete(ctx, req.ID, jobID)`
7. Return `CompleteIncomingResponse{JobID: jobID}`

For `FinalDir` in the convert payload: use `NormalizedName` if available on the item, else `fmt.Sprintf("ingest_%d", req.ID)`.

**Important:** The service needs the `IncomingItem` to build the convert payload (for `normalized_name`, `tmdb_id`). `Complete` should first `GetByID` to fetch the item, then proceed.

Add `GetByID(ctx, id) (*IncomingItem, error)` to the repository.

- [ ] **Write failing unit tests** — validate empty source_path, empty local_path, zero ID

- [ ] **Run — expect FAIL**

- [ ] **Implement service**

- [ ] **Run — expect PASS**

- [ ] **Commit**
```bash
git add api/internal/service/ingest.go api/internal/service/ingest_test.go
git commit -m "feat(api): add IngestService; complete creates media_job and pushes convert_queue"
```

---

## Task 6: Ingest HTTP Handlers

**Files:**
- Create: `api/internal/handler/ingest.go`

Look at `api/internal/handler/jobs.go` for patterns: `respondJSON`, error handling, JSON decode.

Handlers: `Register`, `Claim`, `Progress`, `Fail`, `Complete`

- `Claim`: if `req.Limit <= 0`, default to 3; if `req.ClaimTTLSec <= 0`, default to 900
- `Fail`: pass `cfg.IngestMaxAttempts` (thread through handler constructor)
- `Complete`: return 200 with `CompleteIncomingResponse{JobID: jobID}`
- All others: 204 on success, 400 on validation error, 500 on internal error

Handler constructor: `NewIngestHandler(svc *service.IngestService, maxAttempts int) *IngestHandler`

- [ ] **Write failing handler tests** — decode errors (400), success paths

- [ ] **Run — expect FAIL**

- [ ] **Implement**

- [ ] **Run — expect PASS**

- [ ] **Commit**
```bash
git add api/internal/handler/ingest.go api/internal/handler/ingest_test.go
git commit -m "feat(api): add IngestHandler for 5 ingest endpoints"
```

---

## Task 7: Wire Routes and Dependencies

**Files:**
- Modify: `api/internal/server/server.go`
- Modify: `api/cmd/api/main.go`

**server.go changes:**
1. Add `IngestHandler *handler.IngestHandler` to `Dependencies`
2. In `New()`, add after player routes:
```go
r.Route("/api/ingest", func(r chi.Router) {
    r.Use(auth.ServiceTokenMiddleware(deps.Cfg.IngestServiceToken))
    r.Post("/incoming/register",  deps.IngestHandler.Register)
    r.Post("/incoming/claim",     deps.IngestHandler.Claim)
    r.Post("/incoming/progress",  deps.IngestHandler.Progress)
    r.Post("/incoming/fail",      deps.IngestHandler.Fail)
    r.Post("/incoming/complete",  deps.IngestHandler.Complete)
})
```

**main.go changes** (follow existing wiring pattern exactly):
```go
incomingRepo := repository.NewIncomingRepository(pool)
ingestSvc    := service.NewIngestService(incomingRepo, jobRepo, redisClient, cfg)
ingestH      := handler.NewIngestHandler(ingestSvc, cfg.IngestMaxAttempts)
// add IngestHandler: ingestH to server.Dependencies{...}
```

Note: `jobRepo` is already wired, `redisClient` is already wired.

- [ ] **Build check**
```bash
cd api && go build ./...
```
Expected: no errors.

- [ ] **Smoke test**
```bash
curl -s -X POST http://localhost:8000/api/ingest/incoming/register \
  -H "X-Service-Token: testtoken" \
  -H "Content-Type: application/json" \
  -d '{"source_path":"/incoming/test.mkv","source_filename":"test.mkv","content_kind":"movie"}'
# Expected: 200 with {id, status:"new", ...}

curl -s -X POST http://localhost:8000/api/ingest/incoming/register \
  -H "X-Service-Token: wrongtoken" \
  -H "Content-Type: application/json" \
  -d '{}'
# Expected: 401
```

- [ ] **Commit**
```bash
git add api/internal/server/server.go api/cmd/api/main.go
git commit -m "feat(api): wire /api/ingest/* routes with ServiceTokenMiddleware"
```

---

## Task 8: Worker Config

**Files:**
- Modify: `worker/internal/config/config.go`

Add to `Config` struct and `Load()`:
```go
ConverterAPIURL       string  // CONVERTER_API_URL, default "http://api:8000"
IngestServiceToken    string  // INGEST_SERVICE_TOKEN, default ""
IngestConcurrency     int     // INGEST_CONCURRENCY, default 3
IngestClaimTTLSec     int     // INGEST_CLAIM_TTL_SEC, default 900
IngestMaxAttempts     int     // INGEST_MAX_ATTEMPTS, default 3
IngestSourceRemote    string  // INGEST_SOURCE_REMOTE, default ""
IngestSourceBasePath  string  // INGEST_SOURCE_BASE_PATH, default "/incoming"
```

- [ ] **Add fields and defaults**

- [ ] **Build check**
```bash
cd worker && go build ./...
```

- [ ] **Commit**
```bash
git add worker/internal/config/config.go
git commit -m "feat(worker): add ingest config fields"
```

---

## Task 9: Worker Ingest Client

**Files:**
- Create: `worker/internal/ingest/client.go`

HTTP client for ingest API. Methods:
- `Claim(ctx, limit, claimTTLSec int) ([]IncomingItem, error)`
- `Progress(ctx, id int64, status string, pct int) error`
- `Fail(ctx, id int64, msg string) error`
- `Complete(ctx, id int64, localPath string) (jobID string, err error)`

`IncomingItem` (worker-side struct, only fields worker needs):
```go
type IncomingItem struct {
    ID             int64   `json:"id"`
    SourcePath     string  `json:"source_path"`
    SourceFilename string  `json:"source_filename"`
    ContentKind    string  `json:"content_kind"`
    NormalizedName *string `json:"normalized_name,omitempty"`
    TMDBID         *string `json:"tmdb_id,omitempty"`
}
```

Look at `worker/internal/transfer/transfer.go` for how rclone is called — follow the same exec pattern for the client's HTTP calls (`http.Client` with 30s timeout).

- [ ] **Write failing test** — mock HTTP server verifies correct path, header, body; checks response parsing

```go
func TestClient_Claim_ParsesItems(t *testing.T) { ... }
func TestClient_Complete_ReturnsJobID(t *testing.T) { ... }
func TestClient_Fail_SendsError(t *testing.T) { ... }
```

- [ ] **Run — expect FAIL**

- [ ] **Implement `client.go`**

- [ ] **Run — expect PASS**

- [ ] **Commit**
```bash
git add worker/internal/ingest/client.go worker/internal/ingest/client_test.go
git commit -m "feat(worker): add ingest API client"
```

---

## Task 10: Worker Puller (rclone copy)

**Files:**
- Create: `worker/internal/ingest/puller.go`

Pattern: look at `worker/internal/transfer/transfer.go` for how rclone is invoked with `exec.CommandContext`.

```go
type Puller struct {
    remote   string // e.g. "storage"
    basePath string // e.g. "/incoming"
}

// Copy copies sourcePath (full path on remote, e.g. "/incoming/subdir/film.mkv")
// into destDir on local disk. Returns absolute local path of copied file.
func (p *Puller) Copy(ctx context.Context, sourcePath, destDir string) (string, error)
```

rclone invocation: `rclone copy {remote}:{sourcePath} {destDir} --progress --stats-one-line --stats=5s`

Stdout/stderr to `slog` (not os.Stdout — follow transfer.go pattern for stderr parsing if any).

- [ ] **Write failing test** — verifies `buildArgs` output (no actual rclone execution)

- [ ] **Run — expect FAIL**

- [ ] **Implement `puller.go`**

- [ ] **Run — expect PASS**

- [ ] **Commit**
```bash
git add worker/internal/ingest/puller.go worker/internal/ingest/puller_test.go
git commit -m "feat(worker): add rclone-based Puller for ingest source copy"
```

---

## Task 11: Worker Main Loop

**Files:**
- Create: `worker/internal/ingest/worker.go`

```go
type Worker struct {
    client       *Client
    puller       *Puller
    mediaRoot    string
    concurrency  int
    claimTTLSec  int
    pollInterval time.Duration // 10s
}
```

Poll loop (single goroutine, not per-item goroutines — worker is already launched N times from main.go):
```
for {
    select ctx.Done → return
    select ticker →
        items = client.Claim(ctx, 1, claimTTLSec)  // claim 1 item per goroutine
        if len(items) == 0 → continue
        processItem(ctx, items[0])
}
```

**Note on concurrency model:** Do NOT launch goroutines inside the worker. In `main.go` we launch `IngestConcurrency` goroutines each running `worker.Run(ctx)`, same as how `dlWorker` and `cvWorker` work. Each goroutine claims 1 item at a time.

`processItem(ctx, item)`:
1. `destDir = mediaRoot + "/downloads/ingest_" + strconv.FormatInt(item.ID, 10)`
2. `client.Progress(ctx, item.ID, "copying", 0)`
3. `localPath, err = puller.Copy(ctx, item.SourcePath, destDir)` — on error → `client.Fail(...)` → return
4. `client.Progress(ctx, item.ID, "copied", 100)`
5. `jobID, err = client.Complete(ctx, item.ID, localPath)` — on error → `client.Fail(...)` → return
6. log `"ingest item processed"` with `job_id`

- [ ] **Write failing tests**:
```go
func TestWorker_ProcessItem_FailsOnCopyError(t *testing.T)
func TestWorker_ProcessItem_FailsOnCompleteError(t *testing.T)
func TestWorker_ProcessItem_Success(t *testing.T)
```
Use mock client and mock puller (interfaces).

- [ ] **Run — expect FAIL**

- [ ] **Implement `worker.go`** — define `ClientInterface` and `PullerInterface` for testability; `Client` and `Puller` satisfy them.

- [ ] **Run — expect PASS**

- [ ] **Commit**
```bash
git add worker/internal/ingest/worker.go worker/internal/ingest/worker_test.go
git commit -m "feat(worker): add ingest poll loop with claim/copy/complete flow"
```

---

## Task 12: Wire Ingest Worker in main.go + .env.example

**Files:**
- Modify: `worker/cmd/worker/main.go`
- Modify: `.env.example`

**main.go** — follow existing transfer worker gating pattern exactly:
```go
if cfg.IngestServiceToken != "" && cfg.IngestSourceRemote != "" {
    ingestClient := ingest.NewClient(cfg.ConverterAPIURL, cfg.IngestServiceToken)
    ingestPuller := ingest.NewPuller(cfg.IngestSourceRemote, cfg.IngestSourceBasePath)
    ingestWkr    := ingest.New(ingestClient, ingestPuller, cfg.MediaRoot,
                               cfg.IngestClaimTTLSec)
    for i := 0; i < cfg.IngestConcurrency; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ingestWkr.Run(ctx)
        }()
    }
    slog.Info("ingest worker enabled", "concurrency", cfg.IngestConcurrency)
} else {
    slog.Info("ingest worker disabled (INGEST_SERVICE_TOKEN or INGEST_SOURCE_REMOTE not set)")
}
```

**.env.example** — append at end, following existing comment style:
```bash
# ── Ingest worker (Block 2 — scanner pull) ───────────────────────────────────
INGEST_SERVICE_TOKEN=          # shared secret: scanner, ingest worker, converter API
INGEST_CONCURRENCY=3           # parallel ingest slots (each claims 1 item)
INGEST_CLAIM_TTL_SEC=900       # lease TTL in seconds (15 min)
INGEST_MAX_ATTEMPTS=3          # retries before permanent failure
INGEST_SOURCE_REMOTE=          # rclone remote name for incoming/ on storage server
INGEST_SOURCE_BASE_PATH=/incoming
CONVERTER_API_URL=http://api:8000  # ingest worker → converter API base URL
```

- [ ] **Build check**
```bash
cd worker && go build ./...
```

- [ ] **Commit**
```bash
git add worker/cmd/worker/main.go .env.example
git commit -m "feat(worker): register ingest worker goroutines in main; document env vars"
```

---

## Task 13: Documentation

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/contracts/api.md`
- Modify: `docs/contracts/worker.md`
- Modify: `docs/roadmap/roadmap.md`
- Create: ADR via `./scripts/new-adr.sh "incoming scanner api-driven ingest split"`

**CHANGELOG.md** under `## [Unreleased]`:
```markdown
### Added
- `api/internal/db/migrations/012_incoming_media_items.sql`: ingest queue table with status machine, lease expiry, quality and duplicate fields
- `api/internal/handler/ingest.go`: 5 service-token-protected endpoints: register, claim, progress, fail, complete
- `api/internal/service/ingest.go`: ingest business logic; complete endpoint creates media_job and pushes to convert_queue
- `api/internal/repository/incoming.go`: atomic batch claim with SKIP LOCKED and expired-lease reset in single CTE
- `api/internal/auth/service_token.go`: X-Service-Token middleware for service-to-service auth
- `worker/internal/ingest/`: ingest worker — polls claim API, rclone-copies source file, calls complete via API
```

**docs/contracts/api.md** — add section `## Ingest API (service-to-service)` documenting all 5 endpoints with auth, request bodies, response shapes.

**ADR** — Context: scanner on storage server, no direct DB access allowed. Decision: API-only status; complete endpoint owns job creation and queue push. Consequences: INGEST_SERVICE_TOKEN required; worker never touches Redis or DB directly.

- [ ] **Create ADR, fill all sections**

- [ ] **Update all docs**

- [ ] **Commit**
```bash
git add CHANGELOG.md docs/
git commit -m "docs: document ingest API endpoints, worker, and ADR for api-driven ingest split"
```
