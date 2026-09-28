// Package project реализует теги Projects и ProjectResources контракта:
// CRUD проектов (initiatives) вместе со связанными ресурсами (репозитории и
// локальные каталоги, initiative_resources) и полнотекстовый поиск по ним.
// Таблицы — 005_tasks.up.sql (initiatives, initiative_resources).
package project

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена project.
type Deps struct {
	Store     *Store
	Workspace httpapi.WorkspaceMembership
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

// New строит Deps домена вокруг общего пула БД и store домена workspace
// (только для резолва воркспейса/роли — см. workspace.Store.HTTPAPIMembership).
func New(db *store.Store, wsStore *workspace.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db),
		Workspace: wsStore.HTTPAPIMembership(),
		Publisher: pub,
		Logger:    logger,
	}
}
