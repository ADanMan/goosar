# Контракт HTTP API Goosar

## Введение

Этот документ и файл `docs/50-api-contract.yaml` (OpenAPI 3.1) вместе
образуют единый источник истины о поведении HTTP- и WebSocket-API Goosar —
для реализации сервера "с нуля" в этом clean-room-проекте. Реализатору,
работающему по этому контракту, не нужен и не должен быть доступен код
прежнего сервера: всё, что наблюдаемо снаружи (маршруты, тела запросов и
ответов, коды ошибок, побочные эффекты вроде realtime-событий и
уведомлений), описано здесь и в OpenAPI-файле как поведение, а не как
реализация.

**Как читать эти два файла вместе.** `docs/50-api-contract.yaml` — точная,
проверяемая машиной форма контракта: пути, методы, схемы тел, коды ответов,
`operationId` каждой операции. Этот файл (`50-api-contract.md`) —
сопроводительная проза: то, что плохо ложится в JSON Schema — порядок
резолва неоднозначных полей, инварианты, побочные эффекты (realtime-события,
аудит, уведомления), причины отказов, к которым приводит не только
формальная невалидность тела. Там, где текст здесь и схема в YAML
расходятся, схема в YAML главнее для формы данных (типы полей,
обязательность, коды ответов), а этот документ — источник истины для
поведения и порядка действий сервера, которые в OpenAPI выразить нельзя.
Названия операций (`operationId`) в тексте ниже совпадают с `operationId` в
`docs/50-api-contract.yaml` — по ним легко найти точную схему запроса/ответа.

Документ и YAML собраны из четырёх ранее отдельных фрагментов, покрывавших
разные группы маршрутов (платформа/аутентификация/демон;
рабочие пространства/интеграции/деплой; задачи и их подресурсы
(комментарии, метки, свойства, проекты, отряды, автопилоты, закрепления,
агенты); шаблоны агентов/конструктор агента/навыки/дашборд/runtime/чат/
инбокс/уведомления) — в один. Полный набор маршрутов сверен автоматическим
сравнением с фактическим набором маршрутов сервера: расхождений нет — каждый
маршрут сервера документирован ровно один раз и с тем же методом/путём, и в
контракте нет ни одного маршрута, которого не существует на сервере.
Формы ответов и коды ошибок дополнительно сверены прогоном контрактных
тестов (`e2e/contract`) против работающего сервера.

## Общие обозначения

- Идентификаторы схем и операций (`AgentTask`, `createIssue`, …) — те, что
  использованы в `docs/50-api-contract.yaml`; в таблицах маршрутов путь и
  метод достаточны, чтобы найти операцию в YAML по её `paths`.
- `человек`/`human`, `агент`/`agent`, `демон`/`daemon-клиент` — три разных
  типа вызывающего: человек — сессия или личный токен живого пользователя;
  агент — запрос, сделанный от имени исполняющейся задачи (`X-Task-ID` и
  связанное с ним зачёт-авторство); daemon-клиент — процесс, обслуживающий
  один или несколько раннеров (runtime) и говорящий по протоколу
  `/api/daemon/**` — см. «Аутентификация» ниже.
- Таблицы маршрутов используют колонку «Права» с значениями `owner`,
  `admin`, `member`, `deployment-admin`, `human`, `any-authenticated` — их
  точный смысл см. в «Общие правила» → «Роли и права доступа в таблицах
  маршрутов».

---

## Общие правила

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

Единый формат тела ошибки на всём API (см. `components.schemas.Error` в `docs/50-api-contract.yaml`):

```json
{ "error": "human readable message", "code": "stable_machine_code", "request_id": "..." }
```

`code` присутствует на большинстве осмысленных 4xx (например, `invalid_or_expired_code`, `rate_limited`, `daemon_too_old`, `account_deactivated`), но не гарантирован на всех — часть отказов (`writeError`) отдаёт только `error` (+`request_id`, если уже был выставлен `X-Request-ID`). 5xx обычно тоже используют этот формат, но носят общее сообщение без `code`.

Отдельно: часть ручек демона на «отчёте о результате» (`daemonReportUpdateResult`, `daemonReportModelListResult`, `daemonReportLocalSkillsResult`, `daemonReportLocalSkillImportResult`, `daemonReportTaskProgress`, `daemonReportTaskUsage`, `daemonReportTaskMessages`, `daemonAckTaskCancelled` и т.п.) всегда возвращают `200 {"status":"ok"}`, даже если внутренняя обработка одной записи не удалась — ошибка логируется на сервере, а не транслируется демону, чтобы не блокировать очередь.

### 1.3 Аутентификация

Три независимые цепочки проверки аутентификации в этом разделе:

1. **Cookie-сессия** (`goosar_auth`, HttpOnly, `SameSite=Strict`, `Secure` включается автоматически, если `FRONTEND_ORIGIN` — https). Любой не-GET/HEAD/OPTIONS запрос, аутентифицированный этой cookie, обязан прислать заголовок `X-CSRF-Token`, значение которого проверяется HMAC-подписью относительно значения `goosar_auth` (сам CSRF-токен лежит в отдельной non-HttpOnly cookie `goosar_csrf`, которую фронтенд читает и кладёт в заголовок). Без валидного `X-CSRF-Token` — `403`.
2. **Bearer-токен** (`Authorization: Bearer <token>`) — обычный пользовательский путь, применяется ко всей группе `/api/**`, кроме публичных ручек и `/api/daemon/**`. Разбирает префикс токена по правилам из §1.1. Во всех случаях, кроме PAT, дополнительно проверяется, что учётная запись не деактивирована и версия токена (`tv` claim) совпадает с текущей `token_version` пользователя (иначе `401 session_revoked`).
3. **Daemon-токен** — отдельная цепочка на весь `/api/daemon/**` (включая `/api/daemon/ws`). Принимает те же префиксы `gsln_`/`gsl_`, plus `mdt_...` (даемон-токен), plus JWT-сессию человека. Daemon-токен несёт `workspace_id`/`daemon_id` прямо в себе (не требует похода в БД при попадании в кэш); человеческие пути требуют, чтобы вызывающий был участником workspace (с кэшированием членства на 5 минут).

Дополнительно: проверка «только человек» отклоняет (`403`) запросы, у которых `X-Actor-Source` равен `task_token` или `cloud_pat` (т.е. запрос пришёл от `mat_...` задачи-агента либо от cloud-PAT сервисного вызова), а не от живого человека. Навешана на MFA-ручки, `cli-token`, `client-usage`, `export`, ревокацию сессий.

`X-Actor-Source` — внутренний заголовок, который сервер сам выставляет/чистит на входе (клиент не может его подделать: он удаляется в начале каждой auth-миддлвари перед разбором токена).

### 1.4 Резолв workspace

Там, где workspace не идёт в пути URL, порядок резолва такой (первое непустое значение побеждает):

1. Если `X-Actor-Source: task_token` — workspace жёстко берётся из `X-Workspace-ID`, выставленного самой авторизацией токена задачи; клиентские заголовки/параметры при этом игнорируются.
2. Уже резолвленный workspace в контексте запроса (если более ранняя миддлварь его установила).
3. Заголовок `X-Workspace-Slug` → поиск по слагу.
4. Query-параметр `workspace_slug` → поиск по слагу.
5. Заголовок `X-Workspace-ID`.
6. Query-параметр `workspace_id`.

В разделе «Платформа, аутентификация, демон» этот механизм фактически используется в `/api/upload-file` и `/api/client-usage` (workspace опционален: без него — персональная загрузка/учёт использования, с ним — требуется членство).

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
| Присоединение к workspace/инвайты (используется вне раздела «Платформа, аутентификация, демон», но лимитер общий) | `RATE_LIMIT_JOIN` | 20/час | по user (иначе IP) |
| Вебхук автопилота, на токен триггера | внутренний лимитер по токену триггера | 60/мин | по токену триггера |
| Вебхуки (Stripe, автопилот) по IP при плохом деливери | лимитер по IP при плохом деливери | 30/мин | по IP |
| Вебхуки, абсолютный потолок по IP | абсолютный лимитер по IP | 600/мин | по IP, независимо от исхода |
| Доверенные прокси для расчёта клиентского IP | `RATE_LIMIT_TRUSTED_PROXIES` (иначе список доверенных прокси по умолчанию) | — | — |

Превышение лимита — `429` с заголовком `Retry-After` (секунды) и телом `{"error":"too many requests","code":"rate_limited"}` (для http-миддлвари), либо специфичный код (`code_rate_limited`, `429` без кастомного кода на `/auth/send-code` повторной отправке и т.п.) на ручках со своей отдельной проверкой.

### 1.6 CORS

CORS настроен глобально: разрешённые источники — из `CORS_ALLOWED_ORIGINS` (список через запятую), иначе `FRONTEND_ORIGIN`, иначе дефолт `http://localhost:3000,5173,5174`. Разрешённые методы: `GET, POST, PUT, PATCH, DELETE, OPTIONS`. Разрешённые заголовки: `Accept, Authorization, Content-Type, X-Workspace-ID, X-Workspace-Slug, X-Request-ID, X-Agent-ID, X-Task-ID, X-CSRF-Token, X-Client-Platform, X-Client-Version, X-Client-OS, X-Client-Capabilities`. `Access-Control-Expose-Headers: X-Request-ID`. `AllowCredentials: true`, `MaxAge: 300`.

### 1.7 Прочие сквозные заголовки

- `X-Request-ID` — генерируется/пробрасывается сервером (значение клиента принимается, только если состоит из безопасного алфавита и не длиннее лимита; иначе генерируется заново).
- `X-Client-Platform`, `X-Client-Version`, `X-Client-OS` — метаданные клиента, читаются сервером и кладутся в контекст запроса; используются, в частности, для отсечения устаревших демонов (проверка минимальной версии демона) и в аналитике/`client-usage`.
- `Content-Security-Policy` выставляется отдельной миддлварью на все ответы.
- Ответ на rate-limit и на `writeLookupUnavailable` может содержать `Retry-After`.

### 1.8 Пагинация

В разделе «Платформа, аутентификация, демон» пагинации нет: все списочные ручки (`/api/auth/sessions`, `/api/daemon/tasks/{taskId}/messages` (кроме курсора `since` по `seq`), `/api/workspace-templates`, `/api/daemon/workspaces`, `/api/daemon/workspaces/{workspaceId}/runtime-profiles`, список задач в `/api/daemon/runtimes/{runtimeId}/tasks/pending`) возвращают полный список без `limit`/`offset`/`cursor` — объём естественно ограничен (сессии одного пользователя, задачи одного рантайма и т.п.). В других частях контракта используются как минимум три разных схемы пагинации (`limit/offset`, `limit/cursor`, `page/per_page`) — единого стандарта на всё API нет.

### 1.9 Переменные окружения, влияющие на внешнее поведение платформенных маршрутов

**Сеть/CORS/публичные URL:** `CORS_ALLOWED_ORIGINS`, `FRONTEND_ORIGIN`, `GOOSAR_APP_URL`, `GOOSAR_PUBLIC_URL`, `GOOSAR_TRUSTED_PROXIES`, `RATE_LIMIT_TRUSTED_PROXIES`, `PORT`/`BACKEND_PORT`.

**Регистрация и вход:** `ALLOW_SIGNUP`, `ALLOWED_EMAILS`, `ALLOWED_EMAIL_DOMAINS`, `DISABLE_WORKSPACE_CREATION`, `APP_ENV` (только `production` отключает dev-код), `GOOSAR_DEV_VERIFICATION_CODE` (фиксированный код входа вне production), `AUTH_TOKEN_TTL` (по умолчанию 30 дней), `COOKIE_DOMAIN`, `JWT_SECRET`/`JWT_SECRET_PREVIOUS`.

**MFA:** `GOOSAR_MCP_SECRET_KEY` (тот же ключ шифрует и MCP-конфиги агентов, и TOTP-секреты; без него — `503 mfa_unavailable`, а сохранённые MCP-конфиги пишутся в БД plaintext), `GOOSAR_MCP_SECRET_KEY_PREVIOUS` (ротация ключа), `GOOSAR_TOTP_ISSUER`.

**Корпоративный вход:** переменные OIDC/LDAP читаются пакетом `corpauth` (не в этом диапазоне файлов, но эффект виден в `/api/auth/methods`, `/api/auth/oidc/**`, `/api/auth/ldap/login`).

**Рейт-лимиты:** `RATE_LIMIT_AUTH`, `RATE_LIMIT_AUTH_VERIFY`, `RATE_LIMIT_AUTH_EMAIL`, `RATE_LIMIT_TOKEN`, `RATE_LIMIT_MFA_VERIFY`, `RATE_LIMIT_API`, `RATE_LIMIT_CONTACT_SALES`, `RATE_LIMIT_EXPORT`, `RATE_LIMIT_JOIN`.

**Вложения:** `ATTACHMENT_DOWNLOAD_MODE` (`auto`/`cloudfront`/`presign`/`proxy`), `ATTACHMENT_DOWNLOAD_URL_TTL` (по умолчанию 30 минут), `LOCAL_UPLOAD_DIR` (включает локальный backend хранилища и, вместе с ним, маршрут `/uploads/*`), `S3_BUCKET`/`S3_REGION`/`AWS_*` (S3-backend), `CLOUDFRONT_KEY_PAIR_ID`/`CLOUDFRONT_PRIVATE_KEY(_SECRET)`/`CLOUDFRONT_DOMAIN`.

**Демон/рантаймы:** `GOOSAR_MIN_DAEMON_VERSION` (минимальная версия CLI для получения задач; `none`/`off`/`0`/`false` отключает проверку), `GOOSAR_TRUSTED_PROXIES` (тот же, что для CORS/realtime), `REALTIME_METRICS_TOKEN` (доступ к `/health/realtime` не с loopback), `GOOSAR_PROVISIONING_STORE`/`GOOSAR_PROVISIONING_*` (не бьёт напрямую по этим маршрутам, но влияет на то, что видит демон при регистрации через смежные ручки).

**Интеграции, чьи вебхуки/колбэки лежат в разделе «Платформа, аутентификация, демон»:** `GITHUB_WEBHOOK_SECRET`, `GITHUB_APP_SLUG`, `GITHUB_APP_ID`/`GITHUB_APP_PRIVATE_KEY`, `GOOSAR_VCS_INTEGRATION_ENABLED`, `GOOSAR_VCS_SECRET_KEY`(`_PREVIOUS`), `COMPOSIO_API_KEY`, `COMPOSIO_STATE_SECRET` (иначе `JWT_SECRET`), `COMPOSIO_CALLBACK_BASE_URL` (иначе `GOOSAR_PUBLIC_URL`), `GOOSAR_SLACK_SECRET_KEY`(`_PREVIOUS`) (не даёт публичных HTTP-маршрутов напрямую, но включается в общей инициализации роутера).

**Аналитика/фича-флаги (влияют на `/api/config`):** `POSTHOG_API_KEY`, `POSTHOG_HOST`, `ANALYTICS_FRONTEND_ENABLED`, `ANALYTICS_DISABLED`, `GOOSAR_FEATURE_FLAGS_FILE`, `GOOSAR_DELIVERY_PROFILE`, `GOOSAR_DEPLOYMENT_JIRA_URL`/`_CONFLUENCE_URL`/`_EWS_URL`/`_MAIL_DOMAIN`/`_LLM_API_BASE`/`_LLM_MODEL`, `GOOSAR_OFFICIAL_CLOUD_HOST` (скрывает `server_version`/daemon setup URLs на официальном облаке).

### 1.10 Роли и права доступа в таблицах маршрутов

- **Права** — кто может вызвать: `owner`/`admin`/`member` — роль в пространстве;
  `deployment-admin` — отдельная роль уровня деплоя (не связана с ролью в
  конкретном пространстве); `human` — вызывающий не должен быть агентом-задачей
  (`X-Actor-Source: task_token`) и не облачным PAT-раннером (`cloud_pat`) —
  personal access token и обычная сессия под этот запрет не попадают;
  `any-authenticated` — достаточно быть аутентифицированным любым способом
  (сессия, PAT, task-токен, cloud PAT).

---

## Протоколы WebSocket

### 2.1 `/ws` — realtime-канал для клиентов (веб/десктоп)

**Handshake.** `GET /ws?workspace_id=<uuid>|workspace_slug=<slug>[&client_platform=&client_version=&client_os=]`. Один из `workspace_id`/`workspace_slug` обязателен (slug резолвится в id на сервере; неизвестный slug → `404` ещё до апгрейда). Проверка `Origin` (см. CORS выше плюс `X-Forwarded-Host` от доверенного прокси) — несовпадающий origin отклоняется до апгрейда.

Дальше — два пути аутентификации:

- **Если у запроса есть валидная cookie `goosar_auth`** — сервер сразу проверяет членство в workspace (с учётом `token_version` из claim `tv`) и, если всё ок, апгрейдит соединение без дополнительного обмена сообщениями.
- **Иначе** (нет cookie, либо клиент desktop/CLI без cookie-сессии) — апгрейд происходит сразу, но сервер даёт 10 секунд на первый входящий фрейм вида `{"type":"auth","payload":{"token":"<gsl_/gsln_ бессмысленны здесь — принимается PAT gsl_/сессионный JWT>"}}`. Если фрейм не пришёл, невалиден или токен не проходит проверку членства — сервер отправляет `{"type":"auth_error",...}` (или `{"error":"..."}`) и закрывает соединение; при успехе отправляет `{"type":"auth_ack"}`.

Токен в auth-фрейме принимает: `gsl_...` PAT (тем же способом, что и обычные PAT-запросы) или HS256 сессионный JWT (`sub`, опционально `tv` для проверки ревокации по `token_version`). Токен вида `gsln_`/`mdt_`/`mat_` тут не поддерживается — только PAT или сессия.

**После апгрейда** клиент автоматически подписан на две комнаты: `workspace:<workspace_id>` и (если аутентифицирован) `user:<user_id>`.

**Входящие фреймы клиент → сервер** (JSON, `{"type": "...", "payload": {...}}`, лимит размера фрейма 64 KiB):

| type | payload | назначение |
|---|---|---|
| `subscribe` | `{"scope": "workspace"\|"user"\|"task"\|"chat", "id": "<uuid>"}` | подписаться на дополнительную комнату. `workspace`/`user` разрешены только на собственные id (иначе `subscribe_error` с `error:"forbidden"`); `task`/`chat` проходят отдельную проверку доступа (пользователь должен иметь доступ к этой задаче/чату в рамках своего workspace) — отказ даёт `error:"forbidden"` или `"lookup_failed"`. Неизвестный scope → `error:"unknown_scope"`. Успех → `subscribe_ack`. |
| `unsubscribe` | `{"scope": "...", "id": "..."}` | отписаться; всегда отвечает `unsubscribe_ack`. |
| `ping` | — | сервер отвечает `{"type":"pong"}`. |

Любой другой/невалидный фрейм молча игнорируется (debug-лог на сервере). Сервер также сам шлёт protocol-уровневые WS ping-фреймы каждые ~54 с (обычный WebSocket ping/pong, не JSON) и ожидает pong в течение 60 с, иначе закрывает соединение.

**Исходящие события сервер → клиент** — все в форме `{"type": "<event>", "payload": {...}, "actor_id"?: "...", "actor_type"?: "..."}`. Полный список типов событий и их payload:

- **Issues**: `issue:created` `{issue}`, `issue:updated` `{issue, assignee_changed?, status_changed?, project_changed?}`, `issue:deleted` `{issue_id}`, `issue_labels:changed` `{issue_id, labels}`, `issue_metadata:changed` `{issue_id, metadata}`, `issue_properties:changed` `{issue_id, properties}`, `issue_reaction:added` `{reaction, issue_id}`, `issue_reaction:removed` `{issue_id, emoji, actor_type, actor_id}`.
- **Comments/reactions**: `comment:created`/`comment:updated` `{comment}`, `comment:deleted` `{comment_id, issue_id}`, `comment:resolved`/`comment:unresolved` `{comment}`, `reaction:added` `{reaction, issue_id}`, `reaction:removed` `{comment_id, issue_id, emoji, actor_type, actor_id}`.
- **Agents**: `agent:status`/`agent:created`/`agent:archived`/`agent:restored` — все `{agent}`.
- **Tasks**: `task:queued` `{task_id, agent_id, issue_id, chat_session_id?, status}`, `task:dispatch` `{task_id, agent_id, issue_id, runtime_id, chat_session_id?}`, `task:running`/`task:completed`/`task:failed`/`task:cancelled` — `{task_id, agent_id, issue_id, chat_session_id?, status}`, `task:waiting_local_directory` — то же плюс `wait_reason?`, `task:progress` — свободная форма (`task_id, summary, step?, total?`), `task:message` — `{task_id, issue_id, chat_session_id?, seq, type, tool?, content?, input?, output?, created_at?}`.
- **Inbox**: `inbox:new` `{item}`, `inbox:read` `{item_id, recipient_id}`, `inbox:archived` `{item_id, recipient_id}`, `inbox:unarchived` `{item_id, issue_id, recipient_id}`, `inbox:batch-read`/`inbox:batch-archived` `{recipient_id, count}`.
- **Workspace/members**: `workspace:updated` `{workspace}`, `workspace:deleted` `{workspace_id}`, `member:added` `{member, workspace_id, workspace_name?}`, `member:updated` `{member}`, `member:removed` `{member_id, user_id, workspace_id}`.
- **Daemon (эхо в клиентский канал)**: `daemon:heartbeat`, `daemon:register` — форма свободная (объект с деталями регистрации/дерегистрации рантаймов).
- **Skills**: `skill:created`/`skill:updated`/`skill:deleted` — форма свободная.
- **Subscribers**: `subscriber:added` `{issue_id, user_type, user_id, reason}`, `subscriber:removed` `{issue_id, user_type, user_id}`.
- **Activity**: `activity:created` `{issue_id, entry}`.
- **Chat**: `chat:message` `{chat_session_id, message_id, role, content, task_id?, created_at}`, `chat:done` `{chat_session_id, task_id, message_id?, content?, elapsed_ms?, created_at?, message_kind?}`, `chat:cancel_finalized` `{outcome: "stopped"|"restored", chat_session_id, task_id, initiator_user_id?, message_id?, content?, message_kind?, created_at?, elapsed_ms?}`, `chat:session_read` `{chat_session_id}`, `chat:session_deleted` `{chat_session_id}`, `chat:session_updated` — форма свободная.
- **Projects**: `project:created`/`project:updated` `{project}`, `project:deleted` `{project_id}`.
- **Squads/labels/properties/pins** (форма свободная, детали в другом разделе): `squad:created/updated/deleted`, `label:created/updated/deleted`, `property:created/updated` `{property}`, `pin:created/deleted/reordered`.
- **Invitations**: `invitation:created` `{invitation, workspace_name?}`, `invitation:accepted` `{invitation_id, member}`, `invitation:declined`/`invitation:revoked` `{invitation_id, invitee_email}`.
- **GitHub/VCS**: `github_installation:created`/`github_installation:deleted`, `pull_request:linked`/`pull_request:updated`/`pull_request:unlinked` — форма свободная (их публикуют вебхук-обработчики GitHub/VCS).
- **Служебное**: `connection:revoked` — сервер шлёт клиенту при принудительном разрыве (например, после revoke-all-sessions) `{"type":"connection:revoked"}` без payload, затем закрывает соединение.

Доставка внутри одного процесса идёт через центральный диспетчер realtime-сообщений (комнаты `workspace:<id>`, `user:<id>`, `task:<id>`, `chat:<id>`); в многопроцессном/Redis-режиме события реплицируются между инстансами через внутренний Redis-релей (не отдельный публичный протокол — детали релея не наблюдаемы клиентом). Дедупликация "уже видел это событие" — по `event_id` внутри окна последних 128 событий на клиента.

### 2.2 `/api/daemon/ws` — канал для демонов

**Handshake.** `GET /api/daemon/ws?runtime_id=<uuid>[&runtime_id=<uuid>...]|runtime_ids=<uuid>,<uuid>` под той же проверкой демон-токена, что и остальной `/api/daemon/**` (заголовок `Authorization`, см. §1.3). Нужен хотя бы один `runtime_id`/`runtime_ids`, либо аутентифицированный человек без привязки к рантайму (тогда соединение привязывается только к `user:<id>`). Каждый переданный `runtime_id` должен реально существовать и быть доступен вызывающему (та же проверка доступа, что и на HTTP-ручках) — иначе `404`; если запрос пришёл по daemon-токену, привязанному к конкретному `daemon_id`, рантайм должен принадлежать этому же `daemon_id`. `Origin` не проверяется для этого канала (сервисный клиент). Апгрейд без дополнительного обмена сообщениями — аутентификация целиком на уровне HTTP handshake, отдельного auth-фрейма, в отличие от `/ws`, здесь нет.

После подключения клиент регистрируется сразу в нескольких индексах: по каждому `runtime_id`, по каждому доступному `workspace_id` (выведенному из рантаймов), и по `user_id`, если он есть.

**Сообщения демон → сервер** (JSON `{"type": "...", "payload": {...}}`):

| type | payload | ответ/эффект |
|---|---|---|
| `daemon:heartbeat` | `{"runtime_id","supports_batch_import"?}` | сервер вызывает тот же обработчик, что и `POST /api/daemon/heartbeat`, и присылает `daemon:heartbeat_ack` с тем же телом, что REST-ответ (`status`, `pending_update`/`pending_model_list`/`pending_local_skills`/`pending_local_skill_import(s)`). Хартбит для `runtime_id`, не входящего в список, с которым соединение зарегистрировалось, отклоняется молча (только серверный warn-лог, ack не отправляется). |
| `daemon:rpc_request` | `{"request_id","method","body"?,"timeout_ms"?}` | сервер асинхронно выполняет RPC (не более 8 одновременных запросов на одно соединение — сверх лимита сразу `daemon:rpc_response` со `status:429`) и присылает `daemon:rpc_response` `{"request_id","status","body"?,"error"?}`. |

Любой другой `type` игнорируется без ответа.

**Методы `daemon:rpc_request`.** Канал сделан как обобщённый запрос-ответ по `method`, но на сегодня и на сервере, и в референсном демоне реализован ровно один метод — остальные вернут `daemon:rpc_response` со `status:404` и `error:"unknown rpc method \"<name>\""`:

| method | params (`body`) | результат (`body` при `status:200`) | что это |
|---|---|---|---|
| `tasks.claim` | `{"daemon_id","runtime_ids":[...],"max_tasks"}` — идентичен телу `POST /api/daemon/tasks/claim` | `{"tasks":[...]}` — идентичен телу того же REST-ответа (массив `AgentTask`) | Пакетный захват задач через уже открытое WS-соединение вместо отдельного HTTP-запроса. Демон сам решает, использовать WS или HTTP: сначала пробует RPC (если соединение объявило capability `rpc-v1`, см. `X-Client-Capabilities: rpc-v1` при апгрейде), при неопределённом исходе (обрыв соединения посреди запроса) на время выжидания переключается на HTTP fallback, а если сервер явно ответил «метод не поддерживается» — на дальнейшие вызовы даже не пробует RPC для этой сессии, сразу идёт через `POST /api/daemon/tasks/claim`. На сервере обработчик `tasks.claim` — тонкая обёртка: строит внутренний HTTP-запрос к тому же самому хендлеру, что обслуживает `POST /api/daemon/tasks/claim`, и возвращает его тело как есть (тот же формат ошибок, тот же `426` для устаревшего клиента, тот же контракт). |

Любой другой `method` — не реализован ни списком, ни поведением; реализатору достаточно завести диспетчер по `method` с единственной веткой `tasks.claim` и `default → 404`.

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

## Платформа, аутентификация, демон

Права (`x-roles`) и лимиты — как в `docs/50-api-contract.yaml`; здесь — краткое действие и наблюдаемые побочные эффекты (публикуемые realtime-события, что создаётся/меняется).

### 3.1 Health / метрики

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/health` | public | Ливнес: всегда `{"status":"ok"}`, без обращения к БД. |
| GET | `/readyz` | public | Ready-проверка: пинг БД + сверка применённых миграций со списком, вкомпилированным в бинарник. Результат кэшируется на 3 секунды. `503`, если БД недоступна или есть неприменённые миграции. |
| GET | `/healthz` | public | Алиас `/readyz` (тот же обработчик). |
| GET | `/health/realtime` | operator | Снимок счётчиков realtime-диспетчера для клиентских и демон-соединений (подключения, комнаты, дропы). Доступ — либо строго с loopback-адреса без заголовков форвардинга, либо (если задан `REALTIME_METRICS_TOKEN`) с `Authorization: Bearer <token>` откуда угодно. |

### 3.2 Realtime

| Метод | Путь | Права | Поведение и побочные эффекты |
|---|---|---|---|
| GET | `/ws` | any-authenticated (см. §2.1) | Апгрейд до WebSocket, полный протокол — §2.1. Побочный эффект — подписка клиента на комнаты `workspace`/`user`, видна другим наблюдателям только как изменение счётчиков `/health/realtime`. |

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
| GET | `/api/config` | public | Публичная конфигурация фронтенда/CLI (см. `AppConfig` в `docs/50-api-contract.yaml`): доступность регистрации, CDN, фича-флаги, аналитика, allow-list провайдеров и т.д. Не требует аутентификации. |
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

Схема `AgentTask` в этом контракте перечисляет **все** поля объекта задачи, включая вложенные `agent`/`connected_apps`/`repos`/`project_resources`/`mcp_policy`/skills и т.п., а не только «управляющие» — это сверено построчно с тем, что реально разбирает референсный daemon-клиент. Поля, которые в ответе присутствуют, но daemon-клиентом не разбираются вообще (`status`, `priority`, `dispatched_at`, `started_at`, `completed_at`, `result`, `error`, `failure_reason`, `attempt`, `max_attempts`, `parent_task_id`, `created_at`, `work_dir` на верхнем уровне, `relative_work_dir`, `delivered_comment_ids`, `kind`, `attribution`), помечены в описании фразой «демон это поле не читает» — для этих полей реализация сервера может расходиться в деталях подсчёта/формата без риска сломать текущего daemon-клиента, но они всё равно нужны другим потребителям того же ответа (веб/десктоп-клиент, аудит, будущие версии daemon-клиента).

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
| POST | `/api/client-usage` | human | Апсертит суточную запись использования клиента (web/desktop), опционально с результатом локальной пробы рантаймов (только для desktop); строгая схема, отклоняющая неизвестные поля. |
| GET | `/api/workspace-templates` | any-authenticated | Список включённых на сервере шаблонов воркспейса, локализованный под язык вызывающего. |

---

## Рабочие пространства, интеграции, деплой-администрирование

### 1. `/api/workspaces/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/workspaces` | any-authenticated | Список пространств вызывающего | — |
| POST `/api/workspaces` | any-authenticated (обычно human; deployment-admin может обойти отключённое создание пространств) | Создаёт пространство, вызывающий становится owner. Если создание пространств отключено настройкой деплоя и вызывающий не deployment-admin — 403 `workspace_creation_disabled` | При обходе отключения — запись в admin-аудит `workspace.create.deployment_admin_bypass` **до** валидации тела; аналитическое событие `workspace_created`; уведомление десктоп-раннеров о новом списке пространств |
| GET `/api/workspaces/{id}` | member | Карточка пространства | — |
| PUT/PATCH `/api/workspaces/{id}` | owner, admin | Частичный патч (оба метода — один обработчик) | realtime `workspace.updated`; если менялось `name` — уведомление раннеров всех участников |
| DELETE `/api/workspaces/{id}` | owner | Необратимое каскадное удаление (чат-сессии, закреплённые агенты чата и т.д.) в одной транзакции | realtime `workspace.deleted`; уведомление раннеров всех бывших участников |
| GET `/api/workspaces/{id}/capabilities` | member | Витрина возможностей/примеров задач шаблона роли; пусто, если шаблона нет | — |
| GET `/api/workspaces/{id}/members` | member | Участники с данными пользователя | — |
| POST `/api/workspaces/{id}/members` | owner, admin | Пригласить по email (роль admin/member, не owner) | письмо-приглашение (асинхронно, ошибка не влияет на ответ); realtime `invitation.created`; аналитика `team_invite_sent` |
| PATCH `/api/workspaces/{id}/members/{memberId}` | owner, admin | Изменить роль/`perimeter_access`. Владение ролью owner трогает только owner. Нельзя оставить пространство без owner | инвалидация кэша членства; realtime `member.updated` |
| DELETE `/api/workspaces/{id}/members/{memberId}` | owner, admin | Удалить участника (owner может удалить только owner, и только не последнего) | отзыв токенов/сессий удаляемого в пространстве, закрытие его realtime-подключений; аудит `workspace_member.removed`; realtime `member.removed`; уведомление раннеров удалённого |
| POST `/api/workspaces/{id}/leave` | member | Выйти самому (owner — если не последний) | то же, что при удалении участника |
| GET `/api/workspaces/{id}/invitations` | member | Список pending-приглашений | — |
| DELETE `/api/workspaces/{id}/invitations/{invitationId}` | owner, admin | Отозвать pending-приглашение | realtime `invitation.revoked` |
| GET `/api/workspaces/{id}/github/installations` | member | Список установок GitHub App; `installation_id` виден только owner/admin | — |
| GET `/api/workspaces/{id}/github/connect` | owner, admin | URL установки GitHub App (подписанный `state`); `configured:false`, если деплой не настроен | — |
| GET `.../github/installations/{installationId}/repositories` | owner, admin | Проксирует список репозиториев из GitHub API, постранично | — |
| DELETE `.../github/installations/{installationId}` | owner, admin | Отключить установку | realtime `github.installation.deleted` |
| GET `/api/workspaces/{id}/vcs/connections` | member | Список self-hosted VCS-подключений; `available:false`, если интеграция выключена | — |
| POST `/api/workspaces/{id}/vcs/connections` | owner, admin | Подключить self-hosted VCS: токен проверяется у провайдера, секреты запечатываются | realtime `vcs.connection.created` |
| POST `.../vcs/connections/{connectionId}/rotate-webhook` | owner, admin | Перевыпустить вебхук-секрет (старый сразу недействителен) | realtime `vcs.connection.created` |
| DELETE `.../vcs/connections/{connectionId}` | owner, admin | Удалить подключение | realtime `vcs.connection.deleted` |
| GET `/api/workspaces/{id}/runtime-profiles` | member | Список профилей рантайма | — |
| POST `/api/workspaces/{id}/runtime-profiles` | owner, admin | Создать профиль; `command_name` — один токен без пробелов, `display_name` уникален | уведомление раннеров; realtime `daemon.register` |
| GET `.../runtime-profiles/{profileId}` | member | Карточка профиля | — |
| PATCH/PUT `.../runtime-profiles/{profileId}` | owner, admin | Частичный патч (оба метода — один обработчик) | уведомление раннеров; realtime `daemon.register` |
| DELETE `.../runtime-profiles/{profileId}` | owner, admin | 409, если на профиле есть активные агенты, или активные отряды с архивным лидером на его рантаймах. При успехе — каскадная очистка отрядов/назначений/каналов архивных агентов профиля | уведомление раннеров; realtime `daemon.register` |
| POST `/api/workspaces/{id}/export` | owner, **human** | Запускает фоновую сборку полного архива данных пространства (асинхронно, таймаут ~2 часа по умолчанию); один активный экспорт на пространство | admin-аудит `workspace.export` пишется сразу при запуске |
| GET `.../export/{jobId}` | owner, **human** | Статус job'а | — |
| GET `.../export/{jobId}/download` | owner, **human** | Скачать `.tar.gz`; 409 если не завершён, 410 если архив уже стёрт по ретенции | — |
| GET `/api/workspaces/{id}/slack/installations` | member | Список Slack-установок; `configured:false`, если не настроено | — |
| DELETE `.../slack/installations/{installationId}` | owner, admin | Отозвать установку | realtime `slack.installation.revoked` |
| POST `/api/workspaces/{id}/slack/install/byo` | owner, admin | Подключить своё Slack-приложение к агенту (`agent_id` в query); токены проверяются у Slack; один Slack-team — одно место привязки (409 иначе) | realtime `slack.installation.created` |

#### Валидация (workspaces)

- `slug` при создании: нижний регистр, `^[a-z0-9]+(?:-[a-z0-9]+)*$`, не из списка
  зарезервированных слов; уникален (409 `workspace_slug_taken`).
- `issue_prefix`: если не передан при создании — первые до 3 букв имени в
  верхнем регистре; при обновлении приводится к верхнему регистру, пустая
  строка игнорируется (не может обнулить префикс).
- `repos` (в PATCH/PUT пространства): каждый элемент — `{url, description?}`,
  `url` обязателен и должен быть валидным http(s)/ssh git URL; дубликаты по
  `url` молча схлопываются.
- Приглашение: роль `owner` запрещена; email нормализуется (lower+trim); если
  уже есть pending-приглашение на этот email в это пространство — 409
  `invitation_already_pending`; если уже участник — 409 `already_member`.
- Смена роли участника на/с `owner`, а также любое изменение участника-owner,
  разрешено только вызывающему-owner; нельзя понизить последнего owner (400).
- `runtime-profiles`: `protocol_family` — из списка поддерживаемых типов
  агентского протокола; `command_name` — без пробелов/табов/переводов строки и
  без NUL; `fixed_args` — непустые строки без NUL; `display_name` уникален в
  пространстве (409 при конфликте).
- VCS: `instance_url` нормализуется и должен парситься как абсолютный http(s)
  URL; `access_token` не пуст и валидируется прямым вызовом к провайдеру
  (ошибка аутентификации на стороне VCS-провайдера → 400, сетевой сбой → 502); требует настроенного на
  сервере ключа шифрования секретов (иначе 503 при отсутствии
  `GOOSAR_VCS_SECRET_KEY`).
- GitHub `return_to` — только `github` или `repositories`.
- Slack BYO: `bot_token`/`app_token` проверяются вызовом к Slack API; конфликт
  привязки team различает три случая (другой агент в этом же пространстве,
  архивный агент, другое пространство) — все 409 с разным текстом.

---

### 2. `/api/slack/binding/redeem`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| POST `/api/slack/binding/redeem` | any-authenticated | Погашает одноразовый токен привязки Slack-аккаунта к вызывающему пользователю (токен выдаёт Slack-бот). 403, если не участник соответствующего пространства; 409, если Slack-аккаунт уже привязан к другому пользователю; 410, если токен недействителен/истёк | — |

---

### 3. `/api/integrations/composio/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| POST `/connect/init` | any-authenticated | Начать OAuth-подключение тулкита для вызывающего | — |
| GET `/toolkits` | any-authenticated | Каталог тулкитов Composio | — |
| GET `/connections` | any-authenticated | Подключения вызывающего | — |
| DELETE `/connections/{id}` | any-authenticated | Отключить подключение | — |

Все четыре — сквозной прокси к внешнему сервису Composio: 503, если интеграция
или её MCP-приложения не включены флагом `composioMCPAppsEnabled`; 502/400 —
как ответил сам Composio.

---

### 4. `/api/invitations/**` (личные приглашения вызывающего)

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/invitations` | any-authenticated | Мои pending-приглашения (по `user_id` и по email) | — |
| GET `/api/invitations/{id}` | any-authenticated | Карточка; 403 `invitation_not_yours`, если адресовано не вызывающему | — |
| POST `/api/invitations/{id}/accept` | any-authenticated | Принять: нужен статус pending и непросроченный срок (иначе 400/410) | создаёт членство; первое принятие помечает онбординг завершённым; realtime `member.added` + `invitation.accepted`; уведомление раннеров; аналитика `team_invite_accepted` (+ `onboarding_completed` при первом онбординге) |
| POST `/api/invitations/{id}/decline` | any-authenticated | Отклонить (нужен статус pending) | realtime `invitation.declined` |

---

### 5. `/api/tokens/**` (личные PAT)

Общий лимит группы — **20 запросов/час на пользователя-или-IP** (поверх
базового лимита 600/мин).

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/tokens` | human | Список PAT вызывающего (без значений) | — |
| POST `/api/tokens` | human | Создать PAT; `name` обязателен; `expires_in_days` > 0 или бессрочный | значение токена в открытом виде — только в этом ответе; аудит `pat.created` |
| POST `/api/tokens/current/renew` | human (аутентификация именно Bearer-PAT) | Продлить PAT, которым выполнен запрос, если до истечения ≤ 7 дней (ещё на 90 дней); иначе `renewed:false` без изменений; 400, если запрос выполнен не PAT | — |
| DELETE `/api/tokens/{id}` | human | Отозвать PAT (идемпотентно — 204 даже если уже не существует) | инвалидация PAT-кэша; аудит `pat.revoked` |

---

### 6. `/api/cloud-billing/**`

Все восемь ручек — прозрачный прокси к внешнему облачному биллинговому
сервису (`cloudruntime`): тело и код ответа ретранслируются как есть; 503, если
облачный рантайм не подключён; 502 — сбой на его стороне; 504 — таймаут.

| Метод и путь | Права | Поведение |
|---|---|---|
| GET `/balance` | human | Баланс вызывающего |
| GET `/transactions` | human | История транзакций, `page`/`page_size` |
| GET `/batches` | human | Пакеты начислений, `page`/`page_size` |
| GET `/topups` | human | История пополнений, `page`/`page_size` |
| GET `/price-tiers` | human | Тарифные пакеты пополнения |
| POST `/checkout-sessions` | human | Создать Stripe checkout-сессию (`tier_id`, опц. `customer_email`) |
| GET `/checkout-sessions/{sessionId}` | human | Статус сессии; `sessionId` валидируется по алфавиту (буквы/цифры/`_`) до похода к биллингу — иначе 400 |
| POST `/portal-sessions` | human | Ссылка на Stripe billing portal |

---

### 7. `/api/deployment/**`

Везде, где не указано иное — требуется роль **deployment-admin** (проверяется
запросом к таблице держателей роли, не связано с ролью в конкретном
пространстве) и **human**.

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/admins` | deployment-admin | Список держателей роли | — |
| POST `/admins` | deployment-admin | Заявляет **выдачу** роли по email (двухканальное подтверждение — см. ниже). Если цель уже admin — 200 без заявки | admin-аудит `deployment_admin.grant.requested` |
| DELETE `/admins/{userId}` | deployment-admin | Заявляет **отзыв** роли; 409, если это последний admin | admin-аудит `deployment_admin.revoke.requested` |
| GET `/admins/pending` | deployment-admin | Заявки, ждущие подтверждения оператором | — |
| GET `/audit` | deployment-admin | Объединённый журнал admin+auth событий, с курсорной постраничностью | — |
| POST `/users/{userId}/deactivate` | deployment-admin | Деактивирует аккаунт; 409 на попытку деактивировать себя | отзыв всех PAT/токенов раннера пользователя, принудительный offline его рантаймов, закрытие realtime-подключений; admin-аудит `user.deactivate` |
| POST `/users/{userId}/reactivate` | deployment-admin | Восстановить | admin-аудит `user.reactivate` |
| POST `/users/{userId}/revoke-sessions` | deployment-admin | Инкрементирует token_version пользователя и удаляет хранимые сессии (не трогает PAT/токены раннера) | аудит `sessions_revoked` |
| DELETE `/users/{userId}` | deployment-admin | Анонимизирует персональные данные, удаляет все членства; 409 на себя или на действующего deployment-admin (сначала отозвать роль) | то же, что деактивация, плюс необратимая анонимизация; admin-аудит `user.delete` |
| GET `/mcp-servers` | deployment-admin | Библиотека MCP-серверов деплоя (конфигурация никогда не отдаётся — только метаданные и `credential_schema`) | — |
| POST `/mcp-servers` | deployment-admin | Добавить сервер в библиотеку | admin-аудит `deployment_mcp_server.create` с хэшем содержимого (не самим содержимым) |
| PUT `/mcp-servers/{serverId}` | deployment-admin | Обновить (частично) | admin-аудит `deployment_mcp_server.update` |
| DELETE `/mcp-servers/{serverId}` | deployment-admin | Удалить; каскадно снимает включения у всех пространств, отвязку от агентов, стирает пользовательские креды | admin-аудит `deployment_mcp_server.delete` |
| GET `/policy` | deployment-admin | Документ политики деплоя (`{}` если не задан) | — |
| PUT `/policy` | deployment-admin | Заменить документ целиком (≤ 64 КБ, только `llm`/`mcp`/`session`, без секретоподобных полей) | admin-аудит `deployment_policy.set` с хэшами до/после; инвалидация кэша политик сессий |
| GET `/workspaces` | deployment-admin | Все пространства деплоя с числом участников | — |
| GET `/join-targets` | any-authenticated | Ролевые пространства, открытые для самостоятельного вступления | — |
| POST `/join-targets/{workspaceId}/join` | any-authenticated (**лимит 20/час**) | Вступить участником; 404 если не открыто; идемпотентно `already_member:true`, если уже участник | обычный (не admin-) аудит; realtime `member.added`; уведомление раннеров; при первом онбординге — аналитика |
| GET `/fleet` | deployment-admin | Живая сводка по машинам-раннерам (heartbeat/задачи/агенты), список обрезан до 200 записей | — |
| GET `/workspaces/{workspaceId}/members` | deployment-admin | Состав участников произвольного пространства | — |
| PATCH `/workspaces/{workspaceId}` | deployment-admin | Вкл/выкл `open_join`; только для пространств из шаблона роли (иначе 400) | admin-аудит `workspace.open_join.set` |
| GET/PUT `/workspaces/{workspaceId}/config` | owner/admin пространства **или** deployment-admin | Тот же слой конфигурации, что `/api/workspace-config` (см. раздел 9), но пространство берётся из пути | при входе через deployment-admin — доп. admin-аудит `config.read` (GET) / `workspace_config.set` (PUT) с хэшами |
| GET `/workspaces/{workspaceId}/config/overrides` | owner/admin **или** deployment-admin | Список персональных override'ов всех участников | при deployment-admin — admin-аудит `config.read` |
| GET/PUT/DELETE `.../overrides/{userId}` | owner/admin **или** deployment-admin | Персональный override конкретного участника | при deployment-admin — `config.read`/`user_config_override.set`/`user_config_override.delete` |

#### Двухканальное подтверждение состава deployment-admin

`POST /admins` и `DELETE /admins/{userId}` не меняют состав роли напрямую — они
только фиксируют заявку (`202 Accepted`, поле `confirm_hint` — готовая
инструкция) и пишут запрос в admin-аудит. Применяет заявку оператор отдельной
серверной командой вне HTTP API (список/подтверждение/отклонение). Подтверждение
повторно проверяет все инварианты (роль ещё нужна, нельзя остаться без единого
администратора) и пишет ещё одну запись аудита.

#### Валидация (deployment)

- MCP-сервер деплоя: `config` не может содержать `enabled`/`disabled` (это
  прерогатива пространства через отдельную ручку) и служебный маркер маски;
  если задана `credential_schema` — транспорт сервера обязан поддерживать
  переменные окружения (`http`/`sse` отклоняются 400, там нет env-канала).
  Требует `GOOSAR_MCP_SECRET_KEY` на сервере — иначе 503.
- Политика деплоя: только объект с ключами `llm`/`mcp`/`session` (лишние
  верхнеуровневые ключи — 400); любое поле с именем, похожим на секрет
  (`key`/`token`/`secret`/`password` в подстроке, на любой глубине) —
  отклоняется; запись `mcp["*"]` — только каноническая форма
  `{"enabled": false, "locked": true}`.
- `open_join` применим только к пространствам с непустым `template_key`.

---

### 8. `/api/assignee-frequency`, `/api/status`

Обе — под членством в пространстве (без явного `{id}` в пути, workspace
резолвится по заголовку/query, см. правило в начале документа).

| Метод и путь | Права | Поведение |
|---|---|---|
| GET `/api/assignee-frequency` | member | Частота назначений исполнителей вызывающим (сумма по изменениям в активностях и по назначениям при создании задачи), для сортировки подсказок |
| GET `/api/status` | member | Диагностическая сводка на лету: кто вызывающий (человек/агент), рантаймы, provisioning, MCP, периметр, LLM — ничего не сохраняет |

---

### 9. `/api/provisioning/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/manifest` | member | Разрешённый манифест пакетов для платформы (`platform` обязателен в query); если у пространства заданы pin'ы — манифест строится из них плюс транзитивные `requires`, иначе из полного каталога; пакеты, отозванные политикой, перечисляются в `revokedPackages`, не исключаясь молча | фиксирует отдачу как «доставленный пакет» (используется `/api/status`) |
| GET `/blob/{name}/{version}` | member | Скачать `.zstd`-блоб; 404, если пакет не входит в разрешённый манифест для этого пространства/платформы, даже если физически существует в хранилище | заголовок `X-Package-Sha256` для проверки целостности |
| GET `/catalog` | owner, admin + human | Полный каталог пакетов деплоя, без учёта pin'ов пространства | — |
| GET `/pins` | owner, admin + human | Список закреплений пространства | — |
| PUT `/pins` | owner, admin + human | Полная замена набора pin'ов (delete-then-insert в одной транзакции) | — |

#### Валидация (provisioning)

- `platform` — один из `*`, `darwin-arm64`, `darwin-x64`, `win-x64`,
  `linux-x64`, `linux-arm64`.
- `package_type` — `skill`/`mcp-server`/`runtime`; `package_name`/`version` —
  `^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`; на пару
  (`package_type`,`package_name`) допустима одна запись в `PUT /pins` (иначе
  400 duplicate).

---

### 10. `/api/workspace-config/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/workspace-config` | owner/admin пространства **или** deployment-admin + human | Слой LLM/MCP-умолчаний пространства. `has_llm_api_key` — булев признак, сам ключ не отдаётся; `mcp_defaults` — маскирован (значения `env` заменены на `true`/`false`) | при входе через deployment-admin — admin-аудит `config.read` |
| PUT `/api/workspace-config` | owner/admin **или** deployment-admin + human | Частичный патч слоя. `llm_api_key` записывается запечатанным и никогда не возвращается | при deployment-admin — admin-аудит `workspace_config.set` с хэшами до/после |
| GET/PUT/DELETE `/overrides/{userId}` | owner/admin **или** deployment-admin + human | Персональный override того же слоя для конкретного участника (`mcp_overrides` вместо `mcp_defaults`); 404, если `userId` не участник пространства | при deployment-admin — `config.read` / `user_config_override.set` / `user_config_override.delete` |

#### Валидация (workspace-config)

- `llm_base_url` — абсолютный http(s) URL, ≤ 2048 символов (пустая строка
  очищает поле).
- `llm_model` — ≤ 256 символов; `llm_api_key` — ≤ 4096 символов.
- `mcp_defaults`/`mcp_overrides` — JSON-объект `{имя_сервера: {enabled?,
  env?}}`; `env` — карта строка→строка, `null` в значении удаляет переменную;
  документ целиком ≤ 64 КБ; `null` на верхнем уровне поля полностью очищает
  слой MCP.
- Запись `llm_api_key` или непустого MCP-документа требует настроенного на
  сервере ключа шифрования (`GOOSAR_MCP_SECRET_KEY`) — иначе 503.
- Доступ: owner/admin пространства ИЛИ deployment-admin (не участник и не
  deployment-admin — 403).

---

### 11. `/api/workspace-mcp-servers/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/workspace-mcp-servers` | member + human | Объединяет собственные серверы пространства и включённые из библиотеки деплоя; для каждого — `provided_credentials`/`missing_credentials` по вызывающему | — |
| POST `/api/workspace-mcp-servers` | owner, admin + human, **не агент-актор** | Создать собственный сервер пространства | — |
| PUT `.../{serverId}` | owner, admin + human, не агент | Обновить (частично) | — |
| DELETE `.../{serverId}` | owner, admin + human, не агент | Удалить; каскадно снимает назначения у агентов и стирает пользовательские креды | — |
| PUT `.../{serverId}/credentials` | member + human, не агент | Задать **свои** значения credential-полей (самообслуживание, не только owner/admin); объединяет с уже сохранёнными, пустая строка удаляет ключ | значения запечатываются перед сохранением |
| DELETE `.../{serverId}/credentials` | member + human, не агент | Очистить свои значения | — |

#### Валидация (workspace-mcp-servers)

- `name` — буквы/цифры/дефис/подчёркивание, уникален в пространстве.
- `config` — тот же запрет на `enabled`/`disabled` и маркер маски, что у
  серверов деплоя; при непустой `credential_schema` транспорт должен
  поддерживать env (не `http`/`sse`).
- `credential_schema`: до 32 полей, `key` — 1–128 символов
  (`[A-Za-z0-9_]`, не начинается с цифры), уникален в схеме;
  `label`/`hint` — до 500 символов.
- `credentials` (`values`): ключи должны входить в объявленную схему сервера
  (иначе 400); значение — до 8192 символов; если у сервера вообще нет
  `credential_schema` — 400.
- Требует `GOOSAR_MCP_SECRET_KEY` — иначе 503 у операций записи.

---

### 12. `/api/deployment-mcp-servers/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/deployment-mcp-servers` | member + human | Серверы из библиотеки деплоя, доступные пространству, с флагом `enabled` | — |
| PUT `.../{serverId}/enabled` | owner, admin + human, не агент | Включить/выключить сервер библиотеки для пространства | при выключении — снимает назначения сервера у всех агентов пространства |

---

### 13. Одиночные ручки

| Метод и путь | Права | Поведение |
|---|---|---|
| GET `/api/deployment-policy` | owner/admin пространства **или** deployment-admin + human | Read-only документ политики деплоя, тот же, что у `GET /api/deployment/policy`, но через членский вход |
| GET `/api/effective-config` | member + human | Итоговая конфигурация LLM/MCP для пары пространство+пользователь после наложения слоёв политика→пространство→персональный override, с `origin` (откуда взято) и `locked` (можно ли переопределить ниже); секреты не отдаются |
| GET `/api/deployment/client-secrets` | any-authenticated, но только для инсталляций, запущенных самим приложением | Единственный канал выдачи ключа LLM деплоя и адресов MCP-библиотеки десктоп-раннеру вне активной задачи; требует заголовок `X-Goosar-Launched-By: desktop` (иначе 403 `client_secrets_not_managed`); персональные MCP-креды сюда не попадают |
| GET `/api/llm/health` | member + human | Здоровье LLM-эндпоинта деплоя (пинг `/models`), кэш 60 секунд на процесс |

`GET /api/deployment/client-secrets` пишет admin-аудит
`deployment.client_secrets.issued` со списком выданных полей (без значений)
при каждой успешной выдаче.

---

## Задачи, комментарии, метки, свойства, проекты, отряды, автопилоты, закрепления, агенты

### Актор запроса: человек или агент

Многие маршруты этого раздела (задачи, комментарии, реакции, метки на задаче,
свойства задачи, подписки) одинаково доступны и человеку, и агенту,
действующему в рамках своего запуска. Тип актора (`member` либо `agent`) и
его ID определяются сервером из совокупности: сессии/токена вызывающего,
заголовка `X-Task-ID` (какой агентский запуск сейчас пишет) и заголовков
делегирования. Ответ с актором `agent` — это, как правило, сам агент,
записывающий результат своей работы (комментарий, смену статуса, реакцию), а
не человек, управляющий ботом вручную.

Отдельно от актора запроса есть понятие **полномочия на вызов агента**
(invoke authority): даже аутентифицированный агент не может просто назначить
задачу любому другому агенту или отряду за пределами области, в которой сам
работает, если у него нет на это специально выданных прав — см. «Правила
назначения» ниже.

### 1. Задачи (Issues)

#### 1.1 Идентификация и нумерация — инвариант

Каждая задача получает `id` (UUID) и `number` (целое число). Число
монотонно возрастает **в пределах воркспейса** и никогда не переиспользуется
(нет повторного использования номеров удалённых задач). Человекочитаемый
`identifier` строится как `<ISSUE_PREFIX>-<number>`, где `ISSUE_PREFIX` —
настраиваемый на уровне воркспейса префикс (например `ENG`). Строка
идентификатора распознаётся обратно в номер в поисковых и фильтрующих
запросах (`ENG-42` эквивалентно фильтру по `number = 42`).

#### 1.2 Перечисления

| Поле | Допустимые значения |
|---|---|
| `status` | `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled` |
| `priority` | `urgent`, `high`, `medium`, `low`, `none` |
| `assignee_type` / `creator_type` | `member`, `agent` (`creator_type` не может быть `squad` — отряд не может быть автором задачи) ; `assignee_type` дополнительно допускает `squad` |
| `stage` | целое число ≥ 1 или отсутствует; произвольный «под-этап» внутри статуса, не валидируется относительно набора статусов |

Терминальные для целей подсчёта прогресса дочерних задач статусы — `done` и
`cancelled`. Канбан-порядок статусов, используемый при сортировке `sort=status`,
не совпадает с порядком объявления: `backlog → todo → in_progress → in_review
→ done → blocked → cancelled`.

#### 1.3 Маршруты

| Метод | Путь | Права | Что делает |
|---|---|---|---|
| POST | `/api/issues/table/groups` | участник или агент | Группы задач для табличного вида |
| POST | `/api/issues/table/rows` | участник или агент | Постраничные строки внутри группы/ветки иерархии |
| POST | `/api/issues/table/facets` | участник или агент | Счётчики значений для панели фильтров |
| GET | `/api/issues/search` | участник или агент | Полнотекстовый поиск с ранжированием |
| GET | `/api/issues/child-progress` | участник или агент | Прогресс дочерних задач по всем родителям воркспейса |
| GET | `/api/issues/children` | участник или агент | Дочерние задачи для набора родителей |
| GET | `/api/issues/grouped` | участник или агент | Упрощённая группировка по исполнителю |
| GET | `/api/issues` | участник или агент | Основной листинг с фильтрами |
| POST | `/api/issues/query` | участник или агент | То же самое, но параметры в JSON-теле |
| POST | `/api/issues` | участник или агент | Создать задачу |
| POST | `/api/issues/quick-create` | участник или агент | Создать задачу из текстового промпта фоновой задачей |
| POST | `/api/issues/preview-trigger` | участник или агент | Проверить, что запустило бы правило автозапуска, без побочных эффектов |
| POST | `/api/issues/batch-update` | участник или агент | Массово применить один патч к нескольким задачам |
| POST | `/api/issues/batch-delete` | участник или агент | Массово удалить задачи |
| GET | `/api/issues/{id}` | участник или агент | Получить задачу |
| PUT | `/api/issues/{id}` | участник или агент | Изменить задачу |
| POST | `/api/issues/{id}/move` | участник или агент | Переставить в списке + опционально изменить поля |
| DELETE | `/api/issues/{id}` | участник или агент | Удалить задачу |
| POST | `/api/issues/{id}/comments/trigger-preview` | участник или агент | Прогноз запуска агентов комментарием без публикации |
| POST | `/api/issues/{id}/comments` | участник или агент | Добавить комментарий |
| GET | `/api/issues/{id}/comments` | участник или агент | Список комментариев |
| GET | `/api/issues/{id}/timeline` | участник или агент | Лента активности |
| GET | `/api/issues/{id}/subscribers` | участник или агент | Список подписчиков |
| POST | `/api/issues/{id}/subscribe` | участник или агент | Подписаться (или подписать другого — только owner/admin) |
| POST | `/api/issues/{id}/unsubscribe` | участник или агент | Отписаться (аналогично) |
| GET | `/api/issues/{id}/active-task` | участник или агент | Активные запуски агентов на задаче |
| POST | `/api/issues/{id}/tasks/{taskId}/cancel` | участник или агент | Отменить конкретный запуск |
| POST | `/api/issues/{id}/rerun` | участник или агент | Перезапустить агента |
| GET | `/api/issues/{id}/task-runs` | участник или агент | Вся история запусков |
| GET | `/api/issues/{id}/usage` | участник или агент | Сводка токенов/стоимости |
| POST | `/api/issues/{id}/reactions` | участник или агент | Поставить реакцию |
| DELETE | `/api/issues/{id}/reactions` | участник или агент | Снять реакцию |
| GET | `/api/issues/{id}/attachments` | участник или агент | Вложения задачи |
| GET | `/api/issues/{id}/children` | участник или агент | Прямые дочерние задачи |
| GET | `/api/issues/{id}/labels` | участник или агент | Метки задачи |
| POST | `/api/issues/{id}/labels` | участник или агент | Прикрепить метку |
| DELETE | `/api/issues/{id}/labels/{labelId}` | участник или агент | Открепить метку |
| GET | `/api/issues/{id}/metadata` | участник или агент | Метаданные задачи |
| PUT | `/api/issues/{id}/metadata/{key}` | участник или агент | Установить ключ метаданных |
| DELETE | `/api/issues/{id}/metadata/{key}` | участник или агент | Удалить ключ метаданных |
| PUT | `/api/issues/{id}/properties/{propertyId}` | участник или агент | Установить значение кастомного свойства |
| DELETE | `/api/issues/{id}/properties/{propertyId}` | участник или агент | Снять значение свойства |
| GET | `/api/issues/{id}/pull-requests` | участник или агент | Связанные pull request'ы |
| POST | `/api/issues/{id}/squad-evaluated` | **только** агент-лидер отряда, назначенного на задачу, в рамках своего же запуска | Записать решение лидера (взял/не взял/ошибка) |
| GET | `/api/tasks/{taskId}/messages` | участник или агент | Стенограмма одного запуска |

#### 1.4 Фильтры списка задач (`GET /api/issues`, а также `queryIssues`/`listGroupedIssues`/`IssueTable*` с поправками на формат)

Фильтры комбинируются через **И**; значения внутри одного списочного
параметра — через **ИЛИ**.

| Параметр | Формат | Смысл |
|---|---|---|
| `statuses` / `status` (устар.) | список через запятую | `status IN (...)` |
| `priorities` / `priority` (устар.) | список через запятую | `priority IN (...)` |
| `assignee_id` | UUID | точный исполнитель |
| `assignee_ids` | список через запятую | один из исполнителей |
| `assignee_types` | подмножество `member,agent,squad` | тип исполнителя |
| `assignee_filters` | список `type:id` через запятую | пары тип+ID исполнителя (например `agent:<uuid>,squad:<uuid>`) |
| `include_no_assignee` | `true` | включить незакреплённые задачи (комбинируется с `assignee_filters` через ИЛИ) |
| `creator_id` / `creator_filters` | аналогично assignee | фильтр по автору |
| `project_id` / `project_ids` / `include_no_project` | — | аналогично исполнителю, но для проекта |
| `label_ids` | список через запятую | задача имеет хотя бы одну из меток |
| `ids` | список через запятую | точный набор ID (пустой список после парсинга — ни одной задачи) |
| `top_level_only` | `true` | только задачи без родителя |
| `q` | строка | простое посимвольное совпадение по заголовку (все слова через И) плюс точное совпадение по номеру; ранжирования нет (в отличие от `/search`) |
| `scheduled` | `true` | есть `start_date` или `due_date` |
| `metadata` | JSON-объект `{key: примитив}` | containment по `metadata` (jsonb `@>`) |
| `properties` | JSON `{definition_id: [значения]}` | между определениями — И, внутри значений одного определения — ИЛИ; значение проверяется и как скаляр, и как элемент multi_select-массива, и как булево при `"true"`/`"false"` |
| `date_field` + `date_start` + `date_end` | `created_at`/`updated_at` + RFC3339 + RFC3339 | все три обязательны вместе; `date_start < date_end` |
| `involves_user_id` | UUID | задачи, назначенные на агента, которым владеет пользователь, либо на отряд, где он владеет агентом-лидером или агентом-участником |
| `open_only` | `true` | отдельный режим без пагинации: все не-архивные/не-закрытые задачи сразу (лимит/оффсет игнорируются) |

#### 1.5 Сортировка

`sort` — одно из: `position` (по умолчанию), `title`, `created_at`,
`updated_at`, `start_date`/`due_date` (NULLS LAST), `status` (по порядку
канбан-колонок, не по алфавиту), `priority` (`urgent` → `high` → `medium` →
`low` → `none`), либо `property:<UUID определения>` — сортировка по значению
кастомного свойства (тип приведения зависит от типа свойства: число, дата,
текст/URL/select — по строке). `direction` (`asc`/`desc`) применяется ко всем
сортировкам, кроме `position`, где всегда `ASC`. При равенстве значений
сортировки дополнительно упорядочивает `created_at DESC, id DESC` — то есть
результат детерминирован.

#### 1.6 Пагинация

Без `open_only`: `limit` (по умолчанию 100, максимум 100) + `offset`
(по умолчанию 0), классическая offset-пагинация, ответ содержит `total`
(точный COUNT по тем же фильтрам). Табличные эндпоинты (`IssueTable*`)
используют вместо этого непрозрачный курсор, привязанный к
`query_fingerprint` — курсор годится только для того же набора
фильтров/группировки/сортировки, для другого набора нужно начать заново.

#### 1.7 Дочерние задачи

Задача может иметь `parent_issue_id`. Изменение родителя (через `updateIssue`
или `batchUpdateIssues`) проверяется на цикличность подъёмом по цепочке
родителей вверх до 10 уровней; задача не может быть собственным родителем.
`getChildIssueProgress` даёт по каждому родителю пару (всего дочерних,
завершено — в статусе `done` или `cancelled`); когда дочерняя задача
переходит в терминальный статус, у родителя может быть создана системная
запись/уведомление о прогрессе (публикуется как часть более широкой
подсистемы уведомлений, не описанной в этом разделе контракта).

#### 1.8 Назначение на человека, агента или отряд

`assignee_type`/`assignee_id` должны передаваться вместе или не передаваться
вовсе. Валидация назначения (при создании, обновлении, `move`, и в частичном
виде — при `batch-update`):

- **`member`** — должен быть участником текущего воркспейса.
- **`agent`** — должен существовать в воркспейсе, не быть архивированным, и
  вызывающий должен иметь право его вызвать (см. ниже «Полномочие на вызов
  агента»).
- **`squad`** — должен существовать, не быть архивированным, его лидер
  (агент) не должен быть архивирован, и вызывающий должен иметь право
  вызвать лидера.

**Полномочие на вызов агента.** Владелец/админ воркспейса и владелец самого
агента могут назначать его всегда. Обычный участник — только если у агента
`permission_mode = public_to` и вызывающий входит в один из его
`invocation_targets` (`workspace` — весь воркспейс, `member` — конкретный
человек, `team` — конкретная команда), либо если он владелец агента.
Агент-актор (не человек), пытающийся назначить задачу на **другого** агента
или отряд, дополнительно обязан быть «в рамках своей области» — то есть
делать это в контексте задачи, на которой он сам сейчас работает (через
заголовок `X-Task-ID`), либо иметь явно выданное «делегированное» полномочие;
иначе — 403 `"agent actor is not authorized to assign an issue outside its
current scope"`.

#### 1.9 Что происходит при назначении задачи агенту или отряду (правило автозапуска)

Это относится к `createIssue`, `updateIssue`, `moveIssue`, `batchUpdateIssues`
и симулируется без побочных эффектов в `previewIssueTrigger`.

Постановка исполнителя в очередь на реальный запуск (публикация `task:queued`
через подсистему задач) происходит, если выполнено **одно из двух**:

1. **Назначение изменилось** (задача только что создана с исполнителем, либо
   `assignee_type`/`assignee_id` реально поменялись на этом запросе) **и**
   итоговый статус задачи **не** `backlog`; или
2. **Статус изменился** с `backlog` на любой другой, кроме `done`/`cancelled`,
   у задачи, у которой **уже был** исполнитель-агент/отряд — но не тогда,
   когда сам запрос пришёл от агента, который уже выполняет ровно эту задачу
   (защита от самозапуска / петли).

При этом:

- Если исполнитель — **агент**: он должен быть привязан к runtime, runtime не
  архивирован, агент не архивирован, и вызывающий должен иметь право его
  вызывать (то же полномочие, что при назначении). Для случая (2) —
  дополнительно нет уже ожидающего запуска этого агента на этой же задаче
  (иначе новый запуск не создаётся повторно).
- Если исполнитель — **отряд**: используется его **лидер** (агент); лидер
  должен быть готов (runtime online, не архивирован) и вызывающий должен
  иметь право его вызвать. Постановка в очередь для лидера отряда идёт через
  отдельный путь (`enqueueSquadLeaderTask`), который дополнительно проверяет,
  что у лидера ещё нет ожидающего запуска на этой задаче.
- `handoff_note` (в `updateIssue`/`moveIssue`) передаётся как дополнительный
  контекст запускаемому агенту.
- `suppress_run: true` полностью отключает автозапуск для этого конкретного
  запроса, даже если правило сработало бы.

Сама постановка в очередь — операция подсистемы задач (`task:queued`,
затем `task:dispatch`, `task:running`, ... — по мере исполнения); эти
события подробно не входят в эту часть контракта, здесь фиксируется только
факт и условие их возникновения.

#### 1.10 Комментарии задачи

Комментарий имеет `type` (`comment` по умолчанию; `status_change`,
`progress_update`, `system` — служебные значения, проставляются сервером для
автоматических записей) и может быть ответом (`parent_id`) в треде.

**Правила запуска агентов из комментария.** Публикация или редактирование
комментария (кроме комментариев, начинающихся с `/note` — те никогда не
запускают агентов) вычисляет набор целей и пытается поставить им задачу в
очередь. Источник срабатывания (`source` в превью) — один из:

- `issue_assignee` — на задаче есть исполнитель-агент/лидер отряда;
- `mention_agent` — агент явно упомянут (`@agent`) в тексте;
- `mention_squad_leader` — упомянут отряд → цель — его лидер;
- `thread_parent` — это ответ на комментарий, автор которого агент (ответ
  автору сообщения в его же треде запускает его);
- `conversation_continuation` — продолжение недавнего диалога с уже активным
  агентом на этой задаче.

Для каждой цели результат (`status`/`reason_code`) один из:
`queued` (поставлено), `deferred` (отложено — например, у агента уже есть
активный запуск на этой задаче), `coalesced` (объединено с уже существующим
ожидающим запуском того же агента; для отряда это может означать, что запуск
на самом деле объединился с запуском другого агента исполняющего роль
лидера — тогда статус тоже `coalesced`, а не `queued`), `blocked`
(отклонено — недостаточно прав на вызов, самозапуск подавлен, атрибуция не
прошла, либо внутренняя ошибка; конкретная причина — в `reason_code`).

`suppress_agent_ids` в теле запроса `createIssueComment`/`updateComment`
исключает перечисленных агентов из авто-запуска именно этим комментарием
(не отменяет уже действующие правила упоминания для будущих комментариев).

Комментарий, созданный **агентом** в ответ на его собственный
триггер-комментарий (задача была поставлена в очередь конкретным
комментарием, у задачи агента записан `trigger_comment_id`), обязан быть
ответом (`parent_id`) на этот же комментарий или на один из
«коалесцированных» с ним — иначе 409 (агент не может «тихо» создать новый
топ-уровневый комментарий из контекста чужого триггера).

**Редактирование и удаление.** Если при `PUT` меняется реально текст
комментария, сервер отменяет ещё не выполненные задачи агентов, которые были
запущены именно этим комментарием как триггером, и заново прогоняет новый
текст через ту же логику определения целей — то есть редактирование
комментария может отменить один запуск агента и запустить другой (или тот же
с обновлённым контекстом, если он уже был активен). То же самое (без
повторного запуска — только отмена и, если что-то выжило, повторный прогон
по следующему уцелевшему сообщению треда) происходит при удалении
комментария.

**Резолюция треда.** `resolveComment` помечает решённым **весь тред**, корень
которого — данный комментарий (или сам комментарий, если он и есть корень).
В треде может быть решён только один комментарий одновременно — если уже был
решён другой, он автоматически «расрешается» (публикуя `comment:unresolved`
для него) в той же транзакции.

**Листинг комментариев** (`GET /api/issues/{id}/comments`) поддерживает пять
режимов постраничной/выборочной загрузки — полный список, по `since`, по
конкретному треду (`thread` + опционально `tail` с курсором), по N последним
тредам (`recent` с курсором), только корни (`roots_only`), а также плоское
«свёртывание» решённых тредов (`fold`); правила несовместимости режимов см.
в `docs/50-api-contract.yaml` (описание `listIssueComments`). Курсор для продолжения выдаётся не в
теле, а в заголовках `X-Goosar-Next-Before` / `X-Goosar-Next-Before-Id`.

#### 1.11 Реакции

Реакции на задачу (`issue_reaction:*`) и реакции на комментарий
(`reaction:*`) — независимые сущности с разными таблицами и разными
событиями, хотя формой идентичны (`{actor_type, actor_id, emoji}`). Ключ
уникальности — комбинация (объект, автор, эмодзи): один и тот же человек не
может поставить одну и ту же реакцию дважды, но может — разные эмодзи.

#### 1.12 Вложения

Вложения могут быть прикреплены к задаче напрямую (`GET /api/issues/{id}/attachments`) и
отдельно — к конкретному комментарию (видны внутри `IssueComment.attachments`).
Загрузка файла (получение `attachment_id` для последующего
`attachment_ids`) выполняется отдельным маршрутом загрузки файлов, не
входящим в эту часть контракта; сюда входят только чтение метаданных,
инлайн-превью текстовых файлов (`getAttachmentContent`, с лимитом размера,
зависящим от типа) и удаление (которое также удаляет сам файл из объектного
хранилища).

#### 1.13 Подписчики

Причина подписки (`reason`) — одно из `creator`, `assignee`, `commenter`,
`mentioned`, `manual`, `autopilot`; вручную через API можно только добавить с
причиной `manual` или снять подписку любой причины. Подписать/отписать
**другого** пользователя может только владелец/админ воркспейса; сам себя
может подписать/отписать кто угодно из участников или агентов.

#### 1.14 История/активность (timeline)

`GET /api/issues/{id}/timeline` отдаёт объединённую хронологическую ленту:
комментарии, изменения полей (созданные автоматически как записи активности)
и системные события (например `squad_leader_evaluated` от
`recordSquadLeaderEvaluation`). Формат записи — единая обёртка `{type, id,
actor_type, actor_id, action, details, created_at}`; конкретный состав
`details` зависит от `action` и не типизирован жёстко.

#### 1.15 Позиция и ручной порядок (ordering)

`position` — число с плавающей точкой; сортировка по `position` всегда
`ASC`. Прямая установка `position` через `PUT` возможна, но основной способ
переставить задачу в списке — `POST /api/issues/{id}/move`, который вместо
абсолютного числа принимает соседей (`before_id`/`after_id`) и сам вычисляет
среднюю точку. Если промежуток между соседями схлопнулся (соседи были
переставлены кем-то ещё, либо математически неразличимы как `float64`) —
409, клиент обязан перечитать список и повторить операцию, полагаясь на
свежие позиции соседей (это единственный явно документированный протокол
конкурентной сортировки в этом разделе контракта).

#### 1.16 Метаданные и кастомные свойства — отличие

**Metadata** — свободный плоский словарь `{строка: примитив}` без
предварительного определения ключей, ограничен 50 ключами и 8 КБ суммарно;
ключ должен соответствовать `^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`. Значение — не
может быть `null` (для удаления ключа — отдельный `DELETE`).

**Properties** (кастомные свойства) — типизированные поля, которые сначала
определяются на уровне воркспейса (`/api/properties`, до 20 активных
одновременно, только owner/admin) с типом из `{text, number, select,
multi_select, date, checkbox, url}`, а затем их значения по одному
проставляются на конкретных задачах (`/api/issues/{id}/properties/{propertyId}`,
доступно всем — участнику и агенту). Итоговый блок `properties` задачи
ограничен 16 КБ. Архивированному определению свойства нельзя присвоить новое
значение (но уже проставленные значения остаются видны, пока определение не
удалено полностью — удаления определений в API нет, только архивация).

#### 1.17 Realtime-события: мутирующие маршруты задач

| Маршрут | Событие(я) |
|---|---|
| `createIssue` | `issue:created`; `task:queued` (если правило автозапуска сработало) |
| `updateIssue` | `issue:updated`; `task:queued` (если сработало) |
| `moveIssue` | `issue:updated` (это PUT с вычисленным `position`) |
| `deleteIssue` | `issue:deleted` |
| `batchUpdateIssues` | `issue:updated` за каждую успешную; `task:queued` где применимо |
| `batchDeleteIssues` | `issue:deleted` за каждую успешную |
| `createIssueComment` | `comment:created`; запуск агентов — через подсистему задач |
| `updateComment` | `comment:updated` |
| `deleteComment` | `comment:deleted` |
| `resolveComment` | `comment:unresolved` (за каждую снятую резолюцию в этом треде) + `comment:resolved` |
| `unresolveComment` | `comment:unresolved` |
| `addCommentReaction` / `removeCommentReaction` | `reaction:added` / `reaction:removed` |
| `subscribeToIssue` / `unsubscribeFromIssue` | `subscriber:added` / `subscriber:removed` |
| `addIssueReaction` / `removeIssueReaction` | `issue_reaction:added` / `issue_reaction:removed` |
| `attachIssueLabel` / `detachIssueLabel` | `issue_labels:changed` |
| `setIssueMetadataKey` / `deleteIssueMetadataKey` | `issue_metadata:changed` |
| `setIssuePropertyValue` / `deleteIssuePropertyValue` | `issue_properties:changed` |
| `recordSquadLeaderEvaluation` | `activity:created` |
| остальные (`GET`-маршруты, `cancelIssueTask`, `rerunIssue`, табличные `POST`) | без собственных событий здесь; `task:*` публикуется подсистемой задач |

---

### 2. Комментарии как отдельный ресурс (`/api/comments/{commentId}`)

Эти маршруты действуют не через контекст задачи, а напрямую по ID
комментария (сам комментарий при этом всегда принадлежит какой-то задаче).

| Метод | Путь | Права | Побочные эффекты |
|---|---|---|---|
| PUT | `/api/comments/{commentId}` | автор комментария или owner/admin | см. §1.10 (отмена/повторный запуск триггеров) |
| DELETE | `/api/comments/{commentId}` | автор или owner/admin | удаляет вложения (и файлы), отменяет/переносит триггеры |
| POST | `/api/comments/{commentId}/resolve` | участник или агент | резолюция треда (единственная на тред) |
| DELETE | `/api/comments/{commentId}/resolve` | участник или агент | снимает резолюцию |
| POST | `/api/comments/{commentId}/reactions` | участник или агент | реакция на комментарий |
| DELETE | `/api/comments/{commentId}/reactions` | участник или агент | снять реакцию |

---

### 3. Метки (Labels)

Метка принадлежит воркспейсу и имеет `resource_type` — `issue` (по умолчанию,
всегда доступен), либо `agent`/`skill` (доступны только если в воркспейсе
включена функция «ресурсные метки»; иначе список/создание/операции с ними —
404). Имя метки уникально в пределах `(workspace, resource_type)`, до 32
символов без управляющих символов; цвет — 6-значный HEX.

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/labels` | участник/агент | список по `resource_type` |
| POST | `/api/labels` | участник/агент | создать |
| GET/PUT/DELETE | `/api/labels/{id}` | участник/агент | получить/изменить/удалить |

Удаление метки — каскадное: в одной транзакции снимает эту метку со всех
задач, агентов и навыков, где она была прикреплена, прежде чем удалить саму
запись.

Прикрепление/открепление метки к задаче (`/api/issues/{id}/labels...`,
только `resource_type=issue`) и к агенту (`/api/agents/{id}/labels...`,
только `resource_type=agent`, требует прав управления агентом) описаны в
соответствующих разделах.

---

### 4. Свойства задач (Properties)

См. §1.16 для отличия от metadata. Ограничения: до 20 неархивированных
определений на воркспейс одновременно (создание и разархивация защищены
advisory-локом БД, чтобы конкурентные запросы не пробили лимит), имя — до 32
символов и не из зарезервированного списка (`status, priority, assignee,
project, parent, stage, label, labels, start_date, due_date, title,
description, creator, created_at, updated_at, metadata, properties`), для
`select`/`multi_select` — до 50 вариантов с уникальными именами и ID.

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/properties` | участник/агент | список определений |
| POST | `/api/properties` | **только owner/admin, не агент** | создать определение |
| GET | `/api/properties/{id}` | участник/агент | получить определение |
| PATCH | `/api/properties/{id}` | **только owner/admin** | изменить (переименовать/варианты/архивировать) |

Удаление варианта из `config`, который ещё используется хотя бы в одной
задаче, запрещено (409 со списком занятых вариантов) — сначала нужно снять
значения на задачах.

---

### 5. Проекты (Projects) и их ресурсы

`status`: `planned, in_progress, paused, completed, cancelled`; `priority` —
как у задач. Проект может иметь `lead_type`/`lead_id` (человек или агент).

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/projects/search` | участник/агент | полнотекстовый поиск |
| GET | `/api/projects` | участник/агент | список с фильтрами по статусу/приоритету |
| POST | `/api/projects` | участник/агент | создать, опционально сразу с ресурсами |
| GET/PUT | `/api/projects/{id}` | участник/агент | получить/изменить |
| DELETE | `/api/projects/{id}` | **только owner/admin** | удалить (снимает контекст проекта у чат-сессий) |
| GET/POST | `/api/projects/{id}/resources` | участник/агент | список/привязать ресурс |
| PUT/DELETE | `/api/projects/{id}/resources/{resourceId}` | участник/агент | изменить/отвязать ресурс |

Ресурс проекта — `github_repo` (URL + опциональные `default_branch_hint`/`ref`)
или `local_directory` (абсолютный локальный путь + `daemon_id` + опциональная
метка). На один `daemon_id` в рамках одного проекта допускается не более
одного `local_directory` — попытка добавить второй возвращает 409/400.

---

### 6. Отряды (Squads)

Отряд — группа из одного агента-лидера и произвольного набора участников
(агентов и/или людей), которой можно назначать задачи и автопилоты как единой
единице; фактически исполняет задачу всегда именно **лидер**.

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/squads` | участник | список отрядов с превью участников |
| POST | `/api/squads` | участник (с правом вызова агента-лидера) | создать |
| GET | `/api/squads/{id}` | участник | получить |
| PUT | `/api/squads/{id}` | owner/admin **или создатель отряда** | изменить (включая смену лидера) |
| DELETE | `/api/squads/{id}` | owner/admin или создатель | архивировать |
| GET | `/api/squads/{id}/members` | участник | список участников |
| GET | `/api/squads/{id}/members/status` | участник | живой статус каждого участника |
| POST | `/api/squads/{id}/members` | owner/admin или создатель | добавить участника |
| DELETE | `/api/squads/{id}/members` | owner/admin или создатель | удалить участника (кроме текущего лидера) |
| PATCH | `/api/squads/{id}/members/role` | owner/admin или создатель | сменить роль участника |
| POST | `/api/issues/{id}/squad-evaluated` | только сам агент-лидер, в рамках своего запуска | записать решение лидера по задаче |

При архивации отряда все задачи и автопилоты, назначенные на отряд,
автоматически переносятся на его текущего лидера (чтобы не осиротить их).
Лидера нельзя удалить из участников напрямую — сначала нужно назначить
другого лидера через `PUT /api/squads/{id}`.

Статус участника-агента в `members/status` вычисляется так: `archived`, если
агент архивирован; иначе `working`, если есть активный запуск; иначе `idle`,
если runtime online; иначе `unstable`, если был на связи <5 минут назад;
иначе `offline`. Для участников-людей статус не вычисляется.

---

### 7. Автопилоты (Autopilots)

Автопилот — правило «по триггеру (расписание / вебхук / ручной вызов API) —
выполнить действие» с исполнителем — агентом или отрядом.

**Статусы:** `active`, `paused`, `archived`. **Режим исполнения**
(`execution_mode`): `create_issue` (на каждый триггер создаётся новая задача
и на неё запускается исполнитель) или `run_only` (исполнитель запускается без
создания задачи). **Типы триггеров** (`kind`): `schedule` (cron + таймзона),
`webhook` (провайдер `generic` или `github`, с уникальным токеном URL,
опциональной проверкой подписи и фильтрами событий), `api` (запуск через
внешний вызов API, вне этого раздела контракта).

#### 7.1 Права

Просмотр — любой участник воркспейса. Управление содержимым (изменение,
удаление, ручной запуск, работа с триггерами и доставками) — создатель
автопилота, владелец/админ воркспейса, либо участник, явно добавленный
**коллаборатором**. Управление самим списком коллабораторов (кто может
писать) — только создатель или владелец/админ, не сами коллабораторы.
Создание автопилота требует человека — агент не может
создать автопилот от своего имени.

#### 7.2 Версионирование правила

Каждое существенное изменение конфигурации автопилота (создание, изменение
исполнителя/режима/шаблона заголовка, удаление) фиксирует новую запись
`autopilot_rule_version` и переустанавливает `published_by` у всех его
триггеров — так последующие срабатывания атрибутируются актуальному автору
правил, а не тому, кто настраивал автопилот изначально.

#### 7.3 Прогоны (Runs) и доставки (Deliveries) — отличие

**Run** — факт срабатывания автопилота (создание задачи и/или запуск
исполнителя), со статусом `issue_created, running, completed, failed,
skipped` и источником `schedule, manual, webhook, api`.

**Delivery** — факт получения **входящего вебхука**, до и независимо от
решения, привело ли оно к прогону: статус `queued, dispatched, rejected,
ignored, failed`, отдельно — статус проверки подписи
(`not_required, valid, invalid, missing`). Доставку можно **реплеить**
(`replayAutopilotDelivery`) — заново прогнать её сохранённое тело через
диспетчер; нельзя реплеить доставку, провалившую проверку подписи, без
сохранённого тела, либо если сам триггер сейчас выключен, либо автопилот не
активен.

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/autopilots` | участник | список |
| POST | `/api/autopilots` | человек-участник | создать |
| GET | `/api/autopilots/cron-preview` | участник | предпросмотр ближайших срабатываний cron |
| GET | `/api/autopilots/{id}` | участник | получить (с триггерами и коллабораторами) |
| PATCH | `/api/autopilots/{id}` | создатель/owner/admin/коллаборатор | изменить |
| DELETE | `/api/autopilots/{id}` | создатель/owner/admin/коллаборатор | архивировать |
| POST | `/api/autopilots/{id}/trigger` | создатель/owner/admin/коллаборатор | запустить вручную сейчас |
| GET | `/api/autopilots/{id}/runs` | участник | история прогонов |
| GET | `/api/autopilots/{id}/runs/{runId}` | участник | один прогон целиком |
| GET | `/api/autopilots/{id}/deliveries` | участник | история доставок |
| GET | `/api/autopilots/{id}/deliveries/{deliveryId}` | участник | одна доставка целиком |
| POST | `/api/autopilots/{id}/deliveries/{deliveryId}/replay` | создатель/owner/admin/коллаборатор | реплей доставки |
| POST | `/api/autopilots/{id}/triggers` | создатель/owner/admin/коллаборатор | добавить триггер |
| PATCH/DELETE | `/api/autopilots/{id}/triggers/{triggerId}` | создатель/owner/admin/коллаборатор | изменить/удалить триггер |
| POST | `.../triggers/{triggerId}/rotate-webhook-token` | создатель/owner/admin/коллаборатор | перевыпустить токен вебхука |
| PUT | `.../triggers/{triggerId}/signing-secret` | создатель/owner/admin/коллаборатор | задать/снять секрет подписи (≥16 символов, либо пусто) |
| POST | `/api/autopilots/{id}/collaborators` | **только создатель/owner/admin** | выдать право коллаборатора |
| DELETE | `.../collaborators/{userId}` | **только создатель/owner/admin** | отозвать право |

---

### 8. Закрепления (Pins)

Личный (не общий для воркспейса) список закладок пользователя на задачи или
проекты. `item_type` — `issue` или `project`. Новая закладка получает
позицию «в конец» (максимум текущих + 1); переупорядочивание — отдельным
батч-запросом `PUT /api/pins/reorder` (применяется по одной записи, без общей
атомарности между ними).

| Метод | Путь | Права |
|---|---|---|
| GET / POST | `/api/pins` | участник (свои закладки) |
| PUT | `/api/pins/reorder` | участник |
| DELETE | `/api/pins/{itemType}/{itemId}` | участник |

---

### 9. Вложения как отдельный ресурс (`/api/attachments/{id}`)

| Метод | Путь | Поведение |
|---|---|---|
| GET | `/api/attachments/{id}` | метаданные + временная подписанная ссылка на скачивание |
| GET | `/api/attachments/{id}/content` | инлайн-текстовое превью (только для текстоподобных файлов малого размера) |
| DELETE | `/api/attachments/{id}` | удалить (в т.ч. файл в объектном хранилище) |

---

### 10. Агенты (Agents)

#### 10.1 Модель прав вызова агента

Каждый агент имеет `permission_mode`: `private` (вызвать может только
владелец агента, либо владелец/админ воркспейса) или `public_to` (вызвать
могут также цели, перечисленные в `invocation_targets` — список записей
`{target_type, target_id}`, где `target_type` одно из `workspace` (весь
воркспейс, `target_id` не нужен), `member` (конкретный человек) или `team`
(конкретная команда)). Поле `visibility` (`private`/`workspace`) — устаревший
плоский вид этой же модели, сохранённый для обратной совместимости: `Update`
принимает любую из двух форм, но не обе одновременно осмысленно (если
переданы оба — используется `permission_mode`/`invocation_targets`).

`status` агента (`idle, working, blocked, error, offline`) — это его
логический статус в системе, отдельно от статуса привязанного к нему
runtime (`online`/`offline`), который определяет, готов ли агент физически
принять новый запуск (см. §1.9).

#### 10.2 Кто может управлять конфигурацией агента

Общее управление (`canManageAgent`): владелец агента **или** владелец/админ
воркспейса, но не рядовой участник и не сам агент (заголовок
`X-Actor-Source: task_token`/`cloud_pat` — то есть агент, действующий от
своего токена, — явно отклоняется с 403 при попытке менять конфигурацию
своего же агента).

Внутри `updateAgent` часть полей ограничена ещё жёстче — **только владелец
агента**, даже если вызывающий владелец/админ воркспейса: `instructions`
(инструкции исполняются на runtime владельца), `custom_args` (командная
строка на его же runtime), `runtime_config` (несёт учётные данные шлюза),
выбор `runtime_id` (перенос агента на другую машину — плейнтекст переменных и
MCP-конфига доходит до этой машины) и права вызова
(`permission_mode`/`invocation_targets`/`visibility`) — попытка их **реально**
изменить не-владельцем возвращает 403; если новое значение совпадает с уже
сохранённым, запрос молча игнорирует это поле, а не отклоняется целиком.

#### 10.3 Секреты агента и их видимость

`mcp_config` хранится зашифрованным на сервере. В ответах API он либо
раскрывается в открытом виде (владельцу агента, либо владельцу/админу
воркспейса — если для воркспейса включена настройка «всегда раскрывать
секреты»), либо полностью скрывается (`mcp_config_redacted: true`), но не
маскируется частично. Значения `custom_env` — либо в открытом виде
(владельцу), либо каждое заменяется маркером `****` (не-владельцу; сами
имена ключей видны всегда). `composio_toolkit_allowlist` скрывается целиком,
если соответствующая функция выключена в воркспейсе, либо помечается
редактированным для не-владельца.

Любое **чтение** переменных окружения (`GET /api/agents/{id}/env`) — и
маскированное перечисление ключей, и раскрытие значений владельцу —
обязательно пишет запись в журнал активности воркспейса; если запись в
журнал не удалась, эндпоинт целиком отказывает (500), а не отдаёт данные без
следа аудита. Изменение переменных (`PUT`) тоже пишет аудит со списками
добавленных/изменённых/удалённых/сохранённых ключей, а отклонённая попытка
не-владельца реально изменить значение — отдельной записью
`agent_env_update_refused`.

Значение переменной окружения, переданное как маркер `****`, означает
«оставить как было»; значение окружения, которое **начинается** с `****`, но
не равно ему целиком — считается испорченным вводом (типичная опечатка
поверх маски в форме) и отклоняется с 400, а не сохраняется как есть.

#### 10.4 Навыки агента: пользовательские (Skill) и встроенные (Runtime)

Есть два независимых механизма:

- **AgentSkills** — привязка сущностей `Skill` воркспейса к агенту
  (`GET/PUT/POST .../skills[/add]`, включение/выключение конкретной привязки
  `PUT .../skills/{skillId}/enabled`, отвязка `DELETE .../skills/{skillId}`).
  Полная замена набора (`PUT`) отличается от добавления (`POST .../add`) тем,
  что первая удаляет всё, чего нет в новом списке.
- **Runtime skills** — навыки, встроенные в сам исполняемый рантайм агента
  (сейчас — провайдеры `runtime-c` и `runtime-e`, только режим `local`), не
  являющиеся сущностями воркспейса. Список выключенных хранится прямо на
  записи агента (`disabled_runtime_skills`) и переоценивается заново при
  смене `runtime_id` (агент, привязанный не к тому runtime, что указан в
  запросе, получает 409).

#### 10.5 MCP-серверы агента

Агент может подключать **общие** MCP-серверы, зарегистрированные на уровне
воркспейса (или включённые для деплоймента) — не создавать собственные.
Привязка/отвязка/включение-выключение конкретной привязки — только
владелец/админ воркспейса, не сами агенты и не рядовые участники
(`requireAgentMcpWriter` явно отклоняет актора-агента).

#### 10.6 Создание агента из шаблона

`createAgentFromTemplate` разворачивает предустановленный набор навыков
(часть — «зашита» в сервер, часть подтягивается по URL источника при первом
использовании и переиспользуется по имени в рамках воркспейса при повторном
создании агентов из того же шаблона). Если хотя бы один внешний источник
недоступен в момент создания — вся операция отменяется целиком (агент не
создаётся), с кодом 422 и списком неудачных URL.

#### 10.7 Маршруты

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/agents` | участник | список видимых агентов (может тихо создать личного помощника/ролевого агента, если их ещё нет) |
| POST | `/api/agents` | участник | создать |
| POST | `/api/agents/from-template` | участник | создать из шаблона |
| GET | `/api/agents/{id}` | участник (с правом видеть, если приватный) | получить |
| PUT | `/api/agents/{id}` | владелец агента / owner / admin (часть полей — только владелец) | изменить |
| POST | `/api/agents/{id}/archive` | владелец / owner / admin | архивировать (отменяет активные задачи) |
| POST | `/api/agents/{id}/restore` | владелец / owner / admin | восстановить |
| POST | `/api/agents/{id}/cancel-tasks` | владелец / owner / admin | отменить все активные запуски |
| GET | `/api/agents/{id}/tasks` | участник (с доступом) | вся история запусков |
| GET/PUT/POST | `/api/agents/{id}/skills[/add]` | владелец / owner / admin (GET — любой участник) | навыки агента |
| PUT | `/api/agents/{id}/skills/{skillId}/enabled` | владелец / owner / admin | вкл/выкл привязку |
| DELETE | `/api/agents/{id}/skills/{skillId}` | владелец / owner / admin | отвязать |
| PUT | `/api/agents/{id}/runtime-skills/enabled` | владелец / owner / admin | вкл/выкл встроенный навык рантайма |
| GET/POST | `/api/agents/{id}/labels` | владелец / owner / admin (GET — любой участник); только при включённых ресурсных метках | метки агента |
| DELETE | `/api/agents/{id}/labels/{labelId}` | владелец / owner / admin | открепить метку |
| GET | `/api/agents/{id}/env` | владелец / owner / admin | переменные окружения (с аудитом чтения) |
| PUT | `/api/agents/{id}/env` | владелец (полная запись) / owner / admin (только маркеры `****`) | изменить окружение (с аудитом) |
| GET | `/api/agents/{id}/mcp-servers` | участник | список привязанных MCP-серверов |
| POST | `/api/agents/{id}/mcp-servers` | **только owner/admin воркспейса** | привязать общий MCP-сервер |
| PUT | `.../mcp-servers/{serverId}/enabled` | только owner/admin | вкл/выкл привязку |
| DELETE | `.../mcp-servers/{serverId}` | только owner/admin | отвязать |

#### 10.8 Realtime-события агентов

| Маршрут | Событие |
|---|---|
| `createAgent` / `createAgentFromTemplate` | `agent:created` |
| `updateAgent` | `agent:status` |
| `archiveAgent` | `agent:archived` |
| `restoreAgent` | `agent:restored` |
| `setAgentSkills` / `addAgentSkills` / `setAgentSkillEnabled` / `removeAgentSkill` | `agent:status` (с полным списком навыков в payload) |
| `setAgentRuntimeSkillEnabled` | `agent:status` |
| `updateAgentEnv` | `agent:status` |
| `attachAgentLabel` / `detachAgentLabel` | `label:updated` |
| `addAgentMcpServer` / `setAgentMcpServerEnabled` / `removeAgentMcpServer` | без realtime-события |
| `cancelAgentTasks` | `task:cancelled` за каждую (через подсистему задач) |

---

### 11. Общие правила валидации и инварианты

- **Нумерация задач** монотонна и уникальна в пределах воркспейса, никогда не
  переиспользуется; `identifier = PREFIX-number`.
- **Позиция** (`position`) — `float64`; конкурентная перестановка через
  `move` обнаруживается по несовместимым/слишком близким соседям и требует
  от клиента перечитать данные и повторить (409).
- **`assignee_type`/`assignee_id`** и **`user_type`/`user_id`** (подписки) —
  всегда передаются парой или не передаются вовсе.
- Различение «поле не передано» и «поле передано как `null`» существенно во
  всех частичных обновлениях (`PUT`/`PATCH`) этого раздела: не передавать поле —
  не трогать значение; передать `null` — явно очистить его (там, где поле
  nullable).
- **Лимиты размера**: `issue.metadata` — 50 ключей / 8 КБ; `issue.properties`
  — 16 КБ; свойство `select`/`multi_select` — до 50 вариантов; воркспейс — до
  20 активных определений свойств одновременно; имя метки/варианта — до 32
  символов; описание агента — до 255 символов; шаблон заголовка автопилота,
  цвет метки (6-значный HEX) и т.п. — см. соответствующие схемы в `docs/50-api-contract.yaml`.
- **Циклы в иерархии**: смена родителя задачи проверяется на цикличность
  (до 10 уровней вверх); отряд не может остаться без лидера (сначала
  назначить нового, потом убрать старого из участников).
- **Мягкое удаление** используется для отрядов (`archived_at`/`archived_by`)
  и автопилотов (`status: archived`) с переносом их назначений на лидера;
  задачи, проекты, метки, комментарии, вложения, определения свойств,
  подписки, закладки, привязки меток/навыков/MCP — удаляются жёстко (без
  восстановления через API). Агенты — мягко (`archived_at`), с явным
  `restore`.
- **Актор в событиях** (`actor_type`/`actor_id` в `x-events`) — тот же актор,
  что вычислен для запроса (см. «Актор запроса» в начале документа), а не
  обязательно человек, инициировавший цепочку изменений.

---

## Шаблоны агентов, конструктор агента, навыки, дашборд, runtime, чат, инбокс, уведомления

### 1. Шаблоны агентов (`/api/agent-templates`)

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/agent-templates` | любой участник | Список шаблонов, зашитых в поставку сервера (не в БД воркспейса). Возвращает краткую сводку без системной инструкции. |
| GET | `/api/agent-templates/{slug}` | любой участник | Полное описание шаблона, включая `instructions` — системный промпт, который получит агент, если его создать из этого шаблона. 404, если slug не зарегистрирован. |

Шаблон описывает не только текст инструкции, но и набор навыков
(`skills`), которые нужно подтянуть при создании агента из шаблона —
каждый навык задан ссылкой на источник (`source_url`) и, опционально,
закэшированным именем/описанием, чтобы не делать сетевой запрос заранее.
Сам процесс создания агента из шаблона (`POST /api/agents/from-template`)
находится в другом разделе спецификации (маршрут зарегистрирован раньше
строки 1061), но раз он ссылается на структуру `AgentTemplate`, для его
понимания важно знать: сервер сначала пытается переиспользовать уже
существующий в воркспейсе навык с тем же именем, и только если такого
нет — импортирует навык по `source_url` (см. раздел 2 про импорт
навыков — правила валидации и источники те же).

### 2. Конструктор агента (`/api/agent-builder/**`)

Agent Builder — это не отдельная подсистема, а обычный чат с
предустановленным системным агентом. Технически:

- `POST /api/agent-builder/sessions` создаёт служебного агента
  (`kind=system`, `system_key = "agent_builder:<flowId>"`, с системной
  инструкцией, зашитой на сервере — она требует от модели заканчивать
  каждый ответ машиночитаемым блоком `<agent_draft>{...}</agent_draft>`
  с текущим черновиком конфигурации агента) и обычную чат-сессию,
  привязанную к этому агенту. Дальше пользователь ведёт диалог обычными
  чат-эндпоинтами раздела 6 (`POST /api/chat/sessions/{sessionId}/messages`
  и далее). Сам конструктор никогда не создаёт агента напрямую — это
  делает клиент отдельным вызовом (`POST /api/agents`, в другом разделе)
  после того, как пользователь одобрит показанный черновик.
- Runtime, на котором будет исполняться диалог, обязан быть в статусе
  `online`; он либо публичный (`visibility=public`), либо принадлежит
  вызывающему пользователю, либо вызывающий — owner/admin воркспейса.
  Иначе 403/409.
- `PATCH /api/agent-builder/sessions/{sessionId}/runtime` меняет runtime
  уже существующей сессии конструктора. Требует: сессия принадлежит
  вызывающему, она активна (не архивирована), у неё нет незавершённой
  задачи (иначе 409 — сначала нужно остановить текущий ответ), а её
  агент действительно распознаётся как carrier конструктора (иначе 404 —
  так системе не подсовывают чужие обычные чаты).

Правила валидации: `runtime_id` обязателен и должен быть валидным UUID
существующего в воркспейсе runtime.

### 3. Навыки (`/api/skills/**`)

Skill — переиспользуемый фрагмент знаний/инструкций (аналог
"файла инструкции" в стиле SKILL.md), который можно подключать агентам.
У навыка есть основное содержимое (`content`, текст SKILL.md) и набор
вспомогательных файлов с произвольными относительными путями.

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/skills` | любой участник | Список навыков воркспейса без `content` (сводка). |
| POST | `/api/skills` | любой участник | Создать навык вручную с содержимым и файлами. Имя уникально в воркспейсе (409 при конфликте). Событие `skill:created`. |
| GET | `/api/skills/search` | любой участник | Поиск во внешнем публичном каталоге навыков (домен clawhub.ai). Источник поиска можно отключить на уровне деплоя (403 `source_disabled`). |
| POST | `/api/skills/import` | любой участник | Импорт навыка по ссылке (JSON) либо из ZIP/.skill-архива (multipart). См. правила ниже. |
| GET | `/api/skills/{id}` | любой участник | Навык вместе со всеми файлами. |
| PUT | `/api/skills/{id}` | владелец навыка либо owner/admin | Частичное обновление; замена набора файлов, если поле `files` передано. Событие `skill:updated`. |
| DELETE | `/api/skills/{id}` | владелец навыка либо owner/admin | Удаление навыка и его назначений меток. Событие `skill:deleted`. |
| GET/POST | `/api/skills/{id}/labels` | чтение — любой участник; запись — владелец/owner/admin | Метки (labels) с `resource_type=skill`, привязанные к навыку. Доступно только если в воркспейсе включена фича labels, иначе 404. |
| DELETE | `/api/skills/{id}/labels/{labelId}` | владелец навыка либо owner/admin | Отвязать метку. |
| GET | `/api/skills/{id}/files` | любой участник | Список вспомогательных файлов. |
| PUT | `/api/skills/{id}/files` | владелец навыка либо owner/admin | Создать/заменить один файл. Путь `SKILL.md` зарезервирован — 400 при попытке его перезаписать этим методом. |
| DELETE | `/api/skills/{id}/files/{fileId}` | владелец навыка либо owner/admin | Удалить один файл; 404, если файл принадлежит другому навыку. |

**"Владелец навыка"** здесь означает: пользователь, который его создал
(`created_by`), либо owner/admin воркспейса. Обычный участник, не
создававший навык, не может его редактировать/удалять/менять файлы или
метки, но может читать.

#### Валидация путей файлов

Путь файла отклоняется (400), если он абсолютный или после
нормализации пути начинается с `..` (попытка выйти за
пределы каталога навыка).

#### Импорт навыков — источники и лимиты

Разрешённые источники по домену ссылки: `clawhub.ai`, `skills.sh`,
`github.com` (или просто `owner/slug` — трактуется как ссылка на
clawhub.ai). Администратор деплоя может отключить любой источник по
отдельности (403 с человекочитаемым сообщением). Единый набор лимитов
для всех источников:

- не более **256** файлов в бандле;
- не более **1 МБ** на файл;
- не более **8 МБ** суммарно;
- файлы с "бинарными" расширениями (изображения, шрифты, архивы,
  документы Office, аудио/видео, исполняемые файлы, базы данных) —
  пропускаются молча, не считаются ошибкой;
- файлы `LICENSE`/`LICENSE.md`/`LICENSE.txt` — пропускаются молча;
- `SKILL.md` обязателен и не может быть пустым — иначе импорт
  отклоняется как ошибка (400/502 в зависимости от источника).
- Общий тайм-аут на получение файлов источника — **45 секунд**
  (504 при превышении).
- Загрузка архива через `multipart/form-data` ограничена **16 МБ**
  на сам файл запроса (до распаковки).

Стратегия при конфликте имени (`on_conflict`, необязательное поле):

| Значение | Поведение |
|---|---|
| `fail` (по умолчанию, либо поле не передано вовсе) | 409, если совпадение по имени. Если поле `on_conflict` не передавалось вообще, ответ — "голый" `Skill`/ошибка (обратная совместимость), а не `SkillImportResult`. |
| `skip` | 200, `status=skipped`, новый навык не создаётся. |
| `overwrite` | Перезаписывает существующий навык (только если вызывающий — его создатель, иначе `status=failed`, 403). Событие `skill:updated`. |
| `rename` | Создаёт копию с суффиксом `-2`, `-3`, ... (до 50 попыток). Событие `skill:created`. |

Если поле `on_conflict` передано явно (даже неявно как `""`, но именно
через наличие ключа в JSON — то есть `on_conflict: ""` тоже считается
"передано"), ответ **всегда** оборачивается в `SkillImportResult` со
статусом `created|updated|skipped|conflict|failed`, а не только при
реальном конфликте.

### 4. Дашборд (`/api/dashboard/**`)

Все шесть маршрутов — только чтение, доступны любому участнику
воркспейса, и принимают одинаковый набор query-параметров:

- `days` (1..365, по умолчанию 30; исключение — `getRuntimeUsage` из
  раздела 5, там по умолчанию 90) — ширина окна;
- `tz` — IANA-таймзона для группировки по календарным дням/часам; если
  не передан — берётся таймзона профиля пользователя, иначе UTC;
- `project_id` — необязательный фильтр по проекту.

Особый случай: `GET /api/dashboard/failures/by-agent` использует
"точную" границу отсчёта (`days` без дополнительного дня запаса),
тогда как все остальные добавляют один день запаса к границе окна —
это учитывает пограничные события на стыке суток в локальной таймзоне
пользователя.

| Маршрут | Возвращает |
|---|---|
| `usage/daily` | Токены/стоимость по дням, провайдеру и модели. |
| `usage/by-agent` | Токены/стоимость по агенту, провайдеру и модели. |
| `agent-runtime` | Суммарное время выполнения задач и число задач/провалов по агенту. |
| `runtime/daily` | То же, но агрегировано по дням для всего воркспейса. |
| `failures/daily` | Число провалившихся задач по дням и причине провала. |
| `failures/by-agent` | Число провалившихся задач по агенту и причине. |

Денежные величины передаются в целых "тиках" USD (`cost_usd_ticks`,
1 тик = 1e-6 доллара), чтобы избежать ошибок округления
чисел с плавающей точкой на клиенте.

### 5. Runtime (`/api/runtimes/**`)

**Runtime** — это daemon-среда: десктопное приложение или облачный
воркер, зарегистрированный в конкретном воркспейсе и способный
принимать и исполнять задачи агентов. У Runtime есть:

- `status`: `online` (daemon недавно присылал heartbeat) или `offline`
  (heartbeat не приходил дольше тайм-аута);
- `visibility`: `private` (доступен только владельцу и owner/admin) или
  `public` (доступен всем участникам воркспейса для привязки агентов);
- `owner_id` — пользователь, который зарегистрировал этот runtime;
- `daemon_id` — идентификатор физического daemon-процесса; несколько
  Runtime (для разных провайдеров агентского движка) могут иметь общий
  `daemon_id`, если это одна и та же машина;
- `profile_id` — если задан, значит этот конкретный экземпляр Runtime
  порождён живым **runtime-профилем** (шаблоном конфигурации,
  которым управляют отдельные маршруты `/api/workspaces/{id}/runtime-profiles`,
  в другом разделе спецификации); такие экземпляры нельзя удалить напрямую —
  нужно удалить профиль.

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| GET | `/api/runtimes` | любой участник | Список runtime воркспейса; `?owner=me` — только свои. |
| PATCH | `/api/runtimes/{runtimeId}` | владелец либо owner/admin | Меняет `visibility` и/или `custom_name`. `apply_to_machine=true` распространяет новое имя на все runtime той же физической машины (`daemon_id`) в воркспейсе (для рядового участника — только среди тех, которыми он сам владеет). Событие `daemon:register`. |
| DELETE | `/api/runtimes/{runtimeId}` | владелец либо owner/admin | См. ниже "Удаление runtime". Событие `daemon:register`. |
| POST | `/api/runtimes/{runtimeId}/mcp-verified` | любой участник | Фиксирует в памяти процесса (не в БД) результат самопроверки MCP-прокси на runtime. |
| GET | `/api/runtimes/{runtimeId}/usage`, `/usage/by-agent`, `/usage/by-hour` | любой участник | Статистика токенов/стоимости конкретного runtime (окно по умолчанию 90 дней для `/usage`, 30 — для остальных двух). |
| GET | `/api/runtimes/{runtimeId}/activity` | любой участник | Почасовое распределение количества задач (0..23) для тепловой карты активности. |
| POST/GET | `/api/runtimes/{runtimeId}/update`, `/update/{updateId}` | инициировать — владелец/owner/admin; смотреть — владелец/owner/admin/инициатор | Асинхронный запрос самообновления daemon. Отключено на деплоях с "периметровым" (perimeter) профилем поставки — 403. |
| POST/GET | `/api/runtimes/{runtimeId}/models`, `/models/{requestId}` | любой участник | Асинхронный запрос списка поддерживаемых runtime моделей. Требует runtime online. |
| POST/GET | `/api/runtimes/{runtimeId}/local-skills`, `/local-skills/{requestId}` | любой участник (чтение) | Асинхронный запрос списка локально установленных на машине runtime навыков и MCP-серверов. |
| POST/GET | `/api/runtimes/{runtimeId}/local-skills/import`, `/local-skills/import/{requestId}` | только владелец runtime | Асинхронный запрос на импорт одного локального навыка в воркспейс (владелец читает файлы со своей машины). |
| POST | `/api/runtimes/{runtimeId}/archive-agents-and-delete` | владелец либо owner/admin | Массовое архивирование активных агентов runtime с последующим удалением runtime. |

#### Асинхронный паттерн "заявка → опрос" (models / update / local-skills)

Четыре группы маршрутов (`update`, `models`, `local-skills`,
`local-skills/import`) устроены одинаково и решают одну и ту же
проблему: HTTP-запрос от пользователя не может ждать, пока
daemon-приложение (которое может быть офлайн, за NAT, отвечать
секундами) выполнит команду. Схема:

1. `POST .../{action}` создаёт запись-заявку со статусом `pending` и
   немедленно возвращает её id — HTTP-ответ не блокируется на daemon.
2. Отдельный daemon-канал (регистрируется до строки 1061, вне этой
   части спецификации, аутентификация `daemonAuth`) забирает
   старейшую `pending`-заявку для своего runtime, переводит её в
   `running`, выполняет команду локально и репортит результат туда же.
3. Клиент опрашивает `GET .../{requestId}` до тех пор, пока статус не
   станет терминальным: `completed`, `failed` или `timeout`.

Тайм-ауты (после которых заявка сама переходит в `timeout`, если
daemon не ответил):

| Заявка | Ожидание отклика (pending) | Ожидание выполнения (running) | Хранение после завершения |
|---|---|---|---|
| `update` | 120 c | 150 c | 5 мин |
| `models` | 30 c | 60 c | 2 мин |
| `local-skills` / `local-skills/import` | 3 мин | 60 c | 5 мин |

На один `runtimeId` не может быть больше одной необработанной заявки
на обновление одновременно (409 при попытке создать вторую); для
`models`/`local-skills` такого ограничения нет — заявки не блокируют
друг друга (обрабатываются по одной, но независимо ставятся в очередь).
Хранилища заявок — в памяти процесса сервера, не переживают рестарт.

Импорт локального навыка (`local-skills/import`) дополнительно
поддерживает `action=overwrite` с `target_skill_id` — тогда при
совпадении имени с уже существующим навыком сервер не создаёт новый, а
предлагает перезаписать указанный (тот же контракт владения, что и у
`PUT /api/skills/{id}`: перезаписать может только создатель целевого
навыка). Терминальный статус `conflict` (доп. к обычным пяти) означает,
что daemon нашёл в воркспейсе навык с таким же именем и запрос не был
структурирован для автоматического разрешения конфликта — тело ответа
содержит `conflict.existing_skill_id`/`can_overwrite`.

#### Удаление runtime

Прямое `DELETE /api/runtimes/{runtimeId}`:

1. 409 `runtime_profile_instance_delete_unsupported`, если у runtime
   есть `profile_id`, ссылающийся на всё ещё существующий (живой)
   runtime-профиль — такие экземпляры удаляются только через удаление
   профиля.
2. 409 `runtime_has_active_agents`, если на runtime есть неархивные
   агенты — тело ответа перечисляет их полностью (объекты `Agent`,
   схема — часть C), чтобы клиент показал список и предложил
   пользователю либо архивировать их вручную, либо перейти к
   `archive-agents-and-delete`.
3. 409 (без отдельного кода), если есть активные отряды (squads), у
   которых лидер — уже архивный агент этого runtime.
4. Если препятствий нет — каскадно, в одной транзакции: ставит на
   паузу автопилоты, чьи назначенные исполнители архивны на этом
   runtime; удаляет отряды, привязанные к архивным агентам этого
   runtime; чистит invocation targets, интеграции внешних каналов,
   закрепления в чате, назначения меток, черновики восстановления
   чата, привязки MCP-серверов; удаляет архивные и системные агенты
   этого runtime; удаляет сам runtime.

`POST .../archive-agents-and-delete` — вариант с явным подтверждением:
клиент показывает пользователю список активных агентов (полученный из
409 выше или из отдельного запроса) и передаёт их id обратно в
`expected_active_agent_ids`. Если к моменту вызова фактический набор
активных агентов изменился (кто-то создал/удалил агента, пока
пользователь читал диалог подтверждения) — 409
`runtime_delete_plan_changed` с обновлённым списком, нужно повторное
подтверждение. При совпадении: архивирует ровно эти агенты, отменяет
их задачи и задачи на runtime, публикует `agent:archived` по каждому,
затем выполняет тот же каскад очистки, что и прямое удаление, и
`daemon:register`.

### 6. Облачный runtime (`/api/cloud-runtime/**`)

Это не самостоятельная подсистема, а **прозрачный HTTP-прокси** во
внешний облачный fleet-сервис управления виртуальными runtime-нодами
(AWS-подобные инстансы, на которых можно поднять daemon в облаке).
Сервер goosar не хранит состояние нод — только форвардит запрос,
добавляя `X-User-ID` там, где сервису важно знать вызывающего, и (для
`nodes/create`, `nodes/start` и т. п.) — тело запроса как есть.
Если fleet-сервис не настроен на конкретном деплое — все маршруты этой
группы отвечают 503.

| Метод | Путь | Поведение |
|---|---|---|
| GET | `/api/cloud-runtime` | Общая информация о сервисе (проксируется). |
| GET | `/api/cloud-runtime/healthz`, `/readyz` | Проверки живости/готовности fleet-сервиса; ответ пользователя не требует. |
| GET | `/api/cloud-runtime/nodes` | Список нод пользователя (`limit`/`offset`). |
| POST | `/api/cloud-runtime/nodes` | Заказать новую ноду. |
| DELETE | `/api/cloud-runtime/nodes` | Удалить ноду — id передаётся в теле (`instance_id`), не в пути. |
| POST | `/api/cloud-runtime/nodes/start`, `/stop`, `/reboot` | Управление жизненным циклом ноды по `instance_id` в теле. |
| POST | `/api/cloud-runtime/nodes/status` | Опрос статуса ноды (реализовано как POST, а не GET, так как несёт тело). |
| POST | `/api/cloud-runtime/nodes/exec` | Выполнение команды на ноде. |

Поля `CloudRuntimeNode` и `CreateCloudRuntimeNodeRequest` в `docs/50-api-contract.yaml`
соответствуют тому, что использует текущий клиент;
точный формат тел `start/stop/reboot/status/exec` определяется
контрактом самого fleet-сервиса и не документируется здесь подробнее —
для реализации достаточно того, что сервер goosar их не разбирает,
а прозрачно передаёт дальше, требуя лишь непустого JSON-тела.

### 7. Отмена задачи и агентская активность воркспейса

`POST /api/tasks/{taskId}/cancel` — отмена задачи агента по инициативе
пользователя. Проверка прав зависит от происхождения задачи: если
задача привязана к чат-сессии — отменять может только создатель этой
сессии; если нет (задача от тикета/автопилота) — нужен доступ к
приватному агенту. Публикует `task:cancelled` и, для чатовых задач,
`chat:cancel_finalized` с исходом `stopped` (сообщение просто
остановлено) либо `restored` (пользовательское сообщение, вызвавшее
задачу, нужно вернуть в поле ввода — это происходит, если клиент
заявил поддержку возможности `chat_draft_restore_v1` через заголовок
клиентских capability).

Четыре маршрута только для чтения агрегируют статус агентов
воркспейса для верхнеуровневых виджетов интерфейса: снимок текущих
задач (`/api/agent-task-snapshot`), какие агенты прямо сейчас работают
(`/api/working-agents`), активность за 30 дней
(`/api/agent-activity-30d`) и число запусков по агентам
(`/api/agent-run-counts`). Все доступны любому участнику воркспейса,
специфических правил валидации параметров нет — состав ответа
определяется исключительно данными воркспейса на момент запроса.

### 8. Чат (`/api/chat/**`)

#### Модель данных

- **ChatSession** — диалог пользователя с одним конкретным агентом.
  У сессии есть статус `active`/`archived`, опциональная привязка к
  проекту, флаг закрепления (`pinned`), счётчик непрочитанных
  сообщений. Сессию видит и ею управляет только её создатель
  (`creator_id`) — в отличие от тикетов, чат не общий ресурс
  воркспейса, это личный канал общения с агентом.
- **ChatMessage** — одно сообщение (`role`: `user`, `assistant` или
  `system`). У сообщения есть `message_kind`: `message` (обычный
  ответ) или `no_response` (агент осознанно ничего не ответил
  текстом — например, выполнил только служебное действие).
  `failure_reason` заполняется, если формирование ответа завершилось
  ошибкой.

#### Как сообщение чата порождает задачу агента

`POST /api/chat/sessions/{sessionId}/messages` — единственная точка
входа для отправки реплики пользователя. Предусловия: сессия активна,
агент не архивирован и у него назначен runtime, у вызывающего есть
право вызвать этого агента именно в контексте данной сессии.
При выполнении:

1. Создаётся **задача агента** (`agent task`) с приоритетом уровня
   чата, привязанная одновременно к чат-сессии и к runtime агента, и
   ставится в очередь этого runtime.
2. Создаётся `ChatMessage` с `role=user`, привязанная к этой задаче.
3. Если переданы `attachment_ids` — ранее загруженные вложения
   привязываются к этому сообщению; в ответе возвращаются только те id,
   что реально были привязаны (защита от повторной привязки чужого
   или уже занятого вложения).
4. Публикуется `chat:message` (`role: "user"`) немедленно — клиент
   сразу видит своё сообщение в списке.
5. Если это первое пользовательское сообщение в сессии — асинхронно (не
   блокируя ответ) запускается генерация заголовка сессии по его
   содержимому.
6. HTTP-ответ возвращается сразу же (`201`) с `message_id` и `task_id` —
   ответ агента **не** ждётся синхронно.

#### Стриминг и завершение ответа

Дальнейшая обработка происходит асинхронно на стороне сервиса задач
(в другом разделе спецификации по коду, но наблюдаема через
realtime-события той же чат-сессии):

- `task:queued` → `task:dispatch`/`task:running` — задача принята
  runtime в работу;
- серия `task:progress` / `task:message` — промежуточные шаги
  (например, вызовы инструментов) по мере их выполнения агентом;
- в конце — либо `task:completed`, либо `task:failed`;
- одновременно с завершением создаётся `ChatMessage` с
  `role=assistant`, публикуется `chat:message` (`role: "assistant"`),
  а следом — `chat:done` с итоговым содержимым, длительностью
  (`elapsed_ms`) и `message_kind`;
- если пользователь отменяет задачу до того, как агент успел
  ответить, — публикуется `chat:cancel_finalized` вместо `chat:done`
  (см. раздел 7).

Клиент, таким образом, узнаёт о новом сообщении и о его прогрессе
исключительно через подписку на realtime-канал воркспейса/сессии, а
не через поллинг `sendChatMessage`; поллинг (`GET .../pending-task`,
`GET /api/chat/pending-tasks`, `.../has-any`) предусмотрен как резервный
механизм на случай разрыва realtime-соединения.

#### Прочие маршруты чата

| Метод | Путь | Права | Поведение |
|---|---|---|---|
| POST/GET | `/api/chat/sessions` | создатель / список — участник | Создать сессию (нужен доступ на вызов агента) либо получить список своих сессий. `?status=all` включает архивные. |
| GET/PATCH/DELETE | `/api/chat/sessions/{sessionId}` | создатель сессии либо owner/admin | Чтение, переименование/смена проекта (ровно одно поле за раз), удаление (каскадно отменяет незавершённые задачи, снимает канальные привязки, удаляет черновики восстановления и, если агент системный, — сам этот служебный агент). |
| PATCH | `.../pin`, `.../archive` | создатель сессии | Закрепление и архивирование/восстановление; при архивировании снимается привязка к внешнему каналу. |
| GET | `.../messages`, `.../messages/page` | создатель сессии | Полная история либо курсорная пагинация (`before_created_at`+`before_id` вместе, `limit` 1..100, по умолчанию 50). |
| GET | `.../pending-task` | создатель сессии | Текущая незавершённая задача сессии либо пустой объект. |
| POST | `.../read` | создатель сессии | Отметить сессию прочитанной. Событие `chat:session_read`. |
| GET/DELETE | `.../draft-restores`, `.../draft-restores/{restoreId}` | создатель сессии | Список черновиков, "потерянных" при отмене задачи (см. раздел 7), и подтверждение их обработки клиентом. |
| GET | `/api/chat/pending-tasks`, `/pending-tasks/has-any` | участник | Все незавершённые чат-задачи текущего пользователя в воркспейсе / просто флаг "есть хоть одна" (для частого поллинга). |
| GET/POST/DELETE | `/api/chat/pinned-agents`, `/pinned-agents/{agentId}` | участник | Быстрый доступ к избранным агентам чата; лимит — 5 закреплений на пользователя, позиция — по порядку добавления. |
| GET | `/api/chat/history`, `/api/chat/thread` | только вызов из контекста исполняющейся задачи | См. ниже. |

#### `/api/chat/history` и `/api/chat/thread`

Особый случай: эти два маршрута вызывает не человек из браузера, а сам
**агент во время выполнения задачи**, читающий историю внешнего
чат-канала (например, Slack-треда), к которому привязана сессия, через
инструмент. Проверка иная, чем везде в этом разделе: обязательны
заголовки `X-Actor-Source: task_token` и `X-Task-ID` с id текущей
задачи; задача обязана быть чатовой (иметь `chat_session_id`), а её
сессия — принадлежать текущему воркспейсу. Если у сессии нет внешней
канальной интеграции (обычный внутренний чат) — возвращается пустой
список с полем `note`, а не ошибка. `getChatThread` дополнительно
требует query-параметр `id` — идентификатор треда во внешнем канале.

### 9. Инбокс (`/api/inbox/**`)

InboxItem — персональное уведомление участника воркспейса (не общий
лог активности тикета — тот отдельный ресурс). У каждого элемента:

- `recipient_type`/`recipient_id` — получатель; человеческий API
  (`/api/inbox`) видит только `recipient_type=member`, где
  `recipient_id` — сам вызывающий пользователь (элементы с
  `recipient_type=agent` существуют в БД, но через эти маршруты не
  читаются);
- `type` — тип уведомления, влияет на то, к какой группе настроек
  уведомлений (раздел 10) он относится и может быть заглушен;
- `severity` — `info`, `action_required` или `attention`;
- `read`/`archived` — независимые булевы флаги;
- `issue_id` — если уведомление связано с тикетом, а `issue_status` —
  денормализованный на момент чтения статус этого тикета (заполняется
  только в списковых выдачах);
- `details` — произвольный JSON, специфичный для `type` (например,
  `{from, to}` для `status_changed`, `{comment_id}` для `new_comment`).

#### Известные значения `type` (список открыт)

| Значение | Когда создаётся | Группа настроек |
|---|---|---|
| `issue_assigned` | тикет назначен на участника | assignments |
| `unassigned` | тикет снят с участника | assignments |
| `assignee_changed` | у тикета сменился исполнитель (уведомляются подписчики, кроме старого/нового исполнителя) | assignments |
| `status_changed` | у тикета сменился статус (уведомляются подписчики; для дочернего тикета уведомление "всплывает" и подписчикам родительского) | status_changes |
| `priority_changed` | сменился приоритет тикета | updates |
| `start_date_changed` | сменилась дата начала | updates |
| `due_date_changed` | сменился срок | updates |
| `new_comment` | новый комментарий к тикету (кроме комментариев от системы) | comments |
| `mentioned` | упоминание `@участник`/`@squad`/`@all` в описании тикета или комментарии | comments |
| `reaction_added` | реакция на тикет или на комментарий пользователя | не участвует в приглушении по группам |
| `task_failed` | задача агента, связанная с тикетом, завершилась ошибкой | agent_activity |
| `quick_create_done` | "быстрое создание" тикета агентом успешно завершилось | не участвует в приглушении по группам |
| `quick_create_failed` | "быстрое создание" тикета завершилось ошибкой | не участвует в приглушении по группам |
| `quick_create_unconfirmed` | не удалось подтвердить, был ли тикет создан (нужно проверить вручную, чтобы не задваивать) | не участвует в приглушении по группам |
| `issue_subscribed` | пользователя подписали на тикет как создателя через агента | не участвует в приглушении по группам |
| `autopilot_paused` | автопилот поставлен на паузу после серии неудач | не участвует в приглушении по группам |

При `status_changed` в терминальный статус (`in_review`, `done`,
`cancelled`) старые элементы инбокса типа `task_failed` по этому же
тикету автоматически архивируются пачкой (публикуется
`inbox:batch-archived` с `reason: "issue_status_terminal"`), чтобы не
копить неактуальные уведомления о провале, если тикет всё же довели до
готовности.

#### Маршруты

| Метод | Путь | Поведение |
|---|---|---|
| GET | `/api/inbox`, `/api/inbox/archived` | Активные / архивные элементы текущего пользователя в текущем воркспейсе. |
| GET | `/api/inbox/unread-count` | Число непрочитанных в текущем воркспейсе. |
| GET | `/api/inbox/unread-summary` | Единственный маршрут инбокса, не привязанный к текущему воркспейсу — сводка непрочитанных по всем воркспейсам пользователя. |
| POST | `/api/inbox/mark-all-read` | Все элементы воркспейса → прочитаны. Событие `inbox:batch-read`. |
| POST | `/api/inbox/archive-all` | Все элементы воркспейса → в архив. |
| POST | `/api/inbox/archive-all-read` | Только уже прочитанные → в архив. |
| POST | `/api/inbox/archive-completed` | Элементы, чей связанный тикет/задача завершены → в архив. |
| POST | `/api/inbox/{id}/read` | Один элемент → прочитан. Событие `inbox:read`. |
| POST | `/api/inbox/{id}/archive` | Один элемент → в архив; если у него есть `issue_id`, заодно архивируются все прочие элементы того же получателя по тому же тикету ("схлопывание" цепочки уведомлений). Событие `inbox:archived`. |
| POST | `/api/inbox/{id}/unarchive` | Симметричная операция возврата из архива, включая связанные по тикету элементы. Событие `inbox:unarchived`. |

Массовые операции (`mark-all-read`, `archive-all*`) публикуют
`inbox:batch-read`/`inbox:batch-archived` с `{ recipient_id, count }` —
клиенту не нужно перечитывать список поэлементно, достаточно обновить
счётчик.

### 10. Настройки уведомлений (`/api/notification-preferences`)

Настройки — плоская карта "группа уведомлений → `all`|`muted`" на
пару (пользователь, воркспейс). Допустимые ключи группы:
`assignments`, `status_changes`, `comments`, `updates`,
`agent_activity`, `system_notifications`. Любой другой ключ или
значение, отличное от `all`/`muted`, — 400.

| Метод | Поведение |
|---|---|
| GET | Текущие настройки; если запись ещё не создавалась — пустой объект `preferences` (не 404 — отсутствие записи равносильно "ничего не заглушено"). |
| PATCH | Слияние: переданные группы перезаписываются, остальные сохраняют прежнее значение. |
| PUT | Полная замена: группы, не перечисленные в запросе, теряют сохранённое значение (в БД остаётся только то, что явно прислано). |

Обратите внимание: не все `type` инбокса относятся к группе настроек
(см. таблицу раздела 9) — уведомления вроде `reaction_added` или
`quick_create_*` не могут быть заглушены через этот механизм.

---

## Спорные места

Ниже — места контракта, где поведение сервера не сводится к простому
правилу и о которых implementer'у стоит знать заранее, а не выяснять их на
проде путём проб и ошибок.

1. **Единый actor-флаг «человек».** Проверка «только человек» блокирует только
   `task_token` и `cloud_pat`; обычный personal access token и сессионная
   cookie проходят как «человек», хотя PAT физически может стоять в CI. В
   таблицах это отражено как право `human`.
2. **Тройная модель доступа к конфиг-слоям.** `/api/workspace-config/**` и
   `/api/deployment/workspaces/{id}/config/**` — один и тот же обработчик с
   одним и тем же правилом доступа (owner/admin пространства ИЛИ
   deployment-admin), но разными путями резолва id пространства (заголовок/
   query vs URL) и разным набором admin-аудита при чтении. Я развёл их на два
   пути в контракте, но семантически это один ресурс, доступный двумя
   входами — реализатору стоит держать это в одной функции.
3. **`confirm_hint` в `DeploymentAdminPendingRequest`** — это готовая
   человекочитаемая инструкция с указанием id заявки, а не структурированные
   данные; реализатор может либо воспроизводить точный текст, либо
   договориться с фронтендом о собственной формулировке — по духу контракта
   важен факт (заявка создана, применяется отдельной операторской командой),
   а не точный текст подсказки.
4. **agent-actor как «анти-роль».** Несколько ручек (workspace-mcp-servers на
   запись, deployment-mcp-servers на запись) явно запрещены агенту-актору
   (когда актор запроса определён как агент), это отдельная проверка от
   требования «только человек» / ролей owner-admin-member. В `x-roles` перечислены
   допустимые роли участника, а запрет агента вынесен в описание — считаю
   нужным явно проговорить это в реализации, а не полагаться на то, что
   агент физически не имеет роли owner/admin.
5. **`/api/cloud-billing/**` и `DeploymentClientSecrets`/`EffectiveConfigView`**
   не имеют собственной валидации на этом уровне (кроме `checkout-sessions/
   {sessionId}` формата) — вся содержательная проверка (существование tier_id,
   валидность email и т.п.) происходит на стороне внешнего облачного сервиса,
   и наш сервер её не дублирует и не может продиагностировать заранее.

---

## Как проверялся этот контракт

Полный список маршрутов, зарегистрированных сервером, сопоставлен один в
один со списком путей и методов в `docs/50-api-contract.yaml` —
автоматической сверкой, до нулевого расхождения в обе стороны (ни одного
маршрута сервера вне контракта, ни одного маршрута контракта, которого нет
на сервере). Формы тел ответов для основных сценариев (регистрация и вход,
создание рабочего пространства, CRUD над задачами/проектами/метками/
комментариями/чатом/агентами, регистрация и heartbeat демона) дополнительно
проверены контрактными тестами (`e2e/contract`) против поднятого сервера с
реальной базой данных: каждый вызов сверен с задокументированной здесь и в
YAML схемой ответа. Итоговое покрытие (сколько операций контракта было
реально вызвано в проверке) печатается тестами при каждом прогоне — см.
`e2e/contract/README.md`.

## Приложение. CLI администратора деплоя (`admin`)

Источник — наблюдаемое поведение эталонной утилиты администратора (вывод `--help`). Новая реализация — `server2/cmd/admin`, собирается в бинарник `goosar_admin`; набор команд и флагов тот же. Утилита работает напрямую с БД сервера: читает `DATABASE_URL` из окружения, как и мигратор. Все изменения полномочий пишутся в аудит деплоя.

| Команда | Поведение |
|---|---|
| `list-pending` | Печатает ожидающие заявки на выдачу/отзыв роли deployment-admin (заявки создаются через API `/api/deployment/admins/**`). |
| `confirm <id>` | Исполняет одну ожидающую заявку (второй канал подтверждения). |
| `reject <id>` | Отклоняет одну ожидающую заявку, ничего не меняя в ролях. |
| `grant <email>` | Аварийная выдача роли deployment-admin существующему пользователю напрямую, минуя заявку; журналируется. |
| `gc-uploads [--dry-run] [--grace=168h] [--limit=500]` | Удаляет осиротевшие загрузки: вложения, не привязанные ни к задаче, ни к комментарию, ни к сообщению чата, старше `--grace`. `--dry-run` только печатает список. |
| `purge [--dry-run] [--chat=720h] [--tasks=720h] [--closed-issues=8760h] [--activity=720h] [--attachment-grace=168h]` | Применяет политику хранения: удаляет старые чаты, задачи агентов, закрытые задачи, активность, вложения по окнам. Окна по умолчанию берутся из переменных `GOOSAR_RETENTION_*`, флаг переопределяет одно окно. `--dry-run` печатает объём по каждому окну и ничего не удаляет. |
| `provision-roles` | Создаёт ролевые воркспейсы деплоя из включённых шаблонов воркспейсов. Идемпотентно: повторный запуск сообщает «пропущено» по каждой роли. Та же процедура выполняется сервером при старте, если `GOOSAR_ROLE_WORKSPACES=auto`. |
| `rotate-secrets --mcp [--mfa] [--dry-run]` | Перешифровывает все значения, запечатанные ключом `GOOSAR_MCP_SECRET_KEY`, текущим ключом (предыдущий ключ — для расшифровки). `--mfa` дополнительно покрывает TOTP-секреты. |
| `mfa-reset <email>` | Аварийный сброс второго фактора: удаляет TOTP и коды восстановления одного аккаунта и завершает его сессии. Выполняется сразу (не через заявку), журналируется. |
| `mcp-library seed [--dry-run]` | Идемпотентно (по имени) заполняет библиотеку MCP-серверов деплоя из переменных `GOOSAR_DEPLOYMENT_*_URL` для сервисов jira, confluence, ews, bitrix24, mcp-gateway. Пустой адрес — сервис пропускается. Запись, изменённая вручную после прошлого заполнения, не перезаписывается (печатается предупреждение). Требует `GOOSAR_MCP_SECRET_KEY`. |

Без аргументов или с `--help` утилита печатает справку по командам; неизвестная команда — ненулевой код выхода.

## Приложение. Переменные окружения сервера

Источник — прямой перебор всех мест, где основной процесс сервера (включая
мигратор и утилиту администратора деплоя `goosar_admin`, которые читают
`DATABASE_URL` так же, как сам сервер) читает переменные окружения, плюс
обёрток чтения (парсинг длительности/числа/булева значения с дефолтом,
разбор ключей шифрования и их `_PREVIOUS`-пары для ротации). Тестовые-only
переменные (включаются только в тестах, интеграционных гейтах и т.п.) в
таблицу не входят. Сверено с самодостаточным self-host docker-compose
стеком, файлом-примером окружения и `SELF_HOSTING.md` — столбец «в
установке» отмечает переменные, которые self-host стек реально прокидывает
в контейнер бэкенда (то есть уже поддержаны «из коробки» при установке;
переменная без этой отметки читается сервером, но self-host-стек её не
объявляет — оператор может задать её вручную поверх стека).

Переменные, которые по сути являются флагами возможностей (пустое значение
= функциональность выключена целиком, без ошибки), отмечены явно в
столбце «поведение».

### БД и запуск

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `DATABASE_URL` | Да, без неё процесс не стартует | нет | DSN `postgres://user:pass@host:port/db?sslmode=...` | Соединение с основной базой. Тот же формат читают мигратор и утилита администратора деплоя. | Да |
| `DATABASE_MAX_CONNS` | Нет | 25, либо `pool_max_conns` из URL, если он там задан | целое | Верхняя граница пула соединений к базе. | Нет |
| `DATABASE_MIN_CONNS` | Нет | 5, либо `pool_min_conns` из URL | целое | Нижняя граница пула соединений. | Нет |
| `PORT` | Нет | 8081 (в self-host стеке фиксирован на 8080 внутри контейнера) | целое | Порт HTTP-листенера. | Да |
| `APP_ENV` | Нет | пусто (не production) | строка, значимо только `production` | На `production` включает проверки безопасности при старте (отказ с небезопасным секретом подписи токенов, отказ без почтового транспорта), правило умолчания для формата и уровня логов, а также запрещает фиксированный код подтверждения входа. | Да |
| `GOOSAR_REPLICAS` | Нет | 1 | целое | При значении больше 1 требует рабочий `REDIS_URL` — без него сервер отказывается стартовать (иначе у каждой реплики свой отдельный лимитер запросов и свой realtime-хаб, что ломает многоузловое развёртывание). | Нет |
| `GOOSAR_SHUTDOWN_HOLD_DURATION` | Нет | 0 | неотрицательная Go-длительность (`300s`, `5m`) | Пауза после сигнала остановки перед началом штатного плавного выключения; платформенный grace period должен покрывать эту паузу плюс само выключение. | Да |
| `GOOSAR_MIGRATION_LOCK_TIMEOUT` | Нет | 5s | Go-длительность | Сколько мигратор ждёт захвата блокировки перед повтором. | Нет |
| `GOOSAR_MIGRATION_STATEMENT_TIMEOUT` | Нет | 0 (без таймаута) | Go-длительность | Таймаут одного SQL-выражения миграции. | Нет |
| `GOOSAR_MIGRATION_LOCK_RETRIES` | Нет | 5 | целое | Число повторов захвата блокировки миграции. | Нет |

### Аутентификация и сессии

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `JWT_SECRET` | Фактически да: на `APP_ENV=production` пустое значение или известный плейсхолдер — отказ старта | встроенный небезопасный dev-секрет с громким предупреждением в логе | произвольная строка (рекомендуется `openssl rand -hex 32`) | Секрет подписи токенов сессии. | Да (генерируется автоматически при установке) |
| `JWT_SECRET_PREVIOUS` | Нет | пусто | список отозванных секретов через запятую | Секреты, которые ещё принимаются для ПРОВЕРКИ подписи (не для новой подписи) — окно ротации без разлогина всех сразу; на `production` плейсхолдер в этом списке — тоже отказ старта. | Да |
| `COOKIE_DOMAIN` | Нет | пусто (host-only cookie) | доменное имя, не IP | Атрибут `Domain` у сессионной куки и куки CloudFront; значение-IP молча игнорируется с предупреждением (браузеры не принимают IP в `Domain`). | Да |
| `AUTH_TOKEN_TTL` | Нет | 2592000 (30 дней) | секунды целым числом или Go-длительность | Срок жизни токена сессии/куки. | Нет |
| `FRONTEND_ORIGIN` | Нет | адрес локального фронтенда для разработки | URL | Источник для вычисления флага `Secure` у куки (https → Secure), запасной вариант для списка разрешённых источников CORS/WebSocket и адрес в ссылках писем. | Да |
| `ALLOWED_ORIGINS` | Нет | пусто | список origin через запятую | Разрешённые источники CORS и WebSocket; приоритетнее `CORS_ALLOWED_ORIGINS`. | Нет |
| `CORS_ALLOWED_ORIGINS` | Нет | при пустом, как и `ALLOWED_ORIGINS`, — три локальных origin для разработки | список origin через запятую | То же самое, но на ступень ниже по приоритету; ниже — `FRONTEND_ORIGIN`. | Да |
| `GOOSAR_APP_URL` | Нет | выводится из `FRONTEND_ORIGIN` | URL | Публичный адрес веб-приложения — используется в письмах, в сравнении с «официальным облаком» и в части ответов API. | Да |
| `GOOSAR_PUBLIC_URL` | Нет | пусто | URL без хвостового `/` | Публичный адрес самого API из открытого интернета — используется, чтобы собрать абсолютные ссылки вебхуков автопилота и правильные команды настройки клиента в интерфейсе; за одноимённым обратным прокси можно не задавать (адрес соберётся из адресной строки браузера). Заголовки хоста намеренно не используются для его вывода, чтобы не давать подделывать адрес через них. | Да |
| `GOOSAR_TRUSTED_PROXIES` | Нет | пусто (доверять только адресу подключения) | список CIDR через запятую | Каким обратным прокси разрешено доверять заголовкам реального IP клиента — используется лимитером запросов к вебхукам и определением клиентского IP в целом; обязательна за реверс-прокси, иначе все реальные пользователи схлопываются в один IP-бакет лимитера. | Да |
| `ALLOW_SIGNUP` | Нет | true | булево | Разрешена ли регистрация новых пользователей вообще; отдаётся клиенту через публичный конфиг, переключение не требует пересборки фронтенда, только рестарт бэкенда. | Да |
| `ALLOWED_EMAILS` | Нет | пусто | список адресов через запятую | Точный allow-list адресов для входа/регистрации. | Да |
| `ALLOWED_EMAIL_DOMAINS` | Нет | пусто | список доменов через запятую | Allow-list доменов почты для входа/регистрации. | Да |
| `DISABLE_WORKSPACE_CREATION` | Нет | пусто (false) | булево | Полностью запрещает создание новых рабочих пространств любым участником — используется, чтобы после начальной раскатки перевести развёртывание на приём только по приглашениям. | Да |
| `GOOSAR_DEV_VERIFICATION_CODE` | Нет | пусто | 6 цифр строкой | Фиксированный код подтверждения входа для локальной разработки и детерминированной автоматизации; полностью игнорируется на `APP_ENV=production`. | Да |
| `GOOSAR_ROLE_WORKSPACES` | Нет | `auto` | `auto` \| `off`; иное значение — отказ старта | `auto` — при каждом старте сервер создаёт по одному ролевому рабочему пространству на каждый включённый шаблон, владелец — первый администратор деплоя (нужен заданный ключ шифрования конфигов MCP и хотя бы один администратор — без администратора ничего не создаётся, попытка повторяется на следующем старте); операция идемпотентна. `off` — роли ведёт оператор вручную. | Да |
| `GOOSAR_DEPLOYMENT_ADMIN_EMAILS` | Нет | пусто | список адресов через запятую | Сидирует роль администратора деплоя существующим пользователям, но только пока таблица администраторов пуста (первое развёртывание) — дальше база авторитетна, а расхождение со списком только логируется. Адреса, ещё не зарегистрированные, резолвятся в момент их регистрации. | Да |
| `REALTIME_METRICS_TOKEN` | Нет | пусто | произвольный секрет | Bearer-токен доступа к эндпойнту состояния realtime-подсистемы. Без него эндпойнт отвечает только прямым обращениям с адреса loopback без заголовков проксирования и 404 всем остальным — обязательна за реверс-прокси, иначе запросы, выглядящие как loopback только на уровне TCP, получают 404. | Нет |

### Почта

Ровно две независимые опции доставки; при отсутствии обеих на `production`
(или на профиле доставки perimeter) вход отказывает кодом ошибки вместо
того, чтобы напечатать код в лог, который пользователь прочитать не может.

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `RESEND_API_KEY` | Нет (один из двух транспортов) | пусто | ключ провайдера Resend | Наличие ключа включает отправку через Resend. Без ключа и без SMTP код входа печатается в лог процесса (только не на production). | Да |
| `RESEND_FROM_EMAIL` | Нет | `noreply@goosar.ru` | email | Адрес отправителя для писем через Resend; также запасной адрес отправителя для SMTP, если не задан `SMTP_FROM_EMAIL`. | Да |
| `SMTP_HOST` | Нет | пусто | хост или хост:порт | Если задан — имеет приоритет над Resend. | Да |
| `SMTP_PORT` | Нет | 25 | целое | Порт SMTP-relay; при 465 без явного `SMTP_TLS` режим TLS переключается на «сразу TLS» автоматически. | Да |
| `SMTP_USERNAME` | Нет | пусто | строка | Логин SMTP; пусто — неаутентифицированный relay. | Да |
| `SMTP_PASSWORD` | Нет | пусто | секрет | Пароль SMTP. | Да |
| `SMTP_FROM_EMAIL` | Нет | наследует `RESEND_FROM_EMAIL`; если не задано ни одно из двух — сервер отказывается стартовать | email | Адрес отправителя конверта/заголовка для писем через SMTP. | Да |
| `SMTP_TLS_INSECURE` | Нет | false | булево | Отключает проверку сертификата TLS — только для приватных/самоподписанных CA. | Да |
| `SMTP_TLS` | Нет | `starttls` | `starttls` \| `implicit` (алиасы `smtps`, `ssl`) | Режим установления TLS-соединения с relay; `implicit` обязателен для провайдеров, отдающих только порт 465 без STARTTLS. | Да |
| `SMTP_EHLO_NAME` | Нет | имя хоста машины | FQDN | Имя, объявляемое в EHLO/HELO; нужно задать реальное имя, если строгий relay (например корпоративный Google Workspace) обрывает соединение по умолчанию. | Да |

### OIDC/LDAP/MFA

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `GOOSAR_AUTH_METHODS` | Нет | пусто = только `email` | список через запятую из `email`, `oidc`, `ldap` | Какие способы входа доступны НА УРОВНЕ СЕРВЕРА, а не только в интерфейсе: метод, не перечисленный здесь, закрыт на самом эндпойнте, даже если ниже для него всё настроено; метод, перечисленный, но недонастроенный — просто не предлагается, о чём сервер пишет в лог при старте. | Да |
| `GOOSAR_TOTP_ISSUER` | Нет | «Goosar» | строка | Имя эмитента, которое показывает приложение-аутентификатор пользователя. Сам второй фактор включается человеком самостоятельно в настройках своего аккаунта; для хранения общего секрета обязателен заданный ключ шифрования конфигов MCP (см. группу «Деплой/политика»). | Да |
| `GOOSAR_OIDC_ISSUER` | Нет (нужна для метода `oidc`) | пусто | URL БЕЗ хвоста `/.well-known/openid-configuration` | Issuer провайдера OIDC (Keycloak, ADFS, единый идентификатор облачной организации, Okta, Authentik и т.п. — любой провайдер с discovery-документом). | Да |
| `GOOSAR_OIDC_CLIENT_ID` | Нет (нужна для `oidc`) | пусто | строка | Идентификатор клиента OIDC. | Да |
| `GOOSAR_OIDC_CLIENT_SECRET` | Нет | пусто = публичный клиент | секрет | PKCE (S256) применяется в любом случае и не опционален — секрет защищает эндпойнт токена, верификатор PKCE защищает редирект, это разные вещи. | Да |
| `GOOSAR_OIDC_REDIRECT_URL` | Нет (нужна для `oidc`) | пусто | URL | Должен ТОЧНО совпадать с адресом обратного вызова, зарегистрированным у провайдера. | Да |
| `GOOSAR_OIDC_SCOPES` | Нет | `openid,profile,email` | список через запятую | `openid` добавляется всегда, даже если не перечислен явно — без него провайдер не обязан вернуть id-токен. | Да |
| `GOOSAR_OIDC_ADMIN_CLAIM` | Нет | пусто | имя claim в токене | Вместе с `GOOSAR_OIDC_ADMIN_VALUE` задаёт правило сопоставления claim'а с ЗАЯВКОЙ на роль администратора деплоя (не прямую выдачу роли — заявку подтверждает оператор отдельной командой); должны быть заданы обе переменные или ни одной. | Да |
| `GOOSAR_OIDC_ADMIN_VALUE` | Нет | пусто | ожидаемое значение claim | См. выше. | Да |
| `GOOSAR_OIDC_DISPLAY_NAME` | Нет | локализованная подпись по умолчанию | строка | Текст на кнопке входа через OIDC. | Да |
| `GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL` | Нет | false (email провайдера должен быть подтверждён — `email_verified=true`) | булево | Разрешает принимать email без подтверждения провайдером. | Нет |
| `GOOSAR_LDAP_URL` | Нет (нужна для `ldap`) | пусто | `ldaps://host:636`, либо `ldap://host:389` строго вместе с `GOOSAR_LDAP_START_TLS=true` | Простой bind по обычному `ldap://` без StartTLS отправляет пароль в открытом виде, поэтому такая комбинация не отклоняется мягко, а метод не предлагается вовсе. | Да |
| `GOOSAR_LDAP_START_TLS` | Нет | false | булево | См. выше. | Да |
| `GOOSAR_LDAP_BIND_DN` | Нет | пусто = анонимный поиск (если каталог это разрешает) | DN сервисной учётки | Учётка, которой сервер ищет запись пользователя ПЕРЕД его собственным bind. | Да |
| `GOOSAR_LDAP_BIND_PASSWORD` | Нет | пусто | секрет | Пароль сервисной учётки поиска. | Да |
| `GOOSAR_LDAP_BASE_DN` | Нет | пусто | DN | Корень поиска в каталоге. | Да |
| `GOOSAR_LDAP_USER_FILTER` | Нет | встроенный фильтр, покрывающий оба варианта написания логина в AD | LDAP-фильтр с `%s`, экранируется по RFC 4515 | Фильтр поиска записи пользователя по введённому логину. | Да |
| `GOOSAR_LDAP_EMAIL_ATTR` | Нет | `mail` | имя атрибута LDAP | Из какого атрибута берётся email пользователя. | Да |
| `GOOSAR_LDAP_NAME_ATTR` | Нет | `displayName` | имя атрибута LDAP | Из какого атрибута берётся отображаемое имя. | Да |
| `GOOSAR_LDAP_ADMIN_GROUP` | Нет | пусто | DN группы | Та же логика заявки, что у `GOOSAR_OIDC_ADMIN_CLAIM`: членство в группе только файлит заявку на роль администратора деплоя, не выдаёт её напрямую. | Да |
| `GOOSAR_LDAP_DISPLAY_NAME` | Нет | локализованная подпись по умолчанию | строка | Текст на кнопке входа через LDAP. | Да |

### Хранилище

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `S3_BUCKET` | Нет (иначе — локальное файловое хранилище) | пусто | имя бакета БЕЗ суффикса `.s3.<регион>.amazonaws.com` | Наличие имени переключает хранилище вложений на S3-совместимое. | Да |
| `S3_REGION` | Фактически да, если задан `S3_BUCKET` и не задан `AWS_ENDPOINT_URL` (иначе сервер отказывается стартовать) | нет | строка региона | Регион бакета для настоящего AWS S3; неявного региона по умолчанию больше нет намеренно, чтобы загрузки не улетали молча на `us-west-2`. | Да |
| `AWS_ACCESS_KEY_ID` | Нет | пусто | ключ доступа | Учётные данные S3-совместимого хранилища. | Да |
| `AWS_SECRET_ACCESS_KEY` | Нет | пусто | секрет | См. выше. | Да |
| `AWS_ENDPOINT_URL` | Нет | пусто | URL | S3-совместимый эндпойнт (MinIO, RustFS, R2 и т. п.) вместо настоящего AWS. | Да |
| `S3_USE_PATH_STYLE` | Нет | пусто = true, если задан `AWS_ENDPOINT_URL` (для MinIO-подобных эндпойнтов), иначе false | булево | Режим адресации ключей объектов (path-style против virtual-hosted-style). | Да |
| `ATTACHMENT_DOWNLOAD_MODE` | Нет | `auto` | строка-режим | Как выдаются ссылки на скачивание вложений — напрямую из объектного хранилища или проксированием через сервер (нужно для внутренних адресов хранилища вроде `http://rustfs:9000`, недоступных браузеру/CLI напрямую). | Да |
| `ATTACHMENT_DOWNLOAD_URL_TTL` | Нет | 30 минут | Go-длительность | Срок жизни подписанной ссылки на скачивание вложения. | Да |
| `CLOUDFRONT_KEY_PAIR_ID` | Нет | пусто | id ключевой пары CloudFront | Для подписи ссылок CloudFront. | Да |
| `CLOUDFRONT_PRIVATE_KEY_SECRET` | Нет | `goosar/cloudfront-signing-key` | имя секрета во внешнем secret-manager | Альтернатива `CLOUDFRONT_PRIVATE_KEY` — откуда достаётся приватный ключ подписи. | Нет |
| `CLOUDFRONT_PRIVATE_KEY` | Нет | пусто | PEM-ключ целиком | Приватный ключ для подписи ссылок CloudFront. | Да |
| `CLOUDFRONT_DOMAIN` | Нет | пусто | домен | Публичный домен CDN перед объектным хранилищем; также расширяет политику безопасности контента (CSP) для изображений собственным доменом хранилища. | Да |
| `LOCAL_UPLOAD_DIR` | Нет | `./data/uploads` | путь на диске | Локальное файловое хранилище вложений — фоллбек, когда `S3_BUCKET` не задан. | Да |
| `LOCAL_UPLOAD_BASE_URL` | Нет | выводится из порта бэкенда | URL | Публичный адрес, под которым отдаются локально сохранённые файлы. | Нет |
| `GOOSAR_EXTERNAL_IMAGES` | Нет | `allow` | `allow` \| `block` \| `allowlist` (любое другое значение трактуется как `block`, отказ закрытый) | Политика показа внешних (не со своего домена) изображений из markdown-контента (комментарии, ответы агента): внешняя картинка в разметке — канал утечки адреса просматривающего на произвольный сторонний сервер. Вложения при этом всегда отображаются в любом режиме. Читается при старте процесса, отражается в заголовке политики безопасности контента для API-ответов; изменение требует рестарта. **Флаг возможности.** | Да |
| `GOOSAR_IMAGE_HOSTS` | Нет | пусто | список хостов через запятую (можно со схемой и портом) | Allow-list хостов для режима `allowlist`, а также «форточка» для внешних изображений вне markdown (хосты аватарок OAuth, CDN логотипов) на развёртываниях со строгим режимом. | Да |
| `GOOSAR_PROVISIONING_STORE` | Нет | пусто — эндпойнты provisioning отвечают отказом «не настроено» | `local` \| `oci` | Источник каталога пакетов (навыки, MCP-серверы, рантаймы), который сервер раздаёт клиентам. `local` переиспользует хранилище вложений (S3 или локальный диск) как адресуемый по содержимому склад пакетов; `oci` читает пакеты из OCI/ORAS-реестра. **Флаг возможности.** | Да |
| `GOOSAR_PROVISIONING_LOCAL_PREFIX` | Нет | `provisioning` | строка-префикс | Префикс ключей внутри бэкенда хранения для режима `local`. | Да |
| `GOOSAR_PROVISIONING_OCI_URL` | Фактически да для режима `oci` | пусто | URL реестра | Без него режим `oci` остаётся выключенным. | Да |
| `GOOSAR_PROVISIONING_OCI_REPOSITORY` | Нет | пусто | строка | Опциональный namespace репозитория пакетов в реестре. | Да |
| `GOOSAR_PROVISIONING_OCI_USERNAME` | Нет | пусто = анонимный доступ | строка | Логин к приватному реестру. | Да |
| `GOOSAR_PROVISIONING_OCI_PASSWORD` | Нет | пусто | секрет | Пароль к приватному реестру. | Да |
| `GOOSAR_PROVISIONING_OCI_PACKAGES` | Нет | пусто = опросить каталог реестра | список `имя@версия` через запятую | Явный список пакетов для зеркалирования на реестрах без сводного каталога (например GHCR). | Да |
| `GOOSAR_PROVISIONING_SYNC_TIMEOUT` | Нет | 3 минуты | Go-длительность | Бюджет времени на первичное зеркалирование комплекта пакетов при старте; по истечении старт продолжается без комплекта, а не блокируется. | Да |

### Realtime/Redis

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `REDIS_URL` | Нет | пусто = лимитер запросов работает в памяти одного процесса | DSN `redis://` | Общее подключение переиспользуется fan-out хабом realtime-событий, кешем персональных токенов доступа, кешем токенов демона и многоузловым лимитером запросов. Без него однопроцессное развёртывание защищено полностью, но при нескольких репликах бюджеты лимитера у каждой свои (см. `GOOSAR_REPLICAS` выше). | Нет |
| `REDIS_DISABLE_CLIENT_NAME` | Нет | false | булево | Пропускает согласование имени клиента при каждом подключении к Redis — нужно для managed Redis с урезанным ACL, где эта команда запрещена. | Нет |
| `REALTIME_RELAY_MODE` | Нет | `sharded` | `sharded` \| `dual` \| `legacy` (иное значение — предупреждение и `sharded`) | Режим работы realtime-relay поверх Redis Streams. | Нет |
| `REALTIME_RELAY_SHARDS` | Нет | встроенный дефолт | положительное целое | Число шардов relay. | Нет |
| `REALTIME_RELAY_STREAM_MAXLEN` | Нет | встроенный дефолт | положительное целое | Верхняя граница длины одного Redis Stream. | Нет |
| `REALTIME_RELAY_XREAD_COUNT` | Нет | встроенный дефолт | положительное целое | Размер батча чтения из Redis Stream. | Нет |
| `REALTIME_RELAY_XREAD_BLOCK` | Нет | встроенный дефолт | Go-длительность | Таймаут блокирующего чтения потока. | Нет |
| `REALTIME_RELAY_REPLAY_GRACE` | Нет | встроенный дефолт | Go-длительность | Окно повторной доставки пропущенных событий после переподключения клиента. | Нет |
| `GOOSAR_RUNTIME_RECONNECT_GRACE` | Нет | 3 часа (значения меньше ~150 секунд фактически ограничиваются окном свежести heartbeat) | положительная Go-длительность | Сколько времени офлайновому рантайму/демону даётся на переподключение, прежде чем его незавершённые задачи считаются провалившимися, а отложенные попытки переподключения истекают. | Да |

### Лимиты

Каждый параметр читается при старте; изменение требует рестарта, не
пересборки. Превышение бюджета отвечает HTTP 429 с телом об ошибке
превышения лимита.

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `RATE_LIMIT_AUTH` | Нет | 5 в минуту на IP | целое | Лимит на отправку кода входа. | Да |
| `RATE_LIMIT_AUTH_VERIFY` | Нет | 20 в минуту на IP | целое | Лимит на проверку кода/ссылки входа. | Да |
| `RATE_LIMIT_AUTH_EMAIL` | Нет | 10 в минуту на адрес почты | целое | Лимит по конкретному почтовому адресу — независим от лимита по IP, останавливает распыление по одному ящику с пула адресов. | Да |
| `RATE_LIMIT_MFA_VERIFY` | Нет | 10 | целое | Лимит попыток ввода второго фактора — считается не по IP и не по аккаунту, а по конкретному незавершённому тикету входа (шестизначный код легко угадать; лимит по аккаунту стал бы способом заблокировать чужой вход). | Да |
| `RATE_LIMIT_TOKEN` | Нет | 20 в час на пользователя | целое | Лимит на выпуск CLI-токена и операций с персональными токенами доступа (создание/продление/отзыв). | Да |
| `RATE_LIMIT_API` | Нет | 600 в минуту | целое | Общий потолок на прочие авторизованные маршруты API — защита от злоупотребления, не формирование трафика. | Да |
| `RATE_LIMIT_CONTACT_SALES` | Нет | 5 в час на IP | целое | Лимит на форму связи с продажами. | Да |
| `RATE_LIMIT_EXPORT` | Нет | 3 в час | целое | Лимит на запуск экспорта воркспейса/данных пользователя. | Да |
| `RATE_LIMIT_JOIN` | Нет | 20 в час | целое | Лимит на присоединение к рабочему пространству. | Да |
| `RATE_LIMIT_TRUSTED_PROXIES` | Нет | пусто = падает на `GOOSAR_TRUSTED_PROXIES` | список CIDR через запятую | Каким обратным прокси доверяет именно лимитер запросов; задаётся отдельно от общего списка только если лимитеру нужен другой список прокси, чем остальному серверу. | Да |

### Интеграции

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `GITHUB_APP_SLUG` | Нет (обе нужны для кнопки подключения GitHub и приёма вебхуков) | пусто | строка — хвост адреса приложения GitHub | Слаг приложения GitHub. | Да |
| `GITHUB_WEBHOOK_SECRET` | Нет | пусто | секрет | Проверка подписи входящих вебхуков GitHub. | Да |
| `GITHUB_APP_ID` | Нет (нужна для карточек PR с CI/готовностью к слиянию и подключения репозитория из списка GitHub) | пусто | числовой ID приложения | Идентификатор приложения GitHub для минтинга короткоживущих токенов установки. | Да |
| `GITHUB_APP_PRIVATE_KEY` | Нет | пусто | PEM-блок целиком, с переносами строк | Приватный ключ приложения GitHub. | Да |
| `GITHUB_TOKEN` | Нет | пусто | токен доступа | Используется при импорте навыков из GitHub; никогда не подставляется автоматически, если источник GitHub отключён политикой источников навыков. | Нет |
| `GOOSAR_VCS_INTEGRATION_ENABLED` | Нет | false | булево | Включает раздел интеграции с самостоятельно размещёнными Git-провайдерами (это отдельная от GitHub функциональность, недоступная в управляемом облаке — только дляself-host). Оба условия ниже обязательны вместе. **Флаг возможности.** | Да |
| `GOOSAR_VCS_SECRET_KEY` | Нет (но нужна, чтобы функциональность выше реально заработала) | пусто | base64 от 32 байт (`openssl rand -base64 32`) | Шифрует токен доступа и секрет вебхука каждого рабочего пространства при хранении. **Флаг возможности**: без ключа интеграция молча остаётся выключенной, без ошибки. | Да |
| `GOOSAR_VCS_SECRET_KEY_PREVIOUS` | Нет | пусто | список retired-ключей через запятую | Ключи, ещё принимаемые для расшифровки в окне ротации. | Да |
| `GOOSAR_SLACK_SECRET_KEY` | Нет | пусто | base64 от 32 байт | Ключ включает интеграцию со Slack (модель «принеси свой сокет», по одной установке на воркспейс). **Флаг возможности**: без ключа интеграция молча выключена. | Да |
| `GOOSAR_SLACK_SECRET_KEY_PREVIOUS` | Нет | пусто | список retired-ключей через запятую | Ключи для расшифровки в окне ротации. | Да |
| `COMPOSIO_API_KEY` | Нет | пусто | ключ провайдера | Включает интеграцию Composio (дополнительно нужен включённый флаг соответствующей функциональности). **Флаг возможности.** | Нет |
| `COMPOSIO_STATE_SECRET` | Нет | пусто = выводится хешированием из `JWT_SECRET` | секрет | Подписывает `state`-параметр OAuth-потока Composio. | Нет |
| `COMPOSIO_CALLBACK_BASE_URL` | Нет | пусто = берётся `GOOSAR_PUBLIC_URL`/`GOOSAR_APP_URL` | URL | Базовый адрес для обратного вызова OAuth Composio. | Нет |
| `GOOSAR_CLOUD_FLEET_URL` | Нет | пусто | URL | Адрес облачного пула рантаймов; приоритетнее `GOOSAR_FLEET_URL`. | Нет |
| `GOOSAR_FLEET_URL` | Нет | пусто | URL | Запасное имя той же переменной. | Нет |
| `GOOSAR_CLOUD_FLEET_TIMEOUT` | Нет | 35 секунд | Go-длительность | Таймаут обращения к облачному пулу рантаймов. | Да |
| `GOOSAR_RUNTIME_CONFIG_PATH` | Нет | встроенный каталог рантаймов и моделей | путь к YAML-файлу | Переопределяет каталог поддерживаемых рантаймов и их моделей, которым пользуются расчёт стоимости в бизнес-метриках и список допустимых провайдеров. | Нет |

### LLM

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `GOOSAR_LLM_API_KEY` | Нет | пусто = внутренний слой LLM выключен, вызовы молча отпадают | ключ upstream-провайдера (OpenAI-совместимый) | Включает внутренний слой LLM-хелперов (например генерация заголовка чата) — сквозной проход к произвольному внешнему OpenAI-совместимому эндпойнту публично больше не предлагается, доступ только внутренний. Требует одновременно заданный `GOOSAR_LLM_BASE_URL`, иначе слой отказывается работать с явной ошибкой (чтобы не уйти молча на публичный адрес провайдера по умолчанию). **Флаг возможности.** | Да |
| `GOOSAR_LLM_BASE_URL` | Фактически да, если задан `GOOSAR_LLM_API_KEY` | нет | URL upstream | Базовый адрес провайдера LLM или шлюза. | Да |
| `GOOSAR_LLM_DEFAULT_MODEL` | Нет | встроенный небольшой дефолт | строка id модели | Модель по умолчанию, когда запрос её не указал явно. | Да |
| `GOOSAR_DEPLOYMENT_LLM_API_BASE` | Нет | пусто | URL | Адрес LLM-контура заказчика — только подсказка, отдаваемая клиентам через публичный конфиг и запечённые настройки десктоп-клиента; реальные учётные данные для выдачи клиентского секрета берутся из `GOOSAR_LLM_*` выше, без них выдача секрета не работает, даже если это поле заполнено. | Да |
| `GOOSAR_DEPLOYMENT_LLM_MODEL` | Нет | пусто | строка | Модель LLM-контура заказчика — тоже только подсказка. | Да |

### Деплой/политика

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `GOOSAR_DELIVERY_PROFILE` | Нет | пусто = `cloud` | `cloud` \| `perimeter`; иное значение — отказ старта | КАК развёртывание доставляется и обновляется. `cloud` — нынешнее публичное поведение без изменений. `perimeter` — управляемый оператором закрытый контур: принудительно выключаются все каналы самообновления клиентов (автообновление демона, обновление CLI, апдейтер десктоп-приложения) независимо от их собственных настроек; значение отдаётся клиентам через публичный конфиг. Self-host-бутстрап при первой установке пишет `perimeter` — самостоятельный хостинг и есть продукт для закрытого контура. | Да |
| `GOOSAR_DEPLOYMENT_PROFILE` | Нет | пусто = `perimeter` | `perimeter` \| `demo` \| `dev` \| `local`; иное значение — отказ старта | КАКОЙ ИМЕННО это стенд — вопрос, отдельный от способа доставки выше. `perimeter` — закрытый контур заказчика: регистрация закрыта, ничего не предзаполняется без явного запроса (без `ALLOWED_EMAIL_DOMAINS`/`ALLOWED_EMAILS` на этом профиле войти не сможет никто, включая устанавливающего). Значение отражается в статусе развёртывания и в собираемом диагностическом пакете. | Да |
| `GOOSAR_SKILL_SOURCES` | Нет | пусто = дефолт профиля доставки (на `cloud` — все источники включены, на `perimeter` — все выключены) | список через запятую из внешних источников навыков, либо `none`; неизвестное имя — отказ старта | Какие внешние источники навыков (поиск и импорт из внешнего каталога навыков, импорт из внешнего каталога скриптов, импорт из GitHub) сервер может опрашивать от имени пользователя. На `perimeter` по умолчанию ни один запрос пользователя и ни одно стороннее содержимое навыка не покидают контур; встроенные шаблоны агентов при этом продолжают работать в любом случае — их навыки зашиты в сам бинарник. Отключённый источник отвечает явным отказом с именем этой переменной, публичный конфиг публикует список включённых, чтобы интерфейс прятал недоступные варианты импорта. **Флаг возможности** (в части «включить/выключить конкретный источник»). | Да |
| `GOOSAR_MCP_ALLOWED_HOSTS` | Нет | поведение по профилю доставки: на `cloud` пусто = без ограничений, на `perimeter` пусто = встроенный корпоративный базовый список | список хостов через запятую | Какие хосты вправе указывать записи MCP-серверов в конфиге агента; проверяется и в адресе удалённой записи, и в любом `http(s)://`-адресе, встроенном в аргументы или переменные окружения записи (петлевой прокси-трафик исключён из проверки). | Да |
| `GOOSAR_MCP_ALLOWED_COMMANDS` | Нет | та же логика по профилю | список исполняемых файлов по базовому имени | Какие команды вправе запускать stdio-записи MCP-сервера; записи конфига обязаны указывать голое имя команды, путь-квалифицированные команды отклоняются. Никогда нельзя допускать в список интерпретаторы или раннеры общего назначения (это давало бы произвольное исполнение кода). | Да |
| `GOOSAR_ALLOWED_PROVIDERS` | Нет | та же логика по профилю | список кодов рантаймов через запятую | Какие рантаймы вправе брать задачи агентов на этом развёртывании; неизвестный код — отказ старта. Действующий список публикуется в конфиге для клиента, чтобы интерфейс прятал недоступные варианты. **Флаг возможности.** | Да |
| `GOOSAR_OFFICIAL_CLOUD_HOST` | Нет | пусто | хост | Используется, чтобы отличить «официальное управляемое облако» от прочих развёртываний при подготовке подсказок настройки клиента. | Нет |
| `GOOSAR_MIN_DAEMON_VERSION` | Нет | встроенный минимум | версия строкой, либо `none`/`off` для отключения проверки | Минимальная версия клиента-демона, которой сервер выдаёт задачи; более старой версии отказывает во взятии задачи с кодом «демон устарел», но регистрация, heartbeat и отчёт о результате продолжают работать как обычно. Перечитывается на каждый запрос, рестарт не нужен. | Да |
| `GOOSAR_MCP_SECRET_KEY` | Фактически да на профиле `perimeter` (иначе отказ старта); на прочих профилях — нет, но со громким предупреждением в логе | пусто | base64 от 32 байт | Шифрует встраиваемую в конфиг агента чувствительную часть (учётные данные корпоративных MCP-шлюзов и т. п.) перед записью в базу; при первом появлении ключа существующие незашифрованные строки перешифровываются фоновой задачей при старте. Без ключа сервер продолжает работать, но хранит эти данные в открытом виде — что на закрытом контуре как раз то, чего такое развёртывание должно избегать. Ключ также нужен второму фактору входа (см. `GOOSAR_TOTP_ISSUER`) и режиму `GOOSAR_ROLE_WORKSPACES=auto`. Удалять ключ после того, как данные уже зашифрованы им, нельзя — без ключа они не читаются. | Да |
| `GOOSAR_MCP_SECRET_KEY_PREVIOUS` | Нет | пусто | список retired-ключей через запятую | Ключи для расшифровки в окне ротации; общая схема — для каждого `GOOSAR_*_SECRET_KEY` есть своя `_PREVIOUS`-пара из списка отозванных, всё ещё годных для расшифровки, пока новый ключ делает всё новое шифрование. | Да |
| `GOOSAR_AUDIT_RETENTION_DAYS` | Нет | 365 | целое дней; 0 = хранить вечно | Срок хранения ОБОИХ журналов аудита — административного (операции над деплоем) и аутентификационного (входы, отказы во входе, обращения к секретам); ежедневная задача удаляет строки старше этого срока. | Да |
| `GOOSAR_DEPLOYMENT_JIRA_URL` | Нет | пусто | URL | Адрес корпоративного Jira заказчика — подсказка, отдаваемая клиентам через публичный конфиг (веб) и запечённые настройки (десктоп); пусто = нейтральные подсказки без пресета. | Да |
| `GOOSAR_DEPLOYMENT_CONFLUENCE_URL` | Нет | пусто | URL | То же для Confluence. | Да |
| `GOOSAR_DEPLOYMENT_EWS_URL` | Нет | пусто | URL | То же для почтового сервера по протоколу EWS. | Да |
| `GOOSAR_DEPLOYMENT_MAIL_DOMAIN` | Нет | пусто | доменное имя | То же для корпоративного почтового домена. | Да |
| `GOOSAR_DEPLOYMENT_BITRIX24_URL` | Нет | пусто | URL | То же для Bitrix24; дополнительно эту переменную читает отдельная команда сидирования библиотеки MCP-серверов деплоя (заполняет соответствующую запись каталога, если адрес задан, и пропускает сервис, если пусто). | Да |
| `GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL` | Нет | пусто | URL | Адрес корпоративного MCP-шлюза; так же участвует в сидировании библиотеки MCP-серверов. | Да |

### Хранение/retention

Значения — в Go-синтаксисе длительности (единицы `s`/`m`/`h`; суток как
единицы нет, 30 суток записываются как `720h`). Нераспознанное значение —
предупреждение в лог и откат к дефолту.

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `GOOSAR_SCHEDULER_AUDIT_RETENTION` | Нет | 720h (30 дней) | Go-длительность; 0 = хранить всё | Срок хранения строк аудита планировщика (по одной строке на сочетание задания, области и момента плана) — без ограничения это быстрорастущая таблица. | Да |
| `GOOSAR_UPLOAD_GC_GRACE` | Нет | 168h (7 дней) | Go-длительность; 0 = отключить очистку | Сколько ждать, прежде чем сборщик осиротевших вложений (не привязанных ни к задаче, ни к комментарию, ни к сообщению чата) удалит вложение и сам объект — черновик может пролежать в браузере долго, поэтому окно должно это учитывать. | Да |
| `GOOSAR_HYGIENE_SWEEP_INTERVAL` | Нет | 1h | Go-длительность | Как часто запускаются оба сборщика выше (аудит планировщика и осиротевшие вложения). | Да |
| `GOOSAR_RETENTION_CHAT` | Нет | пусто = хранить вечно | Go-длительность | Окно хранения истории чата. | Да |
| `GOOSAR_RETENTION_TASKS` | Нет | пусто = вечно | Go-длительность | Окно хранения задач агентов. | Да |
| `GOOSAR_RETENTION_CLOSED_ISSUES` | Нет | пусто = вечно | Go-длительность | Окно хранения закрытых задач/issue. | Да |
| `GOOSAR_RETENTION_ACTIVITY` | Нет | пусто = вечно | Go-длительность | Окно хранения ленты активности. | Да |
| `GOOSAR_ATTACHMENT_PURGE_GRACE` | Нет | 168h (7 дней) | Go-длительность | Дополнительная отсрочка перед физическим удалением вложений, попавших под окна хранения выше — все окна retention пустые по умолчанию намеренно: удаление истории команды или чата это решение оператора, а не то, что продукт вправе предполагать за него. | Да |
| `GOOSAR_EXPORT_DIR` | Нет | путь внутри тома вложений | путь на диске | Куда пишутся файлы экспорта рабочего пространства/данных пользователя — единственный путь на запись, доступный контейнеру с read-only корневой файловой системой. | Да |
| `GOOSAR_EXPORT_TIMEOUT` | Нет | 2 часа | положительная Go-длительность | Таймаут сборки одного задания экспорта. | Да |
| `GOOSAR_EXPORT_MAX_BYTES` | Нет | встроенный дефолт | целое, байт | Верхняя граница размера архива экспорта. | Да |
| `GOOSAR_EXPORT_RETENTION` | Нет | 168h (7 дней) | Go-длительность | Срок хранения готового файла экспорта перед автоматическим удалением. | Да |

### Наблюдаемость

| Переменная | Обязательна? | Значение по умолчанию | Формат | Что меняет | В установке |
|---|---|---|---|---|---|
| `METRICS_ADDR` | Нет | пусто = метрики Prometheus выключены целиком | `host:port` | Адрес, на котором поднимается listener метрик; сбор HTTP-метрик запросов начинается только когда этот listener включён. По умолчанию рекомендуется биндить на loopback либо закрывать доступ приватной сетью/allowlist — эндпойнт не предназначен для публичного входного шлюза. **Флаг возможности.** | Да (self-host по умолчанию оставляет пустым; отдельный оверлей мониторинга переопределяет на публичный для контейнерной сети адрес) |
| `POSTHOG_API_KEY` | Нет | пусто = аналитика работает в режиме заглушки, ничего не отправляется | ключ провайдера PostHog | Включает отправку продуктовых событий (воронка привлечение → активация → расширение). **Флаг возможности.** | Да |
| `POSTHOG_HOST` | Нет | `https://us.i.posthog.com` | URL | Адрес инстанса PostHog. | Да |
| `ANALYTICS_FRONTEND_ENABLED` | Нет | false | булево | Явный opt-in на передачу ключа и адреса PostHog в браузер через публичный конфиг, чтобы фронтенд тоже отправлял свою телеметрию ошибок; по умолчанию серверный ключ никогда не попадает в браузер незаметно для оператора. | Да |
| `ANALYTICS_DISABLED` | Нет | false | булево | Принудительно переводит клиент аналитики в режим заглушки даже при заданном ключе — для CI или явного отключения. | Да |
| `ANALYTICS_ENVIRONMENT` | Нет | выводится из `APP_ENV` и нормализуется к `production`/`staging`/`dev` | строка | Переопределяет свойство события `environment`, отправляемое в PostHog. | Да |
| `LOG_FORMAT` / `GOOSAR_LOG_FORMAT` | Нет | текстовый формат при прямом запуске бинарника; self-host-стек фиксирует json | `json` (одна строка-объект с временем в RFC3339 — формат для систем анализа логов) \| `text` (для чтения человеком) | Формат логов приложения (пишутся в stderr; отдельный журнал аудита владеет stdout). При заданных обеих переменных побеждает `GOOSAR_`-версия. | Да |
| `LOG_LEVEL` / `GOOSAR_LOG_LEVEL` | Нет | `info` на `APP_ENV=production`, иначе `debug` | `debug` \| `info` \| `warn` \| `error` | Уровень логирования. При заданных обеих переменных побеждает `GOOSAR_`-версия. | Да |

### Прочее

Пусто намеренно: у всех переменных, которые сервер читает, нашлось место в
одной из групп выше (минимальная версия демона, которой сервер выдаёт
задачи, — это политика деплоя, поэтому она в группе «Деплой/политика», а не
здесь).

### Переменные, не относящиеся к серверу

Ниже — переменные, которые действительно существуют в кодовой базе, но
относятся не к центральному серверу, а либо к CLI/локальному демону,
исполняющему задачи агентов на машине пользователя (регистрация и
heartbeat демона, опрос и запуск конкретных рантаймов, garbage collection
рабочих каталогов задач, локальный кеш HTTP-клиента CLI и т. п. —
десятки переменных вида `GOOSAR_DAEMON_*`, `GOOSAR_RUNTIME_<код>_*`,
`GOOSAR_GC_*`, `GOOSAR_AGENT_*`, `GOOSAR_TASK_*`, а также окружение самого
исполняемого рантайма вроде переменной окружения кодового ассистента,
задающей каталог его рабочих файлов), либо к клиентской библиотеке CLI
(таймаут HTTP-запросов, отладочный вывод, путь к файлу доверенных
сертификатов). Эти переменные читаются на машине оператора/исполнителя,
а не в процессе центрального сервера, и не входят в контракт сервера:
их поддержка — предмет отдельной спецификации клиента/демона, не этого
документа.

## Спорные места (переменные окружения)

1. **Свежие `GOOSAR_DEPLOYMENT_*_URL` не проверяются форматом.** Сервер
   принимает эти адреса как есть и отдаёт клиентам как подсказки — никакой
   валидации, что это действительно рабочий URL, на этом уровне нет; если
   заказчик опечатается, узнают об этом только по факту нерабочей
   подсказки в интерфейсе.
2. **`ALLOWED_ORIGINS` и `CORS_ALLOWED_ORIGINS` — де-факто одна и та же
   настройка с двумя именами и разным приоритетом.** В self-host-стеке
   задаётся только вторая; первая существует в коде, но нигде в
   поставляемых сценариях установки не документируется как то, что стоит
   трогать — реализатору стоит решить, схлопывать ли их в одну переменную
   или сохранять обе ради обратной совместимости.
3. **Значение по умолчанию `GOOSAR_DEPLOYMENT_PROFILE=perimeter` при пустой
   переменной — это самый строгий профиль, а не самый мягкий.** Это
   осознанный выбор (закрытая регистрация по умолчанию безопаснее открытой),
   но acceptance-тест, поднимающий сервер без единой переменной окружения,
   получит закрытую для входа систему, если явно не задать
   `ALLOWED_EMAIL_DOMAINS`/`ALLOWED_EMAILS`.
4. **`GOOSAR_MCP_SECRET_KEY` — не просто «настройка шифрования», а
   предпосылка для нескольких независимых возможностей одновременно**
   (роль-воркспейсы, второй фактор входа, шифрование MCP-конфигов) —
   отсутствие ключа не ломает ни одну из них с ошибкой, а тихо понижает
   поведение (роли не создаются, TOTP не сохраняется, конфиг хранится
   открытым текстом с предупреждением в логе). Стоит явно решить в
   реализации, поднимать ли уровень этих предупреждений при отсутствии
   ключа на профиле `perimeter`, а не оставлять их равноправными с прочими
   стартовыми логами.
