'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { getToken, fetcher, jobsUrl, formatDate } from '@/lib/api'
import { Nav } from '@/components/Nav'
import type { JobsResponse, JobStatus } from '@/types'

const STATUS_BADGE: Record<JobStatus, string> = {
  queued: 'bg-yellow-900 text-yellow-300',
  in_progress: 'bg-blue-900 text-blue-300',
  completed: 'bg-green-900 text-green-300',
  failed: 'bg-red-900 text-red-300',
}

export default function JobsPage() {
  const router = useRouter()

  useEffect(() => {
    if (!getToken()) router.replace('/login')
  }, [router])

  const { data, error, isLoading } = useSWR<JobsResponse>(
    jobsUrl(undefined, 50),
    fetcher,
    { refreshInterval: 5000 },
  )

  return (
    <div className="min-h-screen">
      <Nav />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
          <h2 className="text-xl font-semibold text-white">Jobs</h2>
          <span className="text-xs text-gray-500">Auto-refreshes every 5 s</span>
        </div>

        {isLoading && (
          <p className="text-sm text-gray-500">Loading…</p>
        )}

        {error && (
          <div className="rounded-md border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-400">
            {error instanceof Error ? error.message : 'Failed to load jobs'}
          </div>
        )}

        {data && data.items.length === 0 && (
          <p className="text-sm text-gray-500">No jobs yet. Go to <Link href="/search" className="text-indigo-400 underline">Search</Link> to start one.</p>
        )}

        {data && data.items.length > 0 && (
          <div className="overflow-x-auto rounded-md border border-gray-800">
            <table className="w-full text-sm">
              <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-400">
                <tr>
                  <th className="px-4 py-3">Job ID</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Stage</th>
                  <th className="px-4 py-3">Progress</th>
                  <th className="px-4 py-3">Updated</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800">
                {data.items.map(job => (
                  <tr key={job.job_id} className="hover:bg-gray-900/50">
                    <td className="px-4 py-3">
                      <Link
                        href={`/jobs/${job.job_id}`}
                        className="font-mono text-xs text-indigo-400 hover:text-indigo-300"
                      >
                        {job.job_id}
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`rounded px-2 py-0.5 text-xs font-medium ${STATUS_BADGE[job.status] ?? 'bg-gray-800 text-gray-300'}`}
                      >
                        {job.status}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-gray-400">{job.stage ?? '—'}</td>
                    <td className="px-4 py-3">
                      {job.status === 'in_progress' ? (
                        <div className="flex items-center gap-2">
                          <div className="h-1.5 w-24 overflow-hidden rounded-full bg-gray-800">
                            <div
                              className="h-full rounded-full bg-indigo-500 transition-all"
                              style={{ width: `${job.progress_percent}%` }}
                            />
                          </div>
                          <span className="tabular-nums text-xs text-gray-400">
                            {job.progress_percent}%
                          </span>
                        </div>
                      ) : (
                        <span className="text-xs text-gray-600">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500">{formatDate(job.updated_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </main>
    </div>
  )
}
