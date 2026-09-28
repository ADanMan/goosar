package note

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// TriggerOutcome — components/schemas/CommentTriggerOutcome.
type TriggerOutcome struct {
	TargetType string `json:"target_type"` // agent|squad
	TargetID   string `json:"target_id"`
	Status     string `json:"status"`      // queued|deferred|coalesced|blocked
	ReasonCode string `json:"reason_code"` // queued|deferred|coalesced|already_active|self_trigger_suppressed|invocation_not_allowed|attribution_blocked|internal_error
}

// mentionRe reconoce упоминания в форме @agent:<uuid> / @squad:<uuid> /
// @member:<uuid>.
//
// Пробел спецификации: контракт описывает только *что* упоминание значит
// («агент явно упомянут (@agent) в тексте»), но не конкретный синтаксис
// мнения (имя? id? markdown-ссылка?) — packages/core, где это решалось бы
// на фронте, вне списка разрешённых файлов этой сессии. Решение (см.
// server2/docs/decisions.md): клиент вставляет упоминание как явный токен
// "@agent:<uuid>"/"@squad:<uuid>" — однозначно, без коллизий по display name.
// T-027 доводка расширяет тот же приём на "@member:<uuid>" — для
// InboxItem.type=mentioned (человеческое упоминание), которое resolveTargets
// по-прежнему игнорирует (member не запускает агентов), но notify.go читает.
var mentionRe = regexp.MustCompile(`@(agent|squad|member):([0-9a-fA-F-]{36})`)

type mention struct {
	kind string // agent|squad|member
	id   string
}

func parseMentions(content string) []mention {
	matches := mentionRe.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	var out []mention
	for _, m := range matches {
		key := m[1] + ":" + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, mention{kind: m[1], id: m[2]})
	}
	return out
}

// isNoteComment — комментарий, начинающийся с "/note" (без учёта регистра,
// с любыми ведущими пробелами), никогда не запускает агентов (контракт §1.10).
func isNoteComment(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	return len(trimmed) >= 5 && strings.EqualFold(trimmed[:5], "/note")
}

// candidateTarget — цель до проверки прав/архивации.
type candidateTarget struct {
	targetType string // agent|squad
	targetID   string // agent: operative id; squad: crew id (лидер резолвится отдельно)
	source     string
}

// operativeRef — минимум об агенте, нужный для проверки прав вызова.
type operativeRef struct {
	ID             string
	WorkspaceID    string
	OwnerAccountID *string
	PermissionMode string
	ArchivedAt     *time.Time
	ExecutorID     string
}

func (s *Store) getOperative(ctx context.Context, workspaceID, id string) (operativeRef, bool, error) {
	var o operativeRef
	o.WorkspaceID = workspaceID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, op_owner_account_id, op_permission_mode, op_archived_at, executor_id
		FROM operatives WHERE workspace_id = $1 AND id = $2`, workspaceID, id).
		Scan(&o.ID, &o.OwnerAccountID, &o.PermissionMode, &o.ArchivedAt, &o.ExecutorID)
	if store.IsNoRows(err) {
		return operativeRef{}, false, nil
	}
	if err != nil {
		return operativeRef{}, false, err
	}
	return o, true, nil
}

type crewRef struct {
	ID         string
	LeaderType string
	LeaderID   string
	ArchivedAt *time.Time
}

func (s *Store) getCrew(ctx context.Context, workspaceID, id string) (crewRef, bool, error) {
	var c crewRef
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, crew_leader_type, crew_leader_id, crew_archived_at
		FROM crews WHERE workspace_id = $1 AND id = $2`, workspaceID, id).
		Scan(&c.ID, &c.LeaderType, &c.LeaderID, &c.ArchivedAt)
	if store.IsNoRows(err) {
		return crewRef{}, false, nil
	}
	if err != nil {
		return crewRef{}, false, err
	}
	return c, true, nil
}

// canInvokeOperative — «Полномочие на вызов агента» (контракт §1.8): владелец
// агента, либо owner/admin воркспейса, либо (permission_mode=public_to) актор
// входит в invocation_targets. Проверка «агент вне своей области» (когда сам
// вызывающий — агент-актор) не реализуется здесь: авторизация задач-агентов
// (task_token) вне объёма T-026/T-027 (см. server2/docs/decisions.md).
func (d *Deps) canInvokeOperative(ctx context.Context, o operativeRef, actorAccountID string, role httpapi.Role) (bool, error) {
	if o.OwnerAccountID != nil && *o.OwnerAccountID == actorAccountID {
		return true, nil
	}
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true, nil
	}
	if o.PermissionMode != "public_to" {
		return false, nil
	}
	rows, err := d.db.Pool.Query(ctx, `
		SELECT opt_target_type, opt_target_id FROM operative_targets WHERE operative_id = $1`, o.ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var targetType string
		var targetID *string
		if err := rows.Scan(&targetType, &targetID); err != nil {
			return false, err
		}
		switch targetType {
		case "workspace":
			return true, nil
		case "member":
			if targetID != nil && *targetID == actorAccountID {
				return true, nil
			}
		case "team":
			// Команды (teams) не входят в модель данных T-027 (нет таблицы
			// в 51-data-model.md для рабочих команд отдельно от space
			// участников) — пробел спецификации, задокументирован в
			// decisions.md: team-таргет никогда не совпадает, пока команды
			// не появятся как домен.
		}
	}
	return false, rows.Err()
}

// resolveTargets определяет источники срабатывания для content
// (контракт §1.10): mention_agent/mention_squad_leader, issue_assignee,
// thread_parent. conversation_continuation — задокументированный пробел
// (см. decisions.md): требует состояния очереди задач агента, которым
// владеет ещё не существующий на момент этой реализации internal/dispatch.
func (d *Deps) resolveTargets(ctx context.Context, ticket TicketInfo, content string, parentID *string) ([]candidateTarget, error) {
	if isNoteComment(content) {
		return nil, nil
	}
	var out []candidateTarget
	seen := map[string]bool{}
	add := func(targetType, targetID, source string) {
		key := targetType + ":" + targetID
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, candidateTarget{targetType: targetType, targetID: targetID, source: source})
	}

	if ticket.AssigneeType != nil && ticket.AssigneeID != nil {
		switch *ticket.AssigneeType {
		case "agent":
			add("agent", *ticket.AssigneeID, "issue_assignee")
		case "squad":
			add("squad", *ticket.AssigneeID, "issue_assignee")
		}
	}

	for _, m := range parseMentions(content) {
		if m.kind == "agent" {
			add("agent", m.id, "mention_agent")
		} else {
			add("squad", m.id, "mention_squad_leader")
		}
	}

	if parentID != nil {
		parent, err := d.Store.GetComment(ctx, *parentID)
		if err == nil && parent.AuthorType == "agent" {
			add("agent", parent.AuthorID, "thread_parent")
		}
	}

	return out, nil
}

// evaluateTargets применяет права/архивацию/self-trigger/suppress к
// candidateTarget-ам и, для тех, что прошли, спрашивает Dispatcher (если он
// уже подключён — см. deps.go) о фактическом исходе постановки в очередь.
// triggerCommentID — id комментария-триггера (пусто для previewCommentTriggers,
// который ничего не создаёт и не ставит в очередь).
func (d *Deps) evaluateTargets(ctx context.Context, ticket TicketInfo, targets []candidateTarget,
	actorAccountID string, role httpapi.Role, suppressAgentIDs map[string]bool, triggerCommentID string) []TriggerOutcome {

	var out []TriggerOutcome
	for _, t := range targets {
		agentID := t.targetID
		if t.targetType == "squad" {
			crew, ok, err := d.Store.getCrew(ctx, ticket.WorkspaceID, t.targetID)
			if err != nil {
				out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "internal_error"})
				continue
			}
			if !ok || crew.ArchivedAt != nil || crew.LeaderType != "agent" {
				out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "invocation_not_allowed"})
				continue
			}
			agentID = crew.LeaderID
		}
		if suppressAgentIDs[agentID] {
			out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "self_trigger_suppressed"})
			continue
		}
		op, ok, err := d.Store.getOperative(ctx, ticket.WorkspaceID, agentID)
		if err != nil {
			out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "internal_error"})
			continue
		}
		if !ok || op.ArchivedAt != nil {
			out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "invocation_not_allowed"})
			continue
		}
		allowed, err := d.canInvokeOperative(ctx, op, actorAccountID, role)
		if err != nil {
			out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "internal_error"})
			continue
		}
		if !allowed {
			out = append(out, TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "invocation_not_allowed"})
			continue
		}
		out = append(out, d.dispatchOutcome(ctx, ticket, t, op, triggerCommentID))
	}
	return out
}

// dispatchOutcome спрашивает Dispatcher (dispatch.Enqueue из соседнего
// пакета T-027, см. deps.go) о фактическом исходе постановки в очередь.
// Пока Dispatcher не подключён (пакет internal/dispatch ещё не существовал
// на момент этой сессии, см. server2/docs/decisions.md), права уже
// проверены выше, но реальная постановка в очередь невозможна — честно
// отражается internal_error, а не выдумывается "queued".
func (d *Deps) dispatchOutcome(ctx context.Context, ticket TicketInfo, t candidateTarget, op operativeRef, triggerCommentID string) TriggerOutcome {
	if d.Dispatcher == nil {
		return TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "internal_error"}
	}
	res, err := d.Dispatcher.EnqueueCommentTrigger(ctx, DispatchTriggerInput{
		WorkspaceID:   ticket.WorkspaceID,
		TicketID:      ticket.ID,
		OperativeID:   op.ID,
		ExecutorID:    op.ExecutorID,
		Source:        t.source,
		TriggerNoteID: triggerCommentID,
	})
	if err != nil {
		return TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: "blocked", ReasonCode: "internal_error"}
	}
	return TriggerOutcome{TargetType: t.targetType, TargetID: t.targetID, Status: res.Status, ReasonCode: res.ReasonCode}
}
