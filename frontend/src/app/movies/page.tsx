'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { useSWRConfig } from 'swr'
import { getToken, fetcher, moviesUrl, movieThumbnailSrc, formatDate, deleteJob, updateMovieIDs, getMovieSubtitles, searchSubtitles } from '@/lib/api'
import { Nav } from '@/components/Nav'
import type { Movie, MoviesResponse, Subtitle } from '@/types'

// ── Thumbnail cell ────────────────────────────────────────────────────────────

function FilmIcon() {
  return (
    <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M7 4v16M17 4v16M3 8h4m10 0h4M3 12h18M3 16h4m10 0h4M4 20h16a1 1 0 001-1V5a1 1 0 00-1-1H4a1 1 0 00-1 1v14a1 1 0 001 1z" />
    </svg>
  )
}

function Thumbnail({ movie }: { movie: Movie }) {
  if (movie.has_thumbnail) {
    return (
      <div className="relative h-16 w-11 shrink-0 overflow-hidden rounded bg-gray-800">
        <img
          src={movieThumbnailSrc(movie.id)}
          alt=""
          className="h-full w-full object-cover"
          onError={e => {
            const el = e.currentTarget
            el.style.display = 'none'
            el.nextElementSibling?.classList.remove('hidden')
          }}
        />
        <div className="absolute inset-0 hidden flex items-center justify-center bg-gray-800">
          <FilmIcon />
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-16 w-11 shrink-0 items-center justify-center rounded bg-gray-800/60">
      <FilmIcon />
    </div>
  )
}

// ── Subtitle cell ─────────────────────────────────────────────────────────────

function SubtitleCell({ movie }: { movie: Movie }) {
  const [subs, setSubs] = useState<Subtitle[] | null>(null)
  const [searching, setSearching] = useState(false)

  useEffect(() => {
    getMovieSubtitles(movie.id).then(r => setSubs(r.items)).catch(() => setSubs([]))
  }, [movie.id])

  async function handleSearch() {
    setSearching(true)
    try {
      const result = await searchSubtitles(movie.id)
      setSubs(result.items)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при поиске субтитров')
    } finally {
      setSearching(false)
    }
  }

  return (
    <div className="flex items-center gap-1 flex-wrap min-w-0">
      {subs === null ? (
        <span className="text-[10px] text-gray-700">…</span>
      ) : subs.length === 0 ? (
        <span className="text-[10px] text-gray-700">—</span>
      ) : (
        subs.map(s => (
          <span key={s.language} className="rounded px-1.5 py-0.5 text-[10px] font-mono uppercase bg-gray-800 text-gray-400">
            {s.language}
          </span>
        ))
      )}
      <button
        onClick={handleSearch}
        disabled={searching || !movie.tmdb_id}
        title={!movie.tmdb_id ? 'Требуется TMDB ID' : 'Поиск субтитров на OpenSubtitles'}
        className="ml-0.5 rounded p-1 text-gray-600 hover:bg-indigo-900/40 hover:text-indigo-400 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
      >
        {searching ? (
          <div className="h-3.5 w-3.5 animate-spin rounded-full border border-gray-600 border-t-indigo-400" />
        ) : (
          <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
              d="M7 8h10M7 12h6m-6 4h4M5 4h14a1 1 0 011 1v14a1 1 0 01-1 1H5a1 1 0 01-1-1V5a1 1 0 011-1z" />
          </svg>
        )}
      </button>
    </div>
  )
}

// ── Editable ID cell ──────────────────────────────────────────────────────────

function EditableID({
  value,
  placeholder,
  width,
  onSave,
}: {
  value: string | undefined
  placeholder: string
  width: string
  onSave: (v: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')

  function open() {
    setDraft(value ?? '')
    setEditing(true)
  }

  function commit() {
    setEditing(false)
    if (draft !== (value ?? '')) onSave(draft)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') commit()
    if (e.key === 'Escape') setEditing(false)
  }

  if (editing) {
    return (
      <input
        autoFocus
        value={draft}
        onChange={e => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        className={`${width} rounded border border-indigo-500 bg-gray-900 px-1.5 py-0.5 font-mono text-xs text-gray-100 outline-none`}
      />
    )
  }

  return (
    <button
      onClick={open}
      title="Нажмите для редактирования"
      className="font-mono text-xs text-gray-400 hover:text-indigo-400 transition-colors"
    >
      {value ?? <span className="text-gray-700">—</span>}
    </button>
  )
}

// ── Player modal ──────────────────────────────────────────────────────────────

function PlayerModal({ movie, playerUrl, onClose }: { movie: Movie; playerUrl: string; onClose: () => void }) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const src = `${playerUrl}/?tmdb_id=${movie.tmdb_id}`

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
        {movie.title && (
          <p className="mt-2 text-center text-sm text-gray-400">{movie.title}{movie.year ? ` (${movie.year})` : ''}</p>
        )}
      </div>
    </div>
  )
}

// ── Table row ─────────────────────────────────────────────────────────────────

function MovieRow({
  movie,
  onDelete,
  onUpdate,
  onPlay,
}: {
  movie: Movie
  onDelete: (jobId: string) => void
  onUpdate: (movieId: number, imdbId: string, tmdbId: string, title: string) => void
  onPlay: (movie: Movie) => void
}) {
  return (
    <tr className="group border-b border-gray-800 hover:bg-gray-900/60">
      {/* Poster */}
      <td className="w-14 px-3 py-2">
        <Thumbnail movie={movie} />
      </td>

      {/* Movie ID */}
      <td className="whitespace-nowrap px-3 py-2 text-xs text-gray-400">
        {movie.id}
      </td>

      {/* IMDb */}
      <td className="whitespace-nowrap px-3 py-2">
        <EditableID
          value={movie.imdb_id}
          placeholder="tt0000000"
          width="w-28"
          onSave={v => onUpdate(movie.id, v, movie.tmdb_id ?? '', movie.title ?? '')}
        />
      </td>

      {/* TMDB */}
      <td className="whitespace-nowrap px-3 py-2">
        <EditableID
          value={movie.tmdb_id}
          placeholder="12345"
          width="w-20"
          onSave={v => onUpdate(movie.id, movie.imdb_id ?? '', v, movie.title ?? '')}
        />
      </td>

      {/* Title */}
      <td className="px-3 py-2">
        <EditableID
          value={movie.title}
          placeholder="Название фильма"
          width="w-72"
          onSave={v => onUpdate(movie.id, movie.imdb_id ?? '', movie.tmdb_id ?? '', v)}
        />
        {!movie.title && (
          <span className="mt-0.5 block font-mono text-[10px] text-gray-700">{movie.storage_key}</span>
        )}
      </td>

      {/* Year */}
      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400">
        {movie.year ?? <span className="text-gray-700">—</span>}
      </td>

      {/* Date */}
      <td className="whitespace-nowrap px-3 py-2 text-xs text-gray-500">
        {formatDate(movie.created_at)}
      </td>

      {/* Subtitles */}
      <td className="px-3 py-2">
        <SubtitleCell movie={movie} />
      </td>

      {/* Play */}
      <td className="px-2 py-2">
        <button
          onClick={() => onPlay(movie)}
          disabled={!movie.tmdb_id}
          title={!movie.tmdb_id ? 'Требуется TMDB ID' : 'Смотреть'}
          className="rounded p-1.5 text-gray-600 hover:bg-green-900/40 hover:text-green-400 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
        >
          <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M8 5v14l11-7z" />
          </svg>
        </button>
      </td>

      {/* Delete */}
      <td className="px-3 py-2 text-right">
        {movie.job_id && (
          <button
            onClick={() => onDelete(movie.job_id!)}
            className="rounded p-1.5 text-gray-600 hover:bg-red-900/40 hover:text-red-400 transition-colors"
            title="Удалить"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          </button>
        )}
      </td>
    </tr>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

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

  return (
    <div className="min-h-screen">
      {playingMovie && <PlayerModal movie={playingMovie} playerUrl={playerUrl} onClose={() => setPlayingMovie(null)} />}
      <Nav />

      <main className="px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
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
          <div className="overflow-hidden rounded-md border border-gray-800">
            <table className="w-full">
              <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-500">
                <tr>
                  <th className="px-3 py-2 w-14" />
                  <th className="px-3 py-2">ID</th>
                  <th className="px-3 py-2">IMDb</th>
                  <th className="px-3 py-2">TMDB</th>
                  <th className="px-3 py-2">Название</th>
                  <th className="px-3 py-2">Год</th>
                  <th className="px-3 py-2">Добавлен</th>
                  <th className="px-3 py-2">Субтитры</th>
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
