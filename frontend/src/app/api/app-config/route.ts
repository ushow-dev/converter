import { NextResponse } from 'next/server'

// Force dynamic rendering so process.env is read at request time, not build time.
export const dynamic = 'force-dynamic'

/**
 * Server-side config endpoint.
 * Exposes runtime env vars to client components without baking them into the bundle.
 * Add here only non-secret, UI-facing config values.
 */
export async function GET() {
  return NextResponse.json({
    playerUrl: (process.env.PLAYER_URL ?? '').replace(/\/$/, ''),
  })
}
