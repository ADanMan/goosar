package note

import (
	"context"
	"log/slog"

	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// DispatchTriggerInput — то немногое, что note знает о постановке задачи в
// очередь (у самого note нет доступа к dispatch_jobs на запись — это домен
// internal/dispatch, T-027, пакет соседнего реализатора).
type DispatchTriggerInput struct {
	WorkspaceID   string
	TicketID      string
	OperativeID   string
	ExecutorID    string
	Source        string // issue_assignee|mention_agent|mention_squad_leader|thread_parent|conversation_continuation
	TriggerNoteID string
}

// DispatchTriggerResult — исход попытки поставить агента в очередь.
type DispatchTriggerResult struct {
	Status     string // queued|deferred|coalesced|blocked
	ReasonCode string
}

// Dispatcher — минимальная поверхность internal/dispatch, которую note
// использует для запуска агента по правилам §1.9/§1.10 контракта. Локальный
// интерфейс (а не прямая ссылка на dispatch.Enqueue) — чтобы note собирался
// и тестировался до появления internal/dispatch и не создавал цикл, если
// когда-нибудь dispatch тоже захочет знать о комментариях.
type Dispatcher interface {
	EnqueueCommentTrigger(ctx context.Context, in DispatchTriggerInput) (DispatchTriggerResult, error)
}

// Deps — зависимости домена note.
type Deps struct {
	Store      *Store
	Assets     *asset.Store
	Resolver   *wsctx.Resolver
	Publisher  realtime.Publisher
	Dispatcher Dispatcher // nil, пока internal/dispatch не подключён — см. triggers.go
	Logger     *slog.Logger
	db         *store.Store // операции по operative_targets, вне store.go (не таблицы note)
}

func New(db *store.Store, assets *asset.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db),
		Assets:    assets,
		Resolver:  wsctx.New(db),
		Publisher: pub,
		Logger:    logger,
		db:        db,
	}
}

// SetDispatcher подключает internal/dispatch к уже собранным Deps — вызывается
// из internal/app/deps.go, если/когда пакет dispatch появится в сборке (см.
// server2/docs/decisions.md); до этого Dispatcher остаётся nil и
// dispatchOutcome честно возвращает internal_error вместо выдуманного queued.
func (d *Deps) SetDispatcher(disp Dispatcher) { d.Dispatcher = disp }
