package authn

import (
	"net"
	"net/http"
)

// setSessionCookies выставляет goosar_auth (HttpOnly) и goosar_csrf
// (не-HttpOnly, тот же токен как значение X-CSRF-Token для сверки).
// TTL — AUTH_TOKEN_TTL; Domain — COOKIE_DOMAIN (IP молча игнорируется:
// RFC 6265 запрещает IP-литерал в атрибуте Domain, браузер такую куку не
// примет вовсе).
func (d *Deps) setSessionCookies(w http.ResponseWriter, token, csrfValue string) {
	secure := d.Config.IsProduction()
	domain := cookieDomain(d.Config.CookieDomain)
	maxAge := int(d.Config.AuthTokenTTL.Seconds())
	http.SetCookie(w, &http.Cookie{
		Name:     cookieAuth,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     cookieCSRF,
		Value:    csrfValue,
		Path:     "/",
		Domain:   domain,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// cookieDomain — COOKIE_DOMAIN как есть, кроме IP-адресов (браузеры
// игнорируют Domain=IP по RFC 6265 — лучше вовсе не выставлять атрибут, чем
// выставить тот, который клиент проигнорирует).
func cookieDomain(v string) string {
	if v == "" || net.ParseIP(v) != nil {
		return ""
	}
	return v
}

// clearSessionCookies стирает обе куки — используется /auth/logout, который
// по контракту "всегда успешен" независимо от наличия валидной сессии.
func (d *Deps) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{cookieAuth, cookieCSRF} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name == cookieAuth,
			MaxAge:   -1,
		})
	}
}
