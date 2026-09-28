// Package task реализует тег Issues контракта (`/api/issues/**`,
// `/api/tasks/{taskId}/messages`, `/api/labels/{id}` — только привязка к
// задаче, `/api/properties/{id}` — только значения на задаче,
// `/api/assignee-frequency`), кроме комментариев/реакций/вложений задачи
// (пакет note того же T-027) и полного CRUD меток/свойств/проектов/отрядов
// (другие домены той же сессии — см. server2/docs/decisions.md).
//
// Таблицы — 005_tasks.up.sql (tickets, ticket_tag_links, ticket_notes*,
// ticket_marks, ticket_subscribers, ticket_pr_links, ticket_activity;
// *ticket_notes/note_marks принадлежат пакету note, task их не пишет, только
// читает при слиянии timeline). Постановка агентов в очередь — через
// server2/internal/dispatch, не напрямую.
package task

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена task. Workspaces — тот же *workspace.Store,
// которым пользуется домен workspace (resolveWorkspace/requireMember ниже
// зовут только его уже публичные ResolveID-подобные методы и
// GetMemberByUser — никаких изменений в пакет workspace сверх аддитивного
// IncrementTicketSeq, добавленного для нумерации задач).
type Deps struct {
	Store      *Store
	Workspaces *workspace.Store
	Dispatch   *dispatch.Deps
	Publisher  realtime.Publisher
	Logger     *slog.Logger
}

func New(db *store.Store, ws *workspace.Store, dispatchDeps *dispatch.Deps, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), Workspaces: ws, Dispatch: dispatchDeps, Publisher: pub, Logger: logger}
}
