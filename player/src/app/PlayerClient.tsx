'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
import Script from 'next/script'

export interface MovieResponse {
  data: {
    movie: { id: number; imdb_id: string; tmdb_id: string }
    playback: { hls: string }
    assets: { poster: string }
  }
  meta: { version: string }
}

interface QualityLevel {
  label: string
  index: number
}

declare global {
  interface Window {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    fluidPlayer: (el: HTMLVideoElement, config: Record<string, unknown>) => any
  }
}

export default function PlayerClient({ initialData }: { initialData: MovieResponse }) {
  const movieData = initialData
  const [fluidReady, setFluidReady] = useState(false)
  const [qualities, setQualities] = useState<QualityLevel[]>([])
  const [selectedQuality, setSelectedQuality] = useState<string>('auto')
  const [showQualityMenu, setShowQualityMenu] = useState(false)

  const videoRef = useRef<HTMLVideoElement>(null)
  const quickbarRef = useRef<HTMLDivElement>(null)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const hlsRef = useRef<any>(null)
  const qualityModeRef = useRef<string>('auto')
  const adActiveRef = useRef(false)
  const hlsRestoreTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const streamUrlRef = useRef<string>(movieData.data.playback.hls)

  const isAppleMobile = useCallback(() => {
    if (typeof navigator === 'undefined') return false
    const ua = navigator.userAgent || ''
    return /iP(hone|od|ad)/.test(ua) || (ua.includes('Macintosh') && navigator.maxTouchPoints > 1)
  }, [])

  const setupHlsJsMode = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    async (video: HTMLVideoElement, streamUrl: string): Promise<any | null> => {
      const HlsMod = await import('hls.js')
      const Hls = HlsMod.default

      if (!Hls.isSupported()) {
        video.src = streamUrl
        return null
      }

      const hls = new Hls({
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
      })

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
      })

      hls.attachMedia(video)
      hls.on(Hls.Events.MEDIA_ATTACHED, () => {
        hls.loadSource(streamUrl)
      })

      return hls
    },
    [],
  )

  const reattachHlsAfterAd = useCallback(() => {
    if (isAppleMobile()) return
    const video = videoRef.current
    if (!video) return

    if (hlsRestoreTimerRef.current) clearTimeout(hlsRestoreTimerRef.current)

    hlsRestoreTimerRef.current = setTimeout(async () => {
      const resumeTime = video.currentTime || 0
      const shouldResume = !video.paused

      if (hlsRef.current) {
        try { hlsRef.current.destroy() } catch { /* ignore */ }
        hlsRef.current = null
      }

      const hls = await setupHlsJsMode(video, streamUrlRef.current)
      if (!hls) return
      hlsRef.current = hls

      const HlsMod = await import('hls.js')
      const Hls = HlsMod.default

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        if (resumeTime > 0) {
          try { video.currentTime = resumeTime } catch { /* ignore */ }
        }
        const q = qualityModeRef.current
        if (q === 'auto') hls.currentLevel = -1
        else {
          const idx = parseInt(q, 10)
          if (!isNaN(idx)) hls.currentLevel = idx
        }
        if (shouldResume) {
          setTimeout(() => {
            const p = video.play()
            if (p) p.catch(() => { /* autoplay blocked */ })
          }, 180)
        }
      })
    }, 120)
  }, [isAppleMobile, setupHlsJsMode])

  const mountSettingsInPlayer = useCallback((attempt: number) => {
    const quickbar = quickbarRef.current
    const video = videoRef.current
    if (!quickbar || !video) return

    const wrapper = video.parentElement?.classList?.contains('fluid_video_wrapper')
      ? video.parentElement
      : null

    if (!wrapper) {
      if (attempt >= 12) return
      setTimeout(() => mountSettingsInPlayer(attempt + 1), 120)
      return
    }

    quickbar.classList.add('in-player')
    if (quickbar.parentElement !== wrapper) wrapper.appendChild(quickbar)
  }, [])

  useEffect(() => {
    if (!movieData || !videoRef.current) return
    const video = videoRef.current
    const streamUrl = movieData.data.playback.hls
    streamUrlRef.current = streamUrl

    if (isAppleMobile()) {
      video.src = streamUrl
      return
    }

    setupHlsJsMode(video, streamUrl).then((hls) => {
      hlsRef.current = hls
    })

    return () => {
      if (hlsRef.current) {
        try { hlsRef.current.destroy() } catch { /* ignore */ }
        hlsRef.current = null
      }
    }
  }, [movieData, isAppleMobile, setupHlsJsMode])

  useEffect(() => {
    if (!fluidReady || !movieData || !videoRef.current) return
    if (typeof window.fluidPlayer !== 'function') return

    const vastTag = process.env.NEXT_PUBLIC_VAST_TAG || ''

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const fpConfig: Record<string, any> = {
      layoutControls: {
        fillToContainer: true,
        responsive: true,
        autoPlay: false,
        playbackRateControl: false,
        qualityControl: false,
      },
    }

    if (vastTag) {
      fpConfig.vastOptions = {
        adList: [
          { roll: 'preRoll', vastTag },
          { roll: 'midRoll', vastTag, timer: 600 },
        ],
        skipButtonCaption: 'Skip ad in [seconds]',
        skipButtonClickCaption: 'Skip ad',
        vastAdvanced: {
          vastVideoEndedCallback: () => {
            adActiveRef.current = false
            reattachHlsAfterAd()
          },
          vastVideoSkippedCallback: () => {
            adActiveRef.current = false
            reattachHlsAfterAd()
          },
          noVastVideoCallback: () => {
            adActiveRef.current = false
            reattachHlsAfterAd()
          },
        },
      }
    }

    const fp = window.fluidPlayer(videoRef.current, fpConfig)

    if (fp && typeof fp.on === 'function') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      fp.on('play', (_arg1: any, arg2: any) => {
        const info = typeof arg2 !== 'undefined' ? arg2 : _arg1
        const sourceType = info?.mediaSourceType ?? 'source'
        if (sourceType !== 'source' && !adActiveRef.current) {
          adActiveRef.current = true
        }
      })
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      fp.on('ended', (_arg1: any, arg2: any) => {
        const info = typeof arg2 !== 'undefined' ? arg2 : _arg1
        const sourceType = info?.mediaSourceType ?? 'source'
        if (sourceType !== 'source') {
          adActiveRef.current = false
        }
      })
    }

    mountSettingsInPlayer(0)
  }, [fluidReady, movieData, reattachHlsAfterAd, mountSettingsInPlayer])

  const applyQuality = useCallback(
    (value: string) => {
      qualityModeRef.current = value
      setSelectedQuality(value)
      setShowQualityMenu(false)

      if (!hlsRef.current) return
      if (value === 'auto') {
        hlsRef.current.currentLevel = -1
      } else {
        const idx = parseInt(value, 10)
        if (!isNaN(idx)) hlsRef.current.currentLevel = idx
      }
    },
    [],
  )

  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (quickbarRef.current && !quickbarRef.current.contains(e.target as Node)) {
        setShowQualityMenu(false)
      }
    }
    document.addEventListener('click', handleClick)
    return () => document.removeEventListener('click', handleClick)
  }, [])

  const poster = movieData.data.assets.poster

  return (
    <>
      <Script
        src="https://cdn.fluidplayer.com/3.57.0/fluidplayer.min.js"
        strategy="afterInteractive"
        onLoad={() => setFluidReady(true)}
      />

      <div className="unified-player-wrapper">
        <div className="player-aspect">
          <video
            ref={videoRef}
            id="universal-video-player"
            poster={poster}
            controls
            playsInline
            crossOrigin="anonymous"
            preload="metadata"
          >
            <source type="application/vnd.apple.mpegurl" />
          </video>
        </div>

        <div className="quality-quickbar" ref={quickbarRef}>
          <button
            type="button"
            className="settings-trigger"
            aria-label="Player settings"
            onClick={(e) => {
              e.stopPropagation()
              setShowQualityMenu((v) => !v)
            }}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M10 2h4l1 3a7 7 0 0 1 2 1l3-1 2 3-2 2a7 7 0 0 1 0 3l2 2-2 3-3-1a7 7 0 0 1-2 1l-1 3h-4l-1-3a7 7 0 0 1-2-1l-3 1-2-3 2-2a7 7 0 0 1 0-3l-2-2 2-3 3 1a7 7 0 0 1 2-1z" />
              <circle cx="12" cy="12" r="3.2" />
            </svg>
          </button>

          <div className={`settings-menu${showQualityMenu ? ' is-open' : ''}`}>
            <button
              type="button"
              className={`quality-item${selectedQuality === 'auto' ? ' is-active' : ''}`}
              onClick={(e) => { e.stopPropagation(); applyQuality('auto') }}
            >
              auto
            </button>
            {qualities.map((q) => (
              <button
                key={q.index}
                type="button"
                className={`quality-item${selectedQuality === String(q.index) ? ' is-active' : ''}`}
                onClick={(e) => { e.stopPropagation(); applyQuality(String(q.index)) }}
              >
                {q.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </>
  )
}
