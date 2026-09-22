package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRequireHumanActor_AllowsHumanRequest(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireHumanActor(next)

	req := httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil)

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !called {
		t.Fatal("inner handler must run for non-task-token requests")
	}
}

func TestRequireHumanActor_BlocksMachineCredentials(t *testing.T) {
	cases := []struct {
		name        string
		actorSource string
	}{

		{name: "task_token", actorSource: "task_token"},

		{name: "cloud_pat", actorSource: "cloud_pat"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatalf("inner handler must NOT run for actor source %q", tc.actorSource)
			})
			mw := RequireHumanActor(next)

			req := httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil)

			req.Header.Set("X-Actor-Source", tc.actorSource)
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
}

func TestRequireHumanActor_IgnoresUnknownActorSource(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireHumanActor(next)

	req := httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil)
	req.Header.Set("X-Actor-Source", "future_kind")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — gate should only block exact 'task_token'", w.Code)
	}
	if !called {
		t.Fatal("inner handler must run for unknown actor sources")
	}
}

func TestRequireHumanActor_AppliedViaChiRouterUse(t *testing.T) {

	r := chi.NewRouter()
	r.Use(RequireHumanActor)
	r.Get("/billing/probe", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler must NOT run when guard rejects")
	})

	req := httptest.NewRequest(http.MethodGet, "/billing/probe", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
