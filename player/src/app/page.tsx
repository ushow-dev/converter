import PlayerClient, { type MovieResponse } from './PlayerClient'

const API_URL = process.env.API_URL ?? 'http://localhost:8000'
const PLAYER_KEY = process.env.PLAYER_KEY ?? ''

type PageProps = {
  searchParams: { imdb_id?: string; tmdb_id?: string }
}

async function fetchMovieData(imdbId?: string, tmdbId?: string): Promise<{ data?: MovieResponse; error?: string }> {
  if (!imdbId && !tmdbId) {
    return { error: 'No movie ID provided. Use ?imdb_id= or ?tmdb_id= query param.' }
  }

  const params = new URLSearchParams()
  if (imdbId) params.set('imdb_id', imdbId)
  else if (tmdbId) params.set('tmdb_id', tmdbId)

  try {
    const res = await fetch(`${API_URL}/api/player/movie?${params.toString()}`, {
      headers: { 'X-Player-Key': PLAYER_KEY },
      cache: 'no-store',
    })
    if (!res.ok) {
      return { error: `API error ${res.status}` }
    }
    const data = (await res.json()) as MovieResponse
    return { data }
  } catch {
    return { error: 'Failed to reach API' }
  }
}

export default async function Page({ searchParams }: PageProps) {
  const { data, error } = await fetchMovieData(searchParams.imdb_id, searchParams.tmdb_id)
  if (error) return <div className="player-status">{error}</div>
  if (!data) return <div className="player-status">No data</div>
  return <PlayerClient initialData={data} />
}
