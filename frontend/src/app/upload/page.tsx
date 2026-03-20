'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Nav } from '@/components/Nav'
import { browseRemoteUrl, createRemoteDownloadJob } from '@/lib/api'
import type { RemoteMovie, DownloadItem, ProxyConfig } from '@/types'

export default function UploadPage() {
  const router = useRouter()

  // ── Remote browse state ───────────────────────────────────────────────────
  const [remoteUrl, setRemoteUrl] = useState('')
  const [browsing, setBrowsing] = useState(false)
  const [browseError, setBrowseError] = useState('')
  const [remoteMovies, setRemoteMovies] = useState<RemoteMovie[]>([])
  const [browseTotal, setBrowseTotal] = useState(0)
  const [browseOffset, setBrowseOffset] = useState(0)
  const [browseHasMore, setBrowseHasMore] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [downloadItems, setDownloadItems] = useState<Map<string, DownloadItem>>(new Map())
  const [downloading, setDownloading] = useState(false)

  // ── Proxy state ───────────────────────────────────────────────────────────
  const [proxyEnabled, setProxyEnabled] = useState(false)
  const [proxyHost, setProxyHost] = useState('')
  const [proxyPort, setProxyPort] = useState('')
  const [proxyType, setProxyType] = useState<'SOCKS5' | 'HTTP'>('SOCKS5')
  const [proxyUser, setProxyUser] = useState('')
  const [proxyPass, setProxyPass] = useState('')

  // Load proxy settings from localStorage on mount
  useEffect(() => {
    try {
      const saved = localStorage.getItem('proxySettings')
      if (saved) {
        const p = JSON.parse(saved)
        if (p.enabled !== undefined) setProxyEnabled(p.enabled)
        if (p.host) setProxyHost(p.host)
        if (p.port) setProxyPort(p.port)
        if (p.type === 'SOCKS5' || p.type === 'HTTP') setProxyType(p.type)
        if (p.username) setProxyUser(p.username)
        if (p.password) setProxyPass(p.password)
      }
    } catch {}
  }, [])

  // Save proxy settings to localStorage whenever they change
  useEffect(() => {
    try {
      localStorage.setItem('proxySettings', JSON.stringify({
        enabled: proxyEnabled, host: proxyHost, port: proxyPort,
        type: proxyType, username: proxyUser, password: proxyPass,
      }))
    } catch {}
  }, [proxyEnabled, proxyHost, proxyPort, proxyType, proxyUser, proxyPass])

  function buildProxyConfig(): ProxyConfig | undefined {
    if (!proxyEnabled) return undefined
    return { enabled: true, host: proxyHost, port: parseInt(proxyPort, 10) || 0, type: proxyType,
             username: proxyUser, password: proxyPass }
  }

  async function handleBrowse() {
    if (!remoteUrl.trim()) return
    setBrowsing(true)
    setBrowseError('')
    setRemoteMovies([])
    setBrowseTotal(0)
    setBrowseOffset(0)
    setBrowseHasMore(false)
    setSelected(new Set())
    setDownloadItems(new Map())
    try {
      const resp = await browseRemoteUrl(remoteUrl.trim(), buildProxyConfig(), 0)
      setRemoteMovies(resp.items)
      setBrowseTotal(resp.total)
      setBrowseOffset(resp.items.length)
      setBrowseHasMore(resp.has_more)
    } catch (err: unknown) {
      setBrowseError(err instanceof Error ? err.message : 'Ошибка обзора')
    } finally {
      setBrowsing(false)
    }
  }

  async function handleLoadMore() {
    if (browsing || !browseHasMore) return
    setBrowsing(true)
    setBrowseError('')
    try {
      const resp = await browseRemoteUrl(remoteUrl.trim(), buildProxyConfig(), browseOffset)
      setRemoteMovies((prev) => [...prev, ...resp.items])
      setBrowseTotal(resp.total)
      setBrowseOffset((prev) => prev + resp.items.length)
      setBrowseHasMore(resp.has_more)
    } catch (err: unknown) {
      setBrowseError(err instanceof Error ? err.message : 'Ошибка загрузки')
    } finally {
      setBrowsing(false)
    }
  }

  function toggleAll() {
    if (selected.size === remoteMovies.filter((m) => m.video_file).length) {
      setSelected(new Set())
    } else {
      setSelected(new Set(remoteMovies.filter((m) => m.video_file).map((m) => m.url)))
    }
  }

  function toggleOne(url: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(url)) next.delete(url)
      else next.add(url)
      return next
    })
  }

  async function handleDownloadSelected() {
    const toDownload = remoteMovies.filter((m) => m.video_file && selected.has(m.url))
    if (toDownload.length === 0) return
    setDownloading(true)

    // Set all selected to 'submitting'
    setDownloadItems((prev) => {
      const next = new Map(prev)
      for (const m of toDownload) next.set(m.url, { movie: m, state: 'submitting' })
      return next
    })

    // Submit all in parallel
    await Promise.all(toDownload.map(async (movie) => {
      try {
        const resp = await createRemoteDownloadJob(movie.video_file!.url, movie.video_file!.name, buildProxyConfig())
        setDownloadItems((prev) => new Map(prev).set(movie.url, {
          movie,
          state: 'queued',
          jobId: resp.job_id,
        }))
      } catch (err: unknown) {
        const fetchErr = err as { code?: string; data?: Record<string, unknown> }
        if (fetchErr.code === 'DUPLICATE') {
          setDownloadItems((prev) => new Map(prev).set(movie.url, {
            movie,
            state: 'duplicate',
            movieId: fetchErr.data?.movie_id as number | undefined,
            movieTitle: fetchErr.data?.title as string | undefined,
          }))
        } else {
          setDownloadItems((prev) => new Map(prev).set(movie.url, {
            movie, state: 'error', error: err instanceof Error ? err.message : 'Ошибка',
          }))
        }
      }
    }))

    setDownloading(false)
    setSelected(new Set())
  }

  const selectableMovies = remoteMovies.filter((m) => m.video_file)
  const allChecked = selectableMovies.length > 0 && selected.size === selectableMovies.length
  const queuedCount = Array.from(downloadItems.values()).filter((d) => d.state === 'queued').length

  return (
    <div className="min-h-screen bg-gray-950 text-gray-100">
      <Nav />
      <main className="w-full px-4 py-8">
        <h1 className="text-2xl font-bold mb-6">Добавить фильм</h1>

        <div className="space-y-5">
          {/* URL input */}
          <div className="flex gap-2">
            <input
              type="url"
              value={remoteUrl}
              onChange={(e) => setRemoteUrl(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleBrowse()}
              placeholder="http://example.com/movies/"
              className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-indigo-500"
            />
            <button
              onClick={handleBrowse}
              disabled={browsing || !remoteUrl.trim()}
              className="px-4 py-2 bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 rounded text-sm font-medium whitespace-nowrap"
            >
              {browsing ? 'Загрузка…' : 'Обзор'}
            </button>
          </div>

          {/* Proxy settings */}
          <div className="rounded-lg border border-gray-800 bg-gray-900/50 px-4 py-3 space-y-3">
            <label className="flex items-center gap-2 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={proxyEnabled}
                onChange={(e) => setProxyEnabled(e.target.checked)}
                className="rounded border-gray-600 bg-gray-700 text-indigo-500 focus:ring-indigo-500"
              />
              <span className="text-sm font-medium text-gray-300">Использовать прокси</span>
            </label>
            {proxyEnabled && (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="sm:col-span-2 flex gap-2">
                  <div className="flex-1">
                    <label className="block text-xs text-gray-500 mb-1">Хост</label>
                    <input
                      type="text"
                      value={proxyHost}
                      onChange={(e) => setProxyHost(e.target.value)}
                      placeholder="103.163.247.44"
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm focus:outline-none focus:border-indigo-500"
                    />
                  </div>
                  <div className="w-24">
                    <label className="block text-xs text-gray-500 mb-1">Порт</label>
                    <input
                      type="text"
                      value={proxyPort}
                      onChange={(e) => setProxyPort(e.target.value)}
                      placeholder="1080"
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm focus:outline-none focus:border-indigo-500"
                    />
                  </div>
                  <div className="w-28">
                    <label className="block text-xs text-gray-500 mb-1">Тип</label>
                    <select
                      value={proxyType}
                      onChange={(e) => setProxyType(e.target.value as 'SOCKS5' | 'HTTP')}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm focus:outline-none focus:border-indigo-500"
                    >
                      <option value="SOCKS5">SOCKS5</option>
                      <option value="HTTP">HTTP</option>
                    </select>
                  </div>
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Логин</label>
                  <input
                    type="text"
                    value={proxyUser}
                    onChange={(e) => setProxyUser(e.target.value)}
                    placeholder="proxyuser"
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm focus:outline-none focus:border-indigo-500"
                  />
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Пароль</label>
                  <input
                    type="password"
                    value={proxyPass}
                    onChange={(e) => setProxyPass(e.target.value)}
                    placeholder="••••••••"
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm focus:outline-none focus:border-indigo-500"
                  />
                </div>
              </div>
            )}
          </div>

          {browseError && (
            <p className="text-sm text-red-400">{browseError}</p>
          )}

          {browsing && (
            <p className="text-sm text-gray-400">Сканирование каталога…</p>
          )}

          {/* Results table */}
          {remoteMovies.length > 0 && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <p className="text-sm text-gray-400">
                  Показано: <span className="text-white font-medium">{remoteMovies.length}</span>
                  {browseTotal > remoteMovies.length && (
                    <span className="text-gray-500"> из {browseTotal}</span>
                  )}
                  {selected.size > 0 && (
                    <span className="ml-2 text-indigo-400">({selected.size} выбрано)</span>
                  )}
                  {queuedCount > 0 && (
                    <span className="ml-2 text-green-400">✓ {queuedCount} в очереди</span>
                  )}
                </p>
                <div className="flex gap-2">
                  {queuedCount > 0 && (
                    <button
                      onClick={() => router.push('/queue')}
                      className="px-4 py-1.5 bg-green-700 hover:bg-green-600 rounded text-sm font-medium"
                    >
                      Открыть задания
                    </button>
                  )}
                  <button
                    onClick={handleDownloadSelected}
                    disabled={selected.size === 0 || downloading}
                    className="px-4 py-1.5 bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed rounded text-sm font-medium"
                  >
                    {downloading ? 'Добавление…' : `Скачать (${selected.size})`}
                  </button>
                </div>
              </div>

              <div className="overflow-x-auto rounded-lg border border-gray-800">
                <table className="w-full text-sm">
                  <thead className="bg-gray-800 text-gray-400 text-xs uppercase tracking-wide">
                    <tr>
                      <th className="px-3 py-2 w-8">
                        <input
                          type="checkbox"
                          checked={allChecked}
                          onChange={toggleAll}
                          className="rounded border-gray-600 bg-gray-700 text-indigo-500 focus:ring-indigo-500"
                        />
                      </th>
                      <th className="px-3 py-2 text-left">Название</th>
                      <th className="px-3 py-2 text-right">Размер</th>
                      <th className="px-3 py-2 text-left">Субтитры</th>
                      <th className="px-3 py-2 text-left">Статус</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-800">
                    {remoteMovies.map((movie) => {
                      const dlItem = downloadItems.get(movie.url)
                      const isSelectable = !!movie.video_file && dlItem?.state !== 'duplicate'
                      const isSelected = selected.has(movie.url)
                      return (
                        <tr
                          key={movie.url}
                          className={`transition-colors ${
                            isSelectable ? 'hover:bg-gray-800/50 cursor-pointer' : 'opacity-50'
                          } ${isSelected ? 'bg-indigo-950/30' : ''}`}
                          onClick={() => isSelectable && toggleOne(movie.url)}
                        >
                          <td className="px-3 py-2 w-8" onClick={(e) => e.stopPropagation()}>
                            <input
                              type="checkbox"
                              checked={isSelected}
                              disabled={!isSelectable || !!dlItem}
                              onChange={() => toggleOne(movie.url)}
                              className="rounded border-gray-600 bg-gray-700 text-indigo-500 focus:ring-indigo-500 disabled:opacity-40"
                            />
                          </td>
                          <td className="px-3 py-2 font-medium text-white">
                            {movie.video_file ? movie.video_file.name : (
                              <span className="text-gray-400 italic">{movie.name}</span>
                            )}
                          </td>
                          <td className="px-3 py-2 text-right text-gray-400 whitespace-nowrap">
                            {movie.video_file?.size || '—'}
                          </td>
                          <td className="px-3 py-2">
                            {movie.subtitle_files.length > 0 ? (
                              <span className="inline-flex items-center gap-1 text-xs text-green-400">
                                <span className="w-1.5 h-1.5 rounded-full bg-green-400 inline-block" />
                                {movie.subtitle_files.length} SRT
                              </span>
                            ) : (
                              <span className="text-gray-600 text-xs">—</span>
                            )}
                          </td>
                          <td className="px-3 py-2 whitespace-nowrap">
                            {!dlItem && <span className="text-gray-600 text-xs">—</span>}
                            {dlItem?.state === 'submitting' && (
                              <span className="text-xs text-indigo-400 animate-pulse">Добавление…</span>
                            )}
                            {dlItem?.state === 'queued' && (
                              <a
                                href={`/jobs/${dlItem.jobId}`}
                                onClick={(e) => e.stopPropagation()}
                                className="text-xs text-green-400 hover:underline"
                              >
                                ✓ В очереди →
                              </a>
                            )}
                            {dlItem?.state === 'duplicate' && (
                              <a
                                href="/movies"
                                onClick={(e) => e.stopPropagation()}
                                className="text-xs text-yellow-400 hover:underline"
                                title={dlItem.movieTitle ?? 'Фильм уже есть в базе'}
                              >
                                ⚠ Уже есть →
                              </a>
                            )}
                            {dlItem?.state === 'error' && (
                              <span className="text-xs text-red-400" title={dlItem.error}>
                                ✗ Ошибка
                              </span>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>

              {browseHasMore && (
                <div className="flex justify-center pt-2">
                  <button
                    onClick={handleLoadMore}
                    disabled={browsing}
                    className="px-6 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded text-sm font-medium"
                  >
                    {browsing ? 'Загрузка…' : `Загрузить ещё (осталось ${browseTotal - browseOffset})`}
                  </button>
                </div>
              )}
            </div>
          )}

          {!browsing && remoteMovies.length === 0 && !browseError && (
            <p className="text-sm text-gray-500">
              Введите URL каталога фильмов и нажмите «Обзор»
            </p>
          )}
        </div>
      </main>
    </div>
  )
}
