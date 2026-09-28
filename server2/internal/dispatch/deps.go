// Package dispatch — постановка задач агентам в очередь исполнения
// (таблицы 008_dispatch.up.sql: dispatch_jobs/dispatch_messages/dispatch_usage,
// "AgentTask"/"TaskMessage"/"TaskUsageEntry" контракта). Забор задач демоном
// (claim, dj_status: queued -> dispatched) — часть T-028 (daemon-протокол);
// этот пакет только кладёт строки в очередь, отдаёт их наружу для чтения
// (issue-scoped task views) и умеет отменять ещё не забранные запуски.
//
// # API пакета
//
// Enqueue кладёт одну строку в очередь и публикует task:queued. Три
// Cancel*-функции переводят строки в dj_status=cancelled и публикуют
// task:cancelled: CancelActiveForTicket — все активные запуски задачи (при
// снятии исполнителя/переходе в done/cancelled), CancelActiveForConvo — то
// же самое по chat-сессии (домен chat), CancelJob — один конкретный запуск
// (cancelIssueTask). Чтение — GetJob, ActiveForTicket, RunsForTicket,
// UsageForTicket, MessagesForJob, HasPendingForOperativeOnTicket (защита от
// повторной постановки правилом автозапуска, contract §1.9) — реализовано
// на *Store и вызывается напрямую (dispatch.NewStore()...), без надобности в
// Publisher.
package dispatch

import (
	"context"
	"log/slog"

	"github.com/adanman/goosar/server2/internal/realtime"
)

// Deps связывает Store с realtime.Publisher: сам Store — чистый SQL-слой без
// побочных эффектов, а публикация событий требует канала наружу, поэтому
// живёт уровнем выше.
type Deps struct {
	Store     *Store
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(), Publisher: pub, Logger: logger}
}

// taskEvent — общая форма пуша по задаче в realtime (contract §2.1: `task:*`
// -> `{task_id, agent_id, issue_id, chat_session_id?, status}`). Отдельный
// тип вместо ad-hoc map на каждый вызов, чтобы Enqueue/notifyCancelled не
// расходились в написании ключей.
type taskEvent struct {
	TaskID        string `json:"task_id"`
	AgentID       string `json:"agent_id"`
	IssueID       string `json:"issue_id,omitempty"`
	ChatSessionID string `json:"chat_session_id,omitempty"`
	Status        string `json:"status"`
}

// Enqueue — единственная точка постановки в очередь для всех вызывающих
// (назначение/статус задачи, комментарий-триггер, чат, автопилот,
// quick-create): вставляет строку через Store.insert, затем публикует
// task:queued в комнату воркспейса. q — *store.Store.Pool или pgx.Tx домена
// task/chat, поэтому вставка в их собственные таблицы и в dispatch_jobs
// может идти одной транзакцией.
func (d *Deps) Enqueue(ctx context.Context, q Querier, spec JobSpec) (JobID, error) {
	id, err := d.Store.insert(ctx, q, spec)
	if err != nil {
		return "", err
	}
	d.notify(spec.WorkspaceID, "task:queued", taskEvent{
		TaskID: id, AgentID: spec.OperativeID, IssueID: spec.TicketID,
		ChatSessionID: spec.ConvoID, Status: string(StatusQueued),
	})
	return id, nil
}

// CancelActiveForTicket отменяет все активные запуски задачи; вызывается
// доменом task при снятии исполнителя-агента/отряда или переходе в
// done/cancelled.
func (d *Deps) CancelActiveForTicket(ctx context.Context, q Querier, workspaceID, ticketID string) ([]Job, error) {
	jobs, err := d.Store.CancelActiveForTicket(ctx, q, ticketID)
	return jobs, d.notifyCancelled(workspaceID, jobs, err)
}

// CancelActiveForConvo — то же самое по chat-сессии (см.
// Store.CancelActiveForConvo); домен chat зовёт это при удалении сессии.
func (d *Deps) CancelActiveForConvo(ctx context.Context, q Querier, workspaceID, convoID string) ([]Job, error) {
	jobs, err := d.Store.CancelActiveForConvo(ctx, q, convoID)
	return jobs, d.notifyCancelled(workspaceID, jobs, err)
}

// CancelJob отменяет один конкретный запуск (cancelIssueTask). found=false —
// запуска нет либо он уже терминален.
func (d *Deps) CancelJob(ctx context.Context, q Querier, workspaceID, jobID string) (job Job, found bool, err error) {
	job, found, err = d.Store.CancelJob(ctx, q, jobID)
	if err != nil || !found {
		return Job{}, found, err
	}
	return job, true, d.notifyCancelled(workspaceID, []Job{job}, nil)
}

// notifyCancelled публикует task:cancelled на каждую строку jobs и
// пробрасывает cancelErr как есть — общий хвост трёх Cancel*-методов выше,
// каждый из которых иначе повторял бы один и тот же цикл+проверку ошибки.
func (d *Deps) notifyCancelled(workspaceID string, jobs []Job, cancelErr error) error {
	if cancelErr != nil {
		return cancelErr
	}
	for _, j := range jobs {
		ev := taskEvent{TaskID: j.ID, AgentID: j.OperativeID, Status: string(StatusCancelled)}
		if j.TicketID != nil {
			ev.IssueID = *j.TicketID
		}
		if j.ConvoID != nil {
			ev.ChatSessionID = *j.ConvoID
		}
		d.notify(workspaceID, "task:cancelled", ev)
	}
	return nil
}

func (d *Deps) notify(workspaceID, eventType string, ev taskEvent) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: ev})
}
