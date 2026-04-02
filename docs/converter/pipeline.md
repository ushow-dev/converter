# FFmpeg HLS Pipeline

## Обзор

Worker конвертирует входной видеофайл (любой формат) в адаптивный HLS-стрим с тремя разрешениями.

## Входные данные

- Файл: `/media/downloads/{jobID}/{filename.ext}`
- Любой видеоформат, поддерживаемый FFmpeg (mp4, mkv, avi, mov, etc.)

## Выходные данные

### Фильм
```
/media/converted/movies/{movieStorageKey}/
├── master.m3u8           # Мастер-плейлист (ссылки на все варианты)
├── 360/
│   ├── index.m3u8
│   └── seg000.ts, seg001.ts, ...
├── 480/
│   ├── index.m3u8
│   └── seg000.ts, ...
├── 720/
│   ├── index.m3u8
│   └── seg000.ts, ...
└── thumbnail.jpg         # JPEG превью (кадр из видео или TMDB backdrop)
```

### Эпизод сериала
```
/media/converted/series/{seriesKey}/sNN/eNN/
├── master.m3u8
├── 360/
│   ├── index.m3u8
│   └── seg000.ts, ...
├── 480/
│   ├── index.m3u8
│   └── seg000.ts, ...
├── 720/
│   ├── index.m3u8
│   └── seg000.ts, ...
└── thumbnail.jpg
```

Где `sNN` = `s01`, `s02`, ... (номер сезона, ноль-дополненный), `eNN` = `e01`, `e02`, ... (номер эпизода).

## FFmpeg профиль (hls_720_480_360)

Одна команда FFmpeg создаёт все три варианта за один проход через `filter_complex` и `-var_stream_map`:

```bash
ffmpeg -i {input} \
  -filter_complex "[0:v]split=3[v720][v480][v360]; \
    [v720]scale=-2:720:flags=bicubic[v720o]; \
    [v480]scale=-2:480:flags=bicubic[v480o]; \
    [v360]scale=-2:360:flags=bicubic[v360o]" \
  \
  # 720p
  -map [v720o] -map 0:a:0 \
  -c:v:0 libx264 -preset fast -profile:v:0 high -level:v:0 4.0 \
  -b:v:0 1050k -maxrate:v:0 1155k -bufsize:v:0 2300k \
  -c:a:0 aac -b:a:0 80k -ar:a:0 48000 -ac:a:0 2 \
  \
  # 480p
  -map [v480o] -map 0:a:0 \
  -c:v:1 libx264 -preset fast -profile:v:1 high -level:v:1 4.0 \
  -b:v:1 700k  -maxrate:v:1 770k  -bufsize:v:1 1540k \
  -c:a:1 aac -b:a:1 80k -ar:a:1 48000 -ac:a:1 2 \
  \
  # 360p
  -map [v360o] -map 0:a:0 \
  -c:v:2 libx264 -preset fast -profile:v:2 high -level:v:2 4.0 \
  -b:v:2 365k  -maxrate:v:2 400k  -bufsize:v:2 800k \
  -c:a:2 aac -b:a:2 80k -ar:a:2 48000 -ac:a:2 2 \
  \
  -f hls -hls_time 4 -hls_playlist_type vod \
  -hls_flags independent_segments \
  -var_stream_map "v:0,a:0,name:720 v:1,a:1,name:480 v:2,a:2,name:360" \
  -hls_segment_filename {outputDir}/%v/seg%03d.ts \
  -master_pl_name master.m3u8 \
  {outputDir}/%v/index.m3u8
```

> Если у источника нет аудиодорожки, FFmpeg подмешивает беззвучный аудиоисточник (`anullsrc`).

## Мульти-аудио (Multi-audio)

Начиная с поддержки сериалов, FFmpeg маппит **все** аудиодорожки источника — не только первую (`0:a:0`).

Процесс:
1. `probe.go` вызывает `ffprobe -show_streams` и парсит все дорожки типа `audio`.
2. Для каждой дорожки извлекаются: `index`, `codec_name`, `tags.language`, `tags.title`.
3. FFmpeg получает отдельный `-map 0:a:N` для каждой аудиодорожки на каждое видеоразрешение.
4. `var_stream_map` расширяется: `"v:0,a:0,a:1,name:720 v:1,a:0,a:1,name:480 ..."`.
5. После конвертации все аудиодорожки записываются в таблицу `audio_tracks` (language, index, label).

Если у источника только одна дорожка — поведение идентично прежнему.

## Параметры HLS

| Параметр | Значение | Описание |
|---|---|---|
| Длительность сегмента | **4 секунды** | `hls_time 4` — стандарт Apple HLS Authoring Spec, быстрое ABR-переключение |
| GOP | `round(4 × fps)` | Вычисляется автоматически по FPS источника; совпадает с границами сегментов |
| Тип плейлиста | VOD | `hls_playlist_type vod` |
| Флаги | `independent_segments` | Каждый сегмент декодируется независимо |
| Видеокодек | H.264 (libx264) | Совместимость с hls.js и всеми браузерами |
| Аудиокодек | AAC 80 kbps | Стандартный для HLS |
| Выравнивание keyframe | Да (`-sc_threshold 0` + явный GOP) | Для корректного переключения качества |

## Разрешения и битрейты

| Разрешение | Видео | Аудио | Масштабирование |
|---|---|---|---|
| 720p | 1050 kbps (max 1155k) | 80 kbps | `-2:720` bicubic |
| 480p | 700 kbps (max 770k) | 80 kbps | `-2:480` bicubic |
| 360p | 365 kbps (max 400k) | 80 kbps | `-2:360` bicubic |

> `-2:N` означает: высота = N пикселей, ширина выбирается автоматически кратной 2 (сохраняет соотношение сторон).

## Мастер-плейлист (master.m3u8)

```m3u8
#EXTM3U
#EXT-X-VERSION:3

#EXT-X-STREAM-INF:BANDWIDTH=1130000,RESOLUTION=1280x720
720/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=780000,RESOLUTION=854x480
480/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=445000,RESOLUTION=640x360
360/index.m3u8
```

## Thumbnail

Worker пробует два источника по приоритету:

1. **TMDB backdrop** — загружается после конвертации если задан `TMDB_API_KEY`
2. **FFmpeg кадр** — извлекается из видео если TMDB недоступен или backdrop не найден

```bash
ffmpeg -i {input} -vframes 1 -q:v 2 -ss 00:00:10 \
  /media/converted/movies/{storageKey}/thumbnail.jpg
```

Если ни один источник не сработал — thumbnail отсутствует (допустимо, плеер работает без него).

## Управление потоками

```
CONVERT_CONCURRENCY=N  →  ffmpegThreads = max(1, cpu_count / N)
```

FFmpeg запускается с `-threads {ffmpegThreads}` чтобы не монополизировать CPU при параллельных конвертациях.

## Обработка ошибок

1. FFmpeg завершился с ненулевым кодом → `status=failed`, stderr пишется в лог
2. Временные файлы в `/media/temp/{jobID}/` удаляются
3. `/media/converted/movies/{movieStorageKey}/` удаляется при неполной конвертации

## Этапы конвертационного воркера

### Ветка фильма (content_type = "movie")
```
1. Получить ConvertPayload из convert_queue
2. Установить Redis lock (NX, 1 ч)
3. UPDATE media_jobs: status=in_progress, stage=convert
4. UPSERT movies → получить movie.StorageKey
5. mkdir /media/converted/movies/{storageKey}/360, /480, /720
6. Probe FPS и аудиодорожки источника (probe.go → ffprobe)
7. Вычислить GOP по FPS
8. Запустить FFmpeg (один проход, все три варианта, все аудиодорожки)
9. Сгенерировать master.m3u8
10. Извлечь thumbnail (FFmpeg кадр)
11. Загрузить TMDB backdrop (опционально, заменяет thumbnail)
12. INSERT media_assets (is_ready=true, storage_path=master.m3u8)
13. Fetch субтитры (OpenSubtitles, опционально)
14. UPDATE media_jobs: status=completed
15. Очистить /media/downloads/{jobID}/
16. RPUSH transfer_queue {TransferMessage} (если RCLONE_REMOTE задан)
```

### Ветка эпизода сериала (content_type = "episode")
```
1. Получить ConvertPayload из convert_queue (поля series_id, season_number, episode_number заполнены)
2. Установить Redis lock (NX, 1 ч)
3. UPDATE media_jobs: status=in_progress, stage=convert
4. UPSERT series → seasons → episodes → получить episode.StorageKey
5. mkdir /media/converted/series/{seriesKey}/sNN/eNN/360, /480, /720
6. Probe FPS и аудиодорожки источника (probe.go → ffprobe)
7. Вычислить GOP по FPS
8. Запустить FFmpeg (один проход, все три варианта, все аудиодорожки)
9. Сгенерировать master.m3u8
10. Извлечь thumbnail (FFmpeg кадр)
11. Загрузить TMDB TV metadata для эпизода (заголовок, описание, still_path)
12. INSERT episode_assets (is_ready=true, storage_path=master.m3u8)
13. INSERT audio_tracks (по результатам probe)
14. Fetch субтитры эпизода (OpenSubtitles, опционально → episode_subtitles)
15. UPDATE media_jobs: status=completed
16. Очистить /media/downloads/{jobID}/
17. RPUSH transfer_queue {TransferMessage} (если RCLONE_REMOTE задан)
```

## Этап переноса (transfer stage)

После успешной конвертации converter отправляет сообщение в `transfer_queue`. `TransferWorker` переносит HLS-файлы на удалённый сервер:

```
convert → [HLS готов в /media/converted/movies/<Title (Year)>/]
        → transfer_queue → rclone move → remote:/storage/movies/<Title (Year)>/
        → movies.storage_location_id обновлён
```

Если `RCLONE_REMOTE` не задан — сообщение не отправляется, файлы остаются локально.

После успешного переноса:
- `movies.storage_location_id` обновляется на ID удалённого хранилища
- Плеер получает `base_url` из `storage_locations` и строит URL к сегментам
- Если `base_url` пустой (прокси ещё не готов) — плеер использует `MEDIA_BASE_URL` (локальный доступ)

## Конфигурация окружения

| Переменная | Назначение |
|---|---|
| `CONVERT_CONCURRENCY` | Параллельные конвертации (default: 1) |
| `TMDB_API_KEY` | Для загрузки метаданных, постеров и TV-информации эпизодов |
| `OPENSUBTITLES_API_KEY` | Для авто-субтитров (фильмы и эпизоды) |
| `RCLONE_REMOTE` | Имя rclone remote для переноса файлов (например `myserver:`); если не задан — перенос отключён |
