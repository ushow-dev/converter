'use client'

import { useEffect } from 'react'

interface PlayerModalProps {
  src: string
  title?: string
  onClose: () => void
}

export function PlayerModal({ src, title, onClose }: PlayerModalProps) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-3xl mx-4"
        onClick={e => e.stopPropagation()}
      >
        <button
          onClick={onClose}
          className="absolute -top-8 right-0 text-gray-400 hover:text-white text-sm"
        >
          ✕ закрыть
        </button>
        <div className="w-full overflow-hidden rounded-lg bg-black" style={{ aspectRatio: '16/10' }}>
          <iframe
            src={src}
            style={{ width: '100%', height: '100%', border: 0, display: 'block' }}
            scrolling="no"
            allow="autoplay; fullscreen; picture-in-picture"
            allowFullScreen
          />
        </div>
        {title && (
          <p className="mt-2 text-center text-sm text-gray-400">{title}</p>
        )}
      </div>
    </div>
  )
}
