package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/corpauth"
)

func stateRoundTrip(t *testing.T, h *Handler, client string) (oidcState, *http.Request) {
	t.Helper()
	st, err := newOIDCState(client)
	if err != nil {
		t.Fatalf("newOIDCState: %v", err)
	}
	w := httptest.NewRecorder()
	if err := h.setOIDCStateCookie(w, st); err != nil {
		t.Fatalf("setOIDCStateCookie: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=c&state="+st.State, nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	return st, r
}

func stateTestHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{}
}

func TestOIDCState_RoundTrips(t *testing.T) {
	h := stateTestHandler(t)
	st, r := stateRoundTrip(t, h, oidcClientDesktop)

	got, err := h.readOIDCState(r, st.State)
	if err != nil {
		t.Fatalf("readOIDCState: %v", err)
	}
	if got.Nonce != st.Nonce {
		t.Errorf("nonce did not survive the round trip")
	}
	if got.Verifier != st.Verifier {
		t.Errorf("PKCE verifier did not survive the round trip")
	}

	if got.Client != oidcClientDesktop {
		t.Errorf("client: got %q, want %q", got.Client, oidcClientDesktop)
	}
}

func TestOIDCState_Refusals(t *testing.T) {
	h := stateTestHandler(t)

	t.Run("state echoed back does not match", func(t *testing.T) {
		_, r := stateRoundTrip(t, h, oidcClientWeb)
		if _, err := h.readOIDCState(r, "some-other-state"); err == nil {
			t.Fatal("a mismatched state verified")
		}
	})

	t.Run("no cookie at all", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=x", nil)
		if _, err := h.readOIDCState(r, "x"); err == nil {
			t.Fatal("a callback with no state cookie verified")
		}
	})

	t.Run("cookie signed with a different key", func(t *testing.T) {
		forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"typ": oidcStateTokenType,
			"st":  "attacker-state", "non": "n", "ver": "v", "cl": oidcClientWeb,
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		signed, err := forged.SignedString([]byte("not-this-deployments-secret"))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback", nil)
		r.AddCookie(&http.Cookie{Name: oidcStateCookieName, Value: signed})
		if _, err := h.readOIDCState(r, "attacker-state"); err == nil {
			t.Fatal("a state cookie signed with a foreign key verified")
		}
	})

	t.Run("expired cookie", func(t *testing.T) {
		expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"typ": oidcStateTokenType,
			"st":  "stale-state", "non": "n", "ver": "v", "cl": oidcClientWeb,
			"exp": time.Now().Add(-time.Minute).Unix(),
		})
		signed, err := expired.SignedString(auth.JWTSecret())
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback", nil)
		r.AddCookie(&http.Cookie{Name: oidcStateCookieName, Value: signed})
		if _, err := h.readOIDCState(r, "stale-state"); err == nil {
			t.Fatal("an expired state cookie verified")
		}
	})

	t.Run("a session token is not a state cookie", func(t *testing.T) {
		session := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "11111111-1111-1111-1111-111111111111",
			"st":  "pretend-state", "non": "n", "ver": "v",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		signed, err := session.SignedString(auth.JWTSecret())
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback", nil)
		r.AddCookie(&http.Cookie{Name: oidcStateCookieName, Value: signed})
		if _, err := h.readOIDCState(r, "pretend-state"); err == nil {
			t.Fatal("a session token was accepted as an authorization state")
		}
	})
}

func TestOIDCState_ReplayIsRefused(t *testing.T) {
	h := stateTestHandler(t)
	st, r := stateRoundTrip(t, h, oidcClientWeb)

	if _, err := h.readOIDCState(r, st.State); err != nil {
		t.Fatalf("first callback: %v", err)
	}

	w := httptest.NewRecorder()
	h.clearOIDCStateCookie(w)
	cleared := w.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("clearOIDCStateCookie did not expire the cookie: %+v", cleared)
	}

	replay := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=c&state="+st.State, nil)
	for _, c := range cleared {
		if c.Value != "" {
			replay.AddCookie(c)
		}
	}
	if _, err := h.readOIDCState(replay, st.State); err == nil {
		t.Fatal("a replayed callback verified a second time")
	}
}

func TestOIDCStateCookie_Attributes(t *testing.T) {
	h := stateTestHandler(t)
	st, err := newOIDCState(oidcClientWeb)
	if err != nil {
		t.Fatalf("newOIDCState: %v", err)
	}
	w := httptest.NewRecorder()
	if err := h.setOIDCStateCookie(w, st); err != nil {
		t.Fatalf("setOIDCStateCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: got %d, want 1", len(cookies))
	}
	c := cookies[0]

	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite: got %v, want Lax — Strict is withheld on the return from the IdP", c.SameSite)
	}
	if !c.HttpOnly {
		t.Error("HttpOnly is off: script on the page can read the authorization state")
	}
	if !strings.HasPrefix(c.Path, "/api/auth/oidc") {
		t.Errorf("Path: got %q, want the cookie scoped to the callback rather than riding every API request", c.Path)
	}
	if c.MaxAge <= 0 || time.Duration(c.MaxAge)*time.Second > oidcStateTTL {
		t.Errorf("MaxAge: got %d, want a positive value no greater than the state TTL", c.MaxAge)
	}

	if strings.Contains(c.Value, st.Verifier) {
		t.Error("the cookie carries the PKCE verifier in the clear")
	}
}

func TestOIDCState_IsFreshPerRequest(t *testing.T) {
	h := stateTestHandler(t)
	a, _ := stateRoundTrip(t, h, oidcClientWeb)
	b, _ := stateRoundTrip(t, h, oidcClientWeb)

	if a.State == b.State || a.Nonce == b.Nonce || a.Verifier == b.Verifier {
		t.Fatal("two flows share state, nonce or verifier")
	}
}

func TestOIDCEndpoints_ClosedWhenMethodDisabled(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}
	t.Setenv(corpauth.MethodsEnvVar, "email")
	prev := testHandler.OIDC
	testHandler.OIDC = nil
	t.Cleanup(func() { testHandler.OIDC = prev })

	for _, tc := range []struct {
		name    string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"start", "/api/auth/oidc/start", testHandler.StartOIDC},
		{"callback", "/api/auth/oidc/callback?code=c&state=s", testHandler.CallbackOIDC},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("status: got %d, want 404", w.Code)
			}
		})
	}
}
