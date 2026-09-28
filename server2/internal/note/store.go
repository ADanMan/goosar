// Package note реализует комментарии задач (/api/issues/{id}/comments/**,
// /api/comments/{commentId}/**) — таблицы ticket_notes/note_marks
// (комментарии и реакции на них) и ticket_marks (реакции на саму задачу),
// все из 005_tasks.up.sql.
package note

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("note: не найдено")

// Comment — форма components/schemas/IssueComment. Поля, помеченные
// "только в определённом режиме листинга/операции", сериализуются лишь
// когда их источник заполнил соответствующий указатель.
type Comment struct {
	ID             string     `json:"id"`
	IssueID        string     `json:"issue_id"`
	AuthorType     string     `json:"author_type"`
	AuthorID       string     `json:"author_id"`
	Content        string     `json:"content"`
	Type           string     `json:"type"`
	ParentID       *string    `json:"parent_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	ResolvedByType *string    `json:"resolved_by_type"`
	ResolvedByID   *string    `json:"resolved_by_id"`
	SourceTaskID   *string    `json:"source_task_id,omitempty"`

	Reactions   []Reaction         `json:"reactions"`
	Attachments []asset.Attachment `json:"attachments"`

	ReplyCount       *int       `json:"reply_count,omitempty"`
	LastActivityAt   *time.Time `json:"last_activity_at,omitempty"`
	ContentTruncated *bool      `json:"content_truncated,omitempty"`
	ThreadResolved   *bool      `json:"thread_resolved,omitempty"`
	FoldedCount      *int       `json:"folded_count,omitempty"`

	TriggerOutcomes []TriggerOutcome `json:"trigger_outcomes,omitempty"`
}

type Reaction struct {
	ID        string    `json:"id"`
	CommentID string    `json:"comment_id"`
	ActorType string    `json:"actor_type"`
	ActorID   string    `json:"actor_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

type IssueReaction struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	ActorType string    `json:"actor_type"`
	ActorID   string    `json:"actor_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

const commentColumns = `id, ticket_id, tn_author_type, tn_author_id, tn_body, tn_kind,
	tn_parent_note_id, tn_resolved_at, tn_resolved_by_type, tn_resolved_by_id,
	tn_source_dispatch_job_id, created_at, updated_at`

func scanComment(row pgx.Row) (Comment, error) {
	var c Comment
	if err := row.Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorID, &c.Content, &c.Type,
		&c.ParentID, &c.ResolvedAt, &c.ResolvedByType, &c.ResolvedByID,
		&c.SourceTaskID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Comment{}, err
	}
	// source_task_id виден только когда автор — агент (правило раскрытия
	// атрибуции, см. components/schemas/IssueComment.source_task_id).
	if c.AuthorType != "agent" {
		c.SourceTaskID = nil
	}
	c.Reactions = []Reaction{}
	c.Attachments = []asset.Attachment{}
	return c, nil
}

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// --- lookups the trigger/handler logic needs about the parent ticket -------

// TicketInfo — минимум о тикете, нужный обработчикам комментариев/реакций
// (существование + поля для x-events payload и правил автозапуска).
type TicketInfo struct {
	ID           string
	WorkspaceID  string
	Headline     string
	Status       string
	AssigneeType *string
	AssigneeID   *string
}

// ticketWorkspaceID резолвит workspace_id тикета по его id — нужен маршрутам
// /api/comments/{commentId}/**, у которых воркспейс не в заголовках/пути
// явно, а определяется через сам комментарий -> его тикет.
func (s *Store) ticketWorkspaceID(ctx context.Context, ticketID string) (string, error) {
	var workspaceID string
	err := s.db.Pool.QueryRow(ctx, `SELECT workspace_id FROM tickets WHERE id = $1`, ticketID).Scan(&workspaceID)
	if store.IsNoRows(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("note: резолв воркспейса тикета: %w", err)
	}
	return workspaceID, nil
}

func (s *Store) GetTicketInfo(ctx context.Context, workspaceID, ticketID string) (TicketInfo, error) {
	var t TicketInfo
	t.WorkspaceID = workspaceID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, tk_headline, tk_status, tk_assignee_type, tk_assignee_id
		FROM tickets WHERE workspace_id = $1 AND id = $2`, workspaceID, ticketID).
		Scan(&t.ID, &t.Headline, &t.Status, &t.AssigneeType, &t.AssigneeID)
	if store.IsNoRows(err) {
		return TicketInfo{}, ErrNotFound
	}
	if err != nil {
		return TicketInfo{}, fmt.Errorf("note: получение задачи: %w", err)
	}
	return t, nil
}

// --- comment CRUD ------------------------------------------------------------

type CreateCommentParams struct {
	TicketID            string
	AuthorType          string
	AuthorID            string
	Content             string
	Kind                string
	ParentID            *string
	SourceDispatchJobID *string
}

func (s *Store) CreateComment(ctx context.Context, p CreateCommentParams) (Comment, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO ticket_notes (ticket_id, tn_author_type, tn_author_id, tn_body, tn_kind,
			tn_parent_note_id, tn_source_dispatch_job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+commentColumns,
		p.TicketID, p.AuthorType, p.AuthorID, p.Content, p.Kind, p.ParentID, p.SourceDispatchJobID)
	return scanComment(row)
}

func (s *Store) GetComment(ctx context.Context, id string) (Comment, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+commentColumns+` FROM ticket_notes WHERE id = $1`, id)
	c, err := scanComment(row)
	if store.IsNoRows(err) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("note: получение комментария: %w", err)
	}
	return c, nil
}

// GetCommentInTicket — как GetComment, но также проверяет принадлежность
// ticketID (для маршрутов вложенных в /api/issues/{id}).
func (s *Store) GetCommentInTicket(ctx context.Context, ticketID, id string) (Comment, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+commentColumns+` FROM ticket_notes WHERE ticket_id = $1 AND id = $2`,
		ticketID, id)
	c, err := scanComment(row)
	if store.IsNoRows(err) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("note: получение комментария задачи: %w", err)
	}
	return c, nil
}

func (s *Store) UpdateCommentContent(ctx context.Context, id, content string) (Comment, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE ticket_notes SET tn_body = $2, updated_at = now() WHERE id = $1
		RETURNING `+commentColumns, id, content)
	c, err := scanComment(row)
	if store.IsNoRows(err) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("note: обновление комментария: %w", err)
	}
	return c, nil
}

func (s *Store) DeleteComment(ctx context.Context, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM ticket_notes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("note: удаление комментария: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RootOf находит корень треда, в котором лежит commentID (сам commentID,
// если у него нет родителя).
func (s *Store) RootOf(ctx context.Context, commentID string) (string, error) {
	var rootID string
	err := s.db.Pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, tn_parent_note_id FROM ticket_notes WHERE id = $1
			UNION ALL
			SELECT tn.id, tn.tn_parent_note_id FROM ticket_notes tn JOIN up ON tn.id = up.tn_parent_note_id
		)
		SELECT id FROM up WHERE tn_parent_note_id IS NULL LIMIT 1`, commentID).Scan(&rootID)
	if store.IsNoRows(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("note: поиск корня треда: %w", err)
	}
	return rootID, nil
}

// --- listing -------------------------------------------------------------------

// ListOptions — параметры GET /api/issues/{id}/comments (см. contract
// listIssueComments); ровно один режим активен за раз (проверяется до
// вызова Store).
type ListOptions struct {
	Since     *time.Time
	Thread    string // id корня
	Tail      int
	Recent    int
	Before    *time.Time
	BeforeID  string
	RootsOnly bool
	Fold      bool
	Summary   bool
}

const maxCommentsListed = 2000

// ListAll — режим по умолчанию/since: все (или созданные после Since)
// комментарии тикета, по возрастанию времени.
func (s *Store) ListAll(ctx context.Context, ticketID string, since *time.Time) ([]Comment, error) {
	var rows pgx.Rows
	var err error
	if since != nil {
		rows, err = s.db.Pool.Query(ctx, `
			SELECT `+commentColumns+` FROM ticket_notes
			WHERE ticket_id = $1 AND created_at > $2 ORDER BY created_at ASC LIMIT $3`,
			ticketID, *since, maxCommentsListed)
	} else {
		rows, err = s.db.Pool.Query(ctx, `
			SELECT `+commentColumns+` FROM ticket_notes
			WHERE ticket_id = $1 ORDER BY created_at ASC LIMIT $2`, ticketID, maxCommentsListed)
	}
	if err != nil {
		return nil, fmt.Errorf("note: список комментариев: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

// ListRoots — только корневые комментарии, с reply_count/last_activity_at.
func (s *Store) ListRoots(ctx context.Context, ticketID string) ([]Comment, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+qualify(commentColumns, "r")+`,
			(SELECT count(*) FROM ticket_notes d WHERE d.tn_parent_note_id = r.id) AS reply_count,
			GREATEST(r.updated_at, (SELECT max(d.created_at) FROM ticket_notes d WHERE d.tn_parent_note_id = r.id)) AS last_activity_at
		FROM ticket_notes r
		WHERE r.ticket_id = $1 AND r.tn_parent_note_id IS NULL
		ORDER BY last_activity_at DESC LIMIT $2`, ticketID, maxCommentsListed)
	if err != nil {
		return nil, fmt.Errorf("note: список корневых комментариев: %w", err)
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		c, replyCount, lastActivity, err := scanCommentWithRootMeta(rows)
		if err != nil {
			return nil, err
		}
		c.ReplyCount = &replyCount
		c.LastActivityAt = &lastActivity
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListThread — корень rootID + все его потомки (тред может быть глубже
// одного уровня — tn_parent_note_id рекурсивен), по возрастанию времени;
// tail>0 оставляет только последние tail ответов плюс корень.
func (s *Store) ListThread(ctx context.Context, rootID string, tail int) ([]Comment, error) {
	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE thread AS (
			SELECT `+commentColumns+` FROM ticket_notes WHERE id = $1
			UNION ALL
			SELECT `+qualify(commentColumns, "tn")+` FROM ticket_notes tn JOIN thread th ON tn.tn_parent_note_id = th.id
		)
		SELECT * FROM thread ORDER BY created_at ASC`, rootID)
	if err != nil {
		return nil, fmt.Errorf("note: список комментариев треда: %w", err)
	}
	defer rows.Close()
	all, err := scanComments(rows)
	if err != nil {
		return nil, err
	}
	if tail <= 0 || len(all) <= tail+1 {
		return all, nil
	}
	// корень + последние tail ответов.
	out := make([]Comment, 0, tail+1)
	out = append(out, all[0])
	out = append(out, all[len(all)-tail:]...)
	return out, nil
}

// ListRecentThreads — id корней последних recent тредов по времени
// последней активности (последний комментарий любого из уровня треда).
func (s *Store) ListRecentThreads(ctx context.Context, ticketID string, recent int) ([]string, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT r.id
		FROM ticket_notes r
		WHERE r.ticket_id = $1 AND r.tn_parent_note_id IS NULL
		ORDER BY GREATEST(r.updated_at, COALESCE((SELECT max(d.created_at) FROM ticket_notes d
			WHERE d.tn_parent_note_id = r.id), r.updated_at)) DESC
		LIMIT $2`, ticketID, recent)
	if err != nil {
		return nil, fmt.Errorf("note: список последних тредов: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanCommentWithRootMeta(rows pgx.Rows) (Comment, int, time.Time, error) {
	var c Comment
	var replyCount int
	var lastActivity time.Time
	if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorID, &c.Content, &c.Type,
		&c.ParentID, &c.ResolvedAt, &c.ResolvedByType, &c.ResolvedByID,
		&c.SourceTaskID, &c.CreatedAt, &c.UpdatedAt, &replyCount, &lastActivity); err != nil {
		return Comment{}, 0, time.Time{}, err
	}
	if c.AuthorType != "agent" {
		c.SourceTaskID = nil
	}
	c.Reactions = []Reaction{}
	c.Attachments = []asset.Attachment{}
	return c, replyCount, lastActivity, nil
}

func scanComments(rows pgx.Rows) ([]Comment, error) {
	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// qualify префиксует alias. к каждой колонке простого списка через запятую
// (commentColumns не содержит вложенных запятых, так что этого достаточно —
// тот же приём, независимо, использует tagging.Store для своих колонок).
func qualify(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// --- resolve/unresolve -----------------------------------------------------------

// ResolveThread помечает rootID решённым, снимая резолюцию с любого другого
// уже решённого комментария того же треда (контракт: "в треде решён только
// один комментарий одновременно"). Возвращает id ранее решённого
// комментария этого треда, если он был другим (для comment:unresolved).
func (s *Store) ResolveThread(ctx context.Context, rootID, commentID, actorType, actorID string) (Comment, *string, error) {
	var unresolvedPrev *string
	var out Comment
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var prevID *string
		err := tx.QueryRow(ctx, `
			WITH RECURSIVE thread AS (
				SELECT id FROM ticket_notes WHERE id = $1
				UNION ALL
				SELECT tn.id FROM ticket_notes tn JOIN thread th ON tn.tn_parent_note_id = th.id
			)
			SELECT id FROM ticket_notes WHERE id IN (SELECT id FROM thread) AND tn_resolved_at IS NOT NULL AND id <> $2
			LIMIT 1`, rootID, commentID).Scan(&prevID)
		if err != nil && !store.IsNoRows(err) {
			return err
		}
		if prevID != nil {
			if _, err := tx.Exec(ctx, `UPDATE ticket_notes SET tn_resolved_at = NULL,
				tn_resolved_by_type = NULL, tn_resolved_by_id = NULL WHERE id = $1`, *prevID); err != nil {
				return err
			}
			unresolvedPrev = prevID
		}
		row := tx.QueryRow(ctx, `
			UPDATE ticket_notes SET tn_resolved_at = now(), tn_resolved_by_type = $2, tn_resolved_by_id = $3,
				updated_at = now()
			WHERE id = $1
			RETURNING `+commentColumns, commentID, actorType, actorID)
		var err2 error
		out, err2 = scanComment(row)
		return err2
	})
	if store.IsNoRows(err) {
		return Comment{}, nil, ErrNotFound
	}
	if err != nil {
		return Comment{}, nil, fmt.Errorf("note: резолюция треда: %w", err)
	}
	return out, unresolvedPrev, nil
}

// UnresolveComment снимает резолюцию с commentID и сообщает, была ли она
// реально снята (contract: событие comment:unresolved публикуется "только
// если комментарий действительно был решён").
func (s *Store) UnresolveComment(ctx context.Context, commentID string) (Comment, bool, error) {
	before, err := s.GetComment(ctx, commentID)
	if err != nil {
		return Comment{}, false, err
	}
	wasResolved := before.ResolvedAt != nil
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE ticket_notes SET tn_resolved_at = NULL, tn_resolved_by_type = NULL, tn_resolved_by_id = NULL,
			updated_at = now()
		WHERE id = $1
		RETURNING `+commentColumns, commentID)
	out, err := scanComment(row)
	if store.IsNoRows(err) {
		return Comment{}, false, ErrNotFound
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("note: снятие резолюции: %w", err)
	}
	return out, wasResolved, nil
}

// --- reactions -------------------------------------------------------------------

func (s *Store) AddCommentReaction(ctx context.Context, commentID, actorType, actorID, emoji string) (Reaction, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO note_marks (ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji) DO UPDATE SET nm_emoji = EXCLUDED.nm_emoji
		RETURNING id, ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji, created_at`,
		commentID, actorType, actorID, emoji)
	var re Reaction
	if err := row.Scan(&re.ID, &re.CommentID, &re.ActorType, &re.ActorID, &re.Emoji, &re.CreatedAt); err != nil {
		if store.IsForeignKeyViolation(err) {
			return Reaction{}, ErrNotFound
		}
		return Reaction{}, fmt.Errorf("note: добавление реакции: %w", err)
	}
	return re, nil
}

func (s *Store) RemoveCommentReaction(ctx context.Context, commentID, actorType, actorID, emoji string) error {
	_, err := s.db.Pool.Exec(ctx, `
		DELETE FROM note_marks WHERE ticket_note_id = $1 AND nm_actor_type = $2 AND nm_actor_id = $3 AND nm_emoji = $4`,
		commentID, actorType, actorID, emoji)
	if err != nil {
		return fmt.Errorf("note: снятие реакции: %w", err)
	}
	return nil
}

func (s *Store) ListCommentReactions(ctx context.Context, commentID string) ([]Reaction, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji, created_at
		FROM note_marks WHERE ticket_note_id = $1 ORDER BY created_at`, commentID)
	if err != nil {
		return nil, fmt.Errorf("note: список реакций комментария: %w", err)
	}
	defer rows.Close()
	var out []Reaction
	for rows.Next() {
		var re Reaction
		if err := rows.Scan(&re.ID, &re.CommentID, &re.ActorType, &re.ActorID, &re.Emoji, &re.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, rows.Err()
}

func (s *Store) AddIssueReaction(ctx context.Context, ticketID, actorType, actorID, emoji string) (IssueReaction, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO ticket_marks (ticket_id, tm_actor_type, tm_actor_id, tm_emoji)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (ticket_id, tm_actor_type, tm_actor_id, tm_emoji) DO UPDATE SET tm_emoji = EXCLUDED.tm_emoji
		RETURNING id, ticket_id, tm_actor_type, tm_actor_id, tm_emoji, created_at`,
		ticketID, actorType, actorID, emoji)
	var re IssueReaction
	if err := row.Scan(&re.ID, &re.IssueID, &re.ActorType, &re.ActorID, &re.Emoji, &re.CreatedAt); err != nil {
		return IssueReaction{}, fmt.Errorf("note: добавление реакции на задачу: %w", err)
	}
	return re, nil
}

func (s *Store) RemoveIssueReaction(ctx context.Context, ticketID, actorType, actorID, emoji string) error {
	_, err := s.db.Pool.Exec(ctx, `
		DELETE FROM ticket_marks WHERE ticket_id = $1 AND tm_actor_type = $2 AND tm_actor_id = $3 AND tm_emoji = $4`,
		ticketID, actorType, actorID, emoji)
	if err != nil {
		return fmt.Errorf("note: снятие реакции с задачи: %w", err)
	}
	return nil
}
