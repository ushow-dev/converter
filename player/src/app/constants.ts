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
  // ISO 639-1
  ru: 'Русский',
  en: 'English',
  uk: 'Українська',
  es: 'Español',
  fr: 'Français',
  de: 'Deutsch',
  it: 'Italiano',
  pt: 'Português',
  pl: 'Polski',
  tr: 'Türkçe',
  ja: 'Japanese',
  ko: 'Korean',
  zh: 'Chinese',
  hi: 'Hindi',
  ar: 'العربية',
  bn: 'বাংলা',
  th: 'ไทย',
  vi: 'Tiếng Việt',
  // ISO 639-2 (ffprobe uses these)
  eng: 'English',
  rus: 'Русский',
  hin: 'Hindi',
  spa: 'Español',
  fre: 'Français',
  fra: 'Français',
  ger: 'Deutsch',
  deu: 'Deutsch',
  ita: 'Italiano',
  por: 'Português',
  pol: 'Polski',
  tur: 'Türkçe',
  jpn: 'Japanese',
  kor: 'Korean',
  chi: 'Chinese',
  zho: 'Chinese',
  ara: 'العربية',
  ben: 'বাংলা',
  tha: 'ไทย',
  vie: 'Tiếng Việt',
  ukr: 'Українська',
  und: 'General',
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
