# Admin UI — Обзор

## Назначение

Web-интерфейс для управления медиа-системой. Доступен по `http://localhost:3000`.

## Технологический стек

| Технология | Версия | Назначение |
|---|---|---|
| Next.js | 14.2 | App Router, SSR/CSR |
| React | 18 | UI компоненты |
| Tailwind CSS | 3.4 | Стилизация |
| SWR | 2.2.5 | Data fetching, кэширование, polling |
| hls.js | 1.5.13 | HLS воспроизведение |

## Страницы

### `/login` — Аутентификация
- Форма email + password
- POST `/api/admin/auth/login`
- JWT токен сохраняется в `localStorage`
- Редирект → `/movies` при успехе

### `/` — Главная
- Auth guard: проверяет `localStorage['token']`
- Если нет токена → `/login`
- Если есть токен → `/movies`

### `/search` — Поиск торрентов
- Поиск через Prowlarr (GET `/api/admin/search?q=...`)
- Отображение: название, размер, сиды/личи, индексатор
- Кнопка "Создать задание" → POST `/api/admin/jobs`
- Поле для ввода IMDb/TMDB ID перед созданием задания

### `/upload` — Загрузка
Два режима:
1. **Загрузка файла** — `<input type="file">` + XMLHttpRequest с прогрессом
   - Endpoint: POST `/api/admin/jobs/upload` (multipart)
   - Прогресс: `xhr.upload.onprogress`
2. **HTTP загрузка** — ввод URL
   - Endpoint: POST `/api/admin/jobs/remote-download`

Поля формы: название, год, IMDb ID, TMDB ID.

### `/queue` — Очередь заданий
- SWR polling каждые 3 секунды
- Фильтрация по статусу (all/created/queued/in_progress/completed/failed)
- Курсорная пагинация
- Progress bar для каждого задания
- Ссылка на детали задания
- Кнопка удаления

### `/jobs/[jobId]` — Детали задания
- Статус, stage, прогресс
- Лог событий задания
- Thumbnail превью
- Download stats (если торрент)
- Конвертационный прогресс
- Ссылка на фильм при завершении

### `/movies` — Каталог фильмов
- Пагинированный список с poster thumbnails
- Метаданные: название, год, IMDb/TMDB ID
- Inline редактирование IMDb/TMDB ID
- HLS плеер (`VideoPlayer.tsx`) для просмотра прямо в UI
- Управление субтитрами (`SubtitleSection.tsx`)
- Удаление фильма

## Компоненты

### `Nav.tsx`
Навигационная панель:
- Ссылки: Search | Upload | Queue | Movies
- Кнопка Logout (очищает localStorage, редирект → /login)

### `VideoPlayer.tsx`
HLS плеер:
- hls.js для Chrome/Firefox
- Нативный video для Safari
- Поддержка WebVTT субтитров через `<track>`

### `SubtitleSection.tsx`
Управление субтитрами:
- Список загруженных субтитров с языком
- Форма ручной загрузки (.srt/.vtt)
- Кнопка "Авто-поиск" (POST `.../subtitles/search`)
- Статус поиска / ошибки

## API клиент (`src/lib/api.ts`)

Централизованные функции:
```typescript
login(email, password)
searchTorrents(query)
createJob(payload)
uploadFile(file, metadata, onProgress)
remoteDownload(url, metadata)
listJobs(cursor?, status?)
getJob(jobId)
deleteJob(jobId)
listMovies(cursor?)
updateMovie(movieId, payload)
deleteMovie(movieId)
searchTmdb(query)
getTmdbById(tmdbId)
listSubtitles(movieId)
uploadSubtitle(movieId, file, language)
searchSubtitles(movieId, language)
```

## Аутентификация в UI

```typescript
// Сохранение токена
localStorage.setItem('token', response.token)

// Использование в запросах
headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }

// Logout
localStorage.removeItem('token')
```

**Проблемы текущей реализации:**
- localStorage уязвим к XSS (нет httpOnly cookie)
- Нет refresh token механизма
- Токен не имеет автоматического logout по истечении на UI-стороне

## Переменные окружения Frontend

```bash
NEXT_PUBLIC_API_URL=http://localhost:8000     # URL API сервиса
NEXT_PUBLIC_PLAYER_URL=http://localhost:3001  # URL standalone плеера (для iframe)
```
