# Converter: текущая реализация

## Принципы

- каталог хранения строится по `movies.storage_key`;
- `movies` — единственный источник истины о готовых фильмах (title, year, imdb_id, tmdb_id);
- связь между артефактом и фильмом хранится явно в `media_assets.movie_id`;
- `imdb_id` и `tmdb_id` в `movies` опциональные;
- API возвращает URL медиа по `storage_key`.

## Модель данных

### `movies`

- `id BIGSERIAL PRIMARY KEY`;
- `storage_key TEXT NOT NULL UNIQUE`;
- `title TEXT NULL`;
- `year INTEGER NULL`;
- `imdb_id TEXT NULL` (unique only when not null);
- `tmdb_id TEXT NULL` (unique only when not null);
- `poster_url TEXT NULL`;
- `created_at TIMESTAMPTZ NOT NULL`;
- `updated_at TIMESTAMPTZ NOT NULL`.

### `media_jobs`

Поле `title TEXT NULL` хранит название напрямую (используется для upload- и http-заданий).
Для торрент-заданий название берётся из `search_results` через JOIN (`COALESCE(j.title, sr.title)`).
Поле `source_type` принимает значения `torrent` | `upload` | `http`.

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

### `movie_subtitles`

- `id BIGSERIAL PRIMARY KEY`;
- `movie_id BIGINT NOT NULL REFERENCES movies(id) ON DELETE CASCADE`;
- `language TEXT NOT NULL` — ISO 639-1 (например `ru`, `en`);
- `source TEXT NOT NULL` — `opensubtitles` | `upload`;
- `storage_path TEXT NOT NULL` — абсолютный путь к `.vtt` файлу на диске;
- `external_id TEXT NULL` — file_id от OpenSubtitles (NULL для ручной загрузки);
- `created_at TIMESTAMPTZ NOT NULL`;
- `updated_at TIMESTAMPTZ NOT NULL`;
- `UNIQUE (movie_id, language)` — один трек на язык на фильм.

Субтитры хранятся рядом с видео: `/media/converted/<storage_key>/subtitles/<lang>.vtt`.

## Флоу добавления фильма

### Торрент (source_type = torrent)

1. Пользователь ищет фильм через `/search`, вводит IMDb ID / TMDB ID, выбирает раздачу.
2. API создаёт job и кладёт `DownloadPayload` (с полем `title`) в `download_queue`.
3. Worker-downloader: скачивает торрент в `/media/downloads/<job_id>/`, затем кладёт `ConvertMessage` (с `title`) в `convert_queue`.
4. Worker-converter: ffmpeg → upsert movies (с title) → move files → create asset → job completed → **поиск субтитров**.

### Локальная загрузка (source_type = upload)

1. Пользователь открывает `/upload` → вкладку **«Локальная загрузка»**.
2. Вводит TMDB ID → API проксирует запрос к TMDB (`GET /api/admin/movies/tmdb/<id>`) → возвращает `title` и `imdb_id`, поля подставляются автоматически.
3. Пользователь выбирает локальный видеофайл и нажимает «Загрузить фильм».
4. Браузер отправляет `POST /api/admin/jobs/upload` (multipart) с полями `file`, `title`, `imdb_id`, `tmdb_id`.
5. API:
  - сохраняет файл в `/media/downloads/<job_id>/<filename>`;
  - создаёт job (`source_type=upload`, `title` сохраняется в `media_jobs.title`);
  - кладёт `ConvertPayload` (с `title`) **напрямую в `convert_queue`** (download worker пропускается).
6. Worker-converter: то же самое что для торрента — ffmpeg → upsert movies → move files → create asset → job completed → **поиск субтитров**.

### Удалённая загрузка по HTTP (source_type = http)

1. Пользователь открывает `/upload` → вкладку **«Удалённый каталог»**.
2. Вводит URL HTTP-директории (например `http://ftp.example.com/Hindi Movies/`).
3. API обходит директорию и вложенные подпапки, возвращает список фильмов с именем видеофайла, размером и наличием SRT.
4. Пользователь отмечает нужные фильмы галочками и нажимает **«Скачать»**.
5. Для каждого выбранного фильма браузер отправляет `POST /api/admin/jobs/remote-download` — параллельно.
6. API:
  - извлекает название и год из имени файла (regex `^(.+?)\s*\((\d{4})\)`);
  - **автоматически ищет фильм в TMDB** по названию/году (best-effort, нет ключа — пропускается);
  - создаёт job (`source_type=http`, `title` и `tmdb_id` сохраняются);
  - кладёт `RemoteDownloadPayload` в `remote_download_queue`.
7. Worker-httpdownloader: скачивает файл по HTTP в `/media/downloads/<job_id>/<filename>` с отслеживанием прогресса → кладёт `ConvertMessage` в `convert_queue`.
8. Worker-converter: то же самое что для торрента.

## Конвертация (worker)

1. Входной файл уже лежит в `downloads` (либо скачан торрентом/HTTP, либо загружен через UI).
2. ffmpeg конвертирует в HLS во временную директорию.
3. Worker upsert-ит `movies` (с title из `ConvertMessage.Payload.Title`) и получает `storage_key`.
4. Итоговые файлы перемещаются в:
  - `/media/converted/<storage_key>/master.m3u8`
  - `/media/converted/<storage_key>/thumbnail.jpg` (если есть).
5. В `media_assets` создается запись c `movie_id`.
6. Job помечается `completed`.
7. **Поиск субтитров** (non-fatal): если задан `TMDB ID` и `OPENSUBTITLES_API_KEY` — воркер запрашивает OpenSubtitles.com, скачивает SRT, конвертирует в VTT, сохраняет в `/media/converted/<storage_key>/subtitles/<lang>.vtt`, создаёт записи в `movie_subtitles`. Ошибка при поиске субтитров **не** влияет на статус задания.

## Субтитры

### Автоматический поиск

Воркер вызывает OpenSubtitles.com REST API v1 после завершения конвертации:

- `GET /api/v1/subtitles?tmdb_id=<id>&languages=ru,en` — поиск (выбирается запись с наибольшим download_count на язык);
- `POST /api/v1/download` — получение одноразовой ссылки на скачивание;
- скачанный SRT конвертируется в WebVTT: добавляется заголовок `WEBVTT` и заменяется `,` на `.` в таймстемпах.

Требует `OPENSUBTITLES_API_KEY`. Если ключ не задан — поиск пропускается, ручная загрузка работает.

### Конфигурация

| Переменная              | По умолчанию | Назначение                              |
|-------------------------|--------------|-----------------------------------------|
| `OPENSUBTITLES_API_KEY` | (пусто)      | API-ключ OpenSubtitles.com              |
| `SUBTITLE_LANGUAGES`    | `ru,en`      | Языки через запятую (ISO 639-1)         |

### Хранение

```
/media/converted/<storage_key>/
├── master.m3u8
├── thumbnail.jpg
├── 720/
├── 480/
├── 360/
└── subtitles/
    ├── ru.vtt
    └── en.vtt
```

## API (Admin)

### `GET /api/admin/movies`

Возвращает список готовых фильмов из таблицы `movies`.
Поддерживает cursor-пагинацию (`cursor`, `limit`).
Ответ включает `id`, `storage_key`, `title`, `year`, `imdb_id`, `tmdb_id`, `has_thumbnail`, `job_id`.

### `GET /api/admin/movies/{movieId}/thumbnail`

Отдаёт JPEG-превью фильма. Авторизация: JWT через заголовок или `?token=`.

### `GET /api/admin/movies/{movieId}/subtitles`

Возвращает список субтитров фильма: `{ items: [{ id, movie_id, language, source, created_at, updated_at }] }`.

### `POST /api/admin/movies/{movieId}/subtitles`

Ручная загрузка субтитра. Multipart form: `language` (ISO 639-1) + `file` (.vtt или .srt).
SRT конвертируется в VTT на лету. Если трек для языка уже есть — перезаписывается.

### `POST /api/admin/movies/{movieId}/subtitles/search`

Принудительный повторный поиск субтитров через OpenSubtitles для данного фильма.
Требует `tmdb_id` у фильма и `OPENSUBTITLES_API_KEY` в окружении.
Возвращает обновлённый список субтитров + `found: N`.

### `POST /api/admin/jobs`

Создать job из торрента. Тело JSON: `source_type=torrent`, `source_ref` (magnet/hash), `imdb_id`, `tmdb_id`, `title`.

### `POST /api/admin/jobs/upload`

Загрузить локальный файл. Multipart form: `file`, `title`, `imdb_id`, `tmdb_id`, `request_id`.
Файл стримируется на сервер без буферизации в памяти. Лимит тела — 50 ГБ.

### `POST /api/admin/jobs/remote-download`

Поставить в очередь загрузку видеофайла по прямому HTTP(S) URL.

Тело JSON:
```json
{ "url": "http://ftp.example.com/Films/Movie%20(2025)/movie.mp4", "filename": "Movie (2025) Hindi 720p.mp4" }
```

Поведение:
- API парсит `filename`: извлекает название и год (regex `^(.+?)\s*\((\d{4})\)`);
- если задан `TMDB_API_KEY` — автоматически ищет фильм в TMDB по названию/году;
- создаёт job (`source_type=http`);
- кладёт сообщение в `remote_download_queue`.

Ответ `202 Accepted`: `{ job_id, status, title, tmdb_id, created_at }`.

### `GET /api/admin/movies/tmdb/{tmdbId}`

Проксирует запрос к TMDB API, возвращает `{ title, imdb_id, poster_url, overview, release_date }`.
Требует `TMDB_API_KEY` в окружении.

### `GET /api/admin/movies/tmdb/search?q=title&year=2025`

Поиск фильма в TMDB по названию (и опциональному году).
Возвращает лучший результат: `{ found, tmdb_id, title, year, poster_url }`.
Используется внутри флоу удалённой загрузки.

### `GET /api/admin/remote-browse?url=...`

Обходит HTTP-директорию (Apache/Nginx autoindex) на один уровень вглубь.
Для каждой подпапки ищет видеофайлы (`.mkv`, `.mp4`, `.avi`, `.mov`, `.m4v`, `.ts`, `.m2ts`) и субтитры (`.srt`).
Возвращает список `RemoteMovie`: `{ name, url, video_file: {name, size, url}, subtitle_files: [{name, size, url}] }`.

## API (Player)

### `GET /api/player/movie?imdb_id=...|tmdb_id=...`

- ищет фильм по `movies.imdb_id` или `movies.tmdb_id`;
- возвращает:
  - `movie.id`, `movie.imdb_id`, `movie.tmdb_id`;
  - `playback.hls` и `assets.poster` по `storage_key`;
  - **`subtitles[]`** — массив `{ language, url }` для всех найденных треков.

Формат ссылок:

- `/media/converted/<storage_key>/master.m3u8`
- `/media/converted/<storage_key>/thumbnail.jpg`
- `/media/converted/<storage_key>/subtitles/<lang>.vtt`

Если включен `MEDIA_SIGNING_KEY`, API добавляет `st` и `e` ко всем URL (включая субтитры).

### `GET /api/player/assets/{assetID}`

Возвращает playback URL на основе `media_assets.storage_path`.

---

## Таблицы базы данных

```
search_results ←— (source_ref) —— media_jobs ——→ media_assets ——→ movies
                                                                     ↓
                                                              movie_subtitles
```

### `movies` — каталог фильмов

| Колонка       | Тип          | Назначение                                                 |
|---------------|--------------|------------------------------------------------------------|
| `id`          | BIGSERIAL PK | внутренний идентификатор                                   |
| `storage_key` | TEXT UNIQUE  | имя папки в `/media/converted/` (например `mov_a1b2c3d4`) |
| `title`       | TEXT NULL    | название фильма                                            |
| `year`        | INTEGER NULL | год выпуска                                                |
| `imdb_id`     | TEXT NULL    | IMDb ID (уникальный, если указан)                          |
| `tmdb_id`     | TEXT NULL    | TMDB ID (уникальный, если указан)                          |
| `poster_url`  | TEXT NULL    | URL постера                                                |

Строка создаётся воркером в момент завершения конвертации.

### `media_jobs` — задания конвейера

| Колонка            | Назначение                                              |
|--------------------|-------------------------------------------------------------|
| `job_id`           | PK, уникальный ID задания                                   |
| `source_type`      | `torrent`, `upload` или `http`                              |
| `source_ref`       | magnet/hash, имя файла или HTTP URL                         |
| `title`            | название — для upload- и http-заданий                       |
| `status`           | `queued` → `in_progress` → `completed` / `failed`          |
| `stage`            | `download` или `convert` (текущий этап)                     |
| `progress_percent` | 0–100                                                       |
| `request_id`       | ключ идемпотентности                                        |

### `media_assets` — готовые артефакты

Создаётся воркером после успешной конвертации. Один job = один asset (`UNIQUE(job_id)`).

| Колонка                                      | Назначение                                     |
|----------------------------------------------|------------------------------------------------|
| `job_id`                                     | FK → media_jobs                                |
| `movie_id`                                   | FK → movies (NOT NULL)                         |
| `storage_path`                               | путь к `master.m3u8`                           |
| `thumbnail_path`                             | путь к `thumbnail.jpg`                         |
| `duration_sec`, `video_codec`, `audio_codec` | технические метаданные                         |
| `is_ready`                                   | `true` когда файл доступен для воспроизведения |

### `movie_subtitles` — субтитры

Создаётся воркером после конвертации (автопоиск) или через API (ручная загрузка).
`UNIQUE(movie_id, language)` — повторный поиск/загрузка заменяет существующий трек.

| Колонка        | Назначение                                    |
|----------------|-----------------------------------------------|
| `movie_id`     | FK → movies                                   |
| `language`     | ISO 639-1 (`ru`, `en`, …)                     |
| `source`       | `opensubtitles` или `upload`                  |
| `storage_path` | абсолютный путь к `.vtt` файлу                |
| `external_id`  | file_id от OpenSubtitles (NULL при `upload`)  |

### `search_results` — кэш поиска

Результаты, возвращённые индексатором (Prowlarr). Кэшируются при поиске, используются для JOIN в списке заданий (`COALESCE(j.title, sr.title)`).

| Колонка                             | Назначение        |
|-------------------------------------|-------------------|
| `external_id`                       | PK от индексатора |
| `title`                             | название раздачи  |
| `source_ref`                        | magnet/hash       |
| `seeders`, `leechers`, `size_bytes` | статистика        |

### `job_events` — лог событий

Аудит-лог для отладки. Хранит события по заданию в виде JSONB. Сейчас не заполняется активно.

---

## Параллелизм

Воркер поддерживает параллелизм через три переменные окружения:

```yaml
DOWNLOAD_CONCURRENCY:      2   # параллельные торрент-загрузки
CONVERT_CONCURRENCY:       1   # параллельные конвертации (CPU-bound)
HTTP_DOWNLOAD_CONCURRENCY: 3   # параллельные HTTP-загрузки удалённых файлов
```

В `main.go` на каждый слот запускается отдельная горутина. Все три типа воркеров работают параллельно (разные очереди, разные горутины).

`ffmpeg` — CPU-bound задача. Рекомендуется `CONVERT_CONCURRENCY = количество физических ядер / 2`.

| Подход                                  | Как                                         | Когда имеет смысл                              |
|-----------------------------------------|---------------------------------------------|------------------------------------------------|
| Увеличить `CONVERT_CONCURRENCY`         | `CONVERT_CONCURRENCY=2` в docker-compose    | Много ядер CPU (8+), небольшие файлы           |
| Увеличить `DOWNLOAD_CONCURRENCY`        | `DOWNLOAD_CONCURRENCY=4`                    | Медленный торрент-трекер, быстрый интернет     |
| Увеличить `HTTP_DOWNLOAD_CONCURRENCY`   | `HTTP_DOWNLOAD_CONCURRENCY=5`               | Много фильмов из удалённого каталога сразу     |
| Несколько воркер-контейнеров            | Масштабировать `worker` в docker-compose    | Несколько физических машин                     |
