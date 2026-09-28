package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

const (
	cookieAuth = "goosar_auth"
	cookieCSRF = "goosar_csrf"
)

// extractToken достаёт токен из Authorization: Bearer (приоритет) или из
// cookie goosar_auth, и сообщает, откуда он взят (нужно для CSRF-проверки:
// см. components/securitySchemes/cookieAuth — она обязательна только для
// state-changing запросов, аутентифицированных именно cookie).
func extractToken(r *http.Request) (token string, fromCookie bool) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if v, ok := strings.CutPrefix(auth, "Bearer "); ok && v != "" {
			return v, false
		}
	}
	if c, err := r.Cookie(cookieAuth); err == nil && c.Value != "" {
		return c.Value, true
	}
	return "", false
}

func isUnsafeMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// resolveActor разбирает токен по его форме (см. securitySchemes/bearerAuth
// и classifyToken в crypto.go). kindCloudPAT (gsln_, облачный fleet-сервис,
// вне рамок этой clean-room реализации) по-прежнему отклоняется — см.
// decisions.md; kindTaskToken/kindDaemonToken реализованы правкой T-028
// (agent_actor.go/daemon_token.go).
func (d *Deps) resolveActor(ctx context.Context, token string) (*httpapi.Actor, bool) {
	if token == "" {
		return nil, false
	}
	switch classifyToken(token) {
	case kindPAT:
		return d.actorFromPAT(ctx, token)
	case kindSessionJWT:
		return d.actorFromSession(ctx, token)
	case kindTaskToken:
		return d.actorFromTaskToken(ctx, token)
	case kindDaemonToken:
		return d.actorFromDaemonToken(token)
	default: // kindCloudPAT
		return nil, false
	}
}

func (d *Deps) actorFromPAT(ctx context.Context, token string) (*httpapi.Actor, bool) {
	pat, err := d.Store.FindPATByToken(ctx, token)
	if err != nil {
		return nil, false
	}
	acct, err := d.Store.FindAccountByID(ctx, pat.AccountID)
	if err != nil {
		return nil, false
	}
	return &httpapi.Actor{
		UserID: acct.ID, Email: acct.Email, Name: acct.Name,
		IsHuman: true, Source: httpapi.SourcePAT,
	}, true
}

func (d *Deps) actorFromSession(ctx context.Context, token string) (*httpapi.Actor, bool) {
	claims, err := d.Signer.Verify(token)
	if err != nil {
		return nil, false
	}
	acct, err := d.Store.FindAccountByID(ctx, claims.Sub)
	if err != nil || acct.TokenEpoch != claims.TV {
		return nil, false
	}
	valid, err := d.Store.IsSessionValid(ctx, claims.SID, acct.ID)
	if err != nil || !valid {
		return nil, false
	}
	return &httpapi.Actor{
		UserID: acct.ID, Email: acct.Email, Name: acct.Name,
		IsHuman: true, Source: httpapi.SourceSession, SessionID: claims.SID,
	}, true
}

// Middleware — глобальный слой аутентификации: если запрос несёт валидный
// токен (cookie или Bearer), кладёт httpapi.Actor в контекст. Само по себе
// ничего не блокирует — требование "нужен ли этот эндпоинт аутентификации"
// (x-roles контракта) проверяет каждый обработчик через httpapi.RequireActor/
// RequireHuman. Единственная блокировка на этом уровне — CSRF для
// cookie-аутентифицированных мутирующих запросов.
func (d *Deps) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, fromCookie := extractToken(r)
		actor, ok := d.resolveActor(r.Context(), token)
		if ok && fromCookie && isUnsafeMethod(r.Method) {
			csrf, err := r.Cookie(cookieCSRF)
			if err != nil || csrf.Value == "" || csrf.Value != r.Header.Get("X-CSRF-Token") {
				ok = false
			}
		}
		if ok {
			r = r.WithContext(httpapi.WithActor(r.Context(), actor))
		}
		next.ServeHTTP(w, r)
	})
}

// AuthenticateRequest реализует realtime.Authenticator для обработчика /ws:
// аутентификация по cookie на этапе handshake (contract: "cookie auth optional
// at handshake").
func (d *Deps) AuthenticateRequest(r *http.Request) (*httpapi.Actor, bool) {
	c, err := r.Cookie(cookieAuth)
	if err != nil || c.Value == "" {
		return nil, false
	}
	return d.resolveActor(r.Context(), c.Value)
}

// AuthenticateToken реализует вторую половину realtime.Authenticator:
// проверка токена, присланного первым WS-фреймом, когда handshake прошёл без cookie.
func (d *Deps) AuthenticateToken(ctx context.Context, token string) (*httpapi.Actor, bool) {
	return d.resolveActor(ctx, token)
}
