// Package chat реализует тег Chat контракта (`/api/chat/sessions/**`,
// `/api/chat/pending-tasks*`, `/api/chat/pinned-agents*`, `/api/chat/history`,
// `/api/chat/thread`): сессии диалога пользователя с агентом, сообщения,
// закреплённые агенты и черновики восстановления после отмены задачи.
// Таблицы — 007_chat.up.sql (convos, convo_messages, convo_drafts,
// convo_pinned_operatives, convo_channel_links) плюс узкая добавка
// 140_chat_read_state.up.sql (convos.cv_last_read_at — единственный "читатель"
// сессии — это её создатель, поэтому одна метка на convos, не отдельная
// таблица "прочитано на пользователя").
//
// Отправка сообщения пользователя ставит задачу агенту в очередь через
// server2/internal/dispatch (см. Dispatch в Deps) — chat не пишет в
// dispatch_jobs напрямую. Ответ агента (после того как демон завершит
// запуск, T-028) приходит в этот пакет через AppendAgentReply/FinalizeRun
// (agent_reply.go) — это точка входа, которую вызовет протокол daemon.
package chat

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена chat.
type Deps struct {
	Store     *Store
	DB        *store.Store // передаётся в dispatch.* как Querier (см. handlers.go)
	Workspace httpapi.WorkspaceMembership
	Dispatch  *dispatch.Deps
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, wsStore *workspace.Store, dispatchDeps *dispatch.Deps, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db),
		DB:        db,
		Workspace: wsStore.HTTPAPIMembership(),
		Dispatch:  dispatchDeps,
		Publisher: pub,
		Logger:    logger,
	}
}
