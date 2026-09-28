package billing

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func withActor(r *http.Request) *http.Request {
	actor := &httpapi.Actor{UserID: "user-1", IsHuman: true, Source: httpapi.SourceSession}
	return r.WithContext(httpapi.WithActor(r.Context(), actor))
}

func TestBalanceProxiesBodyAndStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/billing/balance" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if r.Header.Get("X-User-ID") != "user-1" {
			t.Fatalf("expected X-User-ID forwarded, got %q", r.Header.Get("X-User-ID"))
		}
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			t.Fatalf("expected Authorization forwarded, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"owner_id": "user-1", "balance_micro": 42})
	}))
	defer upstream.Close()

	deps := New(upstream.URL, "upstream-key", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil))
	rec := httptest.NewRecorder()
	deps.proxy("/api/v1/billing/balance", false)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"balance_micro":42`) {
		t.Fatalf("expected relayed body, got %s", rec.Body.String())
	}
}

func TestBalanceUnconfiguredReturns503(t *testing.T) {
	deps := New("", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/cloud-billing/balance", nil))
	rec := httptest.NewRecorder()
	deps.proxy("/api/v1/billing/balance", false)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when not configured, got %d", rec.Code)
	}
}

func TestCheckoutSessionValidatesSessionIDFormat(t *testing.T) {
	deps := New("http://example.invalid", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/cloud-billing/checkout-sessions/bad!id", nil))
	req.SetPathValue("sessionId", "bad!id")
	rec := httptest.NewRecorder()
	deps.handleGetCheckoutSession(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid sessionId, got %d", rec.Code)
	}
}

func TestCreateCheckoutSessionRequiresBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called without a body")
	}))
	defer upstream.Close()
	deps := New(upstream.URL, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := withActor(httptest.NewRequest(http.MethodPost, "/api/cloud-billing/checkout-sessions", nil))
	rec := httptest.NewRecorder()
	deps.proxy("/api/v1/billing/checkout-sessions", true)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without body, got %d", rec.Code)
	}
}

func TestStripeWebhookRequiresSignatureHeader(t *testing.T) {
	deps := New("http://example.invalid", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	deps.handleStripeWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without Stripe-Signature, got %d", rec.Code)
	}
}

func TestStripeWebhookForwardsVerbatim(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/webhooks/stripe" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Stripe-Signature") != "t=1,v1=abc" {
			t.Fatalf("expected Stripe-Signature forwarded, got %q", r.Header.Get("Stripe-Signature"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"type":"checkout.session.completed"}` {
			t.Fatalf("unexpected forwarded body: %s", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	deps := New(upstream.URL, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe", strings.NewReader(`{"type":"checkout.session.completed"}`))
	req.Header.Set("Stripe-Signature", "t=1,v1=abc")
	rec := httptest.NewRecorder()
	deps.handleStripeWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
