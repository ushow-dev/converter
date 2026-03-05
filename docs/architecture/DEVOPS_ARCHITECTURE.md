# DevOps архитектура

## 1) Цели и вводные

- Плеер: `Fluid Player` на `Next.js`, встраивается на внешние сайты через `iframe`.
- Backend API: `Go` (отдает метаданные и `HLS` ссылку для плеера).
- Раздача видео: `nginx` + CDN слой.
- Контент-пайплайн: админка -> downloader -> converter (`ffmpeg`) -> HLS.
- База: PostgreSQL с данными о фильмах и ссылками на поток.
- Качества HLS: `360p`, `480p`, `720p`.
- Стартовый каталог: ~`100` фильмов.
- Нагрузка на старте: ~`1 000 000` просмотров/месяц.
- Ограничение: домены с плеером могут блокироваться, нужен быстрый переезд на другой прокси/домен.

## 2) Роли компонентов

1. **Player (Next.js + Fluid Player)**
  - Отдает embed-страницу (`/embed/{imdb_id}`).
  - Запрашивает API и инициализирует HLS воспроизведение.
2. **Public API (Go)**
  - Эндпоинт для плеера: `GET /api/v1/movies/{imdb_id}`.
  - Возвращает `is_ready`, `title`, `poster`, `streams.hls`.
3. **Admin API + Admin UI**
  - Управление каталогом, задачами скачивания и конвертации.
4. **Downloader + Converter Worker**
  - Скачивает источник.
  - Транскодирует в HLS профили `360/480/720`.
  - Публикует готовый контент в объектное хранилище.
5. **Data Layer**
  - `PostgreSQL` (каталог фильмов, статус готовности, ссылки потоков).
  - `Redis` (очереди задач).
6. **Streaming Delivery**
  - Origin (`nginx`) хранит/читает HLS.
  - CDN кэширует `.m3u8` и `.ts` на edge.

## 3) Целевая схема окружения

```text
[External Sites]
   iframe src=https://player.example.com/embed/tt1234567
            |
            v
[Edge Proxy / CDN]  <--- primary domain(s), anti-blocking layer
   |            \
   |             \--> CDN cache (HLS)
   v
[App Cluster]
  - player (Next.js)
  - public api (Go)
  - admin api/ui
            |
            v
[Data Cluster]
  - PostgreSQL (primary + replica optional)
  - Redis

[Processing Cluster]
  - downloader
  - converter worker (ffmpeg)
            |
            v
[Object Storage / Media Origin]
  /movies/{imdb_id}/master.m3u8
  /movies/{imdb_id}/720/*.ts
  /movies/{imdb_id}/480/*.ts
  /movies/{imdb_id}/360/*.ts
```

## 4) Разделение по серверам 

### Сервер A: Edge / Delivery

- `nginx` reverse proxy + TLS termination.
- Подключение CDN (Cloudflare/CloudFront/Bunny/аналог).
- Отдача player-домена и проксирование API.

**Профиль:** высокая сеть, умеренный CPU.

### Сервер B: App + Data Control Plane

- `Go API` (public + admin).
- `Admin UI`, `Player Next.js` (можно как static + node runtime).
- `PostgreSQL`, `Redis` (или managed сервисы).

**Профиль:** стабильность, IOPS для БД, невысокая CPU нагрузка.

### Сервер C: Processing

- `Downloader` + `Converter Worker` + `ffmpeg`.
- Временное storage для конвертации.
- Публикация готовых HLS в object storage.

**Профиль:** CPU-heavy + disk I/O.

## 5) Требования к железу (для оценки стоимости)

### 5.1 Стартовый production (1M просмотров/мес, 100 фильмов)


| Узел              | CPU     | RAM      | Disk            | Network | Примечание                     |
| ----------------- | ------- | -------- | --------------- | ------- | ------------------------------ |
| Edge/Delivery (A) | 4 vCPU  | 8 GB     | 80-120 GB SSD   | 1 Gbps  | TLS, reverse proxy, CDN origin |
| App+Data (B)      | 8 vCPU  | 16-32 GB | 500 GB NVMe     | 1 Gbps  | API + Postgres + Redis         |
| Processing (C)    | 16 vCPU | 32 GB    | 2-4 TB NVMe/SSD | 1 Gbps  | ffmpeg, temp files, downloader |


### 5.2 Минимум для пилота (до активного роста)


| Узел                  | CPU     | RAM   | Disk       |
| --------------------- | ------- | ----- | ---------- |
| Edge+App (совмещенно) | 8 vCPU  | 16 GB | 200 GB SSD |
| Processing            | 12 vCPU | 24 GB | 2 TB SSD   |


> Для долгого буферного хранения и стабильного CDN предпочтителен object storage (S3-совместимый) вместо локального диска в origin.

## ) Сетевые и системные требования

### Порты и доступ

- В интернет: только `80/443` на Edge.
- Внутренний доступ:
  - App -> DB: `5432`
  - App/Worker -> Redis: `6379`
  - Worker -> Object Storage: `443`
- Админка и admin API ограничены по IP/VPN + MFA.

### Nginx/CDN policy

- `.ts`: `Cache-Control: public, max-age=31536000, immutable`
- `.m3u8`: `Cache-Control: public, max-age=10-30`
- CORS для плеера (`GET, HEAD, OPTIONS`).
- Range requests включены.

### SLO/наблюдаемость

- SLO playback availability: `>= 99.9%`.
- Метрики: API p95, 4xx/5xx, CDN cache hit ratio, origin egress, ffmpeg duration.
- Логи в централизованное хранилище + алерты в Telegram/Slack.

## 8) Anti-blocking стратегия (быстрый переезд на другой прокси/домен)

### Обязательные практики

1. **Domain abstraction в конфиге**
  - Player и API не хардкодят домены.
  - Все публичные URL собираются из env-переменных:
    - `PLAYER_BASE_URL`
    - `API_BASE_URL`
    - `STREAM_BASE_URL`
2. **Несколько резервных доменов заранее**
  - Подготовить пул: `player-1`, `player-2`, `stream-1`, `stream-2`.
  - Выпустить TLS сертификаты заранее.
3. **Низкий DNS TTL**
  - TTL `60-120` секунд для критичных доменов.
  - Failover через DNS/Traffic Manager.
4. **Слой прокси перед origin**
  - Быстрый перенос достигается сменой upstream на edge/proxy без пересборки приложений.
5. **Feature flag для endpoint switch**
  - Переключение stream endpoint в runtime (через env + reload, без full redeploy).

### Runbook (целевое время переключения: 5-15 минут)

1. Обнаружили блокировку (рост 4xx/timeout по региону).
2. Активировали резервный домен в DNS/CDN.
3. Переключили `STREAM_BASE_URL` и `PLAYER_BASE_URL` в конфиге.
4. Reload `nginx` и rolling restart player/api (если требуется).
5. Проверили health-checks + реальный playback test.
6. Обновили embed snippet для новых партнеров (старые iframe продолжают работать через редирект/прокси).

## 9) План внедрения для DevOps

### Этап 1 (1-2 недели): Foundation

- IaC (Terraform/Ansible) для 3 серверов.
- Docker Compose или k8s baseline.
- Edge nginx + TLS + базовый WAF/rate limit.
- Postgres backup policy (daily + PITR при возможности).

### Этап 2 (1 неделя): Media delivery

- Подключение object storage.
- Настройка CDN кэша и правил для HLS.
- Загрузка тестовых 10-20 фильмов, прогон нагрузочного playback теста.

### Этап 3 (1 неделя): Reliability

- Мониторинг/алерты/дашборды.
- Автоматизированный health-check playback.
- Документированный runbook anti-blocking + учения.

### Этап 4 (по мере роста)

- Вынос Postgres/Redis в managed.
- Горизонтальное масштабирование worker.
- Multi-region edge/proxy.

## 10) Чеклист для расчета бюджета

- Compute: 3 сервера (A/B/C) по таблице выше.
- Storage: object storage под HLS + lifecycle policy.
- CDN: egress 175-439 TB/мес (зависит от watch-time/bitrate).
- Backup: snapshot + offsite backup БД.
- Monitoring/Logs: managed stack или self-hosted.
- Резерв доменов/сертификатов для anti-blocking.

---

