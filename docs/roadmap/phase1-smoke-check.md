# Phase 1 Smoke Check — Compose and Infrastructure Baseline

**Дата:** 2026-03-01
**Scope:** инфраструктурные сервисы Phase 1 (postgres, redis, prowlarr, qbittorrent).
**Важно:** образы приложений (`api`, `worker`, `frontend`) в Phase 1 не реализованы — их проверка выполняется в рамках Phase 2+.

---

## Предварительные требования

```bash
# 1. Убедиться, что Docker Engine >= 24 и Docker Compose >= 2.20 установлены
docker --version
docker compose version

# 2. Скопировать .env
cp .env.example .env
# Заполнить обязательные секреты: POSTGRES_PASSWORD, JWT_SECRET, PLAYER_API_KEY
# Prowlarr API key будет получен после первого запуска (см. шаг SC-04)

# 3. Убедиться, что порт 3000 (или FRONTEND_PORT) свободен
lsof -i :3000
```

---

## Чек-листы

### SC-01 — Старт инфраструктурных сервисов

```bash
# Запустить только инфраструктурные сервисы (без app-образов)
docker compose up -d postgres redis prowlarr qbittorrent

# Ожидаемый результат: все 4 контейнера Started
docker compose ps
```

**DoD:** все 4 сервиса в статусе `running` или `starting`.

---

### SC-02 — Healthchecks инфраструктуры

```bash
# Подождать ~60 с для инициализации prowlarr/qbittorrent, затем:
docker compose ps

# Ожидаемый результат:
# NAME              STATUS          PORTS
# converter-postgres-1      Up (healthy)
# converter-redis-1         Up (healthy)
# converter-prowlarr-1      Up (healthy)
# converter-qbittorrent-1   Up (healthy)
```

Проверить каждый healthcheck вручную:

```bash
# PostgreSQL
docker compose exec postgres pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}
# Ожидается: /var/run/postgresql:5432 - accepting connections

# Redis
docker compose exec redis redis-cli ping
# Ожидается: PONG

# Prowlarr
docker compose exec prowlarr curl -sf http://localhost:9696/ping
# Ожидается: {"status":"OK"} или HTTP 200

# qBittorrent
docker compose exec qbittorrent curl -sf http://localhost:8080/api/v2/app/version
# Ожидается: JSON с версией qBittorrent
```

**DoD:** `docker compose ps` показывает `(healthy)` для всех 4 сервисов.

---

### SC-03 — Сетевая связность (app_net / DNS)

```bash
# Проверить, что сервисы резолвят друг друга по DNS-именам внутри app_net
docker compose exec postgres ping -c 2 redis
docker compose exec redis ping -c 2 postgres
docker compose exec redis ping -c 2 prowlarr
docker compose exec redis ping -c 2 qbittorrent

# Если ping недоступен — проверить через curl
docker compose exec prowlarr curl -sf http://redis:6379 || true
# Ожидается: ответ (или TCP error) — главное, что DNS-резолюция работает
```

**DoD:** Docker DNS резолвит имена сервисов внутри `app_net`.

---

### SC-04 — Получение Prowlarr API Key

```bash
# Открыть UI Prowlarr недоступен снаружи, поэтому используем port-forward:
docker compose exec prowlarr cat /config/config.xml | grep ApiKey
# Или временно проверить через curl изнутри контейнера:
docker compose exec prowlarr curl -sf http://localhost:9696/api/v1/system/status \
  -H "X-Api-Key: $(docker compose exec prowlarr cat /config/config.xml | grep -oP '(?<=<ApiKey>)[^<]+')"
# Ожидается: JSON с полем "version"
```

Скопировать ключ в `.env` → `PROWLARR_API_KEY=...`

**DoD:** API Key получен, вписан в `.env`.

---

### SC-05 — Персистентность данных (volumes)

```bash
# Записать тестовую запись в PostgreSQL
docker compose exec postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} \
  -c "CREATE TABLE IF NOT EXISTS _smoke (id serial PRIMARY KEY, val text); INSERT INTO _smoke(val) VALUES ('phase1-ok');"

# Записать тестовую запись в Redis
docker compose exec redis redis-cli set smoke-key "phase1-ok"

# Перезапустить контейнеры
docker compose restart postgres redis

# Убедиться, что данные сохранились
docker compose exec postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} \
  -c "SELECT val FROM _smoke;"
# Ожидается: phase1-ok

docker compose exec redis redis-cli get smoke-key
# Ожидается: phase1-ok

# Очистить тестовую таблицу
docker compose exec postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} \
  -c "DROP TABLE _smoke;"
```

**DoD:** данные сохранились после рестарта контейнеров.

---

### SC-06 — Ограничение внешнего доступа (P1-07)

```bash
# Убедиться, что у postgres/redis/prowlarr/qbittorrent нет host-портов
docker compose ps --format json | python3 -c "
import sys, json
for line in sys.stdin:
    s = json.loads(line)
    print(s['Name'], '→ ports:', s.get('Publishers', []))
"
# postgres, redis, prowlarr, qbittorrent должны иметь пустой Publishers список

# Попытаться подключиться снаружи — должно быть отказано:
curl -v http://localhost:5432 2>&1 | grep -E 'refused|Failed'
curl -v http://localhost:6379 2>&1 | grep -E 'refused|Failed'
curl -v http://localhost:9696 2>&1 | grep -E 'refused|Failed'
curl -v http://localhost:8080 2>&1 | grep -E 'refused|Failed'
```

**DoD:** ни один из внутренних сервисов не слушает на host-интерфейсе.

---

### SC-07 — Лог-ротация

```bash
# Проверить конфигурацию лог-драйвера для каждого сервиса
docker inspect converter-postgres-1 \
  --format '{{.HostConfig.LogConfig.Type}} {{.HostConfig.LogConfig.Config}}'
# Ожидается: json-file map[max-file:3 max-size:10m]

docker inspect converter-redis-1 \
  --format '{{.HostConfig.LogConfig.Type}} {{.HostConfig.LogConfig.Config}}'
# Ожидается аналогично
```

**DoD:** у всех сервисов `json-file` с `max-size` и `max-file`.

---

### SC-08 — Restart policy

```bash
# Проверить restart policy
docker inspect converter-postgres-1 --format '{{.HostConfig.RestartPolicy}}'
# Ожидается: {unless-stopped 0}

docker inspect converter-redis-1 --format '{{.HostConfig.RestartPolicy}}'
# Ожидается: {unless-stopped 0}
```

**DoD:** `RestartPolicy.Name == unless-stopped` у всех сервисов.

---

### SC-09 — Полный запуск stack (с placeholder app-образами)

> **Примечание:** этот шаг выполнять только если собраны placeholder/dev образы для `api`, `worker`, `frontend`.
> В Phase 1 без образов приложений следует пропустить и зафиксировать как "pending Phase 2".

```bash
docker compose up -d
docker compose ps
# Ожидается: все сервисы running, инфраструктурные — healthy
```

**DoD:** `docker compose up -d` завершается без ошибок, `docker compose ps` показывает все сервисы.

---

## Итог Phase 1 Smoke Check

| Пункт | Статус     | Примечание |
|-------|-----------|------------|
| SC-01 | pending   | |
| SC-02 | pending   | |
| SC-03 | pending   | |
| SC-04 | pending   | Заполнить PROWLARR_API_KEY после получения |
| SC-05 | pending   | |
| SC-06 | pending   | |
| SC-07 | pending   | |
| SC-08 | pending   | |
| SC-09 | pending Phase 2 | Требует сборки образов приложений |

**Заполнить после прохождения каждого шага: Done / Failed / Skipped.**

---

## Известные ограничения и риски Phase 1

| # | Риск / Допущение | Влияние | Митигация |
|---|-----------------|---------|-----------|
| R1 | Образы `app/api`, `app/worker`, `app/frontend` не существуют — `docker compose up -d` упадёт для этих сервисов | Блокирует полный старт | Phase 2 реализует и собирает образы |
| R2 | qBittorrent не слушает BitTorrent-порты на хосте (P1-07) — входящие peer-соединения невозможны | Скорость скачивания ниже | При необходимости добавить `ports: ["6881:6881/tcp", "6881:6881/udp"]` в compose и зафиксировать как controlled exception |
| R3 | Prowlarr API Key получается только после первого запуска UI | Требует ручного шага | Задокументировано в SC-04; автоматизация — Phase 5 |
| R4 | Worker healthcheck (`/health`) не работает без реализованного Go-образа | `docker compose ps` покажет unhealthy | Устраняется в Phase 2 при сборке worker |
| R5 | `NEXT_PUBLIC_API_URL` — браузерная переменная; в production должна указывать на публичный URL API, а не `localhost` | Фронтенд не сможет обращаться к API из браузера на сервере | Задокументировано в `.env.example`; задача конфигурации при деплое |
