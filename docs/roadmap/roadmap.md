# Дорожная карта

> Планируемые улучшения, упорядоченные по приоритету.
> При завершении задачи — переносите в `CHANGELOG.md` и удаляйте отсюда.
> AI-ассистенты обязаны обновлять этот файл при реализации или планировании новых фич.

---

## Ближайшие задачи (следующий спринт)

### SECURITY-001: Ротация секретов
**Приоритет:** критический
**Что:** заменить все placeholder-значения в `.env` реальными секретами
**Почему:** текущие значения утекли при аудите
**Как:** `make gen-secrets`, затем обновить `.env`
**Связано:** ADR-006, technical-debt.md #1

### SECURITY-002: JWT в httpOnly cookie
**Приоритет:** высокий
**Что:** перенести JWT из `localStorage` в httpOnly cookie
**Почему:** защита от XSS атак
**Затрагивает:** `frontend/src/lib/api.ts`, `api/internal/auth/`, `api/internal/handler/auth.go`
**Связано:** technical-debt.md #2

### RELIABILITY-001: Retry для convert_queue
**Приоритет:** высокий
**Что:** добавить поле `attempt` в `ConvertPayload`, retry до 3 раз при ошибке FFmpeg
**Почему:** сейчас любой сбой FFmpeg — постоянный `failed` без повтора
**Затрагивает:** `worker/internal/converter/`, `api/internal/model/`, `worker/internal/model/`
**Связано:** ADR-001, technical-debt.md #5
**Требует ADR:** нет (расширение существующего решения)

### DEV-001: Первые unit тесты
**Приоритет:** высокий
**Что:** тесты для `api/internal/auth/jwt.go` и `api/internal/service/job.go`
**Почему:** нет ни одного теста, высокий риск регрессий
**Затрагивает:** `api/internal/auth/jwt_test.go`, `api/internal/service/job_test.go`

---

## Среднесрочные задачи (1-2 месяца)

### DEV-002: GitHub Actions CI
**Что:** пайплайн с `go build`, `go vet`, `npm run build` при каждом PR
**Затрагивает:** `.github/workflows/ci.yml`
**Требует ADR:** нет

### RELIABILITY-002: Cleanup воркер при старте
**Что:** при старте worker удалять осиротевшие `/media/temp/{jobID}/` директории
**Почему:** при крэше воркера временные файлы остаются навсегда
**Затрагивает:** `worker/cmd/worker/main.go`

### OBS-001: Структурированное логирование
**Что:** заменить текущие логи на `slog` (JSON в prod, текст в dev)
**Затрагивает:** оба Go сервиса
**Требует ADR:** да (если меняется формат, влияет на log aggregation)

### API-001: Версионирование эндпоинтов `/api/v1/`
**Что:** добавить `/v1/` в пути всех эндпоинтов
**Почему:** возможность ввести `/v2/` без ломки клиентов
**Затрагивает:** `api/internal/server/server.go`, `frontend/src/lib/api.ts`
**Требует ADR:** да — breaking change для всех клиентов

### RELIABILITY-003: Dead Letter Queue
**Что:** задания, провалившие все retry, попадают в отдельную таблицу или DLQ
**Затрагивает:** worker, новая DB миграция
**Требует ADR:** да

---

## Долгосрочные задачи (3+ месяца)

### OBS-002: Prometheus метрики
**Что:** `/metrics` эндпоинт, счётчики заданий, histogram конвертации, размер очередей
**Затрагивает:** оба сервиса, docker-compose (prometheus + grafana)
**Требует ADR:** да

### MEDIA-001: Поддержка 1080p
**Что:** добавить 1080p профиль в FFmpeg (условно: если источник >= 1080p)
**Затрагивает:** `worker/internal/ffmpeg/`, `docs/converter/pipeline.md`

### ARCH-001: Go workspace для shared типов
**Что:** вынести `DownloadPayload`, `ConvertPayload`, `RemoteDownloadPayload` в `shared/model/`
**Почему:** устранить дублирование моделей между api и worker
**Затрагивает:** оба Go модуля
**Требует ADR:** да — изменение структуры модулей (см. ADR-002)

### SEC-003: Rate limiting на login
**Что:** middleware с ограничением попыток входа (например, 10/мин с одного IP)
**Затрагивает:** `api/internal/server/server.go`

---

## Идеи для обсуждения

- Поддержка нескольких языков субтитров в одном запросе
- Webhook уведомления при завершении конвертации
- Поддержка аппаратного кодирования (NVENC, VAAPI)
- Превью (sprite sheet) для перемотки в плеере
- Автоматический бэкап PostgreSQL в S3
