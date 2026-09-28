package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

var ErrNotFound = errors.New("task: не найдено")
var ErrConflict = errors.New("task: конфликт (например, схлопнувшаяся позиция при move)")

var validStatuses = map[string]bool{
	"backlog": true, "todo": true, "in_progress": true, "in_review": true,
	"done": true, "blocked": true, "cancelled": true,
}
var validPriorities = map[string]bool{"urgent": true, "high": true, "medium": true, "low": true, "none": true}
var validAssigneeTypes = map[string]bool{"member": true, "agent": true, "squad": true}
var validCreatorTypes = map[string]bool{"member": true, "agent": true}

// terminalStatuses — статусы, терминальные для подсчёта прогресса дочерних
// задач (contract §1.7).
var terminalStatuses = map[string]bool{"done": true, "cancelled": true}

// statusOrder — канбан-порядок для sort=status (contract §1.5), не алфавитный.
var statusOrder = map[string]int{
	"backlog": 0, "todo": 1, "in_progress": 2, "in_review": 3, "done": 4, "blocked": 5, "cancelled": 6,
}

// priorityOrder — urgent -> none (contract §1.5).
var priorityOrder = map[string]int{"urgent": 0, "high": 1, "medium": 2, "low": 3, "none": 4}

// Store — доступ к tickets и связанным таблицам 005_tasks.up.sql.
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// pool отдаёт пул соединений как dispatch.Querier — так handlers.go может
// звать dispatch.Deps.Enqueue/CancelActiveForTicket вне собственной
// транзакции task (постановка в очередь — уже после закоммиченного
// изменения задачи, не в одной с ним транзакции).
func (s *Store) pool() *pgxpool.Pool { return s.db.Pool }

const ticketColumns = `id, workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_narrative, tk_status,
	tk_priority, tk_assignee_type, tk_assignee_id, tk_creator_type, tk_creator_id, tk_parent_ticket_id,
	initiative_id, tk_position, tk_stage, tk_start_date, tk_due_date, tk_metadata, tk_custom_field_values,
	created_at, updated_at`

func scanIssue(row pgx.Row) (Issue, error) {
	var i Issue
	var metadata, properties []byte
	if err := row.Scan(&i.ID, &i.WorkspaceID, &i.Number, &i.Identifier, &i.Title, &i.Description, &i.Status,
		&i.Priority, &i.AssigneeType, &i.AssigneeID, &i.CreatorType, &i.CreatorID, &i.ParentIssueID,
		&i.ProjectID, &i.Position, &i.Stage, &i.StartDate, &i.DueDate, &metadata, &properties,
		&i.CreatedAt, &i.UpdatedAt); err != nil {
		return Issue{}, err
	}
	if len(metadata) > 0 {
		i.Metadata = metadata
	}
	if len(properties) > 0 {
		i.Properties = properties
	}
	return i, nil
}

func collectIssues(rows pgx.Rows) ([]Issue, error) {
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// CreateParams — вход CreateIssue (createIssue/quickCreateIssue).
type CreateParams struct {
	WorkspaceID   string
	Title         string
	Description   *string
	Status        string // "" -> "todo" по умолчанию (contract)
	Priority      string // "" -> "none"
	AssigneeType  *string
	AssigneeID    *string
	CreatorType   string
	CreatorID     string
	ParentIssueID *string
	ProjectID     *string
	Stage         *int
	StartDate     *time.Time
	DueDate       *time.Time
	Metadata      json.RawMessage
	Properties    json.RawMessage
}

// CreateIssue присваивает number/identifier атомарно (workspace.Store.IncrementTicketSeq
// в той же транзакции, что и INSERT) и создаёт строку tickets. Позиция новой
// задачи — конец списка её статуса (max(position)+1024, либо 1024, если
// список пуст), чтобы новые задачи не требовали немедленного move.
func (s *Store) CreateIssue(ctx context.Context, ws *workspace.Store, p CreateParams) (Issue, error) {
	if p.Status == "" {
		p.Status = "todo"
	}
	if p.Priority == "" {
		p.Priority = "none"
	}
	var out Issue
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		numbering, err := ws.IncrementTicketSeq(ctx, tx, p.WorkspaceID)
		if err != nil {
			return err
		}
		identifier := fmt.Sprintf("%s-%d", numbering.Prefix, numbering.Seq)

		var position float64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(tk_position), 0) + 1024 FROM tickets
			WHERE workspace_id = $1 AND tk_status = $2`, p.WorkspaceID, p.Status).Scan(&position); err != nil {
			return fmt.Errorf("task: вычисление позиции новой задачи: %w", err)
		}

		metadata := p.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage(`{}`)
		}
		properties := p.Properties
		if len(properties) == 0 {
			properties = json.RawMessage(`{}`)
		}

		row := tx.QueryRow(ctx, `
			INSERT INTO tickets (
				workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_narrative, tk_status, tk_priority,
				tk_assignee_type, tk_assignee_id, tk_creator_type, tk_creator_id, tk_parent_ticket_id,
				initiative_id, tk_position, tk_stage, tk_start_date, tk_due_date, tk_metadata, tk_custom_field_values
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			RETURNING `+ticketColumns,
			p.WorkspaceID, numbering.Seq, identifier, p.Title, p.Description, p.Status, p.Priority,
			p.AssigneeType, p.AssigneeID, p.CreatorType, p.CreatorID, p.ParentIssueID,
			p.ProjectID, position, p.Stage, p.StartDate, p.DueDate, metadata, properties,
		)
		out, err = scanIssue(row)
		if err != nil {
			return fmt.Errorf("task: создание задачи: %w", err)
		}
		return recordActivity(ctx, tx, out.WorkspaceID, out.ID, out.CreatorType, &out.CreatorID, "created", nil)
	})
	if err != nil {
		return Issue{}, err
	}
	return out, nil
}

func (s *Store) GetIssue(ctx context.Context, workspaceID, id string) (Issue, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+ticketColumns+` FROM tickets WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	i, err := scanIssue(row)
	if store.IsNoRows(err) {
		return Issue{}, ErrNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("task: получение задачи: %w", err)
	}
	return i, nil
}

// parseIdentifierNumber резолвит "ENG-42" в number (contract §1.4: "ENG-42
// эквивалентно фильтру number=42").
func parseIdentifierNumber(prefix, q string) (int64, bool) {
	prefixDash := strings.ToUpper(prefix) + "-"
	if !strings.HasPrefix(strings.ToUpper(q), prefixDash) {
		return 0, false
	}
	var n int64
	if _, err := fmt.Sscanf(strings.ToUpper(q)[len(prefixDash):], "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

// execer — минимум для recordActivity: и pgxpool.Pool, и pgx.Tx подходят.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func recordActivity(ctx context.Context, q execer, workspaceID, ticketID, actorType string, actorID *string, action string, details json.RawMessage) error {
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	if actorID != nil && *actorID == "" {
		actorID = nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO ticket_activity (workspace_id, ticket_id, ta_actor_type, ta_actor_id, ta_action, ta_details)
		VALUES ($1,$2,$3,$4,$5,$6)`, workspaceID, ticketID, actorType, actorID, action, details)
	if err != nil {
		return fmt.Errorf("task: запись активности: %w", err)
	}
	return nil
}
