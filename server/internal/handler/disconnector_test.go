package handler

import (
	"testing"

	"github.com/adanman/goosar/server/internal/realtime"
)

type recordingDisconnector struct {
	calls []struct{ user, workspace string }
}

func (d *recordingDisconnector) DisconnectUser(userID, workspaceID string) int {
	d.calls = append(d.calls, struct{ user, workspace string }{userID, workspaceID})
	return 1
}

func TestUserDisconnectorPrefersInjectedDisconnector(t *testing.T) {
	rec := &recordingDisconnector{}
	h := &Handler{Hub: realtime.NewHub(), Disconnector: rec}

	if closed := h.disconnectUser("user-1", "workspace-1"); closed != 1 {
		t.Fatalf("expected the injected disconnector's result, got %d", closed)
	}
	if len(rec.calls) != 1 || rec.calls[0].user != "user-1" || rec.calls[0].workspace != "workspace-1" {
		t.Fatalf("unexpected calls: %+v", rec.calls)
	}
}

func TestUserDisconnectorFallsBackToHub(t *testing.T) {
	h := &Handler{Hub: realtime.NewHub()}
	if closed := h.disconnectUser("user-1", ""); closed != 0 {
		t.Fatalf("expected 0 closed connections on an empty hub, got %d", closed)
	}
}

func TestUserDisconnectorIsSafeWithoutHub(t *testing.T) {
	h := &Handler{}
	if closed := h.disconnectUser("user-1", ""); closed != 0 {
		t.Fatalf("expected a no-op without a hub, got %d", closed)
	}
}
