package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func claimAgentMcpServersFor(t *testing.T, ctx context.Context, agentID, runtimeID, daemonID, originatorUserID string) map[string]json.RawMessage {
	t.Helper()

	if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID); err != nil {
		t.Fatalf("clear previous tasks: %v", err)
	}
	var originator any
	if originatorUserID != "" {
		originator = originatorUserID
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, 'queued', 0, $3, $3)
		RETURNING id
	`, agentID, runtimeID, originator).Scan(&taskID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			Agent *struct {
				McpConfig json.RawMessage `json:"mcp_config"`
			} `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil || resp.Task.Agent == nil {
		t.Fatalf("expected agent data on the claim; body=%s", w.Body.String())
	}
	if len(resp.Task.Agent.McpConfig) == 0 || string(resp.Task.Agent.McpConfig) == "null" {
		return nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(resp.Task.Agent.McpConfig, &doc); err != nil {
		t.Fatalf("decode claimed mcp_config: %v", err)
	}
	servers, err := unmarshalServerMap(doc["mcpServers"])
	if err != nil {
		t.Fatalf("decode claimed mcpServers: %v", err)
	}
	return servers
}

func sharedCredentialFixture(t *testing.T, ctx context.Context, serverName string) (agentID, runtimeID, daemonID, ownerID, serverID string) {
	t.Helper()
	agentID, runtimeID, daemonID = createRuntimeGuardAgent(t, ctx)
	ownerID = createMcpCredentialMember(t, serverName+"-owner")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, ownerID, agentID); err != nil {
		t.Fatalf("set agent owner: %v", err)
	}
	serverID = createMcpServerWithSchemaForTest(t, serverName, `{"command":"mcp-atlassian"}`,
		[]McpCredentialField{{Key: "JIRA_TOKEN", Required: true}})
	assignMcpServerToAgent(t, agentID, serverID)
	if w := setMcpCredentialsAs(t, ownerID, serverID, map[string]string{"JIRA_TOKEN": "owner-token"}); w.Code != http.StatusOK {
		t.Fatalf("seed owner credentials: %d %s", w.Code, w.Body.String())
	}
	return agentID, runtimeID, daemonID, ownerID, serverID
}

func mcpEntryEnvValue(t *testing.T, entry json.RawMessage, key string) string {
	t.Helper()
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(entry, &doc); err != nil {
		t.Fatalf("decode entry %s: %v", entry, err)
	}
	return doc.Env[key]
}

func TestClaim_SharedMcpServerUsesTheRequestersCredentials(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	agentID, runtimeID, daemonID, _, serverID := sharedCredentialFixture(t, ctx, "requester-jira")

	requester := createMcpCredentialMember(t, "mcp-requester-with-token")
	if w := setMcpCredentialsAs(t, requester, serverID, map[string]string{"JIRA_TOKEN": "requester-token"}); w.Code != http.StatusOK {
		t.Fatalf("seed requester credentials: %d %s", w.Code, w.Body.String())
	}

	servers := claimAgentMcpServersFor(t, ctx, agentID, runtimeID, daemonID, requester)
	entry, ok := servers["requester-jira"]
	if !ok {
		t.Fatalf("the shared server must reach a requester who supplied credentials, got %v", keysOf(servers))
	}
	if got := mcpEntryEnvValue(t, entry, "JIRA_TOKEN"); got != "requester-token" {
		t.Fatalf("the run must carry the requester's credentials, got %q", got)
	}
}

func TestClaim_SharedMcpServerRefusedWhenRequesterHasNoCredentials(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	agentID, runtimeID, daemonID, _, _ := sharedCredentialFixture(t, ctx, "refused-jira")

	stranger := createMcpCredentialMember(t, "mcp-requester-no-token")
	servers := claimAgentMcpServersFor(t, ctx, agentID, runtimeID, daemonID, stranger)
	if entry, ok := servers["refused-jira"]; ok {
		t.Fatalf("a requester without credentials must not get the server (least of all the owner's): %s", entry)
	}
}

func TestClaim_SharedMcpServerFallsBackToTheOwnerWhenNoHumanAuthorized(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	agentID, runtimeID, daemonID, _, _ := sharedCredentialFixture(t, ctx, "autopilot-jira")

	servers := claimAgentMcpServersFor(t, ctx, agentID, runtimeID, daemonID, "")
	entry, ok := servers["autopilot-jira"]
	if !ok {
		t.Fatalf("an unauthorized-by-human run must still use the owner's own credentials, got %v", keysOf(servers))
	}
	if got := mcpEntryEnvValue(t, entry, "JIRA_TOKEN"); got != "owner-token" {
		t.Fatalf("owner fallback carried %q", got)
	}
}
