export const HLS_CONFIG = {
  startLevel: 0,
  capLevelToPlayerSize: true,
  testBandwidth: true,
  lowLatencyMode: false,
  abrEwmaDefaultEstimate: 300000,
  abrBandWidthFactor: 0.8,
  abrBandWidthUpFactor: 0.6,
  abrEwmaFastVoD: 3.0,
  abrEwmaSlowVoD: 9.0,
  maxBufferLength: 6,
  maxMaxBufferLength: 10,
  maxBufferSize: 12000000,
  maxBufferHole: 0.5,
  backBufferLength: 20,
}

export const SUBTITLE_LABELS: Record<string, string> = {
  ru: 'Русский',
  en: 'English',
  uk: 'Українська',
  es: 'Espanol',
  fr: 'Francais',
  de: 'Deutsch',
  it: 'Italiano',
  pt: 'Portugues',
  pl: 'Polski',
  tr: 'Turkce',
  ja: 'Japanese',
  ko: 'Korean',
  zh: 'Chinese',
}

export function normalizeLanguageCode(lang: string): string {
  const trimmed = lang.trim().toLowerCase()
  if (!trimmed) return 'und'
  return trimmed.split(/[-_]/)[0] || 'und'
}

export function subtitleLabel(lang: string): string {
  const normalized = normalizeLanguageCode(lang)
  return SUBTITLE_LABELS[normalized] ?? normalized.toUpperCase()
}
