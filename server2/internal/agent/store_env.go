package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const maskMarker = "****"

// customEnv расшифровывает op_custom_env_sealed в map[string]string; ok=false
// значит сохранённое значение зашифровано, но ни ключ, ни его предыдущая
// версия не подошли (contract §10.3: тогда чтение честно ничего не может
// показать — вызывающий код решает, как на это реагировать).
func (s *Store) customEnv(sealed []byte) (map[string]string, bool) {
	if len(sealed) == 0 {
		return map[string]string{}, true
	}
	out := map[string]string{}
	ok := openJSON(s.cryptoKey, s.cryptoKeyPrev, sealed, &out)
	if !ok {
		return nil, false
	}
	return out, true
}

// CustomEnvKeys — только имена ключей (видны всегда, независимо от прав) и
// признак, удалось ли вообще прочитать блок (has_custom_env считает даже
// нерасшифровываемый блок как "есть", раз байты в колонке есть).
func (s *Store) CustomEnvKeys(a Agent) (keys []string, decodable bool) {
	if len(a.CustomEnvSeal) == 0 {
		return []string{}, true
	}
	env, ok := s.customEnv(a.CustomEnvSeal)
	if !ok {
		return []string{}, false
	}
	keys = make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	return keys, true
}

// CustomEnvValues — значения в открытом виде (только для владельца/при
// настройке "всегда раскрывать секреты").
func (s *Store) CustomEnvValues(a Agent) (map[string]string, bool) {
	return s.customEnv(a.CustomEnvSeal)
}

// EnvDiff — списки добавленных/изменённых/удалённых/сохранённых ключей для
// аудита updateAgentEnv (contract §10.3).
type EnvDiff struct {
	Added   []string
	Changed []string
	Removed []string
	Kept    []string
}

// ApplyCustomEnv реализует updateAgentEnv: incoming — новое желаемое
// состояние (маркеры **** уже разрешены вызывающим кодом только для
// не-владельца — здесь просто "оставить как было" для этого ключа).
// mask==true означает "значение этого ключа — маркер, взять текущее".
func (s *Store) ApplyCustomEnv(ctx context.Context, workspaceID, agentID string, current map[string]string, incoming map[string]string) (map[string]string, EnvDiff, error) {
	final := map[string]string{}
	var diff EnvDiff
	for k, v := range incoming {
		if v == maskMarker {
			if cur, ok := current[k]; ok {
				final[k] = cur
				diff.Kept = append(diff.Kept, k)
			}
			continue
		}
		final[k] = v
		if cur, ok := current[k]; ok {
			if cur != v {
				diff.Changed = append(diff.Changed, k)
			} else {
				diff.Kept = append(diff.Kept, k)
			}
		} else {
			diff.Added = append(diff.Added, k)
		}
	}
	for k := range current {
		if _, ok := incoming[k]; !ok {
			diff.Removed = append(diff.Removed, k)
		}
	}

	var sealed []byte
	if len(final) > 0 {
		var err error
		sealed, _, err = sealJSON(s.cryptoKey, final)
		if err != nil {
			return nil, EnvDiff{}, fmt.Errorf("agent: шифрование окружения: %w", err)
		}
	}
	if _, err := s.db.Pool.Exec(ctx, `UPDATE operatives SET op_custom_env_sealed = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2`, workspaceID, agentID, nullableBytes(sealed)); err != nil {
		return nil, EnvDiff{}, fmt.Errorf("agent: сохранение окружения: %w", err)
	}
	return final, diff, nil
}

// BrokenMaskValue — значение начинается с **** но не равно ему целиком
// (contract §10.3: типичная опечатка поверх маски, отклоняется 400).
func BrokenMaskValue(v string) bool {
	return v != maskMarker && strings.HasPrefix(v, maskMarker)
}

// --- аудит доступа к секретам агента ---------------------------------------
//
// recordAudit пишет строку platform_audit_log (011_governance.up.sql) для
// событий §10.3: "любое чтение переменных окружения... обязательно пишет
// запись в журнал активности воркспейса; если запись не удалась — 500".
// paud_source переиспользует уже существующее значение 'admin' (единственная
// таблица аудита в схеме на момент этой сессии — governance/T-029 её ещё не
// создал; решение зафиксировано в server2/docs/decisions.md, раздел T-028):
// 'agent_env_*' — семейство "администрирования ресурса", тот же смысл, что
// остальные записи paud_source=admin.
func (s *Store) recordAudit(ctx context.Context, workspaceID, actorAccountID, action, targetID string, details map[string]any) error {
	var detailsJSON []byte
	if len(details) > 0 {
		var err error
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			return err
		}
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO platform_audit_log (paud_source, paud_action, paud_actor_account_id, paud_actor_type,
			paud_target_type, paud_target_id, paud_outcome, workspace_id, paud_reason)
		VALUES ('admin', $1, NULLIF($2,'')::uuid, 'human', 'agent', $3, 'ok', $4, $5)`,
		action, actorAccountID, targetID, workspaceID, string(detailsJSON))
	if err != nil {
		return fmt.Errorf("agent: запись аудита: %w", err)
	}
	return nil
}

// AuditEnvRead — getAgentEnv, до/после которого маскировано или раскрыто.
func (s *Store) AuditEnvRead(ctx context.Context, workspaceID, actorAccountID, agentID string, valuesMasked bool) error {
	return s.recordAudit(ctx, workspaceID, actorAccountID, "agent_env_read", agentID, map[string]any{"values_masked": valuesMasked})
}

// AuditEnvUpdate — updateAgentEnv успешный (списки ключей diff).
func (s *Store) AuditEnvUpdate(ctx context.Context, workspaceID, actorAccountID, agentID string, diff EnvDiff) error {
	return s.recordAudit(ctx, workspaceID, actorAccountID, "agent_env_update", agentID, map[string]any{
		"added": diff.Added, "changed": diff.Changed, "removed": diff.Removed, "kept": diff.Kept,
	})
}

// AuditEnvUpdateRefused — не-владелец попытался реально изменить значение.
func (s *Store) AuditEnvUpdateRefused(ctx context.Context, workspaceID, actorAccountID, agentID string) error {
	return s.recordAudit(ctx, workspaceID, actorAccountID, "agent_env_update_refused", agentID, nil)
}
