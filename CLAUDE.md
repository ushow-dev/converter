# CLAUDE.md — AI Assistant Guide

## Project Overview

This is a **self-hosted media processing system** for downloading, converting, and streaming video content. It consists of:

- **API** — Go HTTP service (admin & player endpoints)
- **Worker** — Go background processor (download + HLS conversion)
- **Frontend** — Next.js admin UI
- **Infrastructure** — Docker Compose stack (Postgres, Redis, qBittorrent, Prowlarr)

The root of this repository is `/converter/`. All code, docs, and config live here.

---

## Repository Rules (MANDATORY)

### 1. Documentation must stay in sync
- When changing API endpoints → update `docs/contracts/api.md`
- When changing the worker pipeline → update `docs/converter/pipeline.md`
- When changing architecture → update `ARCHITECTURE.md`, `REPO_MAP.md`, and relevant `docs/architecture/` files
- When adding directories → update `REPO_MAP.md`

### 2. API contracts are strict
- Never remove or rename API fields without versioning
- Never change queue message formats without updating both API and Worker simultaneously
- Queue payload structs in `api/internal/model/` and `worker/internal/model/` MUST stay in sync
- Document all changes in `docs/contracts/`

### 3. Service boundaries are enforced
- API service MUST NOT import worker packages
- Worker MUST NOT expose HTTP admin endpoints
- Frontend communicates ONLY with the API service
- Player embed communicates with the API via `PLAYER_API_KEY` (separate auth from admin JWT)

### 4. Security constraints
- Never commit `.env` files with real credentials
- Always use `.env.example` for documentation of config values
- Media signing key (`MEDIA_SIGNING_KEY`) should always be set in production
- Admin password must be bcrypt-hashed in production (`$2y$...` format)
- CORS is intentionally open (`*`) for the player embed use case — do not restrict without checking player functionality

### 5. Database migrations
- Migrations live in `api/db/migrations/`
- Use sequential numbering: `00N_description.sql`
- Migrations are applied automatically on API startup
- NEVER modify existing migrations — always add new ones

### 6. Worker pipeline stages
- Download stage → Convert stage is the canonical flow
- Uploads bypass download and go directly to convert_queue
- Remote HTTP downloads use a separate `remote_download_queue`
- Always update job status atomically with stage transitions

### 7. Media storage layout
- Raw downloads: `/media/downloads/{jobID}/`
- Temp FFmpeg workspace: `/media/temp/{jobID}/`
- Converted HLS (movies): `/media/converted/movies/{movieStorageKey}/`
- Converted HLS (series): `/media/converted/series/{seriesStorageKey}/s{NN}/e{NN}/`
- This layout is assumed throughout — do not reorganize without updating all path references

---

## Architectural Constraints

- **Queue**: Redis BLPOP (list-based, not pub/sub) — order matters
- **Locking**: Redis NX locks prevent duplicate job processing
- **Idempotency**: `request_id` UNIQUE in DB prevents duplicate jobs from retries
- **Pagination**: Cursor-based (job_id as cursor), not offset-based
- **Auth**: Two separate auth schemes — JWT for admin, API key for player
- **HLS**: Multi-resolution output (360p/480p/720p) with keyframe-aligned segments

---

## Key Environment Variables

| Variable | Purpose |
|---|---|
| `JWT_SECRET` | HS256 admin token signing |
| `PLAYER_API_KEY` | Player service authentication |
| `MEDIA_SIGNING_KEY` | Optional HLS URL signing (secure_link) |
| `MEDIA_BASE_URL` | Base URL for HLS segment delivery |
| `DOWNLOAD_CONCURRENCY` | Parallel torrent downloads |
| `CONVERT_CONCURRENCY` | Parallel FFmpeg conversions |

---

## Where to Find Things

| What | Where |
|---|---|
| API route definitions | `api/internal/server/server.go` |
| Business logic | `api/internal/service/` |
| Queue message structs | `api/internal/model/` |
| FFmpeg profiles | `worker/internal/ffmpeg/` |
| DB schema | `api/internal/db/migrations/` |
| Docker config | `docker-compose.yml` |
| Architecture docs | `docs/architecture/` |
| API contracts | `docs/contracts/` |
| Series models | `api/internal/model/series.go`, `worker/internal/model/series.go` |
| Series handlers | `api/internal/handler/series.go` |
| Series repositories | `api/internal/repository/series.go` |
| Audio track repos | `api/internal/repository/audio_track.go` |
| Path resolution | `worker/internal/paths/paths.go` |
| Startup recovery | `worker/internal/recovery/recovery.go` |
| Audio probing | `worker/internal/ffmpeg/probe.go` |

---

## Deployment

### API Server (178.104.100.36)

**CRITICAL: always use `-f docker-compose.api.yml`** — the default `docker-compose.yml` does NOT publish Redis port 6379. Using it breaks Worker connectivity to Redis and stalls all queued jobs.

```bash
ssh -i ~/.ssh/id_rsa_personal root@178.104.100.36
cd /opt/converter && git pull origin main
docker compose -f docker-compose.api.yml build api frontend
docker compose -f docker-compose.api.yml up -d
```

### Worker Server (178.104.53.215)

```bash
ssh -i ~/.ssh/id_ed25519 root@178.104.53.215
cd /opt/converter && git pull origin main
docker compose -f docker-compose.worker.yml build worker
docker compose -f docker-compose.worker.yml up -d
```

If Worker gets `READONLY` errors from Redis after API server redeploy — restart it:
```bash
docker compose -f docker-compose.worker.yml restart worker
```

---

## Common Tasks

### Adding a new API endpoint
1. Add handler in `api/internal/handler/`
2. Register route in `api/internal/server/server.go`
3. Add repository methods if DB access needed
4. Update `docs/contracts/api.md`

### Adding a new worker queue
1. Define message struct in both `api/internal/model/` and `worker/internal/model/`
2. Add queue name constant to both services
3. Add consumer in `worker/internal/`
4. Register worker goroutine in `worker/cmd/worker/main.go`
5. Update `docs/contracts/worker.md`

### Adding a DB migration
1. Create `api/db/migrations/00N_description.sql`
2. Write idempotent SQL (use `IF NOT EXISTS`, `IF EXISTS`)
3. Test locally by restarting the API service

---

## Commit Convention

All commits MUST follow Conventional Commits format:
```
<type>(<scope>): <description>
```

Types: `feat` `fix` `docs` `style` `refactor` `perf` `test` `chore` `ci` `build` `revert`
Scopes: `api` `worker` `frontend` `docker` `docs` `deps` `auth` `queue` `player` `subtitles` `ffmpeg` `db` `config` `scanner`

Rules:
- Description starts with lowercase letter
- No trailing period
- Max 100 characters

Examples:
```
feat(api): add rate limiting middleware for login endpoint
fix(worker): handle ffmpeg timeout on corrupted input files
docs(contracts): update ConvertPayload schema
chore(deps): bump pgx to v5.7.3
```

Full reference: `docs/contributing/conventional-commits.md`

---

## Maintenance Rules for AI Assistants

1. **Always update documentation when architecture changes**
2. **Never change API contracts without updating `docs/contracts/`**
3. **Maintain service boundaries** — API, Worker, Frontend are separate services
4. **Keep worker pipeline documented** in `docs/converter/pipeline.md`
5. **Update `REPO_MAP.md`** when directories are added or removed
6. **Rotate secrets** — never use example values in production
7. **Check migrations** — always prefer adding new migrations over modifying existing ones

---

## Mandatory Protocol: Every Code Change

When making ANY code change, you MUST complete ALL of the following steps before considering the task done.

### Step 1 — CHANGELOG.md (ALWAYS required)

Add an entry under `## [Unreleased]` in `CHANGELOG.md`.

Format:
```markdown
### Added | Changed | Fixed | Removed | Security
- `path/to/file`: short description of what changed and why
```

Rules:
- `Added` — new file, feature, endpoint, field
- `Changed` — modified behaviour, renamed, updated
- `Fixed` — bug fix
- `Removed` — deleted file, field, endpoint
- `Security` — any security-related change

### Step 2 — ADR (required for architectural decisions)

Create a new ADR if your change involves ANY of:
- Adding or removing a service, queue, or database table
- Changing authentication or authorization scheme
- Choosing a new library or infrastructure component
- Changing queue payload format (breaking or non-breaking)
- Any decision that is hard to reverse

How to create:
```bash
./scripts/new-adr.sh "decision title"
```

Then fill in all sections and add to `docs/decisions/README.md` table.

Skip ADR only for: bug fixes, UI tweaks, dependency bumps, documentation-only changes.

### Step 3 — Roadmap (required when completing or adding planned work)

Update `docs/roadmap/roadmap.md`:
- If you completed a roadmap task → remove it from roadmap, add to CHANGELOG.md
- If you discovered new work needed → add it to the appropriate priority section
- If you changed the scope of a planned task → update its description

### Step 4 — Verify related docs are in sync

| What changed | Also update |
|---|---|
| API endpoint added/changed | `docs/contracts/api.md` + `docs/architecture/services.md` (route list) |
| Queue payload changed | `docs/contracts/worker.md` |
| FFmpeg pipeline changed | `docs/converter/pipeline.md` |
| New directory created | `REPO_MAP.md` |
| Auth scheme changed | ADR + `docs/architecture/services.md` |
| DB migration added | `docs/architecture/database-schema.md` + `docs/architecture/services.md` (tables section) + `REPO_MAP.md` (migration count) |
| Worker goroutine added/removed | `docs/architecture/services.md` (горутины section) + `docs/architecture/modules/worker.md` |
| Worker config var added/changed | `docs/architecture/modules/worker.md` (Конфигурация) + `docs/contracts/worker.md` |
| Scanner API changed | `docs/scanner/api.md` |
| Scanner DB schema changed | `docs/scanner/database.md` |
| Scanner architecture changed | `docs/scanner/README.md` + `docs/architecture/modules/scanner.md` |
| Scanner config changed | `docs/scanner/README.md` (Configuration section) |
| Media storage layout changed | `REPO_MAP.md` (media section) + `CLAUDE.md` (Media storage layout) + `docs/architecture/database-schema.md` |

---

## Decision Log Quick Reference

ADRs live in `docs/decisions/`. Next ADR number: check `docs/decisions/README.md`.

| Decision | ADR |
|---|---|
| Redis BLPOP for queues | ADR-001 |
| Two Go modules (api + worker) | ADR-002 |
| Cursor pagination | ADR-003 |
| MD5 URL signing | ADR-004 |
| HLS 360/480/720p | ADR-005 |
| Dual auth (JWT + API Key) | ADR-006 |
| P2P HLS (p2p-media-loader) | ADR-010 |
