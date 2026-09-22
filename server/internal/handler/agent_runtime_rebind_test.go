package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func seedRuntimeOwnedBy(t *testing.T, name, ownerUserID string) string {
	t.Helper()
	ctx := context.Background()
	var owner any
	if ownerUserID != "" {
		owner = ownerUserID
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'handler_test_runtime', 'online',
		        'rebind test runtime', '{}'::jsonb, $3, 'public', now())
		RETURNING id
	`, testWorkspaceID, name, owner).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime %q: %v", name, err)
	}
	t.Cleanup(func() {

		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func moveAgentToRuntime(t *testing.T, actorUserID, agentID, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"runtime_id": runtimeID}
	req := newRequest(http.MethodPut, "/api/agents/"+agentID, body)
	if actorUserID != "" {
		req = newRequestAs(actorUserID, http.MethodPut, "/api/agents/"+agentID, body)
	}
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(req, "id", agentID))
	return w
}

func rebindFixture(t *testing.T, label string) (agentID, ownerUserID, memberRuntimeID, adminRuntimeID string) {
	t.Helper()
	agentID, ownerUserID = secretVisibilityFixture(t, label, nil, map[string]string{
		"JIRA_PERSONAL_TOKEN": "member-token",
	})
	memberRuntimeID = seedRuntimeOwnedBy(t, "Member Runtime "+label, ownerUserID)
	adminRuntimeID = seedRuntimeOwnedBy(t, "Admin Runtime "+label, testUserID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET runtime_id = $1 WHERE id = $2`, memberRuntimeID, agentID,
	); err != nil {
		t.Fatalf("place agent on the member runtime: %v", err)
	}
	return agentID, ownerUserID, memberRuntimeID, adminRuntimeID
}

func TestUpdateAgent_AdminCannotMoveForeignAgentOntoOwnRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, memberRuntimeID, adminRuntimeID := rebindFixture(t, "rebindblock")

	w := moveAgentToRuntime(t, "", agentID, adminRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when an admin moves a foreign agent onto their own runtime, got %d: %s", w.Code, w.Body.String())
	}

	var current string
	if err := testPool.QueryRow(context.Background(),
		`SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&current); err != nil {
		t.Fatalf("read back runtime_id: %v", err)
	}
	if current != memberRuntimeID {
		t.Errorf("agent moved anyway: runtime_id = %s, want %s", current, memberRuntimeID)
	}
}

func TestUpdateAgent_AgentOwnerMovesTheirOwnAgentFreely(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID, _, adminRuntimeID := rebindFixture(t, "rebindowner")

	w := moveAgentToRuntime(t, ownerUserID, agentID, adminRuntimeID)
	if w.Code != http.StatusOK {
		t.Fatalf("the agent owner must be able to choose any runtime they may use, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_AdminCannotReassignToAnUnownedRuntimeEither(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, memberRuntimeID, _ := rebindFixture(t, "rebindhosted")
	hostedRuntimeID := seedRuntimeOwnedBy(t, "Hosted Runtime rebindhosted", "")

	w := moveAgentToRuntime(t, "", agentID, hostedRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-owner rebind onto a hosted runtime, got %d: %s", w.Code, w.Body.String())
	}
	var current string
	if err := testPool.QueryRow(context.Background(),
		`SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&current); err != nil {
		t.Fatalf("read back runtime_id: %v", err)
	}
	if current != memberRuntimeID {
		t.Errorf("agent moved anyway: runtime_id = %s, want %s", current, memberRuntimeID)
	}
}

func TestUpdateAgent_AdminCannotMoveSecretFreeForeignAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "rebindnosecrets", nil, nil)
	memberRuntimeID := seedRuntimeOwnedBy(t, "Member Runtime rebindnosecrets", ownerUserID)
	adminRuntimeID := seedRuntimeOwnedBy(t, "Admin Runtime rebindnosecrets", testUserID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET runtime_id = $1 WHERE id = $2`, memberRuntimeID, agentID,
	); err != nil {
		t.Fatalf("place agent on the member runtime: %v", err)
	}

	w := moveAgentToRuntime(t, "", agentID, adminRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-owner rebind of a secret-free agent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_AdminNoOpRuntimeResubmitTolerated(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, memberRuntimeID, _ := rebindFixture(t, "rebindnoop")

	body := map[string]any{"runtime_id": memberRuntimeID, "description": "admin edit"}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("a no-op runtime_id resubmit must not block an admin edit, got %d: %s", w.Code, w.Body.String())
	}
}
