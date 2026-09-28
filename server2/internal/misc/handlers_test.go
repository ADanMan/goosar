package misc

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func withActor(r *http.Request, userID string, human bool) *http.Request {
	actor := &httpapi.Actor{UserID: userID, IsHuman: human, Source: httpapi.SourceSession}
	return r.WithContext(httpapi.WithActor(r.Context(), actor))
}

func newTestDeps(t *testing.T) (*Deps, string, string) {
	t.Helper()
	db := newTestDB(t)
	s := NewStore(db)
	accountID, workspaceID := seedAccountAndWorkspace(t, s)
	d := New(db, 5, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return d, accountID, workspaceID
}

func TestCreateFeedbackHappyPathAndRateLimit(t *testing.T) {
	d, accountID, _ := newTestDeps(t)
	body := func() *strings.Reader { return strings.NewReader(`{"message":"hello there"}`) }

	req := withActor(httptest.NewRequest(http.MethodPost, "/api/feedback", body()), accountID, true)
	rec := httptest.NewRecorder()
	d.handleCreateFeedback(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	for i := 0; i < feedbackPerHour-1; i++ {
		req := withActor(httptest.NewRequest(http.MethodPost, "/api/feedback", body()), accountID, true)
		rec := httptest.NewRecorder()
		d.handleCreateFeedback(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("iteration %d: expected 201, got %d", i, rec.Code)
		}
	}
	// now at the limit — next one should be 429
	req = withActor(httptest.NewRequest(http.MethodPost, "/api/feedback", body()), accountID, true)
	rec = httptest.NewRecorder()
	d.handleCreateFeedback(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding limit, got %d", rec.Code)
	}
}

func TestCreateFeedbackRejectsEmptyMessage(t *testing.T) {
	d, accountID, _ := newTestDeps(t)
	req := withActor(httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(`{"message":""}`)), accountID, true)
	rec := httptest.NewRecorder()
	d.handleCreateFeedback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func validContactSalesBody() string {
	return `{"first_name":"A","last_name":"B","business_email":"a@company.test",` +
		`"company_name":"Acme","company_size":"1-10","country_region":"US","use_case":"evaluate"}`
}

func TestContactSalesHappyPath(t *testing.T) {
	d, _, _ := newTestDeps(t)
	req := httptest.NewRequest(http.MethodPost, "/api/contact-sales", strings.NewReader(validContactSalesBody()))
	rec := httptest.NewRecorder()
	d.handleContactSales(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContactSalesRejectsFreeMailDomain(t *testing.T) {
	d, _, _ := newTestDeps(t)
	body := `{"first_name":"A","last_name":"B","business_email":"a@gmail.com",` +
		`"company_name":"Acme","company_size":"1-10","country_region":"US","use_case":"evaluate"}`
	req := httptest.NewRequest(http.MethodPost, "/api/contact-sales", strings.NewReader(body))
	rec := httptest.NewRecorder()
	d.handleContactSales(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for free-mail domain, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContactSalesEmailRateLimit(t *testing.T) {
	d, _, _ := newTestDeps(t)
	for i := 0; i < contactSalesEmailPerHour; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/contact-sales", strings.NewReader(validContactSalesBody()))
		req.RemoteAddr = "10.0.0." + string(rune('1'+i)) + ":1234"
		rec := httptest.NewRecorder()
		d.handleContactSales(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("iteration %d: expected 201, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/contact-sales", strings.NewReader(validContactSalesBody()))
	rec := httptest.NewRecorder()
	d.handleContactSales(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding email cap, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestClientUsageValidation(t *testing.T) {
	d, accountID, _ := newTestDeps(t)

	// missing X-Client-Platform
	req := withActor(httptest.NewRequest(http.MethodPost, "/api/client-usage", strings.NewReader(`{"install_id":"11111111-1111-1111-1111-111111111111"}`)), accountID, true)
	rec := httptest.NewRecorder()
	d.handleClientUsage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without X-Client-Platform, got %d", rec.Code)
	}

	// web platform, no runtime — ok
	req = withActor(httptest.NewRequest(http.MethodPost, "/api/client-usage", strings.NewReader(`{"install_id":"11111111-1111-1111-1111-111111111111"}`)), accountID, true)
	req.Header.Set("X-Client-Platform", "web")
	rec = httptest.NewRecorder()
	d.handleClientUsage(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// web platform with runtime data — rejected
	req = withActor(httptest.NewRequest(http.MethodPost, "/api/client-usage", strings.NewReader(
		`{"install_id":"11111111-1111-1111-1111-111111111111","runtime":{"probe_result":"success"}}`)), accountID, true)
	req.Header.Set("X-Client-Platform", "web")
	rec = httptest.NewRecorder()
	d.handleClientUsage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for runtime data from web client, got %d", rec.Code)
	}

	// desktop with consistent runtime counts — ok, upsert idempotent
	desktopBody := `{"install_id":"11111111-1111-1111-1111-111111111111","runtime":{"probe_result":"success","runtime_count":3,"online_count":2,"offline_count":1}}`
	for i := 0; i < 2; i++ {
		req = withActor(httptest.NewRequest(http.MethodPost, "/api/client-usage", strings.NewReader(desktopBody)), accountID, true)
		req.Header.Set("X-Client-Platform", "desktop")
		rec = httptest.NewRecorder()
		d.handleClientUsage(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("desktop attempt %d: expected 204, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	// desktop with inconsistent counts — rejected
	req = withActor(httptest.NewRequest(http.MethodPost, "/api/client-usage", strings.NewReader(
		`{"install_id":"11111111-1111-1111-1111-111111111111","runtime":{"probe_result":"success","runtime_count":3,"online_count":1,"offline_count":1}}`)), accountID, true)
	req.Header.Set("X-Client-Platform", "desktop")
	rec = httptest.NewRecorder()
	d.handleClientUsage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for inconsistent runtime counts, got %d", rec.Code)
	}
}

func TestWorkspaceStatus(t *testing.T) {
	d, accountID, workspaceID := newTestDeps(t)
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/status", nil), accountID, true)
	req.Header.Set("X-Workspace-ID", workspaceID)
	rec := httptest.NewRecorder()
	d.handleWorkspaceStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, key := range []string{`"workspace"`, `"caller"`, `"runtimes"`, `"provisioning"`, `"mcp"`, `"perimeter"`, `"llm"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Fatalf("expected response to contain %s, got %s", key, rec.Body.String())
		}
	}
}

func TestWorkspaceStatusRequiresWorkspaceContext(t *testing.T) {
	d, accountID, _ := newTestDeps(t)
	req := withActor(httptest.NewRequest(http.MethodGet, "/api/status", nil), accountID, true)
	rec := httptest.NewRecorder()
	d.handleWorkspaceStatus(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without workspace context, got %d", rec.Code)
	}
}
