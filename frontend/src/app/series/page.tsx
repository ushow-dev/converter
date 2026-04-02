'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { getToken, fetcher, seriesUrl, formatDate, deleteSeries } from '@/lib/api'
import { Nav } from '@/components/Nav'
import type { Series, SeriesResponse } from '@/types'

function TvIcon() {
  return (
    <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
        d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  )
}

function Thumbnail({ series }: { series: Series }) {
  if (series.poster_url) {
    return (
      <div className="relative h-16 w-11 shrink-0 overflow-hidden rounded bg-gray-800">
        <img
          src={series.poster_url}
          alt=""
          className="h-full w-full object-cover"
          onError={e => {
            const el = e.currentTarget
            el.style.display = 'none'
            el.nextElementSibling?.classList.remove('hidden')
          }}
        />
        <div className="absolute inset-0 hidden flex items-center justify-center bg-gray-800">
          <TvIcon />
        </div>
      </div>
    )
  }
  return (
    <div className="flex h-16 w-11 shrink-0 items-center justify-center rounded bg-gray-800/60">
      <TvIcon />
    </div>
  )
}

export default function SeriesPage() {
  const router = useRouter()
  const [items, setItems] = useState<Series[]>([])
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [playerUrl, setPlayerUrl] = useState('')

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login')
      return
    }
    loadPage(undefined)
  }, [router])

  useEffect(() => {
    fetch('/api/app-config')
      .then(r => r.json())
      .then(cfg => setPlayerUrl(cfg.playerUrl ?? ''))
      .catch(() => {})
  }, [])

  async function loadPage(cursor: string | undefined) {
    try {
      const data = await fetcher<SeriesResponse>(seriesUrl(50, cursor))
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
  }

  function handleLoadMore() {
    if (!nextCursor) return
    setLoadingMore(true)
    loadPage(nextCursor)
  }

  async function handleDelete(seriesId: number) {
    if (!window.confirm('Удалить сериал и все связанные данные?')) return
    try {
      await deleteSeries(seriesId)
      setItems(prev => prev.filter(s => s.id !== seriesId))
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при удалении')
    }
  }

  return (
    <div className="min-h-screen">
      <Nav />

      <main className="px-3 py-4 sm:px-6 sm:py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-y-3">
          <h1 className="text-xl font-semibold text-white">Сериалы</h1>
        </div>

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

        {!loading && !error && items.length === 0 && (
          <div className="flex flex-col items-center gap-4 py-24 text-center">
            <TvIcon />
            <div>
              <p className="text-gray-400">Пока нет сериалов</p>
              <p className="mt-1 text-sm text-gray-600">Сериалы появятся после обработки</p>
            </div>
          </div>
        )}

        {items.length > 0 && (
          <>
            <div className="overflow-x-auto rounded-md border border-gray-800">
              <table className="w-full">
                <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-500">
                  <tr>
                    <th className="px-3 py-2 w-14" />
                    <th className="px-3 py-2">Название</th>
                    <th className="hidden sm:table-cell px-3 py-2">Год</th>
                    <th className="hidden sm:table-cell px-3 py-2">TMDB</th>
                    <th className="hidden sm:table-cell px-3 py-2">Добавлен</th>
                    <th className="px-3 py-2 w-8" />
                    <th className="px-3 py-2 w-10" />
                  </tr>
                </thead>
                <tbody>
                  {items.map(series => (
                    <tr key={series.id} className="group border-b border-gray-800 hover:bg-gray-900/60">
                      {/* Poster */}
                      <td className="w-14 px-3 py-2">
                        <Thumbnail series={series} />
                      </td>

                      {/* Title */}
                      <td className="px-3 py-3">
                        <Link
                          href={`/series/${series.id}`}
                          className="font-medium text-gray-200 hover:text-indigo-400 transition-colors"
                        >
                          {series.title}
                        </Link>
                        <span className="mt-0.5 block font-mono text-[10px] text-gray-700">
                          {series.storage_key}
                        </span>
                      </td>

                      {/* Year */}
                      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-3 text-sm text-gray-400">
                        {series.year ?? <span className="text-gray-700">—</span>}
                      </td>

                      {/* TMDB */}
                      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-3">
                        {series.tmdb_id ? (
                          <a
                            href={`https://www.themoviedb.org/tv/${series.tmdb_id}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-mono text-xs text-gray-400 hover:text-blue-400 transition-colors"
                          >
                            {series.tmdb_id}
                          </a>
                        ) : (
                          <span className="text-gray-700">—</span>
                        )}
                      </td>

                      {/* Date */}
                      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-3 text-xs text-gray-500">
                        {formatDate(series.created_at)}
                      </td>

                      {/* Play + TMDB link */}
                      <td className="px-2 py-3">
                        <div className="flex items-center gap-1">
                          <a
                            href={playerUrl && series.tmdb_id ? `${playerUrl}/?tmdb_id=${series.tmdb_id}&type=series` : '#'}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={e => { if (!playerUrl || !series.tmdb_id) e.preventDefault() }}
                            title={!series.tmdb_id ? 'Требуется TMDB ID' : 'Смотреть'}
                            className={`rounded p-1.5 transition-colors inline-flex ${series.tmdb_id && playerUrl ? 'text-gray-600 hover:bg-green-900/40 hover:text-green-400' : 'text-gray-800 cursor-not-allowed'}`}
                          >
                            <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M8 5v14l11-7z" />
                            </svg>
                          </a>
                          {series.tmdb_id && (
                            <a
                              href={`https://www.themoviedb.org/tv/${series.tmdb_id}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              title="Посмотреть на TMDB"
                              className="rounded p-1.5 text-gray-600 hover:bg-blue-900/40 hover:text-blue-400 transition-colors inline-flex"
                            >
                              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                                  d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                              </svg>
                            </a>
                          )}
                        </div>
                      </td>

                      {/* Delete + detail link */}
                      <td className="px-3 py-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <button
                            onClick={() => handleDelete(series.id)}
                            className="rounded p-1.5 text-gray-600 hover:bg-red-900/40 hover:text-red-400 transition-colors"
                            title="Удалить"
                          >
                            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                                d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                            </svg>
                          </button>
                          <Link
                            href={`/series/${series.id}`}
                            className="rounded p-1.5 text-gray-600 hover:bg-indigo-900/40 hover:text-indigo-400 transition-colors inline-flex"
                            title="Подробнее"
                          >
                            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                                d="M9 5l7 7-7 7" />
                            </svg>
                          </Link>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {nextCursor && (
              <div className="mt-4 flex justify-center">
                <button
                  onClick={handleLoadMore}
                  disabled={loadingMore}
                  className="rounded-md border border-gray-700 px-6 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-white transition-colors disabled:opacity-50"
                >
                  {loadingMore ? (
                    <span className="flex items-center gap-2">
                      <span className="h-4 w-4 animate-spin rounded-full border-2 border-gray-600 border-t-indigo-400" />
                      Загрузка…
                    </span>
                  ) : (
                    'Загрузить ещё'
                  )}
                </button>
              </div>
            )}
          </>
        )}
      </main>
    </div>
  )
}
