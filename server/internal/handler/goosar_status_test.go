package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/deploymentprofile"
)

func getGoosarStatus(t *testing.T) (GoosarStatusResponse, string) {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/status?workspace_id="+testWorkspaceID, nil)
	w := httptest.NewRecorder()
	testHandler.GetGoosarStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/status = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	var resp GoosarStatusResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode status: %v (body %s)", err, body)
	}
	return resp, body
}

func TestGetGoosarStatus_ReportsRealSignals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resp, _ := getGoosarStatus(t)

	if resp.Workspace.ID != testWorkspaceID {
		t.Errorf("workspace.id = %q, want %q", resp.Workspace.ID, testWorkspaceID)
	}
	if resp.GeneratedAt == "" {
		t.Error("generated_at is empty; a status snapshot without a timestamp cannot be aged")
	}

	if resp.Runtimes.State != statusOK {
		t.Errorf("runtimes.state = %q, want %q (fixture seeds an online runtime)", resp.Runtimes.State, statusOK)
	}
	if resp.Runtimes.Online < 1 || resp.Runtimes.Total < 1 {
		t.Errorf("runtimes online/total = %d/%d, want at least 1/1", resp.Runtimes.Online, resp.Runtimes.Total)
	}

	if resp.Caller.Actor != "member" {
		t.Errorf("caller.actor = %q, want member", resp.Caller.Actor)
	}
}

func TestGetGoosarStatus_UnknownIsNotGreen(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resp, _ := getGoosarStatus(t)

	if resp.Perimeter.Kerberos != statusUnknown {
		t.Errorf("perimeter.kerberos = %q, want %q (the server has no Kerberos signal)", resp.Perimeter.Kerberos, statusUnknown)
	}
	if resp.Perimeter.Note == "" {
		t.Error("perimeter.note is empty; an unknown must carry the reason it is unknown")
	}

	if resp.Provisioning.State != statusNotConfigured {
		t.Errorf("provisioning.state = %q, want %q", resp.Provisioning.State, statusNotConfigured)
	}

	if resp.MCP.State != statusNone {
		t.Errorf("mcp.state = %q, want %q", resp.MCP.State, statusNone)
	}
}

func TestGetGoosarStatus_DeclaredMcpServerIsNotVerified(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var serverID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, transport, config, created_by)
		VALUES ($1, 'status-fixture', 'stdio', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&serverID); err != nil {
		t.Fatalf("setup: create workspace mcp server: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_mcp_server WHERE id = $1`, serverID)
	})

	resp, _ := getGoosarStatus(t)

	if resp.MCP.State != statusUnknown {
		t.Errorf("mcp.state = %q, want %q (a declared server is not a working server)", resp.MCP.State, statusUnknown)
	}
	if resp.MCP.WorkspaceServers < 1 {
		t.Errorf("mcp.workspace_servers = %d, want at least 1", resp.MCP.WorkspaceServers)
	}
	if resp.MCP.ToolsVerified != statusUnknown {
		t.Errorf("mcp.tools_verified = %q, want %q", resp.MCP.ToolsVerified, statusUnknown)
	}
}

func TestGetGoosarStatus_NeverLeaksSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	const secret = "sk-status-should-never-print-this"
	sealed, err := testHandler.MCPSecretBox.Seal([]byte(secret))
	if err != nil {
		t.Fatalf("seal llm api key: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_config (workspace_id, llm_base_url, llm_model, llm_api_key)
		VALUES ($1, 'https://llm.example.com/v1', 'test-model', $2)
		ON CONFLICT (workspace_id) DO UPDATE
		SET llm_base_url = EXCLUDED.llm_base_url,
		    llm_model = EXCLUDED.llm_model,
		    llm_api_key = EXCLUDED.llm_api_key
	`, testWorkspaceID, sealed); err != nil {
		t.Fatalf("setup: workspace config: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID)
	})

	resp, body := getGoosarStatus(t)

	if strings.Contains(body, secret) {
		t.Fatalf("status response leaked the LLM API key: %s", body)
	}
	if resp.LLM.State != statusConfigured {
		t.Errorf("llm.state = %q, want %q", resp.LLM.State, statusConfigured)
	}
	if !resp.LLM.HasAPIKey {
		t.Error("llm.has_api_key = false, want true (a key is configured)")
	}
	if resp.LLM.BaseURL != "https://llm.example.com/v1" {
		t.Errorf("llm.base_url = %q, want the configured endpoint", resp.LLM.BaseURL)
	}

	if resp.LLM.State == statusOK {
		t.Error("llm.state must not be ok — no successful-call signal exists")
	}
}

func TestGetGoosarStatus_AgentCallerSeesNoWorkspaceMcpInventory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var serverID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, transport, config, created_by)
		VALUES ($1, 'status-agent-scope', 'stdio', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&serverID); err != nil {
		t.Fatalf("setup: create workspace mcp server: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_mcp_server WHERE id = $1`, serverID)
	})

	req := newRequest(http.MethodGet, "/api/status?workspace_id="+testWorkspaceID, nil)

	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", "00000000-0000-0000-0000-0000000000a9")
	w := httptest.NewRecorder()
	testHandler.GetGoosarStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/status as agent = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp GoosarStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode status: %v", err)
	}

	if resp.Caller.Actor != "agent" {
		t.Fatalf("caller.actor = %q, want agent", resp.Caller.Actor)
	}
	if resp.MCP.WorkspaceServers != 0 {
		t.Errorf("mcp.workspace_servers = %d for an agent caller, want 0 (the library inventory is human-only)", resp.MCP.WorkspaceServers)
	}
	if strings.Contains(resp.MCP.Note, "no MCP server is declared in this workspace") {
		t.Errorf("mcp.note claims the workspace declares nothing, which this caller cannot know: %q", resp.MCP.Note)
	}
}

func TestGetGoosarStatus_ReportsDeploymentProfile(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	origCfg := testHandler.cfg
	t.Cleanup(func() { testHandler.cfg = origCfg })

	testHandler.cfg.DeploymentProfile = deploymentprofile.Demo
	resp, _ := getGoosarStatus(t)
	if resp.Perimeter.DeploymentProfile != "demo" {
		t.Fatalf("perimeter.deployment_profile = %q, want demo", resp.Perimeter.DeploymentProfile)
	}

	testHandler.cfg.DeploymentProfile = ""
	resp, _ = getGoosarStatus(t)
	if resp.Perimeter.DeploymentProfile != statusUnknown {
		t.Fatalf("perimeter.deployment_profile = %q, want %q", resp.Perimeter.DeploymentProfile, statusUnknown)
	}
}
