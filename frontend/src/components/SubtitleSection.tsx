'use client'

import { useRef, useState } from 'react'
import useSWR from 'swr'
import { fetcher, searchSubtitles, uploadSubtitle } from '@/lib/api'
import type { SubtitlesResponse } from '@/types'

const LANG_LABELS: Record<string, string> = {
  ru: 'Русский',
  en: 'English',
  de: 'Deutsch',
  fr: 'Français',
  es: 'Español',
  zh: '中文',
  uk: 'Українська',
}

function langLabel(code: string): string {
  return LANG_LABELS[code] ?? code.toUpperCase()
}

export function SubtitleSection({ movieId }: { movieId: number }) {
  const { data, mutate, isLoading } = useSWR<SubtitlesResponse>(
    `/api/admin/movies/${movieId}/subtitles`,
    fetcher,
  )

  const [searching, setSearching] = useState(false)
  const [searchMsg, setSearchMsg] = useState<string | null>(null)
  const [uploadLang, setUploadLang] = useState('ru')
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  async function handleSearch() {
    setSearching(true)
    setSearchMsg(null)
    try {
      const res = await searchSubtitles(movieId)
      await mutate()
      setSearchMsg(`Найдено: ${res.found}`)
    } catch (e) {
      setSearchMsg(e instanceof Error ? e.message : 'Ошибка поиска')
    } finally {
      setSearching(false)
    }
  }

  async function handleUpload() {
    const file = fileInputRef.current?.files?.[0]
    if (!file || !uploadLang) return
    setUploading(true)
    setUploadError(null)
    try {
      await uploadSubtitle(movieId, uploadLang, file)
      await mutate()
      if (fileInputRef.current) fileInputRef.current.value = ''
    } catch (e) {
      setUploadError(e instanceof Error ? e.message : 'Ошибка загрузки')
    } finally {
      setUploading(false)
    }
  }

  const items = data?.items ?? []

  return (
    <div className="mt-6 rounded-md border border-gray-800 bg-gray-900/50 px-6 py-4">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-200">Субтитры</h3>
        <button
          onClick={handleSearch}
          disabled={searching}
          className="rounded bg-indigo-700 px-3 py-1 text-xs font-medium text-white hover:bg-indigo-600 disabled:opacity-50"
        >
          {searching ? 'Поиск…' : 'Искать субтитры'}
        </button>
      </div>

      {searchMsg && (
        <p className="mb-3 text-xs text-gray-400">{searchMsg}</p>
      )}

      {isLoading && <p className="text-xs text-gray-500">Загрузка…</p>}

      {!isLoading && items.length === 0 && (
        <p className="mb-3 text-xs text-gray-500">Субтитры не найдены</p>
      )}

      {items.length > 0 && (
        <table className="mb-4 w-full text-sm">
          <thead>
            <tr className="border-b border-gray-800">
              <th className="pb-2 text-left text-xs font-medium text-gray-400">Язык</th>
              <th className="pb-2 text-left text-xs font-medium text-gray-400">Источник</th>
              <th className="pb-2 text-left text-xs font-medium text-gray-400">Добавлено</th>
            </tr>
          </thead>
          <tbody>
            {items.map((sub) => (
              <tr key={sub.id} className="border-b border-gray-800/50">
                <td className="py-2 text-gray-200">{langLabel(sub.language)}</td>
                <td className="py-2 text-gray-400">{sub.source === 'opensubtitles' ? 'OpenSubtitles' : 'Вручную'}</td>
                <td className="py-2 text-gray-500 text-xs">{new Date(sub.created_at).toLocaleDateString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div className="flex items-center gap-2 border-t border-gray-800 pt-4">
        <span className="text-xs text-gray-400">Загрузить вручную:</span>
        <select
          value={uploadLang}
          onChange={(e) => setUploadLang(e.target.value)}
          className="rounded border border-gray-700 bg-gray-800 px-2 py-1 text-xs text-gray-200"
        >
          <option value="ru">Русский</option>
          <option value="en">English</option>
          <option value="de">Deutsch</option>
          <option value="fr">Français</option>
          <option value="es">Español</option>
          <option value="uk">Українська</option>
        </select>
        <input
          ref={fileInputRef}
          type="file"
          accept=".vtt,.srt"
          className="text-xs text-gray-300 file:mr-2 file:rounded file:border-0 file:bg-gray-700 file:px-2 file:py-1 file:text-xs file:text-gray-200"
        />
        <button
          onClick={handleUpload}
          disabled={uploading}
          className="rounded bg-gray-700 px-3 py-1 text-xs font-medium text-gray-200 hover:bg-gray-600 disabled:opacity-50"
        >
          {uploading ? 'Загрузка…' : 'Загрузить'}
        </button>
      </div>
      {uploadError && <p className="mt-2 text-xs text-red-400">{uploadError}</p>}
    </div>
  )
}
