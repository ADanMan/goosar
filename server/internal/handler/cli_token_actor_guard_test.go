package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/adanman/goosar/server/internal/auth"
)

func TestIssueCliToken_RejectsTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	r := chi.NewRouter()
	r.With(RequireHumanActor).Post("/api/cli-token", testHandler.IssueCliToken)

	req := newRequest("POST", "/api/cli-token", nil)

	req.Header.Set("X-Actor-Source", "task_token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("POST /api/cli-token with X-Actor-Source=task_token: status = %d, want 403: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err == nil {
		if _, hasToken := body["token"]; hasToken {
			t.Fatalf("response leaked a token for a rejected machine-credential request: %s", w.Body.String())
		}
	}
}

func TestIssueCliToken_AllowsHumanActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	r := chi.NewRouter()
	r.With(RequireHumanActor).Post("/api/cli-token", testHandler.IssueCliToken)

	req := newRequest("POST", "/api/cli-token", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/cli-token as human actor: status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["token"] == "" {
		t.Fatal("expected a non-empty token in the response")
	}
}

func TestIssueCliToken_CarriesSessionID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	r := chi.NewRouter()
	r.With(RequireHumanActor).Post("/api/cli-token", testHandler.IssueCliToken)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest("POST", "/api/cli-token", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	token, err := auth.ParseSessionHS256(body["token"])
	if err != nil {
		t.Fatalf("parse cli token: %v", err)
	}
	claims, _ := token.Claims.(jwt.MapClaims)
	if sid, _ := claims["sid"].(string); sid == "" {
		t.Fatal("cli token carries no sid claim — it escapes the session policy")
	}
}
