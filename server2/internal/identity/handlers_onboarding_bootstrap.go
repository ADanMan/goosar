// handlers_onboarding_bootstrap.go — meBootstrapOnboardingWithRuntime/
// meBootstrapOnboardingWithoutRuntime (docs/50-api-contract.yaml,
// POST /api/me/onboarding/{runtime-bootstrap,no-runtime-bootstrap}). T-026
// оставил оба 501 ("требует вложений/агентов/задач, которых ещё нет" —
// server2/docs/decisions.md, раздел T-026, пункт 9); доводка T-029
// реализует их теперь, когда домены agent/task существуют. Контракт сам
// называет обе ручки "Legacy... behavior is frozen" — реализация здесь
// умышленно минимальна, без попытки угадать более богатое поведение,
// которого спецификация не описывает.
package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// isWorkspaceMember — прямой SQL по space_members (тот же приём, что
// dashboard/misc/export уже применяют к чужим таблицам — см.
// server2/docs/decisions.md).
func (d *Deps) isWorkspaceMember(ctx context.Context, workspaceID, accountID string) (bool, error) {
	var exists bool
	err := d.DB.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM space_members WHERE workspace_id = $1 AND account_id = $2)`,
		workspaceID, accountID).Scan(&exists)
	return exists, err
}

// runtimeAccess — существует ли runtime в пространстве и виден ли он
// вызывающему (не приватный чужой) — тот же критерий, что "приватный —
// только владельцу/админу", повторённый в контракте для этой ручки.
func (d *Deps) runtimeAccess(ctx context.Context, workspaceID, runtimeID, accountID string) (found, allowed bool, err error) {
	var visibility string
	var ownerID *string
	err = d.DB.Pool.QueryRow(ctx,
		`SELECT ex_visibility, ex_owner_account_id FROM executors WHERE id = $1 AND workspace_id = $2`,
		runtimeID, workspaceID).Scan(&visibility, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if visibility != "private" {
		return true, true, nil
	}
	return true, ownerID != nil && *ownerID == accountID, nil
}

// onboardingAssistant находит (по op_system_key, уникален на пространство —
// см. 003_agents.up.sql, operatives_system_key_uk) или создаёт "встроенного
// агента-помощника онбординга" на заданном runtime.
func (d *Deps) onboardingAssistant(ctx context.Context, workspaceID, runtimeID, accountID string) (agentID string, err error) {
	err = d.DB.Pool.QueryRow(ctx,
		`SELECT id FROM operatives WHERE workspace_id = $1 AND op_system_key = 'onboarding_assistant'`,
		workspaceID).Scan(&agentID)
	if err == nil {
		return agentID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	err = d.DB.Pool.QueryRow(ctx, `
		INSERT INTO operatives (workspace_id, executor_id, op_title, op_instructions, op_runtime_mode,
			op_kind, op_system_key, op_owner_account_id)
		VALUES ($1, $2, 'Onboarding Assistant',
			'You are the built-in onboarding assistant. Help the user get comfortable with their new workspace.',
			'local', 'system', 'onboarding_assistant', $3)
		ON CONFLICT (workspace_id, op_system_key) WHERE op_system_key IS NOT NULL
		DO UPDATE SET executor_id = EXCLUDED.executor_id
		RETURNING id`,
		workspaceID, runtimeID, accountID).Scan(&agentID)
	if err != nil {
		return "", err
	}
	return agentID, nil
}

// createStarterIssue вставляет одну задачу-приветствие в пространство,
// нумеруя её тем же способом, что internal/task (ws_next_ticket_seq,
// tk_display_key = префикс-номер) — см. docs/51-data-model.md, «Нумерация
// задач», и internal/workspace.Store.IncrementTicketSeq (тот же SQL,
// повторён здесь напрямую: заводить зависимость identity -> workspace ради
// одного UPDATE не стоит, см. решения T-027/T-028 про "каждый домен
// переизобретает свои маленькие помощники").
func (d *Deps) createStarterIssue(ctx context.Context, workspaceID, headline, narrative, accountID string) (issueID string, err error) {
	var seq int
	var prefix string
	err = d.DB.Pool.QueryRow(ctx,
		`UPDATE spaces SET ws_next_ticket_seq = ws_next_ticket_seq + 1 WHERE id = $1
			RETURNING ws_next_ticket_seq, ws_ticket_prefix`, workspaceID).Scan(&seq, &prefix)
	if err != nil {
		return "", err
	}
	displayKey := fmt.Sprintf("%s-%d", prefix, seq)
	// tk_position — contract: Issue.position не nullable (число, не строка/
	// null); та же формула, что internal/task.Store.CreateIssue использует
	// для нового тикета в статусе по умолчанию ('backlog') — см.
	// server2/docs/decisions.md, «T-029 доводка» (без неё задача,
	// заведённая этой (frozen-по контракту) ручкой, ломала бы схему
	// первого же listIssues/queryIssues на неё).
	var position float64
	err = d.DB.Pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(tk_position), 0) + 1024 FROM tickets WHERE workspace_id = $1 AND tk_status = 'backlog'`,
		workspaceID).Scan(&position)
	if err != nil {
		return "", err
	}
	err = d.DB.Pool.QueryRow(ctx, `
		INSERT INTO tickets (workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_narrative,
			tk_creator_type, tk_creator_id, tk_position)
		VALUES ($1, $2, $3, $4, $5, 'member', $6, $7)
		RETURNING id`,
		workspaceID, seq, displayKey, headline, narrative, accountID, position).Scan(&issueID)
	if err != nil {
		return "", err
	}
	return issueID, nil
}

type bootstrapWithRuntimeRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	RuntimeID     string `json:"runtime_id"`
	StarterPrompt string `json:"starter_prompt"`
}

func (d *Deps) handleBootstrapOnboardingWithRuntime(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req bootstrapWithRuntimeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.WorkspaceID == "" || req.RuntimeID == "" {
		httpapi.BadRequest(w, "workspace_id and runtime_id are required")
		return
	}
	isMember, err := d.isWorkspaceMember(r.Context(), req.WorkspaceID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !isMember {
		httpapi.WriteError(w, http.StatusForbidden, "not a member of this workspace", "forbidden")
		return
	}
	found, allowed, err := d.runtimeAccess(r.Context(), req.WorkspaceID, req.RuntimeID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found || !allowed {
		httpapi.WriteError(w, http.StatusForbidden, "runtime not accessible", "forbidden")
		return
	}
	agentID, err := d.onboardingAssistant(r.Context(), req.WorkspaceID, req.RuntimeID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	narrative := req.StarterPrompt
	if narrative == "" {
		narrative = "Say hello to your onboarding assistant and ask it what it can do."
	}
	issueID, err := d.createStarterIssue(r.Context(), req.WorkspaceID,
		"Get started with your onboarding assistant", narrative, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"workspace_id": req.WorkspaceID, "agent_id": agentID, "issue_id": issueID,
	})
}

type bootstrapWithoutRuntimeRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

func (d *Deps) handleBootstrapOnboardingWithoutRuntime(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req bootstrapWithoutRuntimeRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.WorkspaceID == "" {
		httpapi.BadRequest(w, "workspace_id is required")
		return
	}
	isMember, err := d.isWorkspaceMember(r.Context(), req.WorkspaceID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !isMember {
		httpapi.WriteError(w, http.StatusForbidden, "not a member of this workspace", "forbidden")
		return
	}
	issueID, err := d.createStarterIssue(r.Context(), req.WorkspaceID,
		"Connect a runtime to run your first agent",
		"No runtime is connected yet — install the Goosar daemon and register a runtime to get started.",
		actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"workspace_id": req.WorkspaceID, "issue_id": issueID,
	})
}
