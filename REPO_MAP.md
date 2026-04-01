# REPO_MAP.md — Карта репозитория

> Документ для ориентации в структуре проекта. Обновляйте при добавлении или удалении директорий.

---

## Корневая структура репозитория

```
converter/                      ← корень проекта
├── api/                        # Go API-сервис (HTTP, порт 8000)
├── worker/                     # Go воркер (фоновая обработка)
├── scanner/                    # Python scanner-сервис (порт 8080)
├── frontend/                   # Next.js Admin UI (порт 3000)
├── player/                     # Next.js Player UI (порт 3100)
├── docs/                       # Вся документация
├── infra/
│   ├── nginx/
│   │   ├── api-server.conf     # nginx для API-сервера (178.104.100.36)
│   │   └── storage-server.conf # nginx для Storage-сервера (45.134.174.84)
│   ├── wt-tracker/
│   │   └── config.json         # WebTorrent tracker для P2P HLS
│   ├── prometheus/
│   │   └── prometheus.yml      # Конфигурация Prometheus (scrape jobs)
│   └── grafana/
│       └── provisioning/       # Datasources и dashboards (P2P overview)
├── docker-compose.api.yml      # Compose для API-сервера
├── docker-compose.worker.yml   # Compose для Worker-сервера
├── docker-compose.yml          # Старый all-in-one (устарел)
├── .env.api.example            # Шаблон конфигурации API-сервера
├── .env.worker.example         # Шаблон конфигурации Worker-сервера
├── CLAUDE.md                   # Инструкции для AI-ассистентов
├── REPO_MAP.md                 # Этот файл
├── ARCHITECTURE.md             # Краткий обзор системной архитектуры
├── CHANGELOG.md                # История изменений
├── Makefile                    # Команды для разработки (make help)
├── scripts/
│   └── new-adr.sh              # Скрипт создания нового ADR
└── .githooks/                  # Git хуки (активируются через make setup)
    ├── commit-msg              # Валидация формата коммита
    └── pre-commit              # go vet + проверка на секреты
```

---

## `converter/api/` — API-сервис (Go)

```
api/
├── cmd/api/main.go                 # Точка входа
├── go.mod / go.sum                 # Go зависимости
├── Dockerfile                      # Multi-stage build (alpine)
└── internal/
    ├── config/                     # Загрузка env-переменных
    ├── server/                     # HTTP роутер (chi v5) и middleware
    ├── handler/                    # HTTP-обработчики
    │   ├── auth.go                 # POST /api/admin/auth/login
    │   ├── health.go               # GET /health/live, /health/ready
    │   ├── search.go               # GET /api/admin/search (Prowlarr)
    │   ├── jobs.go                 # CRUD /api/admin/jobs
    │   ├── movies.go               # CRUD /api/admin/movies
    │   ├── series.go               # CRUD /api/admin/series + GET /api/player/series + /api/player/episode
    │   ├── player.go               # GET /api/player/movie, /api/player/assets, POST /api/player/p2p-metrics
    │   ├── metrics.go              # GET /metrics (Prometheus P2P counters)
    │   ├── subtitles.go            # Управление субтитрами
    │   ├── browse.go               # Браузер удалённых файлов (заглушка)
    │   └── respond.go              # Утилиты JSON-ответов
    ├── service/                    # Бизнес-логика
    │   ├── job.go                  # Создание/управление заданиями
    │   └── search.go               # Поиск через Prowlarr
    ├── repository/                 # Доступ к БД
    │   ├── job.go                  # media_jobs таблица
    │   ├── asset.go                # media_assets таблица
    │   ├── movie.go                # movies таблица
    │   ├── subtitle.go             # movie_subtitles таблица
    │   ├── search.go               # search_results кэш
    │   ├── series.go               # series, seasons, episodes таблицы
    │   ├── audio_track.go          # media_audio_tracks таблица
    │   └── episode_subtitle.go     # episode_subtitles таблица
    ├── auth/                       # JWT-токены и middleware авторизации
    ├── model/                      # Доменные модели и payload-структуры очереди
    │   └── series.go               # Series, Season, Episode, AudioTrack структуры
    ├── queue/                      # Redis клиент (RPUSH/BLPOP)
    ├── indexer/                    # Клиент Prowlarr + circuit breaker
    ├── subtitles/                  # Клиент OpenSubtitles + конвертация SRT→VTT
    └── db/
        ├── postgres.go             # Пул соединений pgxpool + авто-миграции
        └── migrations/             # SQL миграции (001–014)
```

---

## `worker/` — Воркер (Go)

```
worker/
├── cmd/worker/main.go              # Точка входа (multi-goroutine оркестрация)
├── go.mod / go.sum
├── Dockerfile
└── internal/
    ├── config/                     # Конфигурация (concurrency, FFmpeg threads)
    ├── model/                      # Модели сообщений очереди
    │   └── series.go               # Series, Season, Episode, AudioTrack структуры (mirror API)
    ├── queue/                      # Redis консьюмер (BLPOP)
    ├── repository/                 # Обновление статусов заданий в БД
    │   ├── series.go               # Запись series/seasons/episodes после конвертации
    │   └── audio_track.go          # Запись audio_tracks после зондирования
    ├── downloader/                 # Потребитель download_queue (торренты)
    ├── httpdownloader/             # Потребитель remote_download_queue (HTTP)
    ├── converter/                  # Потребитель convert_queue (FFmpeg HLS)
    ├── transfer/                   # Потребитель transfer_queue (rclone move на remote)
    ├── ingest/                     # IngestWorker: claim → rclone copy → convert
    │   ├── worker.go               # Poll loop + processItem
    │   ├── client.go               # HTTP клиент к scanner API
    │   └── puller.go               # rclone copy с storage-сервера
    ├── ffmpeg/                     # Обёртка FFmpeg (профили, thumbnail)
    │   └── probe.go                # ffprobe audio track detection
    ├── qbittorrent/                # API-клиент qBittorrent
    ├── subtitles/                  # Авто-получение субтитров
    ├── health/                     # HTTP health server (порт 8001)
    └── db/                         # Подключение к PostgreSQL
```

---

## `frontend/` — Admin UI (Next.js 14)

```
frontend/
├── package.json                    # Node.js зависимости
├── tsconfig.json
├── next.config.mjs
├── tailwind.config.ts
├── Dockerfile
└── src/
    ├── types/index.ts              # TypeScript типы (Job, Movie, Subtitle...)
    ├── lib/api.ts                  # API-клиент (fetcher, auth utils)
    ├── app/                        # Next.js App Router страницы
    │   ├── layout.tsx              # Root layout
    │   ├── page.tsx                # Главная (auth guard → /movies)
    │   ├── login/page.tsx          # Страница логина
    │   ├── search/page.tsx         # Поиск торрентов (Prowlarr)
    │   ├── upload/page.tsx         # Загрузка файлов + HTTP-загрузка
    │   ├── queue/page.tsx          # Очередь заданий
    │   ├── movies/page.tsx         # Каталог фильмов
    │   ├── series/page.tsx         # Каталог сериалов
    │   ├── series/[id]/page.tsx    # Детали сериала (сезоны и эпизоды)
    │   └── jobs/[jobId]/page.tsx   # Детали задания
    └── components/
        ├── Nav.tsx                 # Навигационная панель
        ├── VideoPlayer.tsx         # HLS-плеер (hls.js)
        └── SubtitleSection.tsx     # Управление субтитрами
```

---

## `player/` — Player UI (Next.js)

```
player/
├── package.json
├── next.config.mjs
├── Dockerfile
└── src/
    └── app/
        ├── layout.tsx              # Root layout с HLS + P2P инициализацией
        ├── page.tsx                # Movie player (tmdb_id query param)
        └── SeriesPlayer.tsx        # Series player (сезоны, эпизоды, навигация)
```

---

## `scanner/` — Scanner Service (Python)

```
scanner/
├── docker-compose.yml              # postgres:16-alpine + scanner (python:3.12-slim + ffmpeg)
├── .env.example
├── Dockerfile
├── pyproject.toml                  # fastapi, uvicorn, psycopg2-binary, guessit, requests
└── scanner/
    ├── main.py                     # Точка входа, 3 daemon-потока
    ├── config.py                   # frozen dataclass, env vars
    ├── db.py                       # ThreadedConnectionPool, авто-миграции
    ├── migrations/                 # SQL миграции scanner DB (001–005)
    ├── loops/
    │   ├── scan_loop.py            # Сканирует incoming/ каждые SCAN_INTERVAL_SEC
    │   └── move_worker.py          # os.rename → library/movies/, upsert library
    ├── services/
    │   ├── stability.py            # Проверка стабильности файла
    │   ├── metadata.py             # GuessIt + TMDB lookup + normalized_name (movie + TV)
    │   ├── quality.py              # ffprobe → quality_score (0..100)
    │   ├── duplicates.py           # register / review_duplicate / review_unknown_quality
    │   └── series_detect.py        # Определение TV-сериалов в папках (guessit)
    └── api/
        └── server.py               # FastAPI HTTP API (claim/progress/complete/fail)
```

Документация: `docs/scanner/`

---

## `docs/` — Документация

```
docs/
├── architecture/
│   ├── 00-system-overview.md       # Общий обзор системы
│   ├── services.md                 # Описание каждого сервиса и горутин
│   ├── data-flow.md                # Потоки данных между сервисами
│   ├── deployment.md               # Развёртывание и инфраструктура
│   ├── database-schema.md          # Схема PostgreSQL (converter DB)
│   └── modules/                    # Детальные модули архитектуры
│       ├── core-api.md
│       ├── worker.md
│       └── scanner.md
├── contracts/
│   ├── api.md                      # HTTP API контракты (converter API)
│   └── worker.md                   # Контракты очередей воркера + ingest
├── scanner/
│   ├── README.md                   # Обзор scanner: архитектура, lifecycle, конфигурация
│   ├── api.md                      # Scanner HTTP API контракты
│   └── database.md                 # Scanner DB схема (scanner_incoming_items, scanner_library_movies)
├── converter/
│   ├── pipeline.md                 # FFmpeg HLS pipeline
│   └── ffmpeg.md                   # FFmpeg конфигурация и профили
├── player/
│   ├── player-architecture.md      # Архитектура плеера
│   └── p2p-streaming.md            # P2P HLS: работа, метрики, конфигурация
├── admin/
│   └── admin-overview.md           # Admin UI обзор
├── decisions/
│   ├── README.md                   # Индекс ADR (следующий номер, список)
│   ├── ADR-000-template.md         # Шаблон для новых ADR
│   ├── ADR-001-redis-blpop-queues.md
│   ├── ADR-002-two-go-modules.md
│   ├── ADR-003-cursor-pagination.md
│   ├── ADR-004-md5-url-signing.md
│   ├── ADR-005-hls-multiresolution.md
│   ├── ADR-006-dual-auth-schemes.md
│   ├── ADR-007-remote-storage-rclone.md
│   ├── ADR-008-incoming-scanner-api-driven-ingest-split.md
│   └── ADR-009-scanner-as-ingest-api-server.md
├── roadmap/
│   ├── roadmap.md                  # Дорожная карта (планируемые задачи)
│   └── technical-debt.md           # Технический долг и приоритеты
└── contributing/
    └── conventional-commits.md     # Стандарт оформления коммитов (с примерами)
```

---

## `media/` — Медиа-хранилище

```
media/                              # Bind mount с хоста (MEDIA_PATH в .env)
├── downloads/{jobID}/              # Сырые файлы (торрент / HTTP / ingest)
├── temp/{jobID}/                   # Временное рабочее пространство FFmpeg
└── converted/movies/{storageKey}/  # Готовый HLS-контент (storageKey = "Title (Year)")
    ├── master.m3u8                 # Мастер-плейлист
    ├── 360/                        # HLS сегменты 360p
    ├── 480/                        # HLS сегменты 480p
    ├── 720/                        # HLS сегменты 720p
    ├── thumbnail.jpg               # Превью
    └── subtitles/                  # VTT субтитры ({lang}.vtt)
```

---

## Правило обновления

**Каждый раз при добавлении или удалении директорий** обновляйте этот файл.
AI-ассистенты используют этот файл для первичной навигации по проекту.
