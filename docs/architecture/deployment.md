# Развёртывание и инфраструктура

## Docker Compose Stack

Все сервисы описаны в `converter/docker-compose.yml`.

### Порядок запуска (depends_on)

```
media-init (alpine init)
    │
    ├── postgres (healthcheck: pg_isready)
    │       │
    │       └── api (ждёт postgres + redis)
    │               │
    │               └── worker (ждёт api + redis + qbittorrent)
    │
    ├── redis (healthcheck: ping)
    │
    ├── qbittorrent
    │
    └── frontend (независим, ждёт api)
```

### Exposed порты

| Сервис | Внутренний | Внешний | Назначение |
|---|---|---|---|
| api | 8000 | 8000 | Admin + Player API |
| frontend | 3000 | 3000 | Admin UI |
| qbittorrent | 8080 | 8080 | qBittorrent WebUI |
| prowlarr | 9696 | 9696 | Prowlarr (начальная настройка) |
| worker health | 8001 | — | Только внутренний |
| postgres | 5432 | — | Только внутренний |
| redis | 6379 | — | Только внутренний |
| flaresolverr | 8191 | — | Только внутренний |

### Volumes

| Volume | Тип | Назначение |
|---|---|---|
| `postgres_data` | Docker named | Данные PostgreSQL |
| `redis_data` | Docker named | Redis AOF persistence |
| `prowlarr_config` | Docker named | Prowlarr конфигурация |
| `qbittorrent_config` | Docker named | qBittorrent конфигурация |
| `${MEDIA_PATH}:/media` | Bind mount | Медиа файлы (загрузки, HLS) |

### Сеть

Все сервисы в bridge-сети `app_net`.
Внутренние DNS-имена: `postgres`, `redis`, `api`, `worker`, `qbittorrent`, `prowlarr`, `flaresolverr`.

---

## Конфигурация (.env)

Полный список переменных в `.env.example`.

### Критические переменные

```bash
# Идентификация пользователя (для qBittorrent / Prowlarr)
PUID=1000
PGID=1000

# PostgreSQL
POSTGRES_USER=admin
POSTGRES_PASSWORD=<strong_password>
POSTGRES_DB=mediadb

# Аутентификация
JWT_SECRET=<min_32_chars_random>
PLAYER_API_KEY=<random_key>
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=<bcrypt_hash_or_plaintext>

# Внешние API
TMDB_API_KEY=<tmdb_key>
OPENSUBTITLES_API_KEY=<opensubs_key>
PROWLARR_API_KEY=<prowlarr_key>

# Медиа
MEDIA_PATH=/path/to/media
MEDIA_BASE_URL=https://your-domain.com
MEDIA_SIGNING_KEY=<optional_signing_key>

# Concurrency
DOWNLOAD_CONCURRENCY=2
CONVERT_CONCURRENCY=1
```

---

## Dockerfiles

### API Dockerfile (multistage)
```
Stage 1 (builder): golang:1.23-alpine
  - go mod download
  - go build -o /api ./cmd/api

Stage 2 (runtime): alpine:3.21
  - Копирует бинарник из builder
  - EXPOSE 8000
  - ENTRYPOINT ["/api"]
```

### Worker Dockerfile (multistage)
Аналогичная структура, EXPOSE 8001 (health).

### Frontend Dockerfile (multistage)
```
Stage 1 (deps): node:20-alpine — npm ci
Stage 2 (builder): node:20-alpine — npm run build
Stage 3 (runner): node:20-alpine — standalone output
  - EXPOSE 3000
```

---

## Первоначальная настройка

### 1. Медиа-директории

`media-init` контейнер создаёт:
```bash
/media/downloads/
/media/temp/
/media/converted/
```
Устанавливает права владельца через `chown ${PUID}:${PGID}`.

### 2. Prowlarr

После запуска необходимо вручную:
1. Открыть `http://localhost:9696`
2. Добавить индексаторы
3. Скопировать API key → `.env` `PROWLARR_API_KEY`

### 3. qBittorrent

После запуска:
1. Открыть `http://localhost:8080`
2. Сменить пароль (default: admin/adminadmin)
3. Обновить `.env` `QBITTORRENT_USER` / `QBITTORRENT_PASSWORD`

---

## Продакшн-рекомендации

### nginx для медиа-стриминга

В репозитории есть `ptrack.ink.conf` (nginx config, закомментирован в docker-compose).
Рекомендуется раскомментировать nginx-сервис для:
- Отдачи HLS сегментов напрямую (без Go API)
- Поддержки `secure_link` подписывания URL
- SSL termination

### Безопасность

- Не открывать порты PostgreSQL и Redis наружу
- Установить `MEDIA_SIGNING_KEY` для подписывания медиа-URL
- Использовать bcrypt-хэш для `ADMIN_PASSWORD`
- Настроить reverse proxy (nginx/Caddy) перед API и Frontend
- Рассмотреть firewall rules для ограничения доступа к :8080 (qBittorrent)

### Backup

Критические данные для резервного копирования:
1. Docker volume `postgres_data` (все метаданные)
2. `${MEDIA_PATH}/converted/` (конвертированные медиафайлы)
3. `.env` файл (конфигурация)
