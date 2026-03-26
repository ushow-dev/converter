// p2pMetrics.ts — collects P2P segment stats and beacons them to the API.

const REPORT_INTERVAL_MS = 30_000

interface P2PMetricsState {
  streamId: string
  httpBytes: number
  p2pBytes: number
  httpSegments: number
  p2pSegments: number
  peersConnected: number
  timer: ReturnType<typeof setInterval> | null
}

let state: P2PMetricsState | null = null

const METRICS_API_URL = process.env.NEXT_PUBLIC_API_URL || ''

function reportEndpoint(): string | null {
  if (!METRICS_API_URL) return null
  return `${METRICS_API_URL}/api/player/p2p-metrics`
}

function flush() {
  if (!state) return
  const endpoint = reportEndpoint()
  if (!endpoint) return

  const { streamId, httpBytes, p2pBytes, httpSegments, p2pSegments, peersConnected } = state
  if (httpBytes === 0 && p2pBytes === 0) return

  const payload = JSON.stringify({
    stream_id: streamId,
    http_bytes: httpBytes,
    p2p_bytes: p2pBytes,
    http_segments: httpSegments,
    p2p_segments: p2pSegments,
    peers: peersConnected,
    window_sec: REPORT_INTERVAL_MS / 1000,
  })

  // Use fetch instead of sendBeacon — sendBeacon with application/json
  // triggers CORS preflight which it cannot handle.
  fetch(endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: payload,
    keepalive: true,
    mode: 'cors',
  }).catch(() => { /* ignore */ })

  // Reset accumulators (keep peersConnected — it is a gauge).
  state.httpBytes = 0
  state.p2pBytes = 0
  state.httpSegments = 0
  state.p2pSegments = 0
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function startP2PMetrics(engine: any, streamId: string) {
  stopP2PMetrics()

  state = {
    streamId,
    httpBytes: 0,
    p2pBytes: 0,
    httpSegments: 0,
    p2pSegments: 0,
    peersConnected: 0,
    timer: null,
  }

  // p2p-media-loader v2 core events
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  engine.addEventListener('onSegmentLoaded', (details: any) => {
    if (!state) return
    const bytes = details?.bytesLength ?? details?.byteLength ?? 0
    if (details?.isP2PLoaded ?? details?.type === 'p2p') {
      state.p2pBytes += bytes
      state.p2pSegments += 1
    } else {
      state.httpBytes += bytes
      state.httpSegments += 1
    }
  })

  engine.addEventListener('onPeerConnect', () => {
    if (state) state.peersConnected += 1
  })
  engine.addEventListener('onPeerClose', () => {
    if (state) state.peersConnected = Math.max(0, state.peersConnected - 1)
  })

  state.timer = setInterval(flush, REPORT_INTERVAL_MS)
}

export function stopP2PMetrics() {
  if (state?.timer) {
    clearInterval(state.timer)
    flush() // final report
  }
  state = null
}
