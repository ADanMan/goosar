package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/util/clientip"
)

const ErrCodeRateLimited = "rate_limited"

const maxRateLimitBodyPeek = 8 << 10

func ParseTrustedProxies(raw string) []*net.IPNet { return clientip.ParseTrustedProxies(raw) }

func TrustedProxiesFromEnv() []*net.IPNet {
	if raw := strings.TrimSpace(os.Getenv("RATE_LIMIT_TRUSTED_PROXIES")); raw != "" {
		return ParseTrustedProxies(raw)
	}
	return clientip.FromEnv()
}

func RateLimit(store RateLimitStore, limit int, window time.Duration, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return keyedRateLimit(store, limit, window, func(r *http.Request) string {
		return "ip:" + extractIP(r, trustedProxies)
	}, "")
}

func RateLimitByUserOrIP(store RateLimitStore, scope string, limit int, window time.Duration, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return keyedRateLimit(store, limit, window, func(r *http.Request) string {
		if userID := strings.TrimSpace(r.Header.Get("X-User-ID")); userID != "" {
			return "user:" + userID
		}
		return "ip:" + extractIP(r, trustedProxies)
	}, scope)
}

func RateLimitByJSONField(store RateLimitStore, field string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return jsonFieldRateLimit(store, field, limit, window, false)
}

func RateLimitByJSONFieldHashed(store RateLimitStore, field string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return jsonFieldRateLimit(store, field, limit, window, true)
}

func jsonFieldRateLimit(store RateLimitStore, field string, limit int, window time.Duration, hashed bool) func(http.Handler) http.Handler {
	inner := keyedRateLimit(store, limit, window, func(r *http.Request) string {
		value := rateLimitBodyValue(r, field)
		if hashed && value != "" {
			sum := sha256.Sum256([]byte(value))
			value = hex.EncodeToString(sum[:16])
		}
		return "field:" + field + ":" + value

	}, "")
	return func(next http.Handler) http.Handler {
		limited := inner(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				peek, err := io.ReadAll(io.LimitReader(r.Body, maxRateLimitBodyPeek+1))
				if err != nil {
					_ = r.Body.Close()
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}

				if len(peek) > maxRateLimitBodyPeek {
					_ = r.Body.Close()
					http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
					return
				}

				original := r.Body
				r.Body = struct {
					io.Reader
					io.Closer
				}{io.MultiReader(bytes.NewReader(peek), original), original}
				r = r.WithContext(context.WithValue(r.Context(), rateLimitBodyKey{}, peek))
			}
			limited.ServeHTTP(w, r)
		})
	}
}

type rateLimitBodyKey struct{}

func rateLimitBodyValue(r *http.Request, field string) string {
	body, _ := r.Context().Value(rateLimitBodyKey{}).([]byte)
	if len(body) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return ""
	}
	raw, ok := fields[field]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(value))
}

var rateLimitFallback = NewMemoryRateLimitStore()

func keyedRateLimit(store RateLimitStore, limit int, window time.Duration, keyFn func(*http.Request) string, scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if store == nil || limit <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := keyFn(r)

			if subject == "" || strings.HasSuffix(subject, ":") {
				next.ServeHTTP(w, r)
				return
			}
			bucket := scope
			if bucket == "" {
				bucket = strings.ReplaceAll(strings.TrimPrefix(r.URL.Path, "/"), "/", ":")
			}
			key := fmt.Sprintf("mul:ratelimit:%s:%s", bucket, subject)

			count, retryAfter, err := store.Incr(r.Context(), key, window)
			if err != nil {

				slog.Warn("ratelimit: backend error; falling back to the in-process counter",
					"error", err, "bucket", bucket, "backend", store.Backend())
				count, retryAfter, _ = rateLimitFallback.Incr(r.Context(), key, window)
			}
			if count > int64(limit) {
				writeRateLimited(w, retryAfter, window)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimited(w http.ResponseWriter, retryAfter, window time.Duration) {
	if retryAfter <= 0 {
		retryAfter = window
	}
	seconds := int(retryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "too many requests",
		"code":  ErrCodeRateLimited,
	})
}

func ClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	return clientip.Of(r, trustedProxies)
}

func extractIP(r *http.Request, trustedProxies []*net.IPNet) string {
	return clientip.Of(r, trustedProxies)
}

func isTrustedProxy(ip net.IP, cidrs []*net.IPNet) bool {
	return clientip.IsTrusted(ip, cidrs)
}
