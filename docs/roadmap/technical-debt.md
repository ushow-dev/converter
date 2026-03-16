# Технический долг и приоритеты

> Документ для планирования улучшений. Обновляйте при выявлении новых проблем.

---

## КРИТИЧЕСКИЙ ПРИОРИТЕТ (Безопасность)

### 1. Секреты в .env файле
**Проблема:** `.env` файл с реальными паролями, API-ключами и секретами попал/может попасть в git.
**Риск:** Компрометация всех внешних сервисов.
**Решение:**
- Немедленно ротировать все ключи (POSTGRES_PASSWORD, JWT_SECRET, ADMIN_PASSWORD, TMDB_API_KEY, OPENSUBTITLES_API_KEY)
- Добавить `.env` в `.gitignore` (уже есть, но проверить git history)
- Рассмотреть Docker Secrets или внешний vault (HashiCorp Vault, Doppler)

### 2. JWT в localStorage (XSS уязвимость)
**Проблема:** JWT токен хранится в `localStorage` — доступен через XSS.
**Риск:** Кража токена администратора.
**Решение:** Использовать httpOnly cookies для хранения JWT.

### 3. CORS Allow-All
**Проблема:** API возвращает `Access-Control-Allow-Origin: *`.
**Риск:** Любой сайт может делать запросы к Admin API (ограничено браузерными политиками, но всё же).
**Решение:** Ограничить CORS только доверенными источниками для Admin endpoints. Для Player — оставить `*` (embed use case).

### 4. Отсутствие rate limiting
**Проблема:** Нет rate limiting на login endpoint и поисковые запросы.
**Риск:** Brute force атаки на admin password.
**Решение:** Добавить rate limiting middleware (chi middleware или nginx).

---

## ВЫСОКИЙ ПРИОРИТЕТ (Надёжность)

### 5. Нет retry для convert_queue
**Проблема:** Если FFmpeg упал — задание сразу `failed`, нет автоматического повтора.
**Решение:** Добавить `attempt` поле в ConvertPayload (как в DownloadPayload) и retry логику.

### 6. Нет dead letter queue
**Проблема:** Задания, которые постоянно падают, теряются. Нет механизма для их анализа.
**Решение:** Реализовать DLQ (Dead Letter Queue) в Redis или логировать в отдельную таблицу.

### 7. Временные файлы не очищаются при crash
**Проблема:** При крэше воркера файлы в `/media/temp/{jobID}/` остаются на диске.
**Решение:** Startup cleanup для незавершённых temp-директорий + периодическая уборка.

### 8. Нет таймаута на FFmpeg
**Проблема:** FFmpeg может зависнуть навсегда на повреждённом файле.
**Решение:** Добавить context с таймаутом при запуске ffmpeg процесса.

### 9. qBittorrent статус polling
**Проблема:** Worker постоянно опрашивает qBittorrent через HTTP. При большом числе заданий — нагрузка.
**Решение:** Увеличить интервал polling или использовать WebSocket (если qBittorrent поддерживает).

### 10. Нет graceful degradation при недоступности Prowlarr
**Проблема:** Circuit breaker есть, но UI не показывает пользователю понятную ошибку.
**Решение:** Улучшить error messages на Frontend при срабатывании circuit breaker.

---

## СРЕДНИЙ ПРИОРИТЕТ (Качество кода)

### 11. browse.go — заглушка
**Проблема:** `converter/api/internal/handler/browse.go` является placeholder без реализации.
**Решение:** Реализовать или удалить.

### 12. Дублирование модели queue payload
**Проблема:** `DownloadPayload`, `ConvertPayload` объявлены в обоих сервисах (api + worker).
**Риск:** Расхождение при изменениях.
**Решение:** Вынести в общий Go модуль (`shared/`) или использовать code generation.

### 13. Нет тестов
**Проблема:** Ни unit, ни integration тестов не обнаружено.
**Решение:** Начать с критических путей:
  - Job status transitions (unit)
  - API authentication (unit)
  - Queue payload marshaling (unit)
  - Full pipeline integration test (Docker-based)

### 14. Отсутствие структурированного логирования
**Проблема:** Логи смешивают форматы, нет structured logging (JSON).
**Решение:** Использовать `slog` (Go stdlib) или `zerolog` для structured logs.

### 15. Нет observability
**Проблема:** Нет метрик (Prometheus), нет трассировки (OpenTelemetry).
**Решение:** Добавить prometheus middleware для API, счётчики для worker.

### 16. Frontend использует localStorage для JWT
**Проблема:** (Повтор из безопасности) также нет auto-logout при expiry токена.
**Решение:** Добавить interceptor, который при 401 ответе чистит токен и редиректит на /login.

---

## НИЗКИЙ ПРИОРИТЕТ (Улучшения)

### 17. Нет 1080p варианта HLS
**Проблема:** Максимальное разрешение 720p. Для 4K контента — потеря качества.
**Решение:** Добавить 1080p профиль в FFmpeg настройки (с условием: если источник >= 1080p).

### 18. Nginx не включён по умолчанию
**Проблема:** `ptrack.ink.conf` существует, но nginx закомментирован в docker-compose.
**Решение:** Включить nginx как production-ready конфигурацию с документацией.

### 19. Нет refresh token
**Проблема:** JWT токен одноразовый, нет механизма обновления без logout.
**Решение:** Добавить refresh token endpoint.

### 20. Отсутствие документации по настройке Prowlarr
**Проблема:** Нет инструкций по добавлению индексаторов.
**Решение:** Добавить `docs/operations/prowlarr-setup.md`.

### 21. movie_storage_key генерация
**Проблема:** Ключ `mov_{md5hash}` генерируется непрозрачно. Нет понятной документации алгоритма.
**Решение:** Задокументировать алгоритм генерации.

### 22. Admin password plaintext support
**Проблема:** `ADMIN_PASSWORD` может быть задан как plaintext (без bcrypt). Опасно для production.
**Решение:** Требовать bcrypt формат в production, валидировать при старте.

---

## Предложенная структура директорий (улучшения)

Текущая структура хорошая, но можно улучшить:

```
converter/
├── api/                    ✓ (хорошо)
├── worker/                 ✓ (хорошо)
├── frontend/               ✓ (хорошо)
├── shared/                 ← НОВОЕ: общие Go типы (queue payloads)
│   └── model/
│       ├── jobs.go
│       └── queue.go
├── docs/                   ✓ (хорошо, расширено)
├── scripts/                ← НОВОЕ: утилиты (backup, cleanup, migration)
│   ├── backup.sh
│   └── cleanup-temp.sh
├── tests/                  ← НОВОЕ: integration тесты
│   └── e2e/
└── docker-compose.yml      ✓
```

---

## Метрики для мониторинга (предложение)

При добавлении observability:

| Метрика | Тип | Описание |
|---|---|---|
| `jobs_created_total` | Counter | Всего созданных заданий |
| `jobs_completed_total` | Counter | Успешно завершённых |
| `jobs_failed_total` | Counter | Неудачных |
| `conversion_duration_seconds` | Histogram | Время конвертации |
| `download_queue_size` | Gauge | Текущий размер очереди |
| `convert_queue_size` | Gauge | Текущий размер очереди |
| `prowlarr_circuit_breaker_state` | Gauge | Состояние circuit breaker |
| `media_storage_bytes` | Gauge | Объём медиа на диске |
