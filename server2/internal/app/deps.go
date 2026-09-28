// Package app собирает зависимости сервера (пул БД, конфигурацию, realtime-хаб,
// подписанты JWT, почту) и регистрирует на общем httpapi.Router все домены —
// это единственный пакет, которому разрешено знать обо всех доменах сразу
// (routes.go), чтобы сами домены могли разрабатываться параллельно, не видя
// друг друга.
package app

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/identity"
	"github.com/adanman/goosar/server2/internal/mail"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// Deps — общие зависимости, из которых строится Deps каждого домена.
type Deps struct {
	Config config.Config
	Store  *store.Store
	Logger *slog.Logger
	Mailer mail.Sender
	Hub    *realtime.Hub

	Authn     *authn.Deps
	Identity  *identity.Deps
	Workspace *workspace.Deps
}

// New строит все доменные Deps поверх общей инфраструктуры.
func New(cfg config.Config, db *store.Store, logger *slog.Logger) *Deps {
	mailer := mail.NewLoggerSender(logger)
	hub := realtime.NewHub(logger)

	authnDeps := authn.New(db, cfg, mailer, logger)
	identityDeps := identity.New(authnDeps, logger)
	workspaceDeps := workspace.New(db, authnDeps, hub, mailer, logger)

	return &Deps{
		Config:    cfg,
		Store:     db,
		Logger:    logger,
		Mailer:    mailer,
		Hub:       hub,
		Authn:     authnDeps,
		Identity:  identityDeps,
		Workspace: workspaceDeps,
	}
}
