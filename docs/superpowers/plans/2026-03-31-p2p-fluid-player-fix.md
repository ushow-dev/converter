# Fix P2P + Fluid Player Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix P2P streaming by making hls.js own the video source and Fluid Player act as UI overlay only, with detach/reattach for ads instead of destroy/recreate.

**Architecture:** Single video element. hls.js+P2P initializes first and owns the source via MSE. Fluid Player initializes second as UI overlay. VAST ad callbacks use `hls.detachMedia()`/`hls.attachMedia()` to pause/resume HLS without destroying the P2P engine.

**Tech Stack:** React 18, Next.js 14, hls.js 1.6.15, p2p-media-loader 2.2.2, Fluid Player 3.57.0

---

## File Structure

| Action | File | Responsibility |
|---|---|---|
| Modify | `player/src/app/PlayerClient.tsx` | Rework init order + ad handling |

Single file change. All logic lives in `PlayerClient.tsx`.

---

### Task 1: Replace racing useEffects with sequential initialization

**Files:**
- Modify: `player/src/app/PlayerClient.tsx`

The current code has two independent useEffects that race:
- useEffect at line 303: creates hls.js + loads source
- useEffect at line 378: creates Fluid Player (depends on `streamMode` and `fluidReady`)

The problem: Fluid Player init at line 432 calls `window.fluidPlayer(videoRef.current, fpConfig)` which takes over the video element. When VAST fires (even on error), it calls `reattachHlsAfterAd` which destroys and recreates hls.js+P2P.

The fix: one useEffect that runs sequential initialization: hls.js first → wait for MANIFEST_PARSED → then Fluid Player.

- [ ] **Step 1: Add `hlsReady` state**

In the state declarations (after line 129), add:

```typescript
const [hlsReady, setHlsReady] = useState(false)
```

- [ ] **Step 2: Update `setupHlsJsMode` to signal readiness**

Replace the `setupHlsJsMode` callback (lines 146-194) with:

```typescript
  const setupHlsJsMode = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    async (video: HTMLVideoElement, streamUrl: string): Promise<any | null> => {
      const { Hls, isP2P } = await createHlsInstance()

      if (!Hls.isSupported()) {
        video.src = streamUrl
        setStreamMode('native')
        setHlsReady(true)
        return null
      }
      setStreamMode('hlsjs')

      const hlsConfig = isP2P
        ? { ...HLS_CONFIG, p2p: getP2PConfig(streamUrl) }
        : { ...HLS_CONFIG }

      const hls = new Hls(hlsConfig)

      if (isP2P && hls.p2pEngine) {
        startP2PMetrics(hls.p2pEngine, streamUrl)
      }

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        const levels: QualityLevel[] = (hls.levels || []).map(
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (lvl: any, idx: number) => ({
            label: lvl.height ? `${lvl.height}p` : lvl.bitrate ? `${Math.round(lvl.bitrate / 1000)}kbps` : `level ${idx}`,
            index: idx,
          }),
        )
        setQualities(levels)

        const q = qualityModeRef.current
        if (q === 'auto') hls.currentLevel = -1
        else {
          const idx = parseInt(q, 10)
          if (!isNaN(idx)) hls.currentLevel = idx
        }

        setHlsReady(true)
      })

      hls.attachMedia(video)
      hls.on(Hls.Events.MEDIA_ATTACHED, () => {
        hls.loadSource(streamUrl)
      })

      return hls
    },
    [],
  )
```

Only change: added `setHlsReady(true)` after MANIFEST_PARSED and in the native fallback.

- [ ] **Step 3: Update HLS init useEffect cleanup to reset hlsReady**

Replace the hls init useEffect (lines 303-322) with:

```typescript
  useEffect(() => {
    if (!movieData || !videoRef.current) return
    const video = videoRef.current
    const streamUrl = movieData.data.playback.hls
    streamUrlRef.current = streamUrl
    setStreamMode('pending')
    setHlsReady(false)

    setupHlsJsMode(video, streamUrl).then((hls) => {
      hlsRef.current = hls
    })

    return () => {
      stopP2PMetrics()
      if (hlsRef.current) {
        try { hlsRef.current.destroy() } catch { /* ignore */ }
        hlsRef.current = null
      }
      setStreamMode('pending')
      setHlsReady(false)
    }
  }, [movieData, setupHlsJsMode])
```

Only change: added `setHlsReady(false)` in both init and cleanup.

- [ ] **Step 4: Change Fluid Player useEffect to depend on `hlsReady` instead of `streamMode`**

Replace the Fluid Player useEffect guard (line 379):

```typescript
// OLD:
if (!fluidReady || !movieData || !videoRef.current || streamMode === 'pending') return

// NEW:
if (!fluidReady || !hlsReady || !movieData || !videoRef.current) return
```

And update the dependency array (line 462):

```typescript
// OLD:
}, [fluidReady, movieData, reattachHlsAfterAd, mountSettingsInPlayer, streamMode])

// NEW:
}, [fluidReady, hlsReady, movieData, mountSettingsInPlayer])
```

Note: `reattachHlsAfterAd` and `streamMode` removed from deps (we'll replace the callback in Task 2).

- [ ] **Step 5: Verify build**

```bash
cd player && npm run build
```

Expected: build succeeds (TypeScript will warn about unused `reattachHlsAfterAd` but won't fail).

- [ ] **Step 6: Commit**

```bash
git add player/src/app/PlayerClient.tsx
git commit -m "fix(player): sequence hls.js init before Fluid Player via hlsReady state"
```

---

### Task 2: Replace destroy/recreate with detach/reattach for ads

**Files:**
- Modify: `player/src/app/PlayerClient.tsx`

Replace the `reattachHlsAfterAd` callback (which destroys hls.js+P2P and recreates them) with lightweight `onAdStart`/`onAdEnd` handlers that detach/reattach the existing hls.js instance.

- [ ] **Step 1: Remove `reattachHlsAfterAd` and add `onAdStart`/`onAdEnd`**

Delete the entire `reattachHlsAfterAd` callback (lines 196-255) and the `hlsRestoreTimerRef` ref (line 139). Replace with:

```typescript
  const onAdStart = useCallback(() => {
    adActiveRef.current = true
    if (hlsRef.current) {
      try {
        hlsRef.current.stopLoad()
        hlsRef.current.detachMedia()
      } catch { /* ignore */ }
    }
  }, [])

  const onAdEnd = useCallback(() => {
    adActiveRef.current = false
    const video = videoRef.current
    const hls = hlsRef.current
    if (!video || !hls) return

    try {
      hls.attachMedia(video)
      hls.on(hls.constructor.Events.MEDIA_ATTACHED, () => {
        const resumeTime = video.currentTime || 0
        hls.startLoad(resumeTime)
      })
    } catch { /* ignore */ }
  }, [])
```

Key difference from old code:
- `onAdStart`: only detaches media, P2P engine stays alive
- `onAdEnd`: reattaches same hls instance, no destroy/recreate
- `startP2PMetrics` is NOT called again — the original subscription is still active

- [ ] **Step 2: Update VAST callbacks in Fluid Player config**

In the Fluid Player useEffect, replace the vastAdvanced callbacks (lines 415-428):

```typescript
        vastAdvanced: {
          vastVideoEndedCallback: () => {
            onAdEnd()
          },
          vastVideoSkippedCallback: () => {
            onAdEnd()
          },
          noVastVideoCallback: () => {
            onAdEnd()
          },
        },
```

- [ ] **Step 3: Update Fluid Player `play` event to call `onAdStart`**

Replace the `fp.on('play', ...)` handler (lines 437-443):

```typescript
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      fp.on('play', (_arg1: any, arg2: any) => {
        const info = typeof arg2 !== 'undefined' ? arg2 : _arg1
        const sourceType = info?.mediaSourceType ?? 'source'
        if (sourceType !== 'source' && !adActiveRef.current) {
          onAdStart()
        }
      })
```

- [ ] **Step 4: Update Fluid Player useEffect dependency array**

The dependency array should include `onAdStart` and `onAdEnd`:

```typescript
  }, [fluidReady, hlsReady, movieData, mountSettingsInPlayer, onAdStart, onAdEnd])
```

- [ ] **Step 5: Verify build**

```bash
cd player && npm run build
```

Expected: build succeeds, no unused variable warnings.

- [ ] **Step 6: Commit**

```bash
git add player/src/app/PlayerClient.tsx
git commit -m "fix(player): replace hls.js destroy/recreate with detach/reattach for ads"
```

---

### Task 3: Update CHANGELOG and deploy

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `player/src/app/PlayerClient.tsx` (copy to server)

- [ ] **Step 1: Update CHANGELOG**

Add under `## [Unreleased]` → `### Fixed`:

```markdown
- `player/src/app/PlayerClient.tsx`: fix P2P dying after Fluid Player init — hls.js now initializes before Fluid Player, VAST ads use detach/reattach instead of destroy/recreate so P2P engine stays alive
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(player): add P2P + Fluid Player fix to changelog"
```

- [ ] **Step 3: Push**

```bash
git push origin main
```

- [ ] **Step 4: Deploy to storage server**

```bash
scp -i ~/.ssh/id_rsa_personal player/src/app/PlayerClient.tsx root@45.134.174.84:/opt/player/src/app/PlayerClient.tsx

ssh -i ~/.ssh/id_rsa_personal root@45.134.174.84 'cd /opt/player && docker build --build-arg NEXT_PUBLIC_P2P_ENABLED=true --build-arg NEXT_PUBLIC_P2P_TRACKER_URL=wss://t.pimor.online --build-arg NEXT_PUBLIC_API_URL=https://api.pimor.online --build-arg "NEXT_PUBLIC_VAST_TAG=https://proton9.engine.adglare.net/?320596439" -t ptrack-player . && docker stop ptrack-player && docker rm ptrack-player && docker run -d --name ptrack-player --restart unless-stopped -p 127.0.0.1:3100:3000 --env-file .env ptrack-player'
```

Expected: `player OK`

- [ ] **Step 5: Verify P2P is alive**

Open player in browser, check DevTools Console:

```js
document.getElementById('universal-video-player')?.__hls?.p2pEngine
```

Expected: returns an object (not undefined).

Also check metrics:

```bash
curl -s https://api.pimor.online/metrics | grep p2p
```

Expected: `converter_p2p_peers` > 0 after opening in multiple browsers.
