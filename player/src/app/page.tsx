import PlayerClient, { type MovieResponse } from './PlayerClient'
import SeriesPlayer from './SeriesPlayer'

const API_URL = process.env.API_URL ?? 'http://localhost:8000'
const PLAYER_KEY = process.env.PLAYER_KEY ?? ''

type PageProps = {
  searchParams: { imdb_id?: string; tmdb_id?: string; type?: string; s?: string; e?: string; nav?: string }
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
    if (!res.ok) return { error: `API error ${res.status}` }
    return { data: (await res.json()) as MovieResponse }
  } catch {
    return { error: 'Failed to reach API' }
  }
}

async function fetchSeriesData(tmdbId: string, s?: string, e?: string) {
  try {
    const endpoint = s && e
      ? `${API_URL}/api/player/episode?tmdb_id=${tmdbId}&s=${s}&e=${e}`
      : `${API_URL}/api/player/series?tmdb_id=${tmdbId}`
    const res = await fetch(endpoint, {
      headers: { 'X-Player-Key': PLAYER_KEY },
      cache: 'no-store',
    })
    if (!res.ok) return { error: `API error ${res.status}` }
    return { data: await res.json() }
  } catch {
    return { error: 'Failed to reach API' }
  }
}

export default async function Page({ searchParams }: PageProps) {
  if (searchParams.type === 'series' && searchParams.tmdb_id) {
    const { data, error } = await fetchSeriesData(searchParams.tmdb_id, searchParams.s, searchParams.e)
    if (error) return <div className="player-status">{error}</div>
    if (!data) return <div className="player-status">No data</div>
    const hideNav = searchParams.nav === '0' || !!(searchParams.s && searchParams.e)
    return <SeriesPlayer initialData={data} hideNavigation={hideNav} />
  }

  const { data, error } = await fetchMovieData(searchParams.imdb_id, searchParams.tmdb_id)
  if (error) return <div className="player-status">{error}</div>
  if (!data) return <div className="player-status">No data</div>
  return <PlayerClient initialData={data} />
}
