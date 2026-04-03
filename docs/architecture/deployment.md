# Развёртывание и инфраструктура

## Серверы

| Сервер | IP | Роль | SSH |
|---|---|---|---|
| **API** | `178.104.100.36` | Go API + Admin UI + PostgreSQL + Redis | `ssh -i ~/.ssh/id_rsa_personal root@178.104.100.36` |
| **Converter** | `178.104.53.215` | Go Worker + qBittorrent + Prowlarr + Flaresolverr | `ssh -i ~/.ssh/id_ed25519 root@178.104.53.215` |
| **Storage** | `45.134.174.84` | HLS origin (nginx), Player UI. Мигрируется на MinIO. | `ssh -i ~/.ssh/id_rsa_personal root@45.134.174.84` |
| **Scanner** | `213.111.156.183` | Python scanner (incoming files), HTTP API :8080 | `ssh -i ~/.ssh/id_rsa_personal root@213.111.156.183` |
| **MinIO** | `178.63.205.179` | Object storage для сконвертированных фильмов/сериалов. Заменит Storage. | |
| **Edge/Proxy** | `67.159.52.120` | CDN edge proxy (Сингапур) для MinIO + сайт ultrashow.fun + админка фильмов | |

---

## Сетевая архитектура: внутренний + внешний слой

Принцип: внутренние серверы (API, Worker, Scanner, MinIO, Storage) **никогда не доступны пользователю напрямую**. Все публичные запросы идут через заменяемые Edge proxy серверы. Если Edge заблокируют — поднимается новый VPS, меняется DNS. Внутренняя инфра не трогается.

### Внутренний слой (закрыт фаерволом, доступ только от Edge proxy)

| Сервер | IP | Открытые порты | Доступ |
|---|---|---|---|
| API | 178.104.100.36 | 8000 (API), 3000 (Frontend) | Только от Edge proxy IP |
| Worker | 178.104.53.215 | — | Закрыт (подключается к API PostgreSQL/Redis) |
| Scanner | 213.111.156.183 | 8080 | Только от Worker IP |
| MinIO | 178.63.205.179 | 9000 | Только от Edge proxy IP |
| Storage (legacy) | 45.134.174.84 | 3100 (Player), 80 (nginx) | Только от Edge proxy IP. Мигрируется на MinIO. |

### Внешний слой (заменяемый, смотрит наружу)

| Edge сервер | IP | Что проксирует | Стоимость |
|---|---|---|---|
| **Edge Singapore** | 67.159.52.120 | ultrashow.fun + Media CDN для Азии | Уже есть |
| **Edge Europe** (нужен) | TBD | API proxy + Player proxy + Media CDN для Европы | ~$5/мес VPS |
| **Edge резерв** (опционально) | TBD | Резервный при блокировке основного | ~$5/мес VPS |

### Что нужно закупить

| # | Действие | Стоимость | Приоритет |
|---|---|---|---|
| 1 | VPS Europe (API + Player + Media proxy) | ~$5/мес | Высокий |
| 2 | Домен для API (напр. `newapi.xyz`) | ~$3/год | Высокий |
| 3 | Домен для Media CDN (напр. `cdn-stream.site`) | ~$3/год | Высокий |
| 4 | Закрыть фаерволом API и MinIO — только от Edge IP | Бесплатно | Высокий |
| 5 | VPS Asia (второй Media CDN edge) | ~$5/мес | Средний |
| 6 | Резервный домен | ~$3/год | Низкий |

### Схема маршрутизации (целевая)

```
ultrashow.fun (Edge Singapore 67.159.52.120)
    ├── /api/*     → Edge Europe → API (178.104.100.36:8000)
    ├── /player/*  → Edge Europe → Player UI (Storage/MinIO)
    └── /stream/*  → MinIO (178.63.205.179:9000)

{api-домен} (Edge Europe)
    └── → API (178.104.100.36:8000)

{media-домен} (Edge Europe / Edge Singapore)
    └── → MinIO (178.63.205.179:9000)

Пользователь знает: ultrashow.fun, {api-домен}, {media-домен}
Пользователь НЕ знает: 178.104.100.36, 178.63.205.179, 45.134.174.84
```

При блокировке `{api-домен}`:
1. Купить новый домен
2. Направить DNS на тот же Edge или новый VPS
3. Обновить конфиг плеера (MEDIA_BASE_URL, API_URL)
4. Внутренняя инфра без изменений

---

## Домены и маршрутизация (текущее состояние)

| Домен | Сервер | Назначение | Статус |
|---|---|---|---|
| `admin.pimor.online` | API (178.104.100.36) | Admin UI (Next.js frontend) | Напрямую — **перенести за proxy** |
| `api.pimor.online` | API (178.104.100.36) | Go API (admin + player endpoints) | Напрямую — **перенести за proxy** |
| `pimor.online` | API (178.104.100.36) | Заглушка (404) | Напрямую |
| `media.pimor.online` | Storage (45.134.174.84) | HLS файлы (nginx static) | Напрямую — **перенести за proxy** |
| `player.pimor.online` | Storage (45.134.174.84) | Player UI (Next.js) | Напрямую — **перенести за proxy** |
| `ultrashow.fun` | Edge Singapore (67.159.52.120) | Сайт + админка фильмов | Через proxy ✓ |

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
