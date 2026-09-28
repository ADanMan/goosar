package pin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/adanman/goosar/server2/internal/store"
)

var (
	ErrNotFound     = errors.New("pin: не найдено")
	ErrAlreadyExist = errors.New("pin: уже закреплено")
)

// Pin — форма components/schemas/Pin. item_type различает, из какой из двух
// таблиц (ticket_bookmarks/initiative_bookmarks) взята строка — контракт
// сам говорит, что это split, а не одна полиморфная таблица (см.
// 005_tasks.up.sql, комментарий у ticket_bookmarks).
type Pin struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	ItemType    string    `json:"item_type"`
	ItemID      string    `json:"item_id"`
	Position    float64   `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// List — все закладки accountID в workspaceID, обоих типов, по позиции.
func (s *Store) List(ctx context.Context, workspaceID, accountID string) ([]Pin, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, account_id, 'issue', ticket_id, bm_position, created_at
			FROM ticket_bookmarks WHERE workspace_id = $1 AND account_id = $2
		UNION ALL
		SELECT id, workspace_id, account_id, 'project', initiative_id, bm_position, created_at
			FROM initiative_bookmarks WHERE workspace_id = $1 AND account_id = $2
		ORDER BY 6`, workspaceID, accountID)
	if err != nil {
		return nil, fmt.Errorf("pin: список закладок: %w", err)
	}
	defer rows.Close()
	var out []Pin
	for rows.Next() {
		var p Pin
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.UserID, &p.ItemType, &p.ItemID, &p.Position, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// nextPosition — max(position) + 1 через обе таблицы, чтобы у пользователя
// была единая, сквозная последовательность позиций независимо от типа
// закреплённого элемента (тот же порядок, что отдаёт List).
func (s *Store) nextPosition(ctx context.Context, tx pgx.Tx, workspaceID, accountID string) (float64, error) {
	var maxPos *float64
	err := tx.QueryRow(ctx, `
		SELECT max(pos) FROM (
			SELECT bm_position AS pos FROM ticket_bookmarks WHERE workspace_id = $1 AND account_id = $2
			UNION ALL
			SELECT bm_position AS pos FROM initiative_bookmarks WHERE workspace_id = $1 AND account_id = $2
		) t`, workspaceID, accountID).Scan(&maxPos)
	if err != nil {
		return 0, err
	}
	if maxPos == nil {
		return 1, nil
	}
	return *maxPos + 1, nil
}

// IssueExists / ProjectExists — существование цели перед созданием закладки.
func (s *Store) IssueExists(ctx context.Context, workspaceID, id string) (bool, error) {
	return s.db.RowExists(ctx, `SELECT EXISTS(SELECT 1 FROM tickets WHERE workspace_id = $1 AND id = $2)`, workspaceID, id)
}

func (s *Store) ProjectExists(ctx context.Context, workspaceID, id string) (bool, error) {
	return s.db.RowExists(ctx, `SELECT EXISTS(SELECT 1 FROM initiatives WHERE workspace_id = $1 AND id = $2)`, workspaceID, id)
}

// Create закрепляет itemType/itemID за accountID; позиция — "в конец".
func (s *Store) Create(ctx context.Context, workspaceID, accountID, itemType, itemID string) (Pin, error) {
	var out Pin
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		pos, err := s.nextPosition(ctx, tx, workspaceID, accountID)
		if err != nil {
			return err
		}
		var row pgx.Row
		switch itemType {
		case "issue":
			row = tx.QueryRow(ctx, `
				INSERT INTO ticket_bookmarks (workspace_id, account_id, ticket_id, bm_position)
				VALUES ($1, $2, $3, $4)
				RETURNING id, workspace_id, account_id, 'issue', ticket_id, bm_position, created_at`,
				workspaceID, accountID, itemID, pos)
		case "project":
			row = tx.QueryRow(ctx, `
				INSERT INTO initiative_bookmarks (workspace_id, account_id, initiative_id, bm_position)
				VALUES ($1, $2, $3, $4)
				RETURNING id, workspace_id, account_id, 'project', initiative_id, bm_position, created_at`,
				workspaceID, accountID, itemID, pos)
		default:
			return fmt.Errorf("pin: неизвестный item_type %q", itemType)
		}
		return row.Scan(&out.ID, &out.WorkspaceID, &out.UserID, &out.ItemType, &out.ItemID, &out.Position, &out.CreatedAt)
	})
	if store.IsUniqueViolation(err) {
		return Pin{}, ErrAlreadyExist
	}
	if store.IsForeignKeyViolation(err) {
		return Pin{}, ErrNotFound
	}
	if err != nil {
		return Pin{}, fmt.Errorf("pin: создание закладки: %w", err)
	}
	return out, nil
}

// UpdatePosition ищет строку с этим id среди обеих таблиц закладок этого
// accountID и переставляет её — контракт: "применяется по одной записи, без
// общей атомарности между ними", поэтому каждый вызов — своя операция.
func (s *Store) UpdatePosition(ctx context.Context, accountID, id string, position float64) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE ticket_bookmarks SET bm_position = $3 WHERE account_id = $1 AND id = $2`, accountID, id, position)
	if err != nil {
		return fmt.Errorf("pin: перестановка закладки задачи: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag, err = s.db.Pool.Exec(ctx, `
		UPDATE initiative_bookmarks SET bm_position = $3 WHERE account_id = $1 AND id = $2`, accountID, id, position)
	if err != nil {
		return fmt.Errorf("pin: перестановка закладки проекта: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete снимает закладку по (itemType, itemID) для accountID.
func (s *Store) Delete(ctx context.Context, workspaceID, accountID, itemType, itemID string) error {
	var tag pgconn.CommandTag
	var err error
	switch itemType {
	case "issue":
		tag, err = s.db.Pool.Exec(ctx, `
			DELETE FROM ticket_bookmarks WHERE workspace_id = $1 AND account_id = $2 AND ticket_id = $3`,
			workspaceID, accountID, itemID)
	case "project":
		tag, err = s.db.Pool.Exec(ctx, `
			DELETE FROM initiative_bookmarks WHERE workspace_id = $1 AND account_id = $2 AND initiative_id = $3`,
			workspaceID, accountID, itemID)
	default:
		return fmt.Errorf("pin: неизвестный item_type %q", itemType)
	}
	if err != nil {
		return fmt.Errorf("pin: снятие закладки: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
