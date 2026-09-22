package daemon

import (
	"errors"
	"net/http"
	"testing"
)

func TestDaemonTooOldHint(t *testing.T) {
	body := `{"error":"daemon version 0.1.0 is older than the minimum 1.0.0 this server accepts; upgrade with ` +
		"`goosar update`" + `","code":"daemon_too_old","min_daemon_version":"1.0.0","daemon_version":"0.1.0"}`
	err := error(&requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/tasks/claim",
		StatusCode: http.StatusUpgradeRequired,
		Body:       body,
	})

	minimum, ok := daemonTooOldHint(err)
	if !ok {
		t.Fatal("a 426 from the claim endpoint must be recognised as the version gate")
	}
	if minimum != "1.0.0" {
		t.Fatalf("minimum = %q, want 1.0.0", minimum)
	}

	for _, other := range []error{
		errors.New("connection refused"),
		&requestError{StatusCode: http.StatusNotFound, Body: "not found"},
		&requestError{StatusCode: http.StatusInternalServerError, Body: "boom"},
	} {
		if _, ok := daemonTooOldHint(other); ok {
			t.Fatalf("%v must not be treated as the version gate", other)
		}
	}
}

func TestDaemonTooOldHintWithoutParsableBody(t *testing.T) {
	err := error(&requestError{StatusCode: http.StatusUpgradeRequired, Body: "<html>Upgrade Required</html>"})
	minimum, ok := daemonTooOldHint(err)
	if !ok {
		t.Fatal("426 must be recognised regardless of body shape")
	}
	if minimum != "" {
		t.Fatalf("unknown minimum must be empty, got %q", minimum)
	}
}
