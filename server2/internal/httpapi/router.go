// Package httpapi — общая инфраструктура HTTP-слоя, которую переиспользуют
// все домены: маршрутизатор поверх net/http.ServeMux (Go 1.22+, с шаблонами
// путей вида "/api/workspaces/{id}"), формат JSON-ответов и ошибок по
// контракту, декодирование тела запроса, извлечение актора запроса, резолв
// активного воркспейса и проверка ролей, простой rate limit.
//
// Домены не создают http.ServeMux сами — они получают общий *Router через
// свой Deps и регистрируют на нём маршруты функцией вида
// func Register(router *httpapi.Router, deps *xxx.Deps).
package httpapi

import (
	"net/http"
	"sort"
	"sync"
)

// registerMode различает два способа занять маршрут: modeClaim — «этот путь
// теперь мой, и точка» (для доменов), modeYield — «займу, только если
// свободно» (для 501-заглушек, которые не должны вытеснять домен).
type registerMode int

const (
	modeClaim registerMode = iota
	modeYield
)

// Route — один занятый маршрут в виде "МЕТОД путь", как его понимает net/http.ServeMux (Go 1.22+).
type Route string

// Router оборачивает net/http.ServeMux и запоминает, какие Route уже заняты —
// это нужно генератору заглушек (server2/tools/genstubs): 501-обработчик для
// операции контракта регистрируется, только если домен ещё не занял этот путь.
type Router struct {
	mux *http.ServeMux

	mu    sync.Mutex
	taken map[Route]bool
}

func New() *Router {
	return &Router{mux: http.NewServeMux(), taken: make(map[Route]bool)}
}

func routeOf(method, pattern string) Route { return Route(method + " " + pattern) }

func (r *Router) register(mode registerMode, method, pattern string, h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rt := routeOf(method, pattern)
	switch {
	case r.taken[rt] && mode == modeYield:
		return // заглушка вежливо уступает уже занятому пути
	case r.taken[rt]:
		panic("httpapi: маршрут уже зарегистрирован: " + string(rt))
	}
	r.taken[rt] = true
	r.mux.HandleFunc(string(rt), h)
}

// Handle закрепляет обработчик домена за method+pattern раз и навсегда:
// повторная попытка занять тот же путь — ошибка программиста (два домена
// претендуют на один эндпоинт контракта), а не ситуация, которую можно
// проглотить молча.
func (r *Router) Handle(method, pattern string, h http.HandlerFunc) {
	r.register(modeClaim, method, pattern, h)
}

// HandleStub закрепляет обработчик, только если путь ещё ничей. Только
// генератор заглушек должен звать этот метод: домены регистрируются первыми
// через Handle, заглушки — вторым проходом через HandleStub.
func (r *Router) HandleStub(method, pattern string, h http.HandlerFunc) {
	r.register(modeYield, method, pattern, h)
}

// IsRegistered сообщает, занят ли уже данный method+pattern.
func (r *Router) IsRegistered(method, pattern string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.taken[routeOf(method, pattern)]
}

// Registered — отсортированный снимок всех занятых маршрутов, для теста покрытия.
func (r *Router) Registered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.taken))
	for rt := range r.taken {
		out = append(out, string(rt))
	}
	sort.Strings(out)
	return out
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
