# DevOps архитектура

## 1) Цели и вводные

- Текущая стадия продукта: `MVP`.
- Текущая реализация инфраструктуры: все сервисные пайплайны развернуты в `docker compose`.
- Плеер: `Fluid Player` на `Next.js`, встраивается на внешние сайты через `iframe`.
- Портал с фильмами: `WordPress` (публичный каталог и SEO-страницы фильмов).
- Backend API: `Go` (отдает метаданные и `HLS` ссылку для плеера).
- Раздача видео: `nginx` + CDN слой.
- Контент-пайплайн: админка -> downloader -> converter (`ffmpeg`) -> HLS.
- База: PostgreSQL с данными о фильмах и ссылками на поток.
- Качества HLS: `360p`, `480p`, `720p`.
- Стартовый каталог: ~`100` фильмов.
- Нагрузка на старте: ~`1 000 000` просмотров/месяц.
- Ограничение: могут блокироваться домен `WordPress` и домен с плеером, нужны механизмы быстрого переключения на резервные домены.

## 2) Роли компонентов

1. **Player (Next.js + Fluid Player)**
  - Отдает embed-страницу (`/embed/{imdb_id}`).
  - Запрашивает API и инициализирует HLS воспроизведение.
2. **Movie Portal (WordPress)**
  - Публичный веб-портал с карточками фильмов и страницами каталога.
  - Интеграция с API/виджетом плеера по `imdb_id`.
3. **Public API (Go)**
  - Эндпоинт для плеера: `GET /api/v1/movies/{imdb_id}`.
  - Возвращает `is_ready`, `title`, `poster`, `streams.hls`.
4. **Admin API + Admin UI**
  - Управление каталогом, задачами скачивания и конвертации.
5. **Downloader + Converter Worker**
  - Скачивает источник.
  - Транскодирует в HLS профили `360/480/720`.
  - Публикует готовый контент в объектное хранилище.
6. **Data Layer**
  - `PostgreSQL` (каталог фильмов, статус готовности, ссылки потоков).
  - `Redis` (очереди задач).
7. **Streaming Delivery**
  - Origin (`nginx`) хранит/читает HLS.
  - CDN кэширует `.m3u8` и `.ts` на edge.

## 3) Целевая схема окружения

```text
[External Sites]
   iframe src=https://player.example.com/embed/tt1234567
            |
            v
[Public Users]
   |
   +--> https://portal.example.com (WordPress)
   |
   +--> iframe/partner traffic to player domains
             |
             v
[Edge Proxy / CDN]  <--- primary + reserve domain(s), anti-blocking layer
   |            \
   |             \--> CDN cache (HLS)
   v
[App Cluster]
  - player (Next.js)
  - movie portal (WordPress)
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
- `WordPress` портал (frontend каталог фильмов).
- `PostgreSQL`, `Redis` (или managed сервисы).

**Профиль:** стабильность, IOPS для БД, невысокая CPU нагрузка.

### Сервер C: Processing

- `Downloader` + `Converter Worker` + `ffmpeg`.
- Временное storage для конвертации.
- Публикация готовых HLS в object storage.

**Профиль:** CPU-heavy + disk I/O.

## 5) Требования к железу (для оценки стоимости)

### 5.1 Стартовый production (1M просмотров/мес, 100 фильмов)


| Узел                 | CPU     | RAM      | Disk            | Network | Примечание                     |
| -------------------- | ------- | -------- | --------------- | ------- | ------------------------------ |
| Edge/Delivery (A)    | 4 vCPU  | 8 GB     | 80-120 GB SSD   | 1 Gbps  | TLS, reverse proxy, CDN origin |
| App+Data (B)         | 8 vCPU  | 16-32 GB | 500 GB NVMe     | 1 Gbps  | API + Postgres + Redis         |
| WordPress Portal (D) | 4 vCPU  | 8 GB     | 120-200 GB SSD  | 1 Gbps  | WordPress + cache + media refs |
| Processing (C)       | 16 vCPU | 32 GB    | 2-4 TB NVMe/SSD | 1 Gbps  | ffmpeg, temp files, downloader |

### 5.1.1 Прокси и домены (основные + резервные)

| Контур                 | Основной домен (пример)     | Резервный домен (пример)   | Прокси/CDN endpoint                | Назначение                          |
| ---------------------- | --------------------------- | -------------------------- | ---------------------------------- | ----------------------------------- |
| Portal (`WordPress`)   | `portal-1.example.com`      | `portal-2.example.com`     | `edge-portal.example.net`          | Публичный каталог и страницы фильма |
| Player (`Next.js`)     | `player-1.example.com`      | `player-2.example.com`     | `edge-player.example.net`          | `iframe` и embed-страницы           |
| Stream (HLS delivery)  | `stream-1.example.com`      | `stream-2.example.com`     | `edge-stream.example.net`          | Выдача `.m3u8` и `.ts`              |
| Public API (`Go`)      | `api-1.example.com`         | `api-2.example.com`        | `edge-api.example.net`             | Метаданные и stream URL             |

> Минимально закладывать по `2` домена на каждый публичный контур (основной + резервный) и заранее выпускать TLS для всех доменов.


### 5.2 Минимум для пилота (до активного роста)


| Узел                  | CPU     | RAM   | Disk       |
| --------------------- | ------- | ----- | ---------- |
| Edge+App (совмещенно) | 8 vCPU  | 16 GB | 200 GB SSD |
| Processing            | 12 vCPU | 24 GB | 2 TB SSD   |


> Для долгого буферного хранения и стабильного CDN предпочтителен object storage (S3-совместимый) вместо локального диска в origin.

## 6) Сетевые и системные требования

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

## 8) Anti-blocking стратегия (быстрая смена доменов)

### Какие компоненты могут быть заблокированы

- **Movie Portal (`WordPress`)**: блокировка публичного домена портала (`portal-*`), включая карточки фильмов и SEO-страницы.
- **Player (`Next.js` + `Fluid Player`)**: блокировка домена встраиваемого плеера (`player-*`) для `iframe` и embed-страниц.
- **Streaming Delivery (CDN/edge + media domain)**: блокировка stream-домена (`stream-*`) для выдачи `.m3u8`/`.ts`.

### Необходимые работы (объем для DevOps оценки)

1. **DNS и доменная модель**
  - Подготовить основной и резервные домены для `portal`, `player`, `stream`.
  - Настроить low TTL (`60-120` сек) и шаблон failover в DNS-провайдере.
2. **TLS и CDN/edge подготовка**
  - Выпустить и поддерживать сертификаты для всех primary/backup доменов.
  - Настроить одинаковые CDN/edge policy для основных и резервных доменов.
3. **Конфигурация приложений без хардкода**
  - Перевести публичные URL на env-переменные (`PORTAL_BASE_URL`, `PLAYER_BASE_URL`, `API_BASE_URL`, `STREAM_BASE_URL`).
  - Обеспечить reload/rolling restart без пересборки контейнеров.
4. **Проксирование и маршрутизация**
  - Настроить переключаемые upstream/route на уровне `nginx`/edge.
  - Подготовить редиректы и совместимость старых embed-ссылок.
5. **Операционный контур переключения**
  - Формализовать процедуру активации резервных доменов через DNS/CDN и конфиги.
  - Добавить health-check и smoke playback test после переключения.
6. **Наблюдаемость и контроль блокировок**
  - Метрики/алерты по росту `4xx/5xx`, timeout, ошибкам playback по регионам.
  - Дашборды для решения о переключении и верификации результата.

### Обязательные практики

1. **Domain abstraction в конфиге**
  - WordPress, Player и API не хардкодят домены.
  - Все публичные URL собираются из env-переменных:
    - `PORTAL_BASE_URL`
    - `PLAYER_BASE_URL`
    - `API_BASE_URL`
    - `STREAM_BASE_URL`
2. **Несколько резервных доменов заранее**
  - Подготовить пул: `portal-1`, `portal-2`, `player-1`, `player-2`, `stream-1`, `stream-2`.
  - Выпустить TLS сертификаты заранее.
3. **Низкий DNS TTL**
  - TTL `60-120` секунд для критичных доменов.
  - Failover через DNS/Traffic Manager.
4. **Слой прокси перед origin**
  - Быстрый перенос достигается сменой upstream на edge/proxy без пересборки приложений.
5. **Feature flag для endpoint switch**
  - Переключение stream/player/portal endpoint в runtime (через env + reload, без full redeploy).
6. **Операционный SLO переключения**
  - Целевое время активации резервных доменов: `5-15` минут.
  - Изменения выполняются через DNS/CDN + конфигурацию proxy/app без изменения кода.

---

