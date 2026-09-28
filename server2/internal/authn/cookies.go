package authn

import (
	"net/http"
	"time"
)

const sessionTTL = 30 * 24 * time.Hour

// setSessionCookies выставляет goosar_auth (HttpOnly) и goosar_csrf
// (не-HttpOnly, тот же токен как значение X-CSRF-Token для сверки).
func (d *Deps) setSessionCookies(w http.ResponseWriter, token, csrfValue string) {
	secure := d.Config.IsProduction()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieAuth,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     cookieCSRF,
		Value:    csrfValue,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
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
