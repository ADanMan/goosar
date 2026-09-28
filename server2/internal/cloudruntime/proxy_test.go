package cloudruntime

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// withActor кладёт минимального человека-актора в контекст, обходя
// wsctx.Resolver: эти тесты — про сам прокси-слой (forwardAndRespond), не
// про резолв воркспейса/членство, уже покрытые wsctx/pin/note тестами тем же
// приёмом — proxy() сам зовёт d.Resolver.RequireMember первой строкой и
// делегирует остальное forwardAndRespond, которое тестируется здесь напрямую.
func withActor(r *http.Request, userID string) *http.Request {
	a := &httpapi.Actor{UserID: userID, IsHuman: true}
	return r.WithContext(httpapi.WithActor(r.Context(), a))
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestDeps_Configured(t *testing.T) {
	if (&Deps{}).Configured() {
		t.Fatal("Configured() = true without BaseURL set")
	}
	if !(&Deps{BaseURL: "http://fleet.internal"}).Configured() {
		t.Fatal("Configured() = false with BaseURL set")
	}
}

func TestForwardAndRespond_ForwardsMethodBodyHeadersAndQuery(t *testing.T) {
	var gotMethod, gotPath, gotUserID, gotAuth, gotQuery string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotUserID = r.Header.Get("X-User-ID")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	d := &Deps{BaseURL: upstream.URL, APIKey: "test-key", Client: upstream.Client(), Logger: discardLogger()}

	req := httptest.NewRequest(http.MethodPost, "/api/cloud-runtime/nodes/start?foo=bar",
		strings.NewReader(`{"instance_id":"i-1"}`))
	req = withActor(req, "user-123")
	rec := httptest.NewRecorder()

	d.forwardAndRespond(rec, req, "/nodes/start", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotMethod != http.MethodPost || gotPath != "/nodes/start" {
		t.Errorf("upstream got method=%s path=%s, want POST /nodes/start", gotMethod, gotPath)
	}
	if gotQuery != "foo=bar" {
		t.Errorf("upstream got query=%q, want foo=bar", gotQuery)
	}
	if gotUserID != "user-123" {
		t.Errorf("X-User-ID forwarded = %q, want user-123", gotUserID)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization forwarded = %q, want 'Bearer test-key'", gotAuth)
	}
	if string(gotBody) != `{"instance_id":"i-1"}` {
		t.Errorf("body forwarded = %q", gotBody)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("response body = %q, want upstream body verbatim", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("response Content-Type = %q, want application/json (proxied from upstream)", ct)
	}
}

func TestForwardAndRespond_RequireBody_RejectsEmptyOrInvalidJSON(t *testing.T) {
	d := &Deps{BaseURL: "http://unused.invalid", Client: http.DefaultClient, Logger: discardLogger()}

	rec := httptest.NewRecorder()
	req := withActor(httptest.NewRequest(http.MethodPost, "/api/cloud-runtime/nodes", nil), "u1")
	d.forwardAndRespond(rec, req, "/nodes", true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty body: status = %d, want 400", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	req2 := withActor(httptest.NewRequest(http.MethodPost, "/api/cloud-runtime/nodes", strings.NewReader("not json")), "u1")
	d.forwardAndRespond(rec2, req2, "/nodes", true)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON body: status = %d, want 400", rec2.Code)
	}
}

func TestForwardAndRespond_NotConfigured_ReturnsServiceUnavailable(t *testing.T) {
	d := &Deps{Client: http.DefaultClient, Logger: discardLogger()} // BaseURL пуст
	rec := httptest.NewRecorder()
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/cloud-runtime/nodes", nil), "u1")
	d.forwardAndRespond(rec, req, "/nodes", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestForwardAndRespond_UpstreamUnreachable_ReturnsBadGateway(t *testing.T) {
	d := &Deps{BaseURL: "http://127.0.0.1:1", Client: http.DefaultClient, Logger: discardLogger()}
	rec := httptest.NewRecorder()
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/cloud-runtime/nodes", nil), "u1")
	d.forwardAndRespond(rec, req, "/nodes", false)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}
