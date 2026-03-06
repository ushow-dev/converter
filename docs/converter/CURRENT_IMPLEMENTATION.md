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

Поле `title TEXT NULL` хранит название напрямую (используется для upload-заданий).
Для торрент-заданий название берётся из `search_results` через JOIN (`COALESCE(j.title, sr.title)`).
Поле `source_type` принимает значения `torrent` | `upload`.

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

## Флоу добавления фильма

### Торрент (source_type = torrent)

1. Пользователь ищет фильм через `/search`, вводит IMDb ID / TMDB ID, выбирает раздачу.
2. API создаёт job и кладёт `DownloadPayload` (с полем `title`) в `download_queue`.
3. Worker-downloader: скачивает торрент в `/media/downloads/<job_id>/`, затем кладёт `ConvertMessage` (с `title`) в `convert_queue`.
4. Worker-converter: ffmpeg → upsert movies (с title) → move files → create asset → job completed.

### Локальная загрузка (source_type = upload)

1. Пользователь открывает `/upload` в админке.
2. Вводит TMDB ID → API проксирует запрос к TMDB (`GET /api/admin/movies/tmdb/<id>`) → возвращает `title` и `imdb_id`, поля подставляются автоматически.
3. Пользователь выбирает локальный видеофайл и нажимает «Загрузить фильм».
4. Браузер отправляет `POST /api/admin/jobs/upload` (multipart) с полями `file`, `title`, `imdb_id`, `tmdb_id`.
5. API:
  - сохраняет файл в `/media/downloads/<job_id>/<filename>`;
  - создаёт job (`source_type=upload`, `title` сохраняется в `media_jobs.title`);
  - кладёт `ConvertPayload` (с `title`) **напрямую в `convert_queue`** (download worker пропускается).
6. Worker-converter: то же самое что для торрента — ffmpeg → upsert movies → move files → create asset → job completed.

## Конвертация (worker)

1. Входной файл уже лежит в `downloads` (либо скачан торрентом, либо загружен через UI).
2. ffmpeg конвертирует в HLS во временную директорию.
3. Worker upsert-ит `movies` (с title из `ConvertMessage.Payload.Title`) и получает `storage_key`.
4. Итоговые файлы перемещаются в:
  - `/media/converted/<storage_key>/master.m3u8`
  - `/media/converted/<storage_key>/thumbnail.jpg` (если есть).
5. В `media_assets` создается запись c `movie_id`.
6. Job помечается `completed`.

## API (Admin)

### `GET /api/admin/movies`

Возвращает список готовых фильмов из таблицы `movies`.
Поддерживает cursor-пагинацию (`cursor`, `limit`).
Ответ включает `id`, `storage_key`, `title`, `year`, `imdb_id`, `tmdb_id`, `has_thumbnail`, `job_id`.

### `GET /api/admin/movies/{movieId}/thumbnail`

Отдаёт JPEG-превью фильма. Авторизация: JWT через заголовок или `?token=`.

### `POST /api/admin/jobs`

Создать job из торрента. Тело JSON: `source_type=torrent`, `source_ref` (magnet/hash), `imdb_id`, `tmdb_id`, `title`.

### `POST /api/admin/jobs/upload`

Загрузить локальный файл. Multipart form: `file`, `title`, `imdb_id`, `tmdb_id`, `request_id`.
Файл стримируется на сервер без буферизации в памяти. Лимит тела — 50 ГБ.

### `GET /api/admin/movies/tmdb/{tmdbId}`

Проксирует запрос к TMDB API, возвращает `{ title, imdb_id }`.
Требует `TMDB_API_KEY` в окружении.

## API (Player)

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

В базе данных 5 таблиц:

---

### `movies` — каталог фильмов

Главная таблица. Хранит метаданные готового фильма.


| Колонка       | Тип          | Назначение                                                |
| ------------- | ------------ | --------------------------------------------------------- |
| `id`          | BIGSERIAL PK | внутренний идентификатор                                  |
| `storage_key` | TEXT UNIQUE  | имя папки в `/media/converted/` (например `mov_a1b2c3d4`) |
| `title`       | TEXT NULL    | название фильма                                           |
| `year`        | INTEGER NULL | год выпуска                                               |
| `imdb_id`     | TEXT NULL    | IMDb ID (уникальный, если указан)                         |
| `tmdb_id`     | TEXT NULL    | TMDB ID (уникальный, если указан)                         |
| `poster_url`  | TEXT NULL    | URL постера                                               |


Строка создаётся воркером в момент завершения конвертации.

---

### `media_jobs` — задания конвейера

Отслеживает жизненный цикл одной задачи (скачать + конвертировать или просто конвертировать).


| Колонка            | Назначение                                        |
| ------------------ | ------------------------------------------------- |
| `job_id`           | PK, уникальный ID задания                         |
| `source_type`      | `torrent` или `upload`                            |
| `source_ref`       | magnet/hash или имя загруженного файла            |
| `title`            | название — только для upload-заданий              |
| `status`           | `queued` → `in_progress` → `completed` / `failed` |
| `stage`            | `download` или `convert` (текущий этап)           |
| `progress_percent` | 0–100                                             |
| `request_id`       | ключ идемпотентности                              |


---

### `media_assets` — готовые артефакты

Создаётся воркером после успешной конвертации. Один job = один asset (`UNIQUE(job_id)`).


| Колонка                                      | Назначение                                     |
| -------------------------------------------- | ---------------------------------------------- |
| `job_id`                                     | FK → media_jobs                                |
| `movie_id`                                   | FK → movies (NOT NULL)                         |
| `storage_path`                               | путь к `master.m3u8`                           |
| `thumbnail_path`                             | путь к `thumbnail.jpg`                         |
| `duration_sec`, `video_codec`, `audio_codec` | технические метаданные                         |
| `is_ready`                                   | `true` когда файл доступен для воспроизведения |


Связывает `media_jobs` ↔ `movies`.

---

### `search_results` — кэш поиска

Результаты, возвращённые индексатором (Prowlarr). Кэшируются при поиске, используются для JOIN в списке заданий (`COALESCE(j.title, sr.title)`).


| Колонка                             | Назначение        |
| ----------------------------------- | ----------------- |
| `external_id`                       | PK от индексатора |
| `title`                             | название раздачи  |
| `source_ref`                        | magnet/hash       |
| `seeders`, `leechers`, `size_bytes` | статистика        |


---

### `job_events` — лог событий

Аудит-лог для отладки. Хранит события по заданию в виде JSONB. Сейчас не заполняется активно, таблица создана для будущего использования.

---

**Ключевые связи:**

```
search_results ←— (source_ref) —— media_jobs ——→ media_assets ——→ movies
```



сейчас одновременно можно загружать только 1 фильм, конвертируется тоже только один фильм, остальные стоят в очереди. Как можно распаралеллить этот процесс? Не меняй код, только опиши





- 
- 
- 
- 
- 



1. 



1. 
2. 
3. 
4. 



**Текущая архитектура**

Воркер уже поддерживает параллелизм через две переменные окружения:

```yaml
DOWNLOAD_CONCURRENCY: 2   # по умолчанию — 2 загрузки параллельно
CONVERT_CONCURRENCY:  1   # по умолчанию — 1 конвертация

```

В `main.go` на каждый слот запускается отдельная горутина, читающая из своей очереди. Загрузка и конвертация уже идут параллельно друг с другом (разные очереди, разные горутины).

---

**Почему конвертация ограничена 1**

`ffmpeg` — CPU-bound задача. Один HLS-энкод в `libx264` на 1080p может загружать все ядра процессора на 100%. Если запустить два энкода одновременно:

- они делят одни и те же ядра → каждый работает вдвое медленнее
- суммарная пропускная способность примерно та же, но оба фильма долго висят в `in_progress`
- пиковое потребление RAM удваивается (~1–2 ГБ на энкод)

---

**Варианты распараллеливания**


| Подход                           | Как                                      | Когда имеет смысл                            |
| -------------------------------- | ---------------------------------------- | -------------------------------------------- |
| Увеличить `CONVERT_CONCURRENCY`  | `CONVERT_CONCURRENCY=2` в docker-compose | Много ядер CPU (8+), небольшие файлы         |
| Увеличить `DOWNLOAD_CONCURRENCY` | `DOWNLOAD_CONCURRENCY=4`                 | Медленный торрент-трекер, быстрый интернет   |
| Несколько воркер-контейнеров     | Масштабировать `worker` в docker-compose | Несколько физических машин или мощный сервер |
| Снизить качество энкода          | Изменить ffmpeg-профиль (CRF, preset)    | Нужна скорость важнее качества               |


---



**Конвертация и очередь заданий**

**Практическая рекомендация**

Для одиночного сервера оптимально:

```
CONVERT_CONCURRENCY = количество физических ядер / 2

```

Например, на 4-ядерном сервере → `CONVERT_CONCURRENCY=2`. Каждый энкод получит по 2 ядра, итоговое время конвертации одного фильма вырастет незначительно, зато два фильма завершатся примерно одновременно.

Изменить это можно **только в** `docker-compose.yml` без пересборки образов — это просто env-переменные.