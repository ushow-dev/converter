'use client'

import { useEffect, useState } from 'react'
import { useRouter, useParams } from 'next/navigation'
import Link from 'next/link'
import {
  getToken,
  getSeriesDetail,
  deleteSeries,
  deleteEpisode,
  episodeThumbnailSrc,
  formatDate,
} from '@/lib/api'
import { Nav } from '@/components/Nav'
import type { SeriesDetailResponse, Episode } from '@/types'

function TvIcon() {
  return (
    <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
        d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  )
}

// ── Player modal ───────────────────────────────────────────────────────────────

interface PlayingEpisode {
  seasonNumber: number
  episodeNumber: number
  title?: string
}

function PlayerModal({
  episode,
  tmdbId,
  playerUrl,
  onClose,
}: {
  episode: PlayingEpisode
  tmdbId?: string
  playerUrl: string
  onClose: () => void
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const src = `${playerUrl}/?tmdb_id=${tmdbId}&type=series&s=${episode.seasonNumber}&e=${episode.episodeNumber}`

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-3xl mx-4"
        onClick={e => e.stopPropagation()}
      >
        <button
          onClick={onClose}
          className="absolute -top-8 right-0 text-gray-400 hover:text-white text-sm"
        >
          ✕ закрыть
        </button>
        <div className="w-full overflow-hidden rounded-lg bg-black" style={{ aspectRatio: '16/10' }}>
          <iframe
            src={src}
            style={{ width: '100%', height: '100%', border: 0, display: 'block' }}
            scrolling="no"
            allow="autoplay; fullscreen; picture-in-picture"
            allowFullScreen
          />
        </div>
        {episode.title && (
          <p className="mt-2 text-center text-sm text-gray-400">{episode.title}</p>
        )}
      </div>
    </div>
  )
}

// ── Episode row ────────────────────────────────────────────────────────────────

function EpisodeRow({
  episode,
  seasonNumber,
  tmdbId,
  playerUrl,
  onPlay,
  onDelete,
}: {
  episode: Episode
  seasonNumber: number
  tmdbId?: string
  playerUrl: string
  onPlay: (ep: PlayingEpisode) => void
  onDelete: (id: number) => void
}) {
  return (
    <tr className="border-b border-gray-800/60 hover:bg-gray-900/40">
      {/* Thumbnail */}
      <td className="w-14 px-4 py-2">
        {episode.has_thumbnail ? (
          <div className="relative h-10 w-16 shrink-0 overflow-hidden rounded bg-gray-800">
            <img
              src={episode.thumbnail_url || episodeThumbnailSrc(episode.id)}
              alt=""
              className="h-full w-full object-cover"
              onError={e => { e.currentTarget.style.display = 'none' }}
            />
          </div>
        ) : (
          <div className="flex h-10 w-16 shrink-0 items-center justify-center rounded bg-gray-800/60">
            <TvIcon />
          </div>
        )}
      </td>
      <td className="px-4 py-2 text-sm text-gray-400 w-12">
        {episode.episode_number}
      </td>
      <td className="px-4 py-2 text-sm text-gray-200">
        {episode.title ?? <span className="text-gray-600">Эпизод {episode.episode_number}</span>}
      </td>
      <td className="hidden sm:table-cell px-4 py-2 font-mono text-xs text-gray-600">
        {episode.storage_key}
      </td>
      <td className="hidden sm:table-cell px-4 py-2 text-xs text-gray-600 whitespace-nowrap">
        {formatDate(episode.created_at)}
      </td>
      <td className="px-4 py-2 text-right">
        <div className="flex items-center justify-end gap-1">
          {tmdbId && playerUrl && (
            <button
              onClick={() => onPlay({ seasonNumber, episodeNumber: episode.episode_number, title: episode.title })}
              className="rounded p-1.5 text-gray-600 hover:bg-green-900/40 hover:text-green-400 transition-colors inline-flex"
              title="Смотреть"
            >
              <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                <path d="M8 5v14l11-7z" />
              </svg>
            </button>
          )}
          <button
            onClick={() => onDelete(episode.id)}
            className="rounded p-1.5 text-gray-600 hover:bg-red-900/40 hover:text-red-400 transition-colors"
            title="Удалить эпизод"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          </button>
        </div>
      </td>
    </tr>
  )
}

// ── Season section ─────────────────────────────────────────────────────────────

function SeasonSection({
  season,
  tmdbId,
  playerUrl,
  onPlay,
  onDelete,
}: {
  season: SeriesDetailResponse['seasons'][number]
  tmdbId?: string
  playerUrl: string
  onPlay: (ep: PlayingEpisode) => void
  onDelete: (id: number) => void
}) {
  const [open, setOpen] = useState(true)

  return (
    <div className="rounded-md border border-gray-800 overflow-hidden">
      <button
        onClick={() => setOpen(o => !o)}
        className="w-full flex items-center justify-between px-4 py-3 bg-gray-900 hover:bg-gray-800 transition-colors text-left"
      >
        <div className="flex items-center gap-3">
          {season.poster_url && (
            <img
              src={season.poster_url}
              alt=""
              className="h-10 w-7 rounded object-cover shrink-0"
            />
          )}
          <span className="font-medium text-gray-200">
            Сезон {season.season_number}
          </span>
          <span className="text-xs text-gray-500">
            {season.episodes.length} {episodeWord(season.episodes.length)}
          </span>
        </div>
        <svg
          className={`h-4 w-4 text-gray-500 transition-transform ${open ? 'rotate-180' : ''}`}
          fill="none" viewBox="0 0 24 24" stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {open && (
        <div className="overflow-x-auto">
          {season.episodes.length === 0 ? (
            <p className="px-4 py-3 text-sm text-gray-600">Нет эпизодов</p>
          ) : (
            <table className="w-full">
              <thead className="text-left text-xs uppercase tracking-wider text-gray-600 bg-gray-900/50">
                <tr>
                  <th className="px-4 py-2 w-14" />
                  <th className="px-4 py-2 w-12">#</th>
                  <th className="px-4 py-2">Название</th>
                  <th className="hidden sm:table-cell px-4 py-2">storage_key</th>
                  <th className="hidden sm:table-cell px-4 py-2">Добавлен</th>
                  <th className="px-4 py-2 w-20" />
                </tr>
              </thead>
              <tbody>
                {season.episodes.map(ep => (
                  <EpisodeRow
                    key={ep.id}
                    episode={ep}
                    seasonNumber={season.season_number}
                    tmdbId={tmdbId}
                    playerUrl={playerUrl}
                    onPlay={onPlay}
                    onDelete={onDelete}
                  />
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}

function episodeWord(n: number): string {
  if (n % 100 >= 11 && n % 100 <= 19) return 'эпизодов'
  if (n % 10 === 1) return 'эпизод'
  if (n % 10 >= 2 && n % 10 <= 4) return 'эпизода'
  return 'эпизодов'
}

export default function SeriesDetailPage() {
  const router = useRouter()
  const params = useParams()
  const id = Number(params.id)

  const [detail, setDetail] = useState<SeriesDetailResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [playerUrl, setPlayerUrl] = useState('')
  const [playingEpisode, setPlayingEpisode] = useState<PlayingEpisode | null>(null)

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login')
      return
    }
    getSeriesDetail(id)
      .then(data => setDetail(data))
      .catch(err => setError(err instanceof Error ? err.message : 'Ошибка загрузки'))
      .finally(() => setLoading(false))
  }, [id, router])

  useEffect(() => {
    fetch('/api/app-config')
      .then(r => r.json())
      .then(cfg => setPlayerUrl(cfg.playerUrl ?? ''))
      .catch(() => {})
  }, [])

  async function handleDelete() {
    if (!window.confirm('Удалить сериал и все связанные данные?')) return
    setDeleting(true)
    try {
      await deleteSeries(id)
      router.push('/series')
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при удалении')
      setDeleting(false)
    }
  }

  async function handleDeleteEpisode(episodeId: number) {
    if (!window.confirm('Удалить эпизод и все связанные файлы?')) return
    try {
      await deleteEpisode(episodeId)
      const data = await getSeriesDetail(id)
      setDetail(data)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при удалении')
    }
  }

  return (
    <div className="min-h-screen">
      {playingEpisode && detail?.series.tmdb_id && (
        <PlayerModal
          episode={playingEpisode}
          tmdbId={detail.series.tmdb_id}
          playerUrl={playerUrl}
          onClose={() => setPlayingEpisode(null)}
        />
      )}

      <Nav />

      <main className="px-3 py-4 sm:px-6 sm:py-8 max-w-5xl">
        {/* Back link */}
        <Link
          href="/series"
          className="mb-6 inline-flex items-center gap-1 text-sm text-gray-500 hover:text-gray-300 transition-colors"
        >
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M15 19l-7-7 7-7" />
          </svg>
          Сериалы
        </Link>

        {loading && (
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-gray-700 border-t-indigo-400" />
            Загрузка…
          </div>
        )}

        {error && (
          <div className="rounded-md border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {detail && (
          <>
            {/* Series header */}
            <div className="mb-8 flex gap-5 items-start">
              {detail.series.poster_url ? (
                <img
                  src={detail.series.poster_url}
                  alt=""
                  className="h-32 w-[88px] shrink-0 rounded-md object-cover"
                />
              ) : (
                <div className="flex h-32 w-[88px] shrink-0 items-center justify-center rounded-md bg-gray-800">
                  <TvIcon />
                </div>
              )}

              <div className="flex-1 min-w-0">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <h1 className="text-2xl font-semibold text-white">
                      {detail.series.title}
                      {detail.series.year && (
                        <span className="ml-2 text-lg font-normal text-gray-500">
                          ({detail.series.year})
                        </span>
                      )}
                    </h1>
                    <p className="mt-1 font-mono text-xs text-gray-600">{detail.series.storage_key}</p>
                  </div>
                  <button
                    onClick={handleDelete}
                    disabled={deleting}
                    className="rounded-md border border-red-800 px-3 py-1.5 text-sm text-red-400 hover:bg-red-900/30 transition-colors disabled:opacity-50"
                  >
                    {deleting ? 'Удаление…' : 'Удалить сериал'}
                  </button>
                </div>

                <div className="mt-3 flex flex-wrap gap-x-8 gap-y-2 text-sm">
                  {detail.series.tmdb_id && (
                    <div>
                      <span className="text-gray-600">TMDB </span>
                      <a
                        href={`https://www.themoviedb.org/tv/${detail.series.tmdb_id}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-mono text-gray-400 hover:text-blue-400 transition-colors"
                      >
                        {detail.series.tmdb_id}
                      </a>
                    </div>
                  )}
                  {detail.series.imdb_id && (
                    <div>
                      <span className="text-gray-600">IMDb </span>
                      <a
                        href={`https://www.imdb.com/title/${detail.series.imdb_id}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-mono text-gray-400 hover:text-yellow-400 transition-colors"
                      >
                        {detail.series.imdb_id}
                      </a>
                    </div>
                  )}
                  {detail.series.year && (
                    <div>
                      <span className="text-gray-600">Год </span>
                      <span className="text-gray-400">{detail.series.year}</span>
                    </div>
                  )}
                  <div>
                    <span className="text-gray-600">Сезонов </span>
                    <span className="text-gray-400">{detail.seasons.length}</span>
                  </div>
                  <div>
                    <span className="text-gray-600">Добавлен </span>
                    <span className="text-gray-400">{formatDate(detail.series.created_at)}</span>
                  </div>
                </div>
              </div>
            </div>

            {/* Seasons */}
            {detail.seasons.length === 0 ? (
              <p className="text-sm text-gray-600">Нет сезонов</p>
            ) : (
              <div className="flex flex-col gap-3">
                {detail.seasons.map(season => (
                  <SeasonSection
                    key={season.id}
                    season={season}
                    tmdbId={detail.series.tmdb_id}
                    playerUrl={playerUrl}
                    onPlay={setPlayingEpisode}
                    onDelete={handleDeleteEpisode}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </main>
    </div>
  )
}
