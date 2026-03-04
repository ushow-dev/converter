'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { getToken, fetcher, jobsUrl, formatDate, deleteJob } from '@/lib/api'
import { Nav } from '@/components/Nav'
import { useSWRConfig } from 'swr'
import type { Job, JobsResponse, JobStatus } from '@/types'

// ── helpers ──────────────────────────────────────────────────────────────────

function parseTitle(job: Job): string {
  if (job.title) return job.title
  if (job.source_ref?.startsWith('magnet:')) {
    try {
      const dn = new URL(job.source_ref).searchParams.get('dn')
      if (dn) return decodeURIComponent(dn.replace(/\+/g, ' '))
    } catch { /* ignore */ }
  }
  return job.job_id
}

function thumbnailSrc(jobId: string): string {
  const token = getToken()
  return `/api/admin/jobs/${jobId}/thumbnail${token ? `?token=${encodeURIComponent(token)}` : ''}`
}

// ── Status badge ──────────────────────────────────────────────────────────────

const STATUS_CFG: Record<JobStatus, { label: string; cls: string }> = {
  queued:      { label: 'В очереди', cls: 'border-yellow-500/40 bg-yellow-500/15 text-yellow-300' },
  in_progress: { label: 'Обработка', cls: 'border-blue-500/40 bg-blue-500/15 text-blue-300'   },
  completed:   { label: 'Готово',    cls: 'border-green-500/40 bg-green-500/15 text-green-300' },
  failed:      { label: 'Ошибка',    cls: 'border-red-500/40  bg-red-500/15  text-red-400'    },
}

function StatusBadge({ status }: { status: JobStatus }) {
  const cfg = STATUS_CFG[status] ?? { label: status, cls: 'border-gray-600 bg-gray-800 text-gray-300' }
  return (
    <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${cfg.cls}`}>
      {cfg.label}
    </span>
  )
}

// ── Thumbnail cell ────────────────────────────────────────────────────────────

function Thumbnail({ job }: { job: Job }) {
  if (job.thumbnail_path) {
    return (
      <div className="relative h-16 w-11 shrink-0 overflow-hidden rounded bg-gray-800">
        <img
          src={thumbnailSrc(job.job_id)}
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
      {job.status === 'in_progress' ? (
        <div className="h-5 w-5 animate-spin rounded-full border-2 border-gray-700 border-t-indigo-400" />
      ) : (
        <FilmIcon />
      )}
    </div>
  )
}

function FilmIcon() {
  return (
    <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M7 4v16M17 4v16M3 8h4m10 0h4M3 12h18M3 16h4m10 0h4M4 20h16a1 1 0 001-1V5a1 1 0 00-1-1H4a1 1 0 00-1 1v14a1 1 0 001 1z" />
    </svg>
  )
}

// ── Table row ─────────────────────────────────────────────────────────────────

function MovieRow({ job, onDelete }: { job: Job; onDelete: (id: string) => void }) {
  const title = parseTitle(job)
  const stageName = job.stage === 'download' ? 'Скачивание' : job.stage === 'convert' ? 'Конвертация' : null

  return (
    <tr className="group border-b border-gray-800 hover:bg-gray-900/60">
      {/* Poster */}
      <td className="w-14 px-3 py-2">
        <Link href={`/jobs/${job.job_id}`}>
          <Thumbnail job={job} />
        </Link>
      </td>

      {/* Movie ID */}
      <td className="whitespace-nowrap px-3 py-2 text-xs text-gray-400">
        {job.movie_id ?? '—'}
      </td>

      {/* IMDb */}
      <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-gray-400">
        {job.imdb_id ?? '—'}
      </td>

      {/* TMDB */}
      <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-gray-400">
        {job.tmdb_id ?? '—'}
      </td>

      {/* Title */}
      <td className="px-3 py-2">
        <Link
          href={`/jobs/${job.job_id}`}
          className="block max-w-lg truncate text-sm font-medium text-gray-100 hover:text-white"
          title={title}
        >
          {title}
        </Link>
      </td>

      {/* Status */}
      <td className="whitespace-nowrap px-3 py-2">
        <StatusBadge status={job.status} />
      </td>

      {/* Stage / progress */}
      <td className="whitespace-nowrap px-3 py-2 text-xs text-gray-500">
        {job.status === 'in_progress' ? (
          <div className="flex items-center gap-2">
            <span className="text-gray-400">{stageName ?? '—'}</span>
            <div className="h-1.5 w-20 overflow-hidden rounded-full bg-gray-800">
              <div
                className="h-full rounded-full bg-indigo-500 transition-all duration-500"
                style={{ width: `${job.progress_percent}%` }}
              />
            </div>
            <span className="tabular-nums text-gray-500">{job.progress_percent}%</span>
          </div>
        ) : (
          <span>—</span>
        )}
      </td>

      {/* Date */}
      <td className="whitespace-nowrap px-3 py-2 text-xs text-gray-500">
        {formatDate(job.created_at)}
      </td>

      {/* Delete */}
      <td className="px-3 py-2 text-right">
        <button
          onClick={() => onDelete(job.job_id)}
          className="rounded p-1.5 text-gray-600 hover:bg-red-900/40 hover:text-red-400 transition-colors"
          title="Удалить"
        >
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
              d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
          </svg>
        </button>
      </td>
    </tr>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function MoviesPage() {
  const router = useRouter()
  const { mutate } = useSWRConfig()
  const swrKey = jobsUrl(undefined, 100)

  useEffect(() => {
    if (!getToken()) router.replace('/login')
  }, [router])

  const { data, error, isLoading } = useSWR<JobsResponse>(
    swrKey,
    fetcher,
    { refreshInterval: 5000 },
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

  return (
    <div className="min-h-screen">
      <Nav />

      <main className="px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-xl font-semibold text-white">Фильмы</h1>
          <Link
            href="/search"
            className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
          >
            + Добавить
          </Link>
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
              href="/search"
              className="rounded-md bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-indigo-500"
            >
              Найти фильм
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
                  <th className="px-3 py-2">Статус</th>
                  <th className="px-3 py-2">Прогресс</th>
                  <th className="px-3 py-2">Добавлен</th>
                  <th className="px-3 py-2 w-10" />
                </tr>
              </thead>
              <tbody>
                {data.items.map(job => (
                  <MovieRow key={job.job_id} job={job} onDelete={handleDelete} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </main>
    </div>
  )
}
