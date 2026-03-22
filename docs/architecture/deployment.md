# Развёртывание и инфраструктура

## Серверы

| Сервер | IP | Роль | SSH |
|---|---|---|---|
| **API** | `178.104.100.36` | Go API + Admin UI + PostgreSQL + Redis | `ssh -i ~/.ssh/id_rsa_personal root@178.104.100.36` |
| **Converter** | `178.104.53.215` | Go Worker + qBittorrent + Prowlarr + Flaresolverr | `ssh -i ~/.ssh/id_ed25519 root@178.104.53.215` |
| **Storage** | `45.134.174.84` | HLS origin (nginx), Player UI | `ssh -i ~/.ssh/id_rsa_personal root@45.134.174.84` |
| **Scanner** | `213.111.156.183` | Python scanner (incoming files), HTTP API :8080 | `ssh -i ~/.ssh/id_rsa_personal root@213.111.156.183` |

---

## Домены и маршрутизация

| Домен | Сервер | Назначение |
|---|---|---|
| `admin.pimor.online` | API (178.104.100.36) | Admin UI (Next.js frontend) |
| `api.pimor.online` | API (178.104.100.36) | Go API (admin + player endpoints) |
| `pimor.online` | API (178.104.100.36) | Заглушка (404) |
| `media.pimor.online` | Storage (45.134.174.84) | HLS файлы (nginx static) |
| `player.pimor.online` | Storage (45.134.174.84) | Player UI (Next.js) |

Cloudflare: **Full (strict)** SSL. Let's Encrypt сертификаты на каждом сервере.

---

## API Server (178.104.100.36)

### Стек
```
/opt/converter/
├── docker-compose.api.yml   ← активный compose файл
├── .env                     ← секреты (не в git)
├── secrets/                 ← ключи
└── media/                   ← временное хранение субтитров
```

### Сервисы
| Контейнер | Порт (host) | Назначение |
|---|---|---|
| `converter-api-1` | 8000 | Go API |
| `converter-frontend-1` | 3000 | Admin UI |
| `converter-postgres-1` | 5432 | PostgreSQL (доступен Worker-серверу) |
| `converter-redis-1` | 6379 | Redis (доступен Worker-серверу) |

Postgres и Redis открыты на `0.0.0.0`, доступ ограничен ufw только для IP Worker-сервера (178.104.53.215).

### Nginx
Конфиг: `infra/nginx/api-server.conf` → `/etc/nginx/sites-available/pimor.conf`

### Запуск / обновление
```bash
cd /opt/converter
git pull origin main
docker compose -f docker-compose.api.yml build api frontend
docker compose -f docker-compose.api.yml up -d
```

> ⚠️ **ВАЖНО: всегда указывай `-f docker-compose.api.yml`.**
> Если запустить `docker compose` без флага `-f`, Docker использует дефолтный `docker-compose.yml`,
> в котором Redis **не публикует порт 6379**. Воркер потеряет доступ к Redis и перестанет
> обрабатывать задания. Восстановление — перезапустить воркер на Converter Server.

---

## Converter Server (178.104.53.215)

### Стек
```
/opt/converter/
├── docker-compose.worker.yml  ← compose файл (worker без postgres/redis)
├── .env                       ← секреты (не в git)
└── secrets/
    ├── mediarw_rclone         ← SSH ключ для SFTP на Storage
    └── scanner_rclone         ← SSH ключ для SFTP на Scanner
```

### Сервисы
| Контейнер | Порт (host) | Назначение |
|---|---|---|
| `converter-worker-1` | 8001 | Go Worker (health) |
| `converter-qbittorrent-1` | 8080 | qBittorrent WebUI |
| `converter-prowlarr-1` | 9696 | Prowlarr |
| `converter-flaresolverr-1` | — | FlareSolverr (внутренний) |

Worker подключается к PostgreSQL и Redis на API-сервере:
```
DATABASE_URL=postgres://app:...@178.104.100.36:5432/mediadb
REDIS_URL=redis://178.104.100.36:6379
```

### Nginx
Nginx не установлен. Сервисы доступны напрямую по IP.

---

## Storage Server (45.134.174.84)

### Стек
```
/storage/             ← HLS файлы (rclone заливает сюда)
/opt/player/          ← Player UI (Next.js, порт 3100)
```

### Сервисы
| Контейнер | Порт (host) | Назначение |
|---|---|---|
| `ptrack-player` | 127.0.0.1:3100 | Player UI |

### Nginx
Конфиг: `infra/nginx/storage-server.conf` → `/etc/nginx/sites-available/pimor.conf`

---

## Scanner Server (213.111.156.183)

### Стек
```
/opt/converter/scanner/
├── docker-compose.yml
├── scanner/
└── tests/
```

### Сервисы
| Контейнер | Порт (host) | Назначение |
|---|---|---|
| `scanner-scanner-1` | 8080 | Python FastAPI (HTTP API) |
| `scanner-postgres-1` | — | PostgreSQL (внутренний) |

### Nginx
Nginx не установлен. API доступен напрямую: `http://213.111.156.183:8080`.

---

## Поток данных

```
Browser
  │
  ├── admin.pimor.online (Admin UI)
  │     └── api.pimor.online (Go API)
  │           ├── PostgreSQL (API Server)
  │           └── Redis queues (API Server)
  │                 └── Worker (Converter Server)
  │                       ├── qBittorrent / rclone HTTP download
  │                       ├── FFmpeg → HLS
  │                       └── rclone SFTP → /storage (Storage Server)
  │
  └── player.pimor.online (Player UI)
        └── api.pimor.online (player endpoints)
              └── media.pimor.online (HLS segments, nginx static)
```

---

## Конфигурация (env)

Шаблоны:
- **API Server**: `.env.api.example`
- **Worker Server**: `.env.worker.example`
- **Player**: `player/.env.example`
- **Scanner**: `scanner/.env.example`

---

## Первоначальная настройка сервера

### API Server
```bash
git clone https://github.com/ushow-dev/converter.git /opt/converter
cd /opt/converter
cp .env.api.example .env   # заполнить значения
docker compose -f docker-compose.api.yml up -d
```

### Worker Server
```bash
git clone https://github.com/ushow-dev/converter.git /opt/converter
cd /opt/converter
cp .env.worker.example .env   # заполнить значения
mkdir -p secrets
# скопировать SSH ключи в secrets/mediarw_rclone и secrets/scanner_rclone
docker compose -f docker-compose.worker.yml up -d
```

---

## Резервное копирование

| Данные | Где | Метод |
|---|---|---|
| Метаданные фильмов | API Server — Docker volume `postgres_data` | `pg_dump` |
| HLS файлы | Storage Server — `/storage` | rsync / snapshot |
| Конфигурация | `.env` файлы на каждом сервере | ручное резервирование |
