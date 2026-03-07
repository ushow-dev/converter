import type {
  SearchResponse,
  Job,
  JobsResponse,
  CreateJobResponse,
  JobStatus,
  JobStatusFilter,
  ContentType,
  SourceType,
  Priority,
  Movie,
  MoviesResponse,
} from '@/types'

// ── Token storage ────────────────────────────────────────────────────────────

const TOKEN_KEY = 'admin_token'
const UPLOAD_PATH = '/api/admin/jobs/upload'

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, '')
}

function getUploadEndpoint(): string {
  const configuredOrigin = process.env.NEXT_PUBLIC_UPLOAD_ORIGIN?.trim()
  if (configuredOrigin) {
    return `${trimTrailingSlash(configuredOrigin)}${UPLOAD_PATH}`
  }

  if (typeof window !== 'undefined') {
    const { protocol, hostname } = window.location
    if (hostname.startsWith('admin.')) {
      const baseDomain = hostname.slice('admin.'.length)
      return `${protocol}//upload.${baseDomain}${UPLOAD_PATH}`
    }
  }

  return UPLOAD_PATH
}

export function getToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

// ── HTTP client ──────────────────────────────────────────────────────────────

interface FetchError extends Error {
  status: number
  code?: string
}

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(options?.headers as Record<string, string>),
  }

  const res = await fetch(path, { ...options, headers })

  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    const err = new Error(
      body?.error?.message ?? `HTTP ${res.status}`,
    ) as FetchError
    err.status = res.status
    err.code = body?.error?.code
    throw err
  }

  return res.json() as Promise<T>
}

// SWR-compatible fetcher — throws on error, returns data
export async function fetcher<T>(url: string): Promise<T> {
  return apiFetch<T>(url)
}

// ── Auth ─────────────────────────────────────────────────────────────────────

export async function login(email: string, password: string): Promise<string> {
  const data = await apiFetch<{ access_token: string; token_type: string; expires_in: number }>(
    '/api/admin/auth/login',
    {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    },
  )
  return data.access_token
}

// ── Search ───────────────────────────────────────────────────────────────────

export function searchUrl(
  query: string,
  contentType: ContentType = 'movie',
  limit = 50,
): string {
  const p = new URLSearchParams({
    query,
    content_type: contentType,
    limit: String(limit),
  })
  return `/api/admin/search?${p}`
}

export async function search(
  query: string,
  contentType: ContentType = 'movie',
  limit = 50,
): Promise<SearchResponse> {
  return apiFetch<SearchResponse>(searchUrl(query, contentType, limit))
}

// ── Jobs ─────────────────────────────────────────────────────────────────────

export function jobsUrl(status?: JobStatusFilter, limit = 50, cursor?: string): string {
  const p = new URLSearchParams({ limit: String(limit) })
  if (status) p.set('status', status)
  if (cursor) p.set('cursor', cursor)
  return `/api/admin/jobs?${p}`
}

export async function getJobs(
  status?: JobStatus,
  limit = 50,
  cursor?: string,
): Promise<JobsResponse> {
  return apiFetch<JobsResponse>(jobsUrl(status, limit, cursor))
}

export async function getJob(jobId: string): Promise<Job> {
  return apiFetch<Job>(`/api/admin/jobs/${jobId}`)
}

export async function createJob(params: {
  source_ref: string
  imdb_id: string
  tmdb_id: string
  title?: string
  source_type?: SourceType
  content_type?: ContentType
  priority?: Priority
}): Promise<CreateJobResponse> {
  return apiFetch<CreateJobResponse>('/api/admin/jobs', {
    method: 'POST',
    body: JSON.stringify({
      request_id: crypto.randomUUID(),
      content_type: params.content_type ?? 'movie',
      source_type: params.source_type ?? 'torrent',
      source_ref: params.source_ref,
      imdb_id: params.imdb_id,
      tmdb_id: params.tmdb_id,
      title: params.title ?? '',
      priority: params.priority ?? 'normal',
    }),
  })
}

// ── Movies ────────────────────────────────────────────────────────────────────

export function moviesUrl(limit = 100, cursor?: string): string {
  const p = new URLSearchParams({ limit: String(limit) })
  if (cursor) p.set('cursor', cursor)
  return `/api/admin/movies?${p}`
}

export async function getMovies(limit = 100, cursor?: string): Promise<MoviesResponse> {
  return apiFetch<MoviesResponse>(moviesUrl(limit, cursor))
}

export function movieThumbnailSrc(movieId: number): string {
  const token = getToken()
  return `/api/admin/movies/${movieId}/thumbnail${token ? `?token=${encodeURIComponent(token)}` : ''}`
}


export async function tmdbLookup(tmdbId: string): Promise<{
  title: string
  imdb_id: string
  poster_url: string
  overview: string
  release_date: string
}> {
  return apiFetch(`/api/admin/movies/tmdb/${encodeURIComponent(tmdbId)}`)
}

export async function uploadMovie(
  file: File,
  params: { title: string; imdb_id: string; tmdb_id: string },
  onProgress?: (percent: number) => void,
): Promise<CreateJobResponse> {
  return new Promise((resolve, reject) => {
    const token = getToken()
    const formData = new FormData()
    formData.append('file', file)
    formData.append('title', params.title)
    formData.append('imdb_id', params.imdb_id)
    formData.append('tmdb_id', params.tmdb_id)
    formData.append('request_id', crypto.randomUUID())

    const xhr = new XMLHttpRequest()
    xhr.open('POST', getUploadEndpoint())
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) {
        onProgress(Math.round((e.loaded / e.total) * 100))
      }
    }

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as CreateJobResponse)
        } catch {
          reject(new Error('Invalid response from server'))
        }
      } else {
        try {
          const body = JSON.parse(xhr.responseText)
          reject(new Error(body?.error?.message ?? `HTTP ${xhr.status}`))
        } catch {
          reject(new Error(`HTTP ${xhr.status}`))
        }
      }
    }

    xhr.onerror = () => reject(new Error('Network error during upload'))
    xhr.send(formData)
  })
}

export async function updateMovieIDs(movieId: number, imdbId: string, tmdbId: string, title: string): Promise<void> {
  const token = getToken()
  const res = await fetch(`/api/admin/movies/${movieId}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify({ imdb_id: imdbId, tmdb_id: tmdbId, title }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body?.error?.message ?? `HTTP ${res.status}`)
  }
}

export async function deleteJob(jobId: string): Promise<void> {
  const token = getToken()
  const res = await fetch(`/api/admin/jobs/${jobId}`, {
    method: 'DELETE',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body?.error?.message ?? `HTTP ${res.status}`)
  }
}

// ── Formatters ───────────────────────────────────────────────────────────────

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`
  return `${(bytes / 1024 ** 3).toFixed(1)} GB`
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleString()
}
