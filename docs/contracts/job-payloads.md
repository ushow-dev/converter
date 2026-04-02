# Job Payload Contracts (`download` / `remote_download` / `convert`)

Версия схемы: `v1`
Назначение: зафиксировать единый payload и правила обработки очередей для `movie` и `series` pipeline.

## 1) Базовый envelope

Все сообщения в очередях используют общий envelope:

```json
{
  "schema_version": "v1",
  "job_id": "job_01HZYJ9A",
  "job_type": "download",
  "content_type": "movie",
  "correlation_id": "c8d78757-476c-4972-b3e7-94eb7df677c6",
  "attempt": 1,
  "max_attempts": 5,
  "created_at": "2026-03-01T10:00:00Z",
  "payload": {}
}
```

Обязательные поля envelope:

- `schema_version` (string)
- `job_id` (string)
- `job_type` (enum: `download` | `remote_download` | `convert`)
- `content_type` (enum: `movie` | `episode`)
- `correlation_id` (uuid/string)
- `attempt` (int >= 1)
- `max_attempts` (int >= 1)
- `created_at` (datetime UTC)
- `payload` (object)

## 2) Download payload (torrent)

`job_type = "download"`

```json
{
  "source_type": "torrent",
  "source_ref": "magnet:?xt=urn:btih:...",
  "target_dir": "/media/downloads/job_01HZYJ9A",
  "priority": "normal",
  "request_id": "7c7f7f1a-09cc-4f6e-ae9f-a8e0e23cc1b3"
}
```

Поля:

- `source_type` (enum: `torrent`)
- `source_ref` (string, required)
- `target_dir` (string, required)
- `priority` (enum: `low` | `normal` | `high`, default `normal`)
- `request_id` (string, required for идемпотентность create-flow)

Очередь: `download_queue`
Обработчик: `worker/internal/downloader`
`max_attempts`: 5

## 3) Remote download payload (HTTP)

`job_type = "remote_download"`

Используется когда источник — прямая HTTP(S)-ссылка на видеофайл (например, из Apache/Nginx directory listing).

```json
{
  "source_url": "http://example.com/movies/Inception%20(2010)/Inception.mkv",
  "filename": "Inception.mkv",
  "imdb_id": "tt1375666",
  "tmdb_id": "27205",
  "title": "Inception",
  "target_dir": "/media/downloads/job_01HZYJ9A"
}
```

Поля:

- `source_url` (string, required) — полный HTTP(S)-адрес файла
- `filename` (string, required) — безопасное имя файла для сохранения на диск (специальные символы заменены на `_`)
- `imdb_id` (string, optional) — заполняется если найден на TMDB
- `tmdb_id` (string, optional) — заполняется автоматически через поиск TMDB по названию+году из filename
- `title` (string, optional) — заголовок, извлечённый из имени файла regex `^(.+?)\s*\((\d{4})\)`
- `target_dir` (string, required) — `/media/downloads/{job_id}`

Очередь: `remote_download_queue`
Обработчик: `worker/internal/httpdownloader`
`max_attempts`: 3
HTTP клиент: без таймаута (большие файлы); прогресс обновляется каждые 2%

После успешной загрузки воркер самостоятельно публикует `convert` payload в `convert_queue`.

## 4) Convert payload

`job_type = "convert"`

### Пример: фильм

```json
{
  "input_path": "/media/downloads/job_01HZYJ9A/source.mkv",
  "output_path": "/media/temp/job_01HZYJ9A/output.mp4",
  "output_profile": "mp4_h264_aac_1080p",
  "final_dir": "/media/converted/job_01HZYJ9A",
  "imdb_id": "tt1375666",
  "tmdb_id": "27205",
  "title": "Inception"
}
```

### Пример: эпизод сериала

```json
{
  "input_path": "/media/downloads/job_01HZYJ9B/source.mkv",
  "output_path": "/media/temp/job_01HZYJ9B/output.mp4",
  "output_profile": "mp4_h264_aac_1080p",
  "final_dir": "/media/converted/job_01HZYJ9B",
  "imdb_id": null,
  "tmdb_id": "1396",
  "title": "Breaking Bad",
  "series_id": "ser_01HZYJ01",
  "season_number": 1,
  "episode_number": 3
}
```

Поля:

- `input_path` (string, required)
- `output_path` (string, required)
- `output_profile` (string, required) — единственный поддерживаемый: `mp4_h264_aac_1080p`
- `final_dir` (string, required)
- `imdb_id` (string, optional)
- `tmdb_id` (string, optional)
- `title` (string, optional)
- `series_id` (string, nullable) — ID записи series в БД; заполняется только для эпизодов
- `season_number` (int, nullable) — номер сезона; заполняется только для эпизодов
- `episode_number` (int, nullable) — номер эпизода; заполняется только для эпизодов

Если `series_id` / `season_number` / `episode_number` не заданы (null), worker обрабатывает задание как фильм.

Очередь: `convert_queue`
Обработчик: `worker/internal/converter`
`max_attempts`: 5

## 5) Transfer payload

После успешной конвертации worker публикует сообщение в `transfer_queue` (если `RCLONE_REMOTE` задан).

```json
{
  "content_id": "mov_01HZYJ9A",
  "content_type": "movie",
  "storage_key": "inception_2010_[27205]",
  "local_path": "/media/converted/movies/inception_2010_[27205]"
}
```

Для эпизода:

```json
{
  "content_id": "ep_01HZYJ9B",
  "content_type": "episode",
  "storage_key": "breaking_bad_[1396]/s01/e03",
  "local_path": "/media/converted/series/breaking_bad_[1396]/s01/e03"
}
```

Поля:

- `content_id` (string, required) — ID записи в БД (movie ID или episode ID); ранее называлось `movie_id`, переименовано в `content_id` для поддержки обоих типов
- `content_type` (enum: `movie` | `episode`, required) — определяет тип записи и целевой путь rclone
- `storage_key` (string, required) — относительный путь в хранилище
- `local_path` (string, required) — абсолютный локальный путь к HLS-директории

Очередь: `transfer_queue`
Обработчик: `worker/internal/transfer`

## 6) State model

Состояния задач:

- `created`
- `queued`
- `in_progress`
- `completed`
- `failed`

Дополнительно:

- `stage` (`download` | `convert`)
- `progress_percent` (`0..100`)
- `error_code` / `error_message` (для `failed`)
- `retryable` (bool)

## 7) Retry policy

- Стратегия: exponential backoff.
- Базовая задержка: `5s`.
- Множитель: `x2`.
- Максимальная задержка: `5m`.
- Предел попыток: `max_attempts` (по умолчанию `5`).
- После исчерпания попыток: перевод в `failed` + публикация в DLQ.

Retryable классы ошибок:

- сетевые таймауты;
- временная недоступность `Prowlarr`/`qBittorrent`;
- временные ошибки I/O.

Non-retryable:

- невалидный `source_ref`;
- поврежденный payload;
- ошибка валидации обязательных полей.

## 8) Идемпотентность

- Для входа в pipeline используется `request_id` (на уровне create-job).
- Worker обязан проверять, что `job_id` уже не завершен перед повторной обработкой.
- Publish `convert` job допускается только один раз для пары (`job_id`, `stage=convert`).

## 9) Совместимость и расширение под сериалы

- `content_type` обязателен и уже включен в envelope; поддерживает значения `movie` и `episode`.
- Поля `series_id`, `season_number`, `episode_number` в ConvertPayload добавлены как nullable — backward-compatible с существующими movie-заданиями.
- `TransferPayload` использует `content_id` вместо `movie_id` — при развёртывании необходимо убедиться, что и API, и Worker обновлены одновременно.
