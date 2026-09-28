package httpapi

import (
	"context"
	"net/http"
)

// ActorSource — как был аутентифицирован вызывающий; используется резолвом
// воркспейса (X-Actor-Source: task_token имеет приоритет над заголовками
// воркспейса, см. contract components/parameters/WorkspaceSlugHeader) и
// проверками "только человек" (x-roles: any-authenticated-human).
type ActorSource string

const (
	SourceSession   ActorSource = "session"      // cookie/Bearer session JWT
	SourcePAT       ActorSource = "pat"          // gsl_... personal access token
	SourceCloudPAT  ActorSource = "cloud_pat"    // gsln_...
	SourceTaskToken ActorSource = "task_token"   // mat_...
	SourceDaemon    ActorSource = "daemon_token" // mdt_...
)

// Actor — аутентифицированный вызывающий текущего запроса.
type Actor struct {
	UserID    string
	Email     string
	Name      string
	IsHuman   bool
	Source    ActorSource
	SessionID string // sid claim сессионного JWT, пусто для PAT/токенов

	// TaskWorkspaceID — воркспейс, к которому жёстко привязан task-token;
	// если задан, он имеет приоритет над заголовками выбора воркспейса.
	TaskWorkspaceID string

	// DaemonID — правка T-028: id физического daemon-процесса, зашитый в
	// mdt_-токен (SourceDaemon); пусто для остальных источников.
	DaemonID string
}

type ctxKey int

const (
	ctxActor ctxKey = iota
	ctxRequestID
)

// WithActor кладёт актора в контекст запроса.
func WithActor(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, ctxActor, a)
}

// ActorFrom достаёт актора из контекста, если он был аутентифицирован.
func ActorFrom(ctx context.Context) (*Actor, bool) {
	a, ok := ctx.Value(ctxActor).(*Actor)
	return a, ok && a != nil
}

// RequireActor — как ActorFrom, но пишет 401 и возвращает ok=false, если
// актора нет. Домены вызывают это первой строкой в защищённых обработчиках.
func RequireActor(w http.ResponseWriter, r *http.Request) (*Actor, bool) {
	a, ok := ActorFrom(r.Context())
	if !ok {
		Unauthorized(w, "")
		return nil, false
	}
	return a, true
}

// RequireHuman — как RequireActor, но также отвергает не-человеческих
// вызывающих (x-roles: any-authenticated-human), например задачи-агенты.
func RequireHuman(w http.ResponseWriter, r *http.Request) (*Actor, bool) {
	a, ok := RequireActor(w, r)
	if !ok {
		return nil, false
	}
	if !a.IsHuman {
		Forbidden(w, "this endpoint is only available to human actors")
		return nil, false
	}
	return a, true
}

// WithRequestID кладёт X-Request-ID в контекст (для логирования).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// RequestIDFrom достаёт request id из контекста.
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}
