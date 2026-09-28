package autopilot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// ErrAssigneeNotFound — assignee_type/assignee_id не резолвится ни в один
// operatives/crews этого воркспейса.
var ErrAssigneeNotFound = errors.New("autopilot: исполнитель не найден")

// ErrAssigneeArchived — исполнитель существует, но архивирован (contract:
// "исполнитель должен существовать, не быть архивированным").
var ErrAssigneeArchived = errors.New("autopilot: исполнитель архивирован")

// ErrSquadLeaderNotAgent — у отряда лидер-человек, не агент: автопилот не
// может поставить задачу в очередь агенту (dispatch_jobs.operative_id
// обязателен) — см. server2/docs/decisions.md, раздел T-028.
var ErrSquadLeaderNotAgent = errors.New("autopilot: у отряда нет лидера-агента для постановки в очередь")

// Assignee — то немногое об исполнителе автопилота (агент или лидер отряда),
// что нужно и для проверки прав (CanInvoke), и для постановки в очередь
// (OperativeID/ExecutorID для dispatch.JobSpec).
type Assignee struct {
	OperativeID    string // агент, который реально исполнит запуск (лидер, если assignee_type=squad)
	ExecutorID     string
	CrewID         string // непусто, если assignee_type=squad
	PermissionMode string // op_permission_mode агента-исполнителя
	OwnerAccountID *string
	Archived       bool
}

// ResolveAssignee резолвит assignee_type/assignee_id в operatives (agent) или
// crews (squad, через лидера) этого воркспейса.
func (s *Store) ResolveAssignee(ctx context.Context, workspaceID, assigneeType, assigneeID string) (Assignee, error) {
	switch assigneeType {
	case "agent":
		return s.resolveAgent(ctx, workspaceID, assigneeID)
	case "squad":
		return s.resolveSquad(ctx, workspaceID, assigneeID)
	default:
		return Assignee{}, fmt.Errorf("autopilot: неизвестный assignee_type %q", assigneeType)
	}
}

func (s *Store) resolveAgent(ctx context.Context, workspaceID, agentID string) (Assignee, error) {
	var a Assignee
	var archivedAt *time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT executor_id, op_permission_mode, op_owner_account_id, op_archived_at
		FROM operatives WHERE workspace_id = $1 AND id = $2`, workspaceID, agentID).
		Scan(&a.ExecutorID, &a.PermissionMode, &a.OwnerAccountID, &archivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Assignee{}, ErrAssigneeNotFound
	}
	if err != nil {
		return Assignee{}, fmt.Errorf("autopilot: резолв агента-исполнителя: %w", err)
	}
	a.OperativeID = agentID
	a.Archived = archivedAt != nil
	return a, nil
}

func (s *Store) resolveSquad(ctx context.Context, workspaceID, crewID string) (Assignee, error) {
	var leaderType, leaderID string
	var archivedAt *time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT crew_leader_type, crew_leader_id, crew_archived_at
		FROM crews WHERE workspace_id = $1 AND id = $2`, workspaceID, crewID).
		Scan(&leaderType, &leaderID, &archivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Assignee{}, ErrAssigneeNotFound
	}
	if err != nil {
		return Assignee{}, fmt.Errorf("autopilot: резолв отряда-исполнителя: %w", err)
	}
	if leaderType != "agent" {
		return Assignee{}, ErrSquadLeaderNotAgent
	}
	agent, err := s.resolveAgent(ctx, workspaceID, leaderID)
	if err != nil {
		return Assignee{}, err
	}
	agent.CrewID = crewID
	agent.Archived = agent.Archived || archivedAt != nil
	return agent, nil
}

// CanInvoke — тот же пробел спецификации, что и chat.Store.CanInvoke (см.
// server2/docs/decisions.md, раздел T-027 "project/feed/chat"): контракт не
// формализует алгоритм "доступен вызывающему для запуска", только форму
// operatives/operative_targets. autopilot переиспользует то же решение
// (owner/admin — всегда; private — только владелец; public_to — по
// operative_targets), реализованное здесь заново, а не через internal/chat,
// чтобы не создавать зависимость домена autopilot от домена chat ради одной
// функции (тот же принцип, каким note/tagging/task не делят SQL-хелперы).
func (s *Store) CanInvoke(ctx context.Context, a Assignee, actorID string, role httpapi.Role) (bool, error) {
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true, nil
	}
	if a.PermissionMode == "private" {
		return a.OwnerAccountID != nil && *a.OwnerAccountID == actorID, nil
	}
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM operative_targets ot
			WHERE ot.operative_id = $1 AND (
				ot.opt_target_type = 'workspace'
				OR (ot.opt_target_type = 'member' AND ot.opt_target_id = $2)
				OR (ot.opt_target_type = 'team' AND EXISTS(
					SELECT 1 FROM crew_members cm
					WHERE cm.crew_id = ot.opt_target_id AND cm.cm_member_type = 'member' AND cm.cm_member_id = $2))
			)
		)`, a.OperativeID, actorID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("autopilot: проверка доступа к исполнителю: %w", err)
	}
	return exists, nil
}
