// Package autopilot реализует `/api/autopilots/**` контракта (§7):
// CRUD-автопилотов, триггеры (расписание/вебхук/api), ручной запуск, прогоны
// (runs) и вебхук-доставки (deliveries), коллабораторы, и публичный
// `POST /api/webhooks/autopilots/{token}`. Таблицы — 006_sentinels.up.sql
// (sentinels/sentinel_triggers/sentinel_runs/sentinel_subscribers/
// sentinel_collaborators/webhook_events), спроектированные ещё в T-025 —
// эта сессия их не меняет (диапазон миграций 240-259 не понадобился, схема
// уже покрывает весь домен).
//
// Постановка агента в очередь — через server2/internal/dispatch (Enqueue),
// создание задачи в режиме execution_mode=create_issue — через
// server2/internal/task (Store.CreateIssue) + server2/internal/workspace
// (нумерация тикета); ни один из них этой сессией не менялся сверх того,
// что уже документировано в server2/docs/decisions.md.
package autopilot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("autopilot: не найдено")

// Store — доступ к sentinels и связанным таблицам 006_sentinels.up.sql.
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

const autopilotColumns = `s.id, s.workspace_id, s.sen_title, s.sen_summary, s.initiative_id,
	s.sen_assignee_type, s.sen_assignee_id, s.sen_status, s.sen_execution_mode, s.sen_issue_title_template,
	s.sen_created_by_type, s.sen_created_by_id, s.sen_last_run_at, s.sen_is_template, s.created_at, s.updated_at`

func scanAutopilot(row pgx.Row) (Autopilot, error) {
	var a Autopilot
	if err := row.Scan(&a.ID, &a.WorkspaceID, &a.Title, &a.Description, &a.ProjectID,
		&a.AssigneeType, &a.AssigneeID, &a.Status, &a.ExecutionMode, &a.IssueTitleTemplate,
		&a.CreatedByType, &a.CreatedByID, &a.LastRunAt, &a.IsTemplate, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return Autopilot{}, err
	}
	return a, nil
}

// CreateParams — вход Create (createAutopilot).
type CreateParams struct {
	WorkspaceID        string
	Title              string
	Description        *string
	ProjectID          *string
	AssigneeType       string
	AssigneeID         string
	ExecutionMode      string
	IssueTitleTemplate *string
	CreatedByType      string
	CreatedByID        string
	Subscribers        []SubscriberInput
}

// SubscriberInput — components/schemas/SubscriberInput.
type SubscriberInput struct {
	UserType string
	UserID   string
}

// Create вставляет строку sentinels и (если заданы) subscribers, одной
// транзакцией.
func (s *Store) Create(ctx context.Context, p CreateParams) (Autopilot, error) {
	var id string
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO sentinels (
				workspace_id, sen_title, sen_summary, initiative_id, sen_assignee_type, sen_assignee_id,
				sen_execution_mode, sen_issue_title_template, sen_created_by_type, sen_created_by_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
			p.WorkspaceID, p.Title, p.Description, p.ProjectID, p.AssigneeType, p.AssigneeID,
			p.ExecutionMode, p.IssueTitleTemplate, p.CreatedByType, p.CreatedByID)
		if err := row.Scan(&id); err != nil {
			return fmt.Errorf("autopilot: создание автопилота: %w", err)
		}
		return replaceSubscribersTx(ctx, tx, id, p.Subscribers)
	})
	if err != nil {
		return Autopilot{}, err
	}
	return s.Get(ctx, p.WorkspaceID, id)
}

// Get возвращает автопилот целиком, с вычисляемыми полями (trigger_kinds,
// next_run_at, last_run_status, subscribers) — то, что getAutopilot/
// listAutopilots контракта ожидают в форме Autopilot.
func (s *Store) Get(ctx context.Context, workspaceID, id string) (Autopilot, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+autopilotColumns+` FROM sentinels s WHERE s.workspace_id = $1 AND s.id = $2`, workspaceID, id)
	a, err := scanAutopilot(row)
	if store.IsNoRows(err) {
		return Autopilot{}, ErrNotFound
	}
	if err != nil {
		return Autopilot{}, fmt.Errorf("autopilot: получение автопилота: %w", err)
	}
	if err := s.fillComputed(ctx, &a); err != nil {
		return Autopilot{}, err
	}
	return a, nil
}

func (s *Store) fillComputed(ctx context.Context, a *Autopilot) error {
	rows, err := s.db.Pool.Query(ctx, `SELECT DISTINCT strig_kind FROM sentinel_triggers WHERE sentinel_id = $1`, a.ID)
	if err != nil {
		return fmt.Errorf("autopilot: типы триггеров: %w", err)
	}
	var kinds []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		kinds = append(kinds, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	a.TriggerKinds = kinds

	if err := s.db.Pool.QueryRow(ctx, `
		SELECT MIN(strig_next_run_at) FROM sentinel_triggers
		WHERE sentinel_id = $1 AND strig_kind = 'schedule' AND strig_enabled AND strig_next_run_at IS NOT NULL`,
		a.ID).Scan(&a.NextRunAt); err != nil {
		return fmt.Errorf("autopilot: следующий запуск: %w", err)
	}

	if err := s.db.Pool.QueryRow(ctx, `
		SELECT srun_status FROM sentinel_runs WHERE sentinel_id = $1 ORDER BY created_at DESC LIMIT 1`,
		a.ID).Scan(&a.LastRunStatus); err != nil && !store.IsNoRows(err) {
		return fmt.Errorf("autopilot: статус последнего прогона: %w", err)
	}

	subs, err := s.ListSubscribers(ctx, a.ID)
	if err != nil {
		return err
	}
	a.Subscribers = subs
	return nil
}

// List — listAutopilots (без {id}, весь список воркспейса, опционально
// фильтр по статусу).
func (s *Store) List(ctx context.Context, workspaceID string, status *string) ([]Autopilot, int, error) {
	query := `SELECT ` + autopilotColumns + ` FROM sentinels s WHERE s.workspace_id = $1`
	args := []any{workspaceID}
	if status != nil {
		query += ` AND s.sen_status = $2`
		args = append(args, *status)
	}
	query += ` ORDER BY s.created_at DESC`
	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("autopilot: список автопилотов: %w", err)
	}
	defer rows.Close()
	var out []Autopilot
	for rows.Next() {
		a, err := scanAutopilot(rows)
		if err != nil {
			return nil, 0, err
		}
		if err := s.fillComputed(ctx, &a); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, len(out), nil
}

// UpdatePatch — вход Update (updateAutopilot). Каждое поле — пара
// (Xxx, XxxSet): XxxSet=false — ключ не передавался в запросе, значение не
// трогается; XxxSet=true — ключ передан (пусть даже с null-значением для
// nullable-полей) — тот же приём, что task.UpdateFields (internal/task/update.go).
type UpdatePatch struct {
	Title    string
	TitleSet bool

	Description    *string
	DescriptionSet bool

	ProjectID    *string
	ProjectIDSet bool

	AssigneeType string
	AssigneeID   string
	AssigneeSet  bool // assignee_type/assignee_id приходят вместе

	Status    string
	StatusSet bool

	ExecutionMode    string
	ExecutionModeSet bool

	IssueTitleTemplate    *string
	IssueTitleTemplateSet bool

	Subscribers    []SubscriberInput
	SubscribersSet bool // контракт: "если ключ присутствует, полностью заменяет список"
}

// Update применяет патч и возвращает обновлённый автопилот; ErrNotFound —
// если строки с этим id в этом воркспейсе нет.
func (s *Store) Update(ctx context.Context, workspaceID, id string, p UpdatePatch) (Autopilot, error) {
	setClauses, args := []string{}, []any{workspaceID, id}
	add := func(col string, val any) {
		args = append(args, val)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.TitleSet {
		add("sen_title", p.Title)
	}
	if p.DescriptionSet {
		add("sen_summary", p.Description)
	}
	if p.ProjectIDSet {
		add("initiative_id", p.ProjectID)
	}
	if p.AssigneeSet {
		add("sen_assignee_type", p.AssigneeType)
		add("sen_assignee_id", p.AssigneeID)
	}
	if p.StatusSet {
		add("sen_status", p.Status)
	}
	if p.ExecutionModeSet {
		add("sen_execution_mode", p.ExecutionMode)
	}
	if p.IssueTitleTemplateSet {
		add("sen_issue_title_template", p.IssueTitleTemplate)
	}

	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if len(setClauses) > 0 {
			query := "UPDATE sentinels SET " + joinClauses(setClauses) + ", updated_at = now() WHERE workspace_id = $1 AND id = $2"
			tag, err := tx.Exec(ctx, query, args...)
			if err != nil {
				return fmt.Errorf("autopilot: изменение автопилота: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		} else if exists, err := rowExistsTx(ctx, tx, workspaceID, id); err != nil {
			return err
		} else if !exists {
			return ErrNotFound
		}
		if p.SubscribersSet {
			return replaceSubscribersTx(ctx, tx, id, p.Subscribers)
		}
		return nil
	})
	if err != nil {
		return Autopilot{}, err
	}
	return s.Get(ctx, workspaceID, id)
}

func joinClauses(cs []string) string {
	out := cs[0]
	for _, c := range cs[1:] {
		out += ", " + c
	}
	return out
}

func rowExistsTx(ctx context.Context, tx pgx.Tx, workspaceID, id string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sentinels WHERE workspace_id = $1 AND id = $2)`,
		workspaceID, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("autopilot: проверка существования автопилота: %w", err)
	}
	return exists, nil
}

// Archive — deleteAutopilot (мягкое удаление, status -> archived).
func (s *Store) Archive(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE sentinels SET sen_status = 'archived', updated_at = now()
		WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("autopilot: архивирование автопилота: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- subscribers -------------------------------------------------------------

func (s *Store) ListSubscribers(ctx context.Context, autopilotID string) ([]Subscriber, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT account_id, created_at FROM sentinel_subscribers WHERE sentinel_id = $1 ORDER BY created_at`, autopilotID)
	if err != nil {
		return nil, fmt.Errorf("autopilot: список подписчиков: %w", err)
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		var sub Subscriber
		if err := rows.Scan(&sub.UserID, &sub.CreatedAt); err != nil {
			return nil, err
		}
		sub.UserType = "member"
		out = append(out, sub)
	}
	return out, rows.Err()
}

func replaceSubscribersTx(ctx context.Context, tx pgx.Tx, autopilotID string, subs []SubscriberInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM sentinel_subscribers WHERE sentinel_id = $1`, autopilotID); err != nil {
		return fmt.Errorf("autopilot: очистка подписчиков: %w", err)
	}
	for _, sub := range subs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sentinel_subscribers (sentinel_id, account_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, autopilotID, sub.UserID); err != nil {
			return fmt.Errorf("autopilot: запись подписчика: %w", err)
		}
	}
	return nil
}

// --- collaborators -------------------------------------------------------------

func (s *Store) ListCollaborators(ctx context.Context, autopilotID string) ([]Collaborator, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT account_id, sencol_granted_by, created_at FROM sentinel_collaborators
		WHERE sentinel_id = $1 ORDER BY created_at`, autopilotID)
	if err != nil {
		return nil, fmt.Errorf("autopilot: список коллабораторов: %w", err)
	}
	defer rows.Close()
	var out []Collaborator
	for rows.Next() {
		var c Collaborator
		if err := rows.Scan(&c.UserID, &c.GrantedBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.UserType = "member"
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddCollaborator(ctx context.Context, autopilotID, userID, grantedBy string) ([]Collaborator, error) {
	if _, err := s.db.Pool.Exec(ctx, `
		INSERT INTO sentinel_collaborators (sentinel_id, account_id, sencol_granted_by)
		VALUES ($1, $2, $3) ON CONFLICT (sentinel_id, account_id) DO NOTHING`, autopilotID, userID, grantedBy); err != nil {
		return nil, fmt.Errorf("autopilot: добавление коллаборатора: %w", err)
	}
	return s.ListCollaborators(ctx, autopilotID)
}

func (s *Store) RemoveCollaborator(ctx context.Context, autopilotID, userID string) ([]Collaborator, error) {
	if _, err := s.db.Pool.Exec(ctx, `
		DELETE FROM sentinel_collaborators WHERE sentinel_id = $1 AND account_id = $2`, autopilotID, userID); err != nil {
		return nil, fmt.Errorf("autopilot: отзыв коллаборатора: %w", err)
	}
	return s.ListCollaborators(ctx, autopilotID)
}

func (s *Store) IsCollaborator(ctx context.Context, autopilotID, userID string) (bool, error) {
	return s.db.RowExists(ctx, `
		SELECT EXISTS(SELECT 1 FROM sentinel_collaborators WHERE sentinel_id = $1 AND account_id = $2)`,
		autopilotID, userID)
}

// CreatedBy — обёртка над GetCreator для проверки "создатель ли actorID".
func (s *Store) IsCreator(ctx context.Context, autopilotID, userID string) (bool, error) {
	return s.db.RowExists(ctx, `
		SELECT EXISTS(SELECT 1 FROM sentinels WHERE id = $1 AND sen_created_by_type = 'member' AND sen_created_by_id = $2)`,
		autopilotID, userID)
}

// CanWrite — "создатель/owner/admin/коллаборатор" (contract §7.1, x-roles
// autopilot-creator/owner/admin/autopilot-collaborator на PATCH/DELETE/
// trigger/triggers/deliveries-replay).
func (s *Store) CanWrite(ctx context.Context, autopilotID, userID string, role httpapi.Role) (bool, error) {
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true, nil
	}
	if isCreator, err := s.IsCreator(ctx, autopilotID, userID); err != nil {
		return false, err
	} else if isCreator {
		return true, nil
	}
	return s.IsCollaborator(ctx, autopilotID, userID)
}

// CanManageAccess — "только создатель/owner/admin" (contract §7.1, addAutopilotCollaborator/
// removeAutopilotCollaborator: коллабораторы сами не могут управлять доступом).
func (s *Store) CanManageAccess(ctx context.Context, autopilotID, userID string, role httpapi.Role) (bool, error) {
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true, nil
	}
	return s.IsCreator(ctx, autopilotID, userID)
}

// --- misc ------------------------------------------------------------------

// SetLastRunAt — обновляет sen_last_run_at после каждого прогона (любой
// источник: schedule/manual/webhook/api).
func (s *Store) SetLastRunAt(ctx context.Context, autopilotID string, at time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE sentinels SET sen_last_run_at = $2, updated_at = now() WHERE id = $1`, autopilotID, at)
	return err
}

// GetRaw — минимум полей sentinels, нужный внутренним вызовам (dispatcher,
// scheduler), без похода за вычисляемыми полями fillComputed.
type Raw struct {
	ID                 string
	WorkspaceID        string
	Title              string
	Status             string
	ExecutionMode      string
	AssigneeType       string
	AssigneeID         string
	IssueTitleTemplate *string
	ProjectID          *string
	CreatedByType      string
	CreatedByID        string
}

func (s *Store) GetRaw(ctx context.Context, id string) (Raw, error) {
	var r Raw
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, workspace_id, sen_title, sen_status, sen_execution_mode, sen_assignee_type, sen_assignee_id,
			sen_issue_title_template, initiative_id, sen_created_by_type, sen_created_by_id
		FROM sentinels WHERE id = $1`, id).
		Scan(&r.ID, &r.WorkspaceID, &r.Title, &r.Status, &r.ExecutionMode, &r.AssigneeType, &r.AssigneeID,
			&r.IssueTitleTemplate, &r.ProjectID, &r.CreatedByType, &r.CreatedByID)
	if store.IsNoRows(err) {
		return Raw{}, ErrNotFound
	}
	if err != nil {
		return Raw{}, fmt.Errorf("autopilot: чтение автопилота (raw): %w", err)
	}
	return r, nil
}
