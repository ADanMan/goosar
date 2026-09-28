package app

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/agent"
	"github.com/adanman/goosar/server2/internal/agentbuilder"
	"github.com/adanman/goosar/server2/internal/agenttemplate"
	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/autopilot"
	"github.com/adanman/goosar/server2/internal/billing"
	"github.com/adanman/goosar/server2/internal/chat"
	"github.com/adanman/goosar/server2/internal/cloudruntime"
	"github.com/adanman/goosar/server2/internal/daemon"
	"github.com/adanman/goosar/server2/internal/dashboard"
	"github.com/adanman/goosar/server2/internal/deployment"
	"github.com/adanman/goosar/server2/internal/export"
	"github.com/adanman/goosar/server2/internal/feed"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/identity"
	"github.com/adanman/goosar/server2/internal/integration"
	"github.com/adanman/goosar/server2/internal/misc"
	"github.com/adanman/goosar/server2/internal/note"
	"github.com/adanman/goosar/server2/internal/pin"
	"github.com/adanman/goosar/server2/internal/project"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/runtime"
	"github.com/adanman/goosar/server2/internal/skill"
	"github.com/adanman/goosar/server2/internal/squad"
	"github.com/adanman/goosar/server2/internal/tagging"
	"github.com/adanman/goosar/server2/internal/task"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// NewRouter строит httpapi.Router, регистрирует общую инфраструктуру
// (health/readiness/config), затем ровно по одной строке на домен, и в конце —
// заглушки для операций контракта, ещё не реализованных ни одним доменом
// (RegisterStubs, server2/internal/app/stubs_gen.go, генерируется
// server2/tools/genstubs из docs/50-api-contract.yaml).
//
// Добавить новый домен: реализовать его Register(*httpapi.Router, *xxx.Deps)
// в своём пакете, завести его Deps в Deps (deps.go) и добавить сюда одну
// строку домена — до RegisterStubs(router).
func NewRouter(d *Deps) *httpapi.Router {
	router := httpapi.New()

	RegisterHealth(router, d)
	RegisterConfig(router, d)

	authn.Register(router, d.Authn)
	identity.Register(router, d.Identity)
	workspace.Register(router, d.Workspace)
	task.Register(router, d.Task)
	project.Register(router, d.Project)
	feed.Register(router, d.Feed)
	chat.Register(router, d.Chat)
	tagging.Register(router, d.Tagging)
	asset.Register(router, d.Asset)
	note.Register(router, d.Note)
	pin.Register(router, d.Pin)
	autopilot.Register(router, d.Autopilot)
	cloudruntime.Register(router, d.CloudRuntime)
	runtime.Register(router, d.Runtime)
	daemon.Register(router, d.Daemon)
	agent.Register(router, d.Agent)
	squad.Register(router, d.Squad)
	skill.Register(router, d.Skill)
	agenttemplate.Register(router, d.AgentTemplate)
	agentbuilder.Register(router, d.AgentBuilder)
	dashboard.Register(router, d.Dashboard)
	deployment.Register(router, d.Deployment)
	integration.Register(router, d.Integration)
	billing.Register(router, d.Billing)
	export.Register(router, d.Export)
	misc.Register(router, d.Misc)
	realtime.Register(router, d.Hub, d.Authn, d.Workspace.Store.RealtimeMembership(), d.Task.RealtimeTaskAccess(), chat.NewChatAccessBridge(d.Chat.Store), d.Logger)

	RegisterStubs(router)
	return router
}

// middlewareStage — один слой в цепочке BuildHandler; хранить их как слайс
// функций, а не как явную вложенную запись h = f(h) три раза подряд, чтобы
// порядок применения читался как список, а не как цепочка присваиваний.
type middlewareStage func(http.Handler) http.Handler

// BuildHandler собирает финальный http.Handler сервера: сверху вниз запрос
// проходит CORS → request-id/логирование/recover → глобальную (опциональную)
// аутентификацию, кладущую actor в контекст, и только потом попадает в router.
func (d *Deps) BuildHandler(router *httpapi.Router) http.Handler {
	stages := []middlewareStage{
		func(h http.Handler) http.Handler { return httpapi.WithCORS(h, d.Config.FrontendOrigin) },
		func(h http.Handler) http.Handler { return httpapi.WithCommonMiddleware(h, d.Logger) },
		d.Authn.Middleware,
		// WithAPIRateLimit — T-029, RATE_LIMIT_API (contract §1.5). Стоит
		// после d.Authn.Middleware: ключ лимита предпочитает actor.UserID,
		// который эта миддлварь ещё не положила бы в контекст, будь она
		// раньше в цепочке.
		func(h http.Handler) http.Handler { return httpapi.WithAPIRateLimit(h, d.APILimiter) },
	}

	handler := http.Handler(router)
	for i := len(stages) - 1; i >= 0; i-- {
		handler = stages[i](handler)
	}
	return handler
}
