package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/middleware"
)

func TestMinDaemonVersionFromEnv(t *testing.T) {
	t.Setenv("GOOSAR_MIN_DAEMON_VERSION", "")
	if got := MinDaemonVersion(); got != DefaultMinDaemonVersion {
		t.Fatalf("default = %q, want %q", got, DefaultMinDaemonVersion)
	}
	t.Setenv("GOOSAR_MIN_DAEMON_VERSION", "1.2.3")
	if got := MinDaemonVersion(); got != "1.2.3" {
		t.Fatalf("override = %q, want 1.2.3", got)
	}
	for _, off := range []string{"none", "NONE", "0"} {
		t.Setenv("GOOSAR_MIN_DAEMON_VERSION", off)
		if got := MinDaemonVersion(); got != "" {
			t.Fatalf("%q must disable the gate, got %q", off, got)
		}
	}
}

func TestRequireMinDaemonVersion(t *testing.T) {
	t.Setenv("GOOSAR_MIN_DAEMON_VERSION", "1.0.0")

	cases := []struct {
		name       string
		version    string
		wantStatus int
	}{
		{"current daemon passes", "1.0.0", http.StatusOK},
		{"newer daemon passes", "1.4.2", http.StatusOK},
		{"v-prefixed passes", "v1.0.0", http.StatusOK},
		{"older daemon is refused", "0.9.9", http.StatusUpgradeRequired},
		{"dev build passes", "v0.2.15-235-gdaf0e935", http.StatusOK},
		{"missing version passes", "", http.StatusOK},
		{"unparsable version passes", "garbage", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/claim", nil)
			if tc.version != "" {
				req.Header.Set(middleware.HeaderClientVersion, tc.version)
			}
			rec := httptest.NewRecorder()
			middleware.ClientMetadata(RequireMinDaemonVersion(next)).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusUpgradeRequired {
				if got := rec.Header().Get("X-Min-Daemon-Version"); got != "1.0.0" {
					t.Fatalf("refusal must advertise the floor, got %q", got)
				}
				if body := rec.Body.String(); body == "" {
					t.Fatal("refusal must carry an actionable body")
				}
			}
		})
	}
}

func TestRequireMinDaemonVersionDisabled(t *testing.T) {
	t.Setenv("GOOSAR_MIN_DAEMON_VERSION", "none")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/claim", nil)
	req.Header.Set(middleware.HeaderClientVersion, "0.0.1")
	rec := httptest.NewRecorder()
	middleware.ClientMetadata(RequireMinDaemonVersion(next)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the gate disabled", rec.Code)
	}
}
