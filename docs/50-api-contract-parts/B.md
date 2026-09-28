# Контракт API — часть B: рабочие пространства, интеграции, деплой-администрирование

Источник поведения — маршруты `server/cmd/server/router.go` (строки 663–875) и их обработчики в
`server/internal/handler/**`. Формы ответов сверены с `packages/core/api/client.ts` и
`packages/core/api/schemas.ts`. Общие схемы `Error`, `User`, `Workspace`, `Member`,
security schemes `cookieAuth`/`bearerAuth`/`daemonAuth` и параметр `WorkspaceSlugHeader`
определяет другой фрагмент контракта — здесь они только referenced по имени.

## Как читать таблицы

- **Права** — кто может вызвать: `owner`/`admin`/`member` — роль в пространстве;
  `deployment-admin` — отдельная роль уровня деплоя (не связана с ролью в
  конкретном пространстве); `human` — вызывающий не должен быть агентом-задачей
  (`X-Actor-Source: task_token`) и не облачным PAT-раннером (`cloud_pat`) —
  personal access token и обычная сессия под этот запрет не попадают;
  `any-authenticated` — достаточно быть аутентифицированным любым способом
  (сессия, PAT, task-токен, cloud PAT).
- Все ручки этого фрагмента лежат под общим `middleware.Auth` (сессионная cookie
  с CSRF-проверкой, либо Bearer: PAT `gsr_…`, задача-токен `mat_…`, облачный PAT
  `gsln_…`, либо JWT-сессия как bearer) и общим лимитом **600 запросов/мин на
  пользователя-или-IP**. Дополнительные лимиты указаны отдельно в столбце
  «лимит».
- Пространство, где нет `{id}`/`{workspaceId}` в пути (`/api/workspace-config`,
  `/api/workspace-mcp-servers`, `/api/deployment-mcp-servers`,
  `/api/provisioning/**`, `/api/assignee-frequency`, `/api/status`,
  `/api/effective-config`, `/api/deployment-policy`), определяется по
  приоритету: заголовок `X-Workspace-Slug` → query `workspace_slug` → заголовок
  `X-Workspace-ID` → query `workspace_id`. Отсутствие того и другого — 400.

---

## 1. `/api/workspaces/**`

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

### Валидация (workspaces)

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
  (`vcs.ErrUnauthorized` → 400, сетевой сбой → 502); требует настроенного на
  сервере ключа шифрования секретов (иначе 503 при отсутствии
  `GOOSAR_VCS_SECRET_KEY`).
- GitHub `return_to` — только `github` или `repositories`.
- Slack BYO: `bot_token`/`app_token` проверяются вызовом к Slack API; конфликт
  привязки team различает три случая (другой агент в этом же пространстве,
  архивный агент, другое пространство) — все 409 с разным текстом.

---

## 2. `/api/slack/binding/redeem`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| POST `/api/slack/binding/redeem` | any-authenticated | Погашает одноразовый токен привязки Slack-аккаунта к вызывающему пользователю (токен выдаёт Slack-бот). 403, если не участник соответствующего пространства; 409, если Slack-аккаунт уже привязан к другому пользователю; 410, если токен недействителен/истёк | — |

---

## 3. `/api/integrations/composio/**`

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

## 4. `/api/invitations/**` (личные приглашения вызывающего)

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/invitations` | any-authenticated | Мои pending-приглашения (по `user_id` и по email) | — |
| GET `/api/invitations/{id}` | any-authenticated | Карточка; 403 `invitation_not_yours`, если адресовано не вызывающему | — |
| POST `/api/invitations/{id}/accept` | any-authenticated | Принять: нужен статус pending и непросроченный срок (иначе 400/410) | создаёт членство; первое принятие помечает онбординг завершённым; realtime `member.added` + `invitation.accepted`; уведомление раннеров; аналитика `team_invite_accepted` (+ `onboarding_completed` при первом онбординге) |
| POST `/api/invitations/{id}/decline` | any-authenticated | Отклонить (нужен статус pending) | realtime `invitation.declined` |

---

## 5. `/api/tokens/**` (личные PAT)

Общий лимит группы — **20 запросов/час на пользователя-или-IP** (поверх
базового лимита 600/мин).

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/tokens` | human | Список PAT вызывающего (без значений) | — |
| POST `/api/tokens` | human | Создать PAT; `name` обязателен; `expires_in_days` > 0 или бессрочный | значение токена в открытом виде — только в этом ответе; аудит `pat.created` |
| POST `/api/tokens/current/renew` | human (аутентификация именно Bearer-PAT) | Продлить PAT, которым выполнен запрос, если до истечения ≤ 7 дней (ещё на 90 дней); иначе `renewed:false` без изменений; 400, если запрос выполнен не PAT | — |
| DELETE `/api/tokens/{id}` | human | Отозвать PAT (идемпотентно — 204 даже если уже не существует) | инвалидация PAT-кэша; аудит `pat.revoked` |

---

## 6. `/api/cloud-billing/**`

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

## 7. `/api/deployment/**`

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

### Двухканальное подтверждение состава deployment-admin

`POST /admins` и `DELETE /admins/{userId}` не меняют состав роли напрямую — они
только фиксируют заявку (`202 Accepted`, поле `confirm_hint` — готовая
инструкция) и пишут запрос в admin-аудит. Применяет заявку оператор отдельной
серверной командой вне HTTP API (список/подтверждение/отклонение). Подтверждение
повторно проверяет все инварианты (роль ещё нужна, нельзя остаться без единого
администратора) и пишет ещё одну запись аудита.

### Валидация (deployment)

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

## 8. `/api/assignee-frequency`, `/api/status`

Обе — под членством в пространстве (без явного `{id}` в пути, workspace
резолвится по заголовку/query, см. правило в начале документа).

| Метод и путь | Права | Поведение |
|---|---|---|
| GET `/api/assignee-frequency` | member | Частота назначений исполнителей вызывающим (сумма по изменениям в активностях и по назначениям при создании задачи), для сортировки подсказок |
| GET `/api/status` | member | Диагностическая сводка на лету: кто вызывающий (человек/агент), рантаймы, provisioning, MCP, периметр, LLM — ничего не сохраняет |

---

## 9. `/api/provisioning/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/manifest` | member | Разрешённый манифест пакетов для платформы (`platform` обязателен в query); если у пространства заданы pin'ы — манифест строится из них плюс транзитивные `requires`, иначе из полного каталога; пакеты, отозванные политикой, перечисляются в `revokedPackages`, не исключаясь молча | фиксирует отдачу как «доставленный пакет» (используется `/api/status`) |
| GET `/blob/{name}/{version}` | member | Скачать `.zstd`-блоб; 404, если пакет не входит в разрешённый манифест для этого пространства/платформы, даже если физически существует в хранилище | заголовок `X-Package-Sha256` для проверки целостности |
| GET `/catalog` | owner, admin + human | Полный каталог пакетов деплоя, без учёта pin'ов пространства | — |
| GET `/pins` | owner, admin + human | Список закреплений пространства | — |
| PUT `/pins` | owner, admin + human | Полная замена набора pin'ов (delete-then-insert в одной транзакции) | — |

### Валидация (provisioning)

- `platform` — один из `*`, `darwin-arm64`, `darwin-x64`, `win-x64`,
  `linux-x64`, `linux-arm64`.
- `package_type` — `skill`/`mcp-server`/`runtime`; `package_name`/`version` —
  `^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`; на пару
  (`package_type`,`package_name`) допустима одна запись в `PUT /pins` (иначе
  400 duplicate).

---

## 10. `/api/workspace-config/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/workspace-config` | owner/admin пространства **или** deployment-admin + human | Слой LLM/MCP-умолчаний пространства. `has_llm_api_key` — булев признак, сам ключ не отдаётся; `mcp_defaults` — маскирован (значения `env` заменены на `true`/`false`) | при входе через deployment-admin — admin-аудит `config.read` |
| PUT `/api/workspace-config` | owner/admin **или** deployment-admin + human | Частичный патч слоя. `llm_api_key` записывается запечатанным и никогда не возвращается | при deployment-admin — admin-аудит `workspace_config.set` с хэшами до/после |
| GET/PUT/DELETE `/overrides/{userId}` | owner/admin **или** deployment-admin + human | Персональный override того же слоя для конкретного участника (`mcp_overrides` вместо `mcp_defaults`); 404, если `userId` не участник пространства | при deployment-admin — `config.read` / `user_config_override.set` / `user_config_override.delete` |

### Валидация (workspace-config)

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

## 11. `/api/workspace-mcp-servers/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/workspace-mcp-servers` | member + human | Объединяет собственные серверы пространства и включённые из библиотеки деплоя; для каждого — `provided_credentials`/`missing_credentials` по вызывающему | — |
| POST `/api/workspace-mcp-servers` | owner, admin + human, **не агент-актор** | Создать собственный сервер пространства | — |
| PUT `.../{serverId}` | owner, admin + human, не агент | Обновить (частично) | — |
| DELETE `.../{serverId}` | owner, admin + human, не агент | Удалить; каскадно снимает назначения у агентов и стирает пользовательские креды | — |
| PUT `.../{serverId}/credentials` | member + human, не агент | Задать **свои** значения credential-полей (самообслуживание, не только owner/admin); объединяет с уже сохранёнными, пустая строка удаляет ключ | значения запечатываются перед сохранением |
| DELETE `.../{serverId}/credentials` | member + human, не агент | Очистить свои значения | — |

### Валидация (workspace-mcp-servers)

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

## 12. `/api/deployment-mcp-servers/**`

| Метод и путь | Права | Поведение | Побочные эффекты |
|---|---|---|---|
| GET `/api/deployment-mcp-servers` | member + human | Серверы из библиотеки деплоя, доступные пространству, с флагом `enabled` | — |
| PUT `.../{serverId}/enabled` | owner, admin + human, не агент | Включить/выключить сервер библиотеки для пространства | при выключении — снимает назначения сервера у всех агентов пространства |

---

## 13. Одиночные ручки

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

## Сверка форм с общими схемами

Спецификатор общих схем должен подтвердить, что `#/components/schemas/Workspace`
и `#/components/schemas/Member` содержат перечисленные ниже поля — в этом
фрагменте они переиспользуются как есть (Workspace) или расширяются
(WorkspaceMemberWithUser):

- **Workspace**: `id, name, slug, description, context, settings, repos,
  issue_prefix, avatar_url, created_at, updated_at`.
- **Member** (база для WorkspaceMemberWithUser): `id, workspace_id, user_id,
  role, created_at, perimeter_access` (+ здесь добавлены `name, email,
  avatar_url` из пользователя).

## Спорные места (для обсуждения)

1. **Единый actor-флаг «человек».** RequireHumanActor блокирует только
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
   (`resolveActor(...) == "agent"`), это отдельная проверка от
   RequireHumanActor/ролей owner-admin-member. В `x-roles` перечислены
   допустимые роли участника, а запрет агента вынесен в описание — считаю
   нужным явно проговорить это в реализации, а не полагаться на то, что
   агент физически не имеет роли owner/admin.
5. **`/api/cloud-billing/**` и `DeploymentClientSecrets`/`EffectiveConfigView`**
   не имеют собственной валидации на этом уровне (кроме `checkout-sessions/
   {sessionId}` формата) — вся содержательная проверка (существование tier_id,
   валидность email и т.п.) происходит на стороне внешнего облачного сервиса,
   и наш сервер её не дублирует и не может продиагностировать заранее.

## Проверка

```
python3 -c "import yaml;yaml.safe_load(open('docs/50-api-contract-parts/B.yaml'))"
```
→ OK.

Число маршрутов в диапазоне router.go (663–875 включительно): **106**
(`awk 'NR>=663 && NR<=875' server/cmd/server/router.go | grep -cE '\.(Get|Post|Put|Patch|Delete)\("'`).
Число операций в `B.yaml`: **106** (проверено разбором YAML: подсчитаны все
ключи `get/post/put/patch/delete` внутри `paths`). Совпадает.
