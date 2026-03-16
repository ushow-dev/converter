# Архитектура плеера

## Обзор

Система поддерживает два режима воспроизведения:

1. **Встроенный плеер в Admin UI** — компонент `VideoPlayer.tsx` в `frontend/`
2. **Standalone Player** — отдельное приложение в `player/` (iframe embed)

---

## 1. Встроенный Admin Player (`frontend/src/components/VideoPlayer.tsx`)

### Назначение
Воспроизведение HLS-контента прямо в Admin UI (страница `/movies`).

### Технология
- **hls.js** v1.5.13 — основная библиотека для HLS в браузере
- Нативный `<video>` element как fallback (Safari поддерживает HLS нативно)

### Логика инициализации
```typescript
// Если браузер поддерживает MSE (Media Source Extensions)
if (Hls.isSupported()) {
  const hls = new Hls()
  hls.loadSource(hlsUrl)   // master.m3u8
  hls.attachMedia(video)
}
// Иначе — нативный HLS (Safari)
else if (video.canPlayType('application/vnd.apple.mpegurl')) {
  video.src = hlsUrl
}
```

### Props
```typescript
interface VideoPlayerProps {
  hlsUrl: string      // URL к master.m3u8
  subtitles?: Array<{ language: string; url: string }>
}
```

---

## 2. Standalone Player (`player/`)

### Назначение
Самостоятельное приложение для встраивания через iframe в сторонние сайты.

### Аутентификация
Использует `PLAYER_API_KEY` (не JWT). Передаётся в заголовке `X-Player-Key`.

### API Endpoints (для плеера)

**GET `/api/player/movie`**
```
Query: ?imdb_id=tt1234567
       OR ?tmdb_id=123456
```

Возвращает:
```json
{
  "hls_url": "https://media.example.com/converted/{id}/master.m3u8?md5=...&expires=...",
  "thumbnail_url": "...",
  "title": "Movie Title",
  "subtitles": [
    { "language": "en", "url": "https://..." },
    { "language": "ru", "url": "https://..." }
  ]
}
```

**GET `/api/player/assets/{assetID}`**
Прямой доступ к asset по ID.

**GET `/api/player/jobs/{jobID}/status`**
Polling для ожидания конвертации:
```json
{
  "status": "in_progress",
  "progress_percent": 67,
  "hls_url": null
}
```

---

## Подписывание медиа-URL (Media Signing)

Если установлен `MEDIA_SIGNING_KEY`, API генерирует временные URL:

```
URL = MEDIA_BASE_URL + path + "?md5=" + token + "&expires=" + timestamp
token = base64(md5(MEDIA_SIGNING_KEY + " " + path + " " + expires))
```

Это совместимо с модулем `ngx_http_secure_link_module` nginx.

**nginx конфигурация (secure_link):**
```nginx
location /media/ {
  secure_link $arg_md5,$arg_expires;
  secure_link_md5 "$secure_link_expires$uri $secret_key";

  if ($secure_link = "") { return 403; }
  if ($secure_link = "0") { return 410; }

  root /data;
}
```

**Если `MEDIA_SIGNING_KEY` не задан:**
- URL возвращается без подписи
- Любой, знающий URL, может воспроизвести контент
- Допустимо для частных/локальных установок

---

## HLS Adaptive Streaming

Клиент (hls.js) автоматически выбирает разрешение:

```
master.m3u8
  ├── 360p  (800 kbps) ← медленное соединение
  ├── 480p  (1400 kbps)
  └── 720p  (2800 kbps) ← быстрое соединение (default)
```

hls.js использует ABR (Adaptive Bitrate) алгоритм на основе скорости сети.

---

## Субтитры

Субтитры отдаются в формате **WebVTT** (`.vtt`).

В Admin UI — компонент `SubtitleSection.tsx`:
- Список загруженных субтитров по языкам
- Ручная загрузка (.srt → авто-конвертация в .vtt на сервере)
- Авто-поиск через OpenSubtitles API

В Player — передаются как `<track>` elements:
```html
<track kind="subtitles" src="/media/converted/{id}/subtitles/en.vtt" srclang="en">
<track kind="subtitles" src="/media/converted/{id}/subtitles/ru.vtt" srclang="ru">
```

---

## Архитектурные решения

| Решение | Причина |
|---|---|
| hls.js вместо нативного | Кросс-браузерная совместимость (Chrome не поддерживает HLS нативно) |
| Отдельный Player API Key | Разделение admin и player аутентификации |
| VTT формат субтитров | Нативная поддержка в HTML5 `<track>` |
| MD5 signing (не JWT) | Совместимость с nginx secure_link модулем |
