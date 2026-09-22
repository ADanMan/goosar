package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

var defaultLoggerMu sync.Mutex

func withCapturedLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	defaultLoggerMu.Lock()
	buf := &bytes.Buffer{}
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(orig)
		defaultLoggerMu.Unlock()
	})
	return buf
}

func runRequestLogger(t *testing.T, status int, body string) *bytes.Buffer {
	t.Helper()
	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/heartbeat", nil).
		WithContext(context.Background())
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return logs
}

func requireLogLevel(t *testing.T, logs *bytes.Buffer, want string, disallowed ...string) {
	t.Helper()
	out := logs.String()
	if !strings.Contains(out, "level="+want) {
		t.Fatalf("expected level=%s in logs, got:\n%s", want, out)
	}
	for _, dis := range disallowed {
		if strings.Contains(out, "level="+dis) {
			t.Fatalf("did not expect level=%s in logs, got:\n%s", dis, out)
		}
	}
}

func TestRequestLogger_RuntimeNotFound404DowngradesToInfo(t *testing.T) {

	logs := runRequestLogger(t, http.StatusNotFound, `{"error":"runtime not found"}`)
	requireLogLevel(t, logs, "INFO", "WARN", "ERROR")
}

func TestRequestLogger_TaskNotFound404DowngradesToInfo(t *testing.T) {
	logs := runRequestLogger(t, http.StatusNotFound, `{"error":"task not found"}`)
	requireLogLevel(t, logs, "INFO", "WARN", "ERROR")
}

func TestRequestLogger_GenericNotFound404KeepsWarn(t *testing.T) {

	logs := runRequestLogger(t, http.StatusNotFound, `{"error":"not found"}`)
	requireLogLevel(t, logs, "WARN", "INFO", "ERROR")
}

func TestRequestLogger_400StaysWarn(t *testing.T) {
	logs := runRequestLogger(t, http.StatusBadRequest, `{"error":"bad input"}`)
	requireLogLevel(t, logs, "WARN", "INFO", "ERROR")
}

func TestRequestLogger_500StaysError(t *testing.T) {
	logs := runRequestLogger(t, http.StatusInternalServerError, `{"error":"boom"}`)
	requireLogLevel(t, logs, "ERROR", "WARN", "INFO")
}

func TestRequestLogger_200StaysInfo(t *testing.T) {
	logs := runRequestLogger(t, http.StatusOK, `{"ok":true}`)
	requireLogLevel(t, logs, "INFO", "WARN", "ERROR")
}

func TestRequestLogger_HealthEndpointIsSkipped(t *testing.T) {
	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if logs.Len() != 0 {
		t.Fatalf("/health should not be logged, got:\n%s", logs.String())
	}
}

func TestRequestLogger_BodyStillReachesClient(t *testing.T) {

	rec := httptest.NewRecorder()
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"runtime not found"}`))
	}))
	_ = withCapturedLogs(t)
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/daemon/heartbeat", nil))
	if got := rec.Body.String(); got != `{"error":"runtime not found"}` {
		t.Fatalf("response body lost or mutated: got %q", got)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRequestLogger_LargeBodyBeyondCaptureLimit(t *testing.T) {

	prefix := strings.Repeat("x", softNotFoundBodyCaptureLimit+8)
	logs := runRequestLogger(t, http.StatusNotFound, prefix+`{"error":"runtime not found"}`)
	requireLogLevel(t, logs, "WARN", "INFO", "ERROR")
}

func TestRedactWebhookPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"/api/webhooks/autopilots/awt_secret", "/api/webhooks/autopilots/[redacted]"},
		{"/api/webhooks/autopilots/awt_secret/", "/api/webhooks/autopilots/[redacted]/"},
		{"/api/webhooks/autopilots/", "/api/webhooks/autopilots/"},
		{"/api/webhooks/github", "/api/webhooks/github"},
		{"/api/runtimes/abc", "/api/runtimes/abc"},
		{"/", "/"},
	}
	for _, tc := range cases {
		if got := redactWebhookPath(tc.in); got != tc.want {
			t.Errorf("redactWebhookPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRequestLogger_RedactsWebhookTokenInPath(t *testing.T) {
	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/autopilots/awt_supersecret", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	out := logs.String()
	if strings.Contains(out, "awt_supersecret") {
		t.Fatalf("token leaked into logs:\n%s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("expected [redacted] in logs:\n%s", out)
	}
}

func TestRequestLogger_IncludesWebhookTriggerIDFromContext(t *testing.T) {

	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetWebhookTriggerID(r, "trigger-abc")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/autopilots/awt_supersecret", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	out := logs.String()
	if !strings.Contains(out, "webhook_trigger_id=trigger-abc") {
		t.Fatalf("expected webhook_trigger_id in logs, got:\n%s", out)
	}
	if strings.Contains(out, "awt_supersecret") {
		t.Fatalf("token leaked into logs:\n%s", out)
	}
}

func TestRequestLoggerNeverLogsTheQueryString(t *testing.T) {
	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	req := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?link_token=lt_supersecret&token=jwt_supersecret",
		nil,
	).WithContext(context.Background())
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "/auth/callback") {
		t.Fatalf("expected the path in logs, got:\n%s", out)
	}
	for _, secret := range []string{"lt_supersecret", "jwt_supersecret", "link_token"} {
		if strings.Contains(out, secret) {
			t.Fatalf("query string leaked into logs (%q):\n%s", secret, out)
		}
	}
}

func TestIsSoftNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		body string
		want bool
	}{
		{`{"error":"runtime not found"}`, true},
		{`{"error":"task not found"}`, true},
		{`{"error":"Runtime Not Found"}`, true},
		{`{"error":"not found"}`, false},
		{`{"error":"workspace not found"}`, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isSoftNotFound([]byte(tc.body)); got != tc.want {
			t.Errorf("isSoftNotFound(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestRequestLoggerReturnsRequestIDHeader(t *testing.T) {
	logs := withCapturedLogs(t)

	var seenByHandler string
	handler := chimw.RequestID(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		seenByHandler = w.Header().Get("X-Request-ID")
		w.WriteHeader(http.StatusInternalServerError)
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/anything", nil))

	got := rec.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("response carries no X-Request-ID")
	}
	if seenByHandler != got {
		t.Fatalf("handler saw %q, client got %q", seenByHandler, got)
	}
	if !strings.Contains(logs.String(), got) {
		t.Fatalf("log line does not carry request_id %q: %s", got, logs.String())
	}
}

func TestRequestIDRejectsHostileInboundValues(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"proxy id adopted":  {"c0ffee-000012", "c0ffee-000012"},
		"uuid adopted":      {"3f2504e0-4f89-11d3-9a0c-0305e82c3301", "3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		"oversized dropped": {strings.Repeat("a", maxRequestIDLen+1), ""},
		"spaces dropped":    {"id status=200 msg=owned", ""},
		"quotes dropped":    {`"},{"level":"fake`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got string
			h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = chimw.GetReqID(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
			req.Header.Set("X-Request-Id", tc.in)
			h.ServeHTTP(httptest.NewRecorder(), req)

			if tc.want != "" {
				if got != tc.want {
					t.Fatalf("request id = %q, want the inbound %q", got, tc.want)
				}
				return
			}
			if got == tc.in || got == "" {
				t.Fatalf("hostile inbound id was not replaced: %q", got)
			}
		})
	}
}
