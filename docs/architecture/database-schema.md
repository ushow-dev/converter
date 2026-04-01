# Database Schema

PostgreSQL. Миграции: `api/internal/db/migrations/`, применяются автоматически при старте API.

---

## Схема связей

```
┌─────────────────────────────────────────────────────────────────────┐
│                          search_results                             │
│  external_id PK │ title │ source_type │ source_ref │ size_bytes     │
│  seeders │ leechers │ indexer │ content_type │ created_at           │
│                                                                     │
│  (кэш результатов поиска — не связан с другими таблицами)           │
└─────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────┐
│              media_jobs              │
│  job_id PK                           │
│  content_type  title  priority       │
│  source_type   source_ref            │
│  status  stage  progress_percent     │
│  error_code  error_message  retryable│
│  request_id UNIQUE  correlation_id   │
│  created_at  updated_at              │
└──────┬───────────────────────────────┘
       │ 1
       │
       │ 1                         ┌────────────────────────────────────┐
       ▼                           │              movies                │
┌──────────────────────────────┐   │  id PK (BIGSERIAL)                 │
│          media_assets        │   │  storage_key UNIQUE                │
│  asset_id PK                 │   │  imdb_id UNIQUE (nullable)         │
│  job_id FK → media_jobs      │   │  tmdb_id UNIQUE (nullable)         │
│  movie_id FK → movies    ────┼──▶│  title  year  poster_url           │
│  storage_path                │   │  storage_location_id FK ──────┐   │
│  thumbnail_path              │   │  created_at  updated_at        │   │
│  duration_sec                │   └────────────┬───────────────────┘   │
│  video_codec  audio_codec    │                │ 1           │ N        │
│  is_ready                    │                │             ▼          │
│  created_at  updated_at      │   ┌────────────▼───────┐  ┌──────────────────────────┐
└──────────────────────────────┘   │  movie_subtitles   │  │    storage_locations     │
                                   │  id PK             │  │  id PK (1=local, 2=remote│
┌──────────────────────────────┐   │  movie_id FK       │  │  name UNIQUE             │
│           job_events         │   │  language          │  │  type (local/sftp/s3)    │
│  event_id PK                 │   │  source            │  │  base_url                │
│  job_id FK → media_jobs      │   │  storage_path      │  │  is_active               │
│  event_type  payload JSONB   │   │  external_id       │  └──────────────────────────┘
│  created_at                  │   │  created_at        │
└──────────────────────────────┘   └────────────────────┘
```

---

## Таблицы

### `media_jobs` — задания на скачивание и конвертацию

| Колонка | Тип | Описание |
|---|---|---|
| `job_id` | TEXT PK | Уникальный ID задания (`job_<hex>`) |
| `content_type` | TEXT | Тип контента: `movie` |
| `source_type` | TEXT | Источник: `torrent`, `upload`, `http` |
| `source_ref` | TEXT | Magnet/URL/имя файла |
| `title` | TEXT | Название (из поиска или загрузки) |
| `priority` | TEXT | `low`, `normal`, `high` |
| `status` | TEXT | `created`, `queued`, `in_progress`, `completed`, `failed` |
| `stage` | TEXT | `download`, `convert`, `transfer` — текущий этап |
| `progress_percent` | INTEGER | Прогресс 0–100 |
| `error_code` | TEXT | Код ошибки при `status=failed` |
| `error_message` | TEXT | Сообщение ошибки |
| `retryable` | BOOLEAN | Можно ли повторить |
| `request_id` | TEXT UNIQUE | Idempotency key (дедупликация запросов) |
| `correlation_id` | TEXT | Трассировочный ID запроса |
| `series_id` | BIGINT FK → `series` | Связанный сериал (если задание — эпизод), nullable |
| `season_number` | INT | Номер сезона, nullable |
| `episode_number` | INT | Номер эпизода, nullable |
| `created_at` | TIMESTAMPTZ | Время создания |
| `updated_at` | TIMESTAMPTZ | Время последнего изменения |

**Индексы:** `status`, `created_at DESC`

---

### `media_assets` — готовые HLS-ассеты

| Колонка | Тип | Описание |
|---|---|---|
| `asset_id` | TEXT PK | Уникальный ID ассета |
| `job_id` | TEXT FK → `media_jobs` UNIQUE | Одно задание = один ассет |
| `movie_id` | BIGINT FK → `movies` NOT NULL | Связанный фильм |
| `storage_path` | TEXT | Абсолютный путь до `master.m3u8` |
| `thumbnail_path` | TEXT | Абсолютный путь до `thumbnail.jpg` (nullable) |
| `duration_sec` | INTEGER | Длительность видео в секундах |
| `video_codec` | TEXT | Например `h264` |
| `audio_codec` | TEXT | Например `aac` |
| `is_ready` | BOOLEAN | `true` — конвертация завершена, файлы доступны |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Индексы:** `job_id` (UNIQUE), `movie_id`

---

### `movies` — каталог фильмов

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | Внутренний ID |
| `storage_key` | TEXT UNIQUE NOT NULL | Ключ папки на диске (`Title (Year)`) |
| `imdb_id` | TEXT UNIQUE | IMDb ID (`tt1234567`), nullable |
| `tmdb_id` | TEXT UNIQUE | TMDB ID (`12345`), nullable |
| `title` | TEXT | Название фильма, nullable |
| `year` | INTEGER | Год выхода, nullable |
| `poster_url` | TEXT | URL постера TMDB, nullable |
| `storage_location_id` | BIGINT FK → `storage_locations` | Где хранится файл; NULL = local |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Индексы:** `storage_key` (UNIQUE), `imdb_id` WHERE NOT NULL (UNIQUE), `tmdb_id` WHERE NOT NULL (UNIQUE)

> Partial UNIQUE индексы на `imdb_id` и `tmdb_id` позволяют хранить несколько фильмов без внешних ID, но не допускают дублей когда ID задан.

---

### `storage_locations` — хранилища медиафайлов

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | `1` = local (зарезервировано миграцией) |
| `name` | TEXT UNIQUE | Название: `local`, `myremote` |
| `type` | TEXT | `local`, `sftp`, `s3` |
| `base_url` | TEXT | HTTP base URL для плеера (пустая строка = использовать `MEDIA_BASE_URL`) |
| `is_active` | BOOLEAN | Активно ли хранилище |
| `created_at` | TIMESTAMPTZ | |

> `id=1` — всегда local (вставляется миграцией 010). `id=2` — remote (вставляется миграцией 011). Плеер использует `base_url` из этой таблицы для построения URL сегментов HLS.

---

### `movie_subtitles` — субтитры

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `movie_id` | BIGINT FK → `movies` ON DELETE CASCADE | |
| `language` | TEXT | ISO 639-1 код (`en`, `ru`, `hi`) |
| `source` | TEXT | `opensubtitles` или `upload` |
| `storage_path` | TEXT | Абсолютный путь до `.vtt` файла |
| `external_id` | TEXT | ID на OpenSubtitles, nullable |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Ограничение:** UNIQUE `(movie_id, language)` — один язык на фильм.
**Индексы:** `movie_id`

---

### `series` — сериалы

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | Внутренний ID |
| `storage_key` | TEXT UNIQUE NOT NULL | Ключ папки на диске |
| `tmdb_id` | TEXT | TMDB ID, nullable |
| `imdb_id` | TEXT | IMDb ID, nullable |
| `title` | TEXT NOT NULL | Название сериала |
| `year` | INT | Год начала показа, nullable |
| `poster_url` | TEXT | URL постера, nullable |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Индексы:** `tmdb_id` WHERE NOT NULL, `imdb_id` WHERE NOT NULL

---

### `seasons` — сезоны

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `series_id` | BIGINT FK → `series` ON DELETE CASCADE | |
| `season_number` | INT NOT NULL | Номер сезона |
| `poster_url` | TEXT | URL постера сезона, nullable |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Ограничение:** UNIQUE `(series_id, season_number)`

---

### `episodes` — эпизоды

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `season_id` | BIGINT FK → `seasons` ON DELETE CASCADE | |
| `episode_number` | INT NOT NULL | Номер эпизода |
| `title` | TEXT | Название эпизода, nullable |
| `storage_key` | TEXT UNIQUE NOT NULL | Ключ папки на диске |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Ограничение:** UNIQUE `(season_id, episode_number)`

---

### `episode_assets` — HLS-ассеты эпизодов

| Колонка | Тип | Описание |
|---|---|---|
| `asset_id` | TEXT PK | |
| `job_id` | TEXT FK → `media_jobs` | nullable |
| `episode_id` | BIGINT FK → `episodes` ON DELETE CASCADE NOT NULL | |
| `storage_path` | TEXT NOT NULL | Путь до `master.m3u8` |
| `thumbnail_path` | TEXT | nullable |
| `duration_sec` | INT | nullable |
| `video_codec` | TEXT | nullable |
| `audio_codec` | TEXT | nullable |
| `is_ready` | BOOLEAN DEFAULT false | |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Индексы:** `episode_id`

---

### `episode_subtitles` — субтитры эпизодов

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `episode_id` | BIGINT FK → `episodes` ON DELETE CASCADE NOT NULL | |
| `language` | TEXT NOT NULL | ISO 639-1 код |
| `source` | TEXT NOT NULL DEFAULT `opensubtitles` | |
| `storage_path` | TEXT NOT NULL | |
| `external_id` | TEXT | ID на OpenSubtitles, nullable |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Ограничение:** UNIQUE `(episode_id, language)`

---

### `audio_tracks` — аудиодорожки (фильмы и эпизоды)

| Колонка | Тип | Описание |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `asset_id` | TEXT NOT NULL | ID ассета (`media_assets.asset_id` или `episode_assets.asset_id`) |
| `asset_type` | TEXT NOT NULL | `movie` или `episode` |
| `track_index` | INT NOT NULL | Индекс дорожки в потоке |
| `language` | TEXT | ISO 639-1 код, nullable |
| `label` | TEXT | Отображаемое имя, nullable |
| `is_default` | BOOLEAN DEFAULT false | |

**Ограничение:** UNIQUE `(asset_id, asset_type, track_index)`
**Индексы:** `(asset_id, asset_type)`

---

### `job_events` — лог событий задания

| Колонка | Тип | Описание |
|---|---|---|
| `event_id` | TEXT PK | |
| `job_id` | TEXT FK → `media_jobs` | |
| `event_type` | TEXT | Тип события |
| `payload` | JSONB | Произвольные данные события |
| `created_at` | TIMESTAMPTZ | |

**Индексы:** `job_id`

> Таблица создана для будущего event-sourcing. В текущей реализации не используется активно.

---

### `search_results` — кэш поиска торрентов

| Колонка | Тип | Описание |
|---|---|---|
| `external_id` | TEXT PK | ID релиза у индексера |
| `title` | TEXT | Название релиза |
| `source_type` | TEXT | `torrent` |
| `source_ref` | TEXT | Magnet-ссылка |
| `size_bytes` | BIGINT | Размер файла |
| `seeders` | INTEGER | |
| `leechers` | INTEGER | |
| `indexer` | TEXT | Имя индексера (Prowlarr) |
| `content_type` | TEXT | `movie` |
| `created_at` | TIMESTAMPTZ | |

> Не связана с другими таблицами — чистый кэш ответов Prowlarr.

---

## Жизненный цикл записей

```
Поиск → search_results (кэш)
           │
           ▼ пользователь выбирает релиз
        media_jobs (status=queued)
           │
           ▼ worker скачивает
        media_jobs (status=in_progress, stage=download)
           │
           ▼ worker конвертирует
        media_jobs (status=in_progress, stage=convert)
        movies (UPSERT по imdb_id / tmdb_id / storage_key)
           │
           ▼ конвертация завершена
        media_assets (is_ready=true)
        media_jobs (status=completed)
        movie_subtitles (опционально)
           │
           ▼ transfer (если RCLONE_REMOTE задан)
        media_jobs (status=in_progress, stage=transfer)
           │
           ▼ rclone move завершён
        movies.storage_location_id = 2 (remote)
        media_jobs (status=completed)
```
