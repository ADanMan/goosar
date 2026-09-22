package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AuthCookieName      = "goosar_auth"
	CSRFCookieName      = "goosar_csrf"
	defaultAuthTokenTTL = 30 * 24 * time.Hour
)

var (
	ipCookieDomainWarnOnce sync.Once
	authTokenTTLOnce       sync.Once
	authTokenTTLCached     time.Duration
)

func parseAuthTokenTTL(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0, false
		}
		if d > 10*365*24*time.Hour {
			slog.Warn("AUTH_TOKEN_TTL exceeds 10 years; accepting but verify this is intentional",
				"value", raw, "hours", d.Hours())
		}
		return d, true
	}

	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || secs <= 0 {
		return 0, false
	}
	if secs > int64(math.MaxInt64/int64(time.Second)) {
		return 0, false
	}
	d := time.Duration(secs) * time.Second
	if d > 10*365*24*time.Hour {
		slog.Warn("AUTH_TOKEN_TTL exceeds 10 years; accepting but verify this is intentional",
			"value", raw, "hours", d.Hours())
	}
	return d, true
}

func AuthTokenTTL() time.Duration {
	authTokenTTLOnce.Do(func() {
		raw := os.Getenv("AUTH_TOKEN_TTL")
		if ttl, ok := parseAuthTokenTTL(raw); ok {
			authTokenTTLCached = ttl
			slog.Info("auth token TTL configured", "seconds", int(ttl.Seconds()))
			return
		}
		authTokenTTLCached = defaultAuthTokenTTL
		if strings.TrimSpace(raw) != "" {
			slog.Warn("AUTH_TOKEN_TTL is not a valid duration or positive integer; using default",
				"value", raw, "default_seconds", int(defaultAuthTokenTTL.Seconds()))
		}
	})
	return authTokenTTLCached
}

func cookieDomain() string {
	raw := strings.TrimSpace(os.Getenv("COOKIE_DOMAIN"))
	if raw == "" {
		return ""
	}

	if ip := net.ParseIP(strings.TrimPrefix(raw, ".")); ip != nil {
		ipCookieDomainWarnOnce.Do(func() {
			slog.Warn(
				"COOKIE_DOMAIN looks like an IP address; ignoring. RFC 6265 forbids IP literals in the cookie Domain attribute, so browsers would drop the Set-Cookie. Leave COOKIE_DOMAIN empty for single-host deployments, or use a real domain.",
				"value", raw,
			)
		})
		return ""
	}
	return raw
}

func isSecureCookie() bool {
	raw := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https")
}

func generateCSRFToken(authToken string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	nonceHex := hex.EncodeToString(nonce)

	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write(nonce)
	sig := hex.EncodeToString(mac.Sum(nil))

	return nonceHex + "." + sig, nil
}

func SetAuthCookies(w http.ResponseWriter, token string) error {
	secure := isSecureCookie()
	domain := cookieDomain()
	ttl := AuthTokenTTL()
	now := time.Now()

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		MaxAge:   int(ttl.Seconds()),
		Expires:  now.Add(ttl),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})

	csrfToken, err := generateCSRFToken(token)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    csrfToken,
		Path:     "/",
		Domain:   domain,
		MaxAge:   int(ttl.Seconds()),
		Expires:  now.Add(ttl),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})

	return nil
}

func ClearAuthCookies(w http.ResponseWriter) {
	domain := cookieDomain()
	secure := isSecureCookie()

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func ValidateCSRF(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}

	csrfHeader := r.Header.Get("X-CSRF-Token")
	if csrfHeader == "" {
		return false
	}

	authCookie, err := r.Cookie(AuthCookieName)
	if err != nil || authCookie.Value == "" {
		return false
	}

	parts := strings.SplitN(csrfHeader, ".", 2)
	if len(parts) != 2 {
		return false
	}

	nonce, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	expectedSig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(authCookie.Value))
	mac.Write(nonce)
	return hmac.Equal(mac.Sum(nil), expectedSig)
}

func CookieSecure() bool   { return isSecureCookie() }
func CookieDomain() string { return cookieDomain() }
