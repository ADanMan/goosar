// Package app собирает зависимости сервера (пул БД, конфигурацию, realtime-хаб,
// подписанты JWT, почту) и регистрирует на общем httpapi.Router все домены —
// это единственный пакет, которому разрешено знать обо всех доменах сразу
// (routes.go), чтобы сами домены могли разрабатываться параллельно, не видя
// друг друга.
package app

import (
	"log/slog"
	"time"

	"github.com/adanman/goosar/server2/internal/agent"
	"github.com/adanman/goosar/server2/internal/agentbuilder"
	"github.com/adanman/goosar/server2/internal/agenttemplate"
	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/autopilot"
	"github.com/adanman/goosar/server2/internal/billing"
	"github.com/adanman/goosar/server2/internal/chat"
	"github.com/adanman/goosar/server2/internal/cloudruntime"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/daemon"
	"github.com/adanman/goosar/server2/internal/dashboard"
	"github.com/adanman/goosar/server2/internal/deployment"
	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/export"
	"github.com/adanman/goosar/server2/internal/feed"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/identity"
	"github.com/adanman/goosar/server2/internal/integration"
	"github.com/adanman/goosar/server2/internal/mail"
	"github.com/adanman/goosar/server2/internal/misc"
	"github.com/adanman/goosar/server2/internal/note"
	"github.com/adanman/goosar/server2/internal/pin"
	"github.com/adanman/goosar/server2/internal/project"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/runtime"
	"github.com/adanman/goosar/server2/internal/skill"
	"github.com/adanman/goosar/server2/internal/squad"
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

	// APILimiter — T-029, общий лимит RATE_LIMIT_API на всю группу /api/**
	// (contract §1.5), подключается один раз в BuildHandler поверх готового
	// Router — не принадлежит ни одному домену.
	APILimiter *httpapi.Limiter

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

	// Autopilot/CloudRuntime — T-028, автопилот (расписание/вебхук/ручной
	// запуск, свой планировщик — server2/internal/autopilot) и прозрачный
	// прокси в облачный fleet-сервис (server2/internal/cloudruntime).
	Autopilot    *autopilot.Deps
	CloudRuntime *cloudruntime.Deps

	// Runtime/Daemon — T-028, среды выполнения (`/api/runtimes/**` +
	// агрегаты активности агентов воркспейса, server2/internal/runtime) и
	// daemon-протокол (`/api/daemon/**`, server2/internal/daemon).
	Runtime *runtime.Deps
	Daemon  *daemon.Deps

	// Agent/Squad/Skill/AgentTemplate/AgentBuilder/Dashboard — T-028, эта
	// сессия: агенты и их конфигурация (`/api/agents/**`), отряды
	// (`/api/squads/**` + `/api/issues/{id}/squad-evaluated`), навыки
	// (`/api/skills/**`), каталог шаблонов агентов (`/api/agent-templates`),
	// конструктор агента (`/api/agent-builder/**`) и дашборд использования
	// (`/api/dashboard/**`).
	Agent         *agent.Deps
	Squad         *squad.Deps
	Skill         *skill.Deps
	AgentTemplate *agenttemplate.Deps
	AgentBuilder  *agentbuilder.Deps
	Dashboard     *dashboard.Deps

	// Deployment — T-029, администрирование деплоя: роли deployment-admin с
	// двухканальным подтверждением, аудит деплоя, MCP-серверы деплоя/
	// воркспейса, политика деплоя, обзор воркспейсов/join-targets/fleet,
	// слой конфигурации воркспейса (LLM/MCP) и её персональные override'ы,
	// provisioning (манифест/каталог/pin'ы), client-secrets, llm/health
	// (server2/internal/deployment).
	Deployment *deployment.Deps

	// Integration/Billing/Export/Misc — T-029, эта сессия: интеграции
	// воркспейса (GitHub App/self-hosted VCS/Slack/Composio,
	// server2/internal/integration), прозрачный прокси в облачный биллинг +
	// вебхук Stripe (server2/internal/billing), экспорт воркспейса/личных
	// данных (server2/internal/export) и последние одиночные ручки без
	// отдельного домена — feedback/contact-sales/client-usage/status
	// (server2/internal/misc).
	Integration *integration.Deps
	Billing     *billing.Deps
	Export      *export.Deps
	Misc        *misc.Deps
}

// New строит все доменные Deps поверх общей инфраструктуры.
func New(cfg config.Config, db *store.Store, logger *slog.Logger) *Deps {
	mailer := mail.FromConfig(cfg, logger)
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

	autopilotDeps := autopilot.New(db, workspaceDeps.Store, taskDeps.Store, dispatchDeps, hub,
		cfg.McpSecretKey, cfg.PublicURL, logger)
	cloudRuntimeDeps := cloudruntime.New(db, cfg.CloudRuntimeBaseURL, cfg.CloudRuntimeAPIKey, logger)

	runtimeDeps := runtime.New(db, dispatchDeps, hub, logger)
	daemonDeps := daemon.New(db, runtimeDeps, dispatchDeps, workspaceDeps.Store, hub, cfg, logger)
	// Правка T-028 (internal/authn): подключает проверку mat_-токенов
	// агента-исполнителя задачи, которую до этой сессии authn всегда
	// отклонял (см. decisions.md, T-027 «Пробелы спецификации», п. 5) —
	// тот же приём, что noteDeps.SetDispatcher чуть выше.
	authnDeps.SetTaskActorLookup(daemonDeps)

	agentDeps := agent.New(db, workspaceDeps.Store, dispatchDeps, cfg, hub, logger)
	squadDeps := squad.New(db, agentDeps.Store, workspaceDeps.Store, hub, logger)
	skillDeps := skill.New(db, hub, logger)
	agentTemplateDeps := agenttemplate.New(workspaceDeps.Store.HTTPAPIMembership())
	agentBuilderDeps := agentbuilder.New(db, agentDeps.Store, chatDeps.Store, workspaceDeps.Store, logger)
	dashboardDeps := dashboard.New(db, workspaceDeps.Store, logger)

	deploymentDeps := deployment.New(db, workspaceDeps.Store, authnDeps, hub, cfg, logger)

	integrationDeps := integration.New(db, cfg, hub, logger)
	billingDeps := billing.New(cfg.CloudRuntimeBaseURL, cfg.CloudRuntimeAPIKey, logger)
	exportDeps := export.New(db, assetDeps.Storage, logger)
	miscDeps := misc.New(db, cfg.RateLimits.ContactSales, logger)

	return &Deps{
		Config:     cfg,
		Store:      db,
		Logger:     logger,
		Mailer:     mailer,
		Hub:        hub,
		APILimiter: httpapi.NewLimiter(cfg.RateLimits.API, time.Minute),
		Authn:      authnDeps,
		Identity:   identityDeps,
		Workspace:  workspaceDeps,
		Dispatch:   dispatchDeps,
		Task:       taskDeps,
		Project:    projectDeps,
		Feed:       feedDeps,
		Chat:       chatDeps,

		Note:    noteDeps,
		Tagging: taggingDeps,
		Asset:   assetDeps,
		Pin:     pinDeps,

		Autopilot:    autopilotDeps,
		CloudRuntime: cloudRuntimeDeps,

		Runtime: runtimeDeps,
		Daemon:  daemonDeps,

		Agent:         agentDeps,
		Squad:         squadDeps,
		Skill:         skillDeps,
		AgentTemplate: agentTemplateDeps,
		AgentBuilder:  agentBuilderDeps,
		Dashboard:     dashboardDeps,

		Deployment: deploymentDeps,

		Integration: integrationDeps,
		Billing:     billingDeps,
		Export:      exportDeps,
		Misc:        miscDeps,
	}
}
