package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

// UpdateFields — то, что действительно поменять в задаче (updateIssue/moveIssue,
// частично — batchUpdateIssues). *Set==false — поле не передавалось в запросе
// (contract: "не передавать поле — не трогать значение"); *Set==true с nil-полем
// (для nullable-полей) — поле явно очищено ("передать null — явно очистить").
type UpdateFields struct {
	TitleSet bool
	Title    string

	DescSet bool
	Desc    *string

	StatusSet bool
	Status    string

	PrioritySet bool
	Priority    string

	// AssigneeSet — assignee_type/assignee_id пришли парой (или оба null,
	// чтобы снять исполнителя). Контракт требует передавать их вместе.
	AssigneeSet  bool
	AssigneeType *string
	AssigneeID   *string

	PositionSet bool
	Position    float64

	StartSet bool
	Start    *time.Time

	DueSet bool
	Due    *time.Time

	ParentSet bool
	ParentID  *string

	ProjectSet bool
	ProjectID  *string

	StageSet bool
	Stage    *int
}

func (f UpdateFields) any() bool {
	return f.TitleSet || f.DescSet || f.StatusSet || f.PrioritySet || f.AssigneeSet ||
		f.PositionSet || f.StartSet || f.DueSet || f.ParentSet || f.ProjectSet || f.StageSet
}

// applyUpdate строит "col = $n" SET-список из UpdateFields, с плейсхолдерами
// начиная с $3 ($1/$2 — workspace_id/id в WHERE вызывающего запроса).
func applyUpdate(f UpdateFields) (setClauses []string, args []any) {
	add := func(col string, val any) {
		args = append(args, val)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, len(args)+2))
	}
	if f.TitleSet {
		add("tk_headline", f.Title)
	}
	if f.DescSet {
		add("tk_narrative", f.Desc)
	}
	if f.StatusSet {
		add("tk_status", f.Status)
	}
	if f.PrioritySet {
		add("tk_priority", f.Priority)
	}
	if f.AssigneeSet {
		add("tk_assignee_type", f.AssigneeType)
		add("tk_assignee_id", f.AssigneeID)
	}
	if f.PositionSet {
		add("tk_position", f.Position)
	}
	if f.StartSet {
		add("tk_start_date", f.Start)
	}
	if f.DueSet {
		add("tk_due_date", f.Due)
	}
	if f.ParentSet {
		add("tk_parent_ticket_id", f.ParentID)
	}
	if f.ProjectSet {
		add("initiative_id", f.ProjectID)
	}
	if f.StageSet {
		add("tk_stage", f.Stage)
	}
	return setClauses, args
}

// UpdateIssue применяет f к задаче id в транзакции (SELECT ... FOR UPDATE,
// затем UPDATE), возвращая состояние до и после — вызывающий (handlers.go)
// использует "before" для определения assigneeChanged/statusChanged
// (правило автозапуска, contract §1.9) и для отмены активных запусков, если
// исполнитель снят или статус ушёл в done/cancelled.
func (s *Store) UpdateIssue(ctx context.Context, workspaceID, id string, f UpdateFields) (before, after Issue, err error) {
	if !f.any() {
		before, err = s.GetIssue(ctx, workspaceID, id)
		return before, before, err
	}
	err = s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+ticketColumns+` FROM tickets WHERE workspace_id = $1 AND id = $2 FOR UPDATE`, workspaceID, id)
		b, scanErr := scanIssue(row)
		if store.IsNoRows(scanErr) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("task: чтение задачи для обновления: %w", scanErr)
		}
		before = b

		setClauses, args := applyUpdate(f)
		setClauses = append(setClauses, "updated_at = now()")
		fullArgs := append([]any{workspaceID, id}, args...)
		query := `UPDATE tickets SET ` + strings.Join(setClauses, ", ") + ` WHERE workspace_id = $1 AND id = $2 RETURNING ` + ticketColumns
		row = tx.QueryRow(ctx, query, fullArgs...)
		a, scanErr := scanIssue(row)
		if scanErr != nil {
			return fmt.Errorf("task: обновление задачи: %w", scanErr)
		}
		after = a

		if f.StatusSet && before.Status != after.Status {
			details, _ := json.Marshal(map[string]any{"from": before.Status, "to": after.Status})
			if err := recordActivity(ctx, tx, workspaceID, id, "system", nil, "status_changed", details); err != nil {
				return err
			}
		}
		if f.AssigneeSet && !(ptrEq(before.AssigneeType, after.AssigneeType) && ptrEq(before.AssigneeID, after.AssigneeID)) {
			details, _ := json.Marshal(map[string]any{
				"from_type": before.AssigneeType, "from_id": before.AssigneeID,
				"to_type": after.AssigneeType, "to_id": after.AssigneeID,
			})
			if err := recordActivity(ctx, tx, workspaceID, id, "system", nil, "assignee_changed", details); err != nil {
				return err
			}
		}
		return nil
	})
	return before, after, err
}

// DeleteIssue удаляет задачу жёстко (contract §1421: "задачи... удаляются
// жёстко"). found=false — задачи не было.
func (s *Store) DeleteIssue(ctx context.Context, workspaceID, id string) (found bool, err error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM tickets WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return false, fmt.Errorf("task: удаление задачи: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// fieldsFromRaw разбирает сырое тело PUT/PATCH (updateIssue/moveIssue) в
// UpdateFields, различая "поле не передано" и "поле передано как null" через
// internal/task/patch.go. Дата (start_date/due_date) приходит строкой
// YYYY-MM-DD; assignee_type/assignee_id читаются как пара (валидация "оба
// или ни одного" — на вызывающей стороне, handlers.go, ей же нужен role/actor).
func (s *Store) fieldsFromRaw(body json.RawMessage) (UpdateFields, error) {
	rf, err := rawFields(body)
	if err != nil {
		return UpdateFields{}, err
	}
	var f UpdateFields

	if v, present, err := stringField(rf, "title"); err != nil {
		return UpdateFields{}, err
	} else if present && v != nil {
		f.TitleSet, f.Title = true, *v
	}
	if v, present, err := stringField(rf, "description"); err != nil {
		return UpdateFields{}, err
	} else if present {
		f.DescSet, f.Desc = true, v
	}
	if v, present, err := stringField(rf, "status"); err != nil {
		return UpdateFields{}, err
	} else if present && v != nil {
		f.StatusSet, f.Status = true, *v
	}
	if v, present, err := stringField(rf, "priority"); err != nil {
		return UpdateFields{}, err
	} else if present && v != nil {
		f.PrioritySet, f.Priority = true, *v
	}
	_, assigneeTypePresent := rf["assignee_type"]
	_, assigneeIDPresent := rf["assignee_id"]
	if assigneeTypePresent || assigneeIDPresent {
		f.AssigneeSet = true
		if v, _, err := stringField(rf, "assignee_type"); err != nil {
			return UpdateFields{}, err
		} else {
			f.AssigneeType = v
		}
		if v, _, err := stringField(rf, "assignee_id"); err != nil {
			return UpdateFields{}, err
		} else {
			f.AssigneeID = v
		}
	}
	if v, present, err := float64Field(rf, "position"); err != nil {
		return UpdateFields{}, err
	} else if present && v != nil {
		f.PositionSet, f.Position = true, *v
	}
	if v, present, err := stringField(rf, "start_date"); err != nil {
		return UpdateFields{}, err
	} else if present {
		t, perr := parseDate(v)
		if perr != nil {
			return UpdateFields{}, errBadRequest("start_date must be YYYY-MM-DD")
		}
		f.StartSet, f.Start = true, t
	}
	if v, present, err := stringField(rf, "due_date"); err != nil {
		return UpdateFields{}, err
	} else if present {
		t, perr := parseDate(v)
		if perr != nil {
			return UpdateFields{}, errBadRequest("due_date must be YYYY-MM-DD")
		}
		f.DueSet, f.Due = true, t
	}
	if v, present, err := stringField(rf, "parent_issue_id"); err != nil {
		return UpdateFields{}, err
	} else if present {
		f.ParentSet, f.ParentID = true, v
	}
	if v, present, err := stringField(rf, "project_id"); err != nil {
		return UpdateFields{}, err
	} else if present {
		f.ProjectSet, f.ProjectID = true, v
	}
	if v, present, err := intField(rf, "stage"); err != nil {
		return UpdateFields{}, err
	} else if present {
		f.StageSet, f.Stage = true, v
	}
	return f, nil
}

func ptrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
