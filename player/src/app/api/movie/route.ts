import { NextRequest, NextResponse } from 'next/server'

const API_URL = process.env.API_URL ?? 'http://localhost:8000'
const PLAYER_KEY = process.env.PLAYER_KEY ?? ''

export async function GET(req: NextRequest) {
  const { searchParams } = new URL(req.url)
  const imdbId = searchParams.get('imdb_id')
  const tmdbId = searchParams.get('tmdb_id')

  if (!imdbId && !tmdbId) {
    return NextResponse.json(
      { error: 'imdb_id or tmdb_id query parameter is required' },
      { status: 400 },
    )
  }

  const params = new URLSearchParams()
  if (imdbId) params.set('imdb_id', imdbId)
  else if (tmdbId) params.set('tmdb_id', tmdbId)

  try {
    const apiRes = await fetch(`${API_URL}/api/player/movie?${params}`, {
      headers: { 'X-Player-Key': PLAYER_KEY },
      cache: 'no-store',
    })

    const data = await apiRes.json()
    return NextResponse.json(data, { status: apiRes.status })
  } catch {
    return NextResponse.json({ error: 'Failed to reach API' }, { status: 502 })
  }
}
