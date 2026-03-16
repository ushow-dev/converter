# FFmpeg HLS Pipeline

## Обзор

Worker конвертирует входной видеофайл (любой формат) в адаптивный HLS-стрим с тремя разрешениями.

## Входные данные

- Файл: `/media/downloads/{jobID}/{filename.ext}`
- Любой видеоформат, поддерживаемый FFmpeg (mp4, mkv, avi, mov, etc.)

## Выходные данные

```
/media/converted/{movieStorageKey}/
├── master.m3u8           # Мастер-плейлист (ссылки на все варианты)
├── 360/
│   ├── playlist_360p.m3u8
│   └── segment_000.ts, segment_001.ts, ...
├── 480/
│   ├── playlist_480p.m3u8
│   └── segment_000.ts, ...
├── 720/
│   ├── playlist_720p.m3u8
│   └── segment_000.ts, ...
└── thumbnail.jpg         # JPEG превью (кадр из видео)
```

## FFmpeg профиль (mp4_h264_aac_1080p)

```bash
ffmpeg -i {input} \
  # 360p
  -vf scale=640:360 -c:v libx264 -b:v 800k -c:a aac -b:a 128k \
  -hls_time 10 -hls_playlist_type vod \
  -hls_segment_filename /media/converted/{id}/360/segment_%03d.ts \
  /media/converted/{id}/360/playlist_360p.m3u8 \
  # 480p
  -vf scale=854:480 -c:v libx264 -b:v 1400k -c:a aac -b:a 128k \
  -hls_time 10 -hls_playlist_type vod \
  -hls_segment_filename /media/converted/{id}/480/segment_%03d.ts \
  /media/converted/{id}/480/playlist_480p.m3u8 \
  # 720p
  -vf scale=1280:720 -c:v libx264 -b:v 2800k -c:a aac -b:a 192k \
  -hls_time 10 -hls_playlist_type vod \
  -hls_segment_filename /media/converted/{id}/720/segment_%03d.ts \
  /media/converted/{id}/720/playlist_720p.m3u8
```

## Параметры HLS

| Параметр | Значение | Описание |
|---|---|---|
| Длительность сегмента | 10 секунд | `hls_time` |
| Тип плейлиста | VOD | `hls_playlist_type vod` |
| Видеокодек | H.264 (libx264) | Совместимость с hls.js |
| Аудиокодек | AAC | Стандартный для HLS |
| Выравнивание keyframe | Да | Для корректного переключения качества |

## Разрешения и битрейты

| Разрешение | Видео | Аудио | Ширина × Высота |
|---|---|---|---|
| 360p | 800 kbps | 128 kbps | 640×360 |
| 480p | 1400 kbps | 128 kbps | 854×480 |
| 720p | 2800 kbps | 192 kbps | 1280×720 |

## Мастер-плейлист (master.m3u8)

```m3u8
#EXTM3U
#EXT-X-VERSION:3

#EXT-X-STREAM-INF:BANDWIDTH=928000,RESOLUTION=640x360
360/playlist_360p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1528000,RESOLUTION=854x480
480/playlist_480p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=2992000,RESOLUTION=1280x720
720/playlist_720p.m3u8
```

## Thumbnail

Получение кадра из видео:
```bash
ffmpeg -i {input} -vframes 1 -q:v 2 -ss 00:00:10 \
  /media/converted/{id}/thumbnail.jpg
```

Если FFmpeg не может извлечь кадр (например, повреждённый файл):
- Worker загружает `backdrop` постер с TMDB
- Если TMDB не настроен — thumbnail отсутствует (допустимо)

## Управление потоками

```bash
# Env переменная FFMPEG_THREADS_PER_CONVERSION
# 0 = auto (FFmpeg сам выбирает)
# N = явное ограничение (рекомендуется: cpu_count / CONVERT_CONCURRENCY)
-threads {N}
```

## Обработка ошибок

1. FFmpeg завершился с ненулевым кодом → `status=failed`
2. Воркер логирует stderr FFmpeg
3. Временные файлы в `/media/temp/{jobID}/` удаляются
4. `/media/converted/{movieStorageKey}/` удаляется при неполной конвертации

## Этапы конвертационного воркера

```
1. Получить ConvertPayload из convert_queue
2. Установить Redis lock (NX, 1ч)
3. UPDATE media_jobs: status=in_progress, stage=convert
4. mkdir /media/converted/{movieStorageKey}/360, /480, /720
5. Запустить FFmpeg (многоразрядный)
6. Генерировать master.m3u8
7. Извлечь thumbnail
8. INSERT/UPSERT movies
9. INSERT media_assets (is_ready=true, storage_path=master.m3u8)
10. [optional] Fetch subtitles (OpenSubtitles)
11. [optional] Download TMDB backdrop
12. UPDATE media_jobs: status=completed
13. Очистить /media/downloads/{jobID}/ (исходный файл)
```

## Конфигурация окружения

| Переменная | Назначение |
|---|---|
| `CONVERT_CONCURRENCY` | Параллельные конвертации (default: 1) |
| `FFMPEG_PATH` | Путь к бинарнику ffmpeg (default: /usr/bin/ffmpeg) |
| `TMDB_API_KEY` | Для загрузки метаданных и постеров |
| `OPENSUBTITLES_API_KEY` | Для авто-субтитров |
