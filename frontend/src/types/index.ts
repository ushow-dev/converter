export type JobStatus = 'queued' | 'in_progress' | 'completed' | 'failed'
export type JobStage = 'download' | 'convert'
export type ContentType = 'movie'
export type SourceType = 'torrent'
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
  status: JobStatus
  stage: JobStage | null
  progress_percent: number
  error_code: string | null
  error_message: string | null
  updated_at: string
  created_at?: string
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
