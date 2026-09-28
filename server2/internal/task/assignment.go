package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// operative — то, что нужно из operatives+executors для проверки
// назначения/полномочия на вызов и для правила автозапуска (contract §1.8-1.9).
type operative struct {
	ID             string
	ExecutorID     string
	OwnerAccountID *string
	PermissionMode string
	Archived       bool
	ExecutorOnline bool
}

func (s *Store) getOperative(ctx context.Context, workspaceID, id string) (operative, bool, error) {
	var op operative
	var archivedAt *time.Time
	var executorStatus string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT o.id, o.executor_id, o.op_owner_account_id, o.op_permission_mode, o.op_archived_at, e.ex_status
		FROM operatives o JOIN executors e ON e.id = o.executor_id
		WHERE o.id = $1 AND o.workspace_id = $2`, id, workspaceID).
		Scan(&op.ID, &op.ExecutorID, &op.OwnerAccountID, &op.PermissionMode, &archivedAt, &executorStatus)
	if store.IsNoRows(err) {
		return operative{}, false, nil
	}
	if err != nil {
		return operative{}, false, fmt.Errorf("task: получение агента-исполнителя: %w", err)
	}
	op.Archived = archivedAt != nil
	op.ExecutorOnline = executorStatus == "online"
	return op, true, nil
}

// crewLeader — лидер отряда (для assignee_type=squad, contract §1.8: "его
// лидер (агент) не должен быть архивирован").
func (s *Store) crewLeader(ctx context.Context, workspaceID, crewID string) (leaderID string, isAgent, archived, found bool, err error) {
	var leaderType string
	var archivedAt *time.Time
	err = s.db.Pool.QueryRow(ctx, `
		SELECT crew_leader_type, crew_leader_id, crew_archived_at FROM crews
		WHERE id = $1 AND workspace_id = $2`, crewID, workspaceID).
		Scan(&leaderType, &leaderID, &archivedAt)
	if store.IsNoRows(err) {
		return "", false, false, false, nil
	}
	if err != nil {
		return "", false, false, false, fmt.Errorf("task: получение лидера отряда: %w", err)
	}
	return leaderID, leaderType == "agent", archivedAt != nil, true, nil
}

// canInvokeAgent — «полномочие на вызов агента» (contract §1.8). role —
// роль actor.UserID в workspaceID ("" — actor не человек-участник, т.е. агент).
func (s *Store) canInvokeAgent(ctx context.Context, workspaceID string, actor *httpapi.Actor, role string, op operative) (bool, error) {
	if role == "owner" || role == "admin" {
		return true, nil
	}
	if op.OwnerAccountID != nil && actor.IsHuman && *op.OwnerAccountID == actor.UserID {
		return true, nil
	}
	if op.PermissionMode != "public_to" {
		return false, nil
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT opt_target_type, opt_target_id FROM operative_targets WHERE operative_id = $1`, op.ID)
	if err != nil {
		return false, fmt.Errorf("task: проверка целей вызова агента: %w", err)
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
			if targetID != nil && actor.IsHuman && *targetID == actor.UserID {
				return true, nil
			}
			// "team" — не реализовано в этой сессии (нет домена команд за
			// пределами crews/squads в этом контракте); см. server2/docs/decisions.md.
		}
	}
	return false, rows.Err()
}

// validateAssignee проверяет assignee_type/assignee_id по правилам §1.8 и
// возвращает "агент, которого в итоге стоит запускать" (для squad — лидер),
// если применимо.
func (s *Store) validateAssignee(ctx context.Context, workspaceID string, actor *httpapi.Actor, role string, assigneeType, assigneeID *string) (runnable operative, isSquad bool, err error) {
	if assigneeType == nil || assigneeID == nil {
		return operative{}, false, nil
	}
	switch *assigneeType {
	case "member":
		var exists bool
		err = s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM space_members WHERE workspace_id=$1 AND account_id=$2)`,
			workspaceID, *assigneeID).Scan(&exists)
		if err != nil {
			return operative{}, false, fmt.Errorf("task: проверка участника-исполнителя: %w", err)
		}
		if !exists {
			return operative{}, false, errBadRequest("assignee member is not part of this workspace")
		}
		return operative{}, false, nil
	case "agent":
		op, found, err := s.getOperative(ctx, workspaceID, *assigneeID)
		if err != nil {
			return operative{}, false, err
		}
		if !found || op.Archived {
			return operative{}, false, errBadRequest("assignee agent does not exist or is archived")
		}
		ok, err := s.canInvokeAgent(ctx, workspaceID, actor, role, op)
		if err != nil {
			return operative{}, false, err
		}
		if !ok {
			return operative{}, false, errForbiddenInvoke()
		}
		return op, false, nil
	case "squad":
		leaderID, isAgentLead, archived, found, err := s.crewLeader(ctx, workspaceID, *assigneeID)
		if err != nil {
			return operative{}, false, err
		}
		if !found || archived || !isAgentLead {
			return operative{}, false, errBadRequest("assignee squad does not exist, is archived, or its leader is not an agent")
		}
		op, found, err := s.getOperative(ctx, workspaceID, leaderID)
		if err != nil {
			return operative{}, false, err
		}
		if !found || op.Archived {
			return operative{}, false, errBadRequest("squad leader agent is archived")
		}
		ok, err := s.canInvokeAgent(ctx, workspaceID, actor, role, op)
		if err != nil {
			return operative{}, false, err
		}
		if !ok {
			return operative{}, false, errForbiddenInvoke()
		}
		return op, true, nil
	default:
		return operative{}, false, errBadRequest("assignee_type must be member, agent or squad")
	}
}

// validationError — распознаваемая обёртка над ошибками валидации ввода
// (назначение, metadata, properties), чтобы handlers.go мог отличить 400 от
// 403 от внутренней ошибки без сравнения текста.
type validationError struct {
	forbidden bool
	message   string
}

func (e *validationError) Error() string { return e.message }

func errBadRequest(msg string) error { return &validationError{message: msg} }
func errForbiddenInvoke() error {
	return &validationError{forbidden: true, message: "caller is not authorized to invoke this agent"}
}

// autostartCases — чистая (без БД) часть правила §1.9: два независимых
// условия постановки в очередь. Вынесена отдельно, чтобы юнит-тест
// (rules_test.go) мог проверить саму логику решения без БД — DB-зависимая
// часть (доступность runtime, полномочие на вызов, защита от самозапуска)
// остаётся в EvaluateAutostart.
//
//  1. Назначение изменилось (или задача только что создана с исполнителем) и
//     итоговый статус — не backlog.
//  2. Статус изменился с backlog на любой другой, кроме done/cancelled, у
//     задачи, у которой уже был исполнитель-агент/отряд.
func autostartCases(before, after Issue, isCreate bool) (caseAssignment, caseStatus bool) {
	assigneeChanged := isCreate || !ptrEq(before.AssigneeType, after.AssigneeType) || !ptrEq(before.AssigneeID, after.AssigneeID)
	statusReopened := !isCreate && before.Status == "backlog" && after.Status != "backlog" &&
		after.Status != "done" && after.Status != "cancelled" &&
		before.AssigneeType != nil && before.AssigneeID != nil

	caseAssignment = assigneeChanged && after.Status != "backlog"
	caseStatus = statusReopened
	return caseAssignment, caseStatus
}

// AutostartDecision — итог правила §1.9: сработало ли правило и на кого
// (агент или лидер отряда) ставить задачу в очередь.
type AutostartDecision struct {
	Triggered bool
	Operative operative
	IsSquad   bool
	Reason    string // "assignment_changed" | "status_reopened" | "" (не сработало)
}

// EvaluateAutostart реализует contract §1.9 целиком (без побочных эффектов —
// preview-trigger зовёт это же и просто не вызывает Enqueue). before/after —
// состояние задачи до/после патча уже применённого домену БД; requestFromAgentID —
// если запрос пришёл от агента, id этого агента (для защиты от самозапуска),
// иначе "".
func (s *Store) EvaluateAutostart(ctx context.Context, workspaceID string, before, after Issue, isCreate bool, requestFromAgentID string, suppressRun bool) (AutostartDecision, error) {
	if suppressRun {
		return AutostartDecision{}, nil
	}
	if after.AssigneeType == nil || after.AssigneeID == nil {
		return AutostartDecision{}, nil
	}
	caseAssignment, caseStatus := autostartCases(before, after, isCreate)
	if !caseAssignment && !caseStatus {
		return AutostartDecision{}, nil
	}

	op, isSquad, found, err := s.resolveRunnable(ctx, workspaceID, *after.AssigneeType, *after.AssigneeID)
	if err != nil || !found {
		return AutostartDecision{}, err
	}
	if !op.ExecutorOnline || op.Archived {
		return AutostartDecision{}, nil
	}
	if requestFromAgentID != "" && requestFromAgentID == op.ID {
		// защита от самозапуска/петли (contract §1.9, случай 2).
		return AutostartDecision{}, nil
	}
	if caseStatus {
		has, err := dispatch.NewStore().HasPendingForOperativeOnTicket(ctx, s.db.Pool, op.ID, after.ID)
		if err != nil || has {
			return AutostartDecision{}, err
		}
	}
	reason := "assignment_changed"
	if caseStatus {
		reason = "status_reopened"
	}
	return AutostartDecision{Triggered: true, Operative: op, IsSquad: isSquad, Reason: reason}, nil
}

func (s *Store) resolveRunnable(ctx context.Context, workspaceID, assigneeType, assigneeID string) (operative, bool, bool, error) {
	switch assigneeType {
	case "agent":
		op, found, err := s.getOperative(ctx, workspaceID, assigneeID)
		return op, false, found, err
	case "squad":
		leaderID, isAgentLead, archived, found, err := s.crewLeader(ctx, workspaceID, assigneeID)
		if err != nil || !found || archived || !isAgentLead {
			return operative{}, true, false, err
		}
		op, found, err := s.getOperative(ctx, workspaceID, leaderID)
		return op, true, found, err
	default:
		return operative{}, false, false, nil
	}
}

func jsonRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
