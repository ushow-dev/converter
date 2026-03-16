# Потоки данных

## 1. Поток: Поиск и загрузка торрента

```
Пользователь (браузер)
    │
    │ GET /api/admin/search?q=...
    │ Authorization: Bearer <jwt>
    ▼
API Service
    │
    │ HTTP GET prowlarr:9696/api/v1/indexer/...
    │ (circuit breaker + retry + кэш в search_results)
    ▼
Prowlarr
    │ (если нужен Cloudflare bypass → FlaresolveRR)
    ▼
API → JSON список релизов → Frontend
    │
    │ POST /api/admin/jobs {magnet_link, title, imdb_id}
    ▼
API Service
    ├── INSERT INTO media_jobs (status=queued)
    └── RPUSH download_queue {DownloadPayload JSON}
    │
    ▼ Redis
    │
    │ BLPOP download_queue
    ▼
Worker (download goroutine)
    ├── UPDATE media_jobs SET status=in_progress, stage=download
    ├── POST qbittorrent:8080/api/v2/torrents/add
    ├── Poll qbittorrent status (loop)
    └── On complete:
        ├── RPUSH convert_queue {ConvertPayload JSON}
        └── UPDATE media_jobs SET stage=convert
    │
    ▼ Redis
    │
    │ BLPOP convert_queue
    ▼
Worker (convert goroutine)
    ├── ffmpeg -i /media/downloads/{jobID}/{file} → /media/converted/{movieID}/
    ├── INSERT INTO movies (title, imdb_id, tmdb_id)
    ├── INSERT INTO media_assets (storage_path, is_ready=true)
    ├── [optional] fetch subtitles → movie_subtitles
    └── UPDATE media_jobs SET status=completed
```

## 2. Поток: Загрузка файла (Upload)

```
Пользователь
    │ POST /api/admin/jobs/upload (multipart, до 50 ГБ)
    ▼
API Service
    ├── Сохраняет файл → /media/downloads/{jobID}/{filename}
    ├── INSERT INTO media_jobs (status=queued)
    └── RPUSH convert_queue {ConvertPayload} (минуя download)
    │
    ▼ (сразу на конвертацию)
Worker (convert goroutine)
    └── (тот же поток, что и выше)
```

## 3. Поток: HTTP-загрузка (Remote Download)

```
Пользователь
    │ POST /api/admin/jobs/remote-download {url, title}
    ▼
API Service
    ├── INSERT INTO media_jobs
    └── RPUSH remote_download_queue {RemoteDownloadPayload}
    │
    ▼
Worker (httpdownload goroutine)
    ├── HTTP GET {url} (с поддержкой SOCKS5/HTTP proxy)
    ├── Сохраняет → /media/downloads/{jobID}/
    └── RPUSH convert_queue → (стандартный конвертационный поток)
```

## 4. Поток: Воспроизведение (Player)

```
Плеер (iframe embed)
    │ GET /api/player/movie?imdb_id=tt1234567
    │ X-Player-Key: <player_api_key>
    ▼
API Service
    ├── SELECT movies + media_assets WHERE imdb_id = ?
    └── Если MEDIA_SIGNING_KEY задан:
        └── md5(secret + path + expires) → подписанный URL
    │
    │ JSON: {hls_url, thumbnail, subtitles[]}
    ▼
Player (hls.js)
    │ GET /media/converted/{movieID}/master.m3u8
    │ (опционально: ?md5=...&expires=...)
    ▼
nginx / CDN → /media/ bind mount
    │
    │ HLS сегменты .ts файлы
    ▼
Браузер (видео)
```

## 5. Поток: Субтитры

```
Auto (при конвертации):
Worker → OpenSubtitles API (search by tmdb_id)
       → Download SRT → Convert to VTT
       → /media/converted/{movieID}/subtitles/{lang}.vtt
       → INSERT movie_subtitles

Manual (через Admin UI):
Пользователь → POST /api/admin/movies/{id}/subtitles (файл .srt/.vtt)
API → Convert SRT→VTT если нужно
    → Сохранить файл
    → INSERT/UPSERT movie_subtitles

Auto-search (через Admin UI):
Пользователь → POST /api/admin/movies/{id}/subtitles/search
API → OpenSubtitles search → download → store (как Auto выше)
```

## 6. Поток данных TMDB

```
При создании remote-download задания:
API → parse title+year из URL/filename
    → GET TMDB /search/movie?query=...
    → Сохранить tmdb_id в ConvertPayload

При конвертации (Worker):
Worker → GET TMDB /movie/{tmdb_id} → poster_path
       → Download backdrop image → /media/converted/{movieID}/poster.jpg
       → UPDATE movies SET poster_url = ...
```

## Схема хранения медиа-данных

```
/media/                         (bind mount с хоста)
├── downloads/
│   └── {jobID}/
│       └── {filename.ext}      ← сырой файл (торрент / HTTP)
│
├── temp/
│   └── {jobID}/
│       └── hls_workspace/      ← временный FFmpeg output
│
└── converted/
    └── {movieStorageKey}/      ← формат: mov_{md5hash}
        ├── master.m3u8
        ├── 360/
        │   ├── playlist_360p.m3u8
        │   └── segment_*.ts
        ├── 480/
        │   ├── playlist_480p.m3u8
        │   └── segment_*.ts
        ├── 720/
        │   ├── playlist_720p.m3u8
        │   └── segment_*.ts
        ├── thumbnail.jpg
        └── subtitles/
            ├── en.vtt
            └── ru.vtt
```
