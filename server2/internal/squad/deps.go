package squad

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/agent"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — зависимости домена squad.
type Deps struct {
	Store     *Store
	DB        *store.Store
	Workspace httpapi.WorkspaceMembership
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, agentStore *agent.Store, wsStore *workspace.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db, agentStore),
		DB:        db,
		Workspace: wsStore.HTTPAPIMembership(),
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
