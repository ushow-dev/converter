# Scanner Database Schema

PostgreSQL база данных, используемая исключительно scanner-сервисом. Миграции применяются автоматически при старте через `db.init()`.

Файлы миграций: `scanner/scanner/migrations/`

---

## Таблицы

### scanner_incoming_items

Операционная очередь файлов из `incoming/`. Каждая строка — один видеофайл.

```sql
CREATE TABLE scanner_incoming_items (
    id                              BIGSERIAL PRIMARY KEY,
    source_path                     TEXT NOT NULL UNIQUE,    -- абсолютный путь к файлу
    source_filename                 TEXT NOT NULL,           -- имя файла без пути
    file_size_bytes                 BIGINT,
    first_seen_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stable_since                    TIMESTAMPTZ,             -- когда размер перестал меняться
    status                          TEXT NOT NULL DEFAULT 'new',
    review_reason                   TEXT,                    -- для failed/review_* статусов
    is_upgrade_candidate            BOOLEAN NOT NULL DEFAULT FALSE,
    quality_score                   INTEGER,                 -- 0..100, NULL если ffprobe упал
    api_item_id                     BIGINT,                  -- зарезервировано
    duplicate_of_library_movie_id   BIGINT,
    tmdb_id                         TEXT,
    normalized_name                 TEXT,                    -- "title_year_[tmdb_id]"
    title                           TEXT,
    year                            INTEGER,
    error_message                   TEXT,
    library_relative_path           TEXT,                    -- заполняется после move
    claimed_at                      TIMESTAMPTZ,             -- когда IngestWorker забрал
    claim_expires_at                TIMESTAMPTZ,             -- TTL claim
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Индексы:**

| Индекс | Колонки | Условие | Назначение |
|---|---|---|---|
| `idx_incoming_status_stable` | `(status, stable_since)` | — | scan_loop: поиск по статусу |
| `idx_incoming_dup` | `(duplicate_of_library_movie_id, status)` | — | дублей check |
| `idx_incoming_claim_expires` | `(claim_expires_at)` | `WHERE status = 'claimed'` | TTL claim expiry |

**Статусы:**

| Статус | Описание |
|---|---|
| `new` | Файл обнаружен, ожидает стабильности |
| `registered` | Прошёл metadata pipeline, готов к claim |
| `claimed` | IngestWorker забрал, ожидает копирования |
| `copying` | Идёт rclone copy |
| `copied` | rclone copy завершён, ожидает complete |
| `completed` | /complete вызван, ожидает move |
| `archived` | Перемещён в library/, финальный статус |
| `failed` | Ошибка (rclone, move, или claim TTL истёк) |
| `review_duplicate` | Дублирует existing с похожим quality_score |
| `review_unknown_quality` | ffprobe не сработал, есть existing в library |
| `skipped` | Неподдерживаемый тип файла |

---

### scanner_library_movies

Каталог фильмов в `library/`. Обновляется через UPSERT при каждом успешном move.

```sql
CREATE TABLE scanner_library_movies (
    id                      BIGSERIAL PRIMARY KEY,
    content_kind            TEXT NOT NULL DEFAULT 'movie',
    title                   TEXT NOT NULL,
    title_original          TEXT,
    normalized_name         TEXT NOT NULL UNIQUE,       -- "title_year_[tmdb_id]"
    year                    INTEGER,
    tmdb_id                 TEXT,                       -- UNIQUE WHERE NOT NULL
    imdb_id                 TEXT,
    poster_url              TEXT,
    quality_score           INTEGER NOT NULL,           -- 0..100
    quality_label           TEXT,                       -- 'HD' | 'SD' | NULL
    library_relative_path   TEXT NOT NULL,              -- "movies/normalized_name/file.mkv"
    file_size_bytes         BIGINT,
    status                  TEXT NOT NULL DEFAULT 'ready',
    source_item_id          BIGINT REFERENCES scanner_incoming_items(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Индексы:**

| Индекс | Колонки | Условие | Назначение |
|---|---|---|---|
| `idx_library_tmdb` | `(tmdb_id)` | `WHERE tmdb_id IS NOT NULL` | уникальность по TMDB |
| `idx_library_status` | `(status, updated_at DESC)` | — | фильтрация по статусу |

**Статусы:**

| Статус | Описание |
|---|---|
| `ready` | Фильм доступен в library |
| `replaced` | Заменён более качественной версией |
| `deprecated` | Помечен как устаревший (ручное управление) |

**UPSERT логика** (при каждом successful move):
```sql
ON CONFLICT (normalized_name) DO UPDATE SET
    quality_score         = EXCLUDED.quality_score,
    library_relative_path = EXCLUDED.library_relative_path,
    file_size_bytes       = EXCLUDED.file_size_bytes,
    status                = 'ready',
    updated_at            = NOW()
```

---

## Миграции

| Файл | Описание |
|---|---|
| `001_initial.sql` | Создаёт `scanner_incoming_items` и `scanner_library_movies` с базовыми индексами |
| `002_add_claim_columns.sql` | Добавляет `claimed_at`, `claim_expires_at` и `idx_incoming_claim_expires` |

Применяются автоматически при старте через `db.init(database_url)`. Порядок определяется числовым префиксом файла.

**Правило:** никогда не изменять существующие миграции — только добавлять новые.
