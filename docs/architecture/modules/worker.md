# Модуль: Worker Service (Go)

## Назначение

Асинхронно выполняет тяжелые задачи:

- скачивание исходных файлов;
- конвертация в целевой формат;
- публикация статусов и артефактов.

## Границы ответственности

Входит в модуль:

- consume задач из очереди;
- вызовы torrent client API;
- запуск и контроль `ffmpeg`;
- обновление состояния задач в API/БД через service contract.

Не входит в модуль:

- пользовательский API;
- управление учетными данными администратора;
- хранение долговременной бизнес-истории вне API.

## Интерфейсы и зависимости

Входящие:

- `download_queue`;
- `convert_queue`.

Исходящие:

- HTTP API к `qBittorrent`;
- выполнение `ffmpeg`;
- обновление статусов через Core API или repository contract;
- доступ к media volume.

## Контракты данных

`DownloadJobPayload`:

- `job_id`, `content_type`, `source_ref`, `target_dir`, `correlation_id`.

`ConvertJobPayload`:

- `job_id`, `content_type`, `input_path`, `output_profile`, `correlation_id`.

Стандартный `JobResult`:

- `job_id`, `status`, `progress_percent`, `error_code`, `logs_ref`.

## Отказоустойчивость

- Лок на `job_id` для исключения параллельной обработки одной задачи.
- Retry на сетевые ошибки qBittorrent и transient ffmpeg failures.
- Backoff с ограничением максимального числа попыток.
- Безопасное возобновление после рестарта: worker перечитывает незавершенные задачи из очереди/БД.

## Наблюдаемость и безопасность

- Логи download/convert шагов с `job_id` и `correlation_id`.
- Метрики: время скачивания, время конвертации, число ошибок, retry count.
- Ограничение прав контейнера: только нужные volume mounts.
- Внутренний доступ к qBittorrent только по docker network.

## Точки расширения под сериалы

- Worker выполняет стратегию обработки по `content_type`.
- Слой планирования допускает дочерние units работы (будущие эпизоды), но в текущем этапе запускает только один unit.
- Конвертация работает через profile abstraction, чтобы позже поддержать разные профили на эпизоды и сезонные наборы.
