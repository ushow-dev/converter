# Conventional Commits

Проект использует [Conventional Commits](https://www.conventionalcommits.org/) — стандарт оформления сообщений коммитов.

## Формат

```
<тип>(<скоуп>): <описание>

[опциональное тело]

[опциональный футер]
```

**Правила описания:**
- Начинается со строчной буквы
- Без точки в конце
- Не длиннее 100 символов
- Краткое и ёмкое (что сделано, не как)

---

## Типы

| Тип | Когда использовать |
|---|---|
| `feat` | Новая функциональность |
| `fix` | Исправление бага |
| `docs` | Только изменения документации |
| `style` | Форматирование, пробелы (не влияет на логику) |
| `refactor` | Рефакторинг без новой функциональности и без фиксов |
| `perf` | Улучшение производительности |
| `test` | Добавление или исправление тестов |
| `chore` | Обновление зависимостей, конфигурации |
| `ci` | Изменения CI/CD пайплайна |
| `build` | Изменения системы сборки (Dockerfile, Makefile) |
| `revert` | Откат предыдущего коммита |

---

## Скоупы

| Скоуп | Что затрагивает |
|---|---|
| `api` | Go API сервис (`api/`) |
| `worker` | Go воркер (`worker/`) |
| `frontend` | Next.js Admin UI (`frontend/`) |
| `docker` | Docker Compose, Dockerfiles |
| `docs` | Документация (`docs/`) |
| `deps` | Зависимости (go.mod, package.json) |
| `auth` | Аутентификация и авторизация |
| `queue` | Redis очереди и payload-форматы |
| `player` | Player API и плеер |
| `subtitles` | Субтитры |
| `ffmpeg` | FFmpeg профили и конвертация |
| `db` | Миграции и схема БД |
| `config` | Конфигурация и env-переменные |

Скоуп можно опустить, если изменение глобальное или не укладывается в один модуль.

---

## Примеры

### Хорошие коммиты

```
feat(api): add rate limiting middleware for login endpoint
```
```
fix(worker): handle ffmpeg timeout on corrupted input files
```
```
docs(contracts): update ConvertPayload with attempt field
```
```
chore(deps): bump pgx from v5.7.2 to v5.7.3
```
```
refactor(api): extract job status transitions to separate method
```
```
feat(worker): add retry logic for convert queue (max 3 attempts)
```
```
fix(frontend): redirect to login on 401 response
```
```
ci: add github actions workflow for go build and vet
```
```
build(docker): reduce api image size with alpine 3.21
```
```
test(api): add unit tests for jwt token generation
```

### Коммит с телом (для сложных изменений)

```
fix(worker): prevent orphaned temp files on ffmpeg crash

Previously, if ffmpeg crashed mid-conversion, files in /media/temp/{jobID}/
were left on disk. Added defer-based cleanup that runs regardless of exit path.

Closes #42
```

### Breaking change

```
feat(api): version all endpoints under /api/v1/

BREAKING CHANGE: All /api/admin/* routes now require /api/v1/admin/* prefix.
Update frontend NEXT_PUBLIC_API_URL and any external clients.
```

### Плохие коммиты (так не надо)

```
fix bug                          ← нет типа и описания
WIP                              ← не информативно
Update files                     ← что именно?
Fixed the thing that was broken  ← что за вещь?
feat(api): Add Rate Limiting.    ← заглавная буква + точка
```

---

## Установка хука

Git hook автоматически проверяет формат при каждом `git commit`:

```bash
make setup
```

Или вручную:
```bash
git config core.hooksPath .githooks
```

После этого неверно оформленный коммит будет отклонён с подсказкой:

```
  ✗ Неверный формат commit message.

  Ожидается: <type>(<scope>): <описание>

  Типы:   feat fix docs style refactor perf test chore ci build revert
  Скоупы: api worker frontend docker docs deps auth queue player subtitles ffmpeg db config

  Примеры:
    feat(api): add rate limiting middleware
    fix(worker): handle ffmpeg timeout correctly
```

---

## Автоматическая генерация CHANGELOG

При использовании Conventional Commits история коммитов становится машиночитаемой.
В будущем можно подключить `git-cliff` для автоматической генерации `CHANGELOG.md`:

```bash
# Установка
brew install git-cliff

# Генерация
git cliff --output CHANGELOG.md
```

---

## Связь с версионированием

Conventional Commits определяют тип следующей версии (SemVer):

| Коммиты | Версия |
|---|---|
| только `fix` | patch: `0.1.0` → `0.1.1` |
| хотя бы один `feat` | minor: `0.1.0` → `0.2.0` |
| `BREAKING CHANGE` в любом | major: `0.1.0` → `1.0.0` |
