// agenttask.go — сборка AgentTask (contract §3.7, схема в
// docs/50-api-contract.yaml, components.schemas.AgentTask) из dispatch_jobs
// и присоединённых таблиц. Контракт прямо перечисляет, какие поля
// референсный daemon-клиент реально разбирает — им уделено больше внимания,
// чем полям, помеченным "демон это поле не читает" (см. коммент схемы),
// которые всё равно присутствуют для остальных потребителей того же ответа.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

type agentTaskRow struct {
	ID, WorkspaceID, OperativeID, ExecutorID string
	TicketID, ConvoID, CrewID, SentinelRunID *string
	InitiativeID                             *string
	Kind, Status                             string
	Priority                                 int
	ThreadTitle                              *string
	DispatchedAt, StartedAt, CompletedAt     *time.Time
	Result                                   json.RawMessage
	Error, FailureReason                     *string
	Attempt, MaxAttempts                     int
	ParentJobID                              *string
	IsLeader                                 bool
	PriorSessionID, PriorWorkDir             *string
	ResumeUnavailable                        bool
	WorkDir                                  *string
	TriggerNoteID                            *string
	CoalescedNoteIDs, DeliveredNoteIDs       []string
	TriggerThreadID                          *string
	TriggerSummary, TriggerAuthorType        *string
	ChatChannelType                          *string
	ChatInThread, ChatIntro                  bool
	ChatMessage                              *string
	QuickCreatePrompt, QuickCreatePriority   *string
	QuickCreateDueDate                       *time.Time
	HandoffNote                              *string
	McpPolicy                                json.RawMessage
	InitiatorType                            *string
	InitiatorID                              *string
	Attribution, ConnectedApps, RepoRefs     json.RawMessage
	ContextSnapshot                          json.RawMessage
	CreatedAt                                time.Time

	WorkspaceContext *string

	OpTitle         string
	OpInstructions  string
	OpCustomArgs    json.RawMessage
	OpModel         *string
	OpThinkingLevel *string
	OpServiceTier   *string

	ParentTicketID   *string
	TicketHeadline   *string
	ParentDisplayKey *string
	TicketInitiative *string

	InitTitle, InitSummary *string
	CrewTitle              *string

	SentinelID           *string
	SenTitle, SenSummary *string
	SrunSource           *string
	SrunTriggerPayload   json.RawMessage
}

const agentTaskSQL = `
SELECT dj.id, dj.workspace_id, dj.operative_id, dj.executor_id,
	dj.ticket_id, dj.convo_id, dj.crew_id, dj.sentinel_run_id, dj.initiative_id,
	dj.dj_kind, dj.dj_status, dj.dj_priority, dj.dj_thread_title,
	dj.dj_dispatched_at, dj.dj_started_at, dj.dj_completed_at,
	dj.dj_result, dj.dj_error, dj.dj_failure_reason, dj.dj_attempt, dj.dj_max_attempts, dj.dj_parent_job_id,
	dj.dj_is_leader, dj.dj_prior_session_id, dj.dj_prior_work_dir, dj.dj_resume_unavailable, dj.dj_work_dir,
	dj.dj_trigger_note_id, dj.dj_coalesced_note_ids, dj.dj_delivered_note_ids, dj.dj_trigger_thread_id,
	dj.dj_trigger_summary, dj.dj_trigger_author_type,
	dj.dj_chat_channel_type, dj.dj_chat_in_thread, dj.dj_chat_intro, dj.dj_chat_message,
	dj.dj_quick_create_prompt, dj.dj_quick_create_priority, dj.dj_quick_create_due_date,
	dj.dj_handoff_note, dj.dj_mcp_policy, dj.dj_initiator_type, dj.dj_initiator_id,
	dj.dj_attribution, dj.dj_connected_apps, dj.dj_repo_refs, dj.dj_context_snapshot, dj.created_at,
	ws.ws_operating_context,
	op.op_title, op.op_instructions, op.op_custom_args, op.op_model, op.op_thinking_level, op.op_service_tier,
	tk.tk_parent_ticket_id, tk.tk_headline, parent_tk.tk_display_key, tk.initiative_id,
	ini.init_title, ini.init_summary,
	cr.crew_title,
	srun.sentinel_id, sen.sen_title, sen.sen_summary, srun.srun_source, srun.srun_trigger_payload
FROM dispatch_jobs dj
JOIN operatives op ON op.id = dj.operative_id
LEFT JOIN spaces ws ON ws.id = dj.workspace_id
LEFT JOIN tickets tk ON tk.id = dj.ticket_id
LEFT JOIN tickets parent_tk ON parent_tk.id = tk.tk_parent_ticket_id
LEFT JOIN initiatives ini ON ini.id = COALESCE(dj.initiative_id, tk.initiative_id)
LEFT JOIN crews cr ON cr.id = dj.crew_id
LEFT JOIN sentinel_runs srun ON srun.id = dj.sentinel_run_id
LEFT JOIN sentinels sen ON sen.id = srun.sentinel_id
WHERE dj.id = $1`

func (d *Deps) scanAgentTaskRow(ctx context.Context, jobID string) (agentTaskRow, bool, error) {
	var r agentTaskRow
	err := d.DB.Pool.QueryRow(ctx, agentTaskSQL, jobID).Scan(
		&r.ID, &r.WorkspaceID, &r.OperativeID, &r.ExecutorID,
		&r.TicketID, &r.ConvoID, &r.CrewID, &r.SentinelRunID, &r.InitiativeID,
		&r.Kind, &r.Status, &r.Priority, &r.ThreadTitle,
		&r.DispatchedAt, &r.StartedAt, &r.CompletedAt,
		&r.Result, &r.Error, &r.FailureReason, &r.Attempt, &r.MaxAttempts, &r.ParentJobID,
		&r.IsLeader, &r.PriorSessionID, &r.PriorWorkDir, &r.ResumeUnavailable, &r.WorkDir,
		&r.TriggerNoteID, &r.CoalescedNoteIDs, &r.DeliveredNoteIDs, &r.TriggerThreadID,
		&r.TriggerSummary, &r.TriggerAuthorType,
		&r.ChatChannelType, &r.ChatInThread, &r.ChatIntro, &r.ChatMessage,
		&r.QuickCreatePrompt, &r.QuickCreatePriority, &r.QuickCreateDueDate,
		&r.HandoffNote, &r.McpPolicy, &r.InitiatorType, &r.InitiatorID,
		&r.Attribution, &r.ConnectedApps, &r.RepoRefs, &r.ContextSnapshot, &r.CreatedAt,
		&r.WorkspaceContext,
		&r.OpTitle, &r.OpInstructions, &r.OpCustomArgs, &r.OpModel, &r.OpThinkingLevel, &r.OpServiceTier,
		&r.ParentTicketID, &r.TicketHeadline, &r.ParentDisplayKey, &r.TicketInitiative,
		&r.InitTitle, &r.InitSummary,
		&r.CrewTitle,
		&r.SentinelID, &r.SenTitle, &r.SenSummary, &r.SrunSource, &r.SrunTriggerPayload,
	)
	if store.IsNoRows(err) {
		return agentTaskRow{}, false, nil
	}
	if err != nil {
		return agentTaskRow{}, false, fmt.Errorf("daemon: сборка AgentTask: %w", err)
	}
	return r, true, nil
}

// actorName резолвит отображаемое имя/почту участника/агента по (type, id) —
// используется для requesting_user_name, initiator_name/email,
// trigger_author_name, авторов coalesced-комментариев.
func (d *Deps) actorName(ctx context.Context, actorType *string, actorID *string) (name, email string) {
	if actorType == nil || actorID == nil || *actorID == "" {
		return "", ""
	}
	switch *actorType {
	case "member":
		_ = d.DB.Pool.QueryRow(ctx, `SELECT acct_full_name, acct_email FROM accounts WHERE id = $1`, *actorID).Scan(&name, &email)
	case "agent":
		_ = d.DB.Pool.QueryRow(ctx, `SELECT op_title FROM operatives WHERE id = $1`, *actorID).Scan(&name)
	}
	return name, email
}

// buildAgentTask собирает полный ответ AgentTask для claim/pending-листинга.
// authToken непусто только в момент claim (contract: "present only in claim
// responses").
func (d *Deps) buildAgentTask(ctx context.Context, jobID, authToken string) (map[string]any, bool, error) {
	r, found, err := d.scanAgentTaskRow(ctx, jobID)
	if err != nil || !found {
		return nil, found, err
	}

	out := map[string]any{
		"id": r.ID, "agent_id": r.OperativeID, "runtime_id": r.ExecutorID,
		"issue_id": strOr(r.TicketID, ""), "workspace_id": r.WorkspaceID,
		"status": r.Status, "priority": r.Priority, "attempt": r.Attempt, "max_attempts": r.MaxAttempts,
		"created_at": r.CreatedAt, "delivered_comment_ids": orEmptyIDs(r.DeliveredNoteIDs), "kind": r.Kind,
		"dispatched_at": r.DispatchedAt, "started_at": r.StartedAt, "completed_at": r.CompletedAt,
		"result": rawOrNil(r.Result), "error": r.Error, "failure_reason": strOr(r.FailureReason, ""),
		"parent_task_id": r.ParentJobID, "is_leader_task": r.IsLeader,
		"work_dir": strOr(r.WorkDir, ""), "prior_session_resume_unavailable": r.ResumeUnavailable,
		"connected_apps": rawOrEmptyArray(r.ConnectedApps), "repos": rawOrEmptyArray(r.RepoRefs),
		"coalesced_comment_ids": orEmptyIDs(r.CoalescedNoteIDs),
		"chat_in_thread":        r.ChatInThread, "chat_intro": r.ChatIntro,
		"handoff_note": strOr(r.HandoffNote, ""),
		"attribution":  attributionOrDefault(r.Attribution, r.InitiatorType),
	}
	if hasJSONValue(r.McpPolicy) {
		// TaskMcpPolicy контракта не допускает null (в отличие от "result") —
		// ключ включается, только когда dj_mcp_policy реально задан. pgx
		// отдаёт SQL NULL jsonb-колонки в json.RawMessage как литерал
		// `null` (4 байта), а не пустой срез — простой len()>0 этого не ловит.
		out["mcp_policy"] = r.McpPolicy
	}
	if r.ThreadTitle != nil {
		out["thread_name"] = *r.ThreadTitle
	}
	if r.WorkspaceContext != nil {
		out["workspace_context"] = *r.WorkspaceContext
	}
	if r.PriorSessionID != nil {
		out["prior_session_id"] = *r.PriorSessionID
	}
	if r.PriorWorkDir != nil {
		out["prior_work_dir"] = *r.PriorWorkDir
	}
	if r.TriggerNoteID != nil {
		out["trigger_comment_id"] = *r.TriggerNoteID
		content, authorType, authorID, err := d.noteContent(ctx, *r.TriggerNoteID)
		if err == nil {
			out["trigger_comment_content"] = content
			name, _ := d.actorName(ctx, authorType, authorID)
			out["trigger_author_name"] = name
		}
	}
	if r.TriggerThreadID != nil {
		out["trigger_thread_id"] = *r.TriggerThreadID
	}
	if r.TriggerSummary != nil {
		out["trigger_summary"] = *r.TriggerSummary
	}
	if r.TriggerAuthorType != nil {
		out["trigger_author_type"] = *r.TriggerAuthorType
	}
	if len(r.CoalescedNoteIDs) > 0 {
		out["coalesced_comments"] = d.coalescedComments(ctx, r.CoalescedNoteIDs)
	}
	if r.ConvoID != nil {
		out["chat_session_id"] = *r.ConvoID
	}
	if r.ChatChannelType != nil {
		out["chat_channel_type"] = *r.ChatChannelType
	}
	if r.ChatMessage != nil {
		out["chat_message"] = *r.ChatMessage
	}
	if r.SentinelRunID != nil {
		out["autopilot_run_id"] = *r.SentinelRunID
	}
	if r.SentinelID != nil {
		out["autopilot_id"] = *r.SentinelID
	}
	if r.SenTitle != nil {
		out["autopilot_title"] = *r.SenTitle
	}
	if r.SenSummary != nil {
		out["autopilot_description"] = *r.SenSummary
	}
	if r.SrunSource != nil {
		out["autopilot_source"] = *r.SrunSource
	}
	if len(r.SrunTriggerPayload) > 0 {
		out["autopilot_trigger_payload"] = r.SrunTriggerPayload
	}
	if r.QuickCreatePrompt != nil {
		out["quick_create_prompt"] = *r.QuickCreatePrompt
	}
	if r.QuickCreatePriority != nil {
		out["quick_create_priority"] = *r.QuickCreatePriority
	}
	if r.QuickCreateDueDate != nil {
		out["quick_create_due_date"] = r.QuickCreateDueDate.Format("2006-01-02")
	}
	if r.CrewID != nil {
		out["squad_id"] = *r.CrewID
	}
	if r.CrewTitle != nil {
		out["squad_name"] = *r.CrewTitle
	}
	if r.ParentTicketID != nil {
		out["parent_issue_id"] = *r.ParentTicketID
	}
	if r.ParentDisplayKey != nil {
		out["parent_issue_identifier"] = *r.ParentDisplayKey
	}
	if r.TicketInitiative != nil {
		out["project_id"] = *r.TicketInitiative
	}
	if r.InitTitle != nil {
		out["project_title"] = *r.InitTitle
	}
	if r.InitSummary != nil {
		out["project_description"] = *r.InitSummary
	}
	if res, err := d.projectResources(ctx, r.TicketInitiative); err == nil && len(res) > 0 {
		out["project_resources"] = res
	}
	if r.InitiatorType != nil {
		out["initiator_type"] = *r.InitiatorType
		out["initiator_id"] = strOr(r.InitiatorID, "")
		name, email := d.actorName(ctx, r.InitiatorType, r.InitiatorID)
		out["initiator_name"] = name
		if email != "" {
			out["initiator_email"] = email
		}
		out["requesting_user_name"] = name
	}

	agent := map[string]any{
		"id": r.OperativeID, "name": r.OpTitle, "instructions": r.OpInstructions,
		"custom_args": rawOrEmptyArray(r.OpCustomArgs),
	}
	if r.OpModel != nil {
		agent["model"] = *r.OpModel
	}
	if r.OpThinkingLevel != nil {
		agent["thinking_level"] = *r.OpThinkingLevel
	}
	if r.OpServiceTier != nil {
		agent["service_tier"] = *r.OpServiceTier
	}
	skills, err := d.agentSkills(ctx, r.OperativeID)
	if err == nil && len(skills) > 0 {
		agent["skills"] = skills
	}
	disabled, err := d.disabledRuntimeSkills(ctx, r.OperativeID, r.ExecutorID)
	if err == nil && len(disabled) > 0 {
		agent["disabled_runtime_skills"] = disabled
	}
	out["agent"] = agent

	if authToken != "" {
		out["auth_token"] = authToken
	}
	return out, true, nil
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func orEmptyIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func rawOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func rawOrEmpty(raw json.RawMessage, def string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(def)
	}
	return raw
}

func rawOrEmptyArray(raw json.RawMessage) json.RawMessage { return rawOrEmpty(raw, `[]`) }

// hasJSONValue — raw несёт содержательное значение, а не отсутствие
// (пустой срез) и не JSON-литерал `null` (то, во что pgx превращает SQL
// NULL nullable jsonb-колонки при сканировании в json.RawMessage).
func hasJSONValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && string(trimmed) != "null"
}

// attributionOrDefault — TaskAttribution контракта требует "source"/"precise"
// безусловно; dj_attribution по умолчанию '{}'::jsonb (задачи, поставленные
// доменами, которые ещё не заполняют полную атрибуцию) не проходит эту
// проверку схемы у любого потребителя AgentTask. Решение: если сохранённое
// значение уже несёт "source", отдаём его как есть (полная атрибуция,
// записанная при постановке в очередь); иначе синтезируем минимально
// достаточное значение из dj_initiator_type — тот же сигнал, что и
// initiator_type/initiator_id верхнего уровня AgentTask, только в форме,
// которую требует эта под-схема.
func attributionOrDefault(raw json.RawMessage, initiatorType *string) map[string]any {
	var stored map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &stored)
	}
	if _, hasSource := stored["source"]; hasSource {
		return stored
	}
	source := "system"
	if initiatorType != nil && *initiatorType != "" {
		source = *initiatorType
	}
	return map[string]any{"source": source, "precise": initiatorType != nil}
}
