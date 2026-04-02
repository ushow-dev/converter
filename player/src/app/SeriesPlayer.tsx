'use client'

import { useState, useMemo, useCallback } from 'react'
import PlayerClient, { type MovieResponse } from './PlayerClient'

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

function episodeToMovieResponse(ep: EpisodeAPI, tmdbId?: string): MovieResponse {
  return {
    data: {
      movie: { id: 0, imdb_id: '', tmdb_id: tmdbId ?? '' },
      playback: { hls: ep.playback?.hls ?? '' },
      assets: { poster: ep.assets?.thumbnail ?? '' },
      subtitles: ep.subtitles,
    },
    meta: { version: 'v1' },
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
    return <PlayerClient initialData={episodeToMovieResponse(ep, tmdbId)} />
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

  const movieData = currentEp?.api.playback?.hls ? episodeToMovieResponse(currentEp.api, tmdbId) : null

  const handleEpisodeEnded = useCallback(() => {
    if (hasNext) {
      setShouldAutoPlay(true)
      navigateTo(flatEpisodes[globalIdx + 1])
    }
  }, [hasNext, flatEpisodes, globalIdx])

  return (
    <div className="series-player-wrapper">
      <div className="series-nav">
        {seriesTitle && <span className="series-title">{seriesTitle}</span>}

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
        </div>

        <div className="series-ep-nav">
          <button
            type="button"
            className="ep-nav-btn"
            disabled={!hasPrev}
            onClick={() => hasPrev && navigateTo(flatEpisodes[globalIdx - 1])}
          >
            ← Пред.
          </button>
          <button
            type="button"
            className="ep-nav-btn"
            disabled={!hasNext}
            onClick={() => hasNext && navigateTo(flatEpisodes[globalIdx + 1])}
          >
            След. →
          </button>
        </div>
      </div>

      {movieData ? (
        <PlayerClient
          key={`s${currentEp!.seasonNumber}e${currentEp!.episodeNumber}`}
          initialData={movieData}
          onEnded={handleEpisodeEnded}
          autoPlay={shouldAutoPlay}
        />
      ) : (
        <div className="player-status">Эпизод не готов</div>
      )}
    </div>
  )
}
