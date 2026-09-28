package authn

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// handleOidcCallback — GET /api/auth/oidc/callback: обмен code на identity,
// дальше как verify-code, но результат — редирект (contract §3.3/§3.6:
// "Always responds with an HTTP redirect"). Раз контракт прямо обещает
// редирект даже на ошибку, rate limit здесь тоже отвечает редиректом с
// auth_error=rate_limited, а не голым 429 (в отличие от oidc/start, чей
// список ответов не содержит прозы "always a redirect" — см. handlers_oidc_start.go).
// Хвост, общий с будущими редирект-based провайдерами (MFA-гейт → сессия/
// редирект) — oidc.go, finishExternalLogin.
func (d *Deps) handleOidcCallback(w http.ResponseWriter, r *http.Request) {
	client, nonce, stateErr := d.checkOIDCCallbackState(w, r)
	if d.Config.OIDC.IssuerURL == "" {
		httpapi.WriteError(w, http.StatusNotFound, "OIDC not enabled on this server", "oidc_not_enabled")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Allow(ip) {
		d.oidcFailRedirect(w, r, client, "rate_limited")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		d.oidcFailRedirect(w, r, client, "oidc_"+errParam)
		return
	}
	if stateErr != "" {
		d.oidcFailRedirect(w, r, client, stateErr)
		return
	}

	sub, email, name, failCode, err := d.resolveOIDCIdentity(r.Context(), r.URL.Query().Get("code"), nonce)
	if err != nil {
		d.oidcAbort(w, r, client, "обмен кода/идентификация", failCode, err)
		return
	}
	acct, err := d.loginOrLinkExternal(r, "oidc", sub, email, name)
	if err != nil {
		d.oidcAbort(w, r, client, "вход/создание аккаунта", "internal_error", err)
		return
	}
	d.finishExternalLogin(w, r, client, acct)
}

// oidcAbort логирует причину неудачи одной строкой и уводит редиректом на
// /login#auth_error=<failCode> — общий финал для обоих шагов после
// checkOIDCCallbackState, которым нужно и то, и другое.
func (d *Deps) oidcAbort(w http.ResponseWriter, r *http.Request, client, step, failCode string, err error) {
	d.Logger.Error("auth: oidc: "+step, "err", err)
	d.oidcFailRedirect(w, r, client, failCode)
}
