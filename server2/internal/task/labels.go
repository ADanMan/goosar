package task

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

const labelColumns = `id, workspace_id, tag_resource_type, tag_label, tag_summary, tag_color, tag_usage_count, created_at, updated_at`

func scanLabel(row pgx.Row) (Label, error) {
	var l Label
	var summary *string
	if err := row.Scan(&l.ID, &l.WorkspaceID, &l.ResourceType, &l.Name, &summary, &l.Color, &l.UsageCount, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return Label{}, err
	}
	if summary != nil {
		l.Description = *summary
	}
	return l, nil
}

// ListIssueLabels — метки, прикреплённые к задаче (listIssueLabels).
func (s *Store) ListIssueLabels(ctx context.Context, workspaceID, issueID string) ([]Label, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+labelColumns+` FROM tags t
		JOIN ticket_tag_links l ON l.tag_id = t.id
		WHERE l.ticket_id = $1 AND t.workspace_id = $2
		ORDER BY t.tag_label`, issueID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("task: метки задачи: %w", err)
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if out == nil {
		out = []Label{}
	}
	return out, rows.Err()
}

var ErrLabelNotFound = fmt.Errorf("task: метка (типа issue) не найдена в воркспейсе")

// AttachIssueLabel прикрепляет метку labelID (resource_type=issue) к задаче
// (attachIssueLabel) — идемпотентно (повторное прикрепление не дублирует
// строку и не наращивает usage_count второй раз), возвращает обновлённый
// список меток задачи.
func (s *Store) AttachIssueLabel(ctx context.Context, workspaceID, issueID, labelID string) ([]Label, error) {
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tags WHERE id=$1 AND workspace_id=$2 AND tag_resource_type='issue')`,
			labelID, workspaceID).Scan(&exists); err != nil {
			return fmt.Errorf("task: проверка метки: %w", err)
		}
		if !exists {
			return ErrLabelNotFound
		}
		tag, err := tx.Exec(ctx, `INSERT INTO ticket_tag_links (ticket_id, tag_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, issueID, labelID)
		if err != nil {
			return fmt.Errorf("task: прикрепление метки: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `UPDATE tags SET tag_usage_count = tag_usage_count + 1, updated_at = now() WHERE id = $1`, labelID); err != nil {
				return fmt.Errorf("task: обновление счётчика метки: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ListIssueLabels(ctx, workspaceID, issueID)
}

// DetachIssueLabel открепляет метку от задачи (detachIssueLabel).
func (s *Store) DetachIssueLabel(ctx context.Context, workspaceID, issueID, labelID string) ([]Label, error) {
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tags WHERE id=$1 AND workspace_id=$2 AND tag_resource_type='issue')`,
			labelID, workspaceID).Scan(&exists); err != nil {
			return fmt.Errorf("task: проверка метки: %w", err)
		}
		if !exists {
			return ErrLabelNotFound
		}
		tag, err := tx.Exec(ctx, `DELETE FROM ticket_tag_links WHERE ticket_id=$1 AND tag_id=$2`, issueID, labelID)
		if err != nil {
			return fmt.Errorf("task: открепление метки: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `UPDATE tags SET tag_usage_count = GREATEST(tag_usage_count - 1, 0), updated_at = now() WHERE id = $1`, labelID); err != nil {
				return fmt.Errorf("task: обновление счётчика метки: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ListIssueLabels(ctx, workspaceID, issueID)
}

// ---------------------------------------------------------------------------
// Metadata (contract §1.16): плоский словарь без предопределения ключей.
// ---------------------------------------------------------------------------

var metadataKeyRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)

const maxMetadataKeys = 50
const maxMetadataBytes = 8 * 1024

func (s *Store) GetMetadata(ctx context.Context, workspaceID, issueID string) (json.RawMessage, error) {
	i, err := s.GetIssue(ctx, workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	return orEmptyObject(i.Metadata), nil
}

func orEmptyObject(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	return b
}

// SetMetadataKey — setIssueMetadataKey: value — примитив, не null/objet/array;
// не более maxMetadataKeys ключей и maxMetadataBytes суммарно после установки.
func (s *Store) SetMetadataKey(ctx context.Context, workspaceID, issueID, key string, value any) (json.RawMessage, error) {
	if !metadataKeyRe.MatchString(key) {
		return nil, errBadRequest("metadata key does not match ^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$")
	}
	switch value.(type) {
	case string, float64, bool:
	default:
		return nil, errBadRequest("metadata value must be a string, number or boolean")
	}
	var result json.RawMessage
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT tk_metadata FROM tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, issueID)
		var raw []byte
		if err := row.Scan(&raw); err != nil {
			if store.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		m := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m)
		}
		_, existed := m[key]
		m[key] = value
		if !existed && len(m) > maxMetadataKeys {
			return errBadRequest(fmt.Sprintf("issue metadata cannot have more than %d keys", maxMetadataKeys))
		}
		encoded, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if len(encoded) > maxMetadataBytes {
			return errBadRequest(fmt.Sprintf("issue metadata cannot exceed %d bytes", maxMetadataBytes))
		}
		if _, err := tx.Exec(ctx, `UPDATE tickets SET tk_metadata=$1, updated_at=now() WHERE workspace_id=$2 AND id=$3`, encoded, workspaceID, issueID); err != nil {
			return err
		}
		result = encoded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteMetadataKey — deleteIssueMetadataKey.
func (s *Store) DeleteMetadataKey(ctx context.Context, workspaceID, issueID, key string) (json.RawMessage, error) {
	var result json.RawMessage
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT tk_metadata FROM tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, issueID)
		var raw []byte
		if err := row.Scan(&raw); err != nil {
			if store.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		m := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m)
		}
		delete(m, key)
		encoded, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tickets SET tk_metadata=$1, updated_at=now() WHERE workspace_id=$2 AND id=$3`, encoded, workspaceID, issueID); err != nil {
			return err
		}
		result = encoded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
