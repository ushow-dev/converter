import type { NextConfig } from 'next'

// API_URL is a server-side runtime env var — not baked into client bundles.
// The Next.js standalone server reads it on startup, so the rewrite destination
// can be changed without rebuilding the image.
const apiUrl = process.env.API_URL ?? 'http://api:8000'

const config: NextConfig = {
  output: 'standalone',
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: `${apiUrl}/api/:path*`,
      },
    ]
  },
}

export default config
