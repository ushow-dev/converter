'use client'

import { useState, useMemo } from 'react'
import PlayerClient, { type MovieResponse } from './PlayerClient'

// ── API response shapes ──────────────────────────────────────────────────────

export interface EpisodeItem {
  season: number
  episode: number
  title?: string
  playback: { hls: string }
  assets?: { poster?: string }
  subtitles?: { language: string; url: string }[]
}

/** Full series navigation response — /api/player/series */
export interface SeriesData {
  data: {
    series: { tmdb_id: string; title?: string }
    episodes: EpisodeItem[]
  }
  meta: { version: string }
}

/** Single episode response — /api/player/episode */
export interface EpisodeData {
  data: {
    series?: { tmdb_id: string; title?: string }
    episode: EpisodeItem
  }
  meta: { version: string }
}

type AnySeriesData = SeriesData | EpisodeData

// ── Helpers ──────────────────────────────────────────────────────────────────

function isEpisodeData(d: AnySeriesData): d is EpisodeData {
  return 'episode' in d.data
}

function episodeToMovieResponse(ep: EpisodeItem): MovieResponse {
  return {
    data: {
      movie: { id: 0, imdb_id: '', tmdb_id: '' },
      playback: { hls: ep.playback.hls },
      assets: { poster: ep.assets?.poster ?? '' },
      subtitles: ep.subtitles,
    },
    meta: { version: '1' },
  }
}

function epLabel(ep: EpisodeItem): string {
  const base = `S${String(ep.season).padStart(2, '0')}E${String(ep.episode).padStart(2, '0')}`
  return ep.title ? `${base} — ${ep.title}` : base
}

// ── Component ────────────────────────────────────────────────────────────────

interface SeriesPlayerProps {
  initialData: AnySeriesData
  hideNavigation?: boolean
}

export default function SeriesPlayer({ initialData, hideNavigation = false }: SeriesPlayerProps) {
  // Single-episode embed mode
  if (hideNavigation || isEpisodeData(initialData)) {
    const ep = isEpisodeData(initialData) ? initialData.data.episode : null
    if (ep) {
      return (
        <div className="series-player-wrapper">
          <PlayerClient key={`${ep.season}-${ep.episode}`} initialData={episodeToMovieResponse(ep)} />
        </div>
      )
    }
  }

  // Full navigation mode requires SeriesData
  return <SeriesNavigator data={initialData as SeriesData} />
}

// ── Full navigation sub-component ────────────────────────────────────────────

function SeriesNavigator({ data }: { data: SeriesData }) {
  const episodes = data.data.episodes ?? []
  const title = data.data.series?.title ?? ''

  // Derive unique season numbers
  const seasons = useMemo(
    () => [...new Set(episodes.map((ep) => ep.season))].sort((a, b) => a - b),
    [episodes],
  )

  const [selectedSeason, setSelectedSeason] = useState<number>(seasons[0] ?? 1)
  const [selectedEpIdx, setSelectedEpIdx] = useState<number>(0)

  // Episodes for the current season
  const seasonEpisodes = useMemo(
    () => episodes.filter((ep) => ep.season === selectedSeason).sort((a, b) => a.episode - b.episode),
    [episodes, selectedSeason],
  )

  const currentEp = seasonEpisodes[selectedEpIdx] ?? null

  // Global index helpers for prev/next across seasons
  const globalIdx = useMemo(
    () => (currentEp ? episodes.findIndex((ep) => ep.season === currentEp.season && ep.episode === currentEp.episode) : -1),
    [episodes, currentEp],
  )

  function navigateTo(ep: EpisodeItem) {
    const newSeason = ep.season
    const newSeasonEps = episodes.filter((e) => e.season === newSeason).sort((a, b) => a.episode - b.episode)
    const idx = newSeasonEps.findIndex((e) => e.episode === ep.episode)
    setSelectedSeason(newSeason)
    setSelectedEpIdx(idx >= 0 ? idx : 0)
  }

  function handleSeasonChange(season: number) {
    setSelectedSeason(season)
    setSelectedEpIdx(0)
  }

  function handleEpisodeChange(idx: number) {
    setSelectedEpIdx(idx)
  }

  const hasPrev = globalIdx > 0
  const hasNext = globalIdx >= 0 && globalIdx < episodes.length - 1

  const movieData = currentEp ? episodeToMovieResponse(currentEp) : null

  return (
    <div className="series-player-wrapper">
      <div className="series-nav">
        {title && <span className="series-title">{title}</span>}

        <div className="series-selectors">
          {/* Season selector */}
          <select
            className="series-select"
            value={selectedSeason}
            onChange={(e) => handleSeasonChange(Number(e.target.value))}
            aria-label="Season"
          >
            {seasons.map((s) => (
              <option key={s} value={s}>
                Season {s}
              </option>
            ))}
          </select>

          {/* Episode selector */}
          <select
            className="series-select"
            value={selectedEpIdx}
            onChange={(e) => handleEpisodeChange(Number(e.target.value))}
            aria-label="Episode"
          >
            {seasonEpisodes.map((ep, idx) => (
              <option key={ep.episode} value={idx}>
                {epLabel(ep)}
              </option>
            ))}
          </select>
        </div>

        {/* Prev / Next buttons */}
        <div className="series-ep-nav">
          <button
            type="button"
            className="ep-nav-btn"
            disabled={!hasPrev}
            onClick={() => hasPrev && navigateTo(episodes[globalIdx - 1])}
            aria-label="Previous episode"
          >
            &#8592; Prev
          </button>
          <button
            type="button"
            className="ep-nav-btn"
            disabled={!hasNext}
            onClick={() => hasNext && navigateTo(episodes[globalIdx + 1])}
            aria-label="Next episode"
          >
            Next &#8594;
          </button>
        </div>
      </div>

      {movieData && (
        <PlayerClient
          key={`${currentEp!.season}-${currentEp!.episode}`}
          initialData={movieData}
        />
      )}
    </div>
  )
}
