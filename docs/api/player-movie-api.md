# Player Movie API

Версия контракта: `v1`

## Назначение

Endpoint для плеера (Next.js), который принимает `imdb_id` или `tmdb_id`, находит фильм в таблице `movies` и возвращает ссылки на HLS-поток и постер.

Сами media-файлы отдаются напрямую nginx (или CDN), API возвращает только URL.
Для HLS API возвращает подписанный URL (query-параметры `st` и `e`) с коротким TTL.

## Авторизация

- Заголовок: `X-Player-Key: <value>`
- Без валидного ключа API возвращает `401 UNAUTHORIZED`.

## Endpoint

### `GET /api/player/movie`

Query параметры:

- `imdb_id` (string) - обязателен, если не передан `tmdb_id`
- `tmdb_id` (string) - обязателен, если не передан `imdb_id`

Правило валидации: должен быть передан ровно один параметр (`imdb_id` или `tmdb_id`).

### Пример запроса (IMDb)

```bash
curl -X GET "http://localhost:8000/api/player/movie?imdb_id=tt0133093" \
  -H "X-Player-Key: your-player-key"
```

### Пример запроса (TMDB)

```bash
curl -X GET "http://localhost:8000/api/player/movie?tmdb_id=603" \
  -H "X-Player-Key: your-player-key"
```

## Успешный ответ `200`

```json
{
  "data": {
    "movie": {
      "id": 1,
      "imdb_id": "tt0133093",
      "tmdb_id": "603"
    },
    "playback": {
      "hls": "https://media.example.com/media/converted/1/master.m3u8?e=1739200000&st=..."
    },
    "assets": {
      "poster": "https://media.example.com/media/converted/1/thumbnail.jpg?e=1739200000&st=..."
    }
  },
  "meta": {
    "version": "v1"
  }
}
```

## Ошибки

### `400 VALIDATION_ERROR`

Когда не передан ни один параметр или переданы оба сразу:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "exactly one of imdb_id or tmdb_id must be provided",
    "retryable": false,
    "correlation_id": "..."
  }
}
```

### `401 UNAUTHORIZED`

Когда отсутствует/неверный `X-Player-Key`.

### `404 NOT_FOUND`

Когда фильм не найден в таблице `movies` по указанному ID.

### `500 INTERNAL_ERROR`

При внутренних ошибках API.

## Конфигурация ссылок

- Переменная окружения API: `MEDIA_BASE_URL`
- Если задана, ссылки в ответе формируются как `<MEDIA_BASE_URL>/media/converted/<movie_id>/...`
- Если пустая, API возвращает относительные пути `/media/converted/<movie_id>/...`
- Если задан `MEDIA_SIGNING_KEY`, API добавляет подпись (`st`) и время истечения (`e`) в query string.
- TTL подписи задается через `MEDIA_SIGNING_TTL` (по умолчанию `2m`).

Это позволяет менять домен раздачи media без изменения бизнес-логики endpoint.
