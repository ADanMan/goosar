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

	// DisableWorkspaceCreation — DISABLE_WORKSPACE_CREATION (contract, группа
	// "Аутентификация и сессии"): запрещает создание новых рабочих
	// пространств любым участником — перевод развёртывания на приём только
	// по приглашениям после начальной раскатки.
	DisableWorkspaceCreation bool
}

func New(db *store.Store, authnDeps *authn.Deps, pub realtime.Publisher, mailer mail.Sender, disableWorkspaceCreation bool, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), Authn: authnDeps, Publisher: pub, Mailer: mailer, DisableWorkspaceCreation: disableWorkspaceCreation, Logger: logger}
}

// DB0 — короткий доступ к домен-стору (используется обработчиками).
func (d *Deps) DB0() *Store { return d.Store }
