# P2P HLS Streaming

## Обзор

P2P HLS позволяет зрителям одного контента обмениваться `.ts` сегментами через WebRTC, снижая нагрузку на origin/CDN. Реализован на базе [p2p-media-loader](https://github.com/Novage/p2p-media-loader) v2 (MIT license).

## Как работает

```
Зритель A (первый)
  └─ Скачивает все сегменты с CDN (HTTP)
  └─ Кеширует сегменты локально
  └─ Регистрируется в tracker (wss://t.pimor.online)

Зритель B (второй)
  └─ Подключается к tracker, находит Зрителя A в swarm
  └─ Часть сегментов получает от Зрителя A (WebRTC DataChannel)
  └─ Остальное — с CDN
  └─ Сам становится источником для следующих зрителей

Зритель C, D, ...
  └─ Чем больше зрителей — тем выше доля P2P
```

### Swarm

Зрители объединяются в **swarm** по URL master-плейлиста. Все, кто смотрит один фильм, попадают в один swarm и могут обмениваться сегментами.

### Tracker

WebTorrent tracker (`wt-tracker`) — легковесный сигнальный сервер. Не передаёт медиа-данные, только помогает peers найти друг друга. Работает на API-сервере (178.104.100.36) как Docker-контейнер, доступен по `wss://t.pimor.online`.

### Проактивное кеширование

p2p-media-loader скачивает сегменты **вперёд текущей позиции воспроизведения**, чтобы иметь что раздать другим peers. Это происходит даже когда видео на паузе. Поведение контролируется параметром `cachedSegmentsCount` в конфиге.

## Архитектура компонентов

```
┌─────────────────────────────────────────────────────────┐
│  Browser (player.pimor.online)                          │
│                                                         │
│  ┌──────────┐    ┌──────────────────┐    ┌───────────┐  │
│  │ hls.js   │◄──►│ p2p-media-loader │◄──►│ WebRTC    │  │
│  │          │    │ (mixin)          │    │ DataChan. │  │
│  └────┬─────┘    └────────┬─────────┘    └─────┬─────┘  │
│       │                   │                     │        │
│       ▼                   ▼                     ▼        │
│  HTTP (CDN)         WSS (tracker)          Peer (P2P)    │
└───────┼───────────────────┼─────────────────────┼────────┘
        │                   │                     │
        ▼                   ▼                     │
   media.pimor.online  t.pimor.online             │
   (storage server)    (API server)               │
                                            Другие зрители
```

## Feature Flag

| Переменная | Описание | Default |
|---|---|---|
| `NEXT_PUBLIC_P2P_ENABLED` | Включить/выключить P2P | `false` |
| `NEXT_PUBLIC_P2P_TRACKER_URL` | URL WebSocket tracker | `wss://t.pimor.online` |
| `NEXT_PUBLIC_API_URL` | URL API для отправки метрик | — |

Переменные baked-in при сборке Next.js (`NEXT_PUBLIC_`). Для изменения нужен rebuild player.

## Метрики и мониторинг

### Сбор метрик (клиент → API)

Каждая вкладка плеера собирает статистику через `p2pMetrics.ts`:

| Метрика | Тип | Описание |
|---|---|---|
| `http_bytes` | counter (reset каждые 30s) | Байты загруженные с CDN за окно |
| `p2p_bytes` | counter (reset каждые 30s) | Байты полученные от peers за окно |
| `http_segments` | counter (reset каждые 30s) | Количество сегментов с CDN |
| `p2p_segments` | counter (reset каждые 30s) | Количество сегментов от peers |
| `peers` | gauge | Кол-во подключённых peers (по Set peerId) |

Каждые 30 секунд отправляется `POST /api/player/p2p-metrics` на API.

### Prometheus метрики (API → Prometheus)

API агрегирует данные от всех клиентов и отдаёт на `GET /metrics`:

| Метрика | Тип | Описание |
|---|---|---|
| `converter_p2p_bytes_total{source="http"}` | counter | Суммарные байты с CDN |
| `converter_p2p_bytes_total{source="p2p"}` | counter | Суммарные байты через P2P |
| `converter_p2p_segments_total{source="http"}` | counter | Суммарные сегменты с CDN |
| `converter_p2p_segments_total{source="p2p"}` | counter | Суммарные сегменты через P2P |
| `converter_p2p_peers` | gauge | Последнее полученное кол-во peers |

### Grafana дашборд

**P2P HLS Overview** (`/d/p2p-hls-overview/`) — 5 панелей:

| Панель | Что показывает |
|---|---|
| **P2P Ratio** | % трафика через P2P: `p2p_bytes / total_bytes * 100` |
| **Active Peers** | Количество подключённых peers |
| **Bandwidth Saved** | Скорость P2P vs HTTP (bytes/sec), stacked area |
| **Segments by Source** | Сегменты/сек по источнику (P2P vs HTTP) |
| **Total Bytes Transferred** | Абсолютные значения HTTP total и P2P total |

### Как читать дашборд

- **P2P Ratio 0%** — P2P не работает или только один зритель
- **P2P Ratio 20-30%** — нормально для 2-4 зрителей
- **P2P Ratio 50%+** — хорошая экономия, много одновременных зрителей
- **Active Peers 0** — никто не смотрит или WebRTC заблокирован
- **Bandwidth Saved растёт** — CDN трафик снижается

## Ограничения

- **Минимум 2 зрителя** одного контента одновременно для P2P
- **WebRTC за NAT** — может не работать за строгими firewall/NAT без TURN-сервера
- **iOS Safari** — WebRTC DataChannels поддерживаются с iOS 17.2+
- **iframe Permissions-Policy** — сторонние сайты могут блокировать WebRTC
- **Проактивное кеширование** — сегменты качаются вперёд даже на паузе (см. TODO в roadmap)

## Конфигурация P2P

Текущий конфиг в `PlayerClient.tsx` → `getP2PConfig()`:

```typescript
{
  core: {
    announceTrackers: ['wss://t.pimor.online'],
    rtcConfig: {
      iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
    },
  },
}
```

### Доступные параметры core

| Параметр | Описание | Default |
|---|---|---|
| `announceTrackers` | URL tracker-серверов | — |
| `rtcConfig` | WebRTC ICE configuration | — |
| `cachedSegmentsCount` | Сколько сегментов кешировать вперёд | ~50 |
| `simultaneousHttpDownloads` | Параллельные HTTP загрузки | 2 |
| `simultaneousP2PDownloads` | Параллельные P2P загрузки | 3 |
| `httpDownloadProbability` | Вероятность выбора HTTP вместо P2P | 0.06 |

## Файлы

| Файл | Назначение |
|---|---|
| `player/src/app/PlayerClient.tsx` | Интеграция p2p-media-loader с hls.js |
| `player/src/app/p2pMetrics.ts` | Сбор и отправка P2P метрик |
| `api/internal/handler/player.go` | `POST /api/player/p2p-metrics` endpoint |
| `api/internal/handler/metrics.go` | `GET /metrics` Prometheus endpoint |
| `infra/wt-tracker/` | Dockerfile и конфиг tracker-сервера |
| `infra/grafana/provisioning/dashboards/p2p-overview.json` | Grafana дашборд |

## Связанные решения

- [ADR-010: P2P HLS streaming](../decisions/ADR-010-p2p-hls-streaming.md)
- [ADR-004: MD5 URL signing](../decisions/ADR-004-md5-url-signing.md) — если signing включён, нужен custom `swarmId`
