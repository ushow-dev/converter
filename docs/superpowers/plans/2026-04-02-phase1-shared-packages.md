# Phase 1: Shared Packages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract duplicated models, constants, and error types from api/ and worker/ into a shared Go module.

**Architecture:** Create `shared/` Go module at repo root with `model/` and `errors/` packages. Both api/ and worker/ import via `replace` directive. Queue payload structs unified under single names. Domain-specific models (Movie, Asset with different field sets) stay in their services.

**Tech Stack:** Go 1.23, Go modules with replace directives

**Spec:** `docs/superpowers/specs/2026-04-02-full-refactoring-design.md` — Phase 1

---

## File Structure

### New files

| File | Purpose |
|---|---|
| `shared/go.mod` | Go module definition for `app/shared` |
| `shared/model/queue.go` | Unified queue envelope + job payload structs |
| `shared/model/status.go` | JobStatus, JobStage, JobPriority types and constants |
| `shared/model/constants.go` | ContentType, SourceType constants |
| `shared/model/proxy.go` | ProxyConfig struct |
| `shared/errors/errors.go` | Shared error types: ErrNotFound, ErrConflict, ErrValidation |

### Modified files

| File | Changes |
|---|---|
| `api/go.mod` | Add `require app/shared` + `replace` directive |
| `worker/go.mod` | Same |
| `api/internal/model/model.go` | Remove duplicated queue structs, import from shared |
| `worker/internal/model/model.go` | Remove duplicated queue structs, import from shared |
| `api/internal/repository/*.go` | Use `shared/errors.ErrNotFound` instead of local sentinel |
| `worker/internal/repository/*.go` | Same |
| All files referencing queue types | Update import paths |

---

## Task 1: Create shared Go module with status constants

**Files:**
- Create: `shared/go.mod`
- Create: `shared/model/status.go`
- Create: `shared/model/constants.go`

- [ ] **Step 1: Create shared module**

```bash
mkdir -p shared/model shared/errors
```

Create `shared/go.mod`:
```
module app/shared

go 1.23
```

- [ ] **Step 2: Create status constants**

Create `shared/model/status.go`:
```go
package model

// JobStatus represents the lifecycle state of a media job.
type JobStatus string

const (
	StatusCreated    JobStatus = "created"
	StatusQueued     JobStatus = "queued"
	StatusInProgress JobStatus = "in_progress"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// JobStage represents the active processing stage.
type JobStage string

const (
	StageDownload JobStage = "download"
	StageConvert  JobStage = "convert"
	StageTransfer JobStage = "transfer"
)

// JobPriority represents processing priority.
type JobPriority string

const (
	PriorityLow    JobPriority = "low"
	PriorityNormal JobPriority = "normal"
	PriorityHigh   JobPriority = "high"
)
```

- [ ] **Step 3: Create content/source type constants**

Create `shared/model/constants.go`:
```go
package model

const (
	ContentTypeMovie   = "movie"
	ContentTypeEpisode = "episode"

	SourceTypeTorrent = "torrent"
	SourceTypeUpload  = "upload"
	SourceTypeHTTP    = "http"
	SourceTypeIngest  = "ingest"
)
```

- [ ] **Step 4: Verify shared module builds**

Run: `cd shared && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add shared/
git commit -m "refactor: create shared Go module with status and content type constants"
```

---

## Task 2: Add queue payload structs and ProxyConfig to shared

**Files:**
- Create: `shared/model/queue.go`
- Create: `shared/model/proxy.go`

- [ ] **Step 1: Create ProxyConfig**

Create `shared/model/proxy.go`:
```go
package model

// ProxyConfig holds optional proxy settings for remote HTTP requests.
type ProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
}
```

- [ ] **Step 2: Create unified queue structs**

Create `shared/model/queue.go`:
```go
package model

import "time"

// ── Download queue ──────────────────────────────────────────────────────────

// DownloadMessage is the envelope for download_queue.
type DownloadMessage struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	JobType       string      `json:"job_type"`
	ContentType   string      `json:"content_type"`
	CorrelationID string      `json:"correlation_id"`
	Attempt       int         `json:"attempt"`
	MaxAttempts   int         `json:"max_attempts"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       DownloadJob `json:"payload"`
}

// DownloadJob is the inner payload for a download task.
type DownloadJob struct {
	SourceType string `json:"source_type"`
	SourceRef  string `json:"source_ref"`
	IMDbID     string `json:"imdb_id"`
	TMDBID     string `json:"tmdb_id"`
	Title      string `json:"title"`
	TargetDir  string `json:"target_dir"`
	Priority   string `json:"priority"`
	RequestID  string `json:"request_id"`
}

// ── Convert queue ───────────────────────────────────────────────────────────

// ConvertMessage is the envelope for convert_queue.
type ConvertMessage struct {
	SchemaVersion string     `json:"schema_version"`
	JobID         string     `json:"job_id"`
	JobType       string     `json:"job_type"`
	ContentType   string     `json:"content_type"`
	CorrelationID string     `json:"correlation_id"`
	Attempt       int        `json:"attempt"`
	MaxAttempts   int        `json:"max_attempts"`
	CreatedAt     time.Time  `json:"created_at"`
	Payload       ConvertJob `json:"payload"`
}

// ConvertJob is the inner payload for a convert task.
type ConvertJob struct {
	InputPath     string `json:"input_path"`
	OutputPath    string `json:"output_path"`
	OutputProfile string `json:"output_profile"`
	FinalDir      string `json:"final_dir"`
	IMDbID        string `json:"imdb_id"`
	TMDBID        string `json:"tmdb_id"`
	Title         string `json:"title"`
	StorageKey    string `json:"storage_key,omitempty"`
	SeriesID      *int64 `json:"series_id,omitempty"`
	SeasonNumber  *int   `json:"season_number,omitempty"`
	EpisodeNumber *int   `json:"episode_number,omitempty"`
}

// ── Remote download queue ───────────────────────────────────────────────────

// RemoteDownloadMessage is the envelope for remote_download_queue.
type RemoteDownloadMessage struct {
	SchemaVersion string            `json:"schema_version"`
	JobID         string            `json:"job_id"`
	JobType       string            `json:"job_type"`
	ContentType   string            `json:"content_type"`
	CorrelationID string            `json:"correlation_id"`
	Attempt       int               `json:"attempt"`
	MaxAttempts   int               `json:"max_attempts"`
	CreatedAt     time.Time         `json:"created_at"`
	Payload       RemoteDownloadJob `json:"payload"`
}

// RemoteDownloadJob is the inner payload for an HTTP download task.
type RemoteDownloadJob struct {
	SourceURL   string       `json:"source_url"`
	Filename    string       `json:"filename"`
	IMDbID      string       `json:"imdb_id"`
	TMDBID      string       `json:"tmdb_id"`
	Title       string       `json:"title"`
	StorageKey  string       `json:"storage_key,omitempty"`
	TargetDir   string       `json:"target_dir"`
	ProxyConfig *ProxyConfig `json:"proxy_config,omitempty"`
}

// ── Transfer queue ──────────────────────────────────────────────────────────

// TransferMessage is the envelope for transfer_queue.
type TransferMessage struct {
	SchemaVersion string      `json:"schema_version"`
	JobID         string      `json:"job_id"`
	CorrelationID string      `json:"correlation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Payload       TransferJob `json:"payload"`
}

// TransferJob holds details for a single rclone transfer operation.
type TransferJob struct {
	ContentID   int64  `json:"content_id"`
	StorageKey  string `json:"storage_key"`
	LocalPath   string `json:"local_path"`
	ContentType string `json:"content_type,omitempty"`
}

// ── Cancel queue ────────────────────────────────────────────────────────────

// CancelMessage wraps a cancel request with schema versioning.
type CancelMessage struct {
	SchemaVersion string `json:"schema_version"`
	JobID         string `json:"job_id"`
	CreatedAt     time.Time `json:"created_at"`
}
```

- [ ] **Step 3: Verify**

Run: `cd shared && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add shared/model/queue.go shared/model/proxy.go
git commit -m "refactor: add unified queue payloads and ProxyConfig to shared module"
```

---

## Task 3: Create shared error types

**Files:**
- Create: `shared/errors/errors.go`

- [ ] **Step 1: Create error package**

```go
// Package errors provides shared error types for api and worker services.
package errors

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound indicates the requested entity was not found.
	ErrNotFound = errors.New("not found")

	// ErrConflict indicates a uniqueness or conflict violation.
	ErrConflict = errors.New("conflict")
)

// ValidationError represents an input validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s: %s", e.Field, e.Message)
}

// IsNotFound reports whether err is or wraps ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsConflict reports whether err is or wraps ErrConflict.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// Wrap wraps err with additional context message.
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}
```

- [ ] **Step 2: Verify**

Run: `cd shared && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add shared/errors/
git commit -m "refactor: add shared error types package"
```

---

## Task 4: Wire shared module into api/

**Files:**
- Modify: `api/go.mod`
- Modify: `api/internal/model/model.go`
- Modify: all api files importing queue types

- [ ] **Step 1: Add replace directive to api/go.mod**

Add to `api/go.mod`:
```
require app/shared v0.0.0

replace app/shared => ../shared
```

- [ ] **Step 2: Remove duplicated queue structs from api/internal/model/model.go**

Remove: DownloadPayload, DownloadJob, ConvertPayload, ConvertJob, RemoteDownloadPayload, RemoteDownloadJob, TransferPayload, TransferJob, ProxyConfig from api model.

Keep: Job, Asset, Movie, Subtitle, SearchResult, StorageLocation (domain-specific).

Add type aliases for backward compatibility:
```go
import sharedmodel "app/shared/model"

// Queue type aliases — use shared definitions.
type DownloadPayload = sharedmodel.DownloadMessage
type ConvertPayload = sharedmodel.ConvertMessage
type RemoteDownloadPayload = sharedmodel.RemoteDownloadMessage
type TransferPayload = sharedmodel.TransferMessage
type DownloadJob = sharedmodel.DownloadJob
type ConvertJob = sharedmodel.ConvertJob
type RemoteDownloadJob = sharedmodel.RemoteDownloadJob
type TransferJob = sharedmodel.TransferJob
type ProxyConfig = sharedmodel.ProxyConfig
```

This preserves all existing import paths — no other files need changes.

- [ ] **Step 3: Update api repository ErrNotFound**

In `api/internal/repository/`, find where `ErrNotFound` is defined (likely a package-level var). Keep it for now but add a comment that it maps to `shared/errors.ErrNotFound`. Full migration to shared errors can happen in Phase 3.

- [ ] **Step 4: Verify**

Run: `cd api && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add api/go.mod api/internal/model/model.go
git commit -m "refactor(api): import queue types from shared module"
```

---

## Task 5: Wire shared module into worker/

**Files:**
- Modify: `worker/go.mod`
- Modify: `worker/internal/model/model.go`

- [ ] **Step 1: Add replace directive to worker/go.mod**

Add to `worker/go.mod`:
```
require app/shared v0.0.0

replace app/shared => ../shared
```

- [ ] **Step 2: Remove duplicated queue structs from worker model**

Remove: DownloadMessage, DownloadJob, ConvertMessage, ConvertJob, RemoteDownloadMessage, RemoteDownloadJob, TransferMessage, TransferJob, ProxyConfig, status/stage string constants.

Keep: Asset, Movie, Subtitle, StorageLocation, Series, Season, Episode, EpisodeAsset, AudioTrack (domain-specific).

Add type aliases:
```go
import sharedmodel "app/shared/model"

// Queue type aliases — use shared definitions.
type DownloadMessage = sharedmodel.DownloadMessage
type ConvertMessage = sharedmodel.ConvertMessage
type RemoteDownloadMessage = sharedmodel.RemoteDownloadMessage
type TransferMessage = sharedmodel.TransferMessage
type DownloadJob = sharedmodel.DownloadJob
type ConvertJob = sharedmodel.ConvertJob
type RemoteDownloadJob = sharedmodel.RemoteDownloadJob
type TransferJob = sharedmodel.TransferJob
type ProxyConfig = sharedmodel.ProxyConfig

// Status constants — use shared definitions.
const (
	StatusCreated    = string(sharedmodel.StatusCreated)
	StatusQueued     = string(sharedmodel.StatusQueued)
	StatusInProgress = string(sharedmodel.StatusInProgress)
	StatusCompleted  = string(sharedmodel.StatusCompleted)
	StatusFailed     = string(sharedmodel.StatusFailed)

	StageDownload = string(sharedmodel.StageDownload)
	StageConvert  = string(sharedmodel.StageConvert)
	StageTransfer = string(sharedmodel.StageTransfer)
)
```

- [ ] **Step 3: Verify**

Run: `cd worker && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add worker/go.mod worker/internal/model/model.go
git commit -m "refactor(worker): import queue types from shared module"
```

---

## Task 6: Use shared ContentType constants project-wide

**Files:**
- Modify: all Go files using string literals `"movie"`, `"episode"`, `"series"`

- [ ] **Step 1: Replace string literals in worker**

Search worker/ for `"movie"` and `"episode"` string literals in Go files. Replace with `sharedmodel.ContentTypeMovie` and `sharedmodel.ContentTypeEpisode` where they refer to content type (not queue names or other strings).

Key files: `converter.go`, `ingest/worker.go`, `transfer/transfer.go`, `recovery/recovery.go`.

- [ ] **Step 2: Replace string literals in api**

Same for api/ — `handler/player.go`, `handler/series.go`, `service/job.go`.

- [ ] **Step 3: Verify both build**

Run: `cd api && go build ./... && cd ../worker && go build ./...`

- [ ] **Step 4: Commit**

```bash
git commit -am "refactor: use shared ContentType constants instead of string literals"
```

---

## Dependency Graph

```
Task 1 (status constants)
  → Task 2 (queue structs)
    → Task 3 (error types)
      → Task 4 (wire api) + Task 5 (wire worker) — parallel
        → Task 6 (content type constants)
```
