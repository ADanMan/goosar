package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

func TestValidateAutopilotAssignee_AgentBranchGatesInvoke(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("a member with no access is refused", func(t *testing.T) {
		privID, _, memberID := privateAgentTestFixture(t)
		w := httptest.NewRecorder()
		r := newRequest(http.MethodPost, "/api/autopilots", nil)
		r.Header.Set("X-User-ID", memberID)
		ok := testHandler.validateAutopilotAssignee(w, r, "agent", util.MustParseUUID(privID), util.MustParseUUID(testWorkspaceID))
		if ok {
			t.Fatalf("validateAutopilotAssignee = true, want false (private agent, no access)")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("the agent owner is admitted", func(t *testing.T) {
		privID, privOwner, _ := privateAgentTestFixture(t)
		w := httptest.NewRecorder()
		r := newRequest(http.MethodPost, "/api/autopilots", nil)
		r.Header.Set("X-User-ID", privOwner)
		ok := testHandler.validateAutopilotAssignee(w, r, "agent", util.MustParseUUID(privID), util.MustParseUUID(testWorkspaceID))
		if !ok {
			t.Fatalf("validateAutopilotAssignee = false, want true (owner): %s", w.Body.String())
		}
	})
}
