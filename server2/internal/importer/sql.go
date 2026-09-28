package importer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// writer пишет сущности, прочитанные через client, напрямую в server2 по SQL
// (одна транзакция на весь импорт — см. cmd/import). Это не нарушает T-025
// «перенос только через HTTP API, не SQL-в-SQL»: то правило — про ЧТЕНИЕ из
// исходного сервера (только GET контракта, без прямого SQL к его БД); запись
// в НОВУЮ базу всегда была SQL-слоем (server2 ещё не реализует контракт как
// HTTP API — T-026+, см. docs/51-data-model.md «Перенос данных»).
//
// Идемпотентность: сущности с собственным id из контракта переносятся с этим
// же id (T-025 требует сохранения исходных uuid) через
// `ON CONFLICT (id) DO UPDATE` — повторный запуск обновляет строку до
// текущего состояния источника, не плодит дублей. Join-таблицы без
// собственного id используют `ON CONFLICT (естественный уникальный ключ) DO
// NOTHING`, а на table без единственного уникального индекса, подходящего
// под естественный ключ (operative_targets) — `INSERT ... WHERE NOT EXISTS`.
type writer struct {
	tx pgx.Tx
}

func newWriter(tx pgx.Tx) *writer { return &writer{tx: tx} }

func jsonb(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("importer: маршалинг jsonb: %w", err)
	}
	return string(b), nil
}

func jsonbArray(v any) (string, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("importer: маршалинг jsonb-массива: %w", err)
	}
	return string(b), nil
}

// --- accounts (001) --------------------------------------------------------

func (w *writer) upsertAccount(ctx context.Context, id, email, fullName string, avatarURL *string) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO accounts (id, acct_email, acct_full_name, acct_avatar_uri)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    acct_email      = EXCLUDED.acct_email,
    acct_full_name  = EXCLUDED.acct_full_name,
    acct_avatar_uri = EXCLUDED.acct_avatar_uri,
    updated_at      = now()`,
		id, email, fullName, avatarURL)
	if err != nil {
		return fmt.Errorf("importer: accounts %s: %w", id, err)
	}
	return nil
}

// --- spaces / space_members (002) ------------------------------------------

func (w *writer) upsertSpace(ctx context.Context, ws sourceWorkspace) error {
	settings, err := jsonb(ws.Settings)
	if err != nil {
		return err
	}
	repos, err := jsonbArray(ws.Repos)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO spaces (id, ws_title, ws_slug, ws_summary, ws_operating_context, ws_settings, ws_repo_refs, ws_ticket_prefix, ws_avatar_uri)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    ws_title             = EXCLUDED.ws_title,
    ws_slug              = EXCLUDED.ws_slug,
    ws_summary           = EXCLUDED.ws_summary,
    ws_operating_context = EXCLUDED.ws_operating_context,
    ws_settings          = EXCLUDED.ws_settings,
    ws_repo_refs         = EXCLUDED.ws_repo_refs,
    ws_ticket_prefix     = EXCLUDED.ws_ticket_prefix,
    ws_avatar_uri        = EXCLUDED.ws_avatar_uri,
    updated_at           = now()`,
		ws.ID, ws.Name, ws.Slug, ws.Description, ws.Context, settings, repos, ws.IssuePrefix, ws.AvatarURL)
	if err != nil {
		return fmt.Errorf("importer: spaces %s: %w", ws.ID, err)
	}
	return nil
}

// bumpTicketSeq поднимает spaces.ws_next_ticket_seq до maxSeq, если он ниже —
// чтобы следующий тикет, создаваемый уже в server2 после переноса, не
// столкнулся по (workspace_id, tk_seq_number) с перенесёнными тикетами.
func (w *writer) bumpTicketSeq(ctx context.Context, workspaceID string, maxSeq int) error {
	_, err := w.tx.Exec(ctx, `
UPDATE spaces SET ws_next_ticket_seq = GREATEST(ws_next_ticket_seq, $2)
WHERE id = $1`, workspaceID, maxSeq)
	if err != nil {
		return fmt.Errorf("importer: bumpTicketSeq %s: %w", workspaceID, err)
	}
	return nil
}

func (w *writer) upsertSpaceMember(ctx context.Context, workspaceID string, m sourceMember) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO space_members (id, workspace_id, account_id, sm_role, sm_perimeter_access, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    sm_role             = EXCLUDED.sm_role,
    sm_perimeter_access = EXCLUDED.sm_perimeter_access`,
		m.ID, workspaceID, m.UserID, m.Role, m.PerimeterAccess, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: space_members %s: %w", m.ID, err)
	}
	return nil
}

func (w *writer) upsertRuntimeProfile(ctx context.Context, p sourceRuntimeProfile) error {
	fixedArgs, err := jsonbArray(p.FixedArgs)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO agent_protocols (id, workspace_id, proto_display_title, proto_family, proto_command, proto_summary, proto_fixed_args, proto_visibility, proto_created_by, proto_enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
    proto_display_title = EXCLUDED.proto_display_title,
    proto_family         = EXCLUDED.proto_family,
    proto_command         = EXCLUDED.proto_command,
    proto_summary         = EXCLUDED.proto_summary,
    proto_fixed_args      = EXCLUDED.proto_fixed_args,
    proto_visibility      = EXCLUDED.proto_visibility,
    proto_enabled         = EXCLUDED.proto_enabled,
    updated_at            = now()`,
		p.ID, p.WorkspaceID, p.DisplayName, p.ProtocolFamily, p.CommandName, p.Description, fixedArgs, p.Visibility, p.CreatedBy, p.Enabled)
	if err != nil {
		return fmt.Errorf("importer: agent_protocols %s: %w", p.ID, err)
	}
	return nil
}

// --- executors / operatives / skills (003) ----------------------------------

// upsertExecutor переносит Runtime как строку executors по тому же id, чтобы
// operatives.executor_id (NOT NULL) не нарушал внешний ключ. Данные о живом
// состоянии (ex_status/ex_last_seen_at) на новом деплое не значимы (это не
// история, а факт подключения демона — см. docs/51-data-model.md, п.4
// «Порядок вызовов»); импортёр переносит их как offline-заготовку и
// документирует это в decisions.md — реальный executor появится заново, как
// только соответствующий демон подключится к server2.
func (w *writer) upsertExecutor(ctx context.Context, r sourceRuntime) error {
	metadata, err := jsonb(r.Metadata)
	if err != nil {
		return err
	}
	name := r.CustomName
	title := r.Name
	_, err = w.tx.Exec(ctx, `
INSERT INTO executors (id, workspace_id, ex_daemon_id, ex_title, ex_custom_title, ex_mode, ex_provider, ex_launch_header, ex_status, ex_device_info, ex_metadata, ex_owner_account_id, ex_visibility, ex_last_seen_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'offline', $9, $10::jsonb, $11, $12, $13)
ON CONFLICT (id) DO UPDATE SET
    ex_daemon_id     = EXCLUDED.ex_daemon_id,
    ex_title         = EXCLUDED.ex_title,
    ex_custom_title  = EXCLUDED.ex_custom_title,
    ex_mode          = EXCLUDED.ex_mode,
    ex_provider      = EXCLUDED.ex_provider,
    ex_launch_header = EXCLUDED.ex_launch_header,
    ex_device_info   = EXCLUDED.ex_device_info,
    ex_metadata      = EXCLUDED.ex_metadata,
    ex_owner_account_id = EXCLUDED.ex_owner_account_id,
    ex_visibility    = EXCLUDED.ex_visibility,
    ex_last_seen_at  = EXCLUDED.ex_last_seen_at,
    updated_at       = now()`,
		r.ID, r.WorkspaceID, r.DaemonID, title, name, r.RuntimeMode, r.Provider, r.LaunchHeader, r.DeviceInfo, metadata, r.OwnerID, mapVisibility(r.Visibility), r.LastSeenAt)
	if err != nil {
		return fmt.Errorf("importer: executors %s: %w", r.ID, err)
	}
	return nil
}

// ensureExecutorPlaceholder вставляет заглушку executors, если агент
// ссылается на runtime_id, отсутствующий в listRuntimes источника (например
// runtime был удалён после того, как агент его унаследовал) — без этого
// operatives.executor_id (NOT NULL FK) не даст перенести такого агента.
func (w *writer) ensureExecutorPlaceholder(ctx context.Context, id, workspaceID string) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO executors (id, workspace_id, ex_title, ex_mode, ex_provider, ex_status, ex_device_info, ex_visibility)
VALUES ($1, $2, 'imported-placeholder', 'local', 'unknown', 'offline', 'создано importer: исходный runtime не найден в listRuntimes', 'private')
ON CONFLICT (id) DO NOTHING`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("importer: ensureExecutorPlaceholder %s: %w", id, err)
	}
	return nil
}

// mapVisibility: Runtime.visibility (private/public) -> executors.ex_visibility
// CHECK (private/public) — совпадают один в один, функция оставлена как явная
// точка на случай расхождения словарей в будущих версиях контракта.
func mapVisibility(v string) string { return v }

func (w *writer) upsertCapability(ctx context.Context, s sourceSkillWithFiles) error {
	config, err := jsonb(s.Config)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO capabilities (id, workspace_id, cap_title, cap_summary, cap_config, cap_body_md, cap_created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    cap_title      = EXCLUDED.cap_title,
    cap_summary    = EXCLUDED.cap_summary,
    cap_config     = EXCLUDED.cap_config,
    cap_body_md    = EXCLUDED.cap_body_md,
    updated_at     = EXCLUDED.updated_at`,
		s.ID, s.WorkspaceID, s.Name, s.Description, config, s.Content, s.CreatedBy, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: capabilities %s: %w", s.ID, err)
	}
	return nil
}

func (w *writer) upsertCapabilityFile(ctx context.Context, f sourceSkillFile) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO capability_files (id, capability_id, capf_path, capf_body, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    capf_path  = EXCLUDED.capf_path,
    capf_body  = EXCLUDED.capf_body,
    updated_at = EXCLUDED.updated_at`,
		f.ID, f.SkillID, f.Path, f.Content, f.CreatedAt, f.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: capability_files %s: %w", f.ID, err)
	}
	return nil
}

// upsertOperative переносит Agent. Секретные/зашифрованные поля
// (op_runtime_config_sealed, op_mcp_config_sealed, op_custom_env_sealed)
// намеренно не заполняются — контракт либо не отдаёт их значение вовсе
// (custom_env), либо маскирует секретную часть (runtime_config.gateway.token,
// mcp_config при mcp_config_redacted=true), а печать их обратно потребовала
// бы ключа шифрования server2, которым импортёр не располагает (он не часть
// HTTP-контракта). См. decisions.md.
func (w *writer) upsertOperative(ctx context.Context, executorID string, a sourceAgent) error {
	customArgs, err := jsonbArray(a.CustomArgs)
	if err != nil {
		return err
	}
	var composioAllowlist *string
	if !a.ComposioRedacted && a.ComposioAllowlist != nil {
		s, err := jsonbArray(a.ComposioAllowlist)
		if err != nil {
			return err
		}
		composioAllowlist = &s
	}
	kind := "user"
	if a.SystemKey != nil && *a.SystemKey != "" {
		kind = "system"
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO operatives (
    id, workspace_id, executor_id, op_title, op_summary, op_instructions, op_avatar_uri,
    op_runtime_mode, op_custom_args, op_permission_mode, op_status, op_max_concurrent_tasks,
    op_model, op_thinking_level, op_service_tier, op_composio_allowlist, op_owner_account_id,
    op_kind, op_system_key, op_archived_at, op_archived_by, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9::jsonb, $10, $11, $12,
    $13, $14, $15, $16::jsonb, $17,
    $18, $19, $20, $21, $22, $23
)
ON CONFLICT (id) DO UPDATE SET
    executor_id            = EXCLUDED.executor_id,
    op_title               = EXCLUDED.op_title,
    op_summary             = EXCLUDED.op_summary,
    op_instructions        = EXCLUDED.op_instructions,
    op_avatar_uri          = EXCLUDED.op_avatar_uri,
    op_runtime_mode        = EXCLUDED.op_runtime_mode,
    op_custom_args         = EXCLUDED.op_custom_args,
    op_permission_mode     = EXCLUDED.op_permission_mode,
    op_status              = EXCLUDED.op_status,
    op_max_concurrent_tasks = EXCLUDED.op_max_concurrent_tasks,
    op_model               = EXCLUDED.op_model,
    op_thinking_level      = EXCLUDED.op_thinking_level,
    op_service_tier        = EXCLUDED.op_service_tier,
    op_composio_allowlist  = EXCLUDED.op_composio_allowlist,
    op_owner_account_id    = EXCLUDED.op_owner_account_id,
    op_kind                = EXCLUDED.op_kind,
    op_system_key          = EXCLUDED.op_system_key,
    op_archived_at         = EXCLUDED.op_archived_at,
    op_archived_by         = EXCLUDED.op_archived_by,
    updated_at             = EXCLUDED.updated_at`,
		a.ID, a.WorkspaceID, executorID, a.Name, a.Description, a.Instructions, a.AvatarURL,
		a.RuntimeMode, customArgs, a.PermissionMode, a.Status, a.MaxConcurrentTasks,
		a.Model, a.ThinkingLevel, a.ServiceTier, composioAllowlist, a.OwnerID,
		kind, a.SystemKey, a.ArchivedAt, a.ArchivedBy, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: operatives %s: %w", a.ID, err)
	}
	return nil
}

func (w *writer) upsertOperativeTarget(ctx context.Context, operativeID string, t sourceAgentInvocationTarget) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO operative_targets (operative_id, opt_target_type, opt_target_id)
SELECT $1, $2, $3
WHERE NOT EXISTS (
    SELECT 1 FROM operative_targets
    WHERE operative_id = $1 AND opt_target_type = $2 AND opt_target_id IS NOT DISTINCT FROM $3
)`, operativeID, t.TargetType, t.TargetID)
	if err != nil {
		return fmt.Errorf("importer: operative_targets %s/%s: %w", operativeID, t.TargetType, err)
	}
	return nil
}

func (w *writer) upsertOperativeCapability(ctx context.Context, operativeID string, s sourceAgentSkillSummary) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO operative_capabilities (operative_id, capability_id, opcap_enabled)
VALUES ($1, $2, $3)
ON CONFLICT (operative_id, capability_id) DO UPDATE SET
    opcap_enabled = EXCLUDED.opcap_enabled,
    updated_at    = now()`,
		operativeID, s.ID, s.Enabled)
	if err != nil {
		return fmt.Errorf("importer: operative_capabilities %s/%s: %w", operativeID, s.ID, err)
	}
	return nil
}

// --- crews (004) -------------------------------------------------------------

func (w *writer) upsertCrew(ctx context.Context, s sourceSquad) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO crews (id, workspace_id, crew_title, crew_summary, crew_instructions, crew_avatar_uri, crew_leader_type, crew_leader_id, crew_creator_account_id, crew_archived_at, crew_archived_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'agent', $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    crew_title        = EXCLUDED.crew_title,
    crew_summary      = EXCLUDED.crew_summary,
    crew_instructions = EXCLUDED.crew_instructions,
    crew_avatar_uri   = EXCLUDED.crew_avatar_uri,
    crew_leader_id    = EXCLUDED.crew_leader_id,
    crew_archived_at  = EXCLUDED.crew_archived_at,
    crew_archived_by  = EXCLUDED.crew_archived_by,
    updated_at        = EXCLUDED.updated_at`,
		s.ID, s.WorkspaceID, s.Name, s.Description, s.Instructions, s.AvatarURL, s.LeaderID, s.CreatorID, s.ArchivedAt, s.ArchivedBy, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: crews %s: %w", s.ID, err)
	}
	return nil
}

func (w *writer) upsertCrewMember(ctx context.Context, crewID string, m sourceSquadMember) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO crew_members (id, crew_id, cm_member_type, cm_member_id, cm_role, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    cm_role = EXCLUDED.cm_role`,
		m.ID, crewID, m.MemberType, m.MemberID, m.Role, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: crew_members %s: %w", m.ID, err)
	}
	return nil
}

// --- tasks (005): initiatives / tags / field_defs / tickets -----------------

func (w *writer) upsertInitiative(ctx context.Context, p sourceProject) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO initiatives (id, workspace_id, init_title, init_summary, init_icon, init_status, init_priority, init_lead_type, init_lead_id, init_start_date, init_due_date, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10,'')::date, NULLIF($11,'')::date, $12, $13)
ON CONFLICT (id) DO UPDATE SET
    init_title      = EXCLUDED.init_title,
    init_summary    = EXCLUDED.init_summary,
    init_icon       = EXCLUDED.init_icon,
    init_status     = EXCLUDED.init_status,
    init_priority   = EXCLUDED.init_priority,
    init_lead_type  = EXCLUDED.init_lead_type,
    init_lead_id    = EXCLUDED.init_lead_id,
    init_start_date = EXCLUDED.init_start_date,
    init_due_date   = EXCLUDED.init_due_date,
    updated_at      = EXCLUDED.updated_at`,
		p.ID, p.WorkspaceID, p.Title, p.Description, p.Icon, p.Status, p.Priority, p.LeadType, p.LeadID, strPtrOrEmpty(p.StartDate), strPtrOrEmpty(p.DueDate), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: initiatives %s: %w", p.ID, err)
	}
	return nil
}

func strPtrOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (w *writer) upsertInitiativeResource(ctx context.Context, workspaceID string, r sourceProjectResource) error {
	ref, err := jsonb(r.ResourceRef)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO initiative_resources (id, initiative_id, workspace_id, ir_resource_type, ir_resource_ref, ir_label, ir_position, ir_created_by, created_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    ir_resource_type = EXCLUDED.ir_resource_type,
    ir_resource_ref  = EXCLUDED.ir_resource_ref,
    ir_label         = EXCLUDED.ir_label,
    ir_position      = EXCLUDED.ir_position`,
		r.ID, r.ProjectID, workspaceID, r.ResourceType, ref, r.Label, r.Position, r.CreatedBy, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: initiative_resources %s: %w", r.ID, err)
	}
	return nil
}

func (w *writer) upsertTag(ctx context.Context, l sourceLabel) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO tags (id, workspace_id, tag_resource_type, tag_label, tag_summary, tag_color, tag_usage_count, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    tag_label       = EXCLUDED.tag_label,
    tag_summary     = EXCLUDED.tag_summary,
    tag_color       = EXCLUDED.tag_color,
    tag_usage_count = EXCLUDED.tag_usage_count,
    updated_at      = now()`,
		l.ID, l.WorkspaceID, l.ResourceType, l.Name, l.Description, l.Color, l.UsageCount, l.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: tags %s: %w", l.ID, err)
	}
	return nil
}

func (w *writer) upsertFieldDef(ctx context.Context, p sourceProperty) error {
	config, err := jsonb(p.Config)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO field_defs (id, workspace_id, fd_title, fd_type, fd_summary, fd_icon, fd_config, fd_position, fd_archived_at, fd_usage_count, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    fd_title       = EXCLUDED.fd_title,
    fd_type        = EXCLUDED.fd_type,
    fd_summary     = EXCLUDED.fd_summary,
    fd_icon        = EXCLUDED.fd_icon,
    fd_config      = EXCLUDED.fd_config,
    fd_position    = EXCLUDED.fd_position,
    fd_archived_at = EXCLUDED.fd_archived_at,
    fd_usage_count = EXCLUDED.fd_usage_count,
    updated_at     = EXCLUDED.updated_at`,
		p.ID, p.WorkspaceID, p.Name, p.Type, p.Description, p.Icon, config, p.Position, p.ArchivedAt, p.UsageCount, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: field_defs %s: %w", p.ID, err)
	}
	return nil
}

// upsertTicket вставляет строку tickets. tk_seq_number/tk_display_key берутся
// из Issue.number/identifier источника напрямую (сохраняем исходную
// нумерацию, а не выдаём новую — см. docs/51-data-model.md про
// ws_next_ticket_seq; bumpTicketSeq потом поднимает счётчик воркспейса, чтобы
// следующий тикет, заведённый уже в server2, не столкнулся по номеру).
// upsertTicket вставляет тикет с tk_parent_ticket_id = NULL независимо от
// источника: tk_parent_ticket_id — настоящий self-referencing FK
// (REFERENCES tickets(id)), а порядок задач из источника не гарантирует, что
// родитель уже вставлен (docs/51-data-model.md прямо предписывает второй
// проход для этого поля). setTicketParent проставляет его отдельным вызовом
// после того, как все тикеты воркспейса существуют.
func (w *writer) upsertTicket(ctx context.Context, i sourceIssue) error {
	metadata, err := jsonb(i.Metadata)
	if err != nil {
		return err
	}
	customFields, err := jsonb(i.Properties)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO tickets (
    id, workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_narrative,
    tk_status, tk_priority, tk_assignee_type, tk_assignee_id, tk_creator_type, tk_creator_id,
    tk_parent_ticket_id, initiative_id, tk_position, tk_stage, tk_start_date, tk_due_date,
    tk_metadata, tk_custom_field_values, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    NULL, $13, $14, $15, NULLIF($16,'')::date, NULLIF($17,'')::date,
    $18::jsonb, $19::jsonb, $20, $21
)
ON CONFLICT (id) DO UPDATE SET
    tk_headline          = EXCLUDED.tk_headline,
    tk_narrative         = EXCLUDED.tk_narrative,
    tk_status            = EXCLUDED.tk_status,
    tk_priority          = EXCLUDED.tk_priority,
    tk_assignee_type     = EXCLUDED.tk_assignee_type,
    tk_assignee_id       = EXCLUDED.tk_assignee_id,
    initiative_id        = EXCLUDED.initiative_id,
    tk_position          = EXCLUDED.tk_position,
    tk_stage             = EXCLUDED.tk_stage,
    tk_start_date        = EXCLUDED.tk_start_date,
    tk_due_date          = EXCLUDED.tk_due_date,
    tk_metadata          = EXCLUDED.tk_metadata,
    tk_custom_field_values = EXCLUDED.tk_custom_field_values,
    updated_at           = EXCLUDED.updated_at`,
		i.ID, i.WorkspaceID, i.Number, i.Identifier, i.Title, i.Description,
		i.Status, i.Priority, i.AssigneeType, i.AssigneeID, i.CreatorType, i.CreatorID,
		i.ProjectID, i.Position, i.Stage, strPtrOrEmpty(i.StartDate), strPtrOrEmpty(i.DueDate),
		metadata, customFields, i.CreatedAt, i.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: tickets %s (%s): %w", i.ID, i.Identifier, err)
	}
	return nil
}

// setTicketParent — второй проход простановки tk_parent_ticket_id (см.
// upsertTicket).
func (w *writer) setTicketParent(ctx context.Context, ticketID, parentID string) error {
	_, err := w.tx.Exec(ctx, `UPDATE tickets SET tk_parent_ticket_id = $2 WHERE id = $1`, ticketID, parentID)
	if err != nil {
		return fmt.Errorf("importer: setTicketParent %s -> %s: %w", ticketID, parentID, err)
	}
	return nil
}

func (w *writer) linkTicketTag(ctx context.Context, ticketID, tagID string) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO ticket_tag_links (ticket_id, tag_id) VALUES ($1, $2)
ON CONFLICT (ticket_id, tag_id) DO NOTHING`, ticketID, tagID)
	if err != nil {
		return fmt.Errorf("importer: ticket_tag_links %s/%s: %w", ticketID, tagID, err)
	}
	return nil
}

func (w *writer) upsertTicketMark(ctx context.Context, ticketID string, r sourceIssueReaction) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO ticket_marks (id, ticket_id, tm_actor_type, tm_actor_id, tm_emoji, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (ticket_id, tm_actor_type, tm_actor_id, tm_emoji) DO NOTHING`,
		r.ID, ticketID, r.ActorType, r.ActorID, r.Emoji, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: ticket_marks %s: %w", r.ID, err)
	}
	return nil
}

// upsertTicketNote вставляет комментарий с tn_parent_note_id = NULL: это
// тоже self-referencing FK (тред), и родительский комментарий треда не
// обязательно уже вставлен (комментарии одной задачи собираются одним плоским
// списком без гарантии порядка root-перед-ответом). setTicketNoteParent
// проставляет связь вторым проходом, как и для tickets.
func (w *writer) upsertTicketNote(ctx context.Context, c sourceIssueComment) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO ticket_notes (id, ticket_id, tn_author_type, tn_author_id, tn_body, tn_kind, tn_parent_note_id, tn_resolved_at, tn_resolved_by_type, tn_resolved_by_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE SET
    tn_body             = EXCLUDED.tn_body,
    tn_kind             = EXCLUDED.tn_kind,
    tn_resolved_at      = EXCLUDED.tn_resolved_at,
    tn_resolved_by_type = EXCLUDED.tn_resolved_by_type,
    tn_resolved_by_id   = EXCLUDED.tn_resolved_by_id,
    updated_at          = EXCLUDED.updated_at`,
		c.ID, c.IssueID, c.AuthorType, c.AuthorID, c.Content, c.Type, c.ResolvedAt, c.ResolvedByType, c.ResolvedByID, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: ticket_notes %s: %w", c.ID, err)
	}
	return nil
}

func (w *writer) setTicketNoteParent(ctx context.Context, noteID, parentID string) error {
	_, err := w.tx.Exec(ctx, `UPDATE ticket_notes SET tn_parent_note_id = $2 WHERE id = $1`, noteID, parentID)
	if err != nil {
		return fmt.Errorf("importer: setTicketNoteParent %s -> %s: %w", noteID, parentID, err)
	}
	return nil
}

func (w *writer) upsertNoteMark(ctx context.Context, noteID string, r sourceCommentReaction) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO note_marks (id, ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji) DO NOTHING`,
		r.ID, noteID, r.ActorType, r.ActorID, r.Emoji, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: note_marks %s: %w", r.ID, err)
	}
	return nil
}

func (w *writer) upsertTicketSubscriber(ctx context.Context, s sourceIssueSubscriber) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO ticket_subscribers (ticket_id, tsub_watcher_type, tsub_watcher_id, tsub_reason, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (ticket_id, tsub_watcher_type, tsub_watcher_id) DO UPDATE SET
    tsub_reason = EXCLUDED.tsub_reason`,
		s.IssueID, s.UserType, s.UserID, s.Reason, s.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: ticket_subscribers %s/%s: %w", s.IssueID, s.UserID, err)
	}
	return nil
}

func (w *writer) upsertTicketPRLink(ctx context.Context, l sourceIssuePullRequestLink) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO ticket_pr_links (ticket_id, tpr_provider, tpr_url, tpr_number, tpr_title, tpr_state, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (ticket_id, tpr_url) DO UPDATE SET
    tpr_number = EXCLUDED.tpr_number,
    tpr_title  = EXCLUDED.tpr_title,
    tpr_state  = EXCLUDED.tpr_state`,
		l.IssueID, l.Provider, l.URL, l.Number, l.Title, l.State, l.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: ticket_pr_links %s/%s: %w", l.IssueID, l.URL, err)
	}
	return nil
}

// --- sentinels (006) ----------------------------------------------------------

func (w *writer) upsertSentinel(ctx context.Context, a sourceAutopilot) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO sentinels (id, workspace_id, sen_title, sen_summary, initiative_id, sen_assignee_type, sen_assignee_id, sen_status, sen_execution_mode, sen_issue_title_template, sen_created_by_type, sen_created_by_id, sen_last_run_at, sen_is_template, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (id) DO UPDATE SET
    sen_title               = EXCLUDED.sen_title,
    sen_summary             = EXCLUDED.sen_summary,
    initiative_id           = EXCLUDED.initiative_id,
    sen_assignee_type       = EXCLUDED.sen_assignee_type,
    sen_assignee_id         = EXCLUDED.sen_assignee_id,
    sen_status              = EXCLUDED.sen_status,
    sen_execution_mode      = EXCLUDED.sen_execution_mode,
    sen_issue_title_template = EXCLUDED.sen_issue_title_template,
    sen_last_run_at         = EXCLUDED.sen_last_run_at,
    sen_is_template         = EXCLUDED.sen_is_template,
    updated_at              = EXCLUDED.updated_at`,
		a.ID, a.WorkspaceID, a.Title, a.Description, a.ProjectID, a.AssigneeType, a.AssigneeID, a.Status, a.ExecutionMode, a.IssueTitleTemplate, a.CreatedByType, a.CreatedByID, a.LastRunAt, a.IsTemplate, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: sentinels %s: %w", a.ID, err)
	}
	return nil
}

// upsertSentinelTrigger переносит триггер БЕЗ секретов: webhook_token
// приходит от контракта уже хэшированным заранее? Нет — контракт вообще не
// отдаёт значение токена/подписи на чтение (см. docs/51-data-model.md,
// таблица «Что не переносится»), поэтому strig_webhook_token_digest и
// strig_signing_secret_sealed остаются NULL — секрет придётся выпустить
// заново через rotate-webhook-token/signing-secret после переноса.
// strig_webhook_path тоже не переносится, чтобы не столкнуться с уникальным
// индексом sentinel_triggers_webhook_path_uk на пути, всё ещё занятом старым
// сервером.
func (w *writer) upsertSentinelTrigger(ctx context.Context, t sourceAutopilotTrigger) error {
	eventFilters, err := jsonbArray(t.EventFilters)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO sentinel_triggers (id, sentinel_id, strig_kind, strig_enabled, strig_cron_expression, strig_timezone, strig_next_run_at, strig_provider, strig_label, strig_last_fired_at, strig_event_filters, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13)
ON CONFLICT (id) DO UPDATE SET
    strig_enabled        = EXCLUDED.strig_enabled,
    strig_cron_expression = EXCLUDED.strig_cron_expression,
    strig_timezone        = EXCLUDED.strig_timezone,
    strig_next_run_at     = EXCLUDED.strig_next_run_at,
    strig_provider        = EXCLUDED.strig_provider,
    strig_label           = EXCLUDED.strig_label,
    strig_last_fired_at   = EXCLUDED.strig_last_fired_at,
    strig_event_filters   = EXCLUDED.strig_event_filters,
    updated_at            = EXCLUDED.updated_at`,
		t.ID, t.AutopilotID, t.Kind, t.Enabled, t.CronExpression, t.Timezone, t.NextRunAt, t.Provider, t.Label, t.LastFiredAt, eventFilters, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: sentinel_triggers %s: %w", t.ID, err)
	}
	return nil
}

// --- chat (007) ----------------------------------------------------------------

func (w *writer) upsertConvo(ctx context.Context, s sourceChatSession) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO convos (id, workspace_id, operative_id, cv_creator_account_id, initiative_id, cv_title, cv_status, cv_pinned, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
    cv_title   = EXCLUDED.cv_title,
    cv_status  = EXCLUDED.cv_status,
    cv_pinned  = EXCLUDED.cv_pinned,
    updated_at = EXCLUDED.updated_at`,
		s.ID, s.WorkspaceID, s.AgentID, s.CreatorID, s.ProjectID, s.Title, s.Status, s.Pinned, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: convos %s: %w", s.ID, err)
	}
	return nil
}

func (w *writer) upsertConvoMessage(ctx context.Context, convoID string, m sourceChatMessage) error {
	kind := m.MessageKind
	if kind == "" {
		kind = "message"
	}
	_, err := w.tx.Exec(ctx, `
INSERT INTO convo_messages (id, convo_id, cvm_role, cvm_body, dispatch_job_id, cvm_failure_reason, cvm_elapsed_ms, cvm_kind, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    cvm_body           = EXCLUDED.cvm_body,
    cvm_failure_reason = EXCLUDED.cvm_failure_reason,
    cvm_elapsed_ms     = EXCLUDED.cvm_elapsed_ms,
    cvm_kind           = EXCLUDED.cvm_kind`,
		m.ID, convoID, m.Role, m.Content, m.TaskID, m.FailureReason, m.ElapsedMs, kind, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("importer: convo_messages %s: %w", m.ID, err)
	}
	return nil
}

// --- integrations (010): space_mcp_servers / space_config ----------------------

// upsertWorkspaceMcpServer переносит регистрацию сервера БЕЗ секретов
// (wmcp_config_sealed остаётся NULL — контракт отдаёт только
// provided_credentials/missing_credentials, не значения; см.
// docs/51-data-model.md). credential_schema (список ключей, не значений)
// переносится — это не секрет, а описание формы.
func (w *writer) upsertWorkspaceMcpServer(ctx context.Context, workspaceID string, m sourceWorkspaceMcpServer) error {
	schema, err := jsonbArray(m.CredentialSchema)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(ctx, `
INSERT INTO space_mcp_servers (id, workspace_id, wmcp_name, wmcp_transport, wmcp_source, wmcp_credential_schema, wmcp_enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, true, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    wmcp_name              = EXCLUDED.wmcp_name,
    wmcp_transport         = EXCLUDED.wmcp_transport,
    wmcp_credential_schema = EXCLUDED.wmcp_credential_schema,
    updated_at             = EXCLUDED.updated_at`,
		m.ID, workspaceID, m.Name, m.Transport, m.Source, schema, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: space_mcp_servers %s: %w", m.ID, err)
	}
	return nil
}

// upsertSpaceConfig переносит нечувствительную часть конфигурации воркспейса
// (cfg_llm_api_key_sealed остаётся NULL — see decisions.md).
func (w *writer) upsertSpaceConfig(ctx context.Context, workspaceID string, c sourceWorkspaceConfigLayer) error {
	_, err := w.tx.Exec(ctx, `
INSERT INTO space_config (workspace_id, cfg_llm_base_url, cfg_llm_model, cfg_updated_by, updated_at)
VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4, COALESCE($5, now()))
ON CONFLICT (workspace_id) DO UPDATE SET
    cfg_llm_base_url = EXCLUDED.cfg_llm_base_url,
    cfg_llm_model    = EXCLUDED.cfg_llm_model,
    cfg_updated_by   = EXCLUDED.cfg_updated_by,
    updated_at       = EXCLUDED.updated_at`,
		workspaceID, c.LLMBaseURL, c.LLMModel, c.UpdatedBy, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("importer: space_config %s: %w", workspaceID, err)
	}
	return nil
}
