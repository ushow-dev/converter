# Converter: текущая реализация

## Принципы

- каталог хранения строится по `movies.storage_key`;
- связь между артефактом и фильмом хранится явно в `media_assets.movie_id`;
- `imdb_id` и `tmdb_id` в `movies` опциональные;
- API возвращает URL медиа по `storage_key`.

## Модель данных

### `movies`

- `id BIGSERIAL PRIMARY KEY`;
- `storage_key TEXT NOT NULL UNIQUE`;
- `imdb_id TEXT NULL` (unique only when not null);
- `tmdb_id TEXT NULL` (unique only when not null);
- `poster_url TEXT NULL`;
- `created_at TIMESTAMPTZ NOT NULL`;
- `updated_at TIMESTAMPTZ NOT NULL`.

### `media_assets`

- `asset_id TEXT PRIMARY KEY`;
- `job_id TEXT NOT NULL REFERENCES media_jobs(job_id)`;
- `movie_id BIGINT NOT NULL REFERENCES movies(id)`;
- `storage_path TEXT NOT NULL`;
- `thumbnail_path TEXT NULL`;
- `duration_sec INTEGER NULL`;
- `video_codec TEXT NULL`;
- `audio_codec TEXT NULL`;
- `is_ready BOOLEAN NOT NULL`;
- `created_at TIMESTAMPTZ NOT NULL`;
- `updated_at TIMESTAMPTZ NOT NULL`;
- один job -> один asset (`UNIQUE (job_id)`).

## Конвертация (worker)

1. Файл скачивается в `downloads`.
2. ffmpeg конвертирует во временную директорию.
3. Worker upsert-ит `movies` и получает `storage_key`.
4. Итоговые файлы перемещаются в:
   - `/media/converted/<storage_key>/master.m3u8`
   - `/media/converted/<storage_key>/thumbnail.jpg` (если есть).
5. В `media_assets` создается запись c `movie_id`.
6. Job помечается `completed`.

## API

### `GET /api/player/movie?imdb_id=...|tmdb_id=...`

- ищет фильм по `movies.imdb_id` или `movies.tmdb_id`;
- возвращает:
  - `movie.id`,
  - `movie.imdb_id` / `movie.tmdb_id` (могут быть `null`),
  - `playback.hls` и `assets.poster` по `storage_key`.

Формат ссылок:

- `/media/converted/<storage_key>/master.m3u8`
- `/media/converted/<storage_key>/thumbnail.jpg`

Если включен `MEDIA_SIGNING_KEY`, API добавляет `st` и `e`.

### `GET /api/player/assets/{assetID}`

- возвращает playback URL на основе `media_assets.storage_path`.
