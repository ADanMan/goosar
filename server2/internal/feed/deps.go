// Package feed реализует теги Inbox и NotificationPreferences контракта
// (уведомления участников: чтение/архивация/группировка по тикету, настройки
// групп уведомлений), а также внутренний API Notify — им пользуются другие
// домены (issues/comments/chat/autopilots/...), чтобы создать уведомление в
// инбоксе получателя, не создавая цикла импорта на feed. Таблицы —
// 009_feed.up.sql (alerts, notification_prefs).
package feed

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена feed.
type Deps struct {
	Store     *Store
	Workspace httpapi.WorkspaceMembership
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, wsStore *workspace.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db),
		Workspace: wsStore.HTTPAPIMembership(),
		Publisher: pub,
		Logger:    logger,
	}
}
