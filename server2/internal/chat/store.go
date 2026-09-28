package chat

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

var ErrNotFound = errors.New("chat: не найдено")

// Store — доступ к convos/convo_messages/convo_drafts/convo_pinned_operatives
// (007_chat.up.sql + 140_chat_read_state.up.sql).
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// --- agents (только то, что нужно chat: резолв доступа/runtime) -----------------

// AgentRef — часть operatives, нужная chat для проверки прав вызова и
// постановки задачи в очередь (executor_id).
type AgentRef struct {
	ID             string
	WorkspaceID    string
	ExecutorID     string
	Title          string
	PermissionMode string
	OwnerAccountID *string
	ArchivedAt     *time.Time
}

func (s *Store) GetAgent(ctx context.Context, workspaceID, agentID string) (AgentRef, error) {
	var a AgentRef
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, workspace_id, executor_id, op_title, op_permission_mode, op_owner_account_id, op_archived_at
		FROM operatives WHERE workspace_id = $1 AND id = $2`, workspaceID, agentID).
		Scan(&a.ID, &a.WorkspaceID, &a.ExecutorID, &a.Title, &a.PermissionMode, &a.OwnerAccountID, &a.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentRef{}, ErrNotFound
	}
	if err != nil {
		return AgentRef{}, fmt.Errorf("chat: чтение агента: %w", err)
	}
	return a, nil
}

// CanInvoke — пробел спецификации (см. server2/docs/decisions.md): контракт
// не описывает алгоритм проверки доступа "invoke" к агенту, только форму
// operatives/operative_targets. Решение: owner/admin воркспейса могут
// вызывать любого агента; иначе — приватный агент вызывается только своим
// владельцем (op_owner_account_id), публичный — если для agentID есть
// подходящая цель в operative_targets (весь воркспейс, сам actorID как
// member, либо отряд из crew_members, где actorID состоит участником).
func (s *Store) CanInvoke(ctx context.Context, a AgentRef, actorID string, role httpapi.Role) (bool, error) {
	if httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return true, nil
	}
	if a.PermissionMode == "private" {
		return a.OwnerAccountID != nil && *a.OwnerAccountID == actorID, nil
	}
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM operative_targets ot
			WHERE ot.operative_id = $1 AND (
				ot.opt_target_type = 'workspace'
				OR (ot.opt_target_type = 'member' AND ot.opt_target_id = $2)
				OR (ot.opt_target_type = 'team' AND EXISTS(
					SELECT 1 FROM crew_members cm
					WHERE cm.crew_id = ot.opt_target_id AND cm.cm_member_type = 'member' AND cm.cm_member_id = $2))
			)
		)`, a.ID, actorID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("chat: проверка доступа к агенту: %w", err)
	}
	return exists, nil
}

// --- sessions --------------------------------------------------------------------

// Session — форма components/schemas/ChatSession.
type Session struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	AgentID     string    `json:"agent_id"`
	CreatorID   string    `json:"creator_id"`
	ProjectID   *string   `json:"project_id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	Pinned      bool      `json:"pinned"`
	LastReadAt  time.Time `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const sessionColumns = `id, workspace_id, operative_id, cv_creator_account_id, initiative_id, cv_title,
	cv_status, cv_pinned, cv_last_read_at, created_at, updated_at`

func scanSession(row pgx.Row) (Session, error) {
	var s Session
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.AgentID, &s.CreatorID, &s.ProjectID, &s.Title,
		&s.Status, &s.Pinned, &s.LastReadAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return Session{}, err
	}
	return s, nil
}

type CreateSessionParams struct {
	WorkspaceID string
	AgentID     string
	CreatorID   string
	ProjectID   *string
	Title       string
}

func (s *Store) CreateSession(ctx context.Context, p CreateSessionParams) (Session, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO convos (workspace_id, operative_id, cv_creator_account_id, initiative_id, cv_title)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+sessionColumns,
		p.WorkspaceID, p.AgentID, p.CreatorID, p.ProjectID, p.Title)
	return scanSession(row)
}

func (s *Store) GetSession(ctx context.Context, workspaceID, id string) (Session, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+sessionColumns+` FROM convos WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("chat: получение сессии: %w", err)
	}
	return sess, nil
}

// LastMessage — components/schemas/ChatLastMessage.
type LastMessage struct {
	Content       string    `json:"content"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
	FailureReason *string   `json:"failure_reason"`
	MessageKind   string    `json:"message_kind"`
}

// SessionListItem — Session + campos только для списковой выдачи.
type SessionListItem struct {
	Session
	HasUnread   bool         `json:"has_unread"`
	UnreadCount int          `json:"unread_count"`
	LastMessage *LastMessage `json:"last_message"`
}

// ListSessions — сессии, созданные accountID, доступные ему по агенту
// (см. CanInvoke), опционально включая архивные.
func (s *Store) ListSessions(ctx context.Context, workspaceID, accountID string, includeArchived bool) ([]SessionListItem, error) {
	statusClause := "c.cv_status = 'active'"
	if includeArchived {
		statusClause = "TRUE"
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+prefixColumns("c", sessionColumns)+`,
			o.op_permission_mode, o.op_owner_account_id,
			(SELECT count(*) FROM convo_messages m WHERE m.convo_id = c.id AND m.created_at > c.cv_last_read_at AND m.cvm_role <> 'user') AS unread_count,
			lm.cvm_body, lm.cvm_role, lm.created_at, lm.cvm_failure_reason, lm.cvm_kind
		FROM convos c
		JOIN operatives o ON o.id = c.operative_id
		LEFT JOIN LATERAL (
			SELECT cvm_body, cvm_role, created_at, cvm_failure_reason, cvm_kind
			FROM convo_messages WHERE convo_id = c.id ORDER BY created_at DESC LIMIT 1
		) lm ON TRUE
		WHERE c.workspace_id = $1 AND c.cv_creator_account_id = $2 AND `+statusClause+`
		ORDER BY c.cv_pinned DESC, c.updated_at DESC`, workspaceID, accountID)
	if err != nil {
		return nil, fmt.Errorf("chat: список сессий: %w", err)
	}
	defer rows.Close()
	var out []SessionListItem
	for rows.Next() {
		var item SessionListItem
		var permMode string
		var ownerID *string
		var lmBody, lmRole, lmKind *string
		var lmCreatedAt *time.Time
		var lmFailure *string
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.AgentID, &item.CreatorID, &item.ProjectID,
			&item.Title, &item.Status, &item.Pinned, &item.LastReadAt, &item.CreatedAt, &item.UpdatedAt,
			&permMode, &ownerID, &item.UnreadCount, &lmBody, &lmRole, &lmCreatedAt, &lmFailure, &lmKind); err != nil {
			return nil, err
		}
		item.HasUnread = item.UnreadCount > 0
		if lmBody != nil {
			item.LastMessage = &LastMessage{Content: *lmBody, Role: *lmRole, CreatedAt: *lmCreatedAt, FailureReason: lmFailure, MessageKind: *lmKind}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func prefixColumns(alias, cols string) string {
	// sessionColumns не содержит запятых внутри идентификаторов, так что
	// простой подстановки алиаса на каждое поле достаточно для внутреннего
	// использования этим файлом (не общий SQL-хелпер).
	out := ""
	first := true
	for _, c := range splitColumns(cols) {
		if !first {
			out += ", "
		}
		out += alias + "." + c
		first = false
	}
	return out
}

func splitColumns(cols string) []string {
	var out []string
	start := 0
	depth := 0
	for i, r := range cols {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, trimSpace(cols[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, trimSpace(cols[start:]))
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

type UpdateSessionPatch struct {
	Title     *string
	ProjectID *string
	HasProj   bool
}

func (s *Store) UpdateSession(ctx context.Context, workspaceID, id string, p UpdateSessionPatch) (Session, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE convos SET
			cv_title = COALESCE($3, cv_title),
			initiative_id = CASE WHEN $4 THEN $5 ELSE initiative_id END,
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+sessionColumns, workspaceID, id, p.Title, p.HasProj, p.ProjectID)
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Store) SetPinned(ctx context.Context, workspaceID, id string, pinned bool) (Session, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE convos SET cv_pinned = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+sessionColumns, workspaceID, id, pinned)
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Store) SetArchived(ctx context.Context, workspaceID, id string, archived bool) (Session, error) {
	status := "active"
	if archived {
		status = "archived"
	}
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if archived {
			if _, err := tx.Exec(ctx, `DELETE FROM convo_channel_links WHERE convo_id = $1`, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE convos SET cv_status = $3, updated_at = now() WHERE workspace_id = $1 AND id = $2`,
			workspaceID, id, status)
		return err
	})
	if err != nil {
		return Session{}, fmt.Errorf("chat: архивация сессии: %w", err)
	}
	return s.GetSession(ctx, workspaceID, id)
}

// DeleteSession удаляет сессию (каскадом удаляются её сообщения/черновики/
// привязки канала через ON DELETE CASCADE). Отмена незавершённых задач —
// обязанность вызывающего хендлера (через Dispatch), до вызова этого метода.
func (s *Store) DeleteSession(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM convos WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("chat: удаление сессии: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkSessionRead(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE convos SET cv_last_read_at = now() WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("chat: отметка сессии прочитанной: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountMessages — сколько сообщений уже есть в сессии (используется, чтобы
// понять, что отправляемое сообщение — первое, и запустить заголовок).
func (s *Store) CountMessages(ctx context.Context, convoID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM convo_messages WHERE convo_id = $1`, convoID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("chat: подсчёт сообщений: %w", err)
	}
	return n, nil
}

// --- messages --------------------------------------------------------------------

// Attachment — components/schemas/ChatAttachment (минимальный вид,
// хранится в отдельной таблице assets, домен asset её владелец — chat
// только читает и, при отправке сообщения, "заявляет" незанятые строки на
// message_id, см. handlers.go ClaimAttachments).
type Attachment struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Filename    string    `json:"filename"`
	URL         string    `json:"url"`
	DownloadURL string    `json:"download_url"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) attachmentsForMessage(ctx context.Context, messageID string) ([]Attachment, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, as_filename, as_storage_uri, as_download_path, as_content_type, as_size_bytes, created_at
		FROM assets WHERE convo_message_id = $1 ORDER BY created_at`, messageID)
	if err != nil {
		return nil, fmt.Errorf("chat: вложения сообщения: %w", err)
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.Filename, &a.URL, &a.DownloadURL, &a.ContentType, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Message — форма components/schemas/ChatMessage.
type Message struct {
	ID            string       `json:"id"`
	ConvoID       string       `json:"chat_session_id"`
	Role          string       `json:"role"`
	Content       string       `json:"content"`
	TaskID        *string      `json:"task_id"`
	CreatedAt     time.Time    `json:"created_at"`
	FailureReason *string      `json:"failure_reason"`
	ElapsedMs     *int         `json:"elapsed_ms"`
	MessageKind   string       `json:"message_kind"`
	Attachments   []Attachment `json:"attachments"`
}

const messageColumns = `id, convo_id, cvm_role, cvm_body, dispatch_job_id, created_at, cvm_failure_reason, cvm_elapsed_ms, cvm_kind`

func scanMessage(row pgx.Row) (Message, error) {
	var m Message
	if err := row.Scan(&m.ID, &m.ConvoID, &m.Role, &m.Content, &m.TaskID, &m.CreatedAt, &m.FailureReason, &m.ElapsedMs, &m.MessageKind); err != nil {
		return Message{}, err
	}
	return m, nil
}

type CreateMessageParams struct {
	ConvoID string
	Role    string
	Content string
	TaskID  *string
	Kind    string // message|no_response, по умолчанию message
}

func (s *Store) CreateMessage(ctx context.Context, p CreateMessageParams) (Message, error) {
	kind := p.Kind
	if kind == "" {
		kind = "message"
	}
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO convo_messages (convo_id, cvm_role, cvm_body, dispatch_job_id, cvm_kind)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+messageColumns, p.ConvoID, p.Role, p.Content, p.TaskID, kind)
	m, err := scanMessage(row)
	if err != nil {
		return Message{}, fmt.Errorf("chat: создание сообщения: %w", err)
	}
	m.Attachments = []Attachment{}
	if _, err := s.db.Pool.Exec(ctx, `UPDATE convos SET updated_at = now() WHERE id = $1`, p.ConvoID); err != nil {
		return Message{}, fmt.Errorf("chat: обновление updated_at сессии: %w", err)
	}
	return m, nil
}

// setMessageOutcome заполняет cvm_failure_reason/cvm_elapsed_ms уже
// созданного сообщения (AppendAgentReply вызывает это вторым шагом, когда
// протокол daemon знает длительность/причину сбоя запуска).
func (s *Store) setMessageOutcome(ctx context.Context, messageID string, failureReason *string, elapsedMs *int) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE convo_messages SET cvm_failure_reason = COALESCE($2, cvm_failure_reason), cvm_elapsed_ms = COALESCE($3, cvm_elapsed_ms)
		WHERE id = $1`, messageID, failureReason, elapsedMs)
	if err != nil {
		return fmt.Errorf("chat: обновление итога сообщения: %w", err)
	}
	return nil
}

// ClaimAttachments привязывает ранее загруженные, ещё не занятые вложения
// сессии к сообщению messageID; возвращает только реально привязанные id
// (контракт: "без дублей/уже занятых").
func (s *Store) ClaimAttachments(ctx context.Context, workspaceID, convoID, messageID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	rows, err := s.db.Pool.Query(ctx, `
		UPDATE assets SET convo_message_id = $1
		WHERE id = ANY($2) AND workspace_id = $3 AND convo_id = $4 AND convo_message_id IS NULL
		RETURNING id`, messageID, ids, workspaceID, convoID)
	if err != nil {
		return nil, fmt.Errorf("chat: привязка вложений: %w", err)
	}
	defer rows.Close()
	claimed := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		claimed = append(claimed, id)
	}
	return claimed, rows.Err()
}

func (s *Store) ListMessages(ctx context.Context, convoID string) ([]Message, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+messageColumns+` FROM convo_messages WHERE convo_id = $1 ORDER BY created_at`, convoID)
	if err != nil {
		return nil, fmt.Errorf("chat: список сообщений: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		att, err := s.attachmentsForMessage(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Attachments = att
	}
	return out, nil
}

// MessagesPage — components/schemas/ChatMessagesPage.
type MessagesPage struct {
	Messages   []Message  `json:"messages"`
	Limit      int        `json:"limit"`
	HasMore    bool       `json:"has_more"`
	NextCursor *PageCursor `json:"next_cursor"`
}

type PageCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

// ListMessagesPage — курсорная пагинация назад по времени (before_created_at,
// before_id), результат в хронологическом порядке внутри страницы.
func (s *Store) ListMessagesPage(ctx context.Context, convoID string, limit int, before *PageCursor) (MessagesPage, error) {
	var rows pgx.Rows
	var err error
	// Берём limit+1 в убывающем порядке, чтобы узнать has_more, затем
	// разворачиваем в хронологический порядок для ответа.
	if before != nil {
		rows, err = s.db.Pool.Query(ctx, `
			SELECT `+messageColumns+` FROM convo_messages
			WHERE convo_id = $1 AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC LIMIT $4`, convoID, before.CreatedAt, before.ID, limit+1)
	} else {
		rows, err = s.db.Pool.Query(ctx, `
			SELECT `+messageColumns+` FROM convo_messages
			WHERE convo_id = $1
			ORDER BY created_at DESC, id DESC LIMIT $2`, convoID, limit+1)
	}
	if err != nil {
		return MessagesPage{}, fmt.Errorf("chat: постраничный список сообщений: %w", err)
	}
	defer rows.Close()
	var desc []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return MessagesPage{}, err
		}
		desc = append(desc, m)
	}
	if err := rows.Err(); err != nil {
		return MessagesPage{}, err
	}
	page := MessagesPage{Limit: limit}
	if len(desc) > limit {
		page.HasMore = true
		desc = desc[:limit]
	}
	page.Messages = make([]Message, len(desc))
	for i, m := range desc {
		page.Messages[len(desc)-1-i] = m
	}
	for i := range page.Messages {
		att, err := s.attachmentsForMessage(ctx, page.Messages[i].ID)
		if err != nil {
			return MessagesPage{}, err
		}
		page.Messages[i].Attachments = att
	}
	if page.HasMore && len(page.Messages) > 0 {
		oldest := page.Messages[0]
		page.NextCursor = &PageCursor{CreatedAt: oldest.CreatedAt, ID: oldest.ID}
	}
	return page, nil
}

// --- pending tasks -----------------------------------------------------------------

type PendingTask struct {
	TaskID    string    `json:"task_id"`
	Status    string    `json:"status"`
	ChatSessionID string `json:"chat_session_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var activeDispatchStatuses = []string{"queued", "dispatched", "waiting_local_directory", "running", "deferred"}

// PendingForSession — незавершённая задача сессии, если есть.
func (s *Store) PendingForSession(ctx context.Context, convoID string) (*PendingTask, error) {
	var t PendingTask
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, dj_status, created_at FROM dispatch_jobs
		WHERE convo_id = $1 AND dj_status = ANY($2)
		ORDER BY created_at DESC LIMIT 1`, convoID, activeDispatchStatuses).
		Scan(&t.TaskID, &t.Status, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chat: незавершённая задача сессии: %w", err)
	}
	return &t, nil
}

// PendingForUser — все незавершённые чат-задачи пользователя в воркспейсе.
func (s *Store) PendingForUser(ctx context.Context, workspaceID, accountID string) ([]PendingTask, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT dj.id, dj.dj_status, dj.convo_id, dj.created_at
		FROM dispatch_jobs dj
		JOIN convos c ON c.id = dj.convo_id
		WHERE dj.workspace_id = $1 AND dj.dj_kind = 'chat' AND c.cv_creator_account_id = $2 AND dj.dj_status = ANY($3)
		ORDER BY dj.created_at DESC`, workspaceID, accountID, activeDispatchStatuses)
	if err != nil {
		return nil, fmt.Errorf("chat: незавершённые задачи пользователя: %w", err)
	}
	defer rows.Close()
	out := []PendingTask{}
	for rows.Next() {
		var t PendingTask
		if err := rows.Scan(&t.TaskID, &t.Status, &t.ChatSessionID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) HasPendingForUser(ctx context.Context, workspaceID, accountID string) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM dispatch_jobs dj JOIN convos c ON c.id = dj.convo_id
			WHERE dj.workspace_id = $1 AND dj.dj_kind = 'chat' AND c.cv_creator_account_id = $2 AND dj.dj_status = ANY($3)
		)`, workspaceID, accountID, activeDispatchStatuses).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("chat: проверка незавершённых задач: %w", err)
	}
	return exists, nil
}

// --- draft restores ----------------------------------------------------------------

// DraftRestore — components/schemas/ChatDraftRestore. Вложения не
// восстанавливаются (пробел спецификации: assets не хранит ссылку на
// convo_drafts — см. server2/docs/decisions.md), поле всегда [].
type DraftRestore struct {
	ID        string    `json:"id"`
	ConvoID   string    `json:"chat_session_id"`
	TaskID    string    `json:"task_id"`
	Content   string    `json:"content"`
	Attachments []Attachment `json:"attachments"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateDraftRestore — внутренний API для домена, реализующего
// POST /api/tasks/{taskId}/cancel (cancelTaskByUser, тег Tasks — вне
// границ chat, см. server2/docs/decisions.md): когда отменяемая задача
// чатовая, тот домен зовёт это, чтобы сохранить потерянное пользовательское
// сообщение как черновик для восстановления в поле ввода.
func (s *Store) CreateDraftRestore(ctx context.Context, convoID, taskID, content string) (DraftRestore, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO convo_drafts (convo_id, dispatch_job_id, cvd_body)
		VALUES ($1, $2, $3)
		RETURNING id, convo_id, dispatch_job_id, cvd_body, created_at`, convoID, taskID, content)
	var d DraftRestore
	if err := row.Scan(&d.ID, &d.ConvoID, &d.TaskID, &d.Content, &d.CreatedAt); err != nil {
		return DraftRestore{}, fmt.Errorf("chat: создание черновика восстановления: %w", err)
	}
	d.Attachments = []Attachment{}
	return d, nil
}

func (s *Store) ListDraftRestores(ctx context.Context, convoID string) ([]DraftRestore, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, convo_id, dispatch_job_id, cvd_body, created_at FROM convo_drafts
		WHERE convo_id = $1 ORDER BY created_at DESC`, convoID)
	if err != nil {
		return nil, fmt.Errorf("chat: список черновиков восстановления: %w", err)
	}
	defer rows.Close()
	out := []DraftRestore{}
	for rows.Next() {
		var d DraftRestore
		var taskID *string
		if err := rows.Scan(&d.ID, &d.ConvoID, &taskID, &d.Content, &d.CreatedAt); err != nil {
			return nil, err
		}
		if taskID != nil {
			d.TaskID = *taskID
		}
		d.Attachments = []Attachment{}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ConsumeDraftRestore(ctx context.Context, convoID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM convo_drafts WHERE convo_id = $1 AND id = $2`, convoID, id)
	if err != nil {
		return fmt.Errorf("chat: удаление черновика восстановления: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- pinned agents -----------------------------------------------------------------

// PinnedLimit — лимит закреплённых агентов на пользователя (контракт: 5).
const PinnedLimit = 5

type PinnedAgent struct {
	AgentID  string  `json:"agent_id"`
	Position float64 `json:"position"`
}

func (s *Store) ListPinnedAgents(ctx context.Context, accountID string) ([]PinnedAgent, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT operative_id, cvp_position FROM convo_pinned_operatives
		WHERE account_id = $1 ORDER BY cvp_position`, accountID)
	if err != nil {
		return nil, fmt.Errorf("chat: список закреплённых агентов: %w", err)
	}
	defer rows.Close()
	out := []PinnedAgent{}
	for rows.Next() {
		var p PinnedAgent
		if err := rows.Scan(&p.AgentID, &p.Position); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CountPinnedAgents(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM convo_pinned_operatives WHERE account_id = $1`, accountID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("chat: подсчёт закреплённых агентов: %w", err)
	}
	return n, nil
}

func (s *Store) PinAgent(ctx context.Context, accountID, agentID string) (PinnedAgent, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO convo_pinned_operatives (account_id, operative_id, cvp_position)
		VALUES ($1, $2, COALESCE((SELECT max(cvp_position) + 1 FROM convo_pinned_operatives WHERE account_id = $1), 0))
		ON CONFLICT (account_id, operative_id) DO UPDATE SET cvp_position = convo_pinned_operatives.cvp_position
		RETURNING operative_id, cvp_position`, accountID, agentID)
	var p PinnedAgent
	if err := row.Scan(&p.AgentID, &p.Position); err != nil {
		return PinnedAgent{}, fmt.Errorf("chat: закрепление агента: %w", err)
	}
	return p, nil
}

func (s *Store) UnpinAgent(ctx context.Context, accountID, agentID string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM convo_pinned_operatives WHERE account_id = $1 AND operative_id = $2`, accountID, agentID)
	if err != nil {
		return fmt.Errorf("chat: открепление агента: %w", err)
	}
	return nil
}

// --- workspace/actor helper для контекст-снапшота dispatch ------------------------

// BuildContextSnapshot — минимальный dj_context_snapshot для чатовых задач:
// заголовок проекта сессии (если привязан) и имя инициатора. Контракт не
// специфицирует состав снапшота (см. 008_dispatch.up.sql комментарий и
// server2/docs/decisions.md) — решение зафиксировано здесь.
func (s *Store) BuildContextSnapshot(ctx context.Context, sess Session, initiatorName string) json.RawMessage {
	snap := map[string]any{"initiator_name": initiatorName, "chat_session_title": sess.Title}
	if sess.ProjectID != nil {
		var title string
		if err := s.db.Pool.QueryRow(ctx, `SELECT init_title FROM initiatives WHERE id = $1`, *sess.ProjectID).Scan(&title); err == nil {
			snap["project_title"] = title
		}
	}
	body, _ := json.Marshal(snap)
	return body
}
