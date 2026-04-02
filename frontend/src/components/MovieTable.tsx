'use client'

import { useEffect, useState } from 'react'
import { movieThumbnailSrc, formatDate, getMovieSubtitles, searchSubtitles } from '@/lib/api'
import type { Movie, Subtitle } from '@/types'

// ── Thumbnail cell ────────────────────────────────────────────────────────────

export function FilmIcon() {
  return (
    <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M7 4v16M17 4v16M3 8h4m10 0h4M3 12h18M3 16h4m10 0h4M4 20h16a1 1 0 001-1V5a1 1 0 00-1-1H4a1 1 0 00-1 1v14a1 1 0 001 1z" />
    </svg>
  )
}

export function Thumbnail({ movie }: { movie: Movie }) {
  if (movie.has_thumbnail) {
    return (
      <div className="relative h-16 w-11 shrink-0 overflow-hidden rounded bg-gray-800">
        <img
          src={movieThumbnailSrc(movie)}
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

export function SubtitleCell({ movie }: { movie: Movie }) {
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

export function EditableID({
  value,
  placeholder,
  width,
  btnClassName,
  onSave,
}: {
  value: string | undefined
  placeholder: string
  width: string
  btnClassName?: string
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
      className={`font-mono text-xs text-gray-400 hover:text-indigo-400 transition-colors${btnClassName ? ' ' + btnClassName : ''}`}
    >
      {value ?? <span className="text-gray-700">—</span>}
    </button>
  )
}

// ── Table row ─────────────────────────────────────────────────────────────────

export function MovieRow({
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
      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-2 text-xs text-gray-400">
        {movie.id}
      </td>

      {/* IMDb */}
      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-2">
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
      <td className="px-3 py-2 max-w-[8rem] sm:max-w-none overflow-hidden">
        <EditableID
          value={movie.title}
          placeholder="Название фильма"
          width="w-72"
          btnClassName="block truncate max-w-full"
          onSave={v => onUpdate(movie.id, movie.imdb_id ?? '', movie.tmdb_id ?? '', v)}
        />
        {!movie.title && (
          <span className="mt-0.5 block font-mono text-[10px] text-gray-700">{movie.storage_key}</span>
        )}
      </td>

      {/* Year */}
      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-2 text-sm text-gray-400">
        {movie.year ?? <span className="text-gray-700">—</span>}
      </td>

      {/* Date */}
      <td className="hidden sm:table-cell whitespace-nowrap px-3 py-2 text-xs text-gray-500">
        {formatDate(movie.created_at)}
      </td>

      {/* Subtitles */}
      <td className="hidden sm:table-cell px-3 py-2">
        <SubtitleCell movie={movie} />
      </td>

      {/* Play + TMDB */}
      <td className="px-2 py-2">
        <div className="flex items-center gap-1">
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
          {movie.tmdb_id && (
            <a
              href={`https://www.themoviedb.org/movie/${movie.tmdb_id}`}
              target="_blank"
              rel="noopener noreferrer"
              title="Посмотреть на TMDB"
              className="rounded p-1.5 text-gray-600 hover:bg-blue-900/40 hover:text-blue-400 transition-colors"
            >
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                  d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
              </svg>
            </a>
          )}
        </div>
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
