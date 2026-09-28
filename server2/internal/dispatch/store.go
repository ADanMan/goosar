package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("dispatch: не найдено")

// Querier — общий интерфейс выполнения SQL, которому удовлетворяют и
// *pgxpool.Pool (через store.Store.Pool), и pgx.Tx. Enqueue и остальные
// функции пакета принимают его напрямую, чтобы вызывающий домен (task) мог
// звать их как отдельным запросом, так и внутри собственной транзакции
// (например создание задачи + постановка в очередь — одна атомарная операция).
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Kind — dj_kind: как задача попала в очередь.
type Kind string

const (
	KindIssue       Kind = "issue"        // назначение/статус задачи, комментарий-триггер
	KindChat        Kind = "chat"         // сообщение в чат-сессии агента
	KindAutopilot   Kind = "autopilot"    // сработавший автопилот
	KindQuickCreate Kind = "quick_create" // POST /api/issues/quick-create
)

// Status — dj_status.
type Status string

const (
	StatusQueued                Status = "queued"
	StatusDispatched            Status = "dispatched"
	StatusWaitingLocalDirectory Status = "waiting_local_directory"
	StatusRunning               Status = "running"
	StatusCompleted             Status = "completed"
	StatusFailed                Status = "failed"
	StatusCancelled             Status = "cancelled"
	StatusDeferred              Status = "deferred"
)

// activeStatuses — ещё не завершённые запуски (contract §8653 getActiveTaskForIssue).
var activeStatuses = []Status{StatusQueued, StatusDispatched, StatusWaitingLocalDirectory, StatusRunning, StatusDeferred}

// JobSpec — вход Enqueue: всё, что нужно записать в dispatch_jobs, чтобы
// демон впоследствии мог забрать и исполнить запуск. Поля сгруппированы по
// каналу постановки (issue/chat/autopilot/quick_create) — заполняются только
// те, что относятся к каналу spec.Kind.
type JobSpec struct {
	WorkspaceID   string
	OperativeID   string // agent this task runs as (dispatch_jobs.operative_id)
	ExecutorID    string // runtime slot (dispatch_jobs.executor_id)
	TicketID      string // NullString-подобно: пусто, если задача не issue-scoped
	InitiativeID  string
	CrewID        string // если запуск — от лица лидера отряда
	ConvoID       string
	SentinelRunID string

	Kind     Kind
	Priority int // по умолчанию 0

	ThreadTitle string

	// issue-канал: контекст комментария-триггера (contract §1.10)
	TriggerNoteID     string
	CoalescedNoteIDs  []string
	TriggerThreadID   string
	TriggerSummary    string
	TriggerAuthorType string // member/agent/system

	// chat-канал
	ChatChannelType string
	ChatInThread    bool
	ChatMessage     string
	ChatIntro       bool

	// quick_create-канал
	QuickCreatePrompt   string
	QuickCreatePriority string
	QuickCreateDueDate  *time.Time

	HandoffNote string
	McpPolicy   json.RawMessage

	InitiatorType string // member/agent/system
	InitiatorID   string
	Attribution   json.RawMessage
	ConnectedApps json.RawMessage
	RepoRefs      json.RawMessage

	// ContextSnapshot — точка-в-времени контекст, который демону нужен при
	// claim (заголовок проекта/описание, имя запросившего, workspace context,
	// идентификатор родительской задачи, ...) — см. комментарий в
	// 008_dispatch.up.sql. Собирается вызывающим доменом один раз на момент
	// постановки, а не заново на каждый claim/retry.
	ContextSnapshot json.RawMessage

	ParentJobID string // для ретраев/ререна: новая строка ссылается на предыдущую
	IsLeader    bool   // это запуск лидера отряда (enqueueSquadLeaderTask)
	MaxAttempts int    // по умолчанию 2 (см. dj_max_attempts DEFAULT)

	WorkDir string
}

// JobID — идентификатор строки dispatch_jobs.
type JobID = string

// Job — строка dispatch_jobs в форме, достаточной для отдачи наружу
// (schemas.AgentTask и внутренние проверки автозапуска).
type Job struct {
	ID            string
	WorkspaceID   string
	OperativeID   string
	ExecutorID    string
	TicketID      *string
	InitiativeID  *string
	CrewID        *string
	ConvoID       *string
	Kind          Kind
	Status        Status
	Priority      int
	ThreadTitle   *string
	DispatchedAt  *time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	Result        json.RawMessage
	Error         *string
	FailureReason *string
	Attempt       int
	MaxAttempts   int
	ParentJobID   *string
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// DeliveredNoteIDs — dj_delivered_note_ids (schemas.AgentTask.delivered_comment_ids,
	// обязательное поле контракта). Добавлено аддитивно T-028
	// (internal/agent.listAgentTasks — единственный текущий потребитель).
	DeliveredNoteIDs []string
}

// Store — доступ к dispatch_jobs/dispatch_messages/dispatch_usage. Не держит
// пул сам — принимает Querier на каждый вызов (см. Querier).
type Store struct{}

func NewStore() *Store { return &Store{} }

const jobColumns = `id, workspace_id, operative_id, executor_id, ticket_id, initiative_id, crew_id, convo_id,
	dj_kind, dj_status, dj_priority, dj_thread_title, dj_dispatched_at, dj_started_at, dj_completed_at,
	dj_result, dj_error, dj_failure_reason, dj_attempt, dj_max_attempts, dj_parent_job_id, created_at, updated_at,
	dj_delivered_note_ids`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var kind, status string
	var result []byte
	if err := row.Scan(&j.ID, &j.WorkspaceID, &j.OperativeID, &j.ExecutorID, &j.TicketID, &j.InitiativeID,
		&j.CrewID, &j.ConvoID, &kind, &status, &j.Priority, &j.ThreadTitle, &j.DispatchedAt, &j.StartedAt,
		&j.CompletedAt, &result, &j.Error, &j.FailureReason, &j.Attempt, &j.MaxAttempts, &j.ParentJobID,
		&j.CreatedAt, &j.UpdatedAt, &j.DeliveredNoteIDs); err != nil {
		return Job{}, err
	}
	j.Kind = Kind(kind)
	j.Status = Status(status)
	if len(result) > 0 {
		j.Result = result
	}
	return j, nil
}

// insert вставляет новую строку dispatch_jobs (dj_status='queued') и
// возвращает её id; публикация realtime-события — в Deps.Enqueue (обёртке
// над этим методом), потому что событию нужен Publisher, которого у Store
// (чистого SQL-слоя) нет.
func (s *Store) insert(ctx context.Context, q Querier, spec JobSpec) (JobID, error) {
	if spec.WorkspaceID == "" || spec.OperativeID == "" || spec.ExecutorID == "" {
		return "", fmt.Errorf("dispatch: workspace_id/operative_id/executor_id обязательны")
	}
	if spec.Kind == "" {
		spec.Kind = KindIssue
	}
	maxAttempts := spec.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 2
	}
	ctxSnap := spec.ContextSnapshot
	if len(ctxSnap) == 0 {
		ctxSnap = json.RawMessage(`{}`)
	}
	attribution := spec.Attribution
	if len(attribution) == 0 {
		attribution = json.RawMessage(`{}`)
	}
	connectedApps := spec.ConnectedApps
	if len(connectedApps) == 0 {
		connectedApps = json.RawMessage(`[]`)
	}
	repoRefs := spec.RepoRefs
	if len(repoRefs) == 0 {
		repoRefs = json.RawMessage(`[]`)
	}

	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO dispatch_jobs (
			workspace_id, operative_id, executor_id, ticket_id, initiative_id, crew_id, convo_id, sentinel_run_id,
			dj_kind, dj_priority, dj_thread_title,
			dj_trigger_note_id, dj_coalesced_note_ids, dj_trigger_thread_id, dj_trigger_summary, dj_trigger_author_type,
			dj_chat_channel_type, dj_chat_in_thread, dj_chat_message, dj_chat_intro,
			dj_quick_create_prompt, dj_quick_create_priority, dj_quick_create_due_date,
			dj_handoff_note, dj_mcp_policy, dj_initiator_type, dj_initiator_id,
			dj_attribution, dj_connected_apps, dj_repo_refs, dj_context_snapshot,
			dj_parent_job_id, dj_is_leader, dj_max_attempts, dj_work_dir
		) VALUES (
			$1, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, NULLIF($6,'')::uuid, NULLIF($7,'')::uuid, NULLIF($8,'')::uuid,
			$9, $10, NULLIF($11,''),
			NULLIF($12,'')::uuid, $13, NULLIF($14,'')::uuid, NULLIF($15,''), NULLIF($16,''),
			NULLIF($17,''), $18, NULLIF($19,''), $20,
			NULLIF($21,''), NULLIF($22,''), $23,
			NULLIF($24,''), $25, NULLIF($26,''), NULLIF($27,'')::uuid,
			$28, $29, $30, $31,
			NULLIF($32,'')::uuid, $33, $34, NULLIF($35,'')
		) RETURNING id`,
		spec.WorkspaceID, spec.OperativeID, spec.ExecutorID, spec.TicketID, spec.InitiativeID, spec.CrewID, spec.ConvoID, spec.SentinelRunID,
		string(spec.Kind), spec.Priority, spec.ThreadTitle,
		spec.TriggerNoteID, coalescedArray(spec.CoalescedNoteIDs), spec.TriggerThreadID, spec.TriggerSummary, spec.TriggerAuthorType,
		spec.ChatChannelType, spec.ChatInThread, spec.ChatMessage, spec.ChatIntro,
		spec.QuickCreatePrompt, spec.QuickCreatePriority, spec.QuickCreateDueDate,
		spec.HandoffNote, nullableJSON(spec.McpPolicy), spec.InitiatorType, spec.InitiatorID,
		attribution, connectedApps, repoRefs, ctxSnap,
		spec.ParentJobID, spec.IsLeader, maxAttempts, spec.WorkDir,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("dispatch: постановка задачи в очередь: %w", err)
	}
	return id, nil
}

func coalescedArray(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// GetJob — одна строка dispatch_jobs по id.
func (s *Store) GetJob(ctx context.Context, q Querier, jobID string) (Job, error) {
	row := q.QueryRow(ctx, `SELECT `+jobColumns+` FROM dispatch_jobs WHERE id = $1`, jobID)
	j, err := scanJob(row)
	if pgx.ErrNoRows == err {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("dispatch: получение задачи: %w", err)
	}
	return j, nil
}

// ActiveForTicket — незавершённые запуски задачи (getActiveTaskForIssue).
func (s *Store) ActiveForTicket(ctx context.Context, q Querier, ticketID string) ([]Job, error) {
	rows, err := q.Query(ctx, `SELECT `+jobColumns+` FROM dispatch_jobs
		WHERE ticket_id = $1 AND dj_status = ANY($2) ORDER BY created_at DESC`,
		ticketID, statusStrings(activeStatuses))
	if err != nil {
		return nil, fmt.Errorf("dispatch: активные запуски задачи: %w", err)
	}
	return collectJobs(rows)
}

// RunsForTicket — вся история запусков задачи, любой статус (listIssueTaskRuns).
func (s *Store) RunsForTicket(ctx context.Context, q Querier, ticketID string) ([]Job, error) {
	rows, err := q.Query(ctx, `SELECT `+jobColumns+` FROM dispatch_jobs WHERE ticket_id = $1 ORDER BY created_at DESC`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("dispatch: история запусков задачи: %w", err)
	}
	return collectJobs(rows)
}

// RunsForOperative — вся история запусков конкретного агента, по всем
// задачам/каналам (listAgentTasks, T-028, contract §10.7 "GET
// /api/agents/{id}/tasks": "вся история запусков"). Аддитивно к пакету
// T-027, тот же приём, что RunsForTicket.
func (s *Store) RunsForOperative(ctx context.Context, q Querier, operativeID string) ([]Job, error) {
	rows, err := q.Query(ctx, `SELECT `+jobColumns+` FROM dispatch_jobs WHERE operative_id = $1 ORDER BY created_at DESC`, operativeID)
	if err != nil {
		return nil, fmt.Errorf("dispatch: история запусков агента: %w", err)
	}
	return collectJobs(rows)
}

// CancelActiveForOperative отменяет все ещё не завершённые запуски
// конкретного агента, по всем задачам (cancelAgentTasks/archiveAgent,
// T-028). Аддитивно, тот же приём, что CancelActiveForTicket/Convo.
func (s *Store) CancelActiveForOperative(ctx context.Context, q Querier, operativeID string) ([]Job, error) {
	rows, err := q.Query(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'cancelled', updated_at = now()
		WHERE operative_id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns, operativeID, statusStrings(activeStatuses))
	if err != nil {
		return nil, fmt.Errorf("dispatch: отмена активных запусков агента: %w", err)
	}
	return collectJobs(rows)
}

func collectJobs(rows pgx.Rows) ([]Job, error) {
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func statusStrings(statuses []Status) []string {
	out := make([]string, len(statuses))
	for i, st := range statuses {
		out[i] = string(st)
	}
	return out
}

// HasPendingForOperativeOnTicket — есть ли у operativeID уже ожидающий
// (активный) запуск именно на этой задаче — защита от повторной постановки
// правилом автозапуска (contract §1.9, случай 2).
func (s *Store) HasPendingForOperativeOnTicket(ctx context.Context, q Querier, operativeID, ticketID string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM dispatch_jobs
		WHERE operative_id = $1 AND ticket_id = $2 AND dj_status = ANY($3)
	)`, operativeID, ticketID, statusStrings(activeStatuses)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("dispatch: проверка ожидающего запуска: %w", err)
	}
	return exists, nil
}

// CancelActiveForTicket отменяет все ещё не завершённые запуски задачи
// (dj_status -> cancelled). Вызывается доменом task при снятии
// исполнителя-агента/отряда или переходе задачи в done/cancelled. Возвращает
// id отменённых строк — вызывающий домен публикует task:cancelled на каждую
// (у dispatch нет issue_id/agent_id под рукой без второго похода в Job).
func (s *Store) CancelActiveForTicket(ctx context.Context, q Querier, ticketID string) ([]Job, error) {
	rows, err := q.Query(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'cancelled', updated_at = now()
		WHERE ticket_id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns, ticketID, statusStrings(activeStatuses))
	if err != nil {
		return nil, fmt.Errorf("dispatch: отмена активных запусков задачи: %w", err)
	}
	return collectJobs(rows)
}

// CancelActiveForConvo — то же самое (dj_status -> cancelled), но по
// chat-сессии, а не по тикету: используется доменом chat при удалении
// сессии (deleteChatSession отменяет её незавершённые задачи по контракту).
func (s *Store) CancelActiveForConvo(ctx context.Context, q Querier, convoID string) ([]Job, error) {
	rows, err := q.Query(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'cancelled', updated_at = now()
		WHERE convo_id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns, convoID, statusStrings(activeStatuses))
	if err != nil {
		return nil, fmt.Errorf("dispatch: отмена активных запусков сессии: %w", err)
	}
	return collectJobs(rows)
}

// CancelJob отменяет один конкретный запуск, если он ещё активен
// (cancelIssueTask). ErrNotFound — если запуска нет или он уже терминален
// (контракт: 400 "нельзя отменить в текущем состоянии" — вызывающий домен
// сам решает, какой код вернуть по ErrNotFound/found=false).
func (s *Store) CancelJob(ctx context.Context, q Querier, jobID string) (Job, bool, error) {
	row := q.QueryRow(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'cancelled', updated_at = now()
		WHERE id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns, jobID, statusStrings(activeStatuses))
	j, err := scanJob(row)
	if pgx.ErrNoRows == err {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("dispatch: отмена запуска: %w", err)
	}
	return j, true, nil
}

// Usage — сводка токенов/стоимости всех запусков задачи (IssueUsageSummary).
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSDTicks     int64
}

// UsageForTicket суммирует dispatch_usage по всем запускам задачи.
func (s *Store) UsageForTicket(ctx context.Context, q Querier, ticketID string) (Usage, error) {
	var u Usage
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(du.du_input_tokens),0), COALESCE(SUM(du.du_output_tokens),0),
			COALESCE(SUM(du.du_cache_read_tokens),0), COALESCE(SUM(du.du_cache_write_tokens),0),
			COALESCE(SUM(du.du_cost_usd_ticks),0)
		FROM dispatch_usage du
		JOIN dispatch_jobs dj ON dj.id = du.dispatch_job_id
		WHERE dj.ticket_id = $1`, ticketID).
		Scan(&u.InputTokens, &u.OutputTokens, &u.CacheReadTokens, &u.CacheWriteTokens, &u.CostUSDTicks)
	if err != nil {
		return Usage{}, fmt.Errorf("dispatch: сводка использования задачи: %w", err)
	}
	return u, nil
}

// Message — строка dispatch_messages (schemas.TaskMessage).
type Message struct {
	ID        string
	JobID     string
	Seq       int
	Kind      string
	Tool      *string
	Body      *string
	Input     json.RawMessage
	Output    *string
	CreatedAt time.Time
}

// MessagesForJob — стенограмма одного запуска (GET /api/tasks/{taskId}/messages).
func (s *Store) MessagesForJob(ctx context.Context, q Querier, jobID string) ([]Message, error) {
	rows, err := q.Query(ctx, `
		SELECT id, dispatch_job_id, dm_seq, dm_kind, dm_tool, dm_body, dm_input, dm_output, created_at
		FROM dispatch_messages WHERE dispatch_job_id = $1 ORDER BY dm_seq`, jobID)
	if err != nil {
		return nil, fmt.Errorf("dispatch: стенограмма запуска: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var input []byte
		if err := rows.Scan(&m.ID, &m.JobID, &m.Seq, &m.Kind, &m.Tool, &m.Body, &input, &m.Output, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(input) > 0 {
			m.Input = input
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
