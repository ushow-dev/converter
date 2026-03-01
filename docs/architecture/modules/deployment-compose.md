# Модуль: Deployment Architecture (Docker Compose)

## Назначение

Определяет способ развертывания всей системы на single-host через `docker compose` с возможностью дальнейшего переноса на сервер без изменения архитектурных контрактов.

## Состав сервисов

- `frontend` — Next.js admin UI.
- `api` — Go Core API.
- `worker` — Go worker (download + convert).
- `postgres` — persistence.
- `redis` — queue broker.
- `prowlarr` — indexer backend для поиска.
- `qbittorrent` — torrent backend.
- `nginx` (опционально) — выдача готовых файлов/stream proxy.

## Сети

- `app_net` (bridge): единая внутренняя сеть для всех контейнеров.
- Внешняя публикация портов только у `frontend` и, при необходимости, `nginx`.
- `postgres`, `redis`, `qbittorrent`, `prowlarr` не публикуются наружу.

## Volumes

- `postgres_data` — данные PostgreSQL.
- `redis_data` — persistence Redis (если включен AOF/RDB).
- `prowlarr_data` — state/config Prowlarr.
- `torrent_data` — state qBittorrent.
- `media_storage` — общая файловая зона (`/media/downloads`, `/media/converted`, `/media/temp`).

## Health checks

Минимальный набор:

- `frontend`: HTTP check корневой страницы или health route.
- `api`: `/health/ready`.
- `worker`: внутренний health probe (доступ к Redis и media volume).
- `postgres`: `pg_isready`.
- `redis`: `redis-cli ping`.
- `prowlarr`: HTTP/API readiness check.
- `qbittorrent`: API endpoint status/auth check.

## Restart policies

- Для всех runtime-сервисов: `restart: unless-stopped`.
- Для stateful компонентов (`postgres`, `redis`) обязательно корректное завершение и проверка readiness перед стартом зависимых сервисов.

## Порядок старта и зависимостей

- `api` зависит от `postgres`, `redis` и `prowlarr` с учетом health condition.
- `worker` зависит от `api`, `redis`, `qbittorrent` и `media_storage`.
- `frontend` зависит от готовности `api`.
- `nginx` (если используется) зависит от `api` и доступа к `media_storage` (read-only).

## Рекомендации по ресурсам

- Ограничить CPU/RAM для `qbittorrent` и `ffmpeg`-нагрузки в `worker`.
- Выделить минимальный резерв диска для `media_storage`.
- Ввести ротацию логов Docker для защиты от переполнения диска.

## Пример compose-каркаса (укороченный)

```yaml
services:
  prowlarr:
    image: lscr.io/linuxserver/prowlarr:latest
    restart: unless-stopped
    networks: [app_net]

  api:
    image: app/api:latest
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      prowlarr:
        condition: service_started
    networks: [app_net]

  worker:
    image: app/worker:latest
    restart: unless-stopped
    depends_on:
      api:
        condition: service_started
      redis:
        condition: service_healthy
      qbittorrent:
        condition: service_healthy
    volumes:
      - media_storage:/media
    networks: [app_net]

networks:
  app_net:
    driver: bridge

volumes:
  postgres_data:
  redis_data:
  prowlarr_data:
  torrent_data:
  media_storage:
```

## Точки расширения под сериалы

- Compose-уровень остается неизменным при добавлении сериалов.
- Возможное будущее изменение: выделение `download-worker` и `convert-worker` в отдельные сервисы без изменения сети и volume контрактов.
