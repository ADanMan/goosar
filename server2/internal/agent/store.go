package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// ErrNotFound — агент не существует в этом воркспейсе.
var ErrNotFound = errors.New("agent: не найден")

// ErrNameTaken — имя агента уже занято в воркспейсе (409).
var ErrNameTaken = errors.New("agent: имя уже используется в воркспейсе")

// Store — доступ к operatives и связанным таблицам (003_agents.up.sql).
type Store struct {
	db            *store.Store
	cryptoKey     string
	cryptoKeyPrev string
}

func NewStore(db *store.Store, cryptoKey, cryptoKeyPrev string) *Store {
	return &Store{db: db, cryptoKey: cryptoKey, cryptoKeyPrev: cryptoKeyPrev}
}

const agentColumns = `id, workspace_id, executor_id, op_title, op_summary, op_instructions, op_avatar_uri,
	op_runtime_mode, op_runtime_config_sealed, op_custom_args, op_mcp_config_sealed, op_mcp_config_encrypted,
	op_custom_env_sealed, op_permission_mode, op_status, op_max_concurrent_tasks, op_model, op_thinking_level,
	op_service_tier, op_composio_allowlist, op_owner_account_id, op_kind, op_system_key, op_archived_at,
	op_archived_by, created_at, updated_at`

func scanAgent(row pgx.Row) (Agent, error) {
	var a Agent
	var composio []byte
	// op_summary/op_model/op_thinking_level/op_service_tier — nullable text
	// (в отличие от op_instructions/op_runtime_mode/op_permission_mode/
	// op_status/op_kind, у которых есть NOT NULL DEFAULT в 003_agents.up.sql);
	// сканируются через *string, иначе pgx отказывает на NULL ("cannot scan
	// NULL into *string").
	var summary, model, thinkingLevel, serviceTier *string
	if err := row.Scan(&a.ID, &a.WorkspaceID, &a.ExecutorID, &a.Title, &summary, &a.Instructions, &a.AvatarURI,
		&a.RuntimeMode, &a.RuntimeConfigSeal, &a.CustomArgs, &a.McpConfigSeal, &a.McpConfigEncrypted,
		&a.CustomEnvSeal, &a.PermissionMode, &a.Status, &a.MaxConcurrentTasks, &model, &thinkingLevel,
		&serviceTier, &composio, &a.OwnerAccountID, &a.Kind, &a.SystemKey, &a.ArchivedAt,
		&a.ArchivedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return Agent{}, err
	}
	a.Summary = strOr(summary, "")
	a.Model = strOr(model, "")
	a.ThinkingLevel = strOr(thinkingLevel, "")
	a.ServiceTier = strOr(serviceTier, "")
	if len(composio) > 0 {
		a.ComposioAllowlist = composio
	}
	return a, nil
}

// Get — один агент по id, в пределах воркспейса.
func (s *Store) Get(ctx context.Context, workspaceID, id string) (Agent, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+agentColumns+` FROM operatives WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id)
	a, err := scanAgent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("agent: получение агента: %w", err)
	}
	return a, nil
}

// List — все агенты воркспейса (include_archived решает вызывающий фильтром
// видимости, здесь просто присутствие/отсутствие условия по op_archived_at).
func (s *Store) List(ctx context.Context, workspaceID string, includeArchived bool) ([]Agent, error) {
	q := `SELECT ` + agentColumns + ` FROM operatives WHERE workspace_id = $1`
	if !includeArchived {
		q += ` AND op_archived_at IS NULL`
	}
	q += ` ORDER BY created_at ASC`
	rows, err := s.db.Pool.Query(ctx, q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("agent: список агентов: %w", err)
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// NameTaken — есть ли уже агент с этим именем в воркспейсе (409 у
// create/update), кроме самого excludeID (пустая строка — без исключения).
func (s *Store) NameTaken(ctx context.Context, workspaceID, name, excludeID string) (bool, error) {
	// NULLIF(...)::uuid — excludeID пуст при create (нет строки-исключения);
	// сравнение id != '' напрямую отклоняется Postgres на этапе типизации
	// параметра ("invalid input syntax for type uuid"), не на этапе данных.
	return s.db.RowExists(ctx, `SELECT EXISTS(
		SELECT 1 FROM operatives WHERE workspace_id = $1 AND op_title = $2
			AND id != COALESCE(NULLIF($3,'')::uuid, '00000000-0000-0000-0000-000000000000'::uuid))`,
		workspaceID, name, excludeID)
}

// CreateParams — вход Create; поля соответствуют CreateAgentRequest после
// того, как handlers.go уже проверил runtime/полномочия.
type CreateParams struct {
	WorkspaceID        string
	ExecutorID         string
	Title              string
	Summary            string
	Instructions       string
	AvatarURI          *string
	RuntimeMode        string
	RuntimeConfig      map[string]any
	CustomArgs         []string
	McpConfig          map[string]any
	CustomEnv          map[string]string
	PermissionMode     string
	InvocationTargets  []InvocationTarget
	MaxConcurrentTasks int
	Model              string
	ThinkingLevel      string
	ServiceTier        string
	ComposioAllowlist  []string
	OwnerAccountID     string
	Kind               string // "user" (по умолчанию) | "system"
	SystemKey          string
}

// Create вставляет новый агент, его operative_targets и, если заданы
// skill_ids (обрабатывается вызывающим кодом отдельным вызовом
// SetSkills), не трогает капабилити здесь.
func (s *Store) Create(ctx context.Context, p CreateParams) (Agent, error) {
	if p.RuntimeMode == "" {
		p.RuntimeMode = "local"
	}
	if p.MaxConcurrentTasks == 0 {
		p.MaxConcurrentTasks = 6
	}
	if p.PermissionMode == "" {
		p.PermissionMode = "private"
	}
	if p.Kind == "" {
		p.Kind = "user"
	}
	customArgsJSON, _ := json.Marshal(p.CustomArgs)
	if p.CustomArgs == nil {
		customArgsJSON = []byte(`[]`)
	}

	runtimeSealed, _, err := sealJSON(s.cryptoKey, orEmptyObject(p.RuntimeConfig))
	if err != nil {
		return Agent{}, fmt.Errorf("agent: шифрование runtime_config: %w", err)
	}
	var mcpSealed []byte
	var mcpEncrypted bool
	if p.McpConfig != nil {
		mcpSealed, mcpEncrypted, err = sealJSON(s.cryptoKey, p.McpConfig)
		if err != nil {
			return Agent{}, fmt.Errorf("agent: шифрование mcp_config: %w", err)
		}
	}
	var envSealed []byte
	if len(p.CustomEnv) > 0 {
		envSealed, _, err = sealJSON(s.cryptoKey, p.CustomEnv)
		if err != nil {
			return Agent{}, fmt.Errorf("agent: шифрование custom_env: %w", err)
		}
	}
	var composioJSON []byte
	if p.ComposioAllowlist != nil {
		composioJSON, _ = json.Marshal(p.ComposioAllowlist)
	}

	var id string
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO operatives (
			workspace_id, executor_id, op_title, op_summary, op_instructions, op_avatar_uri,
			op_runtime_mode, op_runtime_config_sealed, op_custom_args, op_mcp_config_sealed, op_mcp_config_encrypted,
			op_custom_env_sealed, op_permission_mode, op_max_concurrent_tasks, op_model, op_thinking_level,
			op_service_tier, op_composio_allowlist, op_owner_account_id, op_kind, op_system_key
		) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''),NULLIF($16,''),
			NULLIF($17,''),$18,NULLIF($19,'')::uuid,$20,NULLIF($21,''))
		RETURNING id`,
		p.WorkspaceID, p.ExecutorID, p.Title, p.Summary, p.Instructions, p.AvatarURI,
		p.RuntimeMode, runtimeSealed, customArgsJSON, nullableBytes(mcpSealed), mcpEncrypted,
		nullableBytes(envSealed), p.PermissionMode, p.MaxConcurrentTasks, p.Model, p.ThinkingLevel,
		p.ServiceTier, nullableBytes(composioJSON), p.OwnerAccountID, p.Kind, p.SystemKey,
	).Scan(&id)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return Agent{}, ErrNameTaken
		}
		return Agent{}, fmt.Errorf("agent: создание агента: %w", err)
	}
	if len(p.InvocationTargets) > 0 {
		if err := s.replaceTargets(ctx, id, p.InvocationTargets); err != nil {
			return Agent{}, err
		}
	}
	return s.Get(ctx, p.WorkspaceID, id)
}

// SaveCore обновляет все "простые" (не требующие отдельного маршрута)
// колонки агента разом — вызывается updateAgent (handlers.go) после того,
// как он смержил CurrentAgent с UpdateAgentRequest по правилам §10.2.
func (s *Store) SaveCore(ctx context.Context, a Agent) error {
	customArgs := rawOr(a.CustomArgs, emptyArray())
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE operatives SET
			executor_id = $3, op_title = $4, op_summary = NULLIF($5,''), op_instructions = $6, op_avatar_uri = $7,
			op_runtime_mode = $8, op_runtime_config_sealed = $9, op_custom_args = $10,
			op_mcp_config_sealed = $11, op_mcp_config_encrypted = $12, op_custom_env_sealed = $13,
			op_permission_mode = $14, op_status = $15, op_max_concurrent_tasks = $16, op_model = NULLIF($17,''),
			op_thinking_level = NULLIF($18,''), op_service_tier = NULLIF($19,''), op_composio_allowlist = $20,
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2`,
		a.WorkspaceID, a.ID, a.ExecutorID, a.Title, a.Summary, a.Instructions, a.AvatarURI,
		a.RuntimeMode, a.RuntimeConfigSeal, customArgs, nullableBytes(a.McpConfigSeal), a.McpConfigEncrypted,
		nullableBytes(a.CustomEnvSeal), a.PermissionMode, a.Status, a.MaxConcurrentTasks, a.Model,
		a.ThinkingLevel, a.ServiceTier, nullableBytes(a.ComposioAllowlist),
	)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return ErrNameTaken
		}
		return fmt.Errorf("agent: обновление агента: %w", err)
	}
	return nil
}

func (s *Store) replaceTargets(ctx context.Context, agentID string, targets []InvocationTarget) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM operative_targets WHERE operative_id = $1`, agentID); err != nil {
			return err
		}
		for _, t := range targets {
			if _, err := tx.Exec(ctx, `INSERT INTO operative_targets (operative_id, opt_target_type, opt_target_id)
				VALUES ($1,$2,$3)`, agentID, t.TargetType, t.TargetID); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetInvocationTargets — публичный вход для updateAgent (замена набора
// целей вызова целиком; пустой список — снять все target-ограничения).
func (s *Store) SetInvocationTargets(ctx context.Context, agentID string, targets []InvocationTarget) error {
	return s.replaceTargets(ctx, agentID, targets)
}

// InvocationTargets — текущие цели вызова агента.
func (s *Store) InvocationTargets(ctx context.Context, agentID string) ([]InvocationTarget, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT opt_target_type, opt_target_id FROM operative_targets
		WHERE operative_id = $1 ORDER BY created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent: цели вызова: %w", err)
	}
	defer rows.Close()
	out := []InvocationTarget{}
	for rows.Next() {
		var t InvocationTarget
		if err := rows.Scan(&t.TargetType, &t.TargetID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Archive помечает агента архивированным. found=false — уже был архивирован
// (409 у вызывающего).
func (s *Store) Archive(ctx context.Context, workspaceID, id, byAccountID string) (bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE operatives SET op_archived_at = now(), op_archived_by = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND op_archived_at IS NULL`, workspaceID, id, byAccountID)
	if err != nil {
		return false, fmt.Errorf("agent: архивация: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Restore снимает архивацию. found=false — агент не был архивирован (409).
func (s *Store) Restore(ctx context.Context, workspaceID, id string) (bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE operatives SET op_archived_at = NULL, op_archived_by = NULL, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND op_archived_at IS NOT NULL`, workspaceID, id)
	if err != nil {
		return false, fmt.Errorf("agent: восстановление: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// runtimeRef — то немногое об executors, что нужно agent-домену для
// проверки доступности рантайма при create/update/skills-переоценке.
type runtimeRef struct {
	ID         string
	Status     string
	Visibility string
	Provider   string
	OwnerID    *string
	LastSeenAt *time.Time
}

func (s *Store) getRuntime(ctx context.Context, workspaceID, id string) (runtimeRef, bool, error) {
	var r runtimeRef
	err := s.db.Pool.QueryRow(ctx, `SELECT id, ex_status, ex_visibility, ex_provider, ex_owner_account_id, ex_last_seen_at
		FROM executors WHERE workspace_id = $1 AND id = $2`, workspaceID, id).
		Scan(&r.ID, &r.Status, &r.Visibility, &r.Provider, &r.OwnerID, &r.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeRef{}, false, nil
	}
	if err != nil {
		return runtimeRef{}, false, fmt.Errorf("agent: получение runtime: %w", err)
	}
	return r, true, nil
}

// runtimeAccessible — приватный рантайм доступен только владельцу/владельцу
// или админу воркспейса (contract createAgent: "приватный рантайм — только
// его владельцу или админу").
func runtimeAccessible(r runtimeRef, actorID string, isOwnerOrAdmin bool) bool {
	if r.Visibility == "public" {
		return true
	}
	if isOwnerOrAdmin {
		return true
	}
	return r.OwnerID != nil && *r.OwnerID == actorID
}

// RuntimeAccessible — экспортированная проверка доступности runtime для
// вызывающего (contract §10.1/agentbuilder §2: "online и публичный либо
// принадлежит вызывающему, либо тот owner/admin"), нужна internal/agentbuilder
// (createAgentBuilderSession/switchAgentBuilderRuntime), которая не имеет
// доступа к неэкспортированному getRuntime этого пакета.
func (s *Store) RuntimeAccessible(ctx context.Context, workspaceID, runtimeID, viewerID string, isOwnerOrAdmin bool) (found, online, accessible bool, err error) {
	rt, found, err := s.getRuntime(ctx, workspaceID, runtimeID)
	if err != nil || !found {
		return found, false, false, err
	}
	return true, rt.Status == "online", runtimeAccessible(rt, viewerID, isOwnerOrAdmin), nil
}

func orEmptyObject(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// --- полномочие на вызов агента ---------------------------------------------

// CanInvoke — «полномочие на вызов агента» (contract §1.8/§10.1), реализация
// каноническая для этого T-028: owner/admin воркспейса — всегда; владелец
// агента — всегда; иначе (только человек-участник) — если permission_mode
// public_to и вызывающий входит в один из invocation_targets (workspace —
// весь воркспейс, member — сам вызывающий, team — цель трактуется как отряд:
// вызывающий состоит участником-человеком в crew_members этого отряда,
// contract не заводит отдельной сущности "team" за пределами
// crews/squads — то же решение, что зафиксировано в internal/chat.CanInvoke
// и internal/task.canInvokeAgent T-027). Экспортирован для internal/squad
// (createSquad/updateSquad: "право использовать агента как лидера") — squad
// импортирует agent, не наоборот.
func (s *Store) CanInvoke(ctx context.Context, a Agent, viewerID string, isHuman bool, role httpapi.Role) (bool, error) {
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true, nil
	}
	if a.OwnerAccountID != nil && isHuman && *a.OwnerAccountID == viewerID {
		return true, nil
	}
	if !isHuman || a.PermissionMode != "public_to" {
		return false, nil
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT opt_target_type, opt_target_id FROM operative_targets WHERE operative_id = $1`, a.ID)
	if err != nil {
		return false, fmt.Errorf("agent: проверка целей вызова: %w", err)
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
			if targetID != nil && *targetID == viewerID {
				return true, nil
			}
		case "team":
			if targetID == nil {
				continue
			}
			var member bool
			err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS(
				SELECT 1 FROM crew_members WHERE crew_id = $1 AND cm_member_type = 'member' AND cm_member_id = $2)`,
				*targetID, viewerID).Scan(&member)
			if err != nil {
				return false, fmt.Errorf("agent: проверка отряда-цели: %w", err)
			}
			if member {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

// CanView — «доступ к приватному агенту» (getAgent/listAgents): то же самое
// полномочие, что CanInvoke — контракт не различает "видеть" и "вызывать"
// приватного агента отдельно (оба защищают одно и то же приватное значение).
func (s *Store) CanView(ctx context.Context, a Agent, viewerID string, isHuman bool, role httpapi.Role) (bool, error) {
	return s.CanInvoke(ctx, a, viewerID, isHuman, role)
}
