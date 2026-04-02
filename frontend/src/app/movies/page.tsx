'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { useSWRConfig } from 'swr'
import { getToken, fetcher, moviesUrl, deleteJob, updateMovieIDs } from '@/lib/api'
import { Nav } from '@/components/Nav'
import { PlayerModal } from '@/components/PlayerModal'
import { FilmIcon, MovieRow } from '@/components/MovieTable'
import type { Movie, MoviesResponse } from '@/types'

export default function MoviesPage() {
  const router = useRouter()
  const { mutate } = useSWRConfig()
  const swrKey = moviesUrl(100)
  const [playingMovie, setPlayingMovie] = useState<Movie | null>(null)
  const [playerUrl, setPlayerUrl] = useState('')

  useEffect(() => {
    if (!getToken()) router.replace('/login')
  }, [router])

  useEffect(() => {
    fetch('/api/app-config')
      .then(r => r.json())
      .then(cfg => setPlayerUrl(cfg.playerUrl ?? ''))
      .catch(() => {})
  }, [])

  const { data, error, isLoading } = useSWR<MoviesResponse>(
    swrKey,
    fetcher,
    { refreshInterval: 10000 },
  )

  async function handleDelete(jobId: string) {
    if (!window.confirm('Удалить фильм и все связанные файлы?')) return
    try {
      await deleteJob(jobId)
      mutate(swrKey)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при удалении')
    }
  }

  async function handleUpdate(movieId: number, imdbId: string, tmdbId: string, title: string) {
    try {
      await updateMovieIDs(movieId, imdbId, tmdbId, title)
      mutate(swrKey)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при сохранении')
    }
  }

  function handleExportCSV() {
    if (!data?.items.length) return
    const headers = ['id', 'title', 'year', 'imdb_id', 'tmdb_id', 'storage_key', 'poster_url', 'has_thumbnail', 'job_id', 'created_at', 'updated_at']
    const escape = (v: unknown) => {
      const s = v == null ? '' : String(v)
      return s.includes(',') || s.includes('"') || s.includes('\n') ? `"${s.replace(/"/g, '""')}"` : s
    }
    const rows = data.items.map(m => headers.map(h => escape(m[h as keyof typeof m])).join(','))
    const csv = [headers.join(','), ...rows].join('\n')
    const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8;' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `movies_${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  const playerSrc = playingMovie
    ? `${playerUrl}/?tmdb_id=${playingMovie.tmdb_id}`
    : ''

  const playerTitle = playingMovie?.title
    ? `${playingMovie.title}${playingMovie.year ? ` (${playingMovie.year})` : ''}`
    : undefined

  return (
    <div className="min-h-screen">
      {playingMovie && (
        <PlayerModal
          src={playerSrc}
          title={playerTitle}
          onClose={() => setPlayingMovie(null)}
        />
      )}
      <Nav />

      <main className="px-3 py-4 sm:px-6 sm:py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-y-3">
          <h1 className="text-xl font-semibold text-white">Фильмы</h1>
          <div className="flex items-center gap-2">
            {data && data.items.length > 0 && (
              <button
                onClick={handleExportCSV}
                className="rounded-md border border-gray-700 px-4 py-2 text-sm font-semibold text-gray-300 hover:bg-gray-800 hover:text-white transition-colors"
              >
                Экспорт CSV
              </button>
            )}
            <Link
              href="/upload"
              className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
            >
              + Добавить
            </Link>
          </div>
        </div>

        {isLoading && (
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-gray-700 border-t-indigo-400" />
            Загрузка…
          </div>
        )}

        {error && (
          <div className="rounded-md border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-400">
            {error instanceof Error ? error.message : 'Ошибка загрузки'}
          </div>
        )}

        {data && data.items.length === 0 && (
          <div className="flex flex-col items-center gap-4 py-24 text-center">
            <FilmIcon />
            <div>
              <p className="text-gray-400">Пока нет фильмов</p>
              <p className="mt-1 text-sm text-gray-600">Найдите и добавьте первый фильм</p>
            </div>
            <Link
              href="/upload"
              className="rounded-md bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-indigo-500"
            >
              Добавить фильм
            </Link>
          </div>
        )}

        {data && data.items.length > 0 && (
          <div className="overflow-x-auto rounded-md border border-gray-800">
            <table className="w-full">
              <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-500">
                <tr>
                  <th className="px-3 py-2 w-14" />
                  <th className="hidden sm:table-cell px-3 py-2">ID</th>
                  <th className="hidden sm:table-cell px-3 py-2">IMDb</th>
                  <th className="px-3 py-2">TMDB</th>
                  <th className="px-3 py-2">Название</th>
                  <th className="hidden sm:table-cell px-3 py-2">Год</th>
                  <th className="hidden sm:table-cell px-3 py-2">Добавлен</th>
                  <th className="hidden sm:table-cell px-3 py-2">Субтитры</th>
                  <th className="px-3 py-2 w-8" />
                  <th className="px-3 py-2 w-10" />
                </tr>
              </thead>
              <tbody>
                {data.items.map(movie => (
                  <MovieRow key={movie.id} movie={movie} onDelete={handleDelete} onUpdate={handleUpdate} onPlay={setPlayingMovie} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </main>
    </div>
  )
}
