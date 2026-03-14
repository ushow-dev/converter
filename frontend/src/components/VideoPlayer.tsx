'use client'

import { useEffect, useRef, useState } from 'react'
import type { PlayerSubtitle } from '@/types'

interface VideoPlayerProps {
  hlsUrl: string
  posterUrl?: string
  subtitles?: PlayerSubtitle[]
}

export function VideoPlayer({ hlsUrl, posterUrl, subtitles = [] }: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [activeSubtitle, setActiveSubtitle] = useState<string | null>(null)

  // Initialise HLS
  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    let destroyHls: (() => void) | undefined

    ;(async () => {
      const Hls = (await import('hls.js')).default
      if (Hls.isSupported()) {
        const hls = new Hls()
        hls.loadSource(hlsUrl)
        hls.attachMedia(video)
        destroyHls = () => hls.destroy()
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        // Safari native HLS
        video.src = hlsUrl
      }
    })()

    return () => destroyHls?.()
  }, [hlsUrl])

  // Sync active subtitle track
  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    function sync() {
      const tracks = video!.textTracks
      for (let i = 0; i < tracks.length; i++) {
        tracks[i].mode = tracks[i].language === activeSubtitle ? 'showing' : 'hidden'
      }
    }

    sync()
    // Retry once after a short delay in case tracks haven't loaded yet
    const id = setTimeout(sync, 200)
    return () => clearTimeout(id)
  }, [activeSubtitle])

  return (
    <div>
      <video
        ref={videoRef}
        className="aspect-video w-full rounded-lg bg-black"
        controls
        poster={posterUrl}
        crossOrigin="anonymous"
      >
        {subtitles.map(sub => (
          <track
            key={sub.language}
            kind="subtitles"
            src={sub.url}
            srcLang={sub.language}
            label={sub.language.toUpperCase()}
          />
        ))}
      </video>

      {subtitles.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="text-xs text-gray-500">Субтитры:</span>
          <button
            onClick={() => setActiveSubtitle(null)}
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
              activeSubtitle === null
                ? 'bg-indigo-600 text-white'
                : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
            }`}
          >
            выкл
          </button>
          {subtitles.map(sub => (
            <button
              key={sub.language}
              onClick={() => setActiveSubtitle(sub.language)}
              className={`rounded px-2.5 py-1 font-mono text-xs font-medium uppercase transition-colors ${
                activeSubtitle === sub.language
                  ? 'bg-indigo-600 text-white'
                  : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
              }`}
            >
              {sub.language}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
