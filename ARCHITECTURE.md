# ARCHITECTURE.md — Системная архитектура

> Краткий технический обзор для AI-ассистентов и разработчиков.
> Полная документация: `docs/architecture/`

---

## Обзор системы

Система самостоятельного хостинга для загрузки, конвертации и стриминга видео.

```
┌──────────────────────────────────────────────────────────────┐
│                        Пользователь                          │
│                   (браузер / плеер-embed)                    │
└──────────────┬───────────────────────────┬───────────────────┘
               │ Admin JWT                 │ Player API Key
               ▼                           ▼
┌──────────────────────┐     ┌─────────────────────────────────┐
│   Frontend (3000)    │────▶│         API Service (8000)       │
│   Next.js Admin UI   │     │         Go / chi v5              │
└──────────────────────┘     └──────┬─────────────┬────────────┘
                                    │             │
                              ┌─────▼──┐    ┌────▼─────┐
                              │Postgres│    │  Redis   │
                              │  :5432 │    │  :6379   │
                              └────────┘    └────┬─────┘
                                                 │ BLPOP
                                    ┌────────────▼──────────────┐
                                    │      Worker Service        │
                                    │      Go / goroutines       │
                                    └──┬───────┬────────────┬───┘
                                       │       │            │
                              ┌────────▼──┐  ┌─▼──────┐  ┌─▼────────┐
                              │qBittorrent│  │ FFmpeg │  │OpenSubs  │
                              │   :8080   │  │ (local)│  │   API    │
                              └───────────┘  └────────┘  └──────────┘
```

---

## Сервисы

| Сервис | Технология | Порт | Назначение |
|---|---|---|---|
| **api** | Go 1.23 | 8000 | HTTP API (admin + player), включая series CRUD |
| **worker** | Go 1.23 | 8001 (health) | Загрузка + конвертация (фильмы и сериалы, multi-audio) |
| **frontend** | Next.js 14 | 3000 | Admin UI (включая каталог сериалов) |
| **player** | Next.js | 3100 | Player embed (фильмы + series navigation) |
| **postgres** | PostgreSQL 16 | 5432 | Основная БД |
| **redis** | Redis 7 | 6379 | Очереди заданий |
| **qbittorrent** | LinuxServer | 8080 | Торрент-клиент |
| **prowlarr** | LinuxServer | 9696 | Индексатор торрентов |
| **flaresolverr** | — | 8191 | Обход Cloudflare для Prowlarr |

---

## Очереди Redis

| Очередь | Продюсер | Консьюмер | Payload |
|---|---|---|---|
| `download_queue` | API | Worker/downloader | `DownloadPayload` |
| `convert_queue` | API / Worker | Worker/converter | `ConvertPayload` |
| `remote_download_queue` | API | Worker/httpdownloader | `RemoteDownloadPayload` |

---

## Жизненный цикл задания (Job)

```
Создание задания
      │
      ▼
   [created] ──► download_queue (Redis)
      │
      ▼
   [queued]
      │
      ▼ (воркер подхватывает)
   [in_progress / stage=download]
      │
      ▼ (загрузка завершена)
   ──► convert_queue
      │
      ▼
   [in_progress / stage=convert]
      │
      ▼ (FFmpeg + создание asset/movie записей)
   [completed] ──► media_assets запись создана
      │
      (при ошибке) ──► [failed]
```

**Bypass для загрузки файлов:**
- Upload: API → convert_queue (минуя download_queue)
- Remote HTTP: remote_download_queue → convert_queue

---

## Схема базы данных

```sql
media_jobs          -- задания (статус, прогресс, stage)
  └─► media_assets  -- результаты HLS-конвертации (1:1 с job)
  └─► movies        -- каталог фильмов (imdb_id / tmdb_id)
         └─► movie_subtitles  -- субтитры (lang, VTT-файл)

series              -- сериалы (title, year, tmdb_id, storage_key)
  └─► seasons       -- сезоны (season_number, series_id)
         └─► episodes        -- эпизоды (episode_number, hls_path)
                └─► episode_assets     -- HLS assets эпизода
                └─► episode_subtitles  -- субтитры эпизода (lang, VTT)
media_audio_tracks  -- аудиодорожки (job_id, lang, index, codec)

search_results      -- кэш поисковых результатов Prowlarr
job_events          -- аудит-лог событий (JSONB)
```

---

## HLS Pipeline (Worker → FFmpeg)

```
Входной файл (любой формат)
      │
      ▼ ffprobe (audio track detection)
  media_audio_tracks записи (lang, index, codec)
      │
      ▼ FFmpeg
  ┌───┴──────────────────────────────────────────────────────┐
  │  Фильм:                                                   │
  │    360p → /media/converted/movies/{key}/360/              │
  │    480p → /media/converted/movies/{key}/480/              │
  │    720p → /media/converted/movies/{key}/720/              │
  │    master.m3u8 / thumbnail.jpg                            │
  │                                                           │
  │  Сериал (series branch):                                  │
  │    360p → /media/converted/series/{key}/s{NN}/e{NN}/360/  │
  │    480p → /media/converted/series/{key}/s{NN}/e{NN}/480/  │
  │    720p → /media/converted/series/{key}/s{NN}/e{NN}/720/  │
  │    master.m3u8 / thumbnail.jpg                            │
  └──────────────────────────────────────────────────────────┘
      │
      ▼ Создаются записи в БД
  Фильм:  media_assets + movies (title, year, imdb_id, tmdb_id)
  Сериал: episode_assets + episodes + seasons + series
      │
      ▼ Опционально
  Субтитры: OpenSubtitles API → .vtt файлы
  Постер: TMDB backdrop → poster_url
```

---

## Аутентификация

| Схема | Применение | Механизм |
|---|---|---|
| **JWT (HS256)** | Admin API (`/api/admin/*`) | `Authorization: Bearer <token>` или `?token=<token>` |
| **Player API Key** | Player API (`/api/player/*`) | `X-Player-Key: <key>` |
| **Signed URLs** | HLS сегменты (опционально) | MD5 HMAC + TTL (nginx secure_link) |

---

## Подписывание медиа-URL

Если задан `MEDIA_SIGNING_KEY`, API генерирует временные подписанные URL для HLS-сегментов:
```
/media/{path}?md5={hash}&expires={timestamp}
```
Верификация производится nginx (`secure_link` модуль) или может быть реализована в CDN.

---

## Сетевая топология Docker

Все сервисы в bridge-сети `app_net`.
Внутренние имена хостов: `api`, `worker`, `postgres`, `redis`, `qbittorrent`, `prowlarr`.

---

## Технические решения и их обоснование

| Решение | Почему |
|---|---|
| Redis BLPOP (не pub/sub) | Гарантированная доставка, ровно один раз |
| Cursor pagination (не offset) | Производительность на больших таблицах |
| Два модуля Go (api + worker) | Независимое развёртывание и масштабирование |
| Multi-stage Docker build | Минимальный образ (alpine), нет dev-зависимостей |
| Bcrypt для паролей | Стойкость к брутфорсу |
| `request_id` UNIQUE | Идемпотентность при повторных запросах |
