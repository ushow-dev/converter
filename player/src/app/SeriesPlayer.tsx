'use client'

import { useState, useMemo, useCallback, useEffect, useRef } from 'react'
import PlayerClient from './PlayerClient'
import type { PlaybackData } from './types'
import { SUBTITLE_LABELS } from './constants'

// ── API response shapes ──────────────────────────────────────────────────────

interface EpisodeAPI {
  episode_number: number
  title?: string
  playback?: { hls: string }
  assets?: { thumbnail?: string }
  subtitles?: { language: string; url: string }[]
  audio_tracks?: { index: number; language?: string; label?: string; is_default: boolean }[]
}

interface SeasonAPI {
  season_number: number
  poster_url?: string
  episodes: EpisodeAPI[]
}

/** Full series navigation response — /api/player/series */
export interface SeriesData {
  data: {
    series: { id?: number; tmdb_id?: string; title?: string; year?: number; poster_url?: string }
    seasons: SeasonAPI[]
  }
  meta?: { version: string }
}

/** Single episode response — /api/player/episode */
export interface EpisodeData {
  data: {
    episode: EpisodeAPI
    series?: { id?: number; tmdb_id?: string; title?: string }
  }
  meta?: { version: string }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnySeriesData = SeriesData | EpisodeData | any

// ── Helpers ──────────────────────────────────────────────────────────────────

function isEpisodeData(d: AnySeriesData): d is EpisodeData {
  return d?.data && 'episode' in d.data && typeof d.data.episode === 'object'
}

function isSeriesData(d: AnySeriesData): d is SeriesData {
  return d?.data && 'seasons' in d.data && Array.isArray(d.data.seasons)
}

function episodeToPlayback(ep: EpisodeAPI, tmdbId?: string): PlaybackData {
  return {
    hls: ep.playback?.hls ?? '',
    poster: ep.assets?.thumbnail ?? '',
    subtitles: ep.subtitles,
    tmdbId,
  }
}

// ── Component ────────────────────────────────────────────────────────────────

interface SeriesPlayerProps {
  initialData: AnySeriesData
  hideNavigation?: boolean
}

export default function SeriesPlayer({ initialData, hideNavigation = false }: SeriesPlayerProps) {
  // Single-episode embed mode
  if (isEpisodeData(initialData)) {
    const ep = initialData.data.episode
    if (!ep.playback?.hls) return <div className="player-status">Episode not ready</div>
    const tmdbId = initialData.data.series?.tmdb_id ?? ''
    return <PlayerClient playback={episodeToPlayback(ep, tmdbId)} />
  }

  if (isSeriesData(initialData)) {
    return <SeriesNavigator data={initialData} />
  }

  return <div className="player-status">Invalid series data</div>
}

// ── Flat episode for navigation ──────────────────────────────────────────────

interface FlatEpisode {
  seasonNumber: number
  episodeNumber: number
  title?: string
  api: EpisodeAPI
}

// ── Full navigation sub-component ────────────────────────────────────────────

function SeriesNavigator({ data }: { data: SeriesData }) {
  const seriesTitle = data.data.series?.title ?? ''
  const tmdbId = data.data.series?.tmdb_id ?? ''

  // Flatten all episodes with season context
  const flatEpisodes = useMemo(() => {
    const result: FlatEpisode[] = []
    for (const season of data.data.seasons ?? []) {
      for (const ep of season.episodes ?? []) {
        result.push({
          seasonNumber: season.season_number,
          episodeNumber: ep.episode_number,
          title: ep.title,
          api: ep,
        })
      }
    }
    result.sort((a, b) => a.seasonNumber - b.seasonNumber || a.episodeNumber - b.episodeNumber)
    return result
  }, [data])

  const seasons = useMemo(
    () => [...new Set(flatEpisodes.map((e) => e.seasonNumber))].sort((a, b) => a - b),
    [flatEpisodes],
  )

  const [selectedSeason, setSelectedSeason] = useState<number>(seasons[0] ?? 1)
  const [selectedEpIdx, setSelectedEpIdx] = useState<number>(0)
  const [shouldAutoPlay, setShouldAutoPlay] = useState(false)
  const [selectedAudioTrack, setSelectedAudioTrack] = useState<number>(0)

  // Collect unique audio tracks from current episode (for dropdown).
  const currentAudioTracks = useMemo(() => {
    const ep = flatEpisodes.find((e) => e.seasonNumber === selectedSeason)
    const tracks = ep?.api.audio_tracks ?? []
    return tracks.map((t, idx) => {
      const lang = t.language ?? ''
      // Always prefer human-readable language name over raw label from source file.
      const label = lang
        ? (SUBTITLE_LABELS[lang] ?? lang.toUpperCase())
        : (t.label || `Track ${idx + 1}`)
      return { index: idx, label, language: lang }
    })
  }, [flatEpisodes, selectedSeason])

  const seasonEpisodes = useMemo(
    () => flatEpisodes.filter((e) => e.seasonNumber === selectedSeason),
    [flatEpisodes, selectedSeason],
  )

  const currentEp = seasonEpisodes[selectedEpIdx] ?? null

  const globalIdx = useMemo(
    () => (currentEp ? flatEpisodes.indexOf(currentEp) : -1),
    [flatEpisodes, currentEp],
  )

  function navigateTo(ep: FlatEpisode) {
    setSelectedSeason(ep.seasonNumber)
    const newSeasonEps = flatEpisodes.filter((e) => e.seasonNumber === ep.seasonNumber)
    const idx = newSeasonEps.indexOf(ep)
    setSelectedEpIdx(idx >= 0 ? idx : 0)
  }

  function handleSeasonChange(season: number) {
    setSelectedSeason(season)
    setSelectedEpIdx(0)
  }

  const hasPrev = globalIdx > 0
  const hasNext = globalIdx >= 0 && globalIdx < flatEpisodes.length - 1

  const playbackData = useMemo(
    () => currentEp?.api.playback?.hls ? episodeToPlayback(currentEp.api, tmdbId) : null,
    [currentEp, tmdbId],
  )

  // Show/hide nav with mouse activity (matches Fluid Player control behavior).
  const [navVisible, setNavVisible] = useState(true)
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const navVisibleRef = useRef(true)

  const resetHideTimer = useCallback(() => {
    if (hideTimerRef.current) clearTimeout(hideTimerRef.current)
    hideTimerRef.current = setTimeout(() => {
      navVisibleRef.current = false
      setNavVisible(false)
    }, 3000)
  }, [])

  const showNav = useCallback(() => {
    if (!navVisibleRef.current) {
      navVisibleRef.current = true
      setNavVisible(true)
    }
    resetHideTimer()
  }, [resetHideTimer])

  useEffect(() => {
    resetHideTimer()
    return () => { if (hideTimerRef.current) clearTimeout(hideTimerRef.current) }
  }, [resetHideTimer])

  const handleEpisodeEnded = useCallback(() => {
    if (hasNext) {
      setShouldAutoPlay(true)
      navigateTo(flatEpisodes[globalIdx + 1])
    }
  }, [hasNext, flatEpisodes, globalIdx])

  return (
    <div className="series-player-wrapper" onMouseMove={showNav} onTouchStart={showNav}>
      <div className={`series-nav${navVisible ? '' : ' series-nav-hidden'}`}>
        <div className="series-selectors">
          <select
            className="series-select"
            value={selectedSeason}
            onChange={(e) => handleSeasonChange(Number(e.target.value))}
          >
            {seasons.map((s) => (
              <option key={s} value={s}>
                Season {s}
              </option>
            ))}
          </select>

          <select
            className="series-select"
            value={selectedEpIdx}
            onChange={(e) => setSelectedEpIdx(Number(e.target.value))}
          >
            {seasonEpisodes.map((ep, idx) => (
              <option key={ep.episodeNumber} value={idx}>
                Episode {ep.episodeNumber}
              </option>
            ))}
          </select>

          {currentAudioTracks.length > 1 && (
            <select
              className="series-select"
              value={selectedAudioTrack}
              onChange={(e) => setSelectedAudioTrack(Number(e.target.value))}
            >
              {currentAudioTracks.map((t) => (
                <option key={t.index} value={t.index}>
                  {t.label}
                </option>
              ))}
            </select>
          )}
        </div>
      </div>

      {/* Prev / Next buttons — centered over video, next to play button */}
      <div className={`series-center-nav${navVisible ? '' : ' series-nav-hidden'}`}>
        {hasPrev && (
          <button
            type="button"
            className="series-center-btn"
            onClick={() => navigateTo(flatEpisodes[globalIdx - 1])}
            title="Previous episode"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" width="28" height="28">
              <path d="M6 6h2v12H6zm3.5 6 8.5 6V6z" />
            </svg>
          </button>
        )}
        {hasNext && (
          <button
            type="button"
            className="series-center-btn"
            onClick={() => navigateTo(flatEpisodes[globalIdx + 1])}
            title="Next episode"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" width="28" height="28">
              <path d="M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z" />
            </svg>
          </button>
        )}
      </div>

      {playbackData ? (
        <PlayerClient
          key={`s${currentEp!.seasonNumber}e${currentEp!.episodeNumber}a${selectedAudioTrack}`}
          playback={playbackData}
          onEnded={handleEpisodeEnded}
          autoPlay={shouldAutoPlay}
          initialAudioTrack={selectedAudioTrack}
        />
      ) : (
        <div className="player-status">Эпизод не готов</div>
      )}
    </div>
  )
}
