export type JobStatus = 'queued' | 'in_progress' | 'completed' | 'failed'
export type JobStatusFilter = JobStatus | 'active'
export type JobStage = 'download' | 'convert'
export type ContentType = 'movie'
export type SourceType = 'torrent' | 'upload'
export type Priority = 'low' | 'normal' | 'high'

export interface SearchResultItem {
  external_id: string
  title: string
  source_type: SourceType
  source_ref: string
  size_bytes: number
  seeders: number
  leechers: number
  indexer: string
  content_type: ContentType
}

export interface SearchResponse {
  items: SearchResultItem[]
  total: number
  correlation_id: string
}

export interface Job {
  job_id: string
  content_type: ContentType
  source_type: SourceType
  source_ref: string
  title?: string          // from search_results JOIN (may be absent)
  thumbnail_path?: string // set when asset has a thumbnail
  movie_id?: number
  imdb_id?: string
  tmdb_id?: string
  status: JobStatus
  stage: JobStage | null
  progress_percent: number
  error_code: string | null
  error_message: string | null
  updated_at: string
  created_at: string
}

export interface JobsResponse {
  items: Job[]
  next_cursor: string | null
}

export interface CreateJobResponse {
  job_id: string
  status: JobStatus
  created_at: string
}

export interface ApiError {
  error: {
    code: string
    message: string
    retryable: boolean
    correlation_id: string
  }
}

export interface Movie {
  id: number
  storage_key: string
  imdb_id?: string
  tmdb_id?: string
  title?: string
  year?: number
  poster_url?: string
  has_thumbnail: boolean
  job_id?: string
  created_at: string
  updated_at: string
}

export interface MoviesResponse {
  items: Movie[]
  next_cursor: string | null
}

export interface Subtitle {
  id: number
  movie_id: number
  language: string
  source: 'opensubtitles' | 'upload'
  created_at: string
  updated_at: string
}

export interface SubtitlesResponse {
  items: Subtitle[]
}

export interface ProxyConfig {
  enabled: boolean
  host: string
  port: number
  type: 'SOCKS5' | 'HTTP'
  username: string
  password: string
}

export interface RemoteFile {
  name: string
  size: string
  url: string
}

export interface RemoteMovie {
  name: string
  url: string
  video_file: RemoteFile | null
  subtitle_files: RemoteFile[]
}

export interface RemoteDownloadResponse {
  job_id: string
  status: string
  title: string
  tmdb_id: string
  created_at: string
}

export type DownloadItemState = 'idle' | 'submitting' | 'queued' | 'error'

export interface DownloadItem {
  movie: RemoteMovie
  state: DownloadItemState
  jobId?: string
  error?: string
}
