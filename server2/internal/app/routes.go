package app

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/identity"
	"github.com/adanman/goosar/server2/internal/realtime"
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
	realtime.Register(router, d.Hub, d.Authn, d.Workspace.Store.RealtimeMembership(), d.Logger)

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
	}

	handler := http.Handler(router)
	for i := len(stages) - 1; i >= 0; i-- {
		handler = stages[i](handler)
	}
	return handler
}
