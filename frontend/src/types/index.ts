export type JobStatus = 'queued' | 'in_progress' | 'completed' | 'failed'
export type JobStatusFilter = JobStatus | 'active'
export type JobStage = 'download' | 'convert' | 'transfer'
export type ContentType = 'movie' | 'series'
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
  thumbnail_url?: string
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

export interface BrowseResponse {
  items: RemoteMovie[]
  total: number
  has_more: boolean
}

export interface RemoteDownloadResponse {
  job_id: string
  status: string
  title: string
  tmdb_id: string
  created_at: string
}

export interface PlayerSubtitle {
  language: string
  url: string
}

export interface PlayerMovieResponse {
  data: {
    movie: { id: number; imdb_id: string | null; tmdb_id: string | null }
    playback: { hls: string }
    assets: { poster: string }
    subtitles: PlayerSubtitle[]
  }
  meta: { version: string }
}

export type ScannerDownloadStatus = 'queued' | 'downloading' | 'done' | 'failed'

export interface ScannerDownload {
  id: number
  url: string
  filename: string
  status: ScannerDownloadStatus
  error_message: string | null
  created_at: string
  updated_at: string
}

export interface ScannerDownloadsResponse {
  items: ScannerDownload[]
}

export type DownloadItemState = 'idle' | 'submitting' | 'queued' | 'error' | 'duplicate'

export interface DownloadItem {
  movie: RemoteMovie
  state: DownloadItemState
  jobId?: string
  movieId?: number
  movieTitle?: string
  error?: string
}

export interface Series {
  id: number
  storage_key: string
  tmdb_id?: string
  imdb_id?: string
  title: string
  year?: number
  poster_url?: string
  created_at: string
  updated_at: string
}

export interface SeriesResponse {
  items: Series[]
  next_cursor: string | null
}

export interface Season {
  id: number
  series_id: number
  season_number: number
  poster_url?: string
}

export interface Episode {
  id: number
  season_id: number
  episode_number: number
  title?: string
  storage_key: string
  has_thumbnail?: boolean
  thumbnail_url?: string
  created_at: string
  updated_at: string
}

export interface SeriesDetailResponse {
  series: Series
  seasons: Array<{
    id: number
    season_number: number
    poster_url?: string
    episodes: Episode[]
  }>
}
