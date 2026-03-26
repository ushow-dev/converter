# Architecture Decision Records (ADR)

ADR — это короткие документы, фиксирующие **архитектурное решение**: контекст, варианты, выбор и последствия.

## Зачем

- Новый разработчик (или AI) понимает **почему** система устроена так, а не иначе
- Устраняет повторные споры об одних и тех же решениях
- Даёт основу для ревизии устаревших решений

## Когда создавать ADR

Создавайте ADR при любом из условий:
- Выбор нового инфраструктурного компонента (БД, брокер, кэш)
- Изменение схемы аутентификации или авторизации
- Изменение формата очереди или контракта между сервисами
- Добавление нового сервиса или удаление существующего
- Выбор стратегии хранения или доступа к данным
- Любое решение, которое будет **сложно отменить**

## Статусы

| Статус | Значение |
|---|---|
| `proposed` | Предложено, обсуждается |
| `accepted` | Принято и применяется |
| `deprecated` | Устарело, заменено другим ADR |
| `superseded by ADR-NNN` | Заменено конкретным ADR |

## Нумерация

Файлы именуются: `ADR-NNN-короткое-название.md`
Следующий номер: **011**

## Создание нового ADR

```bash
./scripts/new-adr.sh "название решения"
# Создаёт: docs/decisions/ADR-007-название-решения.md
```

Или скопировать шаблон вручную:
```bash
cp docs/decisions/ADR-000-template.md docs/decisions/ADR-007-название.md
```

## Список решений

| № | Решение | Статус |
|---|---|---|
| [ADR-001](ADR-001-redis-blpop-queues.md) | Redis BLPOP для очередей заданий | accepted |
| [ADR-002](ADR-002-two-go-modules.md) | Два отдельных Go-модуля (api + worker) | accepted |
| [ADR-003](ADR-003-cursor-pagination.md) | Курсорная пагинация вместо offset | accepted |
| [ADR-004](ADR-004-md5-url-signing.md) | MD5 подписывание медиа-URL | accepted |
| [ADR-005](ADR-005-hls-multiresolution.md) | Мульти-разрешение HLS (360/480/720p) | accepted |
| [ADR-006](ADR-006-dual-auth-schemes.md) | Два режима аутентификации (JWT + API Key) | accepted |
| [ADR-007](ADR-007-remote-storage-rclone.md) | Удалённое хранилище медиафайлов через rclone | accepted |
| [ADR-008](ADR-008-incoming-scanner-api-driven-ingest-split.md) | Incoming scanner — API-driven ingest split | accepted |
| [ADR-009](ADR-009-scanner-as-ingest-api-server.md) | Scanner как HTTP-сервер для ingest (инверсия) | accepted |
| [ADR-010](ADR-010-p2p-hls-streaming.md) | P2P HLS streaming через p2p-media-loader | accepted |
