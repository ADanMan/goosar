package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

// positionEpsilon — если промежуток между двумя float64-соседями меньше
// этого порога, считаем позиции "схлопнувшимися" (contract §1.15: "соседи
// были переставлены кем-то ещё, либо математически неразличимы") и требуем
// от клиента перечитать список и повторить.
const positionEpsilon = 1e-9

// MoveIssue — moveIssue (contract §1.15): вычисляет position как среднюю
// точку между before_id/after_id (любой может быть nil — "в самое начало"/
// "в самый конец" списка) и применяет f (остальные поля PUT) в той же
// транзакции. Возвращает ErrConflict, если промежуток схлопнулся.
func (s *Store) MoveIssue(ctx context.Context, workspaceID, id string, beforeID, afterID *string, f UpdateFields) (Issue, error) {
	var out Issue
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var beforePos, afterPos *float64
		if beforeID != nil {
			var p float64
			if err := tx.QueryRow(ctx, `SELECT tk_position FROM tickets WHERE workspace_id=$1 AND id=$2`, workspaceID, *beforeID).Scan(&p); err != nil {
				if store.IsNoRows(err) {
					return ErrNotFound
				}
				return err
			}
			beforePos = &p
		}
		if afterID != nil {
			var p float64
			if err := tx.QueryRow(ctx, `SELECT tk_position FROM tickets WHERE workspace_id=$1 AND id=$2`, workspaceID, *afterID).Scan(&p); err != nil {
				if store.IsNoRows(err) {
					return ErrNotFound
				}
				return err
			}
			afterPos = &p
		}

		var newPos float64
		switch {
		case beforePos != nil && afterPos != nil:
			if *beforePos-*afterPos < positionEpsilon && *afterPos-*beforePos < positionEpsilon {
				return ErrConflict
			}
			newPos = (*beforePos + *afterPos) / 2
			if newPos <= *afterPos || newPos >= *beforePos {
				return ErrConflict
			}
		case beforePos != nil:
			newPos = *beforePos - 1024
		case afterPos != nil:
			newPos = *afterPos + 1024
		default:
			newPos = 1024
		}

		f.PositionSet = true
		f.Position = newPos
		setClauses, args := applyUpdate(f)
		setClauses = append(setClauses, "updated_at = now()")
		fullArgs := append([]any{workspaceID, id}, args...)
		query := `UPDATE tickets SET ` + strings.Join(setClauses, ", ") + ` WHERE workspace_id=$1 AND id=$2 RETURNING ` + ticketColumns
		row := tx.QueryRow(ctx, query, fullArgs...)
		a, scanErr := scanIssue(row)
		if store.IsNoRows(scanErr) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("task: перестановка задачи: %w", scanErr)
		}
		out = a
		return nil
	})
	if err != nil {
		return Issue{}, err
	}
	return out, nil
}
