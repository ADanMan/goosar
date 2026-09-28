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
	client := "web"
	var (
		wantState, nonce string
		stateOK          bool
	)
	if cookie, err := r.Cookie(oidcStateCookie); err == nil {
		if s, c, n, ok := verifyOIDCState(d.Config.JWTSecret, cookie.Value); ok {
			wantState, client, nonce, stateOK = s, c, n, true
		}
	}
	clearOIDCStateCookie(w)

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
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if !stateOK || state == "" || state != wantState || code == "" {
		d.oidcFailRedirect(w, r, client, "invalid_state")
		return
	}

	sub, email, name, failCode, err := d.resolveOIDCIdentity(r.Context(), code, nonce)
	if err != nil {
		d.Logger.Error("auth: oidc: обмен кода/идентификация", "err", err)
		d.oidcFailRedirect(w, r, client, failCode)
		return
	}

	acct, err := d.loginOrLinkExternal(r, "oidc", sub, email, name)
	if err != nil {
		d.Logger.Error("auth: oidc: вход/создание аккаунта", "err", err)
		d.oidcFailRedirect(w, r, client, "internal_error")
		return
	}
	d.finishExternalLogin(w, r, client, acct)
}
