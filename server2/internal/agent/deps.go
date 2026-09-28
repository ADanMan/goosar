package agent

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена agent.
type Deps struct {
	Store     *Store
	DB        *store.Store // передаётся в dispatch.* как Querier (см. handlers_tasks.go)
	Workspace httpapi.WorkspaceMembership
	Dispatch  *dispatch.Deps
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, wsStore *workspace.Store, dispatchDeps *dispatch.Deps, cfg config.Config, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db, cfg.McpSecretKey, cfg.McpSecretKeyPrevious),
		DB:        db,
		Workspace: wsStore.HTTPAPIMembership(),
		Dispatch:  dispatchDeps,
		Publisher: pub,
		Logger:    logger,
	}
}

func (d *Deps) notify(workspaceID, eventType string, payload any) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}
