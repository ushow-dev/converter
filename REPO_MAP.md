# REPO_MAP.md — Карта репозитория

> Документ для ориентации в структуре проекта. Обновляйте при добавлении или удалении директорий.

---

## Корневая структура репозитория

```
converter/                  ← корень проекта
├── api/                    # Go API-сервис (HTTP, порт 8000)
├── worker/                 # Go воркер (фоновая обработка)
├── frontend/               # Next.js Admin UI (порт 3000)
├── docs/                   # Вся документация
├── media/                  # Медиа-хранилище (bind mount)
├── docker-compose.yml      # Оркестрация сервисов
├── .env                    # Конфигурация (НЕ коммитить с реальными данными)
├── .env.example            # Шаблон конфигурации
├── ptrack.ink.conf         # Конфиг nginx (для продакшна)
├── CLAUDE.md               # Инструкции для AI-ассистентов (с протоколом изменений)
├── REPO_MAP.md             # Этот файл
├── ARCHITECTURE.md         # Краткий обзор системной архитектуры
├── CHANGELOG.md            # История изменений (обновляется при каждом изменении)
├── Makefile                # Команды для разработки (make help)
├── scripts/
│   └── new-adr.sh          # Скрипт создания нового ADR
└── .githooks/              # Git хуки (активируются через make setup)
    ├── commit-msg          # Валидация формата коммита
    └── pre-commit          # go vet + проверка на секреты
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
    │   ├── player.go               # GET /api/player/movie, /api/player/assets
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
    │   └── search.go               # search_results кэш
    ├── auth/                       # JWT-токены и middleware авторизации
    ├── model/                      # Доменные модели и payload-структуры очереди
    ├── queue/                      # Redis клиент (RPUSH/BLPOP)
    ├── indexer/                    # Клиент Prowlarr + circuit breaker
    ├── subtitles/                  # Клиент OpenSubtitles + конвертация SRT→VTT
    └── db/
        ├── postgres.go             # Пул соединений pgxpool + авто-миграции
        └── migrations/             # SQL миграции (001–008)
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
    ├── queue/                      # Redis консьюмер (BLPOP)
    ├── repository/                 # Обновление статусов заданий в БД
    ├── downloader/                 # Потребитель download_queue (торренты)
    ├── httpdownloader/             # Потребитель remote_download_queue (HTTP)
    ├── converter/                  # Потребитель convert_queue (FFmpeg HLS)
    ├── ffmpeg/                     # Обёртка FFmpeg (профили, thumbnail)
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
    │   └── jobs/[jobId]/page.tsx   # Детали задания
    └── components/
        ├── Nav.tsx                 # Навигационная панель
        ├── VideoPlayer.tsx         # HLS-плеер (hls.js)
        └── SubtitleSection.tsx     # Управление субтитрами
```

---

## `docs/` — Документация

```
docs/
├── architecture/
│   ├── system-overview.md          # Общий обзор системы
│   ├── services.md                 # Описание каждого сервиса
│   ├── data-flow.md                # Потоки данных между сервисами
│   ├── deployment.md               # Развёртывание и инфраструктура
│   └── modules/                    # Детальные модули архитектуры
├── contracts/
│   ├── api.md                      # HTTP API контракты
│   └── worker.md                   # Контракты очереди воркера
├── converter/
│   ├── pipeline.md                 # FFmpeg HLS pipeline
│   └── ffmpeg.md                   # FFmpeg конфигурация и профили
├── player/
│   └── player-architecture.md      # Архитектура плеера
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
│   └── ADR-006-dual-auth-schemes.md
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
├── downloads/{jobID}/              # Сырые файлы (торрент / HTTP)
├── temp/{jobID}/                   # Временное рабочее пространство FFmpeg
└── converted/{movieStorageKey}/    # Готовый HLS-контент
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
