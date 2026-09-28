package authn

import (
	"net/http"
	"net/url"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// handleOidcStart — GET /api/auth/oidc/start: редирект на authorization_endpoint
// IdP с подписанной state-cookie (contract: "Редирект на IdP; ставит
// подписанную state-cookie"). Ответы этой операции в контракте — только
// 302/404/500 (нет 429): при исчерпанном лимите эта ручка всё равно отвечает
// 429 JSON — контракт группирует её лимит с verify-code/verify-link/mfa-verify
// под одним RATE_LIMIT_AUTH_VERIFY (§1.5), и оставить его молча
// неисполняемым здесь было бы дырой в защите, которую §1.5 явно требует (см.
// server2/docs/decisions.md, раздел T-029).
func (d *Deps) handleOidcStart(w http.ResponseWriter, r *http.Request) {
	if d.Config.OIDC.IssuerURL == "" {
		httpapi.WriteError(w, http.StatusNotFound, "OIDC not enabled on this server", "oidc_not_enabled")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Enforce(w, ip, "too many requests") {
		return
	}

	client := "web"
	if r.URL.Query().Get("client") == "desktop" {
		client = "desktop"
	}
	state, err := randomToken("", 16)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "failed to prepare state", "internal_error")
		return
	}
	nonce, err := randomToken("", 16)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "failed to prepare state", "internal_error")
		return
	}
	disc, err := d.oidc().discover(r.Context())
	if err != nil {
		d.Logger.Error("auth: oidc: discovery", "err", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "failed to prepare state", "internal_error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: signOIDCState(d.Config.JWTSecret, state, client, nonce, oidcStateTTL),
		Path: "/", HttpOnly: true, Secure: d.Config.IsProduction(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(oidcStateTTL.Seconds()),
	})

	q := url.Values{
		"response_type": {"code"},
		"client_id":     {d.Config.OIDC.ClientID},
		"redirect_uri":  {d.oidcRedirectURL()},
		"scope":         {"openid email profile"},
		"state":         {state},
		"nonce":         {nonce},
	}
	http.Redirect(w, r, disc.AuthorizationEndpoint+"?"+q.Encode(), http.StatusFound)
}
