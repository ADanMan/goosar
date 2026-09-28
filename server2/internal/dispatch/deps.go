// Package dispatch — постановка задач агентам в очередь исполнения
// (таблицы 008_dispatch.up.sql: dispatch_jobs/dispatch_messages/dispatch_usage,
// "AgentTask"/"TaskMessage"/"TaskUsageEntry" контракта). Забор задач демоном
// (claim, dj_status: queued -> dispatched) — часть T-028 (daemon-протокол);
// этот пакет только кладёт строки в очередь, отдаёт их наружу для чтения
// (issue-scoped task views) и умеет отменять ещё не забранные запуски.
//
// Публичный API пакета:
//
//	Enqueue(ctx, q Querier, spec JobSpec) (JobID string, err error)
//		Ставит одну задачу в очередь (dj_status=queued) и публикует
//		task:queued в realtime. q — *store.Store.Pool или pgx.Tx: домен
//		task вызывает Enqueue внутри своей же транзакции создания/обновления
//		задачи, чтобы запись в tickets и запись в dispatch_jobs были атомарны.
//	CancelActiveForTicket(ctx, q Querier, ticketID, reason string) ([]JobID, error)
//		Отменяет (dj_status=cancelled) все ещё не завершённые запуски задачи
//		(queued/dispatched/waiting_local_directory/running/deferred) —
//		вызывается доменом task при снятии агента-исполнителя или смене
//		статуса на done/cancelled, публикует task:cancelled на каждую.
//	CancelActiveForConvo(ctx, q Querier, workspaceID, convoID string) ([]Job, error)
//		Тот же приём, что CancelActiveForTicket, по chat-сессии — домен chat
//		(T-027) зовёт это при удалении сессии (deleteChatSession).
//	CancelJob(ctx, q Querier, jobID string) (Job, error)
//		Отменяет один конкретный ещё активный запуск (cancelIssueTask).
//	ActiveForTicket(ctx, q Querier, ticketID string) ([]Job, error)
//	RunsForTicket(ctx, q Querier, ticketID string) ([]Job, error)
//	UsageForTicket(ctx, q Querier, ticketID string) (Usage, error)
//	MessagesForJob(ctx, q Querier, jobID string) ([]Message, error)
//	GetJob(ctx, q Querier, jobID string) (Job, error)
//	HasPendingForOperativeOnTicket(ctx, q Querier, operativeID, ticketID string) (bool, error)
//		Защита от повторной постановки — правило автозапуска (contract §1.9)
//		требует не создавать второй ожидающий запуск того же агента на той
//		же задаче.
package dispatch

import (
	"context"
	"log/slog"

	"github.com/adanman/goosar/server2/internal/realtime"
)

// Deps — зависимости пакета dispatch.
type Deps struct {
	Store     *Store
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(), Publisher: pub, Logger: logger}
}

// Enqueue — точка входа пакета для всех вызывающих (назначение/статус задачи,
// комментарий-триггер, чат, автопилот, quick-create): вставляет строку
// dispatch_jobs через Store.insert и публикует task:queued в комнату
// воркспейса, форма ровно как в contract §2.1: {task_id, agent_id, issue_id,
// chat_session_id?, status}. q — *store.Store.Pool или pgx.Tx домена task,
// так что вставка в tickets/ticket_notes и в dispatch_jobs может быть одной
// транзакцией.
func (d *Deps) Enqueue(ctx context.Context, q Querier, spec JobSpec) (JobID, error) {
	id, err := d.Store.insert(ctx, q, spec)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"task_id":  id,
		"agent_id": spec.OperativeID,
		"issue_id": spec.TicketID,
		"status":   string(StatusQueued),
	}
	if spec.ConvoID != "" {
		payload["chat_session_id"] = spec.ConvoID
	}
	d.publish(spec.WorkspaceID, "task:queued", payload)
	return id, nil
}

// CancelActiveForTicket отменяет все активные запуски задачи и публикует
// task:cancelled на каждый отменённый запуск. Домен task зовёт это при
// снятии исполнителя-агента/отряда с задачи или переходе в done/cancelled.
func (d *Deps) CancelActiveForTicket(ctx context.Context, q Querier, workspaceID, ticketID string) ([]Job, error) {
	jobs, err := d.Store.CancelActiveForTicket(ctx, q, ticketID)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		d.publishCancelled(workspaceID, j)
	}
	return jobs, nil
}

// CancelActiveForConvo — как CancelActiveForTicket, но по chat-сессии (см.
// Store.CancelActiveForConvo); домен chat зовёт это при удалении сессии.
func (d *Deps) CancelActiveForConvo(ctx context.Context, q Querier, workspaceID, convoID string) ([]Job, error) {
	jobs, err := d.Store.CancelActiveForConvo(ctx, q, convoID)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		d.publishCancelled(workspaceID, j)
	}
	return jobs, nil
}

// CancelJob отменяет один конкретный запуск (cancelIssueTask) и публикует
// task:cancelled. found=false — запуска нет либо он уже терминален.
func (d *Deps) CancelJob(ctx context.Context, q Querier, workspaceID, jobID string) (Job, bool, error) {
	j, found, err := d.Store.CancelJob(ctx, q, jobID)
	if err != nil || !found {
		return Job{}, found, err
	}
	d.publishCancelled(workspaceID, j)
	return j, true, nil
}

func (d *Deps) publishCancelled(workspaceID string, j Job) {
	payload := map[string]any{
		"task_id":  j.ID,
		"agent_id": j.OperativeID,
		"status":   string(StatusCancelled),
	}
	if j.TicketID != nil {
		payload["issue_id"] = *j.TicketID
	}
	if j.ConvoID != nil {
		payload["chat_session_id"] = *j.ConvoID
	}
	d.publish(workspaceID, "task:cancelled", payload)
}

func (d *Deps) publish(workspaceID, eventType string, payload any) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}
