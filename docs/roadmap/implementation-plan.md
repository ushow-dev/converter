# Implementation Plan (Checklist)

Статусы:

- `[ ]` не сделано
- `[x]` сделано

## Phase 0 — Architecture Foundation

- Подготовить архитектурные документы по модулям.  
**Артефакт:** `docs/architecture/`*  
**Критерий готовности:** есть `00-system-overview.md` и отдельные модульные файлы.
- Зафиксировать API-контракты для Admin и Player.  
**Артефакт:** `docs/contracts/api-admin-player.md`.  
**Критерий готовности:** определены endpoint, схемы запросов/ответов, коды ошибок.
- Зафиксировать job payload schema (`download`/`convert`).  
**Артефакт:** `docs/contracts/job-payloads.md`.  
**Критерий готовности:** обязательные поля, статусная модель и retry policy описаны.

## Phase 1 — Compose and Infrastructure Baseline

### Task Tracker


| ID    | Статус | Задача                                                                                                                   | Зависимости  | Артефакт                                                                 | DoD                                                                              |
| ----- | ------ | ------------------------------------------------------------------------------------------------------------------------ | ------------ | ------------------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| P1-01 | [ ]    | Подготовить базовый `docker-compose.yml` для `frontend`, `api`, `worker`, `postgres`, `redis`, `qbittorrent`, `prowlarr` | —            | `docker-compose.yml`                                                     | Все сервисы стартуют одной командой `docker compose up -d`                       |
| P1-02 | [ ]    | Добавить `.env.example` с обязательными переменными и дефолтами dev                                                      | P1-01        | `.env.example`                                                           | Все переменные из compose покрыты и документированы                              |
| P1-03 | [ ]    | Настроить volumes: `postgres_data`, `redis_data`, `prowlarr_data`, `torrent_data`, `media_storage`                       | P1-01        | `docker-compose.yml`                                                     | После перезапуска контейнеров данные и state сохраняются                         |
| P1-04 | [ ]    | Настроить сеть `app_net` и service DNS-имена                                                                             | P1-01        | `docker-compose.yml`                                                     | Все внутренние обращения работают по именам сервисов                             |
| P1-05 | [ ]    | Добавить healthchecks для `postgres`, `redis`, `prowlarr`, `qbittorrent`, `api`, `worker`                                | P1-01        | `docker-compose.yml`                                                     | `docker compose ps` показывает `healthy` для обязательных сервисов               |
| P1-06 | [ ]    | Настроить `depends_on` с health conditions для корректного порядка старта                                                | P1-05        | `docker-compose.yml`                                                     | `api` и `worker` не стартуют до готовности зависимостей                          |
| P1-07 | [ ]    | Ограничить внешний доступ: публиковать только `frontend` (и опционально `nginx`)                                         | P1-04        | `docker-compose.yml` + `docs/architecture/modules/deployment-compose.md` | `postgres`, `redis`, `qbittorrent`, `prowlarr` не имеют host-портов              |
| P1-08 | [ ]    | Добавить базовые restart policies и лог-ротацию Docker                                                                   | P1-01        | `docker-compose.yml`                                                     | Для runtime-сервисов задан `restart: unless-stopped`, логи ограничены по размеру |
| P1-09 | [ ]    | Проверить smoke-сценарий инфраструктуры и зафиксировать результат                                                        | P1-01..P1-08 | `docs/roadmap/phase1-smoke-check.md`                                     | Пройден чек: старт/рестарт/health/доступность сервисов                           |


## Phase 2 — Core API MVP

- Реализовать auth и базовую роль `admin`.  
**Артефакт:** middleware/handlers auth.  
**Критерий готовности:** неавторизованный доступ к admin API запрещен.
- Реализовать интеграцию `IndexerProvider` с backend `Prowlarr` (через его API).  
**Артефакт:** provider-слой в Core API + env-конфигурация подключения.  
**Критерий готовности:** Core API получает результаты поиска из Prowlarr через единый внутренний интерфейс.
- Реализовать API поиска и сохранения результатов с нормализацией ответа Prowlarr.  
**Артефакт:** endpoint поиска + mapper в внутренний DTO + таблицы БД.  
**Критерий готовности:** результаты из Prowlarr сохраняются в БД и выдаются в UI в стабильном формате.
- Реализовать API создания/чтения media jobs.  
**Артефакт:** job endpoints + repository.  
**Критерий готовности:** можно создать задачу и получить ее актуальный статус.
- Реализовать устойчивость поиска при недоступности Prowlarr.  
**Артефакт:** timeout/retry/circuit-breaker policy и обработка ошибок уровня API.  
**Критерий готовности:** при недоступности Prowlarr API возвращает контролируемую ошибку без деградации остальных endpoint.

## Phase 3 — Worker Pipeline MVP

- Реализовать download worker с интеграцией qBittorrent.  
**Артефакт:** consumer `download_queue`.  
**Критерий готовности:** задача скачивания переходит в `completed` при успешной загрузке.
- Реализовать convert worker на ffmpeg.  
**Артефакт:** consumer `convert_queue`.  
**Критерий готовности:** после download создается convert job и формируется файл в `converted`.
- Реализовать обновление прогресса и ошибок в state model.  
**Артефакт:** статусные апдейты в БД/API.  
**Критерий готовности:** UI видит `progress_percent`, `failed` и `error_code`.

## Phase 4 — Frontend Admin MVP

- Реализовать экран поиска и список результатов.  
**Артефакт:** страницы и компоненты поиска.  
**Критерий готовности:** админ может найти контент и выбрать элемент.
- Реализовать запуск задачи загрузки из UI.  
**Артефакт:** форма запуска/кнопка enqueue.  
**Критерий готовности:** действие в UI создает job в API.
- Реализовать страницу мониторинга задач и логов.  
**Артефакт:** jobs dashboard.  
**Критерий готовности:** статусы и последние события отображаются в реальном времени/поллинге.

## Phase 5 — Ops, Security, Hardening

- Включить структурированные JSON-логи и correlation id по всему pipeline.  
**Артефакт:** лог-формат в API/worker/frontend.  
**Критерий готовности:** любой job traceable от API до worker.
- Добавить health/readiness probes и базовые метрики.  
**Артефакт:** health endpoints и metrics endpoint.  
**Критерий готовности:** состояние сервисов диагностируется без ручной проверки логов.
- Добавить ограничения ресурсов и retry/backoff policy в runtime.  
**Артефакт:** compose limits + worker config.  
**Критерий готовности:** система корректно деградирует при нагрузке и transient ошибках.

## Phase 6 — Series Readiness (Extension Points Only)

- Пронести `content_type` через API, job payload и worker strategy.  
**Артефакт:** контракты и интерфейсы с `content_type`.  
**Критерий готовности:** система принимает `movie` и архитектурно совместима с `series`.
- Добавить strategy интерфейсы для будущего планировщика сериалов.  
**Артефакт:** `MediaWorkflowStrategy` / `DownloadPlanStrategy` контракты.  
**Критерий готовности:** можно добавить сценарий `series` без ломающих изменений в базовом pipeline.
- Описать миграционный путь к сериалам без изменения compose-топологии.  
**Артефакт:** раздел в архитектурной документации.  
**Критерий готовности:** понятен поэтапный переход к сериалам с минимальными рисками.

