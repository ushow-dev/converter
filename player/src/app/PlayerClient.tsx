'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
import Script from 'next/script'
import { startP2PMetrics, stopP2PMetrics } from './p2pMetrics'

export interface MovieResponse {
  data: {
    movie: { id: number; imdb_id: string; tmdb_id: string }
    playback: { hls: string }
    assets: { poster: string }
    subtitles?: { language: string; url: string }[]
  }
  meta: { version: string }
}

interface QualityLevel {
  label: string
  index: number
}

const SUBTITLE_LABELS: Record<string, string> = {
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

function normalizeLanguageCode(lang: string): string {
  const trimmed = lang.trim().toLowerCase()
  if (!trimmed) return 'und'
  return trimmed.split(/[-_]/)[0] || 'und'
}

function subtitleLabel(lang: string): string {
  const normalized = normalizeLanguageCode(lang)
  return SUBTITLE_LABELS[normalized] ?? normalized.toUpperCase()
}

declare global {
  interface Window {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    fluidPlayer: (el: HTMLVideoElement, config: Record<string, unknown>) => any
  }
}

const P2P_ENABLED = process.env.NEXT_PUBLIC_P2P_ENABLED === 'true'
const P2P_TRACKER_URL = process.env.NEXT_PUBLIC_P2P_TRACKER_URL || 'wss://t.pimor.online'

const HLS_CONFIG = {
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

// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function createHlsInstance(): Promise<{ Hls: any; isP2P: boolean }> {
  const HlsMod = await import('hls.js')
  const Hls = HlsMod.default

  if (!P2P_ENABLED) {
    return { Hls, isP2P: false }
  }

  try {
    const { HlsJsP2PEngine } = await import('p2p-media-loader-hlsjs')
    const HlsWithP2P = HlsJsP2PEngine.injectMixin(Hls)
    return { Hls: HlsWithP2P, isP2P: true }
  } catch {
    return { Hls, isP2P: false }
  }
}

function getP2PConfig() {
  return {
    core: {
      announceTrackers: [P2P_TRACKER_URL],
      rtcConfig: {
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
      },
    },
  }
}

export default function PlayerClient({ initialData }: { initialData: MovieResponse }) {
  const movieData = initialData
  const [fluidReady, setFluidReady] = useState(false)
  const [streamMode, setStreamMode] = useState<'pending' | 'hlsjs' | 'native'>('pending')
  const [isMobileRuntime, setIsMobileRuntime] = useState(false)
  const [qualities, setQualities] = useState<QualityLevel[]>([])
  const [selectedQuality, setSelectedQuality] = useState<string>('auto')
  const [showQualityMenu, setShowQualityMenu] = useState(false)

  const videoRef = useRef<HTMLVideoElement>(null)
  const quickbarRef = useRef<HTMLDivElement>(null)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const hlsRef = useRef<any>(null)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const fluidInstanceRef = useRef<any>(null)
  const qualityModeRef = useRef<string>('auto')
  const adActiveRef = useRef(false)
  const hlsRestoreTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const seekIdleTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const seekLoadStoppedRef = useRef(false)
  const seekWasPlayingRef = useRef(false)
  const streamUrlRef = useRef<string>(movieData.data.playback.hls)
  const subtitleTracks = movieData.data.subtitles ?? []

  const setupHlsJsMode = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    async (video: HTMLVideoElement, streamUrl: string): Promise<any | null> => {
      const { Hls, isP2P } = await createHlsInstance()

      if (!Hls.isSupported()) {
        video.src = streamUrl
        setStreamMode('native')
        return null
      }
      setStreamMode('hlsjs')

      const hlsConfig = isP2P
        ? { ...HLS_CONFIG, p2p: getP2PConfig() }
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
      })

      hls.attachMedia(video)
      hls.on(Hls.Events.MEDIA_ATTACHED, () => {
        hls.loadSource(streamUrl)
      })

      return hls
    },
    [],
  )

  const reattachHlsAfterAd = useCallback(async () => {
    const video = videoRef.current
    if (!video) return

    if (hlsRestoreTimerRef.current) {
      clearTimeout(hlsRestoreTimerRef.current)
      hlsRestoreTimerRef.current = null
    }

    if (hlsRef.current) {
      stopP2PMetrics()
      try { hlsRef.current.destroy() } catch { /* ignore */ }
      hlsRef.current = null
    }

    const { Hls, isP2P } = await createHlsInstance()
    const resumeTime = video.currentTime || 0
    const shouldResume = !video.paused

    if (!Hls.isSupported()) {
      video.src = streamUrlRef.current
      setStreamMode('native')
      return
    }
    setStreamMode('hlsjs')

    const hlsConfig = isP2P
      ? { ...HLS_CONFIG, p2p: getP2PConfig() }
      : { ...HLS_CONFIG }

    const hls = new Hls(hlsConfig)
    hlsRef.current = hls

    if (isP2P && hls.p2pEngine) {
      startP2PMetrics(hls.p2pEngine, streamUrlRef.current)
    }

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

    hls.attachMedia(video)
    hls.on(Hls.Events.MEDIA_ATTACHED, () => {
      hls.loadSource(streamUrlRef.current)
    })
  }, [])

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
    if (typeof window === 'undefined') return
    const pointerQuery = window.matchMedia('(any-pointer: coarse)')
    const widthQuery = window.matchMedia('(max-width: 900px)')
    const apply = () => {
      const coarse = pointerQuery.matches
      const smallViewport = widthQuery.matches
      const touchPoints = typeof navigator !== 'undefined' ? navigator.maxTouchPoints > 0 : false
      setIsMobileRuntime(smallViewport || coarse || touchPoints)
    }
    apply()
    if (typeof pointerQuery.addEventListener === 'function') {
      pointerQuery.addEventListener('change', apply)
      widthQuery.addEventListener('change', apply)
      return () => {
        pointerQuery.removeEventListener('change', apply)
        widthQuery.removeEventListener('change', apply)
      }
    }
    pointerQuery.addListener(apply)
    widthQuery.addListener(apply)
    return () => {
      pointerQuery.removeListener(apply)
      widthQuery.removeListener(apply)
    }
  }, [])

  useEffect(() => {
    if (!movieData || !videoRef.current) return
    const video = videoRef.current
    const streamUrl = movieData.data.playback.hls
    streamUrlRef.current = streamUrl
    setStreamMode('pending')

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
    }
  }, [movieData, setupHlsJsMode])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    if (!isMobileRuntime || streamMode !== 'hlsjs') return

    const onSeeking = () => {
      if (!hlsRef.current) return
      if (!seekLoadStoppedRef.current) {
        seekLoadStoppedRef.current = true
        seekWasPlayingRef.current = !video.paused
        if (seekWasPlayingRef.current) {
          try { video.pause() } catch { /* ignore */ }
        }
        try {
          hlsRef.current.stopLoad()
        } catch {
          // ignore
        }
      }
      if (seekIdleTimerRef.current) {
        clearTimeout(seekIdleTimerRef.current)
        seekIdleTimerRef.current = null
      }
      seekIdleTimerRef.current = setTimeout(() => {
        seekIdleTimerRef.current = null
        if (!hlsRef.current) return
        const targetTime = Number.isFinite(video.currentTime) ? video.currentTime : 0
        try {
          hlsRef.current.startLoad(targetTime)
        } catch {
          // ignore
        }
        const shouldResume = seekWasPlayingRef.current
        seekLoadStoppedRef.current = false
        seekWasPlayingRef.current = false
        if (shouldResume) {
          const p = video.play()
          if (p) p.catch(() => { /* autoplay blocked */ })
        }
      }, 220)
    }

    video.addEventListener('seeking', onSeeking)
    return () => {
      video.removeEventListener('seeking', onSeeking)
      if (seekIdleTimerRef.current) {
        clearTimeout(seekIdleTimerRef.current)
        seekIdleTimerRef.current = null
      }
      seekLoadStoppedRef.current = false
      seekWasPlayingRef.current = false
    }
  }, [isMobileRuntime, streamMode])

  useEffect(() => {
    if (!fluidReady || !movieData || !videoRef.current || streamMode === 'pending') return
    if (typeof window.fluidPlayer !== 'function') return
    if (fluidInstanceRef.current && typeof fluidInstanceRef.current.destroy === 'function') {
      try { fluidInstanceRef.current.destroy() } catch { /* ignore */ }
      fluidInstanceRef.current = null
    }
    if (streamMode === 'native') {
      videoRef.current.src = movieData.data.playback.hls
    }

    const vastTag = process.env.NEXT_PUBLIC_VAST_TAG || ''

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const fpConfig: Record<string, any> = {
      layoutControls: {
        fillToContainer: true,
        responsive: true,
        autoPlay: false,
        subtitlesEnabled: subtitleTracks.length > 0,
        playbackRateControl: false,
        qualityControl: false,
        contextMenu: {
          controls: true,
          links: [],
        },
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
    fluidInstanceRef.current = fp

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

    return () => {
      if (fluidInstanceRef.current && typeof fluidInstanceRef.current.destroy === 'function') {
        try { fluidInstanceRef.current.destroy() } catch { /* ignore */ }
        fluidInstanceRef.current = null
      }
    }
  }, [fluidReady, movieData, reattachHlsAfterAd, mountSettingsInPlayer, streamMode])

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
            {subtitleTracks.map((subtitle) => {
              const langCode = normalizeLanguageCode(subtitle.language)
              return (
                <track
                  key={`${langCode}-${subtitle.url}`}
                  kind="metadata"
                  src={subtitle.url}
                  srcLang={langCode}
                  label={subtitleLabel(subtitle.language)}
                  default={langCode === 'ru'}
                />
              )
            })}
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
            <div className="settings-section-label">Качество</div>
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
