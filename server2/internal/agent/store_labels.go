package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrLabelNotFound — метка типа agent не существует в воркспейсе (404).
var ErrLabelNotFound = errors.New("agent: метка не найдена")

// ErrResourceLabelsDisabled — фича ресурсных меток выключена в воркспейсе (404).
var ErrResourceLabelsDisabled = errors.New("agent: ресурсные метки выключены в воркспейсе")

// Label — components/schemas/Label в контексте resource_type=agent. Здесь —
// собственная копия формы (не импорт internal/tagging), поскольку agent не
// владеет CRUD определений меток, только их привязкой/отвязкой к operatives
// через общую таблицу tags/operative_tag_links (005_tasks.up.sql) — то же
// решение, что internal/pin и internal/note применяют к другим ресурсам.
type Label struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	ResourceType string    `json:"resource_type"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	UsageCount   int       `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ResourceLabelsEnabled — то же правило, что internal/tagging (contract §3):
// spaces.ws_settings->>'resource_labels_enabled', по умолчанию false.
func (s *Store) ResourceLabelsEnabled(ctx context.Context, workspaceID string) (bool, error) {
	var enabled bool
	err := s.db.Pool.QueryRow(ctx, `SELECT COALESCE((ws_settings->>'resource_labels_enabled')::boolean, false)
		FROM spaces WHERE id = $1`, workspaceID).Scan(&enabled)
	if err != nil {
		return false, fmt.Errorf("agent: проверка флага ресурсных меток: %w", err)
	}
	return enabled, nil
}

func scanLabelRow(row interface {
	Scan(dest ...any) error
}) (Label, error) {
	var l Label
	var desc *string
	if err := row.Scan(&l.ID, &l.WorkspaceID, &l.ResourceType, &l.Name, &desc, &l.Color, &l.UsageCount,
		&l.CreatedAt, &l.UpdatedAt); err != nil {
		return Label{}, err
	}
	l.Description = strOr(desc, "")
	return l, nil
}

const labelColumns = `id, workspace_id, tag_resource_type, tag_label, tag_summary, tag_color, tag_usage_count, created_at, updated_at`

// ListAgentLabels — метки, привязанные к агенту.
func (s *Store) ListAgentLabels(ctx context.Context, agentID string) ([]Label, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+labelColumns+` FROM tags t JOIN operative_tag_links l ON l.tag_id = t.id
		WHERE l.operative_id = $1 ORDER BY t.tag_label ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent: метки агента: %w", err)
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		l, err := scanLabelRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AttachAgentLabel — прикрепить метку типа agent к агенту.
func (s *Store) AttachAgentLabel(ctx context.Context, workspaceID, agentID, labelID string) ([]Label, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM tags WHERE id = $1 AND workspace_id = $2 AND tag_resource_type = 'agent')`,
		labelID, workspaceID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("agent: проверка метки: %w", err)
	}
	if !exists {
		return nil, ErrLabelNotFound
	}
	if _, err := s.db.Pool.Exec(ctx, `INSERT INTO operative_tag_links (operative_id, tag_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, agentID, labelID); err != nil {
		return nil, fmt.Errorf("agent: привязка метки: %w", err)
	}
	return s.ListAgentLabels(ctx, agentID)
}

// DetachAgentLabel — открепить.
func (s *Store) DetachAgentLabel(ctx context.Context, agentID, labelID string) ([]Label, error) {
	if _, err := s.db.Pool.Exec(ctx, `DELETE FROM operative_tag_links WHERE operative_id = $1 AND tag_id = $2`,
		agentID, labelID); err != nil {
		return nil, fmt.Errorf("agent: отвязка метки: %w", err)
	}
	return s.ListAgentLabels(ctx, agentID)
}

// --- флаги видимости, читаемые из ws_settings ------------------------------
//
// Ни один из следующих двух флагов не выведен в отдельную колонку контрактом
// или data-model.md — тот же приём, что ResourceLabelsEnabled выше и что
// internal/tagging применяет к своей версии того же флага; решения
// зафиксированы в server2/docs/decisions.md.

// alwaysRevealSecrets — "настройка «всегда раскрывать секреты»" (contract
// §10.3): spaces.ws_settings->>'always_reveal_agent_secrets', по умолчанию false.
func (s *Store) alwaysRevealSecrets(ctx context.Context, workspaceID string) (bool, error) {
	var v bool
	err := s.db.Pool.QueryRow(ctx, `SELECT COALESCE((ws_settings->>'always_reveal_agent_secrets')::boolean, false)
		FROM spaces WHERE id = $1`, workspaceID).Scan(&v)
	if err != nil {
		return false, fmt.Errorf("agent: настройка раскрытия секретов: %w", err)
	}
	return v, nil
}

// composioEnabled — "функция выключена в воркспейсе" для
// composio_toolkit_allowlist (contract §10.3): ws_settings->>'composio_enabled'.
func (s *Store) composioEnabled(ctx context.Context, workspaceID string) (bool, error) {
	var v bool
	err := s.db.Pool.QueryRow(ctx, `SELECT COALESCE((ws_settings->>'composio_enabled')::boolean, false)
		FROM spaces WHERE id = $1`, workspaceID).Scan(&v)
	if err != nil {
		return false, fmt.Errorf("agent: настройка composio: %w", err)
	}
	return v, nil
}
