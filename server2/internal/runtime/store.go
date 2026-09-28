package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("runtime: не найдено")

// heartbeatTimeout — правка/решение T-028 (см. decisions.md): контракт
// определяет "online"/"offline" только описательно ("недавно присылал
// heartbeat" / "таймаут heartbeat истёк"), не фиксируя число — выбрано с
// запасом над типичным интервалом heartbeat демона (обычно 20-30с).
const heartbeatTimeout = 90 * time.Second

// Executor — строка `executors` (Runtime/AgentRuntime контракта).
type Executor struct {
	ID           string
	WorkspaceID  string
	DaemonID     *string
	Title        string
	CustomTitle  *string
	Mode         string
	Provider     string
	LaunchHeader *string
	StoredStatus string
	DeviceInfo   string
	Metadata     json.RawMessage
	OwnerID      *string
	Visibility   string
	ProfileID    *string
	LastSeenAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Status — статус, вычисленный на момент чтения (см. heartbeatTimeout).
func (e Executor) Status() string {
	if e.StoredStatus == "online" && e.LastSeenAt != nil && time.Since(*e.LastSeenAt) < heartbeatTimeout {
		return "online"
	}
	return "offline"
}

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

const executorColumns = `id, workspace_id, ex_daemon_id, ex_title, ex_custom_title, ex_mode, ex_provider,
	ex_launch_header, ex_status, ex_device_info, ex_metadata, ex_owner_account_id, ex_visibility,
	ex_protocol_id, ex_last_seen_at, created_at, updated_at`

func scanExecutor(row pgx.Row) (Executor, error) {
	var e Executor
	var metadata []byte
	if err := row.Scan(&e.ID, &e.WorkspaceID, &e.DaemonID, &e.Title, &e.CustomTitle, &e.Mode, &e.Provider,
		&e.LaunchHeader, &e.StoredStatus, &e.DeviceInfo, &metadata, &e.OwnerID, &e.Visibility,
		&e.ProfileID, &e.LastSeenAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return Executor{}, err
	}
	if len(metadata) > 0 {
		e.Metadata = metadata
	} else {
		e.Metadata = json.RawMessage(`{}`)
	}
	return e, nil
}

func collectExecutors(rows pgx.Rows) ([]Executor, error) {
	defer rows.Close()
	var out []Executor
	for rows.Next() {
		e, err := scanExecutor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RegisterEntry — один элемент DaemonRegisterRequest.runtimes.
type RegisterEntry struct {
	Name      string
	Type      string // provider
	Version   string
	Status    string // online/offline, по умолчанию online
	ProfileID string
}

// Register апсертит рантаймы одного daemon_id воркспейса: естественный ключ
// "уже видели этот daemon_id+provider в этом воркспейсе" (контракт не даёт
// executors отдельного id в DaemonRegisterRequest — демон присылает список
// заново при каждом старте), решение зафиксировано в decisions.md.
func (s *Store) Register(ctx context.Context, workspaceID, daemonID, ownerID, deviceInfo string, entries []RegisterEntry) ([]Executor, error) {
	out := make([]Executor, 0, len(entries))
	for _, e := range entries {
		status := e.Status
		if status == "" {
			status = "online"
		}
		var existingID string
		err := s.db.Pool.QueryRow(ctx, `
			SELECT id FROM executors WHERE workspace_id = $1 AND ex_daemon_id = $2 AND ex_provider = $3`,
			workspaceID, daemonID, e.Type).Scan(&existingID)
		switch {
		case store.IsNoRows(err):
			row := s.db.Pool.QueryRow(ctx, `
				INSERT INTO executors (workspace_id, ex_daemon_id, ex_title, ex_mode, ex_provider,
					ex_status, ex_device_info, ex_owner_account_id, ex_protocol_id, ex_last_seen_at)
				VALUES ($1, $2, $3, 'local', $4, $5, $6, NULLIF($7,'')::uuid, NULLIF($8,'')::uuid, now())
				RETURNING `+executorColumns,
				workspaceID, daemonID, e.Name, e.Type, status, deviceInfo, ownerID, e.ProfileID)
			ex, scanErr := scanExecutor(row)
			if scanErr != nil {
				return nil, fmt.Errorf("runtime: регистрация рантайма: %w", scanErr)
			}
			out = append(out, ex)
		case err != nil:
			return nil, fmt.Errorf("runtime: поиск существующего рантайма: %w", err)
		default:
			row := s.db.Pool.QueryRow(ctx, `
				UPDATE executors SET ex_title = $2, ex_status = $3, ex_device_info = $4, ex_last_seen_at = now(),
					ex_protocol_id = COALESCE(NULLIF($5,'')::uuid, ex_protocol_id), updated_at = now()
				WHERE id = $1 RETURNING `+executorColumns,
				existingID, e.Name, status, deviceInfo, e.ProfileID)
			ex, scanErr := scanExecutor(row)
			if scanErr != nil {
				return nil, fmt.Errorf("runtime: обновление рантайма: %w", scanErr)
			}
			out = append(out, ex)
		}
	}
	return out, nil
}

// Deregister помечает переданные id offline; чужие/неизвестные молча
// пропускаются (WHERE фильтрует по workspaceID, когда он известен — human
// actor; при daemon/task-token actor вызывающий уже ограничил список только
// своим воркспейсом на уровне обработчика).
func (s *Store) Deregister(ctx context.Context, ids []string) ([]Executor, error) {
	rows, err := s.db.Pool.Query(ctx, `
		UPDATE executors SET ex_status = 'offline', updated_at = now()
		WHERE id = ANY($1) RETURNING `+executorColumns, ids)
	if err != nil {
		return nil, fmt.Errorf("runtime: деактивация рантаймов: %w", err)
	}
	return collectExecutors(rows)
}

// Heartbeat обновляет last_seen_at/статус одного рантайма.
func (s *Store) Heartbeat(ctx context.Context, id string) (Executor, bool, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE executors SET ex_status = 'online', ex_last_seen_at = now(), updated_at = now()
		WHERE id = $1 RETURNING `+executorColumns, id)
	ex, err := scanExecutor(row)
	if store.IsNoRows(err) {
		return Executor{}, false, nil
	}
	if err != nil {
		return Executor{}, false, fmt.Errorf("runtime: heartbeat: %w", err)
	}
	return ex, true, nil
}

// Get — один рантайм по id.
func (s *Store) Get(ctx context.Context, id string) (Executor, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+executorColumns+` FROM executors WHERE id = $1`, id)
	ex, err := scanExecutor(row)
	if store.IsNoRows(err) {
		return Executor{}, ErrNotFound
	}
	if err != nil {
		return Executor{}, fmt.Errorf("runtime: получение рантайма: %w", err)
	}
	return ex, nil
}

// ListForWorkspace — GET /api/runtimes. ownerOnly сужает до ex_owner_account_id
// = вызывающий (?owner=me); non-owner/admin участник видит только public
// runtime плюс свои приватные (contract §5 "visibility").
func (s *Store) ListForWorkspace(ctx context.Context, workspaceID string, ownerOnly string, callerID string, isOwnerOrAdmin bool) ([]Executor, error) {
	q := `SELECT ` + executorColumns + ` FROM executors WHERE workspace_id = $1`
	args := []any{workspaceID}
	if ownerOnly != "" {
		q += ` AND ex_owner_account_id = $2`
		args = append(args, callerID)
	} else if !isOwnerOrAdmin {
		q += ` AND (ex_visibility = 'public' OR ex_owner_account_id = $2)`
		args = append(args, callerID)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("runtime: список рантаймов: %w", err)
	}
	return collectExecutors(rows)
}

// ListByDaemon — все рантаймы физической машины (для claim батчем/apply_to_machine).
func (s *Store) ListByDaemon(ctx context.Context, workspaceID, daemonID string) ([]Executor, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+executorColumns+` FROM executors WHERE workspace_id = $1 AND ex_daemon_id = $2`,
		workspaceID, daemonID)
	if err != nil {
		return nil, fmt.Errorf("runtime: рантаймы машины: %w", err)
	}
	return collectExecutors(rows)
}

// UpdateVisibilityName — PATCH /api/runtimes/{id}. customName == nil — не менять,
// пустая строка — сбросить на дефолт (ex_custom_title = NULL).
func (s *Store) UpdateVisibilityName(ctx context.Context, id string, visibility *string, customName *string) (Executor, error) {
	var customArg any
	hasCustom := customName != nil
	if hasCustom {
		if *customName == "" {
			customArg = nil
		} else {
			customArg = *customName
		}
	}
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE executors SET
			ex_visibility = COALESCE($2, ex_visibility),
			ex_custom_title = CASE WHEN $3 THEN $4 ELSE ex_custom_title END,
			updated_at = now()
		WHERE id = $1 RETURNING `+executorColumns,
		id, visibility, hasCustom, customArg)
	ex, err := scanExecutor(row)
	if store.IsNoRows(err) {
		return Executor{}, ErrNotFound
	}
	if err != nil {
		return Executor{}, fmt.Errorf("runtime: обновление рантайма: %w", err)
	}
	return ex, nil
}

// ApplyNameToMachine — apply_to_machine=true: тот же custom_name всем
// рантаймам daemonID воркспейса; restrictOwnerID (не nil) ограничивает
// применение только рантаймами этого владельца (рядовой участник).
func (s *Store) ApplyNameToMachine(ctx context.Context, workspaceID, daemonID string, customName string, restrictOwnerID *string) error {
	var arg any
	if customName == "" {
		arg = nil
	} else {
		arg = customName
	}
	if restrictOwnerID != nil {
		_, err := s.db.Pool.Exec(ctx, `
			UPDATE executors SET ex_custom_title = $4, updated_at = now()
			WHERE workspace_id = $1 AND ex_daemon_id = $2 AND ex_owner_account_id = $3`,
			workspaceID, daemonID, *restrictOwnerID, arg)
		return err
	}
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE executors SET ex_custom_title = $3, updated_at = now()
		WHERE workspace_id = $1 AND ex_daemon_id = $2`, workspaceID, daemonID, arg)
	return err
}

// ActiveAgent — операция удаления/архивации отдаёт минимум полей активного
// агента, достаточный для тела 409 runtime_has_active_agents (контракт:
// "элементы схемы Agent", полную форму строит домен agent — здесь только то,
// что известно этому домену без его зависимостей).
type ActiveAgent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ActiveAgentsOn — неархивные агенты рантайма.
func (s *Store) ActiveAgentsOn(ctx context.Context, executorID string) ([]ActiveAgent, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, op_title FROM operatives WHERE executor_id = $1 AND op_archived_at IS NULL`, executorID)
	if err != nil {
		return nil, fmt.Errorf("runtime: активные агенты рантайма: %w", err)
	}
	defer rows.Close()
	var out []ActiveAgent
	for rows.Next() {
		var a ActiveAgent
		if err := rows.Scan(&a.ID, &a.Title); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ActiveCrewsWithArchivedLeaderOn — есть ли неархивные отряды, чей лидер —
// уже архивный агент этого рантайма (contract "удаление... 409, если есть
// активные отряды, у которых лидер — уже архивный агент этого runtime").
func (s *Store) ActiveCrewsWithArchivedLeaderOn(ctx context.Context, executorID string) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM crews cr
			JOIN operatives op ON op.id = cr.crew_leader_id AND cr.crew_leader_type = 'agent'
			WHERE op.executor_id = $1 AND op.op_archived_at IS NOT NULL AND cr.crew_archived_at IS NULL
		)`, executorID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("runtime: проверка активных отрядов: %w", err)
	}
	return exists, nil
}

// ArchiveAgents архивирует переданные (уже неархивные) агенты рантайма,
// возвращает архивированные ActiveAgent (для события agent:archived на
// уровне обработчика — этот пакет не публикует realtime сам, чтобы не
// зависеть от формы полного Agent).
func (s *Store) ArchiveAgents(ctx context.Context, tx pgx.Tx, executorID string, agentIDs []string, archivedBy string) ([]ActiveAgent, error) {
	rows, err := tx.Query(ctx, `
		UPDATE operatives SET op_archived_at = now(), op_archived_by = $3, updated_at = now()
		WHERE executor_id = $1 AND id = ANY($2) AND op_archived_at IS NULL
		RETURNING id, op_title`, executorID, agentIDs, archivedBy)
	if err != nil {
		return nil, fmt.Errorf("runtime: архивация агентов: %w", err)
	}
	defer rows.Close()
	var out []ActiveAgent
	for rows.Next() {
		var a ActiveAgent
		if err := rows.Scan(&a.ID, &a.Title); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteCascade — упрощённая версия каскада contract §5 "Удаление runtime":
// приостанавливает автопилоты, чьи исполнители — архивные агенты этого
// рантайма, снимает лидерство отрядов (дочерние строки — по существующим
// ON DELETE CASCADE FK на operative_id: operative_capabilities/
// operative_mcp_links/operative_targets/operative_tag_links/
// convo_pinned_operatives/operative_disabled_local_skills), удаляет
// архивные агенты рантайма и сам рантайм. Вызывающий обязан заранее
// убедиться (ActiveAgentsOn/ActiveCrewsWithArchivedLeaderOn), что
// препятствий нет — эта функция сама препятствия не проверяет повторно.
func (s *Store) DeleteCascade(ctx context.Context, executorID string) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE sentinels SET sen_status = 'paused', updated_at = now()
			WHERE sen_assignee_type = 'agent' AND sen_assignee_id IN (
				SELECT id FROM operatives WHERE executor_id = $1 AND op_archived_at IS NOT NULL
			) AND sen_status = 'active'`, executorID); err != nil {
			return fmt.Errorf("runtime: пауза автопилотов: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM crews WHERE crew_leader_type = 'agent' AND crew_leader_id IN (
				SELECT id FROM operatives WHERE executor_id = $1 AND op_archived_at IS NOT NULL
			)`, executorID); err != nil {
			return fmt.Errorf("runtime: удаление отрядов архивных агентов: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM operatives WHERE executor_id = $1 AND op_archived_at IS NOT NULL`, executorID); err != nil {
			return fmt.Errorf("runtime: удаление архивных агентов: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM executors WHERE id = $1`, executorID)
		if err != nil {
			return fmt.Errorf("runtime: удаление рантайма: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// WithTx — обёртка над store.Store.WithTx для обработчиков этого домена
// (archive-agents-and-delete: архивация + каскад в одной транзакции).
func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error { return s.db.WithTx(ctx, fn) }
