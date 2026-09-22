package middleware

import (
	"net/http"
	"time"

	"github.com/adanman/goosar/server/internal/auth"
)

func RefreshCloudFrontCookies(signer *auth.CloudFrontSigner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if signer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie("CloudFront-Policy"); err != nil {
				ttl := auth.AuthTokenTTL()
				for _, cookie := range signer.SignedCookies(time.Now().Add(ttl)) {
					http.SetCookie(w, cookie)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
