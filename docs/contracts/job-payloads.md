# Job Payload Contracts (`download` / `remote_download` / `convert`)

Версия схемы: `v1`
Назначение: зафиксировать единый payload и правила обработки очередей для `movie` pipeline.

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
- `content_type` (enum: `movie`; `series` reserved)
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

Поля:

- `input_path` (string, required)
- `output_path` (string, required)
- `output_profile` (string, required) — единственный поддерживаемый: `mp4_h264_aac_1080p`
- `final_dir` (string, required)
- `imdb_id` (string, optional)
- `tmdb_id` (string, optional)
- `title` (string, optional)

Очередь: `convert_queue`
Обработчик: `worker/internal/converter`
`max_attempts`: 5

## 5) State model

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

## 6) Retry policy

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

## 7) Идемпотентность

- Для входа в pipeline используется `request_id` (на уровне create-job).
- Worker обязан проверять, что `job_id` уже не завершен перед повторной обработкой.
- Publish `convert` job допускается только один раз для пары (`job_id`, `stage=convert`).

## 8) Совместимость и расширение под сериалы

- `content_type` обязателен и уже включен в envelope.
- Добавление сериалов выполняется расширением `payload` и `job_type`-стратегий без изменения существующих обязательных полей.
- Новые необязательные поля (`season`, `episode`, `batch_mode`) добавляются backward-compatible.
