package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("feed: не найдено")

// Queryer — минимальный набор методов pgx, которых достаточно для записи
// alerts/notification_prefs. И *pgxpool.Pool (через Store.db.Pool), и pgx.Tx
// ему удовлетворяют — вызывающий домен передаёт в Notify то, что у него уже
// открыто (обычно свою же транзакцию, чтобы уведомление фиксировалось вместе
// с основным действием), не создавая отдельного соединения.
type Queryer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store — доступ к alerts/notification_prefs (009_feed.up.sql).
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// Q — пул домена как Queryer, для вызовов Notify вне чужой транзакции.
func (s *Store) Q() Queryer { return s.db.Pool }

// Alert — форма components/schemas/InboxItem.
type Alert struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   string          `json:"recipient_id"`
	Type          string          `json:"type"`
	Severity      string          `json:"severity"`
	IssueID       *string         `json:"issue_id"`
	Title         string          `json:"title"`
	Body          *string         `json:"body"`
	Read          bool            `json:"read"`
	Archived      bool            `json:"archived"`
	CreatedAt     time.Time       `json:"created_at"`
	IssueStatus   *string         `json:"issue_status,omitempty"`
	ActorType     *string         `json:"actor_type"`
	ActorID       *string         `json:"actor_id"`
	Details       json.RawMessage `json:"details"`
}

const alertColumns = `id, workspace_id, al_recipient_type, al_recipient_id, al_kind, al_severity, ticket_id,
	al_headline, al_body, al_read_at, al_archived_at, al_actor_type, al_actor_id, al_details, created_at`

func scanAlert(row pgx.Row) (Alert, error) {
	var a Alert
	var readAt, archivedAt *time.Time
	var details []byte
	if err := row.Scan(&a.ID, &a.WorkspaceID, &a.RecipientType, &a.RecipientID, &a.Type, &a.Severity, &a.IssueID,
		&a.Title, &a.Body, &readAt, &archivedAt, &a.ActorType, &a.ActorID, &details, &a.CreatedAt); err != nil {
		return Alert{}, err
	}
	a.Read = readAt != nil
	a.Archived = archivedAt != nil
	if len(details) == 0 {
		details = []byte("{}")
	}
	a.Details = details
	return a, nil
}

func scanAlertRows(rows pgx.Rows) ([]Alert, error) {
	var out []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListActive — активные (неархивные) элементы получателя-участника,
// денормализованные с tk_status (issue_status, только для списковых выдач).
func (s *Store) ListActive(ctx context.Context, workspaceID, accountID string) ([]Alert, error) {
	return s.listByArchived(ctx, workspaceID, accountID, false)
}

// ListArchived — архивные элементы получателя.
func (s *Store) ListArchived(ctx context.Context, workspaceID, accountID string) ([]Alert, error) {
	return s.listByArchived(ctx, workspaceID, accountID, true)
}

func (s *Store) listByArchived(ctx context.Context, workspaceID, accountID string, archived bool) ([]Alert, error) {
	cmp := "IS NULL"
	if archived {
		cmp = "IS NOT NULL"
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT a.id, a.workspace_id, a.al_recipient_type, a.al_recipient_id, a.al_kind, a.al_severity, a.ticket_id,
			a.al_headline, a.al_body, a.al_read_at, a.al_archived_at, a.al_actor_type, a.al_actor_id, a.al_details,
			a.created_at, t.tk_status
		FROM alerts a
		LEFT JOIN tickets t ON t.id = a.ticket_id
		WHERE a.workspace_id = $1 AND a.al_recipient_type = 'member' AND a.al_recipient_id = $2
			AND a.al_archived_at `+cmp+`
		ORDER BY a.created_at DESC`, workspaceID, accountID)
	if err != nil {
		return nil, fmt.Errorf("feed: список инбокса: %w", err)
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		a, err := scanAlertWithStatus(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanAlertWithStatus(rows pgx.Rows) (Alert, error) {
	var a Alert
	var readAt, archivedAt *time.Time
	var details []byte
	if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.RecipientType, &a.RecipientID, &a.Type, &a.Severity, &a.IssueID,
		&a.Title, &a.Body, &readAt, &archivedAt, &a.ActorType, &a.ActorID, &details, &a.CreatedAt, &a.IssueStatus); err != nil {
		return Alert{}, err
	}
	a.Read = readAt != nil
	a.Archived = archivedAt != nil
	if len(details) == 0 {
		details = []byte("{}")
	}
	a.Details = details
	return a, nil
}

// CountUnread — число непрочитанных активных элементов воркспейса.
func (s *Store) CountUnread(ctx context.Context, workspaceID, accountID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM alerts
		WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
			AND al_read_at IS NULL AND al_archived_at IS NULL`, workspaceID, accountID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("feed: подсчёт непрочитанных: %w", err)
	}
	return n, nil
}

// WorkspaceUnread — один элемент getUnreadInboxSummary.
type WorkspaceUnread struct {
	WorkspaceID string `json:"workspace_id"`
	Count       int    `json:"count"`
}

// UnreadSummary — непрочитанные по всем воркспейсам, где accountID состоит участником.
func (s *Store) UnreadSummary(ctx context.Context, accountID string) ([]WorkspaceUnread, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT sm.workspace_id, count(a.id)
		FROM space_members sm
		LEFT JOIN alerts a ON a.workspace_id = sm.workspace_id AND a.al_recipient_type = 'member'
			AND a.al_recipient_id = sm.account_id AND a.al_read_at IS NULL AND a.al_archived_at IS NULL
		WHERE sm.account_id = $1
		GROUP BY sm.workspace_id`, accountID)
	if err != nil {
		return nil, fmt.Errorf("feed: сводка непрочитанных: %w", err)
	}
	defer rows.Close()
	var out []WorkspaceUnread
	for rows.Next() {
		var u WorkspaceUnread
		if err := rows.Scan(&u.WorkspaceID, &u.Count); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkAllRead отмечает прочитанными все непрочитанные активные элементы получателя.
func (s *Store) MarkAllRead(ctx context.Context, workspaceID, accountID string) (int, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE alerts SET al_read_at = now()
		WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
			AND al_read_at IS NULL AND al_archived_at IS NULL`, workspaceID, accountID)
	if err != nil {
		return 0, fmt.Errorf("feed: массовое прочтение: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ArchiveAll архивирует все активные элементы получателя (прочитанные и нет).
func (s *Store) ArchiveAll(ctx context.Context, workspaceID, accountID string) (int, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE alerts SET al_archived_at = now()
		WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
			AND al_archived_at IS NULL`, workspaceID, accountID)
	if err != nil {
		return 0, fmt.Errorf("feed: массовая архивация: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ArchiveAllRead архивирует только уже прочитанные активные элементы.
func (s *Store) ArchiveAllRead(ctx context.Context, workspaceID, accountID string) (int, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE alerts SET al_archived_at = now()
		WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
			AND al_archived_at IS NULL AND al_read_at IS NOT NULL`, workspaceID, accountID)
	if err != nil {
		return 0, fmt.Errorf("feed: массовая архивация прочитанных: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ArchiveCompleted архивирует активные элементы, чей связанный тикет уже
// закрыт. tk_status не знает значения "completed" (см. 005_tasks.up.sql):
// пробел спецификации, решение — considерировать закрытыми done и cancelled
// (см. server2/docs/decisions.md).
func (s *Store) ArchiveCompleted(ctx context.Context, workspaceID, accountID string) (int, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE alerts SET al_archived_at = now()
		WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
			AND al_archived_at IS NULL AND ticket_id IN (
				SELECT id FROM tickets WHERE workspace_id = $1 AND tk_status IN ('done', 'cancelled')
			)`, workspaceID, accountID)
	if err != nil {
		return 0, fmt.Errorf("feed: архивация завершённых: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// getOwnAlert — базовая проверка "элемент существует и принадлежит accountID
// как получателю-участнику" перед MarkRead/Archive/Unarchive.
func (s *Store) getOwnAlert(ctx context.Context, workspaceID, accountID, id string) (Alert, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM alerts
		WHERE workspace_id = $1 AND id = $2 AND al_recipient_type = 'member' AND al_recipient_id = $3`,
		workspaceID, id, accountID)
	a, err := scanAlert(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	if err != nil {
		return Alert{}, fmt.Errorf("feed: чтение элемента инбокса: %w", err)
	}
	return a, nil
}

// MarkRead отмечает один элемент прочитанным.
func (s *Store) MarkRead(ctx context.Context, workspaceID, accountID, id string) (Alert, error) {
	if _, err := s.getOwnAlert(ctx, workspaceID, accountID, id); err != nil {
		return Alert{}, err
	}
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE alerts SET al_read_at = COALESCE(al_read_at, now())
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+alertColumns, workspaceID, id)
	return scanAlert(row)
}

// Archive архивирует элемент и, если он привязан к тикету, все прочие
// активные элементы того же получателя по тому же тикету (см. описание
// archiveInboxItem в контракте — "схлопывает" цепочку).
func (s *Store) Archive(ctx context.Context, workspaceID, accountID, id string) (Alert, error) {
	target, err := s.getOwnAlert(ctx, workspaceID, accountID, id)
	if err != nil {
		return Alert{}, err
	}
	err = s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if target.IssueID != nil {
			if _, err := tx.Exec(ctx, `
				UPDATE alerts SET al_archived_at = now()
				WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
					AND ticket_id = $3 AND al_archived_at IS NULL`,
				workspaceID, accountID, *target.IssueID); err != nil {
				return err
			}
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE alerts SET al_archived_at = now() WHERE workspace_id = $1 AND id = $2 AND al_archived_at IS NULL`,
			workspaceID, id)
		return err
	})
	if err != nil {
		return Alert{}, fmt.Errorf("feed: архивация элемента: %w", err)
	}
	return s.getOwnAlert(ctx, workspaceID, accountID, id)
}

// Unarchive симметричен Archive: возвращает из архива элемент и (если
// привязан к тикету) все прочие архивные элементы того же получателя по
// тому же тикету.
func (s *Store) Unarchive(ctx context.Context, workspaceID, accountID, id string) (Alert, error) {
	target, err := s.getOwnAlert(ctx, workspaceID, accountID, id)
	if err != nil {
		return Alert{}, err
	}
	err = s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if target.IssueID != nil {
			if _, err := tx.Exec(ctx, `
				UPDATE alerts SET al_archived_at = NULL
				WHERE workspace_id = $1 AND al_recipient_type = 'member' AND al_recipient_id = $2
					AND ticket_id = $3 AND al_archived_at IS NOT NULL`,
				workspaceID, accountID, *target.IssueID); err != nil {
				return err
			}
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE alerts SET al_archived_at = NULL WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
		return err
	})
	if err != nil {
		return Alert{}, fmt.Errorf("feed: разархивация элемента: %w", err)
	}
	return s.getOwnAlert(ctx, workspaceID, accountID, id)
}

// --- notification preferences ---------------------------------------------------

// ValidGroups — шесть групп из components/schemas/NotificationPreferencesInput.
var ValidGroups = map[string]bool{
	"assignments": true, "status_changes": true, "comments": true,
	"updates": true, "agent_activity": true, "system_notifications": true,
}

// ValidGroupValues — components/schemas/NotificationGroupValue.
var ValidGroupValues = map[string]bool{"all": true, "muted": true}

// GetPrefs — настройки получателя; пустой объект, если строки ещё нет
// (контракт: "если запись ещё не создавалась, возвращает пустой объект").
func (s *Store) GetPrefs(ctx context.Context, workspaceID, accountID string) (map[string]string, error) {
	var raw []byte
	err := s.db.Pool.QueryRow(ctx, `SELECT np_groups FROM notification_prefs WHERE workspace_id = $1 AND account_id = $2`,
		workspaceID, accountID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feed: чтение настроек уведомлений: %w", err)
	}
	return decodePrefs(raw), nil
}

func decodePrefs(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// MergePrefs — PATCH: сливает переданные группы с уже сохранёнными.
func (s *Store) MergePrefs(ctx context.Context, workspaceID, accountID string, patch map[string]string) (map[string]string, error) {
	current, err := s.GetPrefs(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	for k, v := range patch {
		current[k] = v
	}
	return current, s.putPrefs(ctx, workspaceID, accountID, current)
}

// ReplacePrefs — PUT: заменяет весь объект целиком.
func (s *Store) ReplacePrefs(ctx context.Context, workspaceID, accountID string, next map[string]string) (map[string]string, error) {
	return next, s.putPrefs(ctx, workspaceID, accountID, next)
}

func (s *Store) putPrefs(ctx context.Context, workspaceID, accountID string, groups map[string]string) error {
	body, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO notification_prefs (workspace_id, account_id, np_groups)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id, account_id) DO UPDATE SET np_groups = EXCLUDED.np_groups, updated_at = now()`,
		workspaceID, accountID, string(body))
	if err != nil {
		return fmt.Errorf("feed: сохранение настроек уведомлений: %w", err)
	}
	return nil
}

// prefsForRecipient — прочитанные предпочтения получателя-человека для
// проверки Notify (агентам preferences не считаются — см. Notify).
func prefsForRecipient(ctx context.Context, q Queryer, workspaceID, accountID string) (map[string]string, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT np_groups FROM notification_prefs WHERE workspace_id = $1 AND account_id = $2`,
		workspaceID, accountID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feed: чтение настроек уведомлений для Notify: %w", err)
	}
	return decodePrefs(raw), nil
}
