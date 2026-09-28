// Package app собирает зависимости сервера (пул БД, конфигурацию, realtime-хаб,
// подписанты JWT, почту) и регистрирует на общем httpapi.Router все домены —
// это единственный пакет, которому разрешено знать обо всех доменах сразу
// (routes.go), чтобы сами домены могли разрабатываться параллельно, не видя
// друг друга.
package app

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/chat"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/feed"
	"github.com/adanman/goosar/server2/internal/identity"
	"github.com/adanman/goosar/server2/internal/mail"
	"github.com/adanman/goosar/server2/internal/note"
	"github.com/adanman/goosar/server2/internal/pin"
	"github.com/adanman/goosar/server2/internal/project"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/tagging"
	"github.com/adanman/goosar/server2/internal/task"
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
	// Dispatch — общая точка постановки задач агентам в очередь (T-027,
	// server2/internal/dispatch); держится здесь одним экземпляром, чтобы
	// task и chat пользовались одним Publisher/Logger, не заводя каждый свой.
	Dispatch *dispatch.Deps
	// Task — задачи (issues): CRUD/фильтры/нумерация/назначение+автозапуск/
	// подписчики/метки-привязка/значения свойств/metadata/timeline/move/batch
	// (T-027, server2/internal/task). Комментарии/реакции/вложения задачи —
	// пакет Note.
	Task    *task.Deps
	Project *project.Deps
	Feed    *feed.Deps
	Chat    *chat.Deps

	// Note/Tagging/Asset/Pin — T-027, обсуждение/метки/свойства/вложения/
	// закрепления (server2/internal/{note,tagging,asset,pin}).
	Note    *note.Deps
	Tagging *tagging.Deps
	Asset   *asset.Deps
	Pin     *pin.Deps
}

// New строит все доменные Deps поверх общей инфраструктуры.
func New(cfg config.Config, db *store.Store, logger *slog.Logger) *Deps {
	mailer := mail.NewLoggerSender(logger)
	hub := realtime.NewHub(logger)

	authnDeps := authn.New(db, cfg, mailer, logger)
	identityDeps := identity.New(authnDeps, logger)
	workspaceDeps := workspace.New(db, authnDeps, hub, mailer, logger)
	dispatchDeps := dispatch.New(hub, logger)
	taskDeps := task.New(db, workspaceDeps.Store, dispatchDeps, hub, logger)
	projectDeps := project.New(db, workspaceDeps.Store, hub, logger)
	feedDeps := feed.New(db, workspaceDeps.Store, hub, logger)
	chatDeps := chat.New(db, workspaceDeps.Store, dispatchDeps, hub, logger)

	assetDeps := asset.New(db, hub, cfg, logger)
	taggingDeps := tagging.New(db, hub, logger)
	pinDeps := pin.New(db, hub, logger)
	noteDeps := note.New(db, assetDeps.Store, hub, logger)
	noteDeps.SetDispatcher(note.NewDispatchAdapter(dispatchDeps, db))

	return &Deps{
		Config:    cfg,
		Store:     db,
		Logger:    logger,
		Mailer:    mailer,
		Hub:       hub,
		Authn:     authnDeps,
		Identity:  identityDeps,
		Workspace: workspaceDeps,
		Dispatch:  dispatchDeps,
		Task:      taskDeps,
		Project:   projectDeps,
		Feed:      feedDeps,
		Chat:      chatDeps,

		Note:    noteDeps,
		Tagging: taggingDeps,
		Asset:   assetDeps,
		Pin:     pinDeps,
	}
}
