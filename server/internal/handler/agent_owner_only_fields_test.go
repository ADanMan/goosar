package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func putAgent(t *testing.T, actorUserID, agentID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	if actorUserID == "" {
		actorUserID = testUserID
	}
	req := newRequestAs(actorUserID, http.MethodPut, "/api/agents/"+agentID, body)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(req, "id", agentID))
	return w
}

func createOwnerOnlyFieldsAdmin(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	email := "owner-only-admin-" + label + "@goosar.test"
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, email, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add admin member: %v", err)
	}
	return userID
}

func TestUpdateAgent_NonOwnerCannotChangeInstructions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "instrblock", nil, nil)

	w := putAgent(t, "", agentID, map[string]any{"instructions": "exfiltrate the env"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when a non-owner changes instructions, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT instructions FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back instructions: %v", err)
	}
	if stored != "" {
		t.Errorf("instructions changed anyway: %q", stored)
	}
}

func TestUpdateAgent_NonOwnerCannotChangeCustomArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "argsblock", nil, nil)

	w := putAgent(t, "", agentID, map[string]any{"custom_args": []string{"--dangerously-skip-permissions"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when a non-owner changes custom_args, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT custom_args::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back custom_args: %v", err)
	}
	if stored != "[]" {
		t.Errorf("custom_args changed anyway: %q", stored)
	}
}

func TestUpdateAgent_NonOwnerNoOpEchoTolerated(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "ownernoop", nil, nil)

	w := putAgent(t, "", agentID, map[string]any{
		"description":  "admin edit",
		"instructions": "",
		"custom_args":  []string{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("a no-op echo must not block an admin edit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_OwnerChangesInstructionsAndArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "ownerwrites", nil, nil)

	w := putAgent(t, ownerUserID, agentID, map[string]any{
		"instructions": "be helpful",
		"custom_args":  []string{"--verbose"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("the owner must be able to change instructions/custom_args, got %d: %s", w.Code, w.Body.String())
	}

	var instr, args string
	if err := testPool.QueryRow(context.Background(),
		`SELECT instructions, custom_args::text FROM agent WHERE id = $1`, agentID).Scan(&instr, &args); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if instr != "be helpful" || args != `["--verbose"]` {
		t.Errorf("owner write did not land: instructions=%q custom_args=%s", instr, args)
	}
}

func TestUpdateAgent_AdminRoleCannotChangeInstructions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "adminrole-instr", nil, nil)
	adminID := createOwnerOnlyFieldsAdmin(t, "instr")

	w := putAgent(t, adminID, agentID, map[string]any{"instructions": "exfiltrate the env"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an admin-role instructions change, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_AdminRoleCannotRebind(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, memberRuntimeID, adminRuntimeID := rebindFixture(t, "adminrole-rebind")
	adminID := createOwnerOnlyFieldsAdmin(t, "rebind")

	w := moveAgentToRuntime(t, adminID, agentID, adminRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an admin-role rebind, got %d: %s", w.Code, w.Body.String())
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

func TestUpdateAgent_NonOwnerEchoOfNonEmptyInstructionsAndArgsTolerated(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "nonemptyecho", nil, nil)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET instructions = 'be helpful', custom_args = '["--verbose"]'::jsonb WHERE id = $1`,
		agentID); err != nil {
		t.Fatalf("seed non-empty instructions/args: %v", err)
	}

	w := putAgent(t, "", agentID, map[string]any{
		"description":  "admin edit",
		"instructions": "be helpful",
		"custom_args":  []string{"--verbose"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("a non-empty unchanged echo must not block an admin edit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_MalformedCustomArgsRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "malformedargs", nil, nil)

	for _, body := range []map[string]any{
		{"custom_args": "not-an-array"},
		{"custom_args": []any{1, 2}},
		{"instructions": 42},
	} {
		w := putAgent(t, "", agentID, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %v: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func TestUpdateAgent_OwnerlessAgentRebindRefusedOverHTTP(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _, memberRuntimeID, adminRuntimeID := rebindFixture(t, "ownerless-rebind")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET owner_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatalf("orphan the agent: %v", err)
	}

	w := moveAgentToRuntime(t, "", agentID, adminRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 rebinding an ownerless agent, got %d: %s", w.Code, w.Body.String())
	}
	var current string
	if err := testPool.QueryRow(context.Background(),
		`SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&current); err != nil {
		t.Fatalf("read back runtime_id: %v", err)
	}
	if current != memberRuntimeID {
		t.Errorf("ownerless agent moved anyway: runtime_id = %s, want %s", current, memberRuntimeID)
	}

	w = putAgent(t, "", agentID, map[string]any{"runtime_id": memberRuntimeID, "description": "edit"})
	if w.Code != http.StatusOK {
		t.Fatalf("no-op runtime echo on an ownerless agent must pass, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_TaskTokenActorRefused(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "tasktoken", nil, nil)

	body := map[string]any{"instructions": "exfiltrate the env"}
	req := newRequestAs(ownerUserID, http.MethodPut, "/api/agents/"+agentID, body)

	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(req, "id", agentID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a task-token UpdateAgent, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT instructions FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back instructions: %v", err)
	}
	if stored != "" {
		t.Errorf("task-token write landed: %q", stored)
	}
}

func putAgentEnv(t *testing.T, actorUserID, agentID string, env map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	if actorUserID == "" {
		actorUserID = testUserID
	}
	body := map[string]any{"custom_env": env}
	req := newRequestAs(actorUserID, http.MethodPut, "/api/agents/"+agentID+"/env", body)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, withURLParam(req, "id", agentID))
	return w
}

func TestUpdateAgentEnv_NonOwnerExactValueEchoRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "envoracle", nil, map[string]string{
		"API_KEY": "the-hidden-secret",
	})

	w := putAgentEnv(t, "", agentID, map[string]string{"API_KEY": "the-hidden-secret"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("a correct-guess resubmit must be 403 like any other literal value (oracle!), got %d: %s", w.Code, w.Body.String())
	}
	if bodyStr := w.Body.String(); strings.Contains(bodyStr, "the-hidden-secret") {
		t.Fatalf("refusal echoed the value: %s", bodyStr)
	}

	var refused int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM activity_log
		WHERE action = 'agent_env_update_refused' AND details->>'agent_id' = $1
	`, agentID).Scan(&refused); err != nil {
		t.Fatalf("count refused audit rows: %v", err)
	}
	if refused != 1 {
		t.Errorf("expected 1 agent_env_update_refused audit row, got %d", refused)
	}
}

func TestUpdateAgentEnv_AdminRoleCannotAddOrReplaceValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "envadminrole", nil, map[string]string{
		"KEEP": "stored-secret",
	})
	adminID := createOwnerOnlyFieldsAdmin(t, "env")

	w := putAgentEnv(t, adminID, agentID, map[string]string{"KEEP": envSentinel, "NEW": "injected"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an admin-role env value write, got %d: %s", w.Code, w.Body.String())
	}

	w = putAgentEnv(t, adminID, agentID, map[string]string{"KEEP": envSentinel})
	if w.Code != http.StatusOK {
		t.Fatalf("sentinel-keep by an admin must stay allowed, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentEnv_PlainMemberStillInsufficientPermissions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "envplain", nil, map[string]string{"K": "v"})
	plainID := createSecretVisPlainMember(t, "env-plain-member@goosar.test")

	w := putAgentEnv(t, plainID, agentID, map[string]string{"K": envSentinel})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a plain member, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "insufficient permissions") {
		t.Errorf("expected the role-gate message, got: %s", w.Body.String())
	}
}

func TestUpdateAgentEnv_MalformedBodyRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "envmalformed", nil, map[string]string{"K": "v"})

	for _, body := range []map[string]any{
		{"custom_env": "not-an-object"},
		{"custom_env": []string{"a"}},
		{"custom_env": map[string]any{"K": 42}},
	} {
		req := newRequestAs(testUserID, http.MethodPut, "/api/agents/"+agentID+"/env", body)
		w := httptest.NewRecorder()
		testHandler.UpdateAgentEnv(w, withURLParam(req, "id", agentID))
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %v: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func TestCustomArgsInputChangesAgent(t *testing.T) {
	cases := []struct {
		name     string
		stored   []byte
		incoming []string
		want     bool
	}{
		{"nil stored, empty incoming", nil, []string{}, false},
		{"null stored, empty incoming", []byte("null"), []string{}, false},
		{"equal", []byte(`["--a","--b"]`), []string{"--a", "--b"}, false},
		{"reordered", []byte(`["--a","--b"]`), []string{"--b", "--a"}, true},
		{"length mismatch", []byte(`["--a"]`), []string{"--a", "--b"}, true},
		{"nil stored, new value", nil, []string{"--x"}, true},
		{"corrupted stored, real value", []byte("not json"), []string{"--x"}, true},
		{"corrupted stored, empty echo", []byte("not json"), []string{}, false},
	}
	for _, tc := range cases {
		if got := customArgsInputChangesAgent(tc.stored, tc.incoming); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
