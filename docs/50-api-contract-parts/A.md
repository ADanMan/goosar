# Часть A: платформа, аутентификация, демон

Эта часть описывает поведение маршрутов из диапазона `server/cmd/server/router.go` строк 1–662 (всё, что зарегистрировано до `r.Route("/api/workspaces", ...)`): публичные и health-маршруты, `/ws`, `/api/auth/**`, `/api/config`, вебхуки, весь `/api/daemon/**`, `/api/attachments/{id}/download`, `/api/me/**` и соседние ручки аккаунта. OpenAPI-фрагмент — `A.yaml` в этой же папке.

Число маршрутов в этом диапазоне router.go: **79** (совпадает с числом операций в `A.yaml`).

---

## 1. Общие правила

### 1.1 Идентификаторы и даты

- Все идентификаторы сущностей — UUID (RFC 4122), в стандартном текстовом представлении `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`, в нижнем регистре. Сервер генерирует новые id как UUIDv7 там, где генерация видна снаружи (например, id вложения при загрузке файла); для строк, порождаемых базой (сессии, задачи и т.п.), формат тот же UUID, версия не гарантируется клиенту.
- Токены и секреты — непрозрачные строки с распознаваемым префиксом, тип токена определяется по префиксу, а не по заголовку или полю:
  - `gsl_...` — персональный access-токен (PAT) человека;
  - `gsln_...` — «облачный» PAT, проверяется через внешний cloud-fleet сервис;
  - `mat_...` — короткоживущий (24 часа) токен агента на одну задачу, выдаётся при захвате задачи демоном;
  - `mdt_...` — токен демона (`Authorization` на `/api/daemon/**`), в нём зашита привязка к workspace и daemon_id;
  - `awt_...` — секрет входящего вебхука автопилота (часть URL, не заголовок);
  - иначе строка парсится как HS256 JWT сессии (тот же формат, что в cookie).
- Даты и время — везде RFC 3339 (`2006-01-02T15:04:05Z07:00`), в UTC. Для сообщений задач/чата иногда используется RFC3339 с наносекундами.

### 1.2 Формат ошибок

Единый формат тела ошибки на всём API (см. `components.schemas.Error` в `A.yaml`):

```json
{ "error": "human readable message", "code": "stable_machine_code", "request_id": "..." }
```

`code` присутствует на большинстве осмысленных 4xx (например, `invalid_or_expired_code`, `rate_limited`, `daemon_too_old`, `account_deactivated`), но не гарантирован на всех — часть отказов (`writeError`) отдаёт только `error` (+`request_id`, если уже был выставлен `X-Request-ID`). 5xx обычно тоже используют этот формат, но носят общее сообщение без `code`.

Отдельно: часть ручек демона на «отчёте о результате» (`ReportUpdateResult`, `ReportModelListResult`, `ReportLocalSkillListResult`, `ReportLocalSkillImportResult`, `ReportTaskProgress`, `ReportTaskUsage`, `ReportTaskMessages`, `AckTaskCancelled` и т.п.) всегда возвращают `200 {"status":"ok"}`, даже если внутренняя обработка одной записи не удалась — ошибка логируется на сервере, а не транслируется демону, чтобы не блокировать очередь.

### 1.3 Аутентификация

Три независимых цепочки middleware в этом диапазоне:

1. **Cookie-сессия** (`goosar_auth`, HttpOnly, `SameSite=Strict`, `Secure` включается автоматически, если `FRONTEND_ORIGIN` — https). Любой не-GET/HEAD/OPTIONS запрос, аутентифицированный этой cookie, обязан прислать заголовок `X-CSRF-Token`, значение которого проверяется HMAC-подписью относительно значения `goosar_auth` (сам CSRF-токен лежит в отдельной non-HttpOnly cookie `goosar_csrf`, которую фронтенд читает и кладёт в заголовок). Без валидного `X-CSRF-Token` — `403`.
2. **Bearer-токен** (`Authorization: Bearer <token>`) — обычный пользовательский путь `middleware.Auth`, применяется ко всей группе `/api/**`, кроме публичных ручек и `/api/daemon/**`. Разбирает префикс токена по правилам из §1.1. Во всех случаях, кроме PAT, дополнительно проверяется, что учётная запись не деактивирована и версия токена (`tv` claim) совпадает с текущей `token_version` пользователя (иначе `401 session_revoked`).
3. **Daemon-токен** (`middleware.DaemonAuth`) — отдельная цепочка на весь `/api/daemon/**` (включая `/api/daemon/ws`). Принимает те же префиксы `gsln_`/`gsl_`, plus `mdt_...` (даемон-токен), plus JWT-сессию человека. Daemon-токен несёт `workspace_id`/`daemon_id` прямо в себе (не требует похода в БД при попадании в кэш); человеческие пути требуют, чтобы вызывающий был участником workspace (с кэшированием членства на 5 минут, `MembershipCache`).

Дополнительно: `RequireHumanActor` — миддлварь, отклоняющая (`403`) запросы, у которых `X-Actor-Source` равен `task_token` или `cloud_pat` (т.е. запрос пришёл от `mat_...` задачи-агента либо от cloud-PAT сервисного вызова), а не от живого человека. Навешана на MFA-ручки, `cli-token`, `client-usage`, `export`, ревокацию сессий.

`X-Actor-Source` — внутренний заголовок, который сервер сам выставляет/чистит на входе (клиент не может его подделать: он удаляется в начале каждой auth-миддлвари перед разбором токена).

### 1.4 Резолв workspace

Там, где workspace не идёт в пути URL, порядок резолва (`middleware.ResolveWorkspaceIDFromRequest` / `resolveWorkspaceUUID`) такой (первое непустое значение побеждает):

1. Если `X-Actor-Source: task_token` — workspace жёстко берётся из `X-Workspace-ID`, выставленного самой авторизацией токена задачи; клиентские заголовки/параметры при этом игнорируются.
2. Уже резолвленный workspace в контексте запроса (если более ранняя миддлварь его установила).
3. Заголовок `X-Workspace-Slug` → поиск по слагу.
4. Query-параметр `workspace_slug` → поиск по слагу.
5. Заголовок `X-Workspace-ID`.
6. Query-параметр `workspace_id`.

В части A этот механизм фактически используется в `/api/upload-file` и `/api/client-usage` (workspace опционален: без него — персональная загрузка/учёт использования, с ним — требуется членство).

### 1.5 Rate limiting

Бэкенд лимитера настраивается через Redis, если он подключён (`rdb`), иначе — in-process fallback (тот же интерфейс, per-instance). Ключевые лимиты этого диапазона (переменные окружения → значение по умолчанию):

| Лимит | Переменная | По умолчанию | Ключ |
|---|---|---|---|
| Отправка кода входа | `RATE_LIMIT_AUTH` | 5/мин | по IP |
| Проверка кода/ссылки/OIDC start-callback/MFA verify | `RATE_LIMIT_AUTH_VERIFY` | 20/мин | по IP |
| Отправка/проверка кода по email | `RATE_LIMIT_AUTH_EMAIL` | 10/мин | по полю `email` тела запроса (или `username` для LDAP) |
| Выпуск токенов (`cli-token`, MFA enroll/confirm/disable/recovery-codes) | `RATE_LIMIT_TOKEN` | 20/час | по user (иначе IP) |
| MFA verify по конкретному `mfa_token` | `RATE_LIMIT_MFA_VERIFY` | 10 / 5 минут (TTL «pending»-токена) | по хэшу `mfa_token` |
| Общий API-лимит на всю аутентифицированную группу `/api/**` | `RATE_LIMIT_API` | 600/мин | по user (иначе IP) |
| `/api/contact-sales` | `RATE_LIMIT_CONTACT_SALES` | 5/час | по IP (+ отдельный серверный кап 3/час на email) |
| `/api/me/export` | `RATE_LIMIT_EXPORT` | 3/час | по user |
| Присоединение к workspace/инвайты (используется вне части A, но лимитер общий) | `RATE_LIMIT_JOIN` | 20/час | по user (иначе IP) |
| Вебхук автопилота, на токен триггера | внутренний `WebhookRateLimiter` | 60/мин | по токену триггера |
| Вебхуки (Stripe, автопилот) по IP при плохом деливери | `WebhookIPRateLimiter` | 30/мин | по IP |
| Вебхуки, абсолютный потолок по IP | `WebhookAbsoluteIPRateLimiter` | 600/мин | по IP, независимо от исхода |
| Доверенные прокси для расчёта клиентского IP | `RATE_LIMIT_TRUSTED_PROXIES` (иначе общий `clientip.FromEnv()`) | — | — |

Превышение лимита — `429` с заголовком `Retry-After` (секунды) и телом `{"error":"too many requests","code":"rate_limited"}` (для http-миддлвари), либо специфичный код (`code_rate_limited`, `429` без кастомного кода на `/auth/send-code` повторной отправке и т.п.) на ручках со своей отдельной проверкой.

### 1.6 CORS

`cors.Handler` навешан глобально: `AllowedOrigins` — из `CORS_ALLOWED_ORIGINS` (список через запятую), иначе `FRONTEND_ORIGIN`, иначе дефолт `http://localhost:3000,5173,5174`. Разрешённые методы: `GET, POST, PUT, PATCH, DELETE, OPTIONS`. Разрешённые заголовки: `Accept, Authorization, Content-Type, X-Workspace-ID, X-Workspace-Slug, X-Request-ID, X-Agent-ID, X-Task-ID, X-CSRF-Token, X-Client-Platform, X-Client-Version, X-Client-OS, X-Client-Capabilities`. `Access-Control-Expose-Headers: X-Request-ID`. `AllowCredentials: true`, `MaxAge: 300`.

### 1.7 Прочие сквозные заголовки

- `X-Request-ID` — генерируется/пробрасывается `middleware.RequestID` (значение клиента принимается, только если состоит из безопасного алфавита и не длиннее лимита; иначе генерируется заново).
- `X-Client-Platform`, `X-Client-Version`, `X-Client-OS` — метаданные клиента, читаются `middleware.ClientMetadata` и кладутся в контекст; используются, в частности, для отсечения устаревших демонов (`RequireMinDaemonVersion`) и в аналитике/`client-usage`.
- `Content-Security-Policy` выставляется отдельной миддлварью на все ответы.
- Ответ на rate-limit и на `writeLookupUnavailable` может содержать `Retry-After`.

### 1.8 Пагинация

В диапазоне части A пагинации нет: все списочные ручки (`/api/auth/sessions`, `/api/daemon/tasks/{taskId}/messages` (кроме курсора `since` по `seq`), `/api/workspace-templates`, `/api/daemon/workspaces`, `/api/daemon/workspaces/{workspaceId}/runtime-profiles`, список задач в `/api/daemon/runtimes/{runtimeId}/tasks/pending`) возвращают полный список без `limit`/`offset`/`cursor` — объём естественно ограничен (сессии одного пользователя, задачи одного рантайма и т.п.). В других частях контракта используются как минимум три разных схемы пагинации (`limit/offset`, `limit/cursor`, `page/per_page`) — единого стандарта на всё API нет.

### 1.9 Переменные окружения, влияющие на внешнее поведение части A

**Сеть/CORS/публичные URL:** `CORS_ALLOWED_ORIGINS`, `FRONTEND_ORIGIN`, `GOOSAR_APP_URL`, `GOOSAR_PUBLIC_URL`, `GOOSAR_TRUSTED_PROXIES`, `RATE_LIMIT_TRUSTED_PROXIES`, `PORT`/`BACKEND_PORT`.

**Регистрация и вход:** `ALLOW_SIGNUP`, `ALLOWED_EMAILS`, `ALLOWED_EMAIL_DOMAINS`, `DISABLE_WORKSPACE_CREATION`, `APP_ENV` (только `production` отключает dev-код), `GOOSAR_DEV_VERIFICATION_CODE` (фиксированный код входа вне production), `AUTH_TOKEN_TTL` (по умолчанию 30 дней), `COOKIE_DOMAIN`, `JWT_SECRET`/`JWT_SECRET_PREVIOUS`.

**MFA:** `GOOSAR_MCP_SECRET_KEY` (тот же ключ шифрует и MCP-конфиги агентов, и TOTP-секреты; без него — `503 mfa_unavailable`, а сохранённые MCP-конфиги пишутся в БД plaintext), `GOOSAR_MCP_SECRET_KEY_PREVIOUS` (ротация ключа), `GOOSAR_TOTP_ISSUER`.

**Корпоративный вход:** переменные OIDC/LDAP читаются пакетом `corpauth` (не в этом диапазоне файлов, но эффект виден в `/api/auth/methods`, `/api/auth/oidc/**`, `/api/auth/ldap/login`).

**Рейт-лимиты:** `RATE_LIMIT_AUTH`, `RATE_LIMIT_AUTH_VERIFY`, `RATE_LIMIT_AUTH_EMAIL`, `RATE_LIMIT_TOKEN`, `RATE_LIMIT_MFA_VERIFY`, `RATE_LIMIT_API`, `RATE_LIMIT_CONTACT_SALES`, `RATE_LIMIT_EXPORT`, `RATE_LIMIT_JOIN`.

**Вложения:** `ATTACHMENT_DOWNLOAD_MODE` (`auto`/`cloudfront`/`presign`/`proxy`), `ATTACHMENT_DOWNLOAD_URL_TTL` (по умолчанию 30 минут), `LOCAL_UPLOAD_DIR` (включает локальный backend хранилища и, вместе с ним, маршрут `/uploads/*`), `S3_BUCKET`/`S3_REGION`/`AWS_*` (S3-backend), `CLOUDFRONT_KEY_PAIR_ID`/`CLOUDFRONT_PRIVATE_KEY(_SECRET)`/`CLOUDFRONT_DOMAIN`.

**Демон/рантаймы:** `GOOSAR_MIN_DAEMON_VERSION` (минимальная версия CLI для получения задач; `none`/`off`/`0`/`false` отключает проверку), `GOOSAR_TRUSTED_PROXIES` (тот же, что для CORS/realtime), `REALTIME_METRICS_TOKEN` (доступ к `/health/realtime` не с loopback), `GOOSAR_PROVISIONING_STORE`/`GOOSAR_PROVISIONING_*` (не бьёт напрямую по части A, но влияет на то, что видит демон при регистрации через смежные ручки).

**Интеграции, чьи вебхуки/колбэки лежат в части A:** `GITHUB_WEBHOOK_SECRET`, `GITHUB_APP_SLUG`, `GITHUB_APP_ID`/`GITHUB_APP_PRIVATE_KEY`, `GOOSAR_VCS_INTEGRATION_ENABLED`, `GOOSAR_VCS_SECRET_KEY`(`_PREVIOUS`), `COMPOSIO_API_KEY`, `COMPOSIO_STATE_SECRET` (иначе `JWT_SECRET`), `COMPOSIO_CALLBACK_BASE_URL` (иначе `GOOSAR_PUBLIC_URL`), `GOOSAR_SLACK_SECRET_KEY`(`_PREVIOUS`) (не даёт публичных HTTP-маршрутов в части A напрямую, но включается в общей инициализации роутера).

**Аналитика/фича-флаги (влияют на `/api/config`):** `POSTHOG_API_KEY`, `POSTHOG_HOST`, `ANALYTICS_FRONTEND_ENABLED`, `ANALYTICS_DISABLED`, `GOOSAR_FEATURE_FLAGS_FILE`, `GOOSAR_DELIVERY_PROFILE`, `GOOSAR_DEPLOYMENT_JIRA_URL`/`_CONFLUENCE_URL`/`_EWS_URL`/`_MAIL_DOMAIN`/`_LLM_API_BASE`/`_LLM_MODEL`, `GOOSAR_OFFICIAL_CLOUD_HOST` (скрывает `server_version`/daemon setup URLs на официальном облаке).

---

## 2. Протоколы WebSocket

### 2.1 `/ws` — realtime-канал для клиентов (веб/десктоп)

**Handshake.** `GET /ws?workspace_id=<uuid>|workspace_slug=<slug>[&client_platform=&client_version=&client_os=]`. Один из `workspace_id`/`workspace_slug` обязателен (slug резолвится в id на сервере; неизвестный slug → `404` ещё до апгрейда). Проверка `Origin` (см. CORS выше плюс `X-Forwarded-Host` от доверенного прокси) — несовпадающий origin отклоняется до апгрейда.

Дальше — два пути аутентификации:

- **Если у запроса есть валидная cookie `goosar_auth`** — сервер сразу проверяет членство в workspace (`IsMember`, с учётом `token_version` из claim `tv`) и, если всё ок, апгрейдит соединение без дополнительного обмена сообщениями.
- **Иначе** (нет cookie, либо клиент desktop/CLI без cookie-сессии) — апгрейд происходит сразу, но сервер даёт 10 секунд на первый входящий фрейм вида `{"type":"auth","payload":{"token":"<gsl_/gsln_ бессмысленны здесь — принимается PAT gsl_/сессионный JWT>"}}`. Если фрейм не пришёл, невалиден или токен не проходит проверку членства — сервер отправляет `{"type":"auth_error",...}` (или `{"error":"..."}`) и закрывает соединение; при успехе отправляет `{"type":"auth_ack"}`.

Токен в auth-фрейме принимает: `gsl_...` PAT (через `PATResolver`) или HS256 сессионный JWT (`sub`, опционально `tv` для проверки ревокации по `token_version`). Токен вида `gsln_`/`mdt_`/`mat_` тут не поддерживается — только PAT или сессия.

**После апгрейда** клиент автоматически подписан на две комнаты: `workspace:<workspace_id>` и (если аутентифицирован) `user:<user_id>`.

**Входящие фреймы клиент → сервер** (JSON, `{"type": "...", "payload": {...}}`, лимит размера фрейма 64 KiB):

| type | payload | назначение |
|---|---|---|
| `subscribe` | `{"scope": "workspace"\|"user"\|"task"\|"chat", "id": "<uuid>"}` | подписаться на дополнительную комнату. `workspace`/`user` разрешены только на собственные id (иначе `subscribe_error` с `error:"forbidden"`); `task`/`chat` проходят через `ScopeAuthorizer` (проверка, что пользователь имеет доступ к этой задаче/чату в рамках своего workspace) — отказ даёт `error:"forbidden"` или `"lookup_failed"`. Неизвестный scope → `error:"unknown_scope"`. Успех → `subscribe_ack`. |
| `unsubscribe` | `{"scope": "...", "id": "..."}` | отписаться; всегда отвечает `unsubscribe_ack`. |
| `ping` | — | сервер отвечает `{"type":"pong"}`. |

Любой другой/невалидный фрейм молча игнорируется (debug-лог на сервере). Сервер также сам шлёт protocol-уровневые WS ping-фреймы каждые ~54 с (обычный WebSocket ping/pong, не JSON) и ожидает pong в течение 60 с, иначе закрывает соединение.

**Исходящие события сервер → клиент** — все в форме `{"type": "<event>", "payload": {...}, "actor_id"?: "...", "actor_type"?: "..."}`. Полный список типов событий (из `packages/core/types/events.ts`, канонический источник) и их payload:

- **Issues**: `issue:created` `{issue}`, `issue:updated` `{issue, assignee_changed?, status_changed?, project_changed?}`, `issue:deleted` `{issue_id}`, `issue_labels:changed` `{issue_id, labels}`, `issue_metadata:changed` `{issue_id, metadata}`, `issue_properties:changed` `{issue_id, properties}`, `issue_reaction:added` `{reaction, issue_id}`, `issue_reaction:removed` `{issue_id, emoji, actor_type, actor_id}`.
- **Comments/reactions**: `comment:created`/`comment:updated` `{comment}`, `comment:deleted` `{comment_id, issue_id}`, `comment:resolved`/`comment:unresolved` `{comment}`, `reaction:added` `{reaction, issue_id}`, `reaction:removed` `{comment_id, issue_id, emoji, actor_type, actor_id}`.
- **Agents**: `agent:status`/`agent:created`/`agent:archived`/`agent:restored` — все `{agent}`.
- **Tasks**: `task:queued` `{task_id, agent_id, issue_id, chat_session_id?, status}`, `task:dispatch` `{task_id, agent_id, issue_id, runtime_id, chat_session_id?}`, `task:running`/`task:completed`/`task:failed`/`task:cancelled` — `{task_id, agent_id, issue_id, chat_session_id?, status}`, `task:waiting_local_directory` — то же плюс `wait_reason?`, `task:progress` — свободная форма (см. `TaskProgressPayload` в `pkg/protocol`: `task_id, summary, step?, total?`), `task:message` — `{task_id, issue_id, chat_session_id?, seq, type, tool?, content?, input?, output?, created_at?}`.
- **Inbox**: `inbox:new` `{item}`, `inbox:read` `{item_id, recipient_id}`, `inbox:archived` `{item_id, recipient_id}`, `inbox:unarchived` `{item_id, issue_id, recipient_id}`, `inbox:batch-read`/`inbox:batch-archived` `{recipient_id, count}`.
- **Workspace/members**: `workspace:updated` `{workspace}`, `workspace:deleted` `{workspace_id}`, `member:added` `{member, workspace_id, workspace_name?}`, `member:updated` `{member}`, `member:removed` `{member_id, user_id, workspace_id}`.
- **Daemon (эхо в клиентский канал)**: `daemon:heartbeat`, `daemon:register` — форма свободная (объект с деталями регистрации/дерегистрации рантаймов).
- **Skills**: `skill:created`/`skill:updated`/`skill:deleted` — форма свободная.
- **Subscribers**: `subscriber:added` `{issue_id, user_type, user_id, reason}`, `subscriber:removed` `{issue_id, user_type, user_id}`.
- **Activity**: `activity:created` `{issue_id, entry}`.
- **Chat**: `chat:message` `{chat_session_id, message_id, role, content, task_id?, created_at}`, `chat:done` `{chat_session_id, task_id, message_id?, content?, elapsed_ms?, created_at?, message_kind?}`, `chat:cancel_finalized` `{outcome: "stopped"|"restored", chat_session_id, task_id, initiator_user_id?, message_id?, content?, message_kind?, created_at?, elapsed_ms?}`, `chat:session_read` `{chat_session_id}`, `chat:session_deleted` `{chat_session_id}`, `chat:session_updated` — форма свободная.
- **Projects**: `project:created`/`project:updated` `{project}`, `project:deleted` `{project_id}`.
- **Squads/labels/properties/pins** (форма свободная, детали вне части A): `squad:created/updated/deleted`, `label:created/updated/deleted`, `property:created/updated` `{property}`, `pin:created/deleted/reordered`.
- **Invitations**: `invitation:created` `{invitation, workspace_name?}`, `invitation:accepted` `{invitation_id, member}`, `invitation:declined`/`invitation:revoked` `{invitation_id, invitee_email}`.
- **GitHub/VCS**: `github_installation:created`/`github_installation:deleted`, `pull_request:linked`/`pull_request:updated`/`pull_request:unlinked` — форма свободная (в части A их публикуют вебхук-обработчики GitHub/VCS).
- **Служебное**: `connection:revoked` — сервер шлёт клиенту при принудительном разрыве (например, после revoke-all-sessions) `{"type":"connection:revoked"}` без payload, затем закрывает соединение.

Доставка внутри одного процесса идёт через `Hub` (комнаты `workspace:<id>`, `user:<id>`, `task:<id>`, `chat:<id>`); в многопроцессном/Redis-режиме события реплицируются между инстансами через `redis_relay.go`/`sharded_stream_relay.go` (не отдельный публичный протокол — детали релея не наблюдаемы клиентом). Дедупликация "уже видел это событие" — по `event_id` внутри окна последних 128 событий на клиента.

### 2.2 `/api/daemon/ws` — канал для демонов

**Handshake.** `GET /api/daemon/ws?runtime_id=<uuid>[&runtime_id=<uuid>...]|runtime_ids=<uuid>,<uuid>` под тем же `daemonAuth`, что и остальной `/api/daemon/**` (заголовок `Authorization`, см. §1.3). Нужен хотя бы один `runtime_id`/`runtime_ids`, либо аутентифицированный человек без привязки к рантайму (тогда соединение привязывается только к `user:<id>`). Каждый переданный `runtime_id` должен реально существовать и быть доступен вызывающему (то же самое, что `requireDaemonRuntimeAccess` на HTTP-ручках) — иначе `404`; если запрос пришёл по daemon-токену, привязанному к конкретному `daemon_id`, рантайм должен принадлежать этому же `daemon_id`. `Origin` не проверяется для этого канала (сервисный клиент). Апгрейд без дополнительного обмена сообщениями — аутентификация целиком на уровне HTTP handshake, отдельного auth-фрейма, в отличие от `/ws`, здесь нет.

После подключения клиент регистрируется сразу в нескольких индексах: по каждому `runtime_id`, по каждому доступному `workspace_id` (выведенному из рантаймов), и по `user_id`, если он есть.

**Сообщения демон → сервер** (JSON `{"type": "...", "payload": {...}}`):

| type | payload | ответ/эффект |
|---|---|---|
| `daemon:heartbeat` | `{"runtime_id","supports_batch_import"?}` | сервер вызывает тот же обработчик, что и `POST /api/daemon/heartbeat`, и присылает `daemon:heartbeat_ack` с тем же телом, что REST-ответ (`status`, `pending_update`/`pending_model_list`/`pending_local_skills`/`pending_local_skill_import(s)`). Хартбит для `runtime_id`, не входящего в список, с которым соединение зарегистрировалось, отклоняется молча (только серверный warn-лог, ack не отправляется). |
| `daemon:rpc_request` | `{"request_id","method","body"?,"timeout_ms"?}` | сервер асинхронно выполняет RPC (не более 8 одновременных запросов на одно соединение — сверх лимита сразу `daemon:rpc_response` со `status:429`) и присылает `daemon:rpc_response` `{"request_id","status","body"?,"error"?}`. Пока не документируется набор `method` — это внутренний RPC-канал; трактуйте как непрозрачный запрос-ответ, маршрутизируемый по `request_id`. |

Любой другой `type` игнорируется без ответа.

**Сообщения сервер → демон** (пуш, без запроса от демона):

| type | payload | когда отправляется |
|---|---|---|
| `daemon:task_available` | `{"runtime_id","task_id"?}` | серверу стало известно о новой задаче для этого рантайма (уведомление «есть что забрать» — сама задача не передаётся, демон обязан вызвать claim-ручку) |
| `daemon:runtime_profiles_changed` | `{"workspace_id","runtime_profile_id"?}` | изменился/удалён custom runtime profile воркспейса |
| `daemon:workspaces_changed` | `{}` | список воркспейсов пользователя изменился (приглашение/выход) |
| `daemon:heartbeat_ack` | см. выше | ответ на `daemon:heartbeat` |
| `daemon:rpc_response` | см. выше | ответ на `daemon:rpc_request` |

Пуш-уведомления дедуплицируются по `event_id` (окно 128 последних на клиента), как и в `/ws`. Сервер шлёт стандартные protocol-level ping каждые ~54 с, таймаут pong — 60 с. Максимальный размер входящего фрейма — 64 KiB.

---

## 3. Маршруты по разделам

Права (`x-roles`) и лимиты — как в `A.yaml`; здесь — краткое действие и наблюдаемые побочные эффекты (публикуемые realtime-события, что создаётся/меняется).

### 3.1 Health / метрики

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/health` | public | Ливнес: всегда `{"status":"ok"}`, без обращения к БД. |
| GET | `/readyz` | public | Ready-проверка: пинг БД + сверка применённых миграций со списком, вкомпилированным в бинарник. Результат кэшируется на 3 секунды. `503`, если БД недоступна или есть неприменённые миграции. |
| GET | `/healthz` | public | Алиас `/readyz` (тот же обработчик). |
| GET | `/health/realtime` | operator | Снимок счётчиков `Hub`/`daemonws.Hub` (подключения, комнаты, дропы). Доступ — либо строго с loopback-адреса без заголовков форвардинга, либо (если задан `REALTIME_METRICS_TOKEN`) с `Authorization: Bearer <token>` откуда угодно. |

### 3.2 Realtime

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/ws` | any-authenticated (см. §2.1) | Апгрейд до WebSocket, полный протокол — §2.1. Побочный эффект — подписка клиента на комнаты `workspace`/`user` в `Hub`, видна другим наблюдателям только как изменение счётчиков `/health/realtime`. |

### 3.3 Auth — публичные

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| POST | `/auth/send-code` | public | Создаёт (при разрешённой регистрации) 6-значный код + magic-link токен, инвалидирует более ранние коды на этот email, отправляет письмо. Не создаёт пользователя. Аудит `login_code_sent`. |
| POST | `/auth/verify-code` | public | Сверяет код (или dev-код вне production), помечает его использованным, при первом входе создаёт пользователя (учитывая allow-list/домены), при необходимости требует MFA, иначе выдаёт сессию (ставит cookies `goosar_auth`+`goosar_csrf`, создаёт строку сессии). Может проактивно засеять deployment-админов из `GOOSAR_DEPLOYMENT_ADMIN_EMAILS`, если это первый пользователь и админов ещё нет. |
| POST | `/auth/verify-link` | public | То же самое, но по magic-link токену вместо кода. |
| POST | `/auth/logout` | public | Всегда чистит cookies сессии/CSRF; не требует и не проверяет валидность текущей сессии. Пишет аудит-событие `logout`, если сессию всё же удалось разобрать. |
| POST | `/api/auth/mfa/verify` | public (держатель `mfa_token`) | Завершает вход по TOTP-коду или коду восстановления, выдаёт сессию так же, как verify-code. |
| GET | `/api/auth/methods` | public | Список включённых методов входа (`email` всегда как минимум фолбэк). |
| GET | `/api/auth/oidc/start` | public | Редирект на IdP; ставит подписанную state-cookie. |
| GET | `/api/auth/oidc/callback` | public | Обмен кода на identity у IdP, дальше — как verify-code (создание пользователя/MFA/сессия), но результат — HTTP-редирект (в приложение или `goosar://` для десктопа), а не JSON. |
| POST | `/api/auth/ldap/login` | public | Аутентификация в LDAP/AD, дальше как verify-code. |

### 3.4 Auth — только для аутентифицированных людей

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/api/auth/mfa` | human | Статус MFA (включён/ожидает подтверждения/обязателен политикой/доступен ли вообще на этом сервере). |
| POST | `/api/auth/mfa/totp/enroll` | human | Генерирует TOTP-секрет, шифрует его (`GOOSAR_MCP_SECRET_KEY`) и сохраняет как «ожидающий подтверждения». `409`, если уже включён; `503`, если ключ шифрования не настроен. |
| POST | `/api/auth/mfa/totp/confirm` | human | Подтверждает enrollment живым кодом, включает MFA, выпускает 10 одноразовых recovery-кодов (аудит `mfa_enrolled` + `mfa_recovery_issued`). |
| POST | `/api/auth/mfa/totp/disable` | human | Требует текущий код/recovery-код; выключает MFA и удаляет recovery-коды. |
| POST | `/api/auth/mfa/recovery-codes` | human | Требует текущий код; перевыпускает 10 новых recovery-кодов, старые становятся недействительны. |
| GET | `/api/auth/sessions` | human | Список сессий пользователя с пометкой `current`. |
| DELETE | `/api/auth/sessions/{sessionId}` | human (свои сессии) | Отзывает одну сессию по id. |
| POST | `/api/auth/sessions/revoke-all` | human | Отзывает все сессии и увеличивает `token_version` пользователя (мгновенно инвалидирует все ранее выданные JWT), чистит cookies самого вызвавшего. |

### 3.5 Config / Contact sales

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/api/config` | public | Публичная конфигурация фронтенда/CLI (см. `AppConfig` в `A.yaml`): доступность регистрации, CDN, фича-флаги, аналитика, allow-list провайдеров и т.д. Не требует аутентификации. |
| POST | `/api/contact-sales` | public | Валидирует форму (в т.ч. отклоняет бесплатные почтовые домены как business email), пишет заявку, шлёт аналитическое событие. Rate-limit и по IP, и по email. |

### 3.6 Webhooks

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| POST | `/api/webhooks/autopilots/{token}` | public (секрет в пути) | Нормализует произвольный JSON в конверт `{event, eventPayload, request}`, применяет фильтр событий триггера, при совпадении запускает Autopilot admission (создаёт `autopilot_run` при принятии). Дедуп по `X-GitHub-Delivery`/`Idempotency-Key`: повтор отдаёт тот же ответ, не создавая новый run. |
| POST | `/api/webhooks/github` | public (HMAC) | `ping` → `{"ok":"pong"}`. `installation` → апсертит/удаляет `github_installation`, публикует `github_installation:created`/`:deleted`. `pull_request` → зеркалит PR, пытается связать с issue по идентификаторам в заголовке/теле/ветке, публикует `pull_request:updated`. `check_suite`/`check_run`/`status` → запускает пересчёт снапшота PR. |
| GET | `/api/github/setup` | public (подписанный state) | Коллбэк после установки GitHub App: сохраняет installation, публикует `github_installation:created`, редиректит на настройки воркспейса с `?github_connected=1` либо `&github_error=<code>`. |
| POST | `/api/webhooks/vcs/{connectionId}` | public (HMAC на секрет соединения) | Аналог GitHub-вебхука для другого VCS-провайдера: зеркалит PR/commit-статус, связывает issue, публикует `pull_request:updated`. |
| POST | `/api/webhooks/stripe` | public (Stripe-Signature, проверяется выше по цепочке) | Тело + заголовок форвардятся как есть в биллинговый сервис облачного рантайма; сам сервер только режет по размеру (1 MiB) и лимитирует по IP. |
| GET | `/api/integrations/composio/callback` | public (подписанный state) | OAuth-коллбэк подключения Composio-тулкита; редирект на фронтенд с результатом. |

### 3.7 Daemon API

Все ручки — под `daemonAuth` (см. §1.3), большинство дополнительно проверяет, что аутентифицированный (человек или daemon-токен) имеет доступ к workspace, к которому принадлежит рантайм/задача (кэш членства на 5 минут).

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| POST | `/api/daemon/register` | workspace-member / daemon-token | Апсертит один или несколько рантаймов (обычных или на custom runtime profile) + фиксирует неудачно стартовавшие custom-профили. Публикует `daemon:register`. Может завести «helper»-агента воркспейса и связанную starter-issue при первой регистрации (через общий провижининг, не только легаси-шим из §3.9). |
| POST | `/api/daemon/deregister` | workspace-member / daemon-token | Помечает рантаймы offline; неизвестные/чужие id молча пропускаются. Публикует `daemon:register` (`action:"deregister"`) на каждый затронутый workspace. |
| POST | `/api/daemon/heartbeat` | workspace-member / daemon-token | HTTP-хартбит одного рантайма; отдаёт отложенные задания (self-update, model-list probe, local-skills probe, local-skill import). См. также `daemon:heartbeat` в §2.2 — тот же обработчик используется и по WS. |
| GET | `/api/daemon/ws` | workspace-member / daemon-token | Апгрейд до WS-канала демона, протокол — §2.2. |
| GET | `/api/daemon/workspaces` | any-authenticated | Список воркспейсов вызывающего (человек) либо один воркспейс, к которому привязан daemon-токен. Поддерживает `ETag`/`If-None-Match`. |
| GET | `/api/daemon/workspaces/{workspaceId}/repos` | workspace-member / daemon-token | Текущий список репозиториев воркспейса + `repos_version` (sha256 от отсортированных URL, для дешёвого сравнения на стороне демона) + настройки. |
| GET | `/api/daemon/workspaces/{workspaceId}/runtime-profiles` | workspace-member / daemon-token | Включённые custom runtime profiles воркспейса. |
| POST | `/api/daemon/runtimes/{runtimeId}/tasks/claim` | workspace-member / daemon-token | Захват одной задачи из очереди для одного рантайма; требует минимальной версии клиента (иначе `426`); выдаёт короткоживущий `mat_...` токен внутри объекта задачи. |
| POST | `/api/daemon/tasks/claim` | workspace-member / daemon-token | Пакетный захват до `max_tasks` задач сразу по нескольким рантаймам одного демона; рантаймы, не прошедшие авторизацию/провайдер-policy, молча исключаются. |
| POST | `/api/daemon/claim` | workspace-member / daemon-token | Алиас предыдущей ручки (тот же обработчик, оставлен для старых сборок демона). |
| POST | `/api/daemon/runtimes/{runtimeId}/tasks/{taskId}/prepare-lease` | workspace-member / daemon-token | Продлевает окно «готовлюсь к старту» для только что захваченной задачи. |
| POST | `/api/daemon/runtimes/{runtimeId}/tasks/{taskId}/skill-bundles/resolve` | workspace-member / daemon-token | Резолвит содержимое навыков по (id, source, hash) для задачи, которая ещё готовится; опционально base64. |
| GET | `/api/daemon/runtimes/{runtimeId}/tasks/pending` | workspace-member / daemon-token | Список уже выданных/выполняющихся задач рантайма — для восстановления состояния демона после рестарта. |
| POST | `/api/daemon/runtimes/{runtimeId}/update/{updateId}/result` | workspace-member / daemon-token | Отчёт о результате self-update CLI, запрошенного сервером. Отчёт по уже завершённому/неизвестному апдейту принимается и игнорируется. |
| POST | `/api/daemon/runtimes/{runtimeId}/models/{requestId}/result` | workspace-member / daemon-token | Отчёт о результате пробы «какие модели доступны локально». |
| POST | `/api/daemon/runtimes/{runtimeId}/local-skills/{requestId}/result` | workspace-member / daemon-token | Отчёт о результате пробы локальных навыков/MCP-серверов рантайма. |
| POST | `/api/daemon/runtimes/{runtimeId}/local-skills/import/{requestId}/result` | workspace-member / daemon-token | Отчёт об импорте одного локального навыка в библиотеку воркспейса; при успехе публикует `skill:created`/`skill:updated`. |
| GET | `/api/daemon/tasks/{taskId}/status` | workspace-member / daemon-token | Текущий статус задачи одной строкой. |
| POST | `/api/daemon/tasks/{taskId}/start` | workspace-member / daemon-token | Переводит задачу в `running`. |
| POST | `/api/daemon/tasks/{taskId}/wait-local-directory` | workspace-member / daemon-token | Переводит задачу в `waiting_local_directory` (ждём локальную рабочую директорию), с опциональной причиной. |
| POST | `/api/daemon/tasks/{taskId}/progress` | workspace-member / daemon-token | Публикует `task:progress` в workspace задачи (best-effort, без ошибки, если workspace не резолвится). |
| POST | `/api/daemon/tasks/{taskId}/complete` | workspace-member / daemon-token | Завершает задачу успехом, публикует `task:completed`, отзывает `mat_...` токен задачи; на первом завершении задачи по issue может запустить сопутствующие эффекты выполнения issue. |
| POST | `/api/daemon/tasks/{taskId}/fail` | workspace-member / daemon-token | Завершает задачу неудачей, публикует `task:failed`, отзывает токен задачи. |
| POST | `/api/daemon/tasks/{taskId}/usage` | workspace-member / daemon-token | Записывает построчный расход токенов/стоимости (provider/model); отдельные некорректные строки пропускаются без ошибки всего запроса. |
| POST | `/api/daemon/tasks/{taskId}/messages` | workspace-member / daemon-token | Добавляет одно или несколько сообщений транскрипта (text/thinking/tool_use/tool_result/error); контент/вход/выход инструмента редактируются на известные секреты перед сохранением; каждое сохранённое сообщение публикуется как `task:message`. |
| GET | `/api/daemon/tasks/{taskId}/messages` | workspace-member / daemon-token | Список сообщений транскрипта, опционально `?since=<seq>`. |
| POST | `/api/daemon/tasks/{taskId}/cancel-ack` | workspace-member / daemon-token | Подтверждает демоном завершение отмены задачи — довершает отложенную отмену чат-сессии. |
| POST | `/api/daemon/workspaces/{workspaceId}/issues/gc-check` | workspace-member / daemon-token | Пакетная проверка существования/статуса issue по списку id — для локальной сборки мусора на стороне демона (лимит 500 id на запрос). |
| GET | `/api/daemon/issues/{issueId}/gc-check` | workspace-member / daemon-token | То же самое, но для одной issue. |
| GET | `/api/daemon/chat-sessions/{sessionId}/gc-check` | workspace-member / daemon-token | То же самое для чат-сессии. |
| GET | `/api/daemon/autopilot-runs/{runId}/gc-check` | workspace-member / daemon-token | То же самое для запуска автопилота. |
| GET | `/api/daemon/tasks/{taskId}/gc-check` | workspace-member / daemon-token | То же самое для задачи. |
| POST | `/api/daemon/runtimes/{runtimeId}/recover-orphans` | workspace-member / daemon-token | Находит задачи, зависшие в `dispatched`/`running` на переподключившемся рантайме, и переоткладывает их в очередь либо проваливает. |
| POST | `/api/daemon/tasks/{taskId}/session` | workspace-member / daemon-token | Прикрепляет к задаче id сессии рантайма и/или рабочую директорию, чтобы потом можно было продолжить (`resume`) тот же процесс. |

### 3.8 Attachments

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/api/attachments/{id}/download` | workspace-member или держатель тикета | Отдаёт файл: редирект на подписанный CloudFront/пресайн-URL либо прямой стрим (с поддержкой `Range`), в зависимости от `ATTACHMENT_DOWNLOAD_MODE` и от того, настроен ли CloudFront/пресайнер у хранилища. Авторизация — либо обычная сессия + членство в workspace вложения, либо одноразовый подписанный `?token=`, выданный на конкретный `attachment_id`+`user_id` (например, встроенный в markdown-ссылку вложения). |
| GET | `/uploads/{key}` | public (по знанию ключа) | Отдаёт файл напрямую с диска; маршрут существует только когда активен локальный (не S3) backend хранилища. |

### 3.9 Me / аккаунт

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/api/me` | any-authenticated | Профиль вызывающего. |
| PATCH | `/api/me` | any-authenticated | Обновляет имя/аватар/язык/описание/таймзону (частично, только переданные поля; язык и таймзона валидируются). |
| PATCH | `/api/me/onboarding` | any-authenticated | Патчит JSON-анкету онбординга; первое непустое заполнение и полное завершение анкеты (по версии схемы) шлют аналитические события. |
| POST | `/api/me/onboarding/complete` | any-authenticated | Помечает `onboarded_at`; при первом завершении шлёт аналитическое событие с `completion_path`. |
| POST | `/api/me/onboarding/cloud-waitlist` | any-authenticated | Записывает email+причину в waitlist облачного тарифа, шлёт аналитическое событие. |
| GET | `/api/me/export` | human | Стримит `.tar.gz` со всеми данными пользователя (GDPR-экспорт), пишет аудит-запись об экспорте. |
| POST | `/api/me/onboarding/runtime-bootstrap` | workspace-member | **Легаси**: создаёт (или переиспользует) встроенного helper-агента и стартовую issue на выбранном рантайме; поведение заморожено ради старых сборок десктоп-клиента — актуальные клиенты используют обычные ручки создания агента/issue. |
| POST | `/api/me/onboarding/no-runtime-bootstrap` | workspace-member | Тот же легаси-путь для случая «рантайма ещё нет» — создаёт issue-подсказку подключить рантайм. |
| POST | `/api/cli-token` | human | Выпускает новую сессию (JWT) для CLI, создавая отдельную строку сессии (видна в `/api/auth/sessions`). |
| POST | `/api/upload-file` | any-authenticated (workspace-member, если указан контекст воркспейса) | Загружает файл (multipart), опционально привязывая к issue/comment/chat-сессии/сообщению чата; определяет content-type по сигнатуре+расширению. |
| POST | `/api/feedback` | any-authenticated | Сохраняет произвольный текстовый фидбек с метаданными клиента; лимит 10/час на пользователя (доп. к общему API rate-limit). |
| POST | `/api/client-usage` | human | Апсертит суточную запись использования клиента (web/desktop), опционально с результатом локальной пробы рантаймов (только для desktop); строгая схема (`DisallowUnknownFields`). |
| GET | `/api/workspace-templates` | any-authenticated | Список включённых на сервере шаблонов воркспейса, локализованный под язык вызывающего. |

---

## 4. Живая проверка

Локальный сервер поднимался и опрашивался вживую (Postgres `goosar_spec`, миграции применены командой `go run ./cmd/migrate up`, сервер — `go run ./cmd/server`). Подтверждено соответствие кода и реального ответа для: `GET /health`, `GET /readyz`, `GET /api/config`, `GET /api/auth/methods`, `POST /auth/send-code` → `POST /auth/verify-code` (реальная выдача сессии и `LoginResult`), `GET /api/me`, `GET /api/workspace-templates`, `GET /api/auth/mfa`, `POST /api/cli-token`, и отказ `401` на `POST /api/daemon/register` с неверным токеном. Детали — в отчёте.
