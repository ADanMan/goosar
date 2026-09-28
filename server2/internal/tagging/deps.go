// Package tagging реализует /api/labels/** (+ прикрепление к задаче,
// /api/issues/{id}/labels...) и /api/properties/** (+ /api/issues/{id}/properties/{propertyId}) —
// таблицы tags, field_defs, ticket_tag_links (005_tasks.up.sql).
package tagging

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// Deps — зависимости домена tagging.
type Deps struct {
	Store     *Store
	Resolver  *wsctx.Resolver
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), Resolver: wsctx.New(db), Publisher: pub, Logger: logger}
}
