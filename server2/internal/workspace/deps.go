// Package workspace реализует тег Workspaces (+ RuntimeProfiles, Invitations)
// контракта: CRUD пространств, участники и роли, приглашения, профили
// рантайма (agent_protocols). Таблицы — 002_workspace.up.sql.
package workspace

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/mail"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
)

// Deps — зависимости домена workspace.
type Deps struct {
	Store     *Store
	Authn     *authn.Deps
	Publisher realtime.Publisher
	Mailer    mail.Sender
	Logger    *slog.Logger
}

func New(db *store.Store, authnDeps *authn.Deps, pub realtime.Publisher, mailer mail.Sender, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), Authn: authnDeps, Publisher: pub, Mailer: mailer, Logger: logger}
}

// DB0 — короткий доступ к домен-стору (используется обработчиками).
func (d *Deps) DB0() *Store { return d.Store }
