'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { useSWRConfig } from 'swr'
import { getToken, fetcher, jobsUrl, formatDate, deleteJob, getScannerDownloads, retryScannerDownload } from '@/lib/api'
import { Nav } from '@/components/Nav'
import type { Job, JobsResponse, JobStatus, ScannerDownload, ScannerDownloadsResponse } from '@/types'

// ── helpers ───────────────────────────────────────────────────────────────────

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

// ── Spinner cell ──────────────────────────────────────────────────────────────

function FilmIcon() {
  return (
    <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M7 4v16M17 4v16M3 8h4m10 0h4M3 12h18M3 16h4m10 0h4M4 20h16a1 1 0 001-1V5a1 1 0 00-1-1H4a1 1 0 00-1 1v14a1 1 0 001 1z" />
    </svg>
  )
}

function JobIcon({ job }: { job: Job }) {
  return (
    <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-gray-800/60">
      {job.status === 'in_progress' ? (
        <div className="h-5 w-5 animate-spin rounded-full border-2 border-gray-700 border-t-indigo-400" />
      ) : job.status === 'failed' ? (
        <svg className="h-5 w-5 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 9v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
        </svg>
      ) : (
        <FilmIcon />
      )}
    </div>
  )
}

// ── Table row ─────────────────────────────────────────────────────────────────

function QueueRow({ job, onDelete }: { job: Job; onDelete: (id: string) => void }) {
  const title = parseTitle(job)
  const stageName = job.stage === 'download' ? 'Скачивание' : job.stage === 'convert' ? 'Конвертация' : job.stage === 'transfer' ? 'Перенос' : null

  return (
    <tr className="group border-b border-gray-800 hover:bg-gray-900/60">
      {/* Icon */}
      <td className="w-12 px-3 py-3">
        <Link href={`/jobs/${job.job_id}`}>
          <JobIcon job={job} />
        </Link>
      </td>

      {/* Title */}
      <td className="px-3 py-3">
        <Link
          href={`/jobs/${job.job_id}`}
          className="block max-w-lg truncate text-sm font-medium text-gray-100 hover:text-white"
          title={title}
        >
          {title}
        </Link>
        {job.imdb_id && (
          <span className="mt-0.5 block font-mono text-[10px] text-gray-600">{job.imdb_id}</span>
        )}
      </td>

      {/* Status */}
      <td className="whitespace-nowrap px-3 py-3">
        <StatusBadge status={job.status} />
      </td>

      {/* Stage / progress */}
      <td className="whitespace-nowrap px-3 py-3 text-xs">
        {job.status === 'in_progress' ? (
          <div className="flex items-center gap-2">
            <span className="text-gray-400">{stageName ?? '—'}</span>
            <div className="h-1.5 w-24 overflow-hidden rounded-full bg-gray-800">
              <div
                className="h-full rounded-full bg-indigo-500 transition-all duration-500"
                style={{ width: `${job.progress_percent}%` }}
              />
            </div>
            <span className="tabular-nums text-gray-500">{job.progress_percent}%</span>
          </div>
        ) : job.status === 'failed' && job.error_message ? (
          <span className="max-w-xs truncate text-red-400" title={job.error_message}>
            {job.error_message}
          </span>
        ) : (
          <span className="text-gray-600">—</span>
        )}
      </td>

      {/* Date */}
      <td className="whitespace-nowrap px-3 py-3 text-xs text-gray-500">
        {formatDate(job.created_at)}
      </td>

      {/* Delete */}
      <td className="px-3 py-3 text-right">
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

// ── Scanner downloads ─────────────────────────────────────────────────────────

const SCANNER_STATUS_CFG: Record<string, { label: string; cls: string }> = {
  queued:      { label: 'Очередь',      cls: 'border-yellow-500/40 bg-yellow-500/15 text-yellow-300' },
  downloading: { label: 'Скачивание',   cls: 'border-blue-500/40 bg-blue-500/15 text-blue-300'      },
  done:        { label: 'Готово',        cls: 'border-green-500/40 bg-green-500/15 text-green-300'   },
  failed:      { label: 'Ошибка',        cls: 'border-red-500/40  bg-red-500/15  text-red-400'       },
}

function ScannerStatusBadge({ status }: { status: string }) {
  const cfg = SCANNER_STATUS_CFG[status] ?? { label: status, cls: 'border-gray-600 bg-gray-800 text-gray-300' }
  return (
    <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${cfg.cls}`}>
      {cfg.label}
    </span>
  )
}

function ScannerDownloadRow({ item, onRetry }: { item: ScannerDownload; onRetry: (id: number) => void }) {
  return (
    <tr className="group border-b border-gray-800 hover:bg-gray-900/60">
      <td className="w-12 px-3 py-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-gray-800/60">
          {item.status === 'downloading' ? (
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-gray-700 border-t-indigo-400" />
          ) : item.status === 'failed' ? (
            <svg className="h-5 w-5 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 9v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
            </svg>
          ) : (
            <svg className="h-5 w-5 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
            </svg>
          )}
        </div>
      </td>
      <td className="px-3 py-3">
        <span className="block max-w-lg truncate text-sm font-medium text-gray-100" title={item.filename}>
          {item.filename}
        </span>
        <span className="mt-0.5 block font-mono text-[10px] text-gray-600 truncate max-w-lg" title={item.url}>
          {item.url}
        </span>
      </td>
      <td className="whitespace-nowrap px-3 py-3">
        <ScannerStatusBadge status={item.status} />
      </td>
      <td className="px-3 py-3 text-xs">
        {item.status === 'failed' && item.error_message ? (
          <span className="max-w-xs truncate text-red-400" title={item.error_message}>
            {item.error_message}
          </span>
        ) : (
          <span className="text-gray-600">—</span>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-3 text-xs text-gray-500">
        {formatDate(item.created_at)}
      </td>
      <td className="px-3 py-3 text-right">
        {item.status === 'failed' && (
          <button
            onClick={() => onRetry(item.id)}
            className="rounded p-1.5 text-gray-600 hover:bg-indigo-900/40 hover:text-indigo-400 transition-colors"
            title="Повторить"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          </button>
        )}
      </td>
    </tr>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function QueuePage() {
  const router = useRouter()
  const { mutate } = useSWRConfig()
  const swrKey = jobsUrl('active', 100)

  useEffect(() => {
    if (!getToken()) router.replace('/login')
  }, [router])

  const { data, error, isLoading } = useSWR<JobsResponse>(
    swrKey,
    fetcher,
    { refreshInterval: 3000 },
  )

  const scannerKey = '/api/admin/scanner/downloads'
  const { data: scannerData, mutate: mutateScannerDownloads } = useSWR<ScannerDownloadsResponse>(
    scannerKey,
    () => getScannerDownloads(),
    { refreshInterval: 5000 },
  )

  const activeJobs = data?.items ?? []
  const scannerDownloads = (scannerData?.items ?? []).filter(
    d => d.status !== 'done',
  )

  async function handleDelete(jobId: string) {
    if (!window.confirm('Удалить задачу?')) return
    try {
      await deleteJob(jobId)
      mutate(swrKey)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при удалении')
    }
  }

  async function handleRetryDownload(id: number) {
    try {
      await retryScannerDownload(id)
      mutateScannerDownloads()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Ошибка при повторе')
    }
  }

  return (
    <div className="min-h-screen">
      <Nav />

      <main className="px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-semibold text-white">В работе</h1>
            {activeJobs.length > 0 && (
              <span className="rounded-full bg-indigo-600/30 px-2 py-0.5 text-xs font-semibold text-indigo-300">
                {activeJobs.length}
              </span>
            )}
          </div>
          <span className="text-xs text-gray-600">Обновляется каждые 3 с</span>
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

        {data && activeJobs.length === 0 && (
          <div className="flex flex-col items-center gap-3 py-24 text-center">
            <svg className="h-8 w-8 text-gray-700" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <p className="text-gray-400">Нет активных задач</p>
            <Link
              href="/search"
              className="mt-1 rounded-md bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-indigo-500"
            >
              Добавить фильм
            </Link>
          </div>
        )}

        {activeJobs.length > 0 && (
          <div className="overflow-hidden rounded-md border border-gray-800">
            <table className="w-full">
              <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-500">
                <tr>
                  <th className="px-3 py-2 w-12" />
                  <th className="px-3 py-2">Название</th>
                  <th className="px-3 py-2">Статус</th>
                  <th className="px-3 py-2">Прогресс</th>
                  <th className="px-3 py-2">Добавлен</th>
                  <th className="px-3 py-2 w-10" />
                </tr>
              </thead>
              <tbody>
                {activeJobs.map(job => (
                  <QueueRow key={job.job_id} job={job} onDelete={handleDelete} />
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* ── Scanner downloads section ─────────────────────────────────── */}
        {scannerDownloads.length > 0 && (
          <div className="mt-8">
            <div className="mb-4 flex items-center gap-3">
              <h2 className="text-base font-semibold text-white">Загрузки сканера</h2>
              <span className="rounded-full bg-indigo-600/30 px-2 py-0.5 text-xs font-semibold text-indigo-300">
                {scannerDownloads.length}
              </span>
            </div>
            <div className="overflow-hidden rounded-md border border-gray-800">
              <table className="w-full">
                <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-500">
                  <tr>
                    <th className="px-3 py-2 w-12" />
                    <th className="px-3 py-2">Файл</th>
                    <th className="px-3 py-2">Статус</th>
                    <th className="px-3 py-2">Ошибка</th>
                    <th className="px-3 py-2">Добавлен</th>
                    <th className="px-3 py-2 w-10" />
                  </tr>
                </thead>
                <tbody>
                  {scannerDownloads.map(item => (
                    <ScannerDownloadRow key={item.id} item={item} onRetry={handleRetryDownload} />
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </main>
    </div>
  )
}
