# Модель данных Goosar (server2)

## Введение

Документ описывает схему БД `server2` для T-025 (эпик E8, clean-room):
спроектирована заново от сущностей `docs/50-api-contract.yaml`
(`components.schemas`, 279 схем), а не скопирована из
`server/migrations/001_init.up.sql`. Старая миграция использовалась
только для проверки коллизий имён (чтобы **не** совпасть) — см.
`server2/migrations/check_names.py`.

Ни одно имя таблицы или колонки не совпадает со старой схемой, кроме
`id`, `created_at`, `updated_at`, `workspace_id`. Своя система имён:
каждая таблица имеет короткий буквенный префикс (`ws_`, `acct_`, `tk_`,
`op_`, `dj_` и т.д.), который добавляется к каждой предметной колонке —
поэтому `title` становится, например, `tk_headline` (для тикета) или
`ws_title` (для воркспейса), а не одинаковым `title` в обеих таблицах,
как было в старой схеме. **JSON-имена полей контракта при этом не
меняются** — меняется только то, в какой колонке поле хранится; таблицы
соответствия ниже и в конце каждого раздела фиксируют это отображение.

Миграции лежат в `server2/migrations/NNN_domain.{up,down}.sql`,
12 файлов, применяются тем же раннером, что и раньше (последовательные
`NNN_*.up.sql` вверх, `NNN_*.down.sql` вниз в обратном порядке). PostgreSQL 16.

## Принципы

**Ключи.** Все таблицы используют `uuid` первичным ключом с
`DEFAULT gen_random_uuid()` (расширение `pgcrypto`), кроме:
`space_config`/`space_config_overrides`/`notification_prefs`/
`provisioning_pins`/`operative_mcp_links`/`operative_capabilities`/
`crew_members`(нет, у неё есть uuid)/`platform_admins`/`sentinel_subscribers`/
`sentinel_collaborators`/`convo_pinned_operatives` — это чистые join-таблицы
или таблицы-настройки "один документ на владельца", где естественный
составной ключ (`workspace_id, account_id` и т.п.) яснее суррогатного.
`platform_policy` — таблица-синглтон с фиктивным `id smallint CHECK (id=1)`.
Билинговые таблицы (`012_billing`) используют `wallet_owner_key text` —
непрозрачный ключ владельца кошелька (воркспейс или весь деплой в
self-hosted режиме), без FK, разрешается в коде приложения.

**Временные метки.** Везде `timestamptz`, не `timestamp`. `created_at`
проставляется `DEFAULT now()` и никогда не меняется; `updated_at`
обновляется приложением (или триггером — ни один триггер BEFORE UPDATE
не заведён в миграциях специально, чтобы не плодить поведение, скрытое
от кода; `updated_at` — обязанность слоя доступа к данным).

**Soft delete.** Схема **не вводит** общий механизм soft delete (нет
`deleted_at` ни в одной таблице). Причины:
- у большинства сущностей контракта нет DELETE-семантики "с возможностью
  восстановить" — есть `archived_at`/`archived_by` (agent, squad),
  `status=cancelled/archived` (issue, autopilot, chat_session) или
  явный статус жизненного цикла (`inv_status=revoked`,
  `wtu_status=canceled`) — то есть архивация уже описана в самих схемах
  контракта как явное поле, а не подразумевается инфраструктурно;
- там, где удаление должно быть настоящим (сессии, PAT, MFA-коды,
  webhook-доставки, чат-черновики), `ON DELETE CASCADE` каскадно чистит
  зависимые записи;
- вложения (`assets`) физически не удаляются немедленно при удалении
  родителя в контракте не описано отдельного tombstone-API — поэтому
  каскад по `ON DELETE CASCADE`, а бинарные данные в объектном хранилище
  чистит фоновая задача `server2/cmd/*` по расписанию (вне зоны этой
  миграции; в старой схеме на это была отдельная таблица
  `attachment_tombstone` — здесь оставлено как задача сервиса, а не
  таблица, потому что ни одна схема контракта не выставляет tombstone
  наружу).

**JSONB.** Используется для: (1) полей, которые контракт сам описывает как
`additionalProperties: true`/произвольный объект (`ws_settings`,
`acct_onboarding_survey`, `tk_metadata`, `dj_attribution`, `dj_result`,
`cfg_mcp_defaults`, `wmcp_credential_schema` и т.п.); (2) значений
кастомных полей тикета (`tk_custom_field_values`, словарь
`{field_def_id: значение}` — ровно форма `IssuePropertyValues` из
контракта); (3) снимков контекста, которые должны быть воспроизводимы
после ретрая (`dj_context_snapshot`, `dj_connected_apps`, `dj_repo_refs`).
JSONB не используется там, где нужны честные FK, уникальность или фильтрация
по полю в горячем пути (статусы, роли, типы — везде отдельные `text`-колонки
с `CHECK`).

**Мультиарендность.** Каждая таблица, чьи строки принадлежат ровно одному
воркспейсу, несёт `workspace_id uuid NOT NULL REFERENCES spaces(id) ON
DELETE CASCADE` — включая таблицы, до которых можно дойти по FK через
родителя (например, `ticket_notes` не хранит `workspace_id` напрямую,
потому что до него всегда доходят через `tickets.workspace_id`, а вот
`ticket_activity` хранит `workspace_id` явно, потому что часть записей
не привязана к конкретному тикету). Для таблиц, не привязанных к
воркспейсу вообще (`accounts`, `login_sessions`, `access_keys`,
`platform_*`, `wallet_*`), `workspace_id` не добавляется — мультиарендность
для них не применима по смыслу сущности. Изоляция между воркспейсами на
уровне БД обеспечивается на уровне запросов приложения (обязательный
`WHERE workspace_id = $1` во всех воркспейс-скоуп-таблицах), Postgres RLS
не используется в этой версии (может быть добавлено отдельным ADR).

**Нумерация тикетов в воркспейсе.** `spaces.ws_next_ticket_seq bigint` —
счётчик, инкрементируемый атомарно в той же транзакции, что и вставка
тикета:

```sql
UPDATE spaces SET ws_next_ticket_seq = ws_next_ticket_seq + 1
WHERE id = $1
RETURNING ws_next_ticket_seq;
-- далее в той же транзакции:
INSERT INTO tickets (..., tk_seq_number, tk_display_key, ...)
VALUES (..., $seq, $ws_ticket_prefix || '-' || $seq, ...);
```

Блокировка строки `spaces` при `UPDATE` естественно сериализует выдачу
номеров внутри одного воркспейса (без отдельной последовательности
Postgres на каждый воркспейс — их были бы тысячи). Если транзакция
откатится после инкремента, номер просто пропускается (гарантируется
монотонность и уникальность, не гарантируется отсутствие дыр — это
соответствует `Issue.number`: "монотонно возрастающий", контракт не
требует отсутствия дыр). `tk_display_key` хранится материализованно
(не вычисляется на лету), потому что `ws_ticket_prefix` воркспейса можно
поменять (`UpdateWorkspaceRequest.issue_prefix`), а уже выданные
идентификаторы задач должны остаться прежними.

**Очередь задач агентов (`dispatch_jobs`).** Демон получает задачу через
`SELECT ... FOR UPDATE SKIP LOCKED`, не через `UPDATE ... WHERE status =
'queued' LIMIT 1` напрямую (это не позволяет упорядочить по приоритету
безопасно при конкурентных claim). Форма запроса:

```sql
WITH candidate AS (
    SELECT id FROM dispatch_jobs
    WHERE executor_id = $1 AND dj_status = 'queued'
    ORDER BY dj_priority DESC, created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE dispatch_jobs AS dj
SET dj_status = 'dispatched', dj_dispatched_at = now(), updated_at = now()
FROM candidate
WHERE dj.id = candidate.id
RETURNING dj.*;
```

`SKIP LOCKED` гарантирует, что несколько воркеров/раннеров claim-хендлера,
опрашивающих очередь параллельно (несколько executor-подключений одного
демона, либо несколько процессов сервера), никогда не возьмут одну и ту же
строку и не блокируются друг на друге, ожидая чужую блокировку — они просто
переходят к следующей подходящей строке. Частичный индекс
`dispatch_jobs_claim_ix (executor_id, dj_priority DESC, created_at) WHERE
dj_status = 'queued'` держит "живую" часть очереди маленькой независимо от
того, сколько исторических (`completed`/`failed`/`cancelled`) строк
накопилось — она в индекс не попадает. Долгоживущие терминальные записи не
переносятся в отдельную архивную таблицу в этой версии схемы (контракт
явно допускает читать историю через `/api/.../tasks/{id}` бессрочно), но
ничто не мешает завести партиционирование `dispatch_jobs` по `created_at`
позже как чисто эксплуатационное решение, не меняющее схему логически.

Повторные попытки (`dj_attempt`/`dj_max_attempts`), ретраи/ререны
(`dj_parent_job_id`) и делегирование хранятся на той же строке — новая
попытка это **новая строка** `dispatch_jobs` с `dj_parent_job_id`,
указывающим на предыдущую; так очередь claim-а видит только реально
ожидающие строки, а история цепочки восстанавливается обходом
`dj_parent_job_id`.

## Что сознательно не хранится (и почему)

Часть схем контракта не превращается в таблицу — они либо чисто
вычисляемые (агрегаты по существующим таблицам, посчитанные в момент
запроса), либо представляют внешние каталоги/статическую конфигурацию:

| Схема контракта | Почему не таблица |
|---|---|
| `DeploymentFleet*`, `Dashboard*`, `AssigneeFrequencyEntry`, `ChildIssueProgressEntry`, `IssueTable*` (Query/Groups/Rows/Facets), `WorkspaceStatus` | агрегаты/вьюхи поверх `executors`, `dispatch_jobs`, `dispatch_usage`, `tickets` — считаются на чтении, не материализуются отдельной таблицей в этой версии |
| `ProvisioningManifest`, `ProvisioningCatalog`, `ProvisioningPackage`, `ProvisioningUnavailablePackage` | статический каталог пакетов (skill/mcp-server/runtime), поставляется как конфигурация деплоя/сборки, не пользовательские данные; персистентно хранится только выбор воркспейса — `provisioning_pins` |
| `BillingPriceTier` | статическая тарифная сетка (конфигурация биллингового шлюза), не данные воркспейса/пользователя |
| `WorkspaceTemplateSummary`, `WorkspaceCapabilities`, `WorkspaceCapability`, `WorkspaceSampleTask` | статический контент шаблонов ролей, используемый при создании воркспейса (`CreateWorkspaceRequest.template_key`); хранится как конфигурация сервиса (файлы/константы), не в БД |
| `GitHubRepository`, `GitHubRepositoriesResponse`, `ComposioToolkit` | живой проход через внешний API (GitHub/Composio) в момент запроса, не кэшируется в БД |
| `AppConfig`, `ReadinessResponse`, `LlmHealth`, `EffectiveConfigView`, `DeploymentClientSecrets` | вычисляются на лету из переменных окружения, `space_config`/`space_config_overrides`/`platform_policy` и живых health-проверок |
| `DaemonWorkspace`, `DaemonWorkspaceRepos`, `DaemonHeartbeatAck`, `DaemonRegisterRequest`, `AgentRuntime` | протокольные формы для `spaces`/`executors`/`executor_probes`, не отдельное хранилище |
| `JoinTarget`, `JoinTargetResult` | вычисляются из `spaces.ws_open_join` + `space_members` в момент запроса |
| `IssueTriggerPreviewResponse`, `CommentTriggerPreviewResponse`, `CommentTriggerOutcome` | результат симуляции срабатывания правил автозапуска, не персистентная сущность |


## ER-схема

Диаграмма на уровне доменов (12 файлов миграций); стрелка — внешний ключ
"многие→один". Полный список колонок и FK каждой таблицы — в разделе
"Таблицы" ниже.

```text
001 identity                     002 workspace
┌───────────┐                    ┌───────────┐
│ accounts  │←─────────┐         │  spaces   │
└─────┬─────┘          │         └─────┬─────┘
      │1                │               │1
      │*                │               │*
┌─────┴──────┐   ┌──────┴──────┐  ┌─────┴────────────┐
│auth_bindings│  │login_sessions│  │  space_members    │
│mfa_factors  │  │access_keys   │  │  space_invitations│
│mfa_recovery_│  │login_codes   │  │  space_export_jobs│
│  codes      │  │contact_leads │  │  space_config(_overrides)│
└─────────────┘  └──────────────┘  │  space_mcp_servers│
                                    │  space_mcp_credentials│
                                    │  agent_protocols  │
                                    └───────────────────┘

003 agents                                   004 crews
┌───────────┐      ┌─────────────┐          ┌────────┐
│ executors │←─────┤ operatives  │←────┐    │ crews  │←──┐
└─────┬─────┘      └──────┬──────┘     │    └───┬────┘   │
      │1                   │1           │        │1       │
      │*                   │*           │        │*       │
┌─────┴───────┐   ┌───────┴────────┐    │   ┌────┴───────┐│
│executor_probes│ │operative_targets│   │   │crew_members││
│operative_disabled_local_skills   │    │   └────────────┘│
└──────────────┘  │operative_mcp_links│  │  (member_type=agent│
                   │operative_capabilities│  → operatives.id, │
                   └────────────────┘    │   member_type=member│
      ┌────────────┐                     │   → space_members) │
      │capabilities│←────────────────────┘   crew_leader_id →
      └─────┬──────┘  (operative_capabilities,  same polymorphism
            │1          capability_tag_links)
            │*
      ┌─────┴────────┐
      │capability_files│
      └────────────────┘

005 tasks (ядро)                                  006 sentinels
┌────────────┐      ┌──────────┐                 ┌───────────┐
│ initiatives│←─────┤  tickets │←──┐              │ sentinels │
└─────┬──────┘      └────┬─────┘  │(self:         └─────┬─────┘
      │1                  │1       │ tk_parent_    │1      │assignee_id→
      │*                  │*       │ ticket_id)     │*      │ operatives/crews
┌─────┴─────────┐  ┌──────┴──────┐ │          ┌─────┴───────┐
│initiative_     │  │ticket_notes │←┘(self:    │sentinel_    │
│  resources     │  │ticket_marks │  parent_   │  triggers   │
│initiative_     │  │note_marks   │  note_id)  │sentinel_runs│→ tickets,
│  bookmarks     │  │ticket_      │            │sentinel_    │  dispatch_jobs
└────────────────┘  │  subscribers│            │  subscribers│
                     │ticket_pr_   │            │sentinel_    │
tags ──────┬─────────│  links      │            │  collaborators│
 (issue/    │         │ticket_      │            └─────┬───────┘
 agent/     │         │  activity   │                  │1
 skill) →  ticket_tag_links,       │                  │*
           operative_tag_links,    │            ┌─────┴───────┐
           capability_tag_links    │            │webhook_events│
                                    │            └──────────────┘
field_defs → tk_custom_field_values (jsonb on tickets, no separate EAV table)
ticket_bookmarks → tickets, accounts

007 chat                        008 dispatch (очередь агентов)
┌────────┐                      ┌──────────────┐
│ convos │←──┐                  │ dispatch_jobs│←──┐(self: dj_parent_job_id)
└───┬────┘   │(self via         └──────┬───────┘   │
    │1        │ dj_convo_id)           │1            │
    │*                                 │*            │
┌───┴──────────┐               ┌───────┴──────┐
│convo_messages│──dispatch_job_id→│dispatch_messages│
│convo_drafts  │  (soft/real FK)  │dispatch_usage   │
│convo_pinned_ │               └──────────────┘
│  operatives  │  dispatch_jobs.{ticket_id,initiative_id,crew_id,
│convo_channel_│    convo_id,sentinel_run_id,operative_id,executor_id}
│  links       │    → tickets, initiatives, crews, convos, sentinel_runs,
└──────────────┘    operatives, executors (все настоящие FK)

009 feed                         010 integrations
┌────────┐                       ┌────────────────┐
│ assets │→ tickets|ticket_notes│ │ vcs_connections │→ spaces
│        │  |convos|convo_messages│ github_installations│→ spaces
│        │  |dispatch_jobs      │ │ slack_installations │→ spaces, operatives
└────────┘  (одна из пяти, каждая│ │ composio_connections│→ spaces, accounts
             отдельным nullable FK)└────────────────┘
┌────────┐  ┌──────────────────┐
│ alerts │  │notification_prefs │
└────────┘  └────────────────────┘

011 governance                    012 billing
┌───────────────┐                ┌───────────────┐
│platform_admins │               │wallet_balances │ (PK = wallet_owner_key)
│platform_admin_ │               └───────────────┘
│  requests      │               ┌────────────────┐
│platform_audit_ │               │wallet_transactions│
│  log           │               └───────┬────────┘
│platform_mcp_   │←── space_mcp_servers.wmcp_platform_server_id
│  servers       │                       │1
│platform_policy │                       │*
│provisioning_   │→ spaces        ┌──────┴──────────┐
│  pins          │                │wallet_credit_    │
└────────────────┘                │  batches         │
                                   └──────┬───────────┘
                                          │1
                                          │*
                                   ┌──────┴──────┐
                                   │wallet_topups │
                                   └─────────────┘
```

## Соответствие «схема контракта → таблица → ключевые колонки»

| Схема контракта (`components.schemas`) | Таблица | Ключевые колонки |
|---|---|---|
| `User` | `accounts` | `acct_email`, `acct_full_name`, `acct_avatar_uri`, `acct_locale`, `acct_tz`, `acct_onboarded_at`, `acct_onboarding_survey`, `acct_starter_state`, `acct_bio` |
| `AuthMethodsResponse` (методы входа) | `auth_bindings` | `account_id`, `ab_method`, `ab_external_subject`, `ab_external_email` |
| `MFAStatusResponse`, `MFAEnrollResponse` | `mfa_factors` | `account_id`, `mfa_secret_sealed`, `mfa_pending_since`, `mfa_enabled_at` |
| `MFAConfirmResponse.recovery_codes` | `mfa_recovery_codes` | `account_id`, `mrc_code_digest`, `mrc_used_at` |
| `SessionResponse` | `login_sessions` | `account_id`, `sess_secret_digest`, `sess_client_agent`, `sess_last_ping_at`, `sess_valid_until`, `sess_invalidated_at` |
| `PersonalAccessToken` | `access_keys` | `account_id`, `ak_title`, `ak_secret_digest`, `ak_secret_prefix`, `ak_valid_until`, `ak_last_used_at` |
| `LoginResult` (код входа) | `login_codes` | `lc_email`, `lc_code_digest`, `lc_purpose`, `lc_consumed_at`, `lc_valid_until` |
| `ContactSalesRequest`/`Response` | `contact_leads` | `lead_first_name`, `lead_last_name`, `lead_business_email`, `lead_company_name`, `lead_company_size`, `lead_country_region`, `lead_use_case` |
| `Workspace`, `WorkspaceItem` | `spaces` | `ws_title`, `ws_slug`, `ws_summary`, `ws_operating_context`, `ws_settings`, `ws_repo_refs`, `ws_ticket_prefix`, `ws_avatar_uri`, `ws_open_join` |
| `Member`, `WorkspaceMemberWithUser` | `space_members` | `workspace_id`, `account_id`, `sm_role`, `sm_perimeter_access` |
| `WorkspaceInvitation` | `space_invitations` | `inv_inviter_account_id`, `inv_invitee_email`, `inv_invitee_account_id`, `inv_role`, `inv_status`, `inv_valid_until` |
| `WorkspaceExportJob` | `space_export_jobs` | `exp_status`, `exp_error`, `exp_size_bytes`, `exp_manifest`, `exp_storage_uri`, `exp_completed_at` |
| `WorkspaceConfigLayer` | `space_config` | `cfg_llm_base_url`, `cfg_llm_model`, `cfg_llm_api_key_sealed`, `cfg_mcp_defaults`, `cfg_updated_by` |
| `WorkspaceUserConfigOverride` | `space_config_overrides` | `workspace_id`, `account_id`, `cfgo_llm_base_url`, `cfgo_llm_model`, `cfgo_mcp_overrides` |
| `WorkspaceMcpServer` | `space_mcp_servers` | `wmcp_name`, `wmcp_transport`, `wmcp_source`, `wmcp_config_sealed`, `wmcp_credential_schema`, `wmcp_enabled` |
| `SetWorkspaceMcpCredentialsRequest` | `space_mcp_credentials` | `space_mcp_server_id`, `account_id`, `wmcpc_field_key`, `wmcpc_value_sealed` |
| `RuntimeProfile` | `agent_protocols` | `proto_display_title`, `proto_family`, `proto_command`, `proto_fixed_args`, `proto_visibility`, `proto_enabled` |
| `Runtime`, `AgentRuntime` (одна сущность) | `executors` | `ex_daemon_id`, `ex_title`, `ex_custom_title`, `ex_mode`, `ex_provider`, `ex_status`, `ex_device_info`, `ex_metadata`, `ex_owner_account_id`, `ex_visibility`, `ex_protocol_id`, `ex_last_seen_at` |
| `RuntimeUpdateRequest`, `RuntimeModelListRequest`, `RuntimeLocalSkillListRequest`, `RuntimeLocalSkillImportRequest` | `executor_probes` | `executor_id`, `probe_kind`, `probe_status`, `probe_request`, `probe_outcome`, `probe_error` |
| `Skill`, `SkillWithFiles`, `SkillSummary` | `capabilities` | `cap_title`, `cap_summary`, `cap_config`, `cap_body_md`, `cap_created_by` |
| `SkillFile` | `capability_files` | `capability_id`, `capf_path`, `capf_body` |
| `Agent` | `operatives` | `op_title`, `op_summary`, `op_instructions`, `op_runtime_mode`, `op_runtime_config_sealed`, `op_custom_args`, `op_mcp_config_sealed`, `op_permission_mode`, `op_status`, `op_max_concurrent_tasks`, `op_model`, `op_thinking_level`, `op_service_tier`, `op_composio_allowlist`, `op_owner_account_id`, `op_kind`, `op_system_key`, `op_archived_at` |
| `AgentInvocationTarget` | `operative_targets` | `operative_id`, `opt_target_type`, `opt_target_id` |
| старый `agent_mcp_server` (агентские MCP-переопределения) | `operative_mcp_links` | `operative_id`, `space_mcp_server_id`, `opml_enabled` |
| `AgentSkillSummary` (agent↔skill) | `operative_capabilities` | `operative_id`, `capability_id`, `opcap_enabled`, `opcap_config_override` |
| `DisabledRuntimeSkill` | `operative_disabled_local_skills` | `operative_id`, `executor_id`, `opdis_provider`, `opdis_root`, `opdis_key` |
| `Squad` | `crews` | `crew_title`, `crew_summary`, `crew_instructions`, `crew_leader_type`, `crew_leader_id`, `crew_creator_account_id`, `crew_archived_at` |
| `SquadMember` | `crew_members` | `crew_id`, `cm_member_type`, `cm_member_id`, `cm_role` |
| `Project` | `initiatives` | `init_title`, `init_summary`, `init_icon`, `init_status`, `init_priority`, `init_lead_type`, `init_lead_id`, `init_start_date`, `init_due_date` |
| `ProjectResource` | `initiative_resources` | `initiative_id`, `ir_resource_type`, `ir_resource_ref`, `ir_label`, `ir_position` |
| `Label` | `tags` | `tag_resource_type`, `tag_label`, `tag_summary`, `tag_color`, `tag_usage_count` |
| `Label` join (issue) | `ticket_tag_links` | `ticket_id`, `tag_id` |
| `Label` join (agent) | `operative_tag_links` | `operative_id`, `tag_id` |
| `Label` join (skill) | `capability_tag_links` | `capability_id`, `tag_id` |
| `Property`, `PropertyConfig` | `field_defs` | `fd_title`, `fd_type`, `fd_config`, `fd_position`, `fd_archived_at`, `fd_usage_count` |
| `Issue` | `tickets` | `tk_seq_number`, `tk_display_key`, `tk_headline`, `tk_narrative`, `tk_status`, `tk_priority`, `tk_assignee_type/_id`, `tk_creator_type/_id`, `tk_parent_ticket_id`, `initiative_id`, `tk_position`, `tk_stage`, `tk_start_date`, `tk_due_date`, `tk_metadata` |
| `Issue.properties` (`IssuePropertyValues`) | `tickets.tk_custom_field_values` (jsonb-колонка, не отдельная таблица) | ключи — `field_defs.id` |
| `IssueComment` | `ticket_notes` | `ticket_id`, `tn_author_type/_id`, `tn_body`, `tn_kind`, `tn_parent_note_id`, `tn_resolved_*`, `tn_source_dispatch_job_id` |
| `IssueReaction` | `ticket_marks` | `ticket_id`, `tm_actor_type/_id`, `tm_emoji` |
| `CommentReaction` | `note_marks` | `ticket_note_id`, `nm_actor_type/_id`, `nm_emoji` |
| `IssueSubscriber` | `ticket_subscribers` | `ticket_id`, `tsub_watcher_type/_id`, `tsub_reason` |
| `IssuePullRequestLink` | `ticket_pr_links` | `ticket_id`, `tpr_provider`, `tpr_url`, `tpr_number`, `tpr_state` |
| `IssueTimelineEntry` (системные записи) | `ticket_activity` | `ticket_id`, `ta_actor_type/_id`, `ta_action`, `ta_details` |
| `Pin` (`item_type=issue`) | `ticket_bookmarks` | `account_id`, `ticket_id`, `bm_position` |
| `Pin` (`item_type=project`) | `initiative_bookmarks` | `account_id`, `initiative_id`, `bm_position` |
| `Autopilot` | `sentinels` | `sen_title`, `sen_summary`, `initiative_id`, `sen_assignee_type/_id`, `sen_status`, `sen_execution_mode`, `sen_issue_title_template`, `sen_created_by_type/_id`, `sen_last_run_at` |
| `AutopilotTrigger` | `sentinel_triggers` | `sentinel_id`, `strig_kind`, `strig_cron_expression`, `strig_timezone`, `strig_webhook_token_digest`, `strig_provider`, `strig_signing_secret_sealed`, `strig_event_filters` |
| `AutopilotRun` | `sentinel_runs` | `sentinel_id`, `sentinel_trigger_id`, `srun_source`, `srun_status`, `ticket_id`, `srun_dispatch_job_id`, `srun_trigger_payload`, `srun_result` |
| `AutopilotSubscriber` | `sentinel_subscribers` | `sentinel_id`, `account_id` |
| `AutopilotCollaborator` | `sentinel_collaborators` | `sentinel_id`, `account_id`, `sencol_granted_by` |
| `WebhookDelivery` | `webhook_events` | `sentinel_id`, `sentinel_trigger_id`, `whe_provider`, `whe_event`, `whe_dedupe_key`, `whe_status`, `whe_signature_status`, `whe_selected_headers`, `whe_raw_body` |
| `ChatSession` | `convos` | `operative_id`, `cv_creator_account_id`, `initiative_id`, `cv_title`, `cv_status`, `cv_pinned` |
| `ChatMessage` | `convo_messages` | `convo_id`, `cvm_role`, `cvm_body`, `dispatch_job_id` (JSON: `task_id`), `cvm_failure_reason`, `cvm_elapsed_ms`, `cvm_kind` |
| `ChatDraftRestore` | `convo_drafts` | `convo_id`, `dispatch_job_id` (JSON: `task_id`), `cvd_body` |
| `ChatPinnedAgent` | `convo_pinned_operatives` | `account_id`, `operative_id`, `cvp_position` |
| `ChatChannelHistoryResponse`/`ChatHistoryMessage` (привязка треда) | `convo_channel_links` | `convo_id`, `cvc_channel_type`, `cvc_external_channel_id`, `cvc_external_thread_id` |
| `ChatAttachment` (проекция) | `assets` (через `convo_message_id`) | см. `Attachment` ниже |
| `AgentTask` | `dispatch_jobs` | `operative_id` (JSON: `agent_id`), `executor_id` (JSON: `runtime_id`), `ticket_id` (JSON: `issue_id`), `dj_kind`, `dj_status`, `dj_priority`, `dj_result`, `dj_error`, `dj_failure_reason`, `dj_attempt`, `dj_max_attempts`, `dj_parent_job_id` (JSON: `parent_task_id`), `dj_work_dir`, `dj_trigger_note_id` (JSON: `trigger_comment_id`), `dj_coalesced_note_ids`, `dj_delivered_note_ids`, `crew_id` (JSON: `squad_id`), `convo_id` (JSON: `chat_session_id`), `sentinel_run_id` (JSON: `autopilot_run_id`), `dj_attribution`, `dj_claim_secret_digest` (JSON: `auth_token`, хранится только хэш) |
| `TaskMessage`, `TaskMessageInput` | `dispatch_messages` | `dispatch_job_id`, `dm_seq` (JSON: `seq`), `dm_kind` (JSON: `type`), `dm_tool` (JSON: `tool`), `dm_body` (JSON: `content`), `dm_input`, `dm_output` |
| `TaskUsageEntry` | `dispatch_usage` | `dispatch_job_id`, `du_provider`, `du_model`, `du_input_tokens`, `du_output_tokens`, `du_cache_read_tokens`, `du_cache_write_tokens`, `du_cost_usd_ticks` |
| `Attachment` | `assets` | `ticket_id`, `ticket_note_id` (JSON: `comment_id`), `convo_id` (JSON: `chat_session_id`), `convo_message_id` (JSON: `chat_message_id`), `dispatch_job_id`, `as_uploader_type/_id`, `as_filename`, `as_storage_uri` (JSON: `url`), `as_download_path` (JSON: `download_url`), `as_markdown_ref` (JSON: `markdown_url`), `as_content_type`, `as_size_bytes` |
| `InboxItem` | `alerts` | `al_recipient_type/_id`, `al_kind` (JSON: `type`), `al_severity`, `ticket_id` (JSON: `issue_id`), `al_headline` (JSON: `title`), `al_body`, `al_read_at` (JSON: `read`, boolean), `al_archived_at` (JSON: `archived`, boolean), `al_actor_type/_id`, `al_details` |
| `NotificationPreferences` | `notification_prefs` | `workspace_id`, `account_id`, `np_groups` (JSON: `preferences`) |
| `VcsConnection` | `vcs_connections` | `vcs_provider`, `vcs_instance_uri`, `vcs_account_login`, `vcs_webhook_uri`, `vcs_webhook_path`, `vcs_access_token_sealed` |
| `GitHubInstallation` | `github_installations` | `gh_installation_id`, `gh_account_login`, `gh_account_type`, `gh_account_avatar_uri` |
| `SlackInstallation` | `slack_installations` | `operative_id` (JSON: `agent_id`), `sl_team_id`, `sl_bot_user_id`, `sl_installer_account_id`, `sl_status` |
| `ComposioConnection` | `composio_connections` | `workspace_id`, `account_id`, `cx_toolkit_slug`, `cx_status`, `cx_connected_at`, `cx_last_used_at` |
| `DeploymentAdmin` | `platform_admins` | `account_id`, `pa_granted_by`, `pa_granted_at` |
| `DeploymentAdminPendingRequest` | `platform_admin_requests` | `par_action`, `par_target_account_id`, `par_target_email`, `par_requested_by`, `par_status` |
| `DeploymentAuditEntry` | `platform_audit_log` | `paud_source`, `paud_action`, `paud_actor_*`, `paud_target_*`, `paud_outcome`, `workspace_id` |
| `DeploymentMcpServer` | `platform_mcp_servers` | `pmcp_name`, `pmcp_transport`, `pmcp_config_sealed`, `pmcp_credential_schema` |
| `DeploymentPolicyDocument`, `DeploymentPolicyBody` | `platform_policy` | `pp_body` (JSON: `policy`), `pp_updated_at` |
| `ProvisioningPin` | `provisioning_pins` | `workspace_id`, `prov_package_name`, `prov_package_type`, `prov_version`, `prov_enabled`, `prov_updated_by` |
| `BillingBalance` | `wallet_balances` | `wallet_owner_key` (JSON: `owner_id`), `wb_balance_micro`, `wb_balance_credit` |
| `BillingTransaction` | `wallet_transactions` | `wallet_owner_key`, `wt_idempotency_key`, `wt_tx_type`, `wt_source`, `wt_amount_micro`, `wt_balance_after`, `wt_reference_id` |
| `BillingBatch` | `wallet_credit_batches` | `wallet_owner_key`, `wcb_source_tx_id`, `wcb_source_type`, `wcb_total_micro`, `wcb_remaining_micro`, `wcb_valid_until` |
| `BillingTopup` | `wallet_topups` | `wallet_owner_key`, `wtu_amount_cents`, `wtu_currency`, `wtu_credits`, `wtu_bonus_credits`, `wtu_status`, `wtu_tier_id`, `wtu_credit_batch_id` |

Схемы, не перечисленные в этой таблице (агрегаты, живые проходы к внешним
API, статические каталоги/конфигурация) разобраны в разделе "Что сознательно
не хранится" выше.

## Правило сопоставления JSON-поле → колонка

Общее правило: **имя JSON-поля из контракта не меняется**; в БД оно живёт
под другим именем колонки (с префиксом таблицы). Слой доступа к данным
(`server2/internal/store/*`) отвечает за перевод в обе стороны при
сериализации/десериализации ответа. Ниже — не построчный список всех 900+
пар (он избыточен, так как совпадает с таблицами выше почти построчно), а
компактная памятка по видам расхождений, которые встречаются систематически:

1. **Родовые поля переименованы с добавлением префикса таблицы**, без
   смены смысла: `name`→`*_title`/`*_full_name` (`ws_title`, `op_title`,
   `crew_title`, `acct_full_name`, ...), `title`→`*_headline`/`*_title`
   (`tk_headline` для `Issue.title`, `init_title` для `Project.title`),
   `description`→`*_summary` (`ws_summary`, `op_summary`, `init_summary`,
   `sen_summary`), `content`→`*_body`/`*_body_md` (`tn_body` для
   `IssueComment.content`, `cap_body_md` для `Skill.content`, `cvm_body`
   для `ChatMessage.content`), `status`/`state`→`*_status` (`tk_status`,
   `dj_status`, `sen_status`), `type`→`*_kind`/`*_resource_type`
   (`dj_kind` для `AgentTask.kind`, `tag_resource_type` для
   `Label.resource_type`), `enabled`→`*_enabled`, `visibility`→
   `*_visibility`/`*_source`.
2. **Сущность, которую JSON называет одним именем (agent/runtime), а в
   БД — другим существительным** (`Agent`→`operatives`, `Runtime`→
   `executors`, `Issue`→`tickets`, `Squad`→`crews`, `Autopilot`→
   `sentinels`, `Skill`→`capabilities`, `Label`→`tags`, `Project`→
   `initiatives`, `Comment`→`ticket_notes`) — соответствующие FK-колонки
   называются по БД-имени сущности, а не по JSON-имени: `agent_id`
   (JSON) → колонка `operative_id`, `runtime_id` → `executor_id`,
   `issue_id` → `ticket_id`, `squad_id` → `crew_id`, `autopilot_id`/
   `autopilot_run_id` → `sentinel_id`/`sentinel_run_id`, `project_id` →
   `initiative_id`, `comment_id` → `ticket_note_id`/`note_id`,
   `chat_session_id` → `convo_id`, `task_id` (в контексте
   `AgentTask.id`) → `dispatch_job_id`.
3. **Секреты никогда не хранятся в открытом виде** независимо от того,
   как называется JSON-поле: `*_sealed` (симметрично зашифровано,
   расшифровывается при использовании: LLM/MCP API-ключи, OAuth-токены
   интеграций) или `*_digest` (только хэш для сверки, необратимо: пароли
   сессий/PAT/webhook-токенов/кодов входа/MFA-recovery-кодов). Контракт
   такие поля либо не показывает вовсе (`webhook_secret`,
   `access_token`), либо явно маскирует (`gateway.token` → `***`,
   `mcp_config_redacted`) — хранение "sealed/digest" реализует именно это
   требование контракта, а не противоречит ему.
4. **Массив объектов без собственного `id` в контракте** (например
   `Workspace.repos: WorkspaceRepo[]`, где `WorkspaceRepo` не имеет
   `id`) хранится как единая `jsonb`-колонка на родителе
   (`ws_repo_refs`), а не отдельной таблицей — заводить отдельную
   таблицу под сущность без стабильного идентификатора и без
   собственных операций (`WorkspaceRepo` не адресуется отдельным
   `GET/PATCH`) добавило бы таблицу, не обоснованную поведением
   контракта.


## Таблицы

Полный список таблиц (75), сгенерированный из `server2/migrations/*.up.sql`:
колонки, типы, null/default, смысл, PK/UNIQUE/FK (с ON DELETE) и индексы.

### `accounts`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| acct_email | text | нет |  | адрес электронной почты, уникален без учёта регистра |
| acct_full_name | text | нет |  | отображаемое имя пользователя |
| acct_avatar_uri | text | да |  | URL аватара |
| acct_locale | text | да |  | язык интерфейса (en/zh-Hans/ko/ja/ru) |
| acct_tz | text | да |  | часовой пояс пользователя |
| acct_onboarded_at | timestamptz | да |  | момент завершения онбординга |
| acct_onboarding_survey | jsonb | нет | '{}'::jsonb | ответы анкеты онбординга, произвольный JSON |
| acct_starter_state | text | да |  | состояние наполнения демо-контентом при первом входе |
| acct_bio | text | нет | '' | короткое описание профиля пользователя |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE INDEX `accounts_email_uk` (lower(acct_email) )

### `auth_bindings`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| ab_method | text | нет |  | способ входа: email/oidc/ldap |
| ab_external_subject | text | да |  | subject пользователя у внешнего IdP (oidc/ldap) |
| ab_external_email | text | да |  | email, сообщённый внешним IdP |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (account_id, ab_method)
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

- UNIQUE INDEX `auth_bindings_subject_uk` (ab_method, ab_external_subject) WHERE ab_external_subject IS NOT NULL

### `mfa_factors`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| mfa_secret_sealed | bytea | нет |  | зашифрованный TOTP-секрет |
| mfa_pending_since | timestamptz | нет | now() | момент начала подключения MFA (до подтверждения) |
| mfa_enabled_at | timestamptz | да |  | момент подтверждения и включения MFA |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

### `mfa_recovery_codes`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| mrc_code_digest | text | нет |  | хэш одноразового кода восстановления |
| mrc_used_at | timestamptz | да |  | момент использования кода восстановления |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (account_id, mrc_code_digest)
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

### `login_sessions`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| sess_secret_digest | text | нет |  | хэш секрета сессионной куки/токена |
| sess_client_agent | text | нет | '' | User-Agent клиента |
| sess_origin_ip_digest | text | да |  | хэш IP-адреса, с которого создана сессия |
| sess_last_ping_at | timestamptz | нет | now() | момент последней активности сессии |
| sess_valid_until | timestamptz | да |  | срок действия сессии |
| sess_invalidated_at | timestamptz | да |  | момент завершения/отзыва сессии |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

- UNIQUE INDEX `login_sessions_secret_uk` (sess_secret_digest)
- INDEX `login_sessions_account_ix` (account_id) WHERE sess_invalidated_at IS NULL

### `access_keys`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| ak_title | text | нет |  | название токена, заданное пользователем |
| ak_secret_digest | text | нет |  | хэш секрета персонального токена доступа |
| ak_secret_prefix | text | нет |  | видимый префикс токена для UI |
| ak_valid_until | timestamptz | да |  | срок действия токена (NULL — бессрочный) |
| ak_last_used_at | timestamptz | да |  | момент последнего использования токена |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

- UNIQUE INDEX `access_keys_secret_uk` (ak_secret_digest)
- INDEX `access_keys_account_ix` (account_id)

### `login_codes`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| lc_email | text | нет |  | email, на который выслан код входа |
| lc_code_digest | text | нет |  | хэш одноразового кода входа |
| lc_purpose | text | нет | 'login' | назначение кода (login и т.п.) |
| lc_consumed_at | timestamptz | да |  | момент использования кода |
| lc_valid_until | timestamptz | нет |  | срок действия кода |
| created_at | timestamptz | нет | now() | момент создания строки |

- INDEX `login_codes_email_ix` (lower(lc_email) , lc_purpose)

### `contact_leads`  _(файл 001_identity.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| lead_first_name | text | нет |  | имя обратившегося |
| lead_last_name | text | нет |  | фамилия обратившегося |
| lead_business_email | text | нет |  | рабочий email обратившегося |
| lead_company_name | text | нет |  | название компании |
| lead_company_size | text | нет |  | размер компании (диапазон сотрудников) |
| lead_country_region | text | нет |  | страна/регион |
| lead_use_case | text | нет |  | заявленный сценарий использования |
| lead_goals | text | да |  | свободный текст о целях |
| lead_source | text | да |  | источник обращения |
| lead_consent_outreach | boolean | нет | false | согласие на контакт от продаж |
| lead_consent_updates | boolean | нет | false | согласие на рассылку обновлений |
| created_at | timestamptz | нет | now() | момент создания строки |

### `spaces`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| ws_title | text | нет |  | название воркспейса |
| ws_slug | text | нет |  | URL-слаг воркспейса, уникален |
| ws_summary | text | да |  | описание воркспейса |
| ws_operating_context | text | да |  | свободный текст контекста, добавляемый в промпт всем агентам |
| ws_settings | jsonb | нет | '{}'::jsonb | прочие настройки воркспейса, произвольный JSON |
| ws_repo_refs | jsonb | нет | '[]'::jsonb | список репозиториев воркспейса {url, description} |
| ws_ticket_prefix | text | нет |  | префикс идентификатора тикетов (например ENG) |
| ws_next_ticket_seq | bigint | нет | 0 | счётчик для выдачи следующего номера тикета |
| ws_avatar_uri | text | да |  | URL аватара воркспейса |
| ws_open_join | boolean | нет | false | разрешено ли самостоятельное присоединение к воркспейсу |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE INDEX `spaces_slug_uk` (ws_slug)

### `space_members`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| sm_role | text | нет |  | роль участника: owner/admin/member |
| sm_perimeter_access | boolean | нет | false | расширенный доступ к периметру безопасности |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (workspace_id, account_id)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

- INDEX `space_members_account_ix` (account_id)

### `space_invitations`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| inv_inviter_account_id | uuid | нет |  | кто пригласил |
| inv_invitee_email | text | нет |  | email приглашённого |
| inv_invitee_account_id | uuid | да |  | учётная запись приглашённого, если уже существует |
| inv_role | text | нет |  | роль, предлагаемая приглашением |
| inv_status | text | нет | 'pending' | статус приглашения |
| inv_valid_until | timestamptz | нет |  | срок действия приглашения |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (inv_inviter_account_id) → `accounts`(id)
- FK: (inv_invitee_account_id) → `accounts`(id)

- INDEX `space_invitations_email_ix` (lower(inv_invitee_email) )
- INDEX `space_invitations_workspace_ix` (workspace_id, inv_status)

### `space_export_jobs`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| exp_status | text | нет | 'pending' | статус задания экспорта воркспейса |
| exp_error | text | да |  | текст ошибки экспорта |
| exp_size_bytes | bigint | нет | 0 | размер полученного архива |
| exp_manifest | jsonb | да |  | манифест содержимого архива |
| exp_storage_uri | text | да |  | внутренний URI архива в хранилище |
| exp_completed_at | timestamptz | да |  | момент завершения экспорта |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE

- INDEX `space_export_jobs_workspace_ix` (workspace_id, created_at DESC)

### `space_config`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| workspace_id | uuid | нет |  | воркспейс-владелец строки; PK |
| cfg_llm_base_url | text | да |  | базовый URL LLM для воркспейса |
| cfg_llm_model | text | да |  | модель LLM по умолчанию для воркспейса |
| cfg_llm_api_key_sealed | bytea | да |  | зашифрованный API-ключ LLM воркспейса |
| cfg_mcp_defaults | jsonb | нет | '{}'::jsonb | настройки MCP-серверов по умолчанию для воркспейса |
| cfg_updated_by | uuid | да |  | кто последним менял этот слой конфигурации |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (cfg_updated_by) → `accounts`(id)

### `space_config_overrides`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| cfgo_llm_base_url | text | да |  | переопределение базового URL LLM для конкретного участника |
| cfgo_llm_model | text | да |  | переопределение модели LLM для конкретного участника |
| cfgo_llm_api_key_sealed | bytea | да |  | зашифрованный персональный API-ключ LLM |
| cfgo_mcp_overrides | jsonb | нет | '{}'::jsonb | персональные переопределения MCP-настроек |
| cfgo_updated_by | uuid | да |  | кто последним менял переопределение |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- PK: (workspace_id, account_id)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE
- FK: (cfgo_updated_by) → `accounts`(id)

### `space_mcp_servers`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| wmcp_name | text | нет |  | имя MCP-сервера в рамках воркспейса |
| wmcp_transport | text | нет | 'unknown' | транспорт MCP-сервера: stdio/http/unknown |
| wmcp_source | text | нет | 'workspace' | откуда взят сервер: workspace/deployment |
| wmcp_platform_server_id | uuid | да |  | ссылка на platform_mcp_servers, если сервер взят из каталога деплоя |
| wmcp_config_sealed | bytea | да |  | зашифрованная конфигурация подключения сервера |
| wmcp_credential_schema | jsonb | нет | '[]'::jsonb | описание полей учётных данных, которые должен заполнить каждый пользователь |
| wmcp_enabled | boolean | нет | true | включён ли сервер в воркспейсе |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (workspace_id, wmcp_name)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK (добавлен в 008/011 после создания родительской таблицы): (wmcp_platform_server_id) → `platform_mcp_servers`(id) ON DELETE SET NULL

### `space_mcp_credentials`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| space_mcp_server_id | uuid | нет |  | ссылка на space_mcp_servers |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| wmcpc_field_key | text | нет |  | ключ поля учётных данных из wmcp_credential_schema |
| wmcpc_value_sealed | bytea | нет |  | зашифрованное значение поля учётных данных пользователя |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (space_mcp_server_id, account_id, wmcpc_field_key)
- FK: (space_mcp_server_id) → `space_mcp_servers`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

### `agent_protocols`  _(файл 002_workspace.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| proto_display_title | text | нет |  | отображаемое имя протокола запуска агента |
| proto_family | text | нет |  | семейство агентского протокола |
| proto_command | text | нет |  | имя команды для запуска (без пробелов) |
| proto_summary | text | да |  | описание протокола |
| proto_fixed_args | jsonb | нет | '[]'::jsonb | фиксированные аргументы командной строки |
| proto_visibility | text | нет | 'workspace' | видимость профиля в воркспейсе |
| proto_created_by | uuid | да |  | кто создал профиль |
| proto_enabled | boolean | нет | true | включён ли профиль |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (proto_created_by) → `accounts`(id)

- INDEX `agent_protocols_workspace_ix` (workspace_id) WHERE proto_enabled

### `executors`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| ex_daemon_id | text | да |  | идентификатор физического daemon-процесса |
| ex_title | text | нет |  | имя runtime, присвоенное демоном |
| ex_custom_title | text | да |  | имя runtime, заданное пользователем |
| ex_mode | text | нет |  | режим исполнения: local/cloud |
| ex_provider | text | нет |  | провайдер агентского движка (claude/codex/...) |
| ex_launch_header | text | да |  | служебный заголовок для запуска задач |
| ex_status | text | нет | 'offline' | online/offline по таймауту heartbeat |
| ex_device_info | text | нет | '' | информация об устройстве/машине |
| ex_metadata | jsonb | нет | '{}'::jsonb | произвольные метаданные runtime |
| ex_owner_account_id | uuid | да |  | владелец приватного runtime |
| ex_visibility | text | нет | 'private' | private/public |
| ex_protocol_id | uuid | да |  | ссылка на agent_protocols, если runtime порождён кастомным протоколом |
| ex_last_seen_at | timestamptz | да |  | момент последнего heartbeat |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (ex_owner_account_id) → `accounts`(id)
- FK: (ex_protocol_id) → `agent_protocols`(id)

- INDEX `executors_workspace_ix` (workspace_id)
- INDEX `executors_daemon_ix` (ex_daemon_id) WHERE ex_daemon_id IS NOT NULL

### `executor_probes`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| executor_id | uuid | нет |  | ссылка на executors (runtime-исполнитель) |
| probe_kind | text | нет |  | вид демон-пробы: self_update/model_list/local_skills/local_skill_import |
| probe_status | text | нет | 'pending' | статус пробы: pending/running/completed/failed/timeout |
| probe_request | jsonb | нет | '{}'::jsonb | параметры запроса пробы (например target_version) |
| probe_outcome | jsonb | да |  | результат, присланный демоном |
| probe_error | text | да |  | текст ошибки пробы |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (executor_id) → `executors`(id) ON DELETE CASCADE

- INDEX `executor_probes_pending_ix` (executor_id, probe_kind) WHERE probe_status = 'pending'

### `capabilities`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| cap_title | text | нет |  | название навыка |
| cap_summary | text | да |  | описание навыка |
| cap_config | jsonb | нет | '{}'::jsonb | конфигурация навыка, произвольный JSON |
| cap_body_md | text | нет | '' | содержимое SKILL.md |
| cap_created_by | uuid | да |  | кто создал навык |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (cap_created_by) → `accounts`(id)

- INDEX `capabilities_workspace_ix` (workspace_id)
- INDEX `capabilities_title_trgm_ix` (cap_title gin_trgm_ops)

### `capability_files`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| capability_id | uuid | нет |  | ссылка на capabilities (навык) |
| capf_path | text | нет |  | относительный путь файла навыка |
| capf_body | text | нет | '' | содержимое файла навыка |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (capability_id, capf_path)
- FK: (capability_id) → `capabilities`(id) ON DELETE CASCADE

### `operatives`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| executor_id | uuid | нет |  | ссылка на executors (runtime-исполнитель) |
| op_title | text | нет |  | имя агента |
| op_summary | text | да |  | описание агента |
| op_instructions | text | нет | '' | системный промпт агента |
| op_avatar_uri | text | да |  | URL аватара агента |
| op_runtime_mode | text | нет |  | local/cloud |
| op_runtime_config_sealed | bytea | да |  | зашифрованная конфигурация шлюза исполнения (может содержать токен) |
| op_custom_args | jsonb | нет | '[]'::jsonb | дополнительные аргументы командной строки агента |
| op_mcp_config_sealed | bytea | да |  | зашифрованная конфигурация MCP-серверов агента |
| op_mcp_config_encrypted | boolean | нет | true | хранится ли mcp_config в зашифрованном виде |
| op_custom_env_sealed | bytea | да |  | зашифрованные дополнительные переменные окружения агента |
| op_permission_mode | text | нет | 'private' | private/public_to |
| op_status | text | нет | 'idle' | idle/working/blocked/error/offline |
| op_max_concurrent_tasks | integer | нет | 6 | лимит одновременно выполняемых задач |
| op_model | text | да |  | переопределение модели для этого агента |
| op_thinking_level | text | да |  | уровень режима размышления |
| op_service_tier | text | да |  | уровень обслуживания у провайдера модели |
| op_composio_allowlist | jsonb | да |  | разрешённые Composio-инструменты (NULL — без ограничения) |
| op_owner_account_id | uuid | да |  | владелец агента |
| op_kind | text | нет | 'user' | user/system |
| op_system_key | text | да |  | ключ системного ролевого агента (kind=system) |
| op_archived_at | timestamptz | да |  | момент архивации агента |
| op_archived_by | uuid | да |  | кто архивировал агента |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (executor_id) → `executors`(id)
- FK: (op_owner_account_id) → `accounts`(id)
- FK: (op_archived_by) → `accounts`(id)

- INDEX `operatives_workspace_ix` (workspace_id) WHERE op_archived_at IS NULL
- UNIQUE INDEX `operatives_system_key_uk` (workspace_id, op_system_key) WHERE op_system_key IS NOT NULL

### `operative_targets`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| opt_target_type | text | нет |  | workspace/member/team |
| opt_target_id | uuid | да |  | id цели, кроме target_type=workspace |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (operative_id) → `operatives`(id) ON DELETE CASCADE

- INDEX `operative_targets_operative_ix` (operative_id)

### `operative_mcp_links`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| space_mcp_server_id | uuid | нет |  | ссылка на space_mcp_servers |
| opml_enabled | boolean | нет | true | включён ли этот MCP-сервер у данного агента |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- PK: (operative_id, space_mcp_server_id)
- FK: (operative_id) → `operatives`(id) ON DELETE CASCADE
- FK: (space_mcp_server_id) → `space_mcp_servers`(id) ON DELETE CASCADE

### `operative_capabilities`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| capability_id | uuid | нет |  | ссылка на capabilities (навык) |
| opcap_enabled | boolean | нет | true | включён ли этот навык у данного агента |
| opcap_config_override | jsonb | да |  | переопределение конфигурации навыка для агента |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- PK: (operative_id, capability_id)
- FK: (operative_id) → `operatives`(id) ON DELETE CASCADE
- FK: (capability_id) → `capabilities`(id) ON DELETE CASCADE

### `operative_disabled_local_skills`  _(файл 003_agents.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| executor_id | uuid | нет |  | ссылка на executors (runtime-исполнитель) |
| opdis_provider | text | нет |  | провайдер runtime, к которому относится отключённый локальный навык |
| opdis_root | text | нет |  | каталог локальных навыков: provider/universal/plugin |
| opdis_key | text | нет |  | собственный ключ локального навыка на стороне runtime |
| opdis_title | text | да |  | имя локального навыка |
| opdis_plugin | text | да |  | плагин, из которого пришёл локальный навык |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (operative_id, executor_id, opdis_root, opdis_key)
- FK: (operative_id) → `operatives`(id) ON DELETE CASCADE
- FK: (executor_id) → `executors`(id) ON DELETE CASCADE

### `crews`  _(файл 004_crews.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| crew_title | text | нет |  | название отряда |
| crew_summary | text | да |  | описание отряда |
| crew_instructions | text | да |  | инструкции отряда |
| crew_avatar_uri | text | да |  | URL аватара отряда |
| crew_leader_type | text | нет |  | тип лидера отряда: agent/member |
| crew_leader_id | uuid | нет |  | id лидера отряда |
| crew_creator_account_id | uuid | нет |  | кто создал отряд |
| crew_archived_at | timestamptz | да |  | момент архивации отряда |
| crew_archived_by | uuid | да |  | кто архивировал отряд |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (crew_creator_account_id) → `accounts`(id)
- FK: (crew_archived_by) → `accounts`(id)

- INDEX `crews_workspace_ix` (workspace_id) WHERE crew_archived_at IS NULL

### `crew_members`  _(файл 004_crews.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| crew_id | uuid | нет |  | ссылка на crews (отряд) |
| cm_member_type | text | нет |  | тип участника отряда: agent/member |
| cm_member_id | uuid | нет |  | id участника отряда |
| cm_role | text | нет | '' | роль участника в отряде (leader — зарезервировано) |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (crew_id, cm_member_type, cm_member_id)
- FK: (crew_id) → `crews`(id) ON DELETE CASCADE

- INDEX `crew_members_lookup_ix` (cm_member_type, cm_member_id)

### `initiatives`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| init_title | text | нет |  | название проекта |
| init_summary | text | да |  | описание проекта |
| init_icon | text | да |  | иконка проекта |
| init_status | text | нет | 'planned' | planned/in_progress/paused/completed/cancelled |
| init_priority | text | нет | 'none' | urgent/high/medium/low/none |
| init_lead_type | text | да |  | тип лидера проекта: member/agent |
| init_lead_id | uuid | да |  | id лидера проекта |
| init_start_date | date | да |  | дата начала проекта |
| init_due_date | date | да |  | срок проекта |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE

- INDEX `initiatives_workspace_ix` (workspace_id)
- INDEX `initiatives_title_trgm_ix` (init_title gin_trgm_ops)

### `initiative_resources`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| initiative_id | uuid | нет |  | ссылка на initiatives (проект) |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| ir_resource_type | text | нет |  | github_repo/local_directory |
| ir_resource_ref | jsonb | нет | '{}'::jsonb | ссылка на ресурс, форма зависит от resource_type |
| ir_label | text | да |  | подпись ресурса в UI |
| ir_position | integer | нет | 0 | порядок отображения |
| ir_created_by | uuid | да |  | кто добавил ресурс |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (initiative_id) → `initiatives`(id) ON DELETE CASCADE
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (ir_created_by) → `accounts`(id)

- INDEX `initiative_resources_initiative_ix` (initiative_id)

### `tags`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| tag_resource_type | text | нет |  | к чему применяется метка: issue/agent/skill |
| tag_label | text | нет |  | текст метки |
| tag_summary | text | да |  | описание метки |
| tag_color | text | нет |  | HEX-цвет метки |
| tag_usage_count | integer | нет | 0 | сколько раз метка использована |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (workspace_id, tag_resource_type, tag_label)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE

### `field_defs`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| fd_title | text | нет |  | название кастомного поля |
| fd_type | text | нет |  | text/number/select/multi_select/date/checkbox/url |
| fd_summary | text | да |  | описание поля |
| fd_icon | text | да |  | иконка поля |
| fd_config | jsonb | нет | '{}'::jsonb | конфигурация поля (варианты select и т.п.) |
| fd_position | double precision | нет | 0 | порядок отображения поля |
| fd_archived_at | timestamptz | да |  | момент архивации поля |
| fd_usage_count | integer | нет | 0 | в скольких тикетах поле заполнено |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE

- INDEX `field_defs_workspace_ix` (workspace_id) WHERE fd_archived_at IS NULL

### `tickets`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| tk_seq_number | integer | нет |  | монотонный номер тикета в воркспейсе |
| tk_display_key | text | нет |  | человекочитаемый идентификатор PREFIX-NUMBER |
| tk_headline | text | нет |  | заголовок тикета |
| tk_narrative | text | да |  | описание тикета |
| tk_status | text | нет | 'backlog' | backlog/todo/in_progress/in_review/done/blocked/cancelled |
| tk_priority | text | нет | 'none' | urgent/high/medium/low/none |
| tk_assignee_type | text | да |  | member/agent/squad |
| tk_assignee_id | uuid | да |  | id исполнителя |
| tk_creator_type | text | нет |  | member/agent |
| tk_creator_id | uuid | нет |  | id создателя тикета |
| tk_parent_ticket_id | uuid | да |  | родительский тикет в иерархии подзадач |
| initiative_id | uuid | да |  | ссылка на initiatives (проект) |
| tk_position | double precision | да |  | дробная позиция для ручной сортировки |
| tk_stage | integer | да |  | номер под-этапа внутри статуса |
| tk_start_date | date | да |  | дата начала работы |
| tk_due_date | date | да |  | срок выполнения |
| tk_metadata | jsonb | нет | '{}'::jsonb | произвольные метаданные тикета |
| tk_custom_field_values | jsonb | нет | '{}'::jsonb | значения кастомных полей {field_def_id: значение} |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (workspace_id, tk_seq_number)
- UNIQUE: (workspace_id, tk_display_key)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (tk_parent_ticket_id) → `tickets`(id) ON DELETE SET NULL
- FK: (initiative_id) → `initiatives`(id) ON DELETE SET NULL

- INDEX `tickets_workspace_status_ix` (workspace_id, tk_status)
- INDEX `tickets_assignee_ix` (tk_assignee_type, tk_assignee_id) WHERE tk_assignee_id IS NOT NULL
- INDEX `tickets_initiative_ix` (initiative_id) WHERE initiative_id IS NOT NULL
- INDEX `tickets_parent_ix` (tk_parent_ticket_id) WHERE tk_parent_ticket_id IS NOT NULL
- INDEX `tickets_headline_trgm_ix` (tk_headline gin_trgm_ops)
- INDEX `tickets_narrative_trgm_ix` (tk_narrative gin_trgm_ops) WHERE tk_narrative IS NOT NULL

### `ticket_tag_links`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| ticket_id | uuid | нет |  | ссылка на tickets (тикет) |
| tag_id | uuid | нет |  | ссылка на tags (метка) |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (ticket_id, tag_id)
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE
- FK: (tag_id) → `tags`(id) ON DELETE CASCADE

### `operative_tag_links`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| tag_id | uuid | нет |  | ссылка на tags (метка) |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (operative_id, tag_id)
- FK: (operative_id) → `operatives`(id) ON DELETE CASCADE
- FK: (tag_id) → `tags`(id) ON DELETE CASCADE

### `capability_tag_links`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| capability_id | uuid | нет |  | ссылка на capabilities (навык) |
| tag_id | uuid | нет |  | ссылка на tags (метка) |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (capability_id, tag_id)
- FK: (capability_id) → `capabilities`(id) ON DELETE CASCADE
- FK: (tag_id) → `tags`(id) ON DELETE CASCADE

### `ticket_notes`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| ticket_id | uuid | нет |  | ссылка на tickets (тикет) |
| tn_author_type | text | нет |  | member/agent/system |
| tn_author_id | uuid | нет |  | id автора комментария |
| tn_body | text | нет |  | текст комментария |
| tn_kind | text | нет | 'comment' | comment/status_change/progress_update/system |
| tn_parent_note_id | uuid | да |  | родительский комментарий (тред) |
| tn_resolved_at | timestamptz | да |  | момент разрешения треда |
| tn_resolved_by_type | text | да |  | кто разрешил тред: member/agent |
| tn_resolved_by_id | uuid | да |  | id разрешившего тред |
| tn_source_dispatch_job_id | uuid | да |  | запуск агента, из которого пришёл комментарий (для атрибуции) |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE
- FK: (tn_parent_note_id) → `ticket_notes`(id) ON DELETE CASCADE
- FK (добавлен в 008/011 после создания родительской таблицы): (tn_source_dispatch_job_id) → `dispatch_jobs`(id) ON DELETE SET NULL

- INDEX `ticket_notes_ticket_ix` (ticket_id, created_at)
- INDEX `ticket_notes_parent_ix` (tn_parent_note_id) WHERE tn_parent_note_id IS NOT NULL

### `note_marks`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| ticket_note_id | uuid | нет |  | ссылка на ticket_notes (комментарий тикета) |
| nm_actor_type | text | нет |  | member/agent |
| nm_actor_id | uuid | нет |  | id поставившего реакцию |
| nm_emoji | text | нет |  | эмодзи реакции |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji)
- FK: (ticket_note_id) → `ticket_notes`(id) ON DELETE CASCADE

### `ticket_marks`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| ticket_id | uuid | нет |  | ссылка на tickets (тикет) |
| tm_actor_type | text | нет |  | member/agent |
| tm_actor_id | uuid | нет |  | id поставившего реакцию |
| tm_emoji | text | нет |  | эмодзи реакции |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (ticket_id, tm_actor_type, tm_actor_id, tm_emoji)
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE

### `ticket_subscribers`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| ticket_id | uuid | нет |  | ссылка на tickets (тикет) |
| tsub_watcher_type | text | нет |  | member/agent |
| tsub_watcher_id | uuid | нет |  | id подписчика |
| tsub_reason | text | нет |  | creator/assignee/commenter/mentioned/manual/autopilot |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (ticket_id, tsub_watcher_type, tsub_watcher_id)
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE

### `ticket_pr_links`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| ticket_id | uuid | нет |  | ссылка на tickets (тикет) |
| tpr_provider | text | нет |  | провайдер VCS (github и т.п.) |
| tpr_url | text | нет |  | URL pull request |
| tpr_number | integer | нет |  | номер pull request |
| tpr_title | text | да |  | заголовок pull request |
| tpr_state | text | да |  | состояние pull request |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (ticket_id, tpr_url)
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE

### `ticket_activity`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| ticket_id | uuid | да |  | ссылка на tickets (тикет) |
| ta_actor_type | text | нет |  | member/agent/system |
| ta_actor_id | uuid | да |  | id инициатора события |
| ta_action | text | нет |  | код события (status_changed, assignee_changed, ...) |
| ta_details | jsonb | нет | '{}'::jsonb | детали события, произвольный JSON |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE

- INDEX `ticket_activity_ticket_ix` (ticket_id, created_at)

### `ticket_bookmarks`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| ticket_id | uuid | нет |  | ссылка на tickets (тикет) |
| bm_position | double precision | нет | 0 | дробная позиция закрепления в списке |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (account_id, ticket_id)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE

### `initiative_bookmarks`  _(файл 005_tasks.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| initiative_id | uuid | нет |  | ссылка на initiatives (проект) |
| bm_position | double precision | нет | 0 | дробная позиция закрепления в списке |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (account_id, initiative_id)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE
- FK: (initiative_id) → `initiatives`(id) ON DELETE CASCADE

### `sentinels`  _(файл 006_sentinels.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| sen_title | text | нет |  | название автопилота |
| sen_summary | text | да |  | описание автопилота |
| initiative_id | uuid | да |  | ссылка на initiatives (проект) |
| sen_assignee_type | text | нет |  | agent/squad |
| sen_assignee_id | uuid | нет |  | id исполнителя автопилота |
| sen_status | text | нет | 'active' | active/paused/archived |
| sen_execution_mode | text | нет |  | create_issue/run_only |
| sen_issue_title_template | text | да |  | шаблон заголовка создаваемого тикета |
| sen_created_by_type | text | нет |  | member/agent |
| sen_created_by_id | uuid | нет |  | id создателя автопилота |
| sen_last_run_at | timestamptz | да |  | момент последнего запуска |
| sen_is_template | boolean | нет | false | является ли шаблоном автопилота |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (initiative_id) → `initiatives`(id) ON DELETE SET NULL

- INDEX `sentinels_workspace_ix` (workspace_id)

### `sentinel_triggers`  _(файл 006_sentinels.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| sentinel_id | uuid | нет |  | ссылка на sentinels (автопилот) |
| strig_kind | text | нет |  | schedule/webhook/api |
| strig_enabled | boolean | нет | true | включён ли триггер |
| strig_cron_expression | text | да |  | cron-выражение для kind=schedule |
| strig_timezone | text | да |  | часовой пояс для cron-выражения |
| strig_next_run_at | timestamptz | да |  | момент следующего срабатывания |
| strig_webhook_token_digest | text | да |  | хэш токена вебхука |
| strig_webhook_path | text | да |  | путь вебхука, уникален по всему деплою |
| strig_provider | text | да |  | generic/github, только для kind=webhook |
| strig_signing_secret_sealed | bytea | да |  | зашифрованный секрет подписи вебхука |
| strig_label | text | да |  | подпись триггера в UI |
| strig_last_fired_at | timestamptz | да |  | момент последнего срабатывания |
| strig_event_filters | jsonb | нет | '[]'::jsonb | условия срабатывания на входящее событие |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (sentinel_id) → `sentinels`(id) ON DELETE CASCADE

- INDEX `sentinel_triggers_sentinel_ix` (sentinel_id)
- UNIQUE INDEX `sentinel_triggers_webhook_path_uk` (strig_webhook_path) WHERE strig_webhook_path IS NOT NULL

### `sentinel_runs`  _(файл 006_sentinels.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| sentinel_id | uuid | нет |  | ссылка на sentinels (автопилот) |
| sentinel_trigger_id | uuid | да |  | ссылка на sentinel_triggers |
| srun_source | text | нет |  | schedule/manual/webhook/api |
| srun_status | text | нет | 'running' | issue_created/running/completed/failed/skipped |
| ticket_id | uuid | да |  | ссылка на tickets (тикет) |
| srun_dispatch_job_id | uuid | да |  | запуск задачи агента, порождённый этим run |
| srun_completed_at | timestamptz | да |  | момент завершения run |
| srun_failure_reason | text | да |  | причина неудачи |
| srun_reason_code | text | да |  | код причины пропуска run |
| srun_trigger_payload | jsonb | да |  | исходный payload, вызвавший run |
| srun_result | jsonb | да |  | итоговый результат run |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (sentinel_id) → `sentinels`(id) ON DELETE CASCADE
- FK: (sentinel_trigger_id) → `sentinel_triggers`(id) ON DELETE SET NULL
- FK: (ticket_id) → `tickets`(id) ON DELETE SET NULL
- FK (добавлен в 008/011 после создания родительской таблицы): (srun_dispatch_job_id) → `dispatch_jobs`(id) ON DELETE SET NULL

- INDEX `sentinel_runs_sentinel_ix` (sentinel_id, created_at DESC)

### `sentinel_subscribers`  _(файл 006_sentinels.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| sentinel_id | uuid | нет |  | ссылка на sentinels (автопилот) |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (sentinel_id, account_id)
- FK: (sentinel_id) → `sentinels`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

### `sentinel_collaborators`  _(файл 006_sentinels.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| sentinel_id | uuid | нет |  | ссылка на sentinels (автопилот) |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| sencol_granted_by | uuid | нет |  | кто выдал доступ соавтора |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (sentinel_id, account_id)
- FK: (sentinel_id) → `sentinels`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE
- FK: (sencol_granted_by) → `accounts`(id)

### `webhook_events`  _(файл 006_sentinels.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| sentinel_id | uuid | нет |  | ссылка на sentinels (автопилот) |
| sentinel_trigger_id | uuid | нет |  | ссылка на sentinel_triggers |
| whe_provider | text | нет |  | провайдер источника вебхука |
| whe_event | text | нет |  | тип события провайдера |
| whe_dedupe_key | text | да |  | ключ дедупликации |
| whe_dedupe_source | text | да |  | откуда взят ключ дедупликации |
| whe_signature_status | text | нет | 'not_required' | not_required/valid/invalid/missing |
| whe_status | text | нет | 'queued' | queued/dispatched/rejected/ignored/failed |
| whe_attempt_count | integer | нет | 0 | число попыток обработки |
| whe_dispatch_attempts | integer | нет | 0 | число попыток передачи на исполнение |
| whe_available_at | timestamptz | нет | now() | не раньше какого момента доставка может обрабатываться повторно |
| whe_content_type | text | да |  | Content-Type входящего запроса |
| whe_response_status | integer | да |  | HTTP-код ответа, отданного источнику |
| sentinel_run_id | uuid | да |  | ссылка на sentinel_runs (запуск автопилота) |
| whe_replayed_from_id | uuid | да |  | исходная доставка, если это повтор |
| whe_error | text | да |  | текст ошибки обработки |
| whe_selected_headers | jsonb | да |  | отобранные заголовки запроса |
| whe_raw_body | text | да |  | исходное тело запроса |
| whe_response_body | text | да |  | тело ответа, отданного источнику |
| whe_received_at | timestamptz | нет | now() | момент получения запроса |
| whe_last_attempt_at | timestamptz | да |  | момент последней попытки обработки |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (sentinel_id) → `sentinels`(id) ON DELETE CASCADE
- FK: (sentinel_trigger_id) → `sentinel_triggers`(id) ON DELETE CASCADE
- FK: (sentinel_run_id) → `sentinel_runs`(id) ON DELETE SET NULL
- FK: (whe_replayed_from_id) → `webhook_events`(id)

- INDEX `webhook_events_trigger_ix` (sentinel_trigger_id, created_at DESC)
- INDEX `webhook_events_dedupe_ix` (sentinel_trigger_id, whe_dedupe_key) WHERE whe_dedupe_key IS NOT NULL

### `convos`  _(файл 007_chat.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| cv_creator_account_id | uuid | нет |  | кто создал чат-сессию |
| initiative_id | uuid | да |  | ссылка на initiatives (проект) |
| cv_title | text | нет | '' | заголовок чат-сессии |
| cv_status | text | нет | 'active' | active/archived |
| cv_pinned | boolean | нет | false | закреплена ли сессия |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (operative_id) → `operatives`(id)
- FK: (cv_creator_account_id) → `accounts`(id)
- FK: (initiative_id) → `initiatives`(id) ON DELETE SET NULL

- INDEX `convos_workspace_ix` (workspace_id, updated_at DESC)
- INDEX `convos_operative_ix` (operative_id)

### `convo_messages`  _(файл 007_chat.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| convo_id | uuid | нет |  | ссылка на convos (чат-сессия) |
| cvm_role | text | нет |  | user/assistant/system |
| cvm_body | text | нет |  | текст сообщения |
| dispatch_job_id | uuid | да |  | ссылка на dispatch_jobs (строка очереди задач) |
| cvm_failure_reason | text | да |  | причина неудачи ответа агента |
| cvm_elapsed_ms | integer | да |  | время генерации ответа, мс |
| cvm_kind | text | нет | 'message' | message/no_response |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (convo_id) → `convos`(id) ON DELETE CASCADE
- FK (добавлен в 008/011 после создания родительской таблицы): (dispatch_job_id) → `dispatch_jobs`(id) ON DELETE SET NULL

- INDEX `convo_messages_convo_ix` (convo_id, created_at)

### `convo_drafts`  _(файл 007_chat.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| convo_id | uuid | нет |  | ссылка на convos (чат-сессия) |
| dispatch_job_id | uuid | да |  | ссылка на dispatch_jobs (строка очереди задач) |
| cvd_body | text | нет |  | черновик сообщения |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (convo_id) → `convos`(id) ON DELETE CASCADE
- FK (добавлен в 008/011 после создания родительской таблицы): (dispatch_job_id) → `dispatch_jobs`(id) ON DELETE SET NULL

- INDEX `convo_drafts_convo_ix` (convo_id)

### `convo_pinned_operatives`  _(файл 007_chat.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| cvp_position | double precision | нет | 0 | порядок закреплённого агента в сайдбаре |
| created_at | timestamptz | нет | now() | момент создания строки |

- PK: (account_id, operative_id)
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE
- FK: (operative_id) → `operatives`(id) ON DELETE CASCADE

### `convo_channel_links`  _(файл 007_chat.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| convo_id | uuid | нет |  | ссылка на convos (чат-сессия) |
| cvc_channel_type | text | нет |  | тип внешнего канала (slack и т.п.) |
| cvc_external_channel_id | text | да |  | id канала во внешней системе |
| cvc_external_thread_id | text | да |  | id треда во внешней системе |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (convo_id, cvc_channel_type)
- FK: (convo_id) → `convos`(id) ON DELETE CASCADE

### `dispatch_jobs`  _(файл 008_dispatch.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| executor_id | uuid | нет |  | ссылка на executors (runtime-исполнитель) |
| ticket_id | uuid | да |  | ссылка на tickets (тикет) |
| initiative_id | uuid | да |  | ссылка на initiatives (проект) |
| crew_id | uuid | да |  | ссылка на crews (отряд) |
| convo_id | uuid | да |  | ссылка на convos (чат-сессия) |
| sentinel_run_id | uuid | да |  | ссылка на sentinel_runs (запуск автопилота) |
| dj_kind | text | нет |  | issue/chat/autopilot/quick_create |
| dj_status | text | нет | 'queued' | статус жизненного цикла строки очереди |
| dj_priority | integer | нет | 0 | приоритет очереди (больше — раньше) |
| dj_thread_title | text | да |  | имя исходного треда/канала для логирования |
| dj_dispatched_at | timestamptz | да |  | момент выдачи демону |
| dj_started_at | timestamptz | да |  | момент начала выполнения (StartTask) |
| dj_completed_at | timestamptz | да |  | момент завершения (успех или неудача) |
| dj_result | jsonb | да |  | свободная форма результата (CompleteTask/FailTask) |
| dj_error | text | да |  | текст ошибки после failed |
| dj_failure_reason | text | да |  | машиночитаемая категория ошибки |
| dj_attempt | integer | нет | 1 | номер попытки, считая с 1 |
| dj_max_attempts | integer | нет | 2 | предел попыток |
| dj_parent_job_id | uuid | да |  | задача, от которой это ретрай/ререн/делегирование |
| dj_is_leader | boolean | нет | false | координирующая задача запуска отряда |
| dj_prior_session_id | text | да |  | id предыдущей сессии runtime для возобновления |
| dj_prior_work_dir | text | да |  | рабочая директория предыдущего запуска |
| dj_resume_unavailable | boolean | нет | false | возобновление предыдущей сессии невозможно |
| dj_work_dir | text | да |  | рабочая директория текущего запуска |
| dj_trigger_note_id | uuid | да |  | комментарий, вызвавший запуск |
| dj_coalesced_note_ids | uuid[] | нет | '{}' | id комментариев, объединённых в один запуск |
| dj_delivered_note_ids | uuid[] | нет | '{}' | id комментариев, уже доставленных демону (at-least-once) |
| dj_trigger_thread_id | uuid | да |  | id треда комментария-триггера |
| dj_trigger_summary | text | да |  | короткое описание причины запуска для UI |
| dj_trigger_author_type | text | да |  | member/agent/system |
| dj_chat_channel_type | text | да |  | поверхность чата: app/slack/... |
| dj_chat_in_thread | boolean | нет | false | отвечать ли в существующем треде канала |
| dj_chat_message | text | да |  | текст сообщения пользователя, на которое отвечает агент |
| dj_chat_intro | boolean | нет | false | первое сообщение новой чат-сессии |
| dj_quick_create_prompt | text | да |  | текст промпта быстрого создания |
| dj_quick_create_priority | text | да |  | приоритет из запроса быстрого создания |
| dj_quick_create_due_date | date | да |  | срок из запроса быстрого создания |
| dj_handoff_note | text | да |  | заметка того, кто передал задачу агенту |
| dj_mcp_policy | jsonb | да |  | ограничения периметра на MCP-серверы/команды для этого запуска |
| dj_initiator_type | text | да |  | member/agent/system |
| dj_initiator_id | uuid | да |  | id инициатора |
| dj_attribution | jsonb | нет | '{}'::jsonb | полная атрибуция задачи (TaskAttribution) для аудита |
| dj_connected_apps | jsonb | нет | '[]'::jsonb | снимок внешних интеграций, доступных агенту в этом запуске |
| dj_repo_refs | jsonb | нет | '[]'::jsonb | снимок репозиториев, доступных агенту в этом запуске |
| dj_context_snapshot | jsonb | нет | '{}'::jsonb | собранный на момент постановки контекст (project/parent/requester/autopilot-заголовки) |
| dj_claim_secret_digest | text | да |  | хэш короткоживущего токена claim (auth_token) |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (operative_id) → `operatives`(id)
- FK: (executor_id) → `executors`(id)
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE
- FK: (initiative_id) → `initiatives`(id) ON DELETE SET NULL
- FK: (crew_id) → `crews`(id) ON DELETE SET NULL
- FK: (convo_id) → `convos`(id) ON DELETE CASCADE
- FK: (sentinel_run_id) → `sentinel_runs`(id) ON DELETE SET NULL
- FK: (dj_parent_job_id) → `dispatch_jobs`(id)
- FK: (dj_trigger_note_id) → `ticket_notes`(id) ON DELETE SET NULL

- INDEX `dispatch_jobs_claim_ix` (executor_id, dj_priority DESC, created_at) WHERE dj_status = 'queued'
- INDEX `dispatch_jobs_ticket_ix` (ticket_id) WHERE ticket_id IS NOT NULL
- INDEX `dispatch_jobs_convo_ix` (convo_id) WHERE convo_id IS NOT NULL
- INDEX `dispatch_jobs_workspace_status_ix` (workspace_id, dj_status)
- INDEX `dispatch_jobs_operative_ix` (operative_id, dj_status)

### `dispatch_messages`  _(файл 008_dispatch.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| dispatch_job_id | uuid | нет |  | ссылка на dispatch_jobs (строка очереди задач) |
| dm_seq | integer | нет |  | порядковый номер сообщения в рамках запуска |
| dm_kind | text | нет |  | text/thinking/tool_use/tool_result/error |
| dm_tool | text | да |  | имя инструмента для tool_use/tool_result |
| dm_body | text | да |  | текстовое содержимое сообщения |
| dm_input | jsonb | да |  | аргументы вызова инструмента |
| dm_output | text | да |  | результат вызова инструмента |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (dispatch_job_id, dm_seq)
- FK: (dispatch_job_id) → `dispatch_jobs`(id) ON DELETE CASCADE

- INDEX `dispatch_messages_job_ix` (dispatch_job_id, dm_seq)

### `dispatch_usage`  _(файл 008_dispatch.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| dispatch_job_id | uuid | нет |  | ссылка на dispatch_jobs (строка очереди задач) |
| du_provider | text | да |  | провайдер модели |
| du_model | text | нет |  | идентификатор модели |
| du_input_tokens | bigint | нет | 0 | входные токены |
| du_output_tokens | bigint | нет | 0 | выходные токены |
| du_cache_read_tokens | bigint | нет | 0 | токены, прочитанные из кэша |
| du_cache_write_tokens | bigint | нет | 0 | токены, записанные в кэш |
| du_cost_usd_ticks | bigint | нет | 0 | стоимость в тиках (1e-4 USD) |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (dispatch_job_id) → `dispatch_jobs`(id) ON DELETE CASCADE

- INDEX `dispatch_usage_job_ix` (dispatch_job_id)

### `assets`  _(файл 009_feed.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| ticket_id | uuid | да |  | ссылка на tickets (тикет) |
| ticket_note_id | uuid | да |  | ссылка на ticket_notes (комментарий тикета) |
| convo_id | uuid | да |  | ссылка на convos (чат-сессия) |
| convo_message_id | uuid | да |  | ссылка на convo_messages (сообщение чата) |
| dispatch_job_id | uuid | да |  | ссылка на dispatch_jobs (строка очереди задач) |
| as_uploader_type | text | нет |  | member/agent |
| as_uploader_id | uuid | нет |  | id загрузившего файл |
| as_filename | text | нет |  | исходное имя файла |
| as_storage_uri | text | нет |  | внутренний URL в хранилище |
| as_download_path | text | нет |  | путь/подписанный URL для скачивания |
| as_markdown_ref | text | нет |  | готовая markdown-ссылка на файл |
| as_download_ticket_uri | text | да |  | одноразовый билет на скачивание |
| as_content_type | text | нет |  | MIME-тип файла |
| as_size_bytes | bigint | нет |  | размер файла в байтах |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE
- FK: (ticket_note_id) → `ticket_notes`(id) ON DELETE CASCADE
- FK: (convo_id) → `convos`(id) ON DELETE CASCADE
- FK: (convo_message_id) → `convo_messages`(id) ON DELETE CASCADE
- FK: (dispatch_job_id) → `dispatch_jobs`(id) ON DELETE CASCADE

- INDEX `assets_workspace_ix` (workspace_id)
- INDEX `assets_ticket_ix` (ticket_id) WHERE ticket_id IS NOT NULL
- INDEX `assets_note_ix` (ticket_note_id) WHERE ticket_note_id IS NOT NULL
- INDEX `assets_convo_message_ix` (convo_message_id) WHERE convo_message_id IS NOT NULL
- INDEX `assets_dispatch_job_ix` (dispatch_job_id) WHERE dispatch_job_id IS NOT NULL

### `alerts`  _(файл 009_feed.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| al_recipient_type | text | нет |  | member/agent |
| al_recipient_id | uuid | нет |  | id получателя уведомления |
| al_kind | text | нет |  | тип уведомления (issue_assigned, new_comment, ...) |
| al_severity | text | нет | 'info' | info/action_required/attention |
| ticket_id | uuid | да |  | ссылка на tickets (тикет) |
| al_headline | text | нет |  | заголовок уведомления |
| al_body | text | да |  | текст уведомления |
| al_read_at | timestamptz | да |  | момент прочтения (NULL — непрочитано) |
| al_archived_at | timestamptz | да |  | момент архивации (NULL — не архивировано) |
| al_actor_type | text | да |  | member/agent/system |
| al_actor_id | uuid | да |  | id того, чьё действие породило уведомление |
| al_details | jsonb | нет | '{}'::jsonb | структурированные детали, зависят от al_kind |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (ticket_id) → `tickets`(id) ON DELETE CASCADE

- INDEX `alerts_recipient_ix` (workspace_id, al_recipient_type, al_recipient_id, created_at DESC) WHERE al_archived_at IS NULL

### `notification_prefs`  _(файл 009_feed.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| np_groups | jsonb | нет | '{}'::jsonb | предпочтения по группам уведомлений {группа: all|muted} |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- PK: (workspace_id, account_id)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

### `vcs_connections`  _(файл 010_integrations.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| vcs_provider | text | нет |  | провайдер VCS (gitlab/bitbucket/...) |
| vcs_instance_uri | text | нет |  | адрес инстанса VCS |
| vcs_account_login | text | да |  | логин подключённой учётной записи |
| vcs_webhook_uri | text | да |  | полный URL вебхука |
| vcs_webhook_path | text | да |  | путь вебхука |
| vcs_access_token_sealed | bytea | нет |  | зашифрованный access token |
| vcs_webhook_secret_sealed | bytea | да |  | зашифрованный секрет подписи вебхука |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE

- INDEX `vcs_connections_workspace_ix` (workspace_id)

### `github_installations`  _(файл 010_integrations.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| gh_installation_id | bigint | да |  | id установки GitHub App |
| gh_account_login | text | нет |  | логин организации/пользователя GitHub |
| gh_account_type | text | нет |  | тип аккаунта GitHub |
| gh_account_avatar_uri | text | да |  | URL аватара аккаунта GitHub |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE

- INDEX `github_installations_workspace_ix` (workspace_id)

### `slack_installations`  _(файл 010_integrations.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| operative_id | uuid | нет |  | ссылка на operatives (агент) |
| sl_team_id | text | нет |  | id команды Slack |
| sl_bot_user_id | text | да |  | id бота в Slack |
| sl_bot_token_sealed | bytea | да |  | зашифрованный bot-токен |
| sl_app_token_sealed | bytea | да |  | зашифрованный app-токен |
| sl_installer_account_id | uuid | нет |  | кто установил интеграцию |
| sl_status | text | нет | 'active' | статус установки |
| sl_installed_at | timestamptz | да |  | момент установки |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (operative_id) → `operatives`(id)
- FK: (sl_installer_account_id) → `accounts`(id)

- INDEX `slack_installations_workspace_ix` (workspace_id)

### `composio_connections`  _(файл 010_integrations.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| account_id | uuid | нет |  | ссылка на accounts (учётная запись) |
| cx_toolkit_slug | text | нет |  | слаг набора инструментов Composio |
| cx_external_connection_id | text | да |  | id подключения на стороне Composio |
| cx_status | text | нет | 'pending' | статус подключения |
| cx_connected_at | timestamptz | да |  | момент подключения |
| cx_last_used_at | timestamptz | да |  | момент последнего использования |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (workspace_id, account_id, cx_toolkit_slug)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (account_id) → `accounts`(id) ON DELETE CASCADE

### `platform_admins`  _(файл 011_governance.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| account_id | uuid | нет |  | ссылка на accounts (учётная запись); PK |
| pa_granted_by | uuid | нет |  | кто выдал права администратора деплоя |
| pa_granted_at | timestamptz | нет | now() | момент выдачи прав |

- FK: (account_id) → `accounts`(id) ON DELETE CASCADE
- FK: (pa_granted_by) → `accounts`(id)

### `platform_admin_requests`  _(файл 011_governance.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| par_action | text | нет |  | grant/revoke |
| par_target_account_id | uuid | да |  | учётная запись — цель заявки |
| par_target_email | text | да |  | email цели, если аккаунта ещё нет |
| par_requested_by | uuid | нет |  | кто подал заявку |
| par_status | text | нет | 'pending' | статус заявки |
| par_requested_at | timestamptz | нет | now() | момент подачи заявки |
| par_resolved_at | timestamptz | да |  | момент решения по заявке |

- FK: (par_target_account_id) → `accounts`(id)
- FK: (par_requested_by) → `accounts`(id)

### `platform_audit_log`  _(файл 011_governance.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| paud_source | text | нет |  | admin/auth |
| paud_action | text | нет |  | код действия |
| paud_actor_account_id | uuid | да |  | учётная запись, выполнившая действие |
| paud_actor_type | text | да |  | тип актора (человек/сервис) |
| paud_actor_id | text | да |  | внешний id актора |
| paud_actor_role | text | да |  | роль актора на момент действия |
| paud_target_type | text | да |  | тип объекта действия |
| paud_target_id | text | да |  | id объекта действия |
| paud_outcome | text | да |  | итог действия |
| paud_reason | text | да |  | причина/комментарий |
| paud_before_hash | text | да |  | хэш состояния до изменения |
| paud_after_hash | text | да |  | хэш состояния после изменения |
| workspace_id | uuid | да |  | воркспейс-владелец строки |
| paud_request_id | text | да |  | id запроса (X-Request-ID) |
| paud_client_ip_digest | text | да |  | хэш IP-адреса клиента |
| paud_client_agent | text | да |  | User-Agent клиента |
| created_at | timestamptz | нет | now() | момент создания строки |

- FK: (paud_actor_account_id) → `accounts`(id)
- FK: (workspace_id) → `spaces`(id) ON DELETE SET NULL

- INDEX `platform_audit_log_created_ix` (created_at DESC)
- INDEX `platform_audit_log_workspace_ix` (workspace_id) WHERE workspace_id IS NOT NULL

### `platform_mcp_servers`  _(файл 011_governance.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| pmcp_name | text | нет |  | имя сервера в каталоге деплоя |
| pmcp_transport | text | нет | 'unknown' | stdio/http/unknown |
| pmcp_config_sealed | bytea | да |  | зашифрованная конфигурация сервера |
| pmcp_credential_schema | jsonb | нет | '[]'::jsonb | схема полей учётных данных сервера |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- UNIQUE: (pmcp_name)

### `platform_policy`  _(файл 011_governance.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | smallint | нет | 1 | первичный ключ; PK |
| pp_body | jsonb | нет | '{}'::jsonb | тело политики деплоя (llm/mcp/session) |
| pp_updated_at | timestamptz | да |  | момент последнего изменения политики |

### `provisioning_pins`  _(файл 011_governance.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| workspace_id | uuid | нет |  | воркспейс-владелец строки |
| prov_package_name | text | нет |  | имя пакета (skill/mcp-server/runtime) |
| prov_package_type | text | нет |  | skill/mcp-server/runtime |
| prov_version | text | нет |  | закреплённая версия пакета |
| prov_enabled | boolean | нет | true | включён ли пакет в воркспейсе |
| prov_updated_by | uuid | да |  | кто последним менял закрепление |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- PK: (workspace_id, prov_package_name, prov_package_type)
- FK: (workspace_id) → `spaces`(id) ON DELETE CASCADE
- FK: (prov_updated_by) → `accounts`(id)

### `wallet_balances`  _(файл 012_billing.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| wallet_owner_key | text | нет |  | непрозрачный ключ владельца кошелька (воркспейс или деплой); PK |
| wb_balance_micro | numeric(20,6) | нет | 0 | баланс в микро-единицах валюты |
| wb_balance_credit | numeric(20,6) | нет | 0 | баланс в кредитах |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

### `wallet_transactions`  _(файл 012_billing.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| wallet_owner_key | text | нет |  | непрозрачный ключ владельца кошелька (воркспейс или деплой) |
| wt_idempotency_key | text | нет |  | ключ идемпотентности операции |
| wt_tx_type | text | нет |  | topup/deduction/refund/expire/adjustment |
| wt_source | text | нет |  | gateway/fleet/topup/refund/admin/system |
| wt_amount_micro | numeric(20,6) | нет |  | сумма операции в микро-единицах |
| wt_balance_after | numeric(20,6) | нет |  | баланс после операции |
| wt_reference_id | text | да |  | id связанной внешней сущности |
| wt_description | text | да |  | описание операции |
| wt_metadata | jsonb | нет | '{}'::jsonb | произвольные метаданные операции |
| created_at | timestamptz | нет | now() | момент создания строки |

- UNIQUE: (wallet_owner_key, wt_idempotency_key)

- INDEX `wallet_transactions_owner_ix` (wallet_owner_key, created_at DESC)

### `wallet_credit_batches`  _(файл 012_billing.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| wallet_owner_key | text | нет |  | непрозрачный ключ владельца кошелька (воркспейс или деплой) |
| wcb_source_tx_id | uuid | да |  | транзакция, породившая пакет кредитов |
| wcb_source_type | text | нет |  | purchase/bonus/adjustment |
| wcb_total_micro | numeric(20,6) | нет |  | исходный размер пакета |
| wcb_remaining_micro | numeric(20,6) | нет |  | остаток пакета |
| wcb_valid_until | timestamptz | да |  | срок сгорания пакета |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (wcb_source_tx_id) → `wallet_transactions`(id)

- INDEX `wallet_credit_batches_owner_ix` (wallet_owner_key) WHERE wcb_remaining_micro > 0

### `wallet_topups`  _(файл 012_billing.up.sql)_

| Колонка | Тип | Null | По умолчанию | Смысл |
|---|---|---|---|---|
| id | uuid | нет | gen_random_uuid() | первичный ключ; PK |
| wallet_owner_key | text | нет |  | непрозрачный ключ владельца кошелька (воркспейс или деплой) |
| wtu_amount_cents | integer | нет |  | сумма платежа в центах |
| wtu_currency | text | нет | 'usd' | валюта платежа |
| wtu_credits | numeric(20,6) | нет | 0 | начисляемые кредиты |
| wtu_bonus_credits | numeric(20,6) | нет | 0 | бонусные кредиты |
| wtu_status | text | нет | 'pending' | pending/paid/credited/failed/canceled |
| wtu_tier_id | text | да |  | выбранный тариф пополнения |
| wtu_gateway_checkout_id | text | да |  | id сессии оплаты у платёжного шлюза |
| wtu_credit_batch_id | uuid | да |  | пакет кредитов, начисленный по этому платежу |
| created_at | timestamptz | нет | now() | момент создания строки |
| updated_at | timestamptz | нет | now() | момент последнего изменения строки |

- FK: (wtu_credit_batch_id) → `wallet_credit_batches`(id)

- INDEX `wallet_topups_owner_ix` (wallet_owner_key, created_at DESC)


## Перенос данных

`server2/cmd/import` переносит один воркспейс из старого `server`
(`api.example/v1.2.0`, тот же контракт `docs/50-api-contract.yaml`) в новую
БД **только через HTTP API контракта** — ни один SQL-запрос не идёт в базу
старого сервера напрямую (T-025 acceptance: "перенос данных — отдельный
скрипт экспорт/импорт через API-контракт, не SQL-в-SQL"). Инструмент
работает как обычный клиент API со своим personal access token с ролью
`owner` в исходном воркспейсе, и создаёт данные в `server2` через тот же
контракт (`server2` реализует его по T-026+).

### Сохранение исходных `uuid`

Все таблицы `server2` используют `uuid` первичным ключом (см. "Принципы"),
и импортёр **не генерирует новые id** — он передаёт исходный `id` каждой
сущности при создании соответствующей записи в `server2`, если API создания
допускает клиентский id (иначе — сразу после создания подменяет строку на
целевой id в той же импортирующей транзакции набора запросов через
служебный маршрут `POST /api/admin/import/reassign-id`, добавляемый в
`server2` специально для импортёра и недоступный обычным вызывающим вне
режима импорта). Это гарантирует, что все относительные ссылки
(`parent_issue_id`, `trigger_comment_id`, `assignee_id` и т.д.), которые
экспорт вычитывает по старому `id`, разрешаются в новом воркспейсе без
перестройки графа зависимостей.

### Порядок вызовов

Импорт идёт по зависимостям "сначала то, на что ссылаются" — тот же
порядок, что и порядок доменов миграций (`001`…`012`):

1. `GET /api/workspaces/{id}` → `POST /api/workspaces` (с `template_key:
   null`, затем `PATCH` полей, которые `POST` не принимает: `issue_prefix`,
   `settings`, `repos`, `avatar_url`).
2. `GET /api/workspaces/{id}/members` → для каждого участника: если
   аккаунт с таким `email` уже существует в целевом деплое — привязать по
   email; иначе создать через приглашение (`POST .../invitations`) и
   автопринять от имени этого пользователя (импорт предполагает, что все
   участники согласны на перенос — это фиксируется в UI запуска импорта,
   не в этом документе).
3. `GET /api/workspaces/{id}/runtime-profiles` → `POST .../runtime-profiles`.
4. `GET /api/workspaces/{id}/runtimes` (executors) → `POST .../runtimes`
   если API это допускает как явное действие, либо executors создаются
   лениво при первом `daemon/register` нового демона — в этом случае
   импортёр **не** переносит executors как данные (это живые подключения,
   не история), а только фиксирует маппинг "старый runtime → новый
   runtime" по `daemon_id`/`name`, использованный дальше для строк 5-9.
5. `GET /api/workspaces/{id}/agents` → `POST .../agents` (с явным `id` =
   исходному через reassign-id), включая `custom_env`/`mcp_config` —
   **не переносятся** (см. ниже), переносятся остальные поля.
6. `GET /api/workspaces/{id}/skills` (+ `GET .../skills/{id}` для файлов) →
   `POST .../skills` + `POST .../skills/{id}/files`.
7. `GET /api/workspaces/{id}/squads` (+ `.../members`) → `POST .../squads`
   + `POST .../squads/{id}/members`.
8. `GET /api/workspaces/{id}/labels`, `.../properties` → `POST` те же.
9. `GET /api/workspaces/{id}/projects` (+ `.../resources`) → `POST` те же.
10. `GET /api/workspaces/{id}/issues?include_sub_issues=true` постранично,
    в порядке `created_at ASC` (родители обычно раньше детей, но импортёр
    всё равно делает второй проход и патчит `parent_issue_id`/
    `assignee_id`/`project_id` после того, как весь набор задач создан —
    это снимает любую зависимость от порядка) → `POST .../issues` →
    второй проход `PATCH .../issues/{id}`.
11. Для каждой задачи: `GET .../issues/{id}/comments`, `.../reactions`,
    `.../subscribers`, `.../pull-requests` → соответствующие `POST`.
12. `GET /api/workspaces/{id}/autopilots` (+ `.../triggers`, без
    `webhook_token`/`signing_secret` — контракт их не отдаёт на чтение) →
    `POST` те же; `.../runs` переносятся как read-only история через
    отдельный bulk-эндпоинт импорта (обычный `POST .../runs` контрактом не
    предусмотрен, так как runs создаёт только сам сервер) — если целевой
    API его не выставляет, история запусков автопилотов **не переносится**
    и это фиксируется в отчёте импорта как ожидаемая потеря
    (не критичные данные, автопилот продолжит работать вперёд).
13. `GET /api/workspaces/{id}/chats` (+ `.../messages`) → `POST` те же.
14. Вложения: `GET .../attachments/{id}` (метаданные) +
    `GET .../attachments/{id}/download` (байты) → `POST /api/attachments`
    (multipart) с последующей привязкой к перенесённому родителю через
    `attachment_ids` при создании/патче родительской записи.
15. `GET /api/workspaces/{id}/inbox` (только непрочитанные/непросмотренные
    за настраиваемый период) → создаётся best-effort, потому что `InboxItem`
    не имеет отдельного `POST`-эндпоинта в контракте; вместо переноса
    исторических записей импортёр **регенерирует** уведомления штатной
    логикой сервера как побочный эффект действий 10-14 (например, создание
    задачи с назначенным исполнителем само порождает `issue_assigned`) —
    старые уведомления, если создание того же события в целевом сервере не
    порождает их автоматически, теряются осознанно (см. таблицу ниже).
16. `GET .../mcp-servers`, `.../config` → `PUT` те же (кроме секретов).

### Что не переносится через API, и почему

| Данные | Почему не переносятся |
|---|---|
| Хэши паролей / секреты сессий (`sess_secret_digest`, `mfa_secret_sealed`, MFA recovery-коды) | Контракт никогда не отдаёт их на чтение ни одному клиенту, включая владельца — они существуют только внутри старого сервера. Участники проходят обычный вход (email-код/OIDC/LDAP) и заново включают MFA в новом деплое. |
| PAT (`access_keys.ak_secret_digest`) | Токен виден только один раз, в момент выпуска (`CreatePersonalAccessTokenResponse.token`); контракт не отдаёт значение существующих токенов. Участники выпускают новые PAT после переноса. |
| Webhook `signing_secret` автопилотов, `webhook_token` | Контракт отдаёт `has_signing_secret: boolean` и `signing_secret_hint`, но не сам секрет. После переноса секрет генерируется заново (`PUT .../triggers/{id}/signing-secret`), внешний источник вебхука перенастраивается на новый URL/секрет вручную. |
| `runtime_config`/`mcp_config` агентов, если они несут `gateway.token` или иные учётные данные (поле замаскировано `***` в ответе) | Контракт маскирует секретную часть в каждом чтении — импортёр физически не может её прочитать. Переносятся немаскированные поля конфигурации; секретные поля владелец агента вводит заново в новом деплое. |
| LLM API-ключ воркспейса/пользователя (`cfg_llm_api_key_sealed`, `cfgo_llm_api_key_sealed`) | `WorkspaceConfigLayer`/`WorkspaceUserConfigOverride` отдают только `has_llm_api_key: boolean`. Ключ вводится заново через `PUT .../config`. |
| Учётные данные MCP-серверов воркспейса (`space_mcp_credentials`) | `WorkspaceMcpServer` отдаёt только `provided_credentials`/`missing_credentials` (какие ключи заполнены), не значения. Участники перезаполняют через `PUT .../mcp-servers/{id}/credentials`. |
| VCS/GitHub/Slack/Composio OAuth-токены (`vcs_access_token_sealed`, GitHub App installation-токены, `sl_bot_token_sealed`, Composio connection secrets) | Это токены сторонних систем, привязанные к OAuth-приложению/GitHub App конкретного деплоя; они физически не валидны в новом деплое (другой callback URL, другой client_id). Интеграции переустанавливаются через обычный OAuth-флоу после переноса. |
| Биллинговые балансы/история (`wallet_*`) | Контракт описывает биллинг как отдельный, платёжно-критичный домен вне зоны переноса воркспейса T-025 (перенос финансовых обязательств между деплоями — не техническая, а договорная операция); переносится по отдельному процессу с участием провайдера платежей, не этим импортёром. |
| Аудит (`platform_audit_log`), административные записи (`platform_admins`, `platform_admin_requests`) | Это данные **деплоя**, не воркспейса — они не принадлежат переносимому воркспейсу и не имеют смысла в целевом деплое (другие администраторы, другая история). |
| Исторические `dispatch_jobs`/`dispatch_messages`/`dispatch_usage`, кроме тех, что видны через `GET .../issues/{id}/...usage`/таймлайн | Контракт не выставляет bulk-экспорт очереди задач как таковой (это внутренний исполнительный журнал, не пользовательский контент); переносится только то, что видно через публичные ресурсы задачи (комментарии, usage-сводка по задаче через `IssueUsageSummary`, где такой эндпоинт есть) — сырые построчные записи выполнения остаются в старом деплое как архив. |
| Внешние `login_sessions` (активные сессии участников) | Сессии — атрибут браузера/устройства участника в старом деплое; после переноса участники просто заново входят в новом. |

Отчёт импортёра (`server2/cmd/import` печатает JSON-сводку в stdout) явно
перечисляет каждую из этих категорий с количеством пропущенных объектов, а
не молчаливо их опускает — так итог переноса воспроизводим и проверяем.
