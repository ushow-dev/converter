# Fix P2P + Fluid Player Integration

## Problem

Fluid Player and hls.js fight over the video element. Fluid Player's VAST handling (even on ad error) triggers `reattachHlsAfterAd` which destroys and recreates hls.js + P2P engine. This kills WebRTC peer connections and causes P2P to drop to zero.

Evidence from debugging:
- `video.src` = undefined after Fluid Player init
- No `.ts`/`.m3u8` in performance entries
- `__hls` and `p2pEngine` = undefined on video element
- P2P metrics show traffic briefly, then drop to 0

## Solution

One video element. hls.js+P2P owns the source. Fluid Player is a UI overlay that does not touch `video.src`. VAST ads use detach/reattach instead of destroy/recreate.

## Initialization Order

```
1. createHlsInstance() → Hls with P2P mixin
2. hls.attachMedia(video) + hls.loadSource(url)
3. startP2PMetrics(hls.p2pEngine)
4. Wait for MANIFEST_PARSED
5. THEN → fluidPlayer(video, config)
   - Fluid Player sees video already playing via MSE
   - Does not set video.src itself
   - Acts only as UI chrome + VAST overlay
```

Current code has two independent useEffects that race — hls.js in one, Fluid Player in another. Fix: single sequential flow controlled by state transitions: `pending → hlsReady → playerReady`.

## VAST Ad Handling

Replace `reattachHlsAfterAd` (destroy + recreate) with detach/reattach:

**Ad start:**
```
hls.stopLoad()
hls.detachMedia()
// Fluid Player takes over video for ad playback
// P2P engine stays alive, WebRTC connections preserved
```

**Ad end / skip / error:**
```
hls.attachMedia(video)
hls.startLoad(resumeTime)
// P2P engine resumes with same peers
// No need to recreate anything
```

P2P engine is created once and never destroyed during playback. `startP2PMetrics` called once at init.

## Files to Modify

- `player/src/app/PlayerClient.tsx` — rework initialization order, replace `reattachHlsAfterAd` with detach/reattach pattern
- `player/src/app/p2pMetrics.ts` — no changes needed

## What Does NOT Change

- Fluid Player stays as the UI player
- VAST configuration (pre-roll, mid-roll) stays
- Quality selector, subtitles, mobile seek optimization — unchanged
- Dockerfile, P2P tracker config, nginx — unchanged

## Constraints

- Fluid Player 3.57.0 (loaded via CDN script)
- p2p-media-loader v2.2.2
- hls.js v1.6.15
- Next.js 14.2.15 with `'use client'` component
