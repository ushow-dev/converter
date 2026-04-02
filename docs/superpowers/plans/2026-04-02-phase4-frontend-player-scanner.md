# Phase 4: Frontend + Player + Scanner Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clean up frontend, player, and scanner — extract components, replace MovieResponse shim with generic PlaybackData, deduplicate scanner constants, extract DB queries.

**Architecture:** Component extraction in frontend/player (no behavior changes), interface cleanup in player (PlaybackData replaces MovieResponse), scanner data layer extraction.

**Tech Stack:** Next.js/React/TypeScript, Python 3.12/FastAPI

**Spec:** `docs/superpowers/specs/2026-04-02-full-refactoring-design.md` — Phase 4

**Independent of:** Phases 1-3 (can be done in parallel)

---

## Task 1: Player — Replace MovieResponse with PlaybackData

PlayerClient currently requires a `MovieResponse` interface which SeriesPlayer fakes with `episodeToMovieResponse()` creating objects with `movie: { id: 0, imdb_id: '' }`. Replace with a generic `PlaybackData` interface.

**Files:**
- Modify: `player/src/app/PlayerClient.tsx`
- Modify: `player/src/app/SeriesPlayer.tsx`
- Modify: `player/src/app/page.tsx`

- [ ] **Step 1: Define PlaybackData interface in PlayerClient.tsx**

Add new interface and update component props:

```typescript
export interface PlaybackData {
  hls: string
  poster?: string
  subtitles?: { language: string; url: string }[]
  tmdbId?: string // for P2P swarm ID derivation
}

// Keep MovieResponse for backward compat in page.tsx server fetch
export interface MovieResponse {
  data: {
    movie: { id: number; imdb_id: string; tmdb_id: string }
    playback: { hls: string }
    assets: { poster: string }
    subtitles?: { language: string; url: string }[]
  }
  meta: { version: string }
}

// Helper to convert legacy MovieResponse → PlaybackData
export function movieResponseToPlayback(resp: MovieResponse): PlaybackData {
  return {
    hls: resp.data.playback.hls,
    poster: resp.data.assets.poster,
    subtitles: resp.data.subtitles,
    tmdbId: resp.data.movie.tmdb_id,
  }
}
```

- [ ] **Step 2: Update PlayerClient to accept PlaybackData**

Change component signature:
```typescript
export default function PlayerClient({
  playback,
  onEnded,
  autoPlay = false,
}: {
  playback: PlaybackData
  onEnded?: () => void
  autoPlay?: boolean
})
```

Update all internal references:
- `movieData.data.playback.hls` → `playback.hls`
- `movieData.data.assets.poster` → `playback.poster`
- `movieData.data.subtitles` → `playback.subtitles`
- `movieData.data.movie.tmdb_id` in deriveSwarmId → `playback.tmdbId`

- [ ] **Step 3: Update page.tsx**

```typescript
export default async function Page({ searchParams }: PageProps) {
  // ... fetch logic stays the same ...

  // Movie mode:
  return <PlayerClient playback={movieResponseToPlayback(data)} />

  // Series mode:
  return <SeriesPlayer initialData={data} hideNavigation={hideNav} />
}
```

- [ ] **Step 4: Update SeriesPlayer.tsx**

Remove `episodeToMovieResponse()` shim. Build PlaybackData directly:

```typescript
function episodeToPlayback(ep: EpisodeAPI, tmdbId?: string): PlaybackData {
  return {
    hls: ep.playback?.hls ?? '',
    poster: ep.assets?.thumbnail ?? '',
    subtitles: ep.subtitles,
    tmdbId,
  }
}
```

Update all `<PlayerClient initialData={...} />` to `<PlayerClient playback={...} />`.

- [ ] **Step 5: Verify**

Run: `cd player && npx next build`

- [ ] **Step 6: Commit**

```bash
git add player/src/app/
git commit -m "refactor(player): replace MovieResponse with generic PlaybackData interface"
```

---

## Task 2: Player — Extract constants and HLS config

**Files:**
- Create: `player/src/app/constants.ts`
- Modify: `player/src/app/PlayerClient.tsx`

- [ ] **Step 1: Create constants.ts**

Extract from PlayerClient.tsx:
```typescript
// player/src/app/constants.ts

export const HLS_CONFIG = {
  startLevel: 0,
  capLevelToPlayerSize: true,
  testBandwidth: true,
  lowLatencyMode: false,
  abrEwmaDefaultEstimate: 300000,
  abrBandWidthFactor: 0.8,
  abrBandWidthUpFactor: 0.6,
  abrEwmaFastVoD: 3.0,
  abrEwmaSlowVoD: 9.0,
  maxBufferLength: 6,
  maxMaxBufferLength: 10,
  maxBufferSize: 12000000,
  maxBufferHole: 0.5,
  backBufferLength: 20,
}

export const SUBTITLE_LABELS: Record<string, string> = {
  ru: 'Русский',
  en: 'English',
  uk: 'Українська',
  es: 'Espanol',
  fr: 'Francais',
  de: 'Deutsch',
  it: 'Italiano',
  pt: 'Portugues',
  pl: 'Polski',
  tr: 'Turkce',
  ja: 'Japanese',
  ko: 'Korean',
  zh: 'Chinese',
}

export function normalizeLanguageCode(lang: string): string {
  const trimmed = lang.trim().toLowerCase()
  if (!trimmed) return 'und'
  return trimmed.split(/[-_]/)[0] || 'und'
}

export function subtitleLabel(lang: string): string {
  const normalized = normalizeLanguageCode(lang)
  return SUBTITLE_LABELS[normalized] ?? normalized.toUpperCase()
}
```

- [ ] **Step 2: Update PlayerClient.tsx imports**

Replace inline definitions with imports:
```typescript
import { HLS_CONFIG, SUBTITLE_LABELS, normalizeLanguageCode, subtitleLabel } from './constants'
```

Remove the corresponding code blocks from PlayerClient.tsx.

- [ ] **Step 3: Verify and commit**

```bash
cd player && npx next build
git add player/src/app/constants.ts player/src/app/PlayerClient.tsx
git commit -m "refactor(player): extract HLS config and subtitle labels to constants.ts"
```

---

## Task 3: Frontend — Extract movie page components

**Files:**
- Create: `frontend/src/components/MovieTable.tsx`
- Create: `frontend/src/components/PlayerModal.tsx`
- Modify: `frontend/src/app/movies/page.tsx`

- [ ] **Step 1: Extract PlayerModal**

Move `PlayerModal` component from `movies/page.tsx` to `frontend/src/components/PlayerModal.tsx`. It's already self-contained with props `{ movie, playerUrl, onClose }`. Generalize it to accept any content:

```typescript
interface PlayerModalProps {
  src: string       // iframe URL
  title?: string    // display title
  onClose: () => void
}
```

- [ ] **Step 2: Extract MovieTable components**

Move `MovieRow`, `Thumbnail`, `EditableID`, `SubtitleCell` to `frontend/src/components/MovieTable.tsx`. These are only used by movies page but extracting them reduces the page from 473 to ~150 lines.

- [ ] **Step 3: Update movies/page.tsx**

Import extracted components. Page becomes orchestration only: state, data fetching, render.

- [ ] **Step 4: Verify and commit**

```bash
cd frontend && npm run build
git add frontend/src/components/ frontend/src/app/movies/page.tsx
git commit -m "refactor(frontend): extract MovieTable and PlayerModal components"
```

---

## Task 4: Frontend — Extract series detail components

**Files:**
- Create: `frontend/src/components/SeasonAccordion.tsx`
- Modify: `frontend/src/app/series/[id]/page.tsx`

- [ ] **Step 1: Extract SeasonAccordion**

Move `SeasonSection` and `EpisodeRow` components from `series/[id]/page.tsx` to `frontend/src/components/SeasonAccordion.tsx`. They're already self-contained.

- [ ] **Step 2: Update series detail page**

Import extracted components. Page becomes orchestration: state, fetch, render.

- [ ] **Step 3: Verify and commit**

```bash
cd frontend && npm run build
git add frontend/src/components/SeasonAccordion.tsx 'frontend/src/app/series/[id]/page.tsx'
git commit -m "refactor(frontend): extract SeasonAccordion component"
```

---

## Task 5: Frontend — Pagination hook

**Files:**
- Create: `frontend/src/hooks/usePaginatedList.ts`
- Modify: `frontend/src/app/series/page.tsx`

- [ ] **Step 1: Create usePaginatedList hook**

```typescript
// frontend/src/hooks/usePaginatedList.ts
import { useState, useEffect, useCallback } from 'react'
import { fetcher } from '@/lib/api'

interface PaginatedResult<T> {
  items: T[]
  next_cursor: string | null
}

export function usePaginatedList<T>(
  buildUrl: (limit: number, cursor?: string) => string,
  limit = 50,
) {
  const [items, setItems] = useState<T[]>([])
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadPage = useCallback(async (cursor?: string) => {
    try {
      const data = await fetcher<PaginatedResult<T>>(buildUrl(limit, cursor))
      if (cursor) {
        setItems(prev => [...prev, ...data.items])
      } else {
        setItems(data.items)
      }
      setNextCursor(data.next_cursor)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Ошибка загрузки')
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [buildUrl, limit])

  useEffect(() => { loadPage() }, [loadPage])

  const loadMore = useCallback(() => {
    if (!nextCursor) return
    setLoadingMore(true)
    loadPage(nextCursor)
  }, [nextCursor, loadPage])

  return { items, loading, loadingMore, error, hasMore: !!nextCursor, loadMore }
}
```

- [ ] **Step 2: Use in series page**

Replace manual pagination state in `series/page.tsx` with:
```typescript
const { items, loading, loadingMore, error, hasMore, loadMore } = usePaginatedList<Series>(seriesUrl)
```

Remove `loadPage`, `handleLoadMore`, manual state declarations.

- [ ] **Step 3: Verify and commit**

```bash
cd frontend && npm run build
git add frontend/src/hooks/ frontend/src/app/series/page.tsx
git commit -m "refactor(frontend): extract usePaginatedList hook, use in series page"
```

---

## Task 6: Scanner — Constants and data layer

**Files:**
- Create: `scanner/scanner/constants.py`
- Create: `scanner/scanner/data/queries.py`
- Modify: `scanner/scanner/loops/scan_loop.py`
- Modify: `scanner/scanner/services/metadata.py`
- Modify: `scanner/scanner/api/server.py`

- [ ] **Step 1: Create constants.py**

```python
# scanner/scanner/constants.py

VIDEO_EXTENSIONS = {".mkv", ".mp4", ".avi", ".mov", ".ts", ".m2ts", ".wmv"}

MIN_FILE_SIZE_BYTES = 1024 * 1024  # 1 MB
```

- [ ] **Step 2: Update imports**

In `scan_loop.py`: replace `VIDEO_EXTENSIONS` and `MIN_FILE_SIZE_BYTES` definitions with:
```python
from scanner.constants import VIDEO_EXTENSIONS, MIN_FILE_SIZE_BYTES
```

In `metadata.py`: replace `VIDEO_EXTENSIONS` definition with:
```python
from scanner.constants import VIDEO_EXTENSIONS
```

In `services/series_detect.py`: same import.

- [ ] **Step 3: Extract common DB queries to data/queries.py**

Create `scanner/scanner/data/__init__.py` (empty) and `scanner/scanner/data/queries.py`:

Extract from `api/server.py` the standalone query functions:
- `_claim_items()` → `data.queries.claim_items()`
- `_get_item_info()` → `data.queries.get_item_info()`
- `_update_status()` → `data.queries.update_status()`
- `_update_status_with_error()` → `data.queries.update_status_with_error()`

Server.py becomes a thin routing layer that calls data functions.

- [ ] **Step 4: Verify**

```bash
cd scanner && python3 -c "import scanner.constants; import scanner.data.queries; print('OK')"
```

- [ ] **Step 5: Commit**

```bash
git add scanner/scanner/constants.py scanner/scanner/data/ scanner/scanner/loops/scan_loop.py scanner/scanner/services/metadata.py scanner/scanner/services/series_detect.py scanner/scanner/api/server.py
git commit -m "refactor(scanner): extract constants and data query layer"
```

---

## Dependency Graph

```
Task 1 (PlaybackData) — independent, highest value
Task 2 (constants.ts) — independent
Task 3 (movie components) — independent
Task 4 (series components) — independent
Task 5 (pagination hook) — independent
Task 6 (scanner cleanup) — independent
```

All tasks are independent. Recommended order: 1 → 2 → 3 → 4 → 5 → 6 (player first since it's most impactful).
