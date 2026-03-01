'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import useSWR from 'swr'
import { getToken, clearToken, fetcher, searchUrl, createJob, formatBytes } from '@/lib/api'
import type { SearchResponse, SearchResultItem } from '@/types'

export default function SearchPage() {
  const router = useRouter()
  const [query, setQuery] = useState('')
  const [submittedQuery, setSubmittedQuery] = useState('')
  const [enqueuing, setEnqueuing] = useState<string | null>(null)
  const [enqueueResult, setEnqueueResult] = useState<{ id: string; title: string } | null>(null)
  const [enqueueError, setEnqueueError] = useState('')

  useEffect(() => {
    if (!getToken()) router.replace('/login')
  }, [router])

  const { data, error, isLoading } = useSWR<SearchResponse>(
    submittedQuery ? searchUrl(submittedQuery, 'movie', 50) : null,
    fetcher,
    { revalidateOnFocus: false },
  )

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    const q = query.trim()
    if (q) setSubmittedQuery(q)
  }

  async function handleEnqueue(item: SearchResultItem) {
    setEnqueuing(item.external_id)
    setEnqueueError('')
    setEnqueueResult(null)
    try {
      const job = await createJob({
        source_ref: item.source_ref,
        source_type: 'torrent',
        content_type: 'movie',
      })
      setEnqueueResult({ id: job.job_id, title: item.title })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to create job'
      setEnqueueError(msg)
    } finally {
      setEnqueuing(null)
    }
  }

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
            <Link href="/search" className="text-sm text-indigo-400 hover:text-indigo-300">
              Search
            </Link>
            <Link href="/jobs" className="text-sm text-gray-400 hover:text-gray-200">
              Jobs
            </Link>
          </div>
          <button
            onClick={handleLogout}
            className="text-sm text-gray-400 hover:text-gray-200"
          >
            Logout
          </button>
        </div>
      </nav>

      <main className="mx-auto max-w-6xl px-6 py-8">
        <h2 className="mb-6 text-xl font-semibold text-white">Search</h2>

        {/* Search form */}
        <form onSubmit={handleSearch} className="mb-6 flex gap-3">
          <input
            type="text"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Movie title…"
            className="flex-1 rounded-md border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white placeholder-gray-500 focus:border-indigo-500 focus:outline-none"
          />
          <button
            type="submit"
            disabled={!query.trim() || isLoading}
            className="rounded-md bg-indigo-600 px-5 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
          >
            {isLoading ? 'Searching…' : 'Search'}
          </button>
        </form>

        {/* Enqueue feedback */}
        {enqueueResult && (
          <div className="mb-4 rounded-md border border-green-800 bg-green-950 px-4 py-3 text-sm text-green-400">
            Job created:{' '}
            <Link
              href={`/jobs/${enqueueResult.id}`}
              className="underline hover:text-green-300"
            >
              {enqueueResult.id}
            </Link>{' '}
            — {enqueueResult.title}
          </div>
        )}
        {enqueueError && (
          <div className="mb-4 rounded-md border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-400">
            {enqueueError}
          </div>
        )}

        {/* Search error */}
        {error && (
          <div className="rounded-md border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-400">
            {error instanceof Error ? error.message : 'Search failed'}
          </div>
        )}

        {/* Results */}
        {data && data.items.length === 0 && (
          <p className="text-sm text-gray-500">No results found for &quot;{submittedQuery}&quot;.</p>
        )}

        {data && data.items.length > 0 && (
          <div className="overflow-x-auto rounded-md border border-gray-800">
            <table className="w-full text-sm">
              <thead className="bg-gray-900 text-left text-xs uppercase tracking-wider text-gray-400">
                <tr>
                  <th className="px-4 py-3">Title</th>
                  <th className="px-4 py-3">Indexer</th>
                  <th className="px-4 py-3 text-right">Seeders</th>
                  <th className="px-4 py-3 text-right">Size</th>
                  <th className="px-4 py-3"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800">
                {data.items.map(item => (
                  <tr key={item.external_id} className="hover:bg-gray-900/50">
                    <td className="max-w-xs truncate px-4 py-3 font-mono text-xs text-gray-200">
                      {item.title}
                    </td>
                    <td className="px-4 py-3 text-gray-400">{item.indexer}</td>
                    <td className="px-4 py-3 text-right tabular-nums text-green-400">
                      {item.seeders}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-gray-400">
                      {formatBytes(item.size_bytes)}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={() => handleEnqueue(item)}
                        disabled={enqueuing === item.external_id}
                        className="rounded bg-indigo-700 px-3 py-1 text-xs font-semibold text-white hover:bg-indigo-600 disabled:opacity-50"
                      >
                        {enqueuing === item.external_id ? '…' : 'Download'}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="border-t border-gray-800 px-4 py-2 text-xs text-gray-500">
              {data.total} result{data.total !== 1 ? 's' : ''}
            </div>
          </div>
        )}
      </main>
    </div>
  )
}
