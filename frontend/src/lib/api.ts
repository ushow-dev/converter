import type {
  SearchResponse,
  Job,
  JobsResponse,
  CreateJobResponse,
  JobStatus,
  ContentType,
  SourceType,
  Priority,
} from '@/types'

// ── Token storage ────────────────────────────────────────────────────────────

const TOKEN_KEY = 'admin_token'

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

export function jobsUrl(status?: JobStatus, limit = 50, cursor?: string): string {
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
      priority: params.priority ?? 'normal',
    }),
  })
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
