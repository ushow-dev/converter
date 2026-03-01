'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { getToken, clearToken, fetcher, formatDate } from '@/lib/api'
import type { Job, JobStatus } from '@/types'

const STATUS_BADGE: Record<JobStatus, string> = {
  queued: 'bg-yellow-900 text-yellow-300',
  in_progress: 'bg-blue-900 text-blue-300',
  completed: 'bg-green-900 text-green-300',
  failed: 'bg-red-900 text-red-300',
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <tr className="border-b border-gray-800">
      <td className="py-3 pr-6 text-sm text-gray-400">{label}</td>
      <td className="py-3 text-sm text-gray-100">{value}</td>
    </tr>
  )
}

export default function JobDetailPage({ params }: { params: { jobId: string } }) {
  const router = useRouter()
  const { jobId } = params

  useEffect(() => {
    if (!getToken()) router.replace('/login')
  }, [router])

  const { data: job, error, isLoading } = useSWR<Job>(
    jobId ? `/api/admin/jobs/${jobId}` : null,
    fetcher,
    {
      refreshInterval: job =>
        job?.status === 'completed' || job?.status === 'failed' ? 0 : 3000,
    },
  )

  function handleLogout() {
    clearToken()
    router.push('/login')
  }

  return (
    <div className="min-h-screen">
      {/* Nav */}
      <nav className="border-b border-gray-800 bg-gray-900 px-6 py-3">
        <div className="mx-auto flex max-w-6xl items-center justify-between">
          <div className="flex items-center gap-6">
            <span className="font-semibold text-white">Media Admin</span>
            <Link href="/search" className="text-sm text-gray-400 hover:text-gray-200">
              Search
            </Link>
            <Link href="/jobs" className="text-sm text-gray-400 hover:text-gray-200">
              Jobs
            </Link>
          </div>
          <button onClick={handleLogout} className="text-sm text-gray-400 hover:text-gray-200">
            Logout
          </button>
        </div>
      </nav>

      <main className="mx-auto max-w-4xl px-6 py-8">
        <div className="mb-6">
          <Link href="/jobs" className="text-sm text-indigo-400 hover:text-indigo-300">
            ← Back to Jobs
          </Link>
        </div>

        <h2 className="mb-6 font-mono text-lg font-semibold text-white">{jobId}</h2>

        {isLoading && <p className="text-sm text-gray-500">Loading…</p>}

        {error && (
          <div className="rounded-md border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-400">
            {error instanceof Error ? error.message : 'Failed to load job'}
          </div>
        )}

        {job && (
          <div className="rounded-md border border-gray-800 bg-gray-900/50 px-6 py-2">
            <table className="w-full">
              <tbody>
                <Row
                  label="Status"
                  value={
                    <span
                      className={`rounded px-2 py-0.5 text-xs font-medium ${STATUS_BADGE[job.status] ?? 'bg-gray-800 text-gray-300'}`}
                    >
                      {job.status}
                    </span>
                  }
                />
                <Row label="Stage" value={job.stage ?? '—'} />
                <Row label="Content type" value={job.content_type} />
                <Row
                  label="Progress"
                  value={
                    <div className="flex items-center gap-3">
                      <div className="h-2 w-48 overflow-hidden rounded-full bg-gray-800">
                        <div
                          className="h-full rounded-full bg-indigo-500 transition-all"
                          style={{ width: `${job.progress_percent}%` }}
                        />
                      </div>
                      <span className="tabular-nums text-gray-300">
                        {job.progress_percent}%
                      </span>
                    </div>
                  }
                />
                {job.error_code && (
                  <Row
                    label="Error code"
                    value={
                      <span className="font-mono text-xs text-red-400">{job.error_code}</span>
                    }
                  />
                )}
                {job.error_message && (
                  <Row
                    label="Error message"
                    value={<span className="text-red-400">{job.error_message}</span>}
                  />
                )}
                {job.created_at && (
                  <Row label="Created" value={formatDate(job.created_at)} />
                )}
                <Row label="Updated" value={formatDate(job.updated_at)} />
              </tbody>
            </table>
          </div>
        )}

        {job?.status === 'in_progress' && (
          <p className="mt-4 text-xs text-gray-500">Auto-refreshing every 3 s…</p>
        )}
      </main>
    </div>
  )
}
