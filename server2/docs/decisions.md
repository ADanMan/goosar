# Решения по пробелам спецификации

Документ ведётся по правилу clean-room: если специфики (docs/50-api-contract.yaml,
docs/51-data-model.md) не хватает, реализатор принимает разумное решение и
записывает его сюда, не блокируясь. Ниже — решения, принятые при реализации
`server2/cmd/import` (T-025).

## Пробелы спецификации

1. **Запись в server2 — по SQL, не по HTTP.** docs/51-data-model.md описывает
   перенос как «GET из старого сервера → POST в server2 через тот же
   контракт», но сам же оговаривает, что `server2` реализует контракт только
   в T-026+. На момент T-025 у server2 нет HTTP API, в которое можно было бы
   писать. Решение: читать источник исключительно через GET контракта (это и
   есть требование «не SQL-в-SQL» — оно про источник), а запись в целевую
   базу вести напрямую по SQL в одной транзакции, как явно и разрешает текст
   тикета в этой сессии («запись в БД в одной транзакции, идемпотентность
   через ON CONFLICT»). Служебный маршрут `POST /api/admin/import/reassign-id`,
   упомянутый в data-model.md, поэтому не нужен: раз запись — SQL, исходный
   `id` просто передаётся как есть в INSERT.

2. **`GET /api/autopilots/{id}/triggers` не существует.** В контракте у
   `/api/autopilots/{id}/triggers` есть только `POST` (create); `GET`/`PATCH`
   по одному триггеру — `/api/autopilots/{id}/triggers/{triggerId}`, но тоже
   без `GET`, только `PATCH`/`DELETE`. Список триггеров переносится через
   `GET /api/autopilots/{id}` (`getAutopilot`), который по контракту
   возвращает `{autopilot, triggers, collaborators}` целиком — этого
   достаточно, отдельный листинг не нужен.

3. **`GET /api/chat/sessions` — только сессии вызывающего.** Контракт прямо
   пишет: «Список чат-сессий, **созданных текущим пользователем**» — нет
   параметра/роли для листинга всех чатов воркспейса от имени owner/admin.
   Решение: импортёр переносит чаты владельца PAT, которым запущен импорт;
   чаты остальных участников — задокументированный пробел (см. отчёт
   импортёра, категория `chat_sessions_of_other_members`). Если нужен полный
   перенос всех чатов, потребуется PAT каждого участника по очереди — вне
   рамок одного запуска `cmd/import`.

4. **Расхождение контракта с реальным сервером на `GET /api/issues/{id}/pull-requests`.**
   docs/50-api-contract.yaml описывает тело ответа как плоский массив
   `IssuePullRequestLink[]`; реально запущенный сервер (localhost:8199,
   `v1.2.0`, тот же контракт по документу) отдаёт `{"pull_requests": [...]}`.
   Обнаружено при живой проверке (см. отчёт). Решение: клиент разбирает
   обёрнутую форму — соответствует поведению сервера, с которым реально
   работает T-025 (перенос делается против живого API, а не против бумажной
   спецификации, если они разошлись). Аналогично `GET .../runtime-profiles`
   оборачивает список в `{"runtime_profiles": [...]}` — это описано и в самой
   схеме `RuntimeProfilesResponse`, здесь просто отмечено как то же семейство
   расхождений (не все списочные эндпоинты — плоские массивы, несмотря на то,
   что схема `IssuePullRequestLink`-эндпоинта в yaml заявлена как массив).

5. **`executors` (Runtime) — «не переносится как данные», но
   `operatives.executor_id` — обязательный FK.** docs/51-data-model.md прямо
   говорит, что runtime — «живое подключение», не история, и импортёр не
   должен переносить его как данные, только маппинг «старый runtime → новый».
   Но раз запись идёт по SQL напрямую в server2 (см. п.1), а
   `operatives.executor_id uuid NOT NULL REFERENCES executors(id)`, без
   строки `executors` с исходным id агент вставить нельзя. Решение: `Runtime`
   всё же переносится строкой `executors` с тем же id, но статус всегда
   выставляется `offline` (не копируется «online» из источника) — это
   заготовка, которая будет замещена/обновлена, как только соответствующий
   демон подключится к server2 (`ex_status`/`ex_last_seen_at` обновятся сами
   через `/api/daemon/register`+heartbeat). Если у агента `runtime_id` не
   находится в `listRuntimes` источника (runtime был удалён), создаётся
   технический executor-заглушка (`ex_title = 'imported-placeholder'`) —
   такой случай в живой проверке не встретился, но код на него рассчитан.

6. **Секретные/зашифрованные поля не восстанавливаются.** `op_runtime_config_sealed`,
   `op_mcp_config_sealed`, `op_custom_env_sealed`, `cfg_llm_api_key_sealed`,
   `wmcp_config_sealed` остаются `NULL`. Причина не только в том, что контракт
   маскирует часть значений (`gateway.token` → `***`, `mcp_config_redacted`),
   но и в том, что даже немаскированную часть `runtime_config`/`mcp_config`
   некуда правильно положить: колонка ожидает результат шифрования ключом
   server2 (`GOOSAR_MCP_SECRET_KEY`-аналог для server2, часть T-026+), которым
   `cmd/import` не располагает и располагать не должен (это ключ сервера, не
   утилиты миграции данных). `op_custom_args` — не секрет, переносится в
   `jsonb` как есть.

7. **Вложения (`assets`) не переносятся.** docs/51-data-model.md описывает
   перенос байтов через `GET .../attachments/{id}/download` +
   `POST /api/attachments` (multipart), но `POST /api/attachments` в
   контракте — часть server2 (T-026+), которой ещё нет, а писать байты
   напрямую в объектное хранилище минуя контракт («не SQL-в-SQL» в широком
   смысле «не обходить API») инструмент, который сам заменяет часть
   контракта, не должен без явного согласования схемы хранения. Решение:
   вложения в этой версии `cmd/import` не переносятся; факт и причина явно
   печатаются в JSON-отчёте (`assets_attachments`). Задокументировано как
   осознанная потеря, а не тихий пропуск.

## T-026: пробелы спецификации в `server2/cmd/server`

1. **Регистрация доменов не через `app.Deps` напрямую.** Текст тикета
   предлагал сигнатуру `func Register(mux *httpapi.Router, deps *app.Deps)`
   для каждого домена. Буквально так домены пришлось бы импортировать пакет
   `app`, а `app/routes.go` импортирует все домены — цикл `app <-> домен` в Go
   не компилируется. Решение: у каждого домена свой `Deps` (например
   `authn.Deps`, `workspace.Deps`), содержащий только то, что домену нужно;
   `internal/app/deps.go` строит их все и передаёт в `Register`, а
   `internal/app/routes.go` остаётся тем самым «одна строка на домен».
   Параллельным реализаторам T-027+ это даже удобнее: не нужно тянуть
   определение `app.Deps`, чтобы понять свои зависимости.

2. **`GOOSAR_MCP_SECRET_KEY` есть в конфиге, но шифрование им не реализовано.**
   MFA/секретные конфиги воркспейса требуют этого ключа по контракту
   (`MFAStatusResponse.available`), но сам механизм шифрования — за пределами
   T-026 (TOTP enroll/confirm/disable оставлены 501, см. ниже). `available`
   честно отражает `GOOSAR_MCP_SECRET_KEY != ""`, не притворяясь, что запись
   секретов уже работает.

3. **`acct_token_epoch` — новая колонка `accounts` (миграция 013).**
   `authRevokeAllSessions` по контракту обязан немедленно инвалидировать
   каждый JWT, выпущенный раньше ("token version check"), а `001_identity`
   такого счётчика на аккаунте не заводит. Добавлена узкая миграция
   `013_authn_token_epoch.up/down.sql` (одна колонка, `DEFAULT 0`) — с
   отчётливым, непохожим на `server/` именем колонки, как и остальные
   в этой схеме.

4. **`/ws` — только транспорт (комнаты + upgrade), без протокола подписок.**
   Контракт прямо отсылает за деталями к `A.md` («Протокол /ws»), которого
   нет в списке разрешённых для чтения файлов этой сессии. Реализованы
   handshake (резолв `workspace_id`/`workspace_slug`, проверка членства,
   коды 400/401/403/404/101), таймаут первого фрейма для
   неаутентифицированного апгрейда, и общий интерфейс `realtime.Publisher`,
   которым будущие домены (T-027+) будут публиковать события. Разбор
   конкретных типов подписок/событий — за рамками T-026, реализуется вместе
   с доменом, который их производит (issues/chat/...).

5. **`workspaceTemplatesList` и `WorkspaceCapabilities.template_key` — пустой
   каталог.** Ни контракт, ни data-model не перечисляют реальный набор
   шаблонов ролей пространства (только форму `WorkspaceTemplateSummary`).
   `GET /api/workspace-templates` возвращает `[]` (валидно по схеме),
   `template_key` в `CreateWorkspaceRequest` принимается, но ни на что не
   влияет; `GET /api/workspaces/{id}/capabilities` всегда отдаёт пустые
   `capabilities`/`sample_tasks` — контракт сам оговаривает это как
   допустимый ответ 200 для пространства без привязанного шаблона.

6. **Список зарезервированных slug — придуман, не выведен из контракта.**
   `workspace_slug_reserved` в контракте объявлен как код ошибки, но сам
   список зарезервированных слов нигде не перечислен. Взят минимальный набор
   путей верхнего уровня, с которыми конфликт slug реально сломал бы
   маршрутизацию (`api`, `admin`, `www`, `app`, `auth`, `health`, `ws`) —
   `internal/workspace/handlers.go`, `reservedSlugs`.

7. **`DELETE .../runtime-profiles/{id}` не проверяет «активных агентов».**
   Контракт требует 409, если у профиля есть активные агенты или отряды с
   архивным лидером — оба понятия (`operatives`, `crews`) принадлежат
   доменам T-027/T-028, которых ещё нет. `HasActiveAgentsOnProfile` — явная
   заглушка, всегда `false`, с комментарием в коде; когда появится домен
   agents, здесь нужно будет подключить настоящую проверку.

8. **MFA (кроме статуса), OIDC, LDAP, magic-link (`/auth/verify-link`) — 501.**
   `GET /api/auth/mfa` (статус) и `GET /api/auth/methods` реализованы
   полностью; enroll/confirm/disable/recovery-codes, `/api/auth/oidc/*`,
   `/api/auth/ldap/login`, `/auth/verify-link` возвращают
   `{"error":"not implemented"}` (501) — ни один контрактный тест раздела
   auth/workspaces/me их не вызывает (см. `e2e/contract/contract_test.go`,
   `testAuth`), а реализация TOTP-хранилища/OIDC-редиректов/LDAP-клиента
   ощутимо увеличила бы объём T-026 без необходимости для критерия приёмки.

9. **`/api/me/export`, `.../onboarding/runtime-bootstrap`,
   `.../onboarding/no-runtime-bootstrap` — 501.** Требуют вложений/агентов/задач
   (`assets`, `operatives`, `tickets` — домены T-027+), которых ещё нет.

10. **Приглашение по email отправляется, но письмо не проверяется тестами.**
    `inviteWorkspaceMember` шлёт письмо через `mail.Sender` в горутине
    (контракт: «ошибка отправки только логируется, ответ API не меняется»);
    dev-реализация просто пишет его в лог (T-029 подключит Resend/SMTP).

## T-027 (project/feed/chat): пробелы спецификации

Реализатор этой сессии отвечал за `internal/{project,feed,chat}` (проекты,
инбокс/уведомления, чат) — `internal/task`/`internal/dispatch` делает
параллельная сессия.

1. **Схема уже существовала.** `initiatives`/`initiative_resources`
   (005_tasks.up.sql), `alerts`/`notification_prefs` (009_feed.up.sql),
   `convos`/`convo_messages`/`convo_drafts`/`convo_pinned_operatives`/
   `convo_channel_links` (007_chat.up.sql) и `dispatch_jobs`
   (008_dispatch.up.sql) были спроектированы ещё в T-025 (см. `cmd/import`) и
   покрывают почти всю доменную область T-027 без изменений. Единственная
   добавленная миграция — `140_chat_read_state.up/down.sql`
   (`convos.cv_last_read_at`): у чат-сессии ровно один читатель-человек (её
   создатель), поэтому достаточно одной метки на строке `convos`, а не
   отдельной таблицы "прочитано на пользователя", как у `alerts`.

2. **`feed.Notify` — группа/severity уведомления по `al_kind`.** Контракт
   перечисляет известные значения `InboxItem.type`, но нигде не сопоставляет
   их ни с шестью группами `NotificationPreferencesInput`, ни со `severity` по
   умолчанию. Решение — таблицы `kindToGroup`/`defaultSeverity` в
   `internal/feed/notify.go`, экспортированная `GroupFor(kind)`; значение вне
   таблицы попадает в группу `updates` с `severity=info` (список типов
   контракт прямо называет открытым). Если получатель — участник и его группа
   выставлена в `muted`, `Notify` не создаёт строку вовсе (не создаёт и не
   помечает прочитанной) и возвращает `created=false, err=nil` — это не
   ошибка вызывающего домена.

3. **`archiveCompletedInbox` — что считать "завершённым".** `tk_status` не
   знает значения `completed` (005_tasks.up.sql: `backlog/todo/in_progress/
   in_review/done/blocked/cancelled`). Решение: считать тикет завершённым,
   если `tk_status IN ('done', 'cancelled')`.

4. **`chat.CanInvoke` — алгоритм доступа "invoke" к агенту.** Контракт
   описывает форму `operatives.op_permission_mode` (`private`/`public_to`) и
   `operative_targets`, но не сам алгоритм проверки. Решение (см.
   `internal/chat/store.go`, `Store.CanInvoke`): owner/admin воркспейса может
   вызвать любого агента; `private` — только его `op_owner_account_id`;
   `public_to` — если для агента в `operative_targets` есть подходящая цель
   (весь воркспейс, сам вызывающий как `member`, либо отряд из `crew_members`,
   где вызывающий состоит участником). Та же проверка используется и в
   `createChatSession`, и в `pinChatAgent`.

5. **Приоритет чат-задач в очереди.** Контракт требует "приоритет чата" выше
   фонового, не называя число (`dispatch_jobs.dj_priority DEFAULT 0`,
   `ORDER BY dj_priority DESC`). Решение — константа `chatPriority = 10` в
   `internal/chat/handlers.go`.

6. **Генерация заголовка сессии по первому сообщению — эвристика, не LLM.**
   Контракт описывает "асинхронно запускается генерация заголовка сессии по
   содержимому сообщения", что в реальном сервисе, видимо, означает вызов
   LLM; в clean-room без внешнего API это не воспроизвести. Решение —
   `generateTitle` в `internal/chat/handlers.go`: обрезка первого сообщения
   до 60 рун с многоточием, синхронно (не асинхронно — нет фоновой очереди
   для этого в рамках T-027).

7. **`dj_context_snapshot` для чатовых задач — минимальный состав.** Контракт
   и `008_dispatch.up.sql` описывают снапшот как "то, что демону нужно при
   claim", не фиксируя точный набор полей для канала chat. Решение —
   `Store.BuildContextSnapshot` кладёт `initiator_name`, `chat_session_title`
   и, если сессия привязана к проекту, `project_title`.

8. **Вложения чат-сообщений: `assets` уже существует (домен `asset`,
   параллельная сессия), их FK на `convo_message_id` — общий контракт между
   доменами.** `sendChatMessage` не создаёт вложения сама (это `POST
   /api/upload-file`, чужой домен) — она только "заявляет" уже загруженные,
   ещё не занятые строки `assets` (`convo_id` = текущая сессия,
   `convo_message_id IS NULL`) на id нового сообщения одним `UPDATE ...
   RETURNING id`, что само по себе и реализует "без дублей/уже занятых" из
   контракта. `ChatDraftRestore.attachments` при этом всегда `[]`: у
   `convo_drafts` нет колонки-владельца в `assets` (черновик — не сообщение),
   заводить её ради этого редкого сценария (черновик появляется только после
   отмены задачи) сочтено избыточным для рамок T-027.

9. **`POST /api/tasks/{taskId}/cancel` (`cancelTaskByUser`) — не реализован
   в этой сессии.** Эндпоинт тега `Tasks`, отменяющий и issue-, и chat-задачи
   в одном хендлере (`dispatch.CancelJob` + опционально запись
   `convo_drafts`), логически ближе к домену `task`/`dispatch` (параллельная
   сессия), у которого уже есть вся инфраструктура отмены. Со стороны chat
   для него подготовлена точка входа `Store.CreateDraftRestore(ctx, convoID,
   taskID, content)` (см. `internal/chat/store.go`) — вызывающий домен читает
   отменённое пользовательское сообщение сам (`dispatch_job_id` совпадает с
   `taskID`) и передаёт его текст сюда, не обращаясь к остальному чат-стору.

10. **`getChatChannelHistory`/`getChatThread` — внешние каналы (Slack и т.п.)
    не реализованы.** `convo_channel_links` (007_chat.up.sql) — таблица есть,
    но привязку к внешнему каналу создаёт домен интеграций (T-029, вне E8 на
    момент этой сессии). Оба маршрута проверяют `X-Actor-Source: task_token` +
    `X-Task-ID` (через `dispatch.Store.GetJob`, публичный метод) и всегда
    отвечают `note` вместо ошибки — ровно то поведение, что контракт
    оговаривает для сессии без канала.

11. **`realtime.TaskAccess`/`ChatAccess` (пробел зафиксирован ещё в
    `internal/realtime/ws.go` до этой сессии).** Реализована `ChatAccess`
    (`internal/chat/access.go`, `ChatAccessBridge.CanAccessChat`: found —
    сессия существует в воркспейсе, allowed — вызывающий её создатель) и
    подключена в `internal/app/routes.go`. `TaskAccess` оставлена `nil` —
    это домен `task`, не мой.

12. **Общая правка `internal/httpapi`:** добавлены `RoleAgent`,
    `WorkspaceMembership` и `RequireWorkspaceMember` (`workspace.go`) — общий
    пролог для доменов, чей контракт резолвит пространство только по
    заголовку/query, без `{id}` в пути (Projects/Inbox/Chat). Это чисто
    добавочные экспортируемые имена, ни одна существующая сигнатура не
    менялась.

13. **Общая правка `internal/workspace`:** добавлен
    `Store.HTTPAPIMembership()` (`httpapi_adapter.go`) — единственная
    реализация `httpapi.WorkspaceMembership` поверх `workspace.Store`,
    которой пользуются `project`/`feed`/`chat`, вместо трёх копий одного и
    того же адаптера в каждом домене.

14. **Общая правка `internal/dispatch`:** добавлен `Store.CancelActiveForConvo`
    + `Deps.CancelActiveForConvo` (`deps.go`, `store.go`) — тот же приём, что
    `CancelActiveForTicket`, по `convo_id`; нужен `deleteChatSession`
    (контракт требует отменять незавершённые задачи сессии при её удалении).
    Заодно исправлена ошибка типов в `Store.insert` (`NULLIF($n,'')` без
    явного `::uuid` — Postgres отказывался присваивать `text` результату
    `NULLIF` в `uuid`-колонку; воспроизводилось на любом вызове `Enqueue` с
    хотя бы одним пустым uuid-полем, в том числе из `chat`). Добавлен
    `::uuid` к каждому `NULLIF` uuid-колонки (`ticket_id`, `initiative_id`,
    `crew_id`, `convo_id`, `sentinel_run_id`, `dj_trigger_note_id`,
    `dj_trigger_thread_id`, `dj_initiator_id`, `dj_parent_job_id`) — без
    изменения сигнатур. Тест на регрессию —
    `internal/dispatch/cancel_convo_test.go`.

15. **Общая правка `internal/app` (`deps.go`, `routes.go`):** заведены поля
    `Dispatch`/`Project`/`Feed`/`Chat`, их сборка в `New`, три строки
    регистрации доменов и обновлён вызов `realtime.Register` (сигнатура уже
    требовала `TaskAccess`/`ChatAccess` — правка `internal/realtime` до этой
    сессии); `taskAccess` передан `nil` с комментарием, что это зона домена
    `task`.

## Прочитанные файлы (кроме docs/50-api-contract.{md,yaml}, docs/51-data-model.md)

Session T-025:
- `server2/migrations/*.up.sql` (001…012) — точные имена/типы колонок для SQL-слоя.
- `server2/migrations/check_names.py` — не читался напрямую (уже описан в data-model.md).
- `scripts/similarity-check.py` — правила проверки схожести с `server/**`.

Session T-026 (дополнительно к списку выше):
- `docs/31-backlog.md` — только раздел эпика E8 / тикета T-026.
- `server2/go.mod`, `server2/README.md`.
- `server2/internal/migrate/migrate.go` (переиспользован без изменений).
- `server2/migrations/001_identity.up.sql`, `002_workspace.up.sql`.
- `e2e/contract/README.md`, `client.go`, `harness.go`, `contract_test.go`
  (только код разделов auth/workspaces/me и общие `call`/`ensureX` хелперы).

Session T-027 (project/feed/chat, дополнительно к спискам выше):
- `docs/31-backlog.md` — разделы T-027/T-028 (границы доменов, "Затрагивает").
- `server2/README.md`, `server2/docs/adr/0001-stack.md`, `server2/docs/decisions.md`.
- `server2/internal/app/{deps.go,routes.go}`, `internal/httpapi/*.go` (кроме
  `middleware.go`, читанного только частично), `internal/realtime/{hub.go,ws.go}`.
- `server2/internal/workspace/*.go` (образец домена, см. T-026).
- `server2/internal/identity/handlers_me.go` (образец без {id}-пути).
- `server2/migrations/002_workspace.up.sql`, `003_agents.up.sql`,
  `004_crews.up.sql`, `005_tasks.up.sql`, `007_chat.up.sql`,
  `008_dispatch.up.sql`, `009_feed.up.sql`, `migrations/check_names.py`.
- Пакеты соседней параллельной сессии (только код, не их черновые
  комментарии по существу задачи task/dispatch): `internal/dispatch/*.go`,
  `internal/wsctx/resolver.go`, `internal/asset/{deps.go,register.go,store.go,handlers.go}`
  (для форм `Attachment`/`assets`, без изменения этих файлов),
  `internal/task/deps.go` (только сигнатура `Deps`, для сверки соглашений).
- `e2e/contract/{README.md,client.go,harness.go,contract_test.go}` — код
  разделов auth/workspaces/me и общие хелперы (уже было в T-026); разделы
  projects/inbox/chat дописаны этой сессией.
- `server2/internal/importer/integration_test.go`,
  `internal/migrate/migrate_test.go` — образец интеграционного теста на
  одноразовой БД (использован в `project`/`feed`/`chat`/`dispatch` тестах).

`server/**` и `packages/core/**` не открывались.
