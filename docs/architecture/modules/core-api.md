# Модуль: Core API (Go)

## Назначение

Центральный backend-модуль, который:

- реализует бизнес-правила;
- хранит и отдает состояние задач;
- управляет жизненным циклом media jobs;
- предоставляет API для admin UI и плеера.

## Границы ответственности

Входит в модуль:

- auth и авторизация;
- API-контракты;
- валидация входных данных;
- запись/чтение состояния в PostgreSQL;
- постановка задач в очередь;
- интеграция поиска через `IndexerProvider` (backend `Prowlarr`).

Не входит в модуль:

- прямое выполнение download/convert;
- обработка ffmpeg;
- управление torrent-клиентом.

## Интерфейсы и зависимости

Входящие:

- `Admin API` для поиска и управления задачами;
- `Player API` для получения статуса и данных воспроизведения.

Исходящие:

- `PostgreSQL` (repository layer);
- `Redis` (job enqueue);
- `IndexerProvider` (поиск в источниках через `Prowlarr`).

## Контракты данных

Ключевые сущности:

- `media_job` (`job_id`, `content_type`, `source_type`, `status`, `progress_percent`);
- `media_asset` (`asset_id`, `job_id`, `storage_path`, `duration`, `codec`, `is_ready`);
- `job_event` (`event_id`, `job_id`, `event_type`, `payload`, `created_at`).

Ключевые API-контракты:

- `POST /api/admin/jobs` — создать задачу;
- `GET /api/admin/jobs/{id}` — получить статус;
- `GET /api/admin/jobs` — список задач;
- `GET /api/player/assets/{id}` — данные для playback.
- `GET /api/admin/search` — поиск релизов через `IndexerProvider`.

## Отказоустойчивость

- Слой сервисов идемпотентен для повторного `create job` через `request_id`.
- Outbox-подход для надежной публикации событий/задач в очередь.
- Таймауты, retry и circuit-breaker на внешние вызовы `Prowlarr`.
- Статус `failed` всегда сопровождается `error_code` и `error_message`.

## Наблюдаемость и безопасность

- JSON-логи с полями: `trace_id`, `job_id`, `endpoint`, `latency_ms`, `status_code`.
- Health check: `/health/live`, `/health/ready`.
- Отдельные метрики по поиску: latency/error rate для `IndexerProvider`.
- JWT auth + role-based guard для admin endpoint.
- CORS allowlist, rate limiting и ограничение body size.

## Точки расширения под сериалы

- Все API принимают/возвращают `content_type`.
- Сервисный слой использует `MediaWorkflowStrategy` по типу контента.
- Модель `media_job` допускает родительскую задачу и дочерние подзадачи (для будущей агрегации эпизодов).
