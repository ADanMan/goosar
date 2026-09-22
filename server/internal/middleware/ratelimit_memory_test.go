package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryStore_BlocksOverLimitAndRecoversAfterWindow(t *testing.T) {
	store := NewMemoryRateLimitStore()
	mw := RateLimit(store, 2, 50*time.Millisecond, nil)
	handler := mw(okHandler)

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
		req.RemoteAddr = "10.9.0.1:9000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if got := call().Code; got != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", got)
	}
	if got := call().Code; got != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", got)
	}

	rec := call()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: expected 429, got %d", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatal("429 must carry Retry-After")
	} else if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Fatalf("Retry-After must be a positive integer of seconds, got %q", ra)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != ErrCodeRateLimited {
		t.Fatalf("expected structured error code %q, got %q", ErrCodeRateLimited, body["code"])
	}

	time.Sleep(60 * time.Millisecond)
	if got := call().Code; got != http.StatusOK {
		t.Fatalf("after window: expected 200, got %d", got)
	}
}

func TestMemoryStore_SeparateKeysHaveSeparateBudgets(t *testing.T) {
	store := NewMemoryRateLimitStore()
	mw := RateLimit(store, 1, time.Minute, nil)
	handler := mw(okHandler)

	for _, addr := range []string{"10.9.1.1:1", "10.9.1.2:1", "10.9.1.3:1"} {
		req := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", addr, rec.Code)
		}
	}
}

func TestMemoryStore_ConcurrentIncrIsExact(t *testing.T) {
	store := NewMemoryRateLimitStore()
	const n = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	max := int64(0)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, _, err := store.Incr(context.Background(), "k", time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if count > max {
				max = count
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if max != n {
		t.Fatalf("expected the highest observed count to be %d, got %d", n, max)
	}
}

func TestRateLimitByJSONField_KeysOnEmailAndPreservesBody(t *testing.T) {
	store := NewMemoryRateLimitStore()
	var seen []string
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("handler must still be able to read the body: %v", err)
		}
		seen = append(seen, body.Email)
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimitByJSONField(store, "email", 1, time.Minute)(echo)

	call := func(email, addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/auth/send-code",
			strings.NewReader(`{"email":"`+email+`"}`))
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := call("a@example.com", "1.1.1.1:1"); got != http.StatusOK {
		t.Fatalf("first: expected 200, got %d", got)
	}

	if got := call("A@Example.com ", "2.2.2.2:2"); got != http.StatusTooManyRequests {
		t.Fatalf("same e-mail from another IP: expected 429, got %d", got)
	}

	if got := call("b@example.com", "2.2.2.2:2"); got != http.StatusOK {
		t.Fatalf("other e-mail: expected 200, got %d", got)
	}
	if len(seen) != 2 || seen[0] != "a@example.com" || seen[1] != "b@example.com" {
		t.Fatalf("handler did not see the original bodies: %#v", seen)
	}
}

func TestRateLimitByUserOrIP_PrefersAuthenticatedUser(t *testing.T) {
	store := NewMemoryRateLimitStore()
	handler := RateLimitByUserOrIP(store, "api", 1, time.Minute, nil)(okHandler)

	call := func(userID, addr string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
		req.RemoteAddr = addr
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := call("user-1", "5.5.5.5:1"); got != http.StatusOK {
		t.Fatalf("first: expected 200, got %d", got)
	}

	if got := call("user-1", "6.6.6.6:1"); got != http.StatusTooManyRequests {
		t.Fatalf("same user, other IP: expected 429, got %d", got)
	}

	if got := call("user-2", "5.5.5.5:1"); got != http.StatusOK {
		t.Fatalf("other user: expected 200, got %d", got)
	}
}

func TestTrustedProxiesEnv_FallsBackToGoosarTrustedProxies(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "")
	t.Setenv("GOOSAR_TRUSTED_PROXIES", "10.0.0.0/8")
	nets := TrustedProxiesFromEnv()
	if len(nets) != 1 || nets[0].String() != "10.0.0.0/8" {
		t.Fatalf("expected GOOSAR_TRUSTED_PROXIES to be honoured, got %v", nets)
	}
}

func TestRateLimitByUserOrIP_ScopesDoNotShareACounter(t *testing.T) {
	store := NewMemoryRateLimitStore()
	apiRL := RateLimitByUserOrIP(store, "api", 2, time.Minute, nil)(okHandler)
	tokenRL := RateLimitByUserOrIP(store, "token", 2, time.Hour, nil)(okHandler)

	call := func(h http.Handler) int {
		req := httptest.NewRequest(http.MethodPost, "/api/tokens", nil)
		req.RemoteAddr = "7.7.7.7:1"
		req.Header.Set("X-User-ID", "user-scopes")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 2; i++ {
		if got := call(apiRL); got != http.StatusOK {
			t.Fatalf("api call %d: expected 200, got %d", i, got)
		}
	}
	if got := call(apiRL); got != http.StatusTooManyRequests {
		t.Fatalf("api budget must run out on its own: expected 429, got %d", got)
	}
	if got := call(tokenRL); got != http.StatusOK {
		t.Fatalf("the token budget must be untouched by API traffic, got %d", got)
	}
}

func TestRateLimitByJSONFieldHashed_BoundsGuessesPerTicket(t *testing.T) {
	store := NewMemoryRateLimitStore()
	handler := RateLimitByJSONFieldHashed(store, "mfa_token", 2, time.Minute)(okHandler)

	call := func(ticket, addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/verify",
			strings.NewReader(`{"mfa_token":"`+ticket+`","code":"000000"}`))
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := range 2 {
		if got := call("ticket-one", "1.1.1.1:1"); got != http.StatusOK {
			t.Fatalf("guess %d: expected 200, got %d", i, got)
		}
	}

	if got := call("ticket-one", "2.2.2.2:2"); got != http.StatusTooManyRequests {
		t.Fatalf("third guess on the same ticket: expected 429, got %d", got)
	}

	if got := call("ticket-two", "2.2.2.2:2"); got != http.StatusOK {
		t.Fatalf("second ticket: expected 200, got %d", got)
	}
}

func TestRateLimitByJSONField_RefusesOversizedBody(t *testing.T) {
	store := NewMemoryRateLimitStore()
	handler := RateLimitByJSONFieldHashed(store, "mfa_token", 2, time.Minute)(okHandler)

	padded := `{"mfa_token":"TICKET","code":"000000","pad":"` + strings.Repeat("x", 9000) + `"}`
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/api/auth/mfa/verify", strings.NewReader(padded))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("request %d: status = %d, want 413 (oversized body must not bypass the budget)", i, w.Code)
		}
	}
}

type erroringRateLimitStore struct{}

func (erroringRateLimitStore) Backend() string { return "redis" }
func (erroringRateLimitStore) Incr(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 0, 0, errors.New("dial tcp: connection refused")
}

func TestKeyedRateLimit_FallsBackToMemoryOnStoreError(t *testing.T) {
	handler := RateLimitByJSONField(erroringRateLimitStore{}, "username", 2, time.Minute)(okHandler)

	allowed := 0
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("POST", "/api/auth/ldap/login", strings.NewReader(`{"username":"victim","password":"x"}`))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed > 2 {
		t.Fatalf("allowed %d of 20 requests while the backend was down, want at most 2", allowed)
	}
}

func TestRateLimitByJSONField_CountsPerRoute(t *testing.T) {
	store := NewMemoryRateLimitStore()
	handler := RateLimitByJSONField(store, "email", 2, time.Minute)(okHandler)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/send-code", strings.NewReader(`{"email":"victim@corp.example"}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("send-code %d: status = %d, want 200", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/verify-code", strings.NewReader(`{"email":"victim@corp.example","code":"123456"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("verify-code status = %d, want 200 — send-code must not spend the verify budget", w.Code)
	}
}
