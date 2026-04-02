// Shared types and helpers — importable from both server and client components.

export interface MovieResponse {
  data: {
    movie: { id: number; imdb_id: string; tmdb_id: string }
    playback: { hls: string }
    assets: { poster: string }
    subtitles?: { language: string; url: string }[]
  }
  meta: { version: string }
}

export interface PlaybackData {
  hls: string
  poster?: string
  subtitles?: { language: string; url: string }[]
  tmdbId?: string
}

export function movieResponseToPlayback(resp: MovieResponse): PlaybackData {
  return {
    hls: resp.data.playback.hls,
    poster: resp.data.assets.poster,
    subtitles: resp.data.subtitles,
    tmdbId: resp.data.movie.tmdb_id,
  }
}
