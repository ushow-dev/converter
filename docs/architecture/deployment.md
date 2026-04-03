# Развёртывание

## Серверы

| Сервер | IP | Роль | SSH |
|---|---|---|---|
| **API** | `178.104.100.36` | Go API + Admin UI + PostgreSQL + Redis | `ssh -i ~/.ssh/id_rsa_personal root@178.104.100.36` |
| **Converter** | `178.104.53.215` | Go Worker + qBittorrent + Prowlarr + Flaresolverr | `ssh -i ~/.ssh/id_ed25519 root@178.104.53.215` |
| **Storage** | `45.134.174.84` | HLS origin (nginx), Player UI. Мигрируется на MinIO. | `ssh -i ~/.ssh/id_rsa_personal root@45.134.174.84` |
| **Scanner** | `213.111.156.183` | Python scanner, HTTP API :8080 | `ssh -i ~/.ssh/id_rsa_personal root@213.111.156.183` |
| **MinIO** | `178.63.205.179` | Object storage для HLS. Заменит Storage. | |
| **Edge 1 Singapore** | `67.159.52.120` | Сайт ultrashow.fun + API proxy | |
| **Edge 2 Singapore** | TBD | HLS потоки + Player UI | |

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
> Без флага `-f` Redis **не публикует порт 6379** → Worker теряет доступ к Redis.

---

## Converter Server (178.104.53.215)

### Стек
```
/opt/converter/
├── docker-compose.worker.yml
├── .env
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

---

## Storage Server (45.134.174.84)

> Мигрируется на MinIO. После миграции будет закрыт.

### Стек
```
/storage/             ← HLS файлы (rclone заливает сюда)
/opt/player/          ← Player UI (Next.js, порт 3100)
```

### Сервисы
| Контейнер | Порт (host) | Назначение |
|---|---|---|
| `ptrack-player` | 127.0.0.1:3100 | Player UI |

### Player деплой
```bash
# С локальной машины:
rsync -avz --exclude='node_modules' --exclude='.next' --exclude='.env' \
  -e "ssh -i ~/.ssh/id_rsa_personal" \
  player/ root@45.134.174.84:/opt/player/

ssh -i ~/.ssh/id_rsa_personal root@45.134.174.84 \
  'cd /opt/player && docker build -t ptrack-player . && \
   docker stop ptrack-player && docker rm ptrack-player && \
   docker run -d --name ptrack-player --restart unless-stopped \
   -p 127.0.0.1:3100:3000 --env-file /opt/player/.env ptrack-player'
```

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
| `scanner-scanner-1` | 8080 | Python FastAPI |
| `scanner-postgres-1` | — | PostgreSQL (внутренний) |

---

## Конфигурация (env)

Шаблоны:
- **API Server**: `.env.api.example`
- **Worker Server**: `.env.worker.example`
- **Player**: `player/.env.example`
- **Scanner**: `scanner/.env.example`

---

## Первоначальная настройка

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
cp .env.worker.example .env
mkdir -p secrets
# скопировать SSH ключи в secrets/mediarw_rclone и secrets/scanner_rclone
docker compose -f docker-compose.worker.yml up -d
```

---

## Резервное копирование

| Данные | Где | Метод |
|---|---|---|
| Метаданные фильмов | API Server — Docker volume `postgres_data` | `pg_dump` |
| HLS файлы | Storage / MinIO | rsync / snapshot |
| Конфигурация | `.env` файлы на каждом сервере | ручное резервирование |
