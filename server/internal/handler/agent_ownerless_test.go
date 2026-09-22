package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const testUUIDForOwnershipTest = "22222222-2222-4222-8222-222222222222"

const ownerlessMcpConfig = `{"mcpServers":{` +
	`"jira":{"env":{"JIRA_PERSONAL_TOKEN":"departed-member-token"}},` +
	`"confluence":{"env":{"CONFLUENCE_PERSONAL_TOKEN":"departed-confluence-token"}}}}`

func ownerlessAgentFixture(t *testing.T, label string, env map[string]string) string {
	t.Helper()
	ctx := context.Background()

	envBytes := []byte(`{}`)
	if env != nil {
		var err error
		envBytes, err = json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal custom_env: %v", err)
		}
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, NULL,
		        '', $4::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, "ownerless-agent-"+label, handlerTestRuntimeID(t),
		string(envBytes), []byte(ownerlessMcpConfig)).Scan(&agentID); err != nil {
		t.Fatalf("create ownerless agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("seed workspace invocation target: %v", err)
	}

	return agentID
}

func TestIsAgentOwner_OwnerlessAgentNeverMatches(t *testing.T) {
	ownerless := db.Agent{}
	owned := db.Agent{OwnerID: parseUUID(testUUIDForOwnershipTest)}

	if isAgentOwner(ownerless, "") {
		t.Error("an anonymous caller must not be treated as the owner of an ownerless agent")
	}
	if isAgentOwner(ownerless, testUUIDForOwnershipTest) {
		t.Error("an ownerless agent has no owner; no user may match it")
	}
	if isAgentOwner(owned, "") {
		t.Error("an anonymous caller must not match an owned agent")
	}
	if isAgentOwner(owned, "11111111-1111-4111-8111-111111111111") {
		t.Error("a different user must not match an owned agent")
	}
	if !isAgentOwner(owned, testUUIDForOwnershipTest) {
		t.Error("the real owner must still match; the guard is not a blanket deny")
	}

	if canViewAgentSecrets(ownerless, "") != isAgentOwner(ownerless, "") {
		t.Error("canViewAgentSecrets must stay bound to isAgentOwner")
	}
}

func TestGetAgent_OwnerlessAgentSecretsStayHiddenFromAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := ownerlessAgentFixture(t, "read", map[string]string{"JIRA_TOKEN": "departed-env-token"})

	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "departed-member-token") {
		t.Fatalf("an ownerless agent leaked its departed owner's MCP secret: %s", w.Body.String())
	}

	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.McpConfigRedacted {
		t.Error("an ownerless agent must carry mcp_config_redacted=true")
	}

	names := maskedServerNames(t, decodeMcpDocument(t, resp.McpConfig))
	if len(names) != 2 {
		t.Errorf("expected both server names to survive masking, got %v", names)
	}

	envReq := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)
	envW := httptest.NewRecorder()
	testHandler.GetAgentEnv(envW, envReq)
	if envW.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv: expected 200, got %d: %s", envW.Code, envW.Body.String())
	}
	if strings.Contains(envW.Body.String(), "departed-env-token") {
		t.Fatalf("an ownerless agent leaked its departed owner's env value: %s", envW.Body.String())
	}
	var envResp AgentEnvResponse
	if err := json.Unmarshal(envW.Body.Bytes(), &envResp); err != nil {
		t.Fatalf("decode env: %v", err)
	}
	if !envResp.ValuesMasked {
		t.Error("an ownerless agent's env must come back flagged as masked")
	}
	if !reflect.DeepEqual(envResp.CustomEnv, map[string]string{"JIRA_TOKEN": envSentinel}) {
		t.Errorf("key names must survive with masked values, got %v", envResp.CustomEnv)
	}
}

func TestOwnerlessAgent_AdminStillManagesEveryField(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := ownerlessAgentFixture(t, "manage", map[string]string{
		"KEEP_ME":   "departed-untouched",
		"ROTATE_ME": "departed-leaked",
		"DROP_ME":   "departed-doomed",
	})

	envBody := map[string]any{"custom_env": map[string]string{
		"KEEP_ME":   envSentinel,
		"ROTATE_ME": envSentinel,
	}}
	envReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", envBody), "id", agentID)
	envW := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(envW, envReq)
	if envW.Code != http.StatusOK {
		t.Fatalf("UpdateAgentEnv on an ownerless agent: expected 200, got %d: %s", envW.Code, envW.Body.String())
	}
	if strings.Contains(envW.Body.String(), "departed-untouched") {
		t.Fatalf("the PUT response handed back a preserved secret: %s", envW.Body.String())
	}

	var storedEnv string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&storedEnv); err != nil {
		t.Fatalf("read back custom_env: %v", err)
	}
	var gotEnv map[string]string
	if err := json.Unmarshal([]byte(storedEnv), &gotEnv); err != nil {
		t.Fatalf("decode stored custom_env: %v", err)
	}
	wantEnv := map[string]string{
		"KEEP_ME":   "departed-untouched",
		"ROTATE_ME": "departed-leaked",
	}
	if !reflect.DeepEqual(gotEnv, wantEnv) {
		t.Errorf("stored custom_env mismatch:\n got:  %v\n want: %v", gotEnv, wantEnv)
	}

	injectBody := map[string]any{"custom_env": map[string]string{
		"KEEP_ME":   envSentinel,
		"ROTATE_ME": "admin-rotated",
	}}
	injectReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", injectBody), "id", agentID)
	injectW := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(injectW, injectReq)
	if injectW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an env value write on an ownerless agent, got %d: %s", injectW.Code, injectW.Body.String())
	}

	mcpBody := map[string]any{"mcp_config": map[string]any{
		"mcpServers": map[string]any{
			"confluence": map[string]any{mcpMaskedMarkerKey: true},
		},
	}}
	mcpReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, mcpBody), "id", agentID)
	mcpW := httptest.NewRecorder()
	testHandler.UpdateAgent(mcpW, mcpReq)
	if mcpW.Code != http.StatusOK {
		t.Fatalf("UpdateAgent on an ownerless agent: expected 200, got %d: %s", mcpW.Code, mcpW.Body.String())
	}
	if strings.Contains(mcpW.Body.String(), "departed-confluence-token") {
		t.Fatalf("the update response handed back the preserved secret: %s", mcpW.Body.String())
	}

	var storedMcp string
	if err := testPool.QueryRow(ctx, `SELECT mcp_config::text FROM agent WHERE id = $1`, agentID).Scan(&storedMcp); err != nil {
		t.Fatalf("read back mcp_config: %v", err)
	}
	if strings.Contains(storedMcp, "departed-member-token") {
		t.Error("the admin's revocation did not remove the leaked jira server")
	}
	if !strings.Contains(storedMcp, "departed-confluence-token") {
		t.Errorf("the placeholder failed to preserve the untouched server: %s", storedMcp)
	}
	if strings.Contains(storedMcp, mcpMaskedMarkerKey) {
		t.Errorf("a masked placeholder was persisted as configuration: %s", storedMcp)
	}
}

func TestOwnerlessAgent_ArchiveResponseStaysMasked(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := ownerlessAgentFixture(t, "archive", nil)

	req := withURLParam(newRequest(http.MethodPost, "/api/agents/"+agentID+"/archive", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ArchiveAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "departed-member-token") {
		t.Fatalf("the archive response leaked the departed owner's secret: %s", w.Body.String())
	}

	restoreReq := withURLParam(newRequest(http.MethodPost, "/api/agents/"+agentID+"/restore", nil), "id", agentID)
	restoreW := httptest.NewRecorder()
	testHandler.RestoreAgent(restoreW, restoreReq)
	if restoreW.Code != http.StatusOK {
		t.Fatalf("RestoreAgent: expected 200, got %d: %s", restoreW.Code, restoreW.Body.String())
	}
	if strings.Contains(restoreW.Body.String(), "departed-member-token") {
		t.Fatalf("the restore response leaked the departed owner's secret: %s", restoreW.Body.String())
	}
}
