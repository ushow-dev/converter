'use client'

import { useState, useRef, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Nav } from '@/components/Nav'
import { tmdbLookup, uploadMovie } from '@/lib/api'

export default function UploadPage() {
  const router = useRouter()

  const [tmdbId, setTmdbId] = useState('')
  const [title, setTitle] = useState('')
  const [imdbId, setImdbId] = useState('')
  const [file, setFile] = useState<File | null>(null)

  const [lookupLoading, setLookupLoading] = useState(false)
  const [lookupError, setLookupError] = useState('')
  const [movieCard, setMovieCard] = useState<{
    posterUrl: string
    overview: string
    releaseDate: string
  } | null>(null)

  const [uploading, setUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState(0)
  const [uploadError, setUploadError] = useState('')

  const fileInputRef = useRef<HTMLInputElement>(null)

  // Warn on browser refresh / tab close during upload.
  useEffect(() => {
    if (!uploading) return
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [uploading])

  async function handleLookup() {
    if (!tmdbId.trim()) return
    setLookupLoading(true)
    setLookupError('')
    setMovieCard(null)
    try {
      const data = await tmdbLookup(tmdbId.trim())
      setTitle(data.title)
      setImdbId(data.imdb_id ?? '')
      setMovieCard({
        posterUrl: data.poster_url ?? '',
        overview: data.overview ?? '',
        releaseDate: data.release_date ?? '',
      })
    } catch (err: unknown) {
      setLookupError(err instanceof Error ? err.message : 'Ошибка запроса к TMDB')
    } finally {
      setLookupLoading(false)
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!file) {
      setUploadError('Выберите видеофайл')
      return
    }
    if (!title.trim()) {
      setUploadError('Укажите название фильма')
      return
    }

    setUploading(true)
    setUploadProgress(0)
    setUploadError('')

    try {
      const resp = await uploadMovie(
        file,
        { title: title.trim(), imdb_id: imdbId.trim(), tmdb_id: tmdbId.trim() },
        setUploadProgress,
      )
      router.push(`/jobs/${resp.job_id}`)
    } catch (err: unknown) {
      setUploadError(err instanceof Error ? err.message : 'Ошибка загрузки')
      setUploading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 text-gray-100">
      <Nav />
      <main className="max-w-xl mx-auto px-4 py-8">
        <h1 className="text-2xl font-bold mb-6">Добавить фильм</h1>

        <form onSubmit={handleSubmit} className="space-y-5">
          {/* TMDB lookup */}
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              TMDB ID
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={tmdbId}
                onChange={(e) => setTmdbId(e.target.value)}
                placeholder="например, 27205"
                className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-indigo-500"
              />
              <button
                type="button"
                onClick={handleLookup}
                disabled={lookupLoading || !tmdbId.trim()}
                className="px-4 py-2 bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 rounded text-sm font-medium"
              >
                {lookupLoading ? 'Поиск…' : 'Найти'}
              </button>
            </div>
            {lookupError && (
              <p className="mt-1 text-sm text-red-400">{lookupError}</p>
            )}
          </div>

          {/* Movie card — shown after successful TMDB lookup */}
          {movieCard && (
            <div className="flex gap-4 rounded-lg border border-gray-700 bg-gray-800/60 p-4">
              {movieCard.posterUrl ? (
                <img
                  src={movieCard.posterUrl}
                  alt={title}
                  className="h-36 w-24 shrink-0 rounded object-cover"
                />
              ) : (
                <div className="flex h-36 w-24 shrink-0 items-center justify-center rounded bg-gray-700 text-gray-500 text-xs">
                  Нет постера
                </div>
              )}
              <div className="flex flex-col gap-1 overflow-hidden">
                <p className="font-semibold text-white leading-tight">
                  {title}
                  {movieCard.releaseDate && (
                    <span className="ml-2 text-sm font-normal text-gray-400">
                      ({movieCard.releaseDate.slice(0, 4)})
                    </span>
                  )}
                </p>
                {movieCard.overview && (
                  <p className="text-sm text-gray-400 line-clamp-4 leading-snug">
                    {movieCard.overview}
                  </p>
                )}
              </div>
            </div>
          )}

          {/* Title */}
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              Название <span className="text-red-400">*</span>
            </label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              required
              placeholder="Название фильма"
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-indigo-500"
            />
          </div>

          {/* IMDb ID */}
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              IMDb ID
            </label>
            <input
              type="text"
              value={imdbId}
              onChange={(e) => setImdbId(e.target.value)}
              placeholder="например, tt1375666"
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-indigo-500"
            />
          </div>

          {/* File picker */}
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              Видеофайл <span className="text-red-400">*</span>
            </label>
            <input
              ref={fileInputRef}
              type="file"
              accept="video/*,.mkv,.avi,.ts,.m2ts"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              className="w-full text-sm text-gray-400 file:mr-3 file:py-2 file:px-4 file:rounded file:border-0 file:text-sm file:font-medium file:bg-indigo-600 file:text-white hover:file:bg-indigo-700 cursor-pointer"
            />
            {file && (
              <p className="mt-1 text-xs text-gray-500">
                {file.name} ({(file.size / 1024 ** 3).toFixed(2)} GB)
              </p>
            )}
          </div>

          {/* Upload progress */}
          {uploading && (
            <div className="space-y-3">
              <div className="rounded bg-yellow-900/40 border border-yellow-700 px-3 py-2 text-sm text-yellow-300">
                Не покидайте страницу — загрузка прервётся
              </div>
              <div>
                <div className="flex justify-between text-sm text-gray-400 mb-1">
                  <span>Загрузка…</span>
                  <span>{uploadProgress}%</span>
                </div>
                <div className="w-full bg-gray-700 rounded-full h-2">
                  <div
                    className="bg-indigo-500 h-2 rounded-full transition-all"
                    style={{ width: `${uploadProgress}%` }}
                  />
                </div>
              </div>
            </div>
          )}

          {uploadError && (
            <p className="text-sm text-red-400">{uploadError}</p>
          )}

          {/* Submit */}
          <button
            type="submit"
            disabled={uploading || !file || !title.trim()}
            className="w-full py-2 bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 rounded font-medium"
          >
            {uploading ? 'Загрузка…' : 'Загрузить фильм'}
          </button>
        </form>
      </main>
    </div>
  )
}
