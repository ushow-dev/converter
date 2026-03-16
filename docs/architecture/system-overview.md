# Обзор системы

## Назначение

Самостоятельно размещаемый (self-hosted) медиа-сервер для:
1. Поиска торрентов через Prowlarr
2. Загрузки через qBittorrent или прямых HTTP-загрузок
3. Конвертации видео в HLS (мульти-разрешение) через FFmpeg
4. Каталогизации фильмов с метаданными (TMDB/IMDb)
5. Стриминга через встраиваемый HLS-плеер

## Топология сервисов

```
[Пользователь]
    │
    ├── Admin Browser ──► Frontend (Next.js :3000) ──► API (:8000)
    │
    └── Player Embed  ──────────────────────────────► API (:8000)
                                                         │
                                    ┌────────────────────┤
                                    │                    │
                               [Postgres]            [Redis]
                               [Prowlarr]          (очереди)
                                    │                    │
                                    └──────► Worker ◄────┘
                                                │
                                         [qBittorrent]
                                           [FFmpeg]
                                        [OpenSubtitles]
```

## Технологический стек

| Компонент | Технология | Версия |
|---|---|---|
| API | Go | 1.23 |
| Worker | Go | 1.23 |
| Frontend | Next.js / React | 14.2 / 18 |
| База данных | PostgreSQL | 16 |
| Очередь | Redis | 7 |
| HTTP-роутер | chi | v5 |
| ORM/драйвер | pgx | v5 |
| HLS-плеер | hls.js | 1.5.x |
| Контейнеризация | Docker Compose | v2 |

## Ключевые потоки работы

### Поток 1: Поиск и загрузка торрента
1. Пользователь вводит запрос в Admin UI
2. Frontend → API → Prowlarr (с circuit breaker)
3. Пользователь выбирает релиз → API создаёт Job → Redis `download_queue`
4. Worker загружает через qBittorrent → Redis `convert_queue`
5. Worker конвертирует FFmpeg → создаёт записи в БД → `completed`

### Поток 2: Загрузка файла
1. Пользователь загружает файл (до 50 ГБ) через форму
2. API сохраняет файл → Redis `convert_queue` (минуя загрузку)
3. Worker конвертирует → `completed`

### Поток 3: Воспроизведение
1. Плеер запрашивает `GET /api/player/movie?imdb_id=...`
2. API возвращает HLS URL (опционально подписанный)
3. hls.js получает `master.m3u8` и стримит сегменты

## Масштабируемость

- `DOWNLOAD_CONCURRENCY` — параллельные загрузки (default: 2)
- `CONVERT_CONCURRENCY` — параллельные конвертации (default: 1)
- Горизонтальное масштабирование Worker ограничено общим состоянием Redis/Postgres
- API stateless — можно запускать несколько экземпляров за балансировщиком
