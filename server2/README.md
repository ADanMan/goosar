# server2

Clean-room переписанный бэкенд Goosar (эпик E8, ветка `backend/clean-room`).
Схема БД спроектирована заново от `docs/50-api-contract.yaml` — см.
`docs/51-data-model.md`. Миграции лежат в `server2/migrations`
(`NNN_domain.{up,down}.sql`, PostgreSQL 16).

## Сборка и тесты

```sh
cd server2
go vet ./...
go build ./...
go test ./...
```

Модуль: `github.com/adanman/goosar/server2`, `go 1.24`. Postgres-драйвер —
`github.com/jackc/pgx/v5`.

## Сервер (`cmd/server`, T-026)

`server2/cmd/server` — HTTP-сервер с тем же внешним контрактом, что и
`server/cmd/server` (`docs/50-api-contract.{md,yaml}`), реализованный
clean-room (см. `server2/docs/adr/0001-stack.md` за выбор стека и
`server2/docs/decisions.md` за решения по пробелам спецификации).

### Запуск

```sh
createdb goosar2_dev   # или: psql -c 'CREATE DATABASE goosar2_dev'

cd server2
DATABASE_URL="postgres://postgres@localhost:5432/goosar2_dev?sslmode=disable" \
JWT_SECRET="dev-secret" \
ALLOW_SIGNUP=true \
GOOSAR_DEV_VERIFICATION_CODE=424242 \
FRONTEND_ORIGIN="http://localhost:3199" \
PORT=8080 \
  go run ./cmd/server -migrate
```

`-migrate` (или `MIGRATE=true`) применяет `server2/migrations` перед стартом;
без него сервер ожидает уже мигрированную БД (тот же раннер, что и
`cmd/import --migrate`, — `internal/migrate`).

### Переменные окружения

Имена, форматы и умолчания совпадают с «Приложение. Переменные окружения
сервера» в `docs/50-api-contract.md` (161 переменная) и с
`e2e/contract/README.md` — это тот же контракт, поэтому те же имена.
Построчная сверка «поддержана / частично / прочитана без эффекта» для всех
161 — `server2/docs/env-parity.md`; таблица ниже — только сводка с акцентом
на то, что реально нужно оператору для запуска и что изменилось в этой
сессии (T-026 доводка).

| Переменная | Обязательна | Смысл |
|---|---|---|
| `DATABASE_URL` | да | DSN Postgres |
| `DATABASE_MAX_CONNS`/`DATABASE_MIN_CONNS` | нет (`25`/`5`, либо `pool_max_conns`/`pool_min_conns` из URL) | верхняя/нижняя граница пула `pgxpool` |
| `PORT` | нет (`8080`) | порт HTTP-сервера |
| `APP_ENV` | нет (`development`) | `production` отключает `GOOSAR_DEV_VERIFICATION_CODE`, требует небезопасный `JWT_SECRET` заменить (иначе отказ старта) |
| `GOOSAR_REPLICAS` | нет (`1`) | `>1` без `REDIS_URL` — отказ старта (Redis-бэкенд лимитера/realtime не реализован в этой версии) |
| `GOOSAR_SHUTDOWN_HOLD_DURATION` | нет | пауза перед graceful shutdown |
| `GOOSAR_MIGRATION_LOCK_TIMEOUT`/`_RETRIES`/`_STATEMENT_TIMEOUT` | нет (`5s`/`5`/без таймаута) | Postgres advisory lock вокруг применения миграций (`-migrate`/`MIGRATE=true`), несколько реплик не гоняются за одни и те же файлы |
| `JWT_SECRET` | фактически да на `production` (иначе небезопасный dev-дефолт) | ключ HMAC для сессионных JWT; пустое/плейсхолдер на `production` — отказ старта |
| `JWT_SECRET_PREVIOUS` | нет | секреты, ещё принимаемые для проверки подписи (окно ротации) |
| `COOKIE_DOMAIN` | нет | атрибут `Domain` сессионной куки; IP молча игнорируется |
| `AUTH_TOKEN_TTL` | нет (`2592000` = 30д) | срок жизни токена сессии/куки |
| `FRONTEND_ORIGIN` | нет (`http://localhost:3199`) | origin фронтенда для разработки; часть цепочки CORS |
| `ALLOWED_ORIGINS`/`CORS_ALLOWED_ORIGINS` | нет | allow-list CORS/WebSocket (приоритет: `ALLOWED_ORIGINS` → `CORS_ALLOWED_ORIGINS` → три локальных дефолта); `FRONTEND_ORIGIN` добавляется в набор всегда |
| `GOOSAR_APP_URL` | нет (= `FRONTEND_ORIGIN`) | публичный адрес приложения — используется в magic-link писем входа |
| `GOOSAR_PUBLIC_URL` | нет | базовый публичный адрес сервера (T-028: `webhook_url` автопилотов, OIDC redirect); без него строится из заголовков запроса |
| `GOOSAR_TRUSTED_PROXIES`/`RATE_LIMIT_TRUSTED_PROXIES` | нет | CIDR обратных прокси, которым доверяют X-Forwarded-For/X-Real-IP (общий резолвер `httpapi.ClientIP`; `RATE_LIMIT_TRUSTED_PROXIES`, если задан, имеет приоритет) |
| `ALLOW_SIGNUP` | нет (`true`) | создавать ли аккаунт при первом входе по коду |
| `ALLOWED_EMAILS`/`ALLOWED_EMAIL_DOMAINS` | нет | allow-list для входа/регистрации (email-код, OIDC, LDAP) |
| `DISABLE_WORKSPACE_CREATION` | нет | запрещает создание новых рабочих пространств |
| `GOOSAR_DEV_VERIFICATION_CODE` | нет | фиксированный 6-значный код входа вне production |
| `GOOSAR_ROLE_WORKSPACES` | нет (`auto`) | `auto`/`off`, иное — отказ старта; `auto` провижинит ролевые воркспейсы при каждом старте сервера (нужен хотя бы один deployment-admin) |
| `GOOSAR_DEPLOYMENT_ADMIN_EMAILS` | нет | сидирует роль администратора деплоя существующим пользователям, пока таблица администраторов пуста |
| `GOOSAR_MCP_SECRET_KEY`(`_PREVIOUS`) | нет | шифрует чувствительную часть `mcp_config`; наличие также включает `MFAStatusResponse.available` |
| `REALTIME_METRICS_TOKEN` | нет | Bearer-токен для `/health/realtime` не с loopback |
| `MIGRATE` | нет (`false`) | применить миграции при старте (то же, что флаг `-migrate`) |
| `MIGRATIONS_DIR` | нет (`server2/migrations`) | откуда брать `NNN_*.up.sql` |
| `E2E_COMPAT_SQL_DIR` | нет | см. «e2e фронтенда» ниже — не для обычного запуска |
| `LOG_FORMAT`/`GOOSAR_LOG_FORMAT` | нет (`text`) | `json`/`text`; `GOOSAR_`-версия побеждает при обеих заданных |
| `LOG_LEVEL`/`GOOSAR_LOG_LEVEL` | нет | `debug`/`info`/`warn`/`error`; дефолт `info` на production, иначе `debug` |
| `GOOSAR_CLOUD_FLEET_URL`(алиас `GOOSAR_FLEET_URL`)/`_TIMEOUT` | нет | адрес облачного fleet-сервиса (T-028, `internal/cloudruntime`); пусто — вся группа `/api/cloud-runtime/**` отвечает `503` |
| `GOOSAR_CLOUDRUNTIME_API_KEY` | нет | `Authorization: Bearer` к fleet-сервису — переменная без аналога в приложении (см. `server2/docs/env-parity.md`) |
| `GITHUB_WEBHOOK_SECRET`/`GITHUB_APP_SLUG`/`GITHUB_APP_ID`/`GITHUB_APP_PRIVATE_KEY` | нет | GitHub App (T-029, `internal/integration`); без `GITHUB_WEBHOOK_SECRET` — `503` на `/api/webhooks/github`, без `GITHUB_APP_SLUG` — `configured:false` на `github/connect` |
| `GOOSAR_VCS_INTEGRATION_ENABLED`(`true`)/`GOOSAR_VCS_SECRET_KEY`(`_PREVIOUS`) | нет | self-hosted VCS (T-029); без ключа шифрования — `503` на `vcs/connections` |
| `COMPOSIO_API_KEY`/`COMPOSIO_STATE_SECRET`/`COMPOSIO_CALLBACK_BASE_URL` | нет | Composio (T-029); без ключа — `503` на всю группу |
| `GOOSAR_SLACK_SECRET_KEY`(`_PREVIOUS`) | нет | Slack BYO-установка (T-029); без ключа — `configured:false` |
| `GOOSAR_GITHUB_API_BASE_URL`/`GOOSAR_SLACK_API_BASE_URL`/`GOOSAR_COMPOSIO_API_BASE_URL` | нет | базовые URL внешних API интеграций — переопределяются в юнит-тестах (`httptest.Server`), в проде пусто = реальный хост; без аналога в приложении |
| `RATE_LIMIT_*` (`AUTH`/`AUTH_VERIFY`/`AUTH_EMAIL`/`MFA_VERIFY`/`TOKEN`/`API`/`CONTACT_SALES`/`EXPORT`/`JOIN`) | нет | лимиты contract §1.5, умолчания и группировка по ручкам — таблица T-029 в `server2/docs/decisions.md` |
| `RESEND_API_KEY`/`RESEND_FROM_EMAIL`(`noreply@goosar.ru`) | нет | транспорт Resend |
| `SMTP_HOST`(приоритетнее Resend)/`SMTP_PORT`(`25`)/`SMTP_USERNAME`/`SMTP_PASSWORD`/`SMTP_FROM_EMAIL`/`SMTP_TLS`(`starttls`\|`implicit`, алиасы `smtps`/`ssl`)/`SMTP_TLS_INSECURE`/`SMTP_EHLO_NAME` | нет | транспорт SMTP; без обоих (Resend и SMTP) — код входа только в лог, кроме `production`, где вход отказывает `503` |
| `GOOSAR_TOTP_ISSUER` | нет (`Goosar`) | издатель в `otpauth://` URI и приложениях-аутентификаторах (MFA) |
| `GOOSAR_AUTH_METHODS` | нет (`email`) | какие методы входа доступны на уровне сервера (не только в интерфейсе) |
| `GOOSAR_OIDC_ISSUER`/`_CLIENT_ID`/`_CLIENT_SECRET`/`_REDIRECT_URL`/`_SCOPES`/`_DISPLAY_NAME`/`_ADMIN_CLAIM`/`_ADMIN_VALUE`/`_TRUST_UNVERIFIED_EMAIL` | нет | корпоративный OIDC; без `GOOSAR_OIDC_ISSUER` — `404` на `/api/auth/oidc/**`, "oidc" не входит в `AuthMethodsResponse.methods`; `_ADMIN_CLAIM`/`_ADMIN_VALUE` читаются, но заявка на роль администратора по совпадению claim не подаётся автоматически (см. env-parity.md) |
| `GOOSAR_LDAP_URL`/`_START_TLS`/`_BIND_DN`/`_BIND_PASSWORD`/`_BASE_DN`/`_USER_FILTER`(`(uid=%s)`)/`_EMAIL_ATTR`(`mail`)/`_NAME_ATTR`(`displayName`)/`_ADMIN_GROUP`/`_DISPLAY_NAME` | нет | корпоративный LDAP/AD; без `GOOSAR_LDAP_URL` или без TLS (`ldaps://` либо `ldap://` + `_START_TLS=true`) — метод не предлагается; `_ADMIN_GROUP` читается без автоматической заявки, как и OIDC-аналог; `_USER_FILTER` поддерживает только одиночный equality-фильтр `(attr=%s)` |
| `GOOSAR_EXTERNAL_IMAGES`(`allow`)/`GOOSAR_IMAGE_HOSTS` | нет | политика внешних изображений — стамплится в заголовок `Content-Security-Policy` каждого ответа API |
| `GOOSAR_DELIVERY_PROFILE`(`cloud`)/`GOOSAR_DEPLOYMENT_PROFILE`(`perimeter`) | нет | валидируются при старте, отражаются в `GET /api/status`/`/api/config` |
| `GOOSAR_SKILL_SOURCES`/`GOOSAR_MCP_ALLOWED_HOSTS`/`GOOSAR_MCP_ALLOWED_COMMANDS`/`GOOSAR_ALLOWED_PROVIDERS` | нет | политики перимитра — только частично реализованы (валидация/отражение в `/api/config`, без enforcement на fetch/mcp_config/dispatch — см. env-parity.md) |
| `GOOSAR_AUDIT_RETENTION_DAYS` | нет (`365`) | суточный фоновый цикл очистки `platform_audit_log` (0 = хранить вечно) |
| `GOOSAR_DEPLOYMENT_JIRA_URL`/`_CONFLUENCE_URL`/`_EWS_URL`/`_MAIL_DOMAIN`/`_BITRIX24_URL`/`_MCP_GATEWAY_URL` | нет | подсказки клиентам через `/api/config`; `_BITRIX24_URL`/`_MCP_GATEWAY_URL` также участвуют в `mcp-library seed` |
| `GOOSAR_LLM_API_KEY`/`_BASE_URL`/`_DEFAULT_MODEL` | нет | реальные креды для `GET /api/llm/health` и `GET /api/deployment/client-secrets`; общий внутренний LLM-хелпер (например для заголовков чата) не реализован |
| `GOOSAR_DEPLOYMENT_LLM_API_BASE`/`_MODEL` | нет | только подсказки клиентам, не креды (креды — `GOOSAR_LLM_*` выше) |
| `S3_BUCKET`/`S3_REGION`/`AWS_*`/`CLOUDFRONT_*` | нет | читаются, backend вложений — только локальный диск (`LOCAL_UPLOAD_DIR`), S3/CloudFront не реализованы |
| `GOOSAR_PROVISIONING_STORE`(`local`\|`oci`)/`_LOCAL_PREFIX`/`_OCI_*` | нет | каталог пакетов деплоя; только `local` работает (`LOCAL_UPLOAD_DIR/GOOSAR_PROVISIONING_LOCAL_PREFIX`) |
| `GOOSAR_RETENTION_CHAT`/`_TASKS`/`_CLOSED_ISSUES`/`_ACTIVITY`/`GOOSAR_ATTACHMENT_PURGE_GRACE`/`GOOSAR_UPLOAD_GC_GRACE` | нет | дефолты флагов `goosar_admin purge`/`gc-uploads`; без периодического автозапуска в `cmd/server` |
| `GOOSAR_EXPORT_TIMEOUT`(`2h`)/`GOOSAR_EXPORT_RETENTION`(`168h`) | нет | таймаут/срок хранения задания экспорта воркспейса |
| `METRICS_ADDR`/`POSTHOG_*`/`ANALYTICS_*`/`GOOSAR_SCHEDULER_AUDIT_RETENTION`/`GOOSAR_HYGIENE_SWEEP_INTERVAL`/`GOOSAR_EXPORT_DIR`/`GOOSAR_EXPORT_MAX_BYTES`/`REDIS_*`/`REALTIME_RELAY_*` | нет | читаются, поведение не реализовано в этой версии — см. `server2/docs/env-parity.md` |

### Раскладка (`internal/`)

- `config` — чтение переменных окружения выше.
- `store` — пул pgx/v5, транзакции, общие SQL-хелперы (`WithTx`, `RowExists`, ...).
- `httpapi` — маршрутизатор поверх `net/http.ServeMux` (Go 1.22+ шаблоны
  путей), формат JSON-ответов/ошибок контракта, `httpapi.Actor` в контексте
  запроса, резолв воркспейса по `X-Workspace-Slug`/`X-Workspace-ID`, роли,
  rate limit (скользящее окно, `Limiter`/`RateLimiter`, T-029).
- `authn` — коды входа по email, magic-link, OIDC, LDAP (T-029, свои
  discovery+JWKS+BER-клиент — см. `docs/adr/0002-auth-providers.md`),
  сессии, cookie+CSRF, JWT, PAT, daemon-токены (заготовка), MFA TOTP
  (enroll/confirm/disable/recovery-codes, вход вторым фактором — T-029).
- `mail` — интерфейс отправки писем, dev-логгер, Resend/SMTP (T-029).
- `realtime` — хаб `/ws`, интерфейс `Publisher` для доменов.
- `identity` — `/api/me`, онбординг, `/api/cli-token`, `/api/tokens`.
- `workspace` — `/api/workspaces/**`, `/api/invitations/**`, runtime-profiles.
- `contractspec` — разбор `docs/50-api-contract.yaml` (операции контракта).
- `app` — сборка всех доменных `Deps`, регистрация маршрутов (`routes.go`),
  health/readiness/`/api/config`, генерируемые заглушки (`stubs_gen.go`).
- `migrate`, `importer` — без изменений, T-024/T-025.
- `wsctx` — резолв воркспейса из `X-Workspace-Slug`/`X-Workspace-ID` +
  проверка членства, для доменов T-027+, у которых воркспейс не в пути.
- `note` — комментарии задач и их реакции/резолюция/треды
  (`/api/issues/{id}/comments/**`, `/api/comments/{commentId}/**`,
  `/api/issues/{id}/reactions`), включая определение целей автозапуска
  агентов по правилам контракта (§1.9/§1.10).
- `tagging` — метки и кастомные свойства задач (`/api/labels/**`,
  `/api/properties/**`, `/api/issues/{id}/labels`,
  `/api/issues/{id}/properties/{propertyId}`).
- `asset` — вложения (`/api/upload-file`, `/api/attachments/**`,
  `/api/issues/{id}/attachments`, `GET /uploads/{key}` при локальном
  хранилище); интерфейс `Storage` с реализациями на диск и заглушкой S3.
- `pin` — личные закладки на задачи/проекты (`/api/pins/**`).
- `autopilot` — автопилоты (`/api/autopilots/**`): CRUD, триггеры
  (расписание/вебхук/api), ручной запуск, прогоны (runs), вебхук-доставки
  (deliveries), коллабораторы; публичный `POST /api/webhooks/autopilots/{token}`.
  Свой разбор cron (`cron.go`, без внешней библиотеки) и фоновый планировщик
  (`Deps.Scheduler`, запускается `cmd/server` отдельной горутиной), с
  защитой от двойного запуска несколькими инстансами через
  `pg_try_advisory_lock`.
- `cloudruntime` — `/api/cloud-runtime/**`: прозрачный HTTP-прокси во внешний
  облачный fleet-сервис (`GOOSAR_CLOUDRUNTIME_BASE_URL`/`_API_KEY`); без
  них — `503` на все маршруты группы.
- `integration` — интеграции воркспейса (T-029): GitHub App
  (`/api/workspaces/{id}/github/**`, `/api/github/setup`,
  `POST /api/webhooks/github`), self-hosted VCS (`/api/workspaces/{id}/vcs/**`,
  `POST /api/webhooks/vcs/{connectionId}`), Slack (`/api/workspaces/{id}/slack/**`,
  `/api/slack/binding/redeem`), Composio (`/api/integrations/composio/**`).
  Внешние API — через клиенты с настраиваемым базовым URL (см. переменные
  выше), подписанные redirect-билеты и AES-GCM для секретов (`seal.go`).
- `billing` — `/api/cloud-billing/**` + `POST /api/webhooks/stripe` (T-029):
  прозрачный прокси в тот же облачный сервис, что `cloudruntime`
  (`GOOSAR_CLOUDRUNTIME_BASE_URL`/`_API_KEY`).
- `export` — `/api/workspaces/{id}/export/**` (асинхронный job,
  `space_export_jobs`) и `GET /api/me/export` (синхронный поток) — T-029;
  `.tar.gz` архив через `internal/asset.Storage`.
- `misc` — последние одиночные ручки без отдельного домена (T-029):
  `POST /api/feedback`, `POST /api/contact-sales`, `POST /api/client-usage`,
  `GET /api/status`.

### Добавить новый домен

1. `internal/<domain>/deps.go` — свой `Deps` (не `app.Deps`: см.
   `server2/docs/decisions.md`, пункт 1 раздела T-026, за причину).
2. `internal/<domain>/register.go` — `func Register(router *httpapi.Router,
   deps *Deps)`, регистрирует маршруты через `router.Handle`.
3. `internal/app/deps.go` — завести поле и собрать домен в `app.New`.
4. `internal/app/routes.go` — одна строка `<domain>.Register(router,
   d.<Domain>)`, **до** `RegisterStubs(router)`.
5. Если контракт обновился — перегенерировать заглушки:
   `cd server2 && go run ./tools/genstubs -spec ../docs/50-api-contract.yaml -out internal/app/stubs_gen.go`.
   `RegisterStubs` сам пропускает пути, которые уже занял домен
   (`router.HandleStub`), так что регенерация безопасна в любой момент.

### Проверка контракта

```sh
# сервер поднят на :8299 (см. "Запуск" выше, PORT=8299)
cd e2e/contract
BASE_URL=http://localhost:8299 GOOSAR_DEV_VERIFICATION_CODE=424242 \
  go test ./... -run 'Contract/(auth|workspaces|me)$' -v
```

### e2e фронтенда

`e2e/{fixtures.ts,helpers.ts,*.spec.ts}` (репозиторий верхнего уровня, вне
server2) резолвят тестового пользователя/воркспейс/задачи частью прямых SQL
по именам таблиц исходного сервера (`verification_code`, `"user"`,
`workspace.issue_counter`, `issue`) — так и задумано (эти файлы не входят в
эту сессию переписи, их менять нельзя). Модель данных server2 — с другими
именами по всей схеме, поэтому для такого прогона нужен необязательный
переводной слой: `server2/testdata/e2e-compat/*.up.sql` — представления,
транслирующие эти имена в реальную схему server2. Это **не миграция**: файлы
не лежат в `server2/migrations` (не должны совпадать с прод-схемой ни по
таблицам, ни по проверке `server2/migrations/check_names.py`) и не
применяются автоматически ни при обычном старте, ни при `--migrate`/`MIGRATE=true`.

Включить явно — переменная `E2E_COMPAT_SQL_DIR` (пусто по умолчанию),
применяется идемпотентно при каждом старте (`DROP ... IF EXISTS` перед
`CREATE`, без отдельной таблицы учёта версий) и только вне production
(`APP_ENV=production` пропускает её с предупреждением в логе — тот же
принцип, что и `GOOSAR_DEV_VERIFICATION_CODE`):

```sh
DATABASE_URL=... MIGRATE=true \
E2E_COMPAT_SQL_DIR=server2/testdata/e2e-compat \
GOOSAR_DEV_VERIFICATION_CODE=424242 PORT=8410 \
  go run ./cmd/server -migrate
```

Подробности и обоснование каждого представления — в самом
`server2/testdata/e2e-compat/001_views.up.sql` и в
`server2/docs/decisions.md` (раздел «T-027 доводка»).

## Импорт

`server2/cmd/import` переносит один воркспейс с работающего старого сервера
(контракт `docs/50-api-contract.yaml`, `v1.2.0`) в базу `server2`. Полное
описание алгоритма, порядка вызовов и того, что сознательно не переносится —
`docs/51-data-model.md`, раздел «Перенос данных»; решения по пробелам
спецификации, обнаруженным при реализации — `server2/docs/decisions.md`.

Источник читается **только** через `GET`-запросы контракта (не SQL-в-SQL);
запись в целевую базу `server2` идёт по SQL, одной транзакцией на весь
импорт — сам `server2` ещё не реализует контракт как HTTP API (это T-026+).
Исходные `uuid` сохраняются: все строки в `server2` создаются с теми же
`id`, что и в источнике, поэтому повторный запуск идемпотентен
(`ON CONFLICT (id) DO UPDATE`/`DO NOTHING` в зависимости от таблицы).

### Флаги

| Флаг | Обязателен | Смысл |
|---|---|---|
| `--source-url` | да | базовый URL исходного сервера, например `http://localhost:8199` |
| `--token` | да | `Authorization: Bearer` PAT или сессионный JWT с ролью `owner` в исходном воркспейсе |
| `--workspace` | да | slug или id исходного воркспейса |
| `--database-url` | да (кроме `--dry-run`) | DSN целевой базы server2 |
| `--migrate` | нет | применить `server2/migrations` к целевой базе перед импортом |
| `--dry-run` | нет | только прочитать источник и напечатать сводку в JSON, ничего не писать в базу |
| `--migrations-dir` | нет | каталог с `NNN_*.up.sql` (по умолчанию `server2/migrations`) |

### Пример запуска

```sh
createdb goosar2_import   # или: psql -c 'CREATE DATABASE goosar2_import'

go run ./server2/cmd/import \
  --source-url http://localhost:8199 \
  --token "$PAT" \
  --workspace acme-workspace \
  --database-url postgres://postgres@localhost:5432/goosar2_import \
  --migrate
```

Утилита печатает в stdout JSON-отчёт: сколько строк каждой сущности
перенесено (`transferred`) и какие категории данных сознательно пропущены с
причиной (`skipped_by_design`) — секреты сессий/PAT/webhook, зашифрованные
поля конфигурации агентов и воркспейса, OAuth-токены интеграций, биллинг,
аудит уровня деплоя, байты вложений, история inbox, чаты участников кроме
владельца токена импорта, сырой журнал очереди задач. Подробное обоснование
каждого пункта — `docs/51-data-model.md` и `server2/docs/decisions.md`.

### Что переносится

Воркспейс (`spaces`), участники (`space_members`, с созданием/обновлением
соответствующих `accounts`), runtime-профили (`agent_protocols`), runtime-
среды (`executors`), агенты (`operatives`, с целями вызова и привязанными
навыками), навыки (`capabilities` + `capability_files`), отряды (`crews` +
`crew_members`), метки (`tags`, по всем `resource_type`, если включены),
кастомные свойства (`field_defs`), проекты (`initiatives` + `initiative_resources`),
задачи (`tickets`, с метками, реакциями, родителем — вторым проходом),
комментарии задач (`ticket_notes` + `note_marks`, с ответами треда — вторым
проходом), подписчики и pull-request-ссылки задач, автопилоты (`sentinels` +
`sentinel_triggers`, без webhook/signing-секретов), чат-сессии владельца
токена и их сообщения (`convos`/`convo_messages`), MCP-серверы воркспейса
(`space_mcp_servers`, без секретов подключения) и незасекреченная часть
конфигурации воркспейса (`space_config`).

### Живая проверка

Проверено вручную против запущенного исходного сервера
(`http://localhost:8199`, вход по email-коду `424242` в dev-режиме): создан
тестовый воркспейс с runtime, агентом, проектом, метками, несколькими
задачами (включая под-задачу), комментариями (в т.ч. `/note`), реакцией,
отрядом и чат-сессией; количества всех перенесённых сущностей совпали с
данными исходного API; повторный запуск импорта не изменил число строк ни в
одной таблице (идемпотентность подтверждена).
