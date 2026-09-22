package daemon

import (
	"errors"
	"log/slog"
	"net/http"
	"testing"
)

func TestIsWorkspaceAccessDeniedError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"membership denial (heartbeat/runtime access)", &requestError{StatusCode: http.StatusNotFound, Body: `{"error":"not found"}`}, true},
		{"membership denial (register path)", &requestError{StatusCode: http.StatusNotFound, Body: `{"error":"workspace not found"}`}, true},
		{"forbidden with JSON error shape", &requestError{StatusCode: http.StatusForbidden, Body: `{"error":"insufficient permissions"}`}, true},
		{"runtime gone has its own recovery", &requestError{StatusCode: http.StatusNotFound, Body: `{"error":"runtime not found"}`}, false},
		{"task gone is not a membership signal", &requestError{StatusCode: http.StatusNotFound, Body: `{"error":"task not found"}`}, false},
		{"chi/proxy plain-text 404 is not a denial", &requestError{StatusCode: http.StatusNotFound, Body: "404 page not found"}, false},
		{"proxy HTML 403 is not a denial", &requestError{StatusCode: http.StatusForbidden, Body: "<html>blocked</html>"}, false},
		{"server error is transient", &requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"boom"}`}, false},
		{"unauthorized means re-login, not disarm", &requestError{StatusCode: http.StatusUnauthorized, Body: `{"error":"user not authenticated"}`}, false},
		{"plain network error", errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		if got := isWorkspaceAccessDeniedError(tc.err); got != tc.want {
			t.Errorf("%s: isWorkspaceAccessDeniedError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAccessDeniedStrikes_CountAloneGatesDisarm(t *testing.T) {
	d := &Daemon{logger: slog.Default(), accessDeniedMinWindow: 0}

	if d.recordWorkspaceAccessDenied("rt-count-gate") {
		t.Fatalf("strike 1 must never confirm")
	}
	if d.recordWorkspaceAccessDenied("rt-count-gate") {
		t.Fatalf("strike 2 with the window already satisfied must not confirm — the count gate is the only thing standing")
	}
	if !d.recordWorkspaceAccessDenied("rt-count-gate") {
		t.Fatalf("strike 3 with the window satisfied must confirm")
	}
}
