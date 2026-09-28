// Package httpapi — общая инфраструктура HTTP-слоя, которую переиспользуют
// все домены: маршрутизатор поверх net/http.ServeMux (Go 1.22+, с шаблонами
// путей вида "/api/workspaces/{id}"), формат JSON-ответов и ошибок по
// контракту, декодирование тела запроса, извлечение актора запроса, резолв
// активного воркспейса и проверка ролей, простой rate limit.
//
// Домены не создают http.ServeMux сами — они получают общий *Router через
// app.Deps и регистрируют на нём свои маршруты функцией вида
// func Register(mux *httpapi.Router, deps *app.Deps).
package httpapi

import (
	"net/http"
	"sort"
	"sync"
)

// Router оборачивает net/http.ServeMux и запоминает, какие пары
// "МЕТОД путь" уже зарегистрированы — это нужно генератору заглушек
// (server2/tools/genstubs): он регистрирует 501-обработчик для каждой
// операции контракта, но только если домен ещё не занял этот маршрут.
type Router struct {
	mux *http.ServeMux

	mu         sync.Mutex
	registered map[string]bool
}

// New создаёт пустой Router.
func New() *Router {
	return &Router{
		mux:        http.NewServeMux(),
		registered: make(map[string]bool),
	}
}

func key(method, pattern string) string { return method + " " + pattern }

// Handle регистрирует обработчик домена на method+pattern. Паникует при
// попытке зарегистрировать один и тот же маршрут дважды — это ошибка
// программиста (два домена претендуют на один эндпоинт контракта), а не
// runtime-ситуация, которую нужно тихо обрабатывать.
func (r *Router) Handle(method, pattern string, h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(method, pattern)
	if r.registered[k] {
		panic("httpapi: маршрут уже зарегистрирован: " + k)
	}
	r.registered[k] = true
	r.mux.HandleFunc(k, h)
}

// HandleStub регистрирует обработчик, только если маршрут ещё свободен.
// Используется исключительно генератором заглушек (stubs_gen.go): домены
// регистрируются первыми через Handle, заглушки — последними через
// HandleStub, поэтому реализованные операции "вытесняют" 501-заглушки.
func (r *Router) HandleStub(method, pattern string, h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(method, pattern)
	if r.registered[k] {
		return
	}
	r.registered[k] = true
	r.mux.HandleFunc(k, h)
}

// IsRegistered — занят ли уже данный method+pattern.
func (r *Router) IsRegistered(method, pattern string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registered[key(method, pattern)]
}

// Registered возвращает отсортированный список всех зарегистрированных
// "МЕТОД путь" — используется тестом покрытия маршрутов.
func (r *Router) Registered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.registered))
	for k := range r.registered {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ServeHTTP делает Router самим http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
