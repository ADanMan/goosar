package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrSkillNotInWorkspace — один из skill_ids не принадлежит воркспейсу (400).
var ErrSkillNotInWorkspace = errors.New("agent: навык не принадлежит воркспейсу")

// ErrSkillNotAttached — навык не привязан к этому агенту (404).
var ErrSkillNotAttached = errors.New("agent: навык не привязан к этому агенту")

const skillSummaryColumns = `c.id, c.workspace_id, c.cap_title, c.cap_summary, c.cap_config,
	oc.opcap_enabled, c.cap_created_by, c.created_at, c.updated_at`

func scanSkillSummary(row pgx.Row) (SkillSummary, error) {
	var s SkillSummary
	var summary *string
	var cfg []byte
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.Name, &summary, &cfg, &s.Enabled, &s.CreatedBy,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return SkillSummary{}, err
	}
	s.Description = strOr(summary, "")
	s.Config = rawOr(cfg, emptyObject())
	return s, nil
}

// ListAgentSkills — навыки, привязанные к агенту (listAgentSkills).
func (s *Store) ListAgentSkills(ctx context.Context, agentID string) ([]SkillSummary, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+skillSummaryColumns+`
		FROM operative_capabilities oc JOIN capabilities c ON c.id = oc.capability_id
		WHERE oc.operative_id = $1 ORDER BY oc.created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent: навыки агента: %w", err)
	}
	defer rows.Close()
	out := []SkillSummary{}
	for rows.Next() {
		sk, err := scanSkillSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// skillIDsInWorkspace проверяет, что каждый id принадлежит воркспейсу.
func (s *Store) skillIDsInWorkspace(ctx context.Context, workspaceID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var count int
	if err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM capabilities WHERE workspace_id = $1 AND id = ANY($2)`,
		workspaceID, ids).Scan(&count); err != nil {
		return fmt.Errorf("agent: проверка навыков: %w", err)
	}
	if count != len(uniqueStrings(ids)) {
		return ErrSkillNotInWorkspace
	}
	return nil
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// SetAgentSkills — полная замена набора (setAgentSkills): удаляет
// непереданные, вставляет/оставляет переданные, все enabled=true.
func (s *Store) SetAgentSkills(ctx context.Context, workspaceID, agentID string, skillIDs []string) ([]SkillSummary, error) {
	if err := s.skillIDsInWorkspace(ctx, workspaceID, skillIDs); err != nil {
		return nil, err
	}
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM operative_capabilities WHERE operative_id = $1 AND NOT (capability_id = ANY($2))`,
			agentID, skillIDs); err != nil {
			return err
		}
		for _, id := range uniqueStrings(skillIDs) {
			if _, err := tx.Exec(ctx, `INSERT INTO operative_capabilities (operative_id, capability_id, opcap_enabled)
				VALUES ($1,$2,true) ON CONFLICT (operative_id, capability_id) DO NOTHING`, agentID, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agent: замена навыков: %w", err)
	}
	return s.ListAgentSkills(ctx, agentID)
}

// AddAgentSkills — добавить, не трогая существующие (addAgentSkills).
func (s *Store) AddAgentSkills(ctx context.Context, workspaceID, agentID string, skillIDs []string) ([]SkillSummary, error) {
	if err := s.skillIDsInWorkspace(ctx, workspaceID, skillIDs); err != nil {
		return nil, err
	}
	for _, id := range uniqueStrings(skillIDs) {
		if _, err := s.db.Pool.Exec(ctx, `INSERT INTO operative_capabilities (operative_id, capability_id, opcap_enabled)
			VALUES ($1,$2,true) ON CONFLICT (operative_id, capability_id) DO NOTHING`, agentID, id); err != nil {
			return nil, fmt.Errorf("agent: добавление навыков: %w", err)
		}
	}
	return s.ListAgentSkills(ctx, agentID)
}

// SetAgentSkillEnabled — вкл/выкл конкретную привязку.
func (s *Store) SetAgentSkillEnabled(ctx context.Context, agentID, skillID string, enabled bool) ([]SkillSummary, error) {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE operative_capabilities SET opcap_enabled = $3, updated_at = now()
		WHERE operative_id = $1 AND capability_id = $2`, agentID, skillID, enabled)
	if err != nil {
		return nil, fmt.Errorf("agent: переключение навыка: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrSkillNotAttached
	}
	return s.ListAgentSkills(ctx, agentID)
}

// RemoveAgentSkill — отвязать.
func (s *Store) RemoveAgentSkill(ctx context.Context, agentID, skillID string) ([]SkillSummary, error) {
	if _, err := s.db.Pool.Exec(ctx, `DELETE FROM operative_capabilities WHERE operative_id = $1 AND capability_id = $2`,
		agentID, skillID); err != nil {
		return nil, fmt.Errorf("agent: отвязка навыка: %w", err)
	}
	return s.ListAgentSkills(ctx, agentID)
}

// --- runtime (встроенные) навыки ------------------------------------------

// ListDisabledRuntimeSkills — disabled_runtime_skills агента.
func (s *Store) ListDisabledRuntimeSkills(ctx context.Context, agentID string) ([]DisabledRuntimeSkill, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT executor_id, opdis_provider, opdis_root, opdis_key, opdis_title, opdis_plugin
		FROM operative_disabled_local_skills WHERE operative_id = $1 ORDER BY created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent: встроенные навыки: %w", err)
	}
	defer rows.Close()
	out := []DisabledRuntimeSkill{}
	for rows.Next() {
		var d DisabledRuntimeSkill
		var title *string
		if err := rows.Scan(&d.RuntimeID, &d.Provider, &d.Root, &d.Key, &title, &d.Plugin); err != nil {
			return nil, err
		}
		d.Name = strOr(title, "")
		out = append(out, d)
	}
	return out, rows.Err()
}

// ErrRuntimeMismatch — агент больше не привязан к указанному runtime_id (409).
var ErrRuntimeMismatch = errors.New("agent: агент не привязан к этому runtime")

// SetRuntimeSkillEnabled — вкл/выкл встроенный навык рантайма
// (setAgentRuntimeSkillEnabled): enabled=false добавляет запись в
// operative_disabled_local_skills, enabled=true удаляет её (по умолчанию
// навык включён — отсутствие строки и есть "включено"). provider — из
// executors.ex_provider текущего runtime (запрос контракта не несёт его
// явно, только root/key/name/plugin — provider выводится сервером).
func (s *Store) SetRuntimeSkillEnabled(ctx context.Context, agentID, currentExecutorID, runtimeID, provider, root, key, name, plugin string, enabled bool) error {
	if runtimeID != currentExecutorID {
		return ErrRuntimeMismatch
	}
	if enabled {
		_, err := s.db.Pool.Exec(ctx, `DELETE FROM operative_disabled_local_skills
			WHERE operative_id = $1 AND executor_id = $2 AND opdis_root = $3 AND opdis_key = $4`,
			agentID, runtimeID, root, key)
		if err != nil {
			return fmt.Errorf("agent: включение навыка рантайма: %w", err)
		}
		return nil
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO operative_disabled_local_skills (operative_id, executor_id, opdis_provider, opdis_root, opdis_key, opdis_title, opdis_plugin)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''))
		ON CONFLICT (operative_id, executor_id, opdis_root, opdis_key) DO NOTHING`,
		agentID, runtimeID, provider, root, key, name, plugin)
	if err != nil {
		return fmt.Errorf("agent: выключение навыка рантайма: %w", err)
	}
	return nil
}

// ClearDisabledRuntimeSkillsForOtherExecutor — при переносе агента на другой
// runtime (contract §10.4: "переоценивается заново при смене runtime_id")
// список выключенных навыков прежнего рантайма больше не имеет смысла.
func (s *Store) ClearDisabledRuntimeSkillsForOtherExecutor(ctx context.Context, agentID, newExecutorID string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM operative_disabled_local_skills
		WHERE operative_id = $1 AND executor_id != $2`, agentID, newExecutorID)
	if err != nil {
		return fmt.Errorf("agent: сброс навыков рантайма при смене runtime: %w", err)
	}
	return nil
}
