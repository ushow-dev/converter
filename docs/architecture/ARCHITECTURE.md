# Архитектура системы видеостриминга

## Обзор

Система состоит из пяти независимых компонентов, каждый со своей зоной ответственности.

```
┌─────────────────────────────────────────────────────────────────────┐
│  Внешние сайты                                                      │
│  <iframe src="https://player.example.com/embed/{imdb_id}">         │
└───────────────────────────┬─────────────────────────────────────────┘
                            │
                    ┌───────▼────────┐
                    │   5. Player    │  HTML-страница с HLS-плеером
                    │  (Next.js /    │  Получает IMDB ID из URL
                    │   статика)     │
                    └───────┬────────┘
                            │ GET /api/v1/movies/{imdb_id}
                    ┌───────▼────────┐
                    │  4. Public API │  Публичный API для плеера
                    │  (Go / chi)    │  Без аутентификации
                    └───────┬────────┘
                            │
              ┌─────────────▼──────────────┐
              │       3. PostgreSQL         │
              │  movies: imdb_id, title,    │
              │  poster_url, playlist_url   │
              └─────────────┬──────────────┘
                            │
              ┌─────────────▼──────────────┐
              │    1. Admin API + Panel     │  JWT-аутентификация
              │    (Go API + Next.js UI)    │  Управление загрузками
              └─────────────┬──────────────┘
                            │ Redis queue
              ┌─────────────▼──────────────┐
              │   2. Worker (downloader +   │  qBittorrent + ffmpeg
              │   converter)               │  HLS: 360/480/720p
              └─────────────┬──────────────┘
                            │
              ┌─────────────▼──────────────┐
              │   Media Storage (NFS/S3/   │  /media/library/{imdb_id}/
              │   локальный диск)          │  master.m3u8, thumbnail.jpg
              └────────────────────────────┘
```

---

## 1. Admin Panel

**Репозиторий:** `frontend/` + `api/`
**Доступ:** внутренний, JWT-аутентификация

### Ответственность
- Поиск фильмов через Prowlarr (торрент-индексер)
- Создание заданий на скачивание с привязкой IMDB ID
- Отображение статуса скачивания/конвертации
- Управление каталогом фильмов (удаление, ретрай)

### API (Admin, protected by JWT)
```
POST   /api/admin/auth/login
GET    /api/admin/search?query=&content_type=movie
POST   /api/admin/jobs              { source_ref, imdb_id, title }
GET    /api/admin/jobs
GET    /api/admin/jobs/{jobID}
DELETE /api/admin/jobs/{jobID}
GET    /api/admin/jobs/{jobID}/thumbnail?token=
```

### Изменения относительно текущего состояния
- Добавить поле `imdb_id` в форму добавления фильма
- Добавить поле `title` (из IMDB или ввода вручную)

---

## 2. Worker (Downloader + Converter)

**Репозиторий:** `worker/`

### Ответственность
- BLPOP из Redis → скачивание через qBittorrent
- ffmpeg HLS-конвертация (360p / 480p / 720p)
- Скриншот на 10-й минуте (thumbnail.jpg)
- Перемещение готового контента в **библиотеку**
- Запись результата в БД

### Файловая структура хранилища
```
/media/
  downloads/{jobID}/          # временно, пока скачивается
  temp/{jobID}/               # временно, пока конвертируется
  library/{imdb_id}/          # постоянное хранилище
    master.m3u8
    thumbnail.jpg
    720/index.m3u8 + *.ts
    480/index.m3u8 + *.ts
    360/index.m3u8 + *.ts
```

**Ключевое отличие от текущего:** готовый контент кладётся в `library/{imdb_id}/` (не `converted/{jobID}/`), что позволяет Player API раздавать файлы по IMDB ID без обращения к БД по каждому сегменту.

### Изменения относительно текущего состояния
- Финальная директория: `library/{imdb_id}` вместо `converted/{jobID}`
- После перемещения записать в таблицу `movies`: `imdb_id`, `playlist_url`, `thumbnail_url`

---

## 3. База данных (PostgreSQL)

### Новая таблица `movies`
```sql
CREATE TABLE movies (
    imdb_id        TEXT PRIMARY KEY,          -- tt1234567
    title          TEXT NOT NULL,
    year           INTEGER,
    poster_url     TEXT,                       -- внешняя ссылка (IMDB/TMDB)
    playlist_url   TEXT NOT NULL,             -- /stream/tt1234567/master.m3u8
    thumbnail_url  TEXT,                      -- /stream/tt1234567/thumbnail.jpg
    duration_sec   INTEGER,
    video_codec    TEXT,
    audio_codec    TEXT,
    is_ready       BOOLEAN NOT NULL DEFAULT FALSE,
    job_id         TEXT REFERENCES media_jobs(job_id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Таблица `media_jobs` — добавить поля
```sql
ALTER TABLE media_jobs
    ADD COLUMN imdb_id TEXT,
    ADD COLUMN movie_title TEXT;
```

### Таблица `media_assets` — оставить как есть
Технические данные конвертации (кодеки, длительность).

---

## 4. Public API (для плеера)

**Репозиторий:** отдельный сервис или новый `router group` в `api/`
**Доступ:** публичный (без аутентификации), rate-limited

### Эндпоинты
```
GET /api/v1/movies/{imdb_id}
```

**Ответ:**
```json
{
  "imdb_id": "tt1234567",
  "title": "Inception",
  "year": 2010,
  "poster_url": "https://image.tmdb.org/...",
  "thumbnail_url": "https://cdn.example.com/stream/tt1234567/thumbnail.jpg",
  "is_ready": true,
  "streams": {
    "hls": "https://cdn.example.com/stream/tt1234567/master.m3u8",
    "qualities": ["720p", "480p", "360p"]
  },
  "duration_sec": 8880
}
```

### Варианты размещения
| Вариант | Плюсы | Минусы |
|---------|-------|--------|
| **Новый router group в api/** | Один бинарник | Нужна изоляция auth-логики |
| **Отдельный Go-сервис** | Чистое разделение, независимый деплой | Дополнительный контейнер |
| **CloudFlare Worker / Edge** | Кеш на уровне CDN | Сложность |

**Рекомендация:** новый group `/api/v1` в существующем `api/` — минимум изменений, достаточная изоляция (отдельный middleware без JWT).

### Раздача HLS-сегментов
HLS-плеер делает запрос на каждый `.ts`-сегмент. Их не должен отдавать Go-API — это дорого. Варианты:

| Вариант | Рекомендуется для |
|---------|-------------------|
| **nginx** как static file server (`/stream/{imdb_id}/`) | Self-hosted, один сервер |
| **S3 + CloudFront** | Масштабируемость, много пользователей |
| **Backblaze B2 + CDN** | Дешевле S3 |

**Для текущей архитектуры** (один хост, Docker): nginx монтирует `/media/library` и раздаёт по `/stream/{imdb_id}/`.

```nginx
location /stream/ {
    alias /media/library/;
    add_header Cache-Control "public, max-age=31536000";
    add_header Access-Control-Allow-Origin "*";
    types {
        application/vnd.apple.mpegurl m3u8;
        video/mp2t ts;
    }
}
```

---

## 5. Плеер

**Репозиторий:** `player/` (новый) или отдельный домен
**Технология:** Next.js или чистый HTML + [hls.js](https://github.com/video-dev/hls.js)

### Механика встраивания
```html
<!-- На сторонних сайтах -->
<iframe
  src="https://player.example.com/embed/tt1234567"
  width="960" height="540"
  allowfullscreen
  frameborder="0">
</iframe>
```

### Страница плеера (`/embed/{imdb_id}`)
```
1. Загрузить /embed/tt1234567
2. Запросить GET /api/v1/movies/tt1234567
3. Если is_ready=false → показать "Скоро"
4. Если is_ready=true  → инициализировать hls.js с streams.hls URL
5. Воспроизвести
```

### Минимальная реализация (hls.js)
```html
<!DOCTYPE html>
<html>
<body style="margin:0;background:#000">
<video id="v" controls style="width:100%;height:100vh"></video>
<script src="https://cdn.jsdelivr.net/npm/hls.js"></script>
<script>
  const imdbId = location.pathname.split('/').pop()
  fetch(`/api/v1/movies/${imdbId}`)
    .then(r => r.json())
    .then(movie => {
      if (!movie.is_ready) return
      const video = document.getElementById('v')
      if (Hls.isSupported()) {
        const hls = new Hls()
        hls.loadSource(movie.streams.hls)
        hls.attachMedia(video)
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = movie.streams.hls  // Safari native HLS
      }
    })
</script>
</body>
</html>
```

---

## Поток данных (полный цикл)

```
Пользователь (Админка)
  │
  ├─ 1. Поиск: GET /api/admin/search?query=Inception
  │         ← Prowlarr возвращает список торрентов
  │
  ├─ 2. Добавление: POST /api/admin/jobs
  │         { source_ref: "magnet:...", imdb_id: "tt1234567", title: "Inception" }
  │         → job создаётся в media_jobs, попадает в Redis download_queue
  │
Worker
  ├─ 3. BLPOP download_queue
  │         → qBittorrent скачивает в /media/downloads/{jobID}/
  │
  ├─ 4. BLPOP convert_queue
  │         → ffmpeg создаёт HLS в /media/temp/{jobID}/
  │         → os.Rename temp/{jobID} → library/{imdb_id}
  │         → INSERT INTO movies (imdb_id, title, playlist_url, ...)
  │         → UPDATE media_jobs SET status='completed'
  │
nginx
  ├─ 5. Раздаёт /stream/{imdb_id}/ → /media/library/{imdb_id}/
  │
Плеер (embed)
  ├─ 6. GET /api/v1/movies/tt1234567
  │         ← { is_ready: true, streams: { hls: "/stream/tt1234567/master.m3u8" } }
  │
  └─ 7. hls.js загружает master.m3u8, затем сегменты напрямую из nginx/CDN
```

---

## Что нужно реализовать (приоритет)

| # | Задача | Где | Сложность |
|---|--------|-----|-----------|
| 1 | Поле `imdb_id` в job creation | api/, worker/, DB | Низкая |
| 2 | Таблица `movies` + миграция | DB | Низкая |
| 3 | Worker: финальная папка `library/{imdb_id}` | worker/ | Низкая |
| 4 | Worker: INSERT INTO movies после конвертации | worker/ | Низкая |
| 5 | Public API: GET /api/v1/movies/{imdb_id} | api/ | Низкая |
| 6 | nginx для раздачи HLS-сегментов | docker-compose | Низкая |
| 7 | Плеер `/embed/{imdb_id}` | player/ или api/ | Средняя |
| 8 | Admin UI: поле imdb_id в форме добавления | frontend/ | Средняя |
| 9 | TMDB-интеграция (постер, метаданные) | api/ или worker/ | Средняя |

---

## Docker Compose (целевой)

```yaml
services:
  postgres:     # БД
  redis:        # Очереди
  prowlarr:     # Индексер торрентов
  qbittorrent:  # Загрузчик
  api:          # Admin API + Public API (порт 8000)
  worker:       # Downloader + Converter
  frontend:     # Админка (порт 3000)
  player:       # Плеер — embed-страница (порт 3001)  ← новый
  nginx:        # Раздача HLS-файлов (порт 80/443)    ← новый
```

### Монтирование томов
```yaml
# api, worker, nginx — все видят одну и ту же папку
volumes:
  - ${MEDIA_PATH:-./media}:/media
```

```
nginx:
  location /stream/  → /media/library/   (public, cached)
  location /api/     → proxy api:8000

api:
  /media/library/{imdb_id}/thumbnail.jpg  → прямой ServeFile

worker:
  читает/пишет /media/downloads, /media/temp, /media/library
```

---

## Разделение по серверам

### Профили нагрузки компонентов

| Компонент | CPU | RAM | Диск | Сеть |
|-----------|-----|-----|------|------|
| PostgreSQL | Низкий | Средний | Высокий (I/O, SSD) | Низкая |
| Redis | Низкий | Низкий | Низкий | Низкая |
| API (Go) | Низкий | Низкий | Нет | Средняя |
| Frontend / Player | Минимальный | Минимальный | Нет | Низкая |
| Prowlarr | Низкий | Средний | Нет | Средняя |
| **qBittorrent** | Средний | Средний | **Критичный** (много файлов) | **Критичная** (входящая) |
| **Worker (ffmpeg)** | **Критичный** | Средний | **Высокий** (I/O) | Нет |
| **nginx (HLS)** | Низкий | Низкий | Нет | **Критичная** (исходящая) |

Из таблицы очевидны три разные роли серверов:
- **Обработка**: CPU + диск (Worker + qBittorrent)
- **Управление**: стабильность, БД, очереди (API + DB + Redis)
- **Раздача**: пропускная способность сети (nginx / CDN)

---

### Вариант A — Один сервер (текущий, до ~20 фильмов/месяц)

```
┌───────────────────────────────────────────────┐
│  Один хост (8 CPU / 16 GB RAM / 2 TB HDD)    │
│                                               │
│  postgres  redis  prowlarr  qbittorrent       │
│  api  worker  frontend  player  nginx         │
│                                               │
│  /media  (все сервисы через bind mount)       │
└───────────────────────────────────────────────┘
```

**Плюсы:** один docker-compose, ноль накладных расходов.
**Минусы:** ffmpeg во время конвертации съедает CPU и мешает API.

---

### Вариант B — Два сервера (рекомендуется на старте)

Естественный разрез — **обработка** отдельно от **управления и раздачи**.

```
┌──────────────────────────┐     ┌──────────────────────────────────┐
│  Сервер 1: Control       │     │  Сервер 2: Processing            │
│  (2-4 CPU / 4-8 GB RAM)  │     │  (8-16 CPU / 8-16 GB RAM / 4 TB) │
│                          │     │                                  │
│  postgres                │     │  qbittorrent                     │
│  redis                   │◄────►  worker (downloader + converter)  │
│  api (admin + public)    │     │  prowlarr                        │
│  frontend                │     │                                  │
│  player                  │     │  /media  (локальный диск)        │
│  nginx  ─────────────────┼─────►  монтируется через NFS           │
│  (HLS-раздача)           │     │                                  │
└──────────────────────────┘     └──────────────────────────────────┘
          │
     Internet (80/443)
```

**Взаимодействие между серверами:**
- Worker → PostgreSQL/Redis: TCP (внутренняя сеть / VPN)
- nginx → /media/library: NFS-монтирование с Сервера 2 **или** Worker пушит готовые файлы на S3
- API → Worker: только через Redis-очередь (никакого прямого соединения)

**NFS vs S3 для файлов:**

| | NFS | S3 / Object Storage |
|--|-----|---------------------|
| Настройка | Просто | Средне |
| Стоимость | 0 (свой железо) | По трафику |
| Надёжность | Зависит от сети | Высокая |
| CDN | Нельзя напрямую | Да (CloudFront, etc.) |
| **Вывод** | Для двух серверов в одной сети | При росте нагрузки |

---

### Вариант C — Три сервера (продакшен, ~100+ фильмов)

```
┌────────────────────┐   ┌────────────────────┐   ┌────────────────────┐
│  Сервер 1: Data    │   │  Сервер 2: App     │   │  Сервер 3: Worker  │
│  (SSD обязателен)  │   │  (2-4 CPU / 4 GB)  │   │  (16 CPU / 16 GB)  │
│                    │   │                    │   │                    │
│  postgres          │◄──►  api               │◄──►  worker (ffmpeg)   │
│  redis             │   │  frontend          │   │  qbittorrent       │
│                    │   │  player            │   │  prowlarr          │
│                    │   │  nginx (→ S3/CDN)  │   │                    │
└────────────────────┘   └────────────────────┘   └────────────────────┘
                                  │
                         ┌────────▼────────┐
                         │  CDN / S3       │
                         │  HLS-сегменты   │
                         │  master.m3u8    │
                         └─────────────────┘
```

Сервер 3 (Worker) загружает готовые HLS-файлы в S3 вместо локального диска.
nginx на Сервере 2 проксирует `/stream/` → S3 (или отдаёт напрямую через presigned URL).

---

### Вариант D — Горизонтальное масштабирование (несколько worker-ов)

Worker не хранит состояния — масштабировать можно запуском нескольких экземпляров:

```
redis (convert_queue)
        │
   ┌────┴────┐
   ▼         ▼
Worker-1   Worker-2   ...
(ffmpeg)   (ffmpeg)

Общий NFS / S3 для /media
```

**Важно:** Distributed lock через Redis (`SETNX job_lock:{job_id}`) уже реализован в коде — один job не будет обработан двумя воркерами одновременно.

---

### Рекомендованная стратегия роста

```
Этап 1: Один сервер
  → всё в одном docker-compose

Этап 2: Worker на отдельный сервер
  → переезд qbittorrent + worker + prowlarr на Сервер 2
  → /media через NFS или rsync→S3 после конвертации

Этап 3: Вынести БД на managed PostgreSQL (RDS, Supabase, etc.)
  → Сервер 1 освобождается от I/O БД
  → Redis → Upstash или Valkey-кластер

Этап 4: nginx → CDN (CloudFront / Cloudflare)
  → Worker пишет HLS в S3
  → CDN раздаёт .ts и .m3u8 без нагрузки на ваши серверы
  → playlist_url в movies становится: https://cdn.example.com/tt1234567/master.m3u8
```

---

### Что не стоит разделять

| Пара | Почему держать вместе |
|------|----------------------|
| **qBittorrent + Worker** | Worker ждёт завершения торрента (polling), сетевой hop добавляет задержку |
| **API + Redis** | API пишет в очередь синхронно при каждом POST /jobs, latency критична |
| **nginx + /media** | Без общего диска/S3 nginx не сможет отдавать `.ts` файлы |
