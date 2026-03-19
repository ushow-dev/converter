# Remove Scanner Download Integration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Убрать интеграцию scanner-сервера из потока скачивания фильмов в конвертер-админке — все загрузки снова идут напрямую через `remote_download_queue` на converter worker.

**Architecture:** `JobService` перестаёт знать о `scannerAPIURL` и `serviceToken`. `ScannerHandler` (прокси для отображения загрузок scanner в очереди) остаётся, но получает свои зависимости напрямую, не через `JobService`. Во frontend убирается состояние `downloading` — `job_id` всегда присутствует в ответе.

**Tech Stack:** Go (api), TypeScript (Next.js frontend)

---

## Что меняется и почему

| Что | Было | Станет |
|---|---|---|
| `CreateRemoteDownloadJob` | проверяет `scannerAPIURL`, публичные URL шлёт на scanner | всегда кладёт в `remote_download_queue` |
| `JobService` struct | поля `scannerAPIURL`, `serviceToken` | убираются из `JobService` |
| `NewJobService(...)` | принимает 7 аргументов | принимает 5 аргументов |
| `forwardToScanner` | метод `JobService` | удаляется |
| `isPrivateURL` | функция в `job.go` | удаляется |
| `main.go` | передаёт `scannerAPIURL`/`serviceToken` в `JobService` | передаёт только в `ScannerHandler` (уже делает) |
| `DownloadItemState` в types.ts | `'downloading'` в union type | убирается |
| `upload/page.tsx` | обрабатывает ответ без `job_id` | упрощается, `job_id` всегда есть |

**Что НЕ меняется:**
- `ScannerHandler` — прокси `/api/admin/scanner/downloads` остаётся (показывает загрузки scanner в очереди)
- `scanner/scanner/api/server.py` — scanner API без изменений
- `scanner/scanner/loops/download_worker.py` — download_worker на scanner без изменений
- Вся инфраструктура scanner (scan_loop, move_worker, ingest_worker) — без изменений

---

## Файлы

| Файл | Действие | Что меняется |
|---|---|---|
| `api/internal/service/job.go` | Modify | Убрать поля `scannerAPIURL`/`serviceToken` из struct, удалить `forwardToScanner`, удалить `isPrivateURL`, упростить `NewJobService`, упростить `CreateRemoteDownloadJob` |
| `api/cmd/api/main.go` | Modify | Убрать передачу `scannerAPIURL`/`serviceToken` в `NewJobService` |
| `frontend/src/types/index.ts` | Modify | Убрать `'downloading'` из `DownloadItemState` |
| `frontend/src/app/upload/page.tsx` | Modify | Упростить обработку ответа — `state` всегда `'queued'`, `jobId` всегда есть |
| `CHANGELOG.md` | Modify | Добавить запись |

---

## Chunk 1: API — убрать scanner forwarding из JobService

### Task 1: Упростить `api/internal/service/job.go`

**Files:**
- Modify: `api/internal/service/job.go`

- [ ] **Step 1: Убрать поля `scannerAPIURL` и `serviceToken` из `JobService` struct**

```go
// Было:
type JobService struct {
    jobs          *repository.JobRepository
    movieRepo     *repository.MovieRepository
    queue         *queue.Client
    mediaRoot     string
    tmdbAPIKey    string
    scannerAPIURL string
    serviceToken  string
}

// Стало:
type JobService struct {
    jobs      *repository.JobRepository
    movieRepo *repository.MovieRepository
    queue     *queue.Client
    mediaRoot string
    tmdbAPIKey string
}
```

- [ ] **Step 2: Обновить `NewJobService` — убрать 2 аргумента**

```go
// Было:
func NewJobService(jobs *repository.JobRepository, movieRepo *repository.MovieRepository, q *queue.Client, mediaRoot, tmdbAPIKey, scannerAPIURL, serviceToken string) *JobService {
    return &JobService{
        jobs:          jobs,
        movieRepo:     movieRepo,
        queue:         q,
        mediaRoot:     mediaRoot,
        tmdbAPIKey:    tmdbAPIKey,
        scannerAPIURL: scannerAPIURL,
        serviceToken:  serviceToken,
    }
}

// Стало:
func NewJobService(jobs *repository.JobRepository, movieRepo *repository.MovieRepository, q *queue.Client, mediaRoot, tmdbAPIKey string) *JobService {
    return &JobService{
        jobs:       jobs,
        movieRepo:  movieRepo,
        queue:      q,
        mediaRoot:  mediaRoot,
        tmdbAPIKey: tmdbAPIKey,
    }
}
```

- [ ] **Step 3: Убрать scanner forwarding из `CreateRemoteDownloadJob`**

Найти и удалить блок:
```go
// If scanner API is configured ...
if s.scannerAPIURL != "" && !isPrivateURL(req.SourceURL) {
    ...
    return synthetic, tmdbID, nil
}
```

- [ ] **Step 4: Удалить функцию `isPrivateURL`**

Удалить целиком функцию `isPrivateURL(rawURL string) bool` (~25 строк).

- [ ] **Step 5: Удалить метод `forwardToScanner`**

Удалить целиком метод `(s *JobService) forwardToScanner(...)` (~20 строк).

- [ ] **Step 6: Убрать неиспользуемые imports `"bytes"` и `"net"`**

Проверить imports — если `bytes` и `net` нигде больше не используются, убрать.

- [ ] **Step 7: Собрать — должно быть 0 ошибок**

```bash
cd api && go build ./... 2>&1
```

Ожидаемо: пустой вывод (ошибок нет).

---

### Task 2: Обновить `api/cmd/api/main.go`

**Files:**
- Modify: `api/cmd/api/main.go`

- [ ] **Step 1: Убрать `scannerAPIURL`/`serviceToken` из вызова `NewJobService`**

```go
// Было:
jobSvc := service.NewJobService(jobRepo, movieRepo, redisClient, cfg.MediaRoot, cfg.TMDBAPIKey, cfg.ScannerAPIURL, cfg.IngestServiceToken)

// Стало:
jobSvc := service.NewJobService(jobRepo, movieRepo, redisClient, cfg.MediaRoot, cfg.TMDBAPIKey)
```

`cfg.ScannerAPIURL` и `cfg.IngestServiceToken` по-прежнему используются в строке `scannerH := handler.NewScannerHandler(...)` — не трогаем.

- [ ] **Step 2: Собрать снова**

```bash
cd api && go build ./... 2>&1
```

Ожидаемо: пустой вывод.

- [ ] **Step 3: Commit**

```bash
git add api/internal/service/job.go api/cmd/api/main.go
git commit -m "refactor(api): remove scanner forwarding from remote download flow

Downloads always go via remote_download_queue on converter worker.
Scanner integration remains for ingest flow only.
ScannerHandler proxy for /queue page visibility is unchanged."
```

---

## Chunk 2: Frontend — упростить upload page

### Task 3: Убрать состояние `downloading` из типов и upload page

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/app/upload/page.tsx`

- [ ] **Step 1: Убрать `'downloading'` из `DownloadItemState` в `types/index.ts`**

```ts
// Было:
export type DownloadItemState = 'idle' | 'submitting' | 'queued' | 'downloading' | 'error' | 'duplicate'

// Стало:
export type DownloadItemState = 'idle' | 'submitting' | 'queued' | 'error' | 'duplicate'
```

- [ ] **Step 2: Упростить обработку ответа в `upload/page.tsx`**

Найти строки (~196):
```tsx
state: resp.job_id ? 'queued' : 'downloading',
jobId: resp.job_id || undefined,
```

Заменить на:
```tsx
state: 'queued',
jobId: resp.job_id,
```

- [ ] **Step 3: Убрать отображение состояния `downloading` в таблице**

Найти в JSX блок который рендерит `dlItem?.state === 'downloading'` (~строка 596) и удалить этот блок целиком. Состояние больше не существует.

- [ ] **Step 4: Проверить TypeScript**

```bash
cd frontend && node_modules/.bin/next build 2>&1 | grep -E "error|warn" | head -20
```

Или если node_modules нет локально — проверить визуально что нет ссылок на `'downloading'` state:

```bash
grep -r "downloading" frontend/src/app/upload/page.tsx
```

Ожидаемо: строк с `state === 'downloading'` нет.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/index.ts frontend/src/app/upload/page.tsx
git commit -m "refactor(frontend): remove downloading state from upload page

Remote downloads always return job_id now. State is always 'queued'."
```

---

## Chunk 3: Документация и деплой

### Task 4: Обновить CHANGELOG и roadmap

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/roadmap/roadmap.md`

- [ ] **Step 1: Добавить запись в CHANGELOG**

В секцию `## [Unreleased]` под `### Changed`:
```markdown
### Changed
- `api/internal/service/job.go`: remote downloads always use converter worker's `remote_download_queue` — scanner forwarding removed; scanner is for ingest flow only
- `frontend/src/app/upload/page.tsx`: removed `downloading` transient state; `job_id` always present in remote download response
```

- [ ] **Step 2: Обновить roadmap — добавить задачу на scanner admin panel**

В секцию `## Среднесрочные задачи` добавить:
```markdown
### SCANNER-001: Отдельная админка для scanner сервера
**Что:** отдельный UI (или раздел в существующей админке) для управления scanner сервером:
  - список файлов в /incoming/ и их статусы
  - список загрузок scanner_downloads с возможностью retry
  - ручной запуск scan_loop
  - статистика: сколько файлов в очереди, сколько обработано
**Почему:** сейчас scanner виден только частично (загрузки в /queue), нужен полноценный интерфейс
**Затрагивает:** новые endpoints в scanner API, новые страницы во frontend (или отдельный сервис)
**Требует ADR:** обсудить — отдельный фронтенд vs раздел в существующем
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md docs/roadmap/roadmap.md
git commit -m "docs: update changelog and roadmap for scanner admin panel"
```

---

### Task 5: Деплой на converter сервер

- [ ] **Step 1: Push**

```bash
git push
```

- [ ] **Step 2: Собрать и задеплоить API и frontend**

```bash
ssh -i ~/.ssh/id_ed25519 root@178.104.53.215 \
  "cd /opt/converter && git pull && \
   docker compose build api frontend && \
   docker compose up -d api frontend"
```

- [ ] **Step 3: Проверить health**

```bash
ssh -i ~/.ssh/id_ed25519 root@178.104.53.215 \
  "docker compose ps api frontend"
```

Ожидаемо: оба в статусе `healthy` или `running`.

- [ ] **Step 4: Smoke test — добавить фильм через UI**

1. Открыть `/upload` в браузере
2. Вставить URL локального медиа-сервера (172.27.27.x)
3. Нажать "Скачать"
4. Убедиться что в `/queue` появляется задание со stage `download`
5. Убедиться что файл скачивается в `/media/downloads/` на converter сервере
