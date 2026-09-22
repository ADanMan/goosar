package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/adanman/goosar/server/internal/auth"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestDaemonAuth_DaemonTokenCacheHit(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}

	const rawToken = "mdt_cache_hit_test_token"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, auth.DaemonTokenIdentity{
		WorkspaceID: "ws-cached",
		DaemonID:    "daemon-cached",
	}, auth.AuthCacheTTL)

	var gotWS, gotDaemon, gotPath string
	mw := DaemonAuth(nil, nil, cache, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWS = DaemonWorkspaceIDFromContext(r.Context())
		gotDaemon = DaemonIDFromContext(r.Context())
		gotPath = DaemonAuthPathFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on cache hit, got %d: %s", w.Code, w.Body.String())
	}
	if gotWS != "ws-cached" || gotDaemon != "daemon-cached" {
		t.Fatalf("expected (ws-cached, daemon-cached), got (%q, %q)", gotWS, gotDaemon)
	}
	if gotPath != DaemonAuthPathDaemonToken {
		t.Fatalf("expected auth path %q, got %q", DaemonAuthPathDaemonToken, gotPath)
	}
}

func TestDaemonAuth_PATCacheHit(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewPATCache(rdb)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}

	const rawToken = auth.PATPrefix + "daemon_pat_cache_hit_test"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, "cached-user-id", auth.AuthCacheTTL)

	var gotUserID, gotPath string
	mw := DaemonAuth(nil, cache, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = r.Header.Get("X-User-ID")
		gotPath = DaemonAuthPathFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUserID != "cached-user-id" {
		t.Fatalf("expected cached X-User-ID, got %q", gotUserID)
	}
	if gotPath != DaemonAuthPathPAT {
		t.Fatalf("expected auth path %q, got %q", DaemonAuthPathPAT, gotPath)
	}
}

func TestDaemonAuth_MissingAuth(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestDaemonAuth_StripsClientSuppliedActorSource(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)

	const rawToken = "mdt_strip_test"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, auth.DaemonTokenIdentity{
		WorkspaceID: "ws-1",
		DaemonID:    "daemon-1",
	}, auth.AuthCacheTTL)

	var gotActorSource string
	mw := DaemonAuth(nil, nil, cache, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActorSource = r.Header.Get("X-Actor-Source")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	req.Header.Set("X-Actor-Source", "cloud_pat")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotActorSource != "" {
		t.Fatalf("X-Actor-Source must be cleared on the mdt_ path, got %q", gotActorSource)
	}
}

func TestDaemonAuth_InvalidMDT_NilQueries(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mdt_unknown")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestDaemonAuth_MCN_NoVerifierConfigured(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when verifier is unconfigured")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+auth.CloudPATPrefix+"anything")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no verifier, got %d", w.Code)
	}
}

func TestDaemonAuth_MCN_ValidTokenSetsUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"valid": true,
			"owner_id": "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001",
			"instance_id": "i-01"
		}`))
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	var gotUser, gotPath, gotActorSource string
	mw := DaemonAuth(nil, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User-ID")
		gotPath = DaemonAuthPathFromContext(r.Context())
		gotActorSource = r.Header.Get("X-Actor-Source")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+auth.CloudPATPrefix+"some_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUser != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
		t.Errorf("expected owner_id propagated as X-User-ID, got %q", gotUser)
	}
	if gotPath != DaemonAuthPathCloudPAT {
		t.Errorf("expected auth path %q, got %q", DaemonAuthPathCloudPAT, gotPath)
	}

	if gotActorSource != "cloud_pat" {
		t.Errorf("expected X-Actor-Source=cloud_pat, got %q", gotActorSource)
	}
}

func TestDaemonAuth_MCN_FleetSaysInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":false,"reason":"token_revoked"}`))
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	mw := DaemonAuth(nil, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when fleet says invalid")
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+auth.CloudPATPrefix+"revoked")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", w.Code)
	}
}

func TestDaemonAuth_MCN_FleetUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	mw := DaemonAuth(nil, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when fleet is unavailable")
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+auth.CloudPATPrefix+"x")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when fleet is unavailable, got %d", w.Code)
	}
}

func TestDaemonAuth_MCN_OwnerNotInLocalDB(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{
			"valid": true,
			"owner_id": "00000000-0000-0000-0000-0000000feed1",
			"instance_id": "i-99"
		}`))
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	mw := DaemonAuth(queries, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when owner_id has no local user")
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+auth.CloudPATPrefix+"phantom_owner")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when local user is missing, got %d", w.Code)
	}
}

func TestDaemonAuth_RefusesMFATicket(t *testing.T) {
	ticket := signTestJWT(t, jwt.MapClaims{
		"sub": "11111111-1111-1111-1111-111111111111",
		"typ": "mfa",
		"exp": jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
	})

	reached := false
	mw := DaemonAuth(nil, nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/daemon/workspaces", nil)
	req.Header.Set("Authorization", "Bearer "+ticket)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if reached {
		t.Fatal("mfa ticket reached a daemon handler")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an mfa ticket, got %d: %s", w.Code, w.Body.String())
	}
}

func signTestJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}
