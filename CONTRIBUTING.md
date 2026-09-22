# Руководство разработчика

Документ описывает локальную разработку Goosar: первую настройку, ежедневную работу в основной копии и в worktree, общую модель PostgreSQL, тестирование, полностековую проверку с демоном, диагностику и сброс окружения.

Правила кода, архитектуры и именования — в `apps/docs/content/docs/developers/conventions.mdx`. Полный набор команд — в `Makefile`, `package.json` и `scripts/`.

## Модель разработки

Локальная разработка использует один общий контейнер PostgreSQL и отдельную базу данных на каждую копию репозитория.

- Основная копия использует `.env` и `POSTGRES_DB=goosar`.
- Каждый Git worktree использует свой `.env.worktree`.
- Все копии подключаются к одному хосту PostgreSQL: `localhost:5433` (контейнер публикует `127.0.0.1:5433:5432`).
- Изоляция происходит на уровне базы данных: отдельный проект Docker Compose на копию не запускается.
- Порты бэкенда и фронтенда уникальны для каждого worktree.

## Требования

- Node.js 20+
- pnpm 10.28+
- Go 1.26+
- Docker (для самостоятельного развёртывания нужен плагин `docker compose`; устаревший `docker-compose` v1 не поддерживается)

## Правила окружения

- Основная копия использует `.env`, worktree — `.env.worktree`.
- Не копируйте `.env` в каталог worktree.

Причина: `Makefile` предпочитает `.env` файлу `.env.worktree` (порядок задан переменной `ENV_FILE`). Если в worktree окажется `.env`, он может указывать на базу основной копии.

## Файлы окружения

### Основная копия

```bash
cp .env.example .env
```

Значения по умолчанию:

```bash
POSTGRES_DB=goosar
POSTGRES_PORT=5433
DATABASE_URL=postgres://goosar:goosar@localhost:5433/goosar?sslmode=disable
PORT=8081
FRONTEND_PORT=3001
```

### Worktree

Из каталога worktree:

```bash
make worktree-env
```

Команда создаёт `.env.worktree` со значениями вида:

```bash
POSTGRES_DB=goosar_my_feature_702
POSTGRES_PORT=5433
PORT=18782
FRONTEND_PORT=13702
DATABASE_URL=postgres://goosar:goosar@localhost:5433/goosar_my_feature_702?sslmode=disable
```

Правила формирования (`scripts/init-worktree-env.sh`):

- `POSTGRES_DB` имеет вид `goosar_<slug>_<offset>`: `slug` — имя каталога, приведённое к строчным буквам и `_`; `offset` — `cksum` пути каталога по модулю 1000.
- `POSTGRES_PORT` всегда `5433`; порт бэкенда — `18080 + offset`, порт фронтенда — `13000 + offset`.
- В файл также записываются `GOOSAR_DEV_VERIFICATION_CODE=888888`, `GOOSAR_SERVER_URL`, `GOOSAR_APP_URL`, `FRONTEND_ORIGIN`, `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_WS_URL`.
- Существующий `.env.worktree` команда не перезаписывает. Пересоздать файл: `FORCE=1 make worktree-env`.

## Первая настройка

Быстрый путь, из любой копии (основной или worktree):

```bash
make dev
```

Команда (`scripts/dev.sh`):

- определяет тип копии (в worktree `.git` — файл, а не каталог);
- создаёт `.env` из `.env.example` или `.env.worktree` через `scripts/init-worktree-env.sh`, если файла нет;
- проверяет наличие Node.js, pnpm, Go и Docker;
- устанавливает зависимости, если нет `node_modules`;
- запускает общий контейнер PostgreSQL и создаёт базу, если её нет;
- применяет миграции и запускает бэкенд и фронтенд.

Раздельная настройка и запуск:

```bash
# основная копия
cp .env.example .env
make setup-main && make start-main     # остановка: make stop-main

# worktree
make worktree-env
make setup-worktree && make start-worktree   # остановка: make stop-worktree
```

## Ежедневная работа

Основная копия — стабильное окружение для `main`: `make start-main`, `make stop-main`, `make check-main`.

Worktree — для изолированных данных и отдельных портов:

```bash
git worktree add ../goosar-feature -b feat/my-change main
cd ../goosar-feature
make dev              # запуск (повторно выполняет настройку, идемпотентно)
make stop-worktree    # остановка
make check-worktree   # проверка
```

Вернуться в ранее настроенный worktree: `make start-worktree`.

Основная копия и worktree могут работать одновременно. Пример: основная — база `goosar`, бэкенд `8081`, фронтенд `3001`; worktree — база `goosar_my_feature_702`, бэкенд `18782`, фронтенд `13702`. Контейнер PostgreSQL и порт `5433` общие, данные приложения разные.

## Справочник команд make

`make` без аргументов и `make help` выводят список целей.

| Цель | Действие |
| --- | --- |
| `make db-up` / `make db-down` | Запустить или остановить общий контейнер PostgreSQL (том и базы сохраняются) |
| `make setup-main`, `start-main`, `stop-main`, `check-main` | Основная копия (`.env`) |
| `make worktree-env`, `setup-worktree`, `start-worktree`, `stop-worktree`, `check-worktree` | Worktree (`.env.worktree`) |
| `make setup` | Установить зависимости, создать БД, применить миграции |
| `make start` | Применить миграции, запустить бэкенд и фронтенд |
| `make stop` | Остановить процессы бэкенда и фронтенда (PostgreSQL продолжает работать) |
| `make dev` | Настроить копию целиком и запустить сервисы |
| `make server` | Запустить только Go-сервер |
| `make check` | Форматирование, typecheck, тесты TS, тесты Go, E2E |
| `make e2e` | Только E2E Playwright (`scripts/test-e2e.sh`) |
| `make test` | Тесты Go (создаёт БД, применяет миграции, `scripts/test-go.sh --race`) |
| `make migrate-up` / `make migrate-down` | Применить или откатить миграции |
| `make sqlc` | Перегенерировать код sqlc после изменения SQL |
| `make build` | Собрать `server`, `goosar`, `migrate` в `server/bin` |
| `make fmt` | `fmt-go` (gofumpt, gci) и `fmt-ts` (Prettier) |
| `make clean` | Удалить кэши сборки, бинарники и временные файлы |
| `make db-reset` | Пересоздать БД текущего окружения (см. «Деструктивный сброс») |
| `make cli ARGS="..."` | Запустить CLI `goosar` из исходников |
| `make daemon` | Перезапустить локальный демон (`daemon restart --profile local`) |

Общие цели (`setup`, `start`, `stop`, `check`, `dev`, `test`, `migrate-*`) требуют файл окружения в текущем каталоге. Цели самостоятельного развёртывания (`selfhost`, `selfhost-build`, `selfhost-stop`, `selfhost-code`, `selfhost-admin`, `selfhost-packages`, `offline-*`) описаны в `SELF_HOSTING.md`. Локальный выпуск релиза: `make release-local TAG=vX.Y.Z [ARGS='--skip-tests']`.

После изменения SQL выполните `make sqlc`, после изменения зарезервированных слагов — `pnpm generate:reserved-slugs`.

## Как создаётся база данных

Создание автоматическое. Цели `setup`, `start`, `dev`, `server`, `test`, `check`, `migrate-up`, `migrate-down` перед работой проверяют, что база существует. Логика находится в `scripts/ensure-postgres.sh`:

- если `DATABASE_URL` указывает на `localhost`, `127.0.0.1` или `::1` (или пуст), скрипт запускает контейнер через `docker compose up -d postgres`, ждёт готовности и создаёт базу `POSTGRES_DB`, если её нет;
- если хост удалённый, Docker не используется, скрипт лишь ждёт доступности сервера (при наличии `pg_isready`).

## Тестирование

Полная локальная проверка:

```bash
make check-main        # основная копия
make check-worktree    # worktree
```

`scripts/check.sh` последовательно выполняет:

1. Проверку форматирования: gofumpt и gci (`scripts/check-go-format.sh`), Prettier (`pnpm format:check`).
2. Проверку типов (`pnpm typecheck`).
3. Модульные тесты TypeScript (`pnpm test`).
4. Проверку обёртки Go-тестов и скриптов установки, миграции, затем тесты Go (`scripts/test-go.sh`).
5. E2E Playwright (`scripts/test-e2e.sh`).

Во время работы запускайте узкие проверки: `pnpm typecheck`, `pnpm test`, `make test`, `pnpm exec playwright test`.

### Тестовая база данных

Тесты Go идут не в рабочую базу. `scripts/test-go.sh`:

- выставляет `GOOSAR_REQUIRE_TEST_DB=1`: недоступность PostgreSQL считается ошибкой, а не пропуском тестов;
- сбрасывает переменные почтовых транспортов (`RESEND_API_KEY`, `RESEND_FROM_EMAIL`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`, `SMTP_TLS_INSECURE`, `SMTP_EHLO_NAME`, `SMTP_FROM_EMAIL`), чтобы тесты не обращались к реальным почтовым сервисам;
- вычисляет отдельную базу `<имя>_test` из `DATABASE_URL` (если имя уже оканчивается на `_test`, оставляет как есть), создаёт её (`go run ./cmd/ensure_test_db`) и применяет миграции;
- запускает тесты через `scripts/go-test-with-agent-cli-guard.sh`; пакеты `pkg/agent` идут отдельным прогоном с `-p 2 -parallel 2`.

Отдельная база нужна, потому что запущенный локально стек опрашивает ту же базу, что указана в `.env`, и его фоновые воркеры могли бы захватывать тестовые записи.

Если тестовая база оказалась в неисправном состоянии, удалите её и запустите тесты заново (имя — `POSTGRES_DB` с суффиксом `_test`):

```bash
docker compose exec -T postgres psql -U goosar -d postgres -v ON_ERROR_STOP=1 \
  -c 'DROP DATABASE IF EXISTS "goosar_test" WITH (FORCE);'
```

Тесты по умолчанию не запускают установленные у пользователя CLI агентов. Новую команду агента по умолчанию добавляйте в `scripts/agent-cli-command-names.txt`.

### Окружение release gate

`scripts/release-local.sh` запускает тот же набор Playwright как барьер перед выпуском, но из worktree тега, а не из вашей копии. Из копии передаётся только файл окружения, поэтому в нём должно быть всё нужное (`bash scripts/release-local.sh --help` выводит тот же список):

| Переменная | Значение |
| --- | --- |
| `DATABASE_URL` | **одноразовая** база: набор тестов очищает её (`TRUNCATE`) |
| `BACKEND_PORT` | порт бэкенда (также читаются `PORT`, `API_PORT`, `SERVER_PORT`) |
| `FRONTEND_PORT` | порт `next dev` |
| `FRONTEND_ORIGIN` | `http://localhost:$FRONTEND_PORT` |
| `GOOSAR_PUBLIC_URL` | тот же origin, для ссылок, которые формирует бэкенд |

Запускайте выпуск из оболочки с `umask 022`, чтобы хранилище pnpm и `.next` оставались читаемыми. Оба порта барьера должны быть свободны: оставшийся стек скрипт обнаруживает заранее и сообщает PID владельца.

`scripts/test-e2e.sh` сам задаёт `TURBO_ENV_MODE=loose` и `REMOTE_API_URL=http://localhost:$PORT`: строгий режим turbo отбрасывает `REMOTE_API_URL` (его нет в `globalEnv` файла `turbo.json`), и прокси Next обращается к адресу, зашитому при сборке, то есть тестирует другой бэкенд.

Переменные `NEXT_PUBLIC_API_URL` и `NEXT_PUBLIC_WS_URL` скрипт, наоборот, **снимает** для запускаемых серверов: они направляют браузер прямо на бэкенд в обход origin фронтенда, и тест реального времени падает на междоменном WebSocket. Браузер должен оставаться на одном origin и обращаться к API через прокси Next. Предупреждения недостаточно, потому что `.env.worktree` и `Makefile` задают эти переменные по умолчанию. Фикстуры Playwright не затронуты: они перечитывают файл окружения, а без него используют `http://localhost:$PORT`.

Коды выхода `scripts/test-e2e.sh`: `0` — тесты прошли, `1` — тесты упали, `2` — не удалась подготовка (браузер, база, сервер не стал здоровым).

## Полностековая изолированная проверка

Сценарий запускает бэкенд, фронтенд и демон из исходников в изолированном окружении. Он нужен для сквозных изменений, затрагивающих несколько компонентов, и для автоматизации без участия человека.

`make daemon` для этого не подходит: он использует токен установленного в системе CLI и сервер из `~/.goosar/config.json`. Для изоляции нужны локальные бэкенд и фронтенд, локальный демон со своим профилем, аутентификация без браузера и отсутствие влияния на рабочую конфигурацию CLI.

### Имя профиля

Каждому worktree нужен собственный профиль демона. Имя выводится из каталога по той же схеме `slug` + `offset`, что и в `scripts/init-worktree-env.sh`:

```bash
WORKTREE_DIR="$(basename "$PWD")"
SLUG="$(printf '%s' "$WORKTREE_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
HASH="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
OFFSET=$((HASH % 1000))
PROFILE="dev-${SLUG}-${OFFSET}"
```

Например, worktree `../goosar-feat-auth` даёт профиль `dev-goosar_feat_auth-347`, согласованный с портами и базой этого worktree. Переменные `PROFILE` и `SERVER` ниже предполагают, что шаги выполняются в одной оболочке из корня worktree.

### Запуск

**1. Бэкенд, фронтенд и база.**

```bash
make dev
```

Дождитесь готовности бэкенда:

```bash
PORT=$( (grep -h '^PORT=' .env.worktree 2>/dev/null || grep -h '^PORT=' .env) | head -1 | cut -d= -f2)
SERVER="http://localhost:${PORT:-8080}"

for i in $(seq 1 30); do
  curl -sf "$SERVER/health" > /dev/null 2>&1 && break
  sleep 2
done
```

**2. Тестовый пользователь и токен.** Для детерминированной автоматизации `GOOSAR_DEV_VERIFICATION_CODE=888888` должен быть задан в файле окружения до запуска бэкенда (в `.env.worktree` он уже есть). Код действует только при `APP_ENV`, отличном от `production`.

```bash
curl -s -X POST "$SERVER/auth/send-code" \
  -H "Content-Type: application/json" \
  -d '{"email": "dev@localhost"}'

JWT=$(curl -s -X POST "$SERVER/auth/verify-code" \
  -H "Content-Type: application/json" \
  -d '{"email": "dev@localhost", "code": "888888"}' | jq -r '.token')

PAT=$(curl -s -X POST "$SERVER/api/tokens" \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"name": "auto-dev", "expires_in_days": 365}' | jq -r '.token')
```

**3. Рабочее пространство.**

```bash
WS=$(curl -s -X POST "$SERVER/api/workspaces" \
  -H "Authorization: Bearer $PAT" \
  -H "Content-Type: application/json" \
  -d '{"name": "Dev", "slug": "dev"}' | jq -r '.id')
```

**4. Конфигурация CLI для профиля** (`PROFILE` вычислен выше).

```bash
FRONTEND_PORT=$( (grep -h '^FRONTEND_PORT=' .env.worktree 2>/dev/null || grep -h '^FRONTEND_PORT=' .env) | head -1 | cut -d= -f2)
CONFIG_DIR="$HOME/.goosar/profiles/$PROFILE"
mkdir -p "$CONFIG_DIR"

cat > "$CONFIG_DIR/config.json" << EOF
{
  "server_url": "$SERVER",
  "app_url": "http://localhost:${FRONTEND_PORT:-3000}",
  "token": "$PAT",
  "workspace_id": "$WS",
  "watched_workspaces": [{"id": "$WS", "name": "Dev"}]
}
EOF
```

**5. Демон из исходников.**

```bash
make cli ARGS="daemon start --profile $PROFILE"
```

Демон работает из Go-кода текущего worktree и подключается к локальному бэкенду. Команды `goosar`, которые выполняют агенты, используют тот же бинарник: демон добавляет свой каталог в начало `PATH`.

### Остановка

```bash
make cli ARGS="daemon stop --profile $PROFILE"    # 1. демон
make stop                                         # 2. бэкенд и фронтенд (основная копия)
make stop-worktree                                #    то же для worktree
make db-down                                      # 3. (необязательно) общий PostgreSQL
make clean                                        # 4. (необязательно) артефакты сборки
rm -rf "$HOME/.goosar/profiles/$PROFILE"          # 5. (необязательно) конфигурация профиля
```

### Десктопное приложение с локальным бэкендом

```bash
pnpm dev:desktop    # бэкенд уже запущен (make dev)
```

Команда автоматически собирает CLI `goosar` из `server/cmd/goosar` в `apps/desktop/resources/bin/goosar`, создаёт изолированный профиль `desktop-localhost-<PORT>`, запускает собственный экземпляр демона и подключается к локальному бэкенду.

Войдите в десктопном интерфейсе как `dev@localhost`; код возьмите из журнала бэкенда либо используйте `888888`, если `GOOSAR_DEV_VERIFICATION_CODE=888888` задан до запуска бэкенда.

Если бэкенд работает на нестандартном порту (worktree), создайте `apps/desktop/.env.development.local`:

```bash
VITE_API_URL=http://localhost:<порт-бэкенда>
VITE_WS_URL=ws://localhost:<порт-бэкенда>/ws
```

Несколько worktree одновременно `pnpm dev:desktop` изолирует автоматически. Из связанного worktree он выводит по пути (тот же `cksum % 1000`, что и для портов в `.env.worktree`):

- `DESKTOP_RENDERER_PORT` = `5174 + offset` — собственный dev-сервер Vite (базовое значение `5174` оставляет `5173` основной копии даже при `offset` равном 0);
- `DESKTOP_APP_SUFFIX` = `<папка>-<offset>` — собственная блокировка единственного экземпляра и `userData`; приложение называется `Goosar Canary <папка>-<offset>`.

Основная копия не затрагивается (`5173`, `Goosar Canary`). Любую из переменных можно задать явно. К какому бэкенду обращается каждый экземпляр, определяют только файлы `apps/desktop/.env*`: чтобы изолировать и профиль демона, направьте десктоп каждого worktree на его бэкенд.

### Что изолировано

Сценарий не затрагивает установленный в системе `goosar` и файл `~/.goosar/config.json`:

| Ресурс | Системный | Локальный (на worktree) |
| --- | --- | --- |
| Конфигурация | `~/.goosar/config.json` | `~/.goosar/profiles/dev-<slug>-<hash>/config.json` |
| PID демона | `~/.goosar/daemon.pid` | `~/.goosar/profiles/dev-<slug>-<hash>/daemon.pid` |
| Порт health | `19514` | `19514 + 1 + (name_hash % 1000)` |
| Каталог рабочих областей | `~/goosar_workspaces/` | `~/goosar_workspaces_dev-<slug>-<hash>/` |
| База данных | удалённая | локальный Docker: `goosar_<slug>_<hash>` |
| Профиль десктопа | `desktop-<хост>` | `desktop-localhost-<порт>` |

## Диагностика

**Нет файла окружения.** Сообщения `Missing env file: .env` и `Missing env file: .env.worktree` означают, что файла нет. Основная копия: `cp .env.example .env`. Worktree: `make worktree-env`.

**Какая база используется.** Посмотрите `cat .env` или `cat .env.worktree`: важны `POSTGRES_DB`, `DATABASE_URL`, `PORT`, `FRONTEND_PORT`.

**Список всех локальных баз:**

```bash
docker compose exec -T postgres psql -U goosar -d postgres -At -c "select datname from pg_database order by datname;"
```

**Worktree работает с базой основной копии.** Проверьте, нет ли в каталоге worktree файла `.env`; его быть не должно. Безопасная последовательность: `make worktree-env`, `make setup-worktree`, `make start-worktree`.

**PostgreSQL продолжает работать после остановки.** Так и задумано: `make stop`, `make stop-main`, `make stop-worktree` останавливают только бэкенд и фронтенд. Остановить общий контейнер: `make db-down`.

## Деструктивный сброс

Остановить PostgreSQL, сохранив базы: `make db-down`.

Получить чистую базу только для текущей копии (удаляет базу `POSTGRES_DB`, создаёт заново и применяет все миграции):

```bash
make stop        # сначала остановите бэкенд и фронтенд
make db-reset
make start
```

- Затрагивается только база текущего окружения; базы других worktree остаются нетронутыми.
- Цель отказывается работать, если `DATABASE_URL` указывает на удалённый хост.
- Чтобы выбрать конкретный worktree, передайте `ENV_FILE=.env.worktree`.

Стереть все локальные данные PostgreSQL этого репозитория:

```bash
docker compose down -v
```

Предупреждение: удаляется общий том Docker, то есть база основной копии и базы всех worktree. После этого нужно заново выполнить `make setup-main` или `make setup-worktree`.
