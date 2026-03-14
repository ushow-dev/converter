# DevOps архитектура

## 1) Цели и вводные

- Текущая стадия продукта: `MVP`.
- Текущая реализация инфраструктуры: все сервисные пайплайны развернуты в `docker compose`.
- Плеер: `player.js` (Fluid Player), встраивается на внешние сайты через `iframe`, обслуживается с Server D.
- Портал с фильмами: `WordPress` (публичный каталог и SEO-страницы фильмов), обслуживается с Server B.
- Backend API: `Go` (отдает метаданные и `HLS` ссылку для плеера), обслуживается с Server B.
- Раздача видео: `nginx` + CDN слой, обслуживается с Server D.
- Контент-пайплайн: админка -> downloader -> converter (`ffmpeg`) -> HLS -> Server D.
- База: PostgreSQL с данными о фильмах и ссылками на поток.
- CI/CD: `GitLab` (server + runner) на Server B.
- Качества HLS: `360p`, `480p`, `720p`.
- Стартовый каталог: ~`100` фильмов.
- Нагрузка на старте: ~`1 000 000` просмотров/месяц.
- Ограничение: могут блокироваться домен `WordPress` и домен с плеером, нужны механизмы быстрого переключения на резервные домены.

## 2) Роли компонентов

1. **Player (`player.js` + Fluid Player)**
  - Обслуживается `nginx` с Server D.
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
  - Развернуты на Server B рядом с API.
5. **Downloader + Converter Worker**
  - Скачивает источник.
  - Транскодирует в HLS профили `360/480/720`.
  - Публикует готовый контент в Object Storage / Media Origin на Server D.
6. **Data Layer**
  - `PostgreSQL` (каталог фильмов, статус готовности, ссылки потоков).
  - `Redis` (очереди задач).
7. **Streaming Delivery**
  - Origin (`nginx`) на Server D хранит/читает HLS и отдает `player.js`.
  - CDN кэширует `.m3u8` и `.ts` на edge.
  - Два резервных CDN/edge-домена (`stream-1`, `stream-2`) для anti-blocking.
8. **CI/CD (GitLab)**
  - `gitlab-server` — хранение кода, MR, pipelines.
  - `gitlab-runner` — выполнение сборки и деплоя.
  - Оба компонента на Server B.

## 3) Целевая схема окружения

```text
[External Sites]
   iframe src=https://player.example.com/embed/tt1234567
            |
            v
[Public Users]
   |
   +--> https://portal.example.com (WordPress)   [зеленый путь]
   |           |
   |           v
   |   [Edge Proxy / CDN — Server A]
   |   [Edge Proxy / CDN — backup-1 ]   <- резервные для portal + api
   |   [Edge Proxy / CDN — backup-2 ]
   |           |
   |           v
   |   [Server B: App Cluster]
   |     - WordPress (portal)
   |     - Admin UI
   |     - Go API (public + admin)
   |     - PostgreSQL
   |     - Redis
   |     - gitlab-server
   |     - gitlab-runner
   |           |
   |           v
   |   [Server C: Processing Worker]
   |     - downloader
   |     - converter worker (ffmpeg)
   |           |
   |           v (публикация HLS)
   |
   +--> https://player.example.com/embed/...     [оранжевый путь]
               |
               v
       [Edge Proxy / CDN — Server D (primary)]
         [CDN node stream-1]  <- резервные для player + stream
         [CDN node stream-2]
               |
               v
       [Server D: HLS Stream + Player]
         - nginx (отдает player.js и HLS)
         - Object Storage / Media Origin
             /movies/{imdb_id}/master.m3u8
             /movies/{imdb_id}/720/*.ts
             /movies/{imdb_id}/480/*.ts
             /movies/{imdb_id}/360/*.ts
```

**Два независимых CDN-пути:**

- **Портальный (зелёный):** пользователь → Edge (Server A) → Server B (WordPress, Go API, Admin UI).
- **Плеерный (оранжевый):** пользователь → Edge (Server D CDN) → Server D (nginx, player.js, HLS stream).

Разделение снижает риск: блокировка портала не затрагивает воспроизведение, и наоборот.

## 4) Разделение по серверам

### Сервер A: Edge / Delivery (портальный путь)

- `nginx` reverse proxy + TLS termination для портала и API.
- Подключение CDN (Cloudflare/CloudFront/Bunny/аналог).
- Основной + 2 резервных CDN/edge-домена для `portal` и `api`.
- Проксирование трафика на Server B.

**Профиль:** высокая сеть, умеренный CPU.

### Сервер B: App + Data + CI/CD

- `Go API` (public + admin).
- `Admin UI`.
- `WordPress` портал (frontend каталог фильмов).
- `PostgreSQL`, `Redis`.
- `gitlab-server` (хранение кода и pipelines).
- `gitlab-runner` (выполнение CI/CD задач).

**Профиль:** стабильность, IOPS для БД, умеренный CPU.

### Сервер C: Processing

- `Downloader` + `Converter Worker` + `ffmpeg`.
- Временное storage для конвертации.
- Публикация готовых HLS в Object Storage на Server D.

**Профиль:** CPU-heavy + disk I/O.

### Сервер D: HLS Stream + Player + Media Origin

- `nginx` — отдает `player.js` (embed-страницы) и HLS (`master.m3u8`, `.ts`).
- Object Storage / Media Origin (локальный диск или S3-совместимое хранилище).
- CDN-edge с двумя резервными доменами (`stream-1`, `stream-2`) для anti-blocking.
- TLS для всех доменов стриминга и плеера.

**Профиль:** высокая сеть + disk I/O (HLS раздача).

## 5) Требования к железу (для оценки стоимости)

### 5.1 Стартовый production (1M просмотров/мес, 100 фильмов)


| Узел                    | CPU     | RAM      | Disk            | Network | Примечание                           |
| ----------------------- | ------- | -------- | --------------- | ------- | ------------------------------------ |
| Edge/Delivery (A)       | 4 vCPU  | 8 GB     | 80-120 GB SSD   | 1 Gbps  | TLS, reverse proxy, CDN origin       |
| App+Data+CI/CD (B)      | 8 vCPU  | 16-32 GB | 500 GB NVMe     | 1 Gbps  | API + Postgres + Redis + GitLab      |
| Processing (C)          | 16 vCPU | 32 GB    | 2-4 TB NVMe/SSD | 1 Gbps  | ffmpeg, temp files, downloader       |
| HLS Stream + Player (D) | 4 vCPU  | 8 GB     | 2-4 TB SSD      | 1 Gbps  | nginx HLS + player.js + media origin |


### 5.1.1 Прокси и домены (основные + резервные)


| Контур                | Основной домен (пример) | Резервный домен (пример) | Прокси/CDN endpoint       | Назначение                          |
| --------------------- | ----------------------- | ------------------------ | ------------------------- | ----------------------------------- |
| Portal (`WordPress`)  | `portal-1.example.com`  | `portal-2.example.com`   | `edge-portal.example.net` | Публичный каталог и страницы фильма |
| Player (`player.js`)  | `player-1.example.com`  | `player-2.example.com`   | `edge-player.example.net` | `iframe` и embed-страницы           |
| Stream (HLS delivery) | `stream-1.example.com`  | `stream-2.example.com`   | `edge-stream.example.net` | Выдача `.m3u8` и `.ts`              |
| Public API (`Go`)     | `api-1.example.com`     | `api-2.example.com`      | `edge-api.example.net`    | Метаданные и stream URL             |


> Минимально закладывать по `2` домена на каждый публичный контур (основной + резервный) и заранее выпускать TLS для всех доменов.

### 5.2 Минимум для пилота (до активного роста)


| Узел                  | CPU     | RAM   | Disk       |
| --------------------- | ------- | ----- | ---------- |
| Edge+App (совмещенно) | 8 vCPU  | 16 GB | 200 GB SSD |
| Processing            | 12 vCPU | 24 GB | 2 TB SSD   |
| HLS Stream + Player   | 4 vCPU  | 8 GB  | 2 TB SSD   |


> Для долгого буферного хранения и стабильного CDN предпочтителен object storage (S3-совместимый) вместо локального диска в origin.

## 6) Сетевые и системные требования

### Порты и доступ

- В интернет: только `80/443` на Edge (Server A) и на Server D (nginx).
- Внутренний доступ:
  - App -> DB: `5432`
  - App/Worker -> Redis: `6379`
  - Worker -> Object Storage (Server D или S3): `443`
  - GitLab runner -> GitLab server: внутренняя сеть
- Админка и admin API ограничены по IP/VPN + MFA.
- GitLab web UI ограничен по IP/VPN.

### Nginx/CDN policy

- `.ts`: `Cache-Control: public, max-age=31536000, immutable`
- `.m3u8`: `Cache-Control: public, max-age=10-30`
- CORS для плеера (`GET, HEAD, OPTIONS`).
- Range requests включены.

## 7) CI/CD (GitLab)

- `gitlab-server` на Server B хранит репозитории и управляет pipelines.
- `gitlab-runner` на Server B выполняет сборку Docker-образов и деплой через `docker compose`.
- Pipeline: push → build → deploy (rolling restart контейнеров без downtime).
- Секреты (env-переменные, SSH-ключи) хранятся в GitLab CI/CD Variables.

## 8) Anti-blocking стратегия (быстрая смена доменов)

### Какие компоненты могут быть заблокированы

- **Movie Portal (`WordPress`)**: блокировка публичного домена портала (`portal-`*), включая карточки фильмов и SEO-страницы.
- **Player (`player.js`)**: блокировка домена встраиваемого плеера (`player-`*) для `iframe` и embed-страниц.
- **Streaming Delivery (CDN/edge + media domain)**: блокировка stream-домена (`stream-`*) для выдачи `.m3u8`/`.ts`.

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

---

