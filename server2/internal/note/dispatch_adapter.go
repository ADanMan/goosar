package note

import (
	"context"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/store"
)

// dispatchAdapter реализует Dispatcher поверх internal/dispatch (T-027,
// пакет соседнего реализатора — появился в ходе этой сессии, см.
// server2/docs/decisions.md). note остаётся единственным, кто знает, как
// правило автозапуска комментария (contract §1.9/§1.10) превращается в
// dispatch.JobSpec; сам dispatch ничего не знает о комментариях.
type dispatchAdapter struct {
	deps *dispatch.Deps
	db   *store.Store
}

// NewDispatchAdapter оборачивает *dispatch.Deps в note.Dispatcher.
// internal/app вызывает это (и Deps.SetDispatcher) после сборки обоих
// доменов.
func NewDispatchAdapter(deps *dispatch.Deps, db *store.Store) Dispatcher {
	return &dispatchAdapter{deps: deps, db: db}
}

// coalescedOutcome и queuedOutcome именуют два единственных успешных исхода,
// которые эта обёртка умеет отдавать (blocked/deferred определяются выше, в
// triggers.go, до того как Dispatcher вообще вызывается).
var (
	coalescedOutcome = DispatchTriggerResult{Status: "coalesced", ReasonCode: "coalesced"}
	queuedOutcome    = DispatchTriggerResult{Status: "queued", ReasonCode: "queued"}
)

// EnqueueCommentTrigger — единственный метод note.Dispatcher: сперва
// смотрит, нет ли у operative уже незавершённого запуска на этом же тикете
// (тогда триггер "объединяется" с ним, contract §1.10 — coalesced, без
// новой строки dispatch_jobs), иначе кладёт новую строку через
// dispatch.Deps.Enqueue внутри одной и той же connection pool'а note.
func (a *dispatchAdapter) EnqueueCommentTrigger(ctx context.Context, in DispatchTriggerInput) (result DispatchTriggerResult, err error) {
	var hasActiveRun bool
	hasActiveRun, err = a.deps.Store.HasPendingForOperativeOnTicket(ctx, a.db.Pool, in.OperativeID, in.TicketID)
	switch {
	case err != nil:
		return
	case hasActiveRun:
		// Дозапись commentID в dj_coalesced_note_ids существующей строки —
		// за пределами этой сессии: dispatch не даёт отдельного метода
		// записи под это, а собирать SQL по чужой таблице в обход её
		// пакета — не наша область (см. server2/docs/decisions.md).
		result = coalescedOutcome
		return
	}
	_, err = a.deps.Enqueue(ctx, a.db.Pool, a.jobSpecFor(in))
	if err == nil {
		result = queuedOutcome
	}
	return
}

func (a *dispatchAdapter) jobSpecFor(in DispatchTriggerInput) dispatch.JobSpec {
	return dispatch.JobSpec{
		WorkspaceID: in.WorkspaceID, OperativeID: in.OperativeID, ExecutorID: in.ExecutorID,
		TicketID: in.TicketID, Kind: dispatch.KindIssue, TriggerNoteID: in.TriggerNoteID,
		TriggerAuthorType: "member", InitiatorType: "member",
	}
}
