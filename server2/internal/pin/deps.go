// Package pin реализует /api/pins/** — личные закладки участника на задачи
// и проекты (ticket_bookmarks/initiative_bookmarks, 005_tasks.up.sql).
package pin

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

type Deps struct {
	Store     *Store
	Resolver  *wsctx.Resolver
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), Resolver: wsctx.New(db), Publisher: pub, Logger: logger}
}
