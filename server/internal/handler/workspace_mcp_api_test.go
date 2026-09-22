package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func createWorkspaceMcpServerForTest(t *testing.T, name, config string) string {
	id, _ := createWorkspaceMcpServerReturningBody(t, name, config)
	return id
}

func createWorkspaceMcpServerReturningBody(t *testing.T, name, config string) (string, string) {
	t.Helper()
	withTestMcpBox(t, newTestMcpBox(t))

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/workspace-mcp-servers", map[string]any{
		"name":   name,
		"config": json.RawMessage(config),
	})
	testHandler.CreateWorkspaceMcpServer(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspaceMcpServer: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM agent_mcp_server WHERE server_id = $1`, resp.ID)
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE id = $1`, resp.ID)
	})
	return resp.ID, w.Body.String()
}

func assignMcpServerToAgent(t *testing.T, agentID, serverID string) []WorkspaceMcpServerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers", map[string]any{"server_id": serverID})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AddAgentMcpServer: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode assignment response: %v", err)
	}
	return resp
}

func enabledAssignmentNames(t *testing.T, agentID string) []string {
	t.Helper()

	rows, err := testHandler.Queries.ListEnabledAgentMcpServers(context.Background(), db.ListEnabledAgentMcpServersParams{
		AgentID:     parseUUID(agentID),
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("ListEnabledAgentMcpServers: %v", err)
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

func TestWorkspaceMcpServer_CreatedButUnassignedReachesNoAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "mcp-unassigned-agent", nil)
	createWorkspaceMcpServerForTest(t, "unassigned", `{"url":"https://mcp.example"}`)

	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("a library entry nobody assigned must reach no agent; the agent got %v", names)
	}
}

func TestWorkspaceMcpServer_DisabledAssignmentIsNotDelivered(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "mcp-toggle-agent", nil)
	serverID := createWorkspaceMcpServerForTest(t, "toggled", `{"url":"https://mcp.example"}`)
	assignMcpServerToAgent(t, agentID, serverID)

	if names := enabledAssignmentNames(t, agentID); len(names) != 1 || names[0] != "toggled" {
		t.Fatalf("an enabled assignment must be delivered, got %v", names)
	}

	setAssignmentEnabled(t, agentID, serverID, false)
	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("a disabled assignment must not be delivered, got %v", names)
	}

	listed := listAgentMcpServers(t, agentID)
	if len(listed) != 1 || listed[0].Enabled == nil || *listed[0].Enabled {
		t.Fatalf("the assignment must survive being switched off, got %+v", listed)
	}

	setAssignmentEnabled(t, agentID, serverID, true)
	if names := enabledAssignmentNames(t, agentID); len(names) != 1 {
		t.Fatalf("re-enabling must restore delivery without recreating anything, got %v", names)
	}
}

func setAssignmentEnabled(t *testing.T, agentID, serverID string, enabled bool) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/agents/"+agentID+"/mcp-servers/"+serverID+"/enabled",
		map[string]any{"enabled": enabled})
	req = withTwoURLParams(req, "id", agentID, "serverId", serverID)
	testHandler.SetAgentMcpServerEnabled(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SetAgentMcpServerEnabled(%v): expected 200, got %d: %s", enabled, w.Code, w.Body.String())
	}
}

func listAgentMcpServers(t *testing.T, agentID string) []WorkspaceMcpServerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/mcp-servers", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentMcpServers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentMcpServers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode agent mcp servers: %v", err)
	}
	return resp
}

func TestWorkspaceMcpServer_NoReadPathReturnsTheStoredEntry(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	const secret = "super-secret-bearer-token"
	agentID := createHandlerTestAgent(t, "mcp-writeonly-agent", nil)
	serverID, createBody := createWorkspaceMcpServerReturningBody(t, "writeonly",
		`{"url":"https://mcp.example","headers":{"Authorization":"Bearer `+secret+`"}}`)
	assignMcpServerToAgent(t, agentID, serverID)

	bodies := map[string]string{"create response": createBody}

	w := httptest.NewRecorder()
	testHandler.ListWorkspaceMcpServers(w, newRequest(http.MethodGet, "/api/workspace-mcp-servers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceMcpServers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	bodies["workspace library listing"] = w.Body.String()

	w = httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/mcp-servers", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentMcpServers(w, req)
	bodies["agent assignment listing"] = w.Body.String()

	bodies["assignment mutation response"] = mutationBody(t, agentID, serverID)

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/agents/"+agentID, nil)
	req = withURLParam(req, "id", agentID)
	testHandler.GetAgent(w, req)
	bodies["agent detail"] = w.Body.String()

	for name, body := range bodies {
		if strings.Contains(body, secret) {
			t.Fatalf("%s returned the stored entry: %s", name, body)
		}
		if strings.Contains(body, "mcp.example") {
			t.Fatalf("%s returned the stored entry's url: %s", name, body)
		}
	}

	listed := listAgentMcpServers(t, agentID)
	if len(listed) != 1 || listed[0].Transport != "http" {
		t.Fatalf("expected the non-secret transport summary, got %+v", listed)
	}
}

func mutationBody(t *testing.T, agentID, serverID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/agents/"+agentID+"/mcp-servers/"+serverID+"/enabled",
		map[string]any{"enabled": true})
	req = withTwoURLParams(req, "id", agentID, "serverId", serverID)
	testHandler.SetAgentMcpServerEnabled(w, req)
	return w.Body.String()
}

func TestWorkspaceMcpServer_ConfigIsSealedAtRest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	const secret = "sealed-token-value"
	serverID := createWorkspaceMcpServerForTest(t, "sealed",
		`{"url":"https://mcp.example","headers":{"Authorization":"Bearer `+secret+`"}}`)

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT config::text FROM workspace_mcp_server WHERE id = $1`, serverID).Scan(&stored); err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if strings.Contains(stored, secret) || strings.Contains(stored, "mcp.example") {
		t.Fatalf("workspace_mcp_server.config was stored in plaintext: %s", stored)
	}
	if !strings.Contains(stored, "__goosar_sealed__") {
		t.Fatalf("expected the sealed envelope, got %s", stored)
	}
}

func TestWorkspaceMcpServer_CreateWithoutSecretKeyIsRefused(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, nil)

	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceMcpServer(w, newRequest(http.MethodPost, "/api/workspace-mcp-servers", map[string]any{
		"name":   "keyless",
		"config": map[string]any{"url": "https://mcp.example"},
	}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without GOOSAR_MCP_SECRET_KEY, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1 AND name = 'keyless'`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count keyless rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("a refused create must store nothing, found %d row(s)", count)
	}
}

func TestWorkspaceMcpServer_DeleteSweepsAssignments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "mcp-delete-agent", nil)
	serverID := createWorkspaceMcpServerForTest(t, "doomed", `{"url":"https://mcp.example"}`)
	assignMcpServerToAgent(t, agentID, serverID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/workspace-mcp-servers/"+serverID, nil)
	req = withURLParam(req, "serverId", serverID)
	testHandler.DeleteWorkspaceMcpServer(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspaceMcpServer: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var orphans int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, serverID).Scan(&orphans); err != nil {
		t.Fatalf("count orphaned assignments: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("deleting a library entry left %d orphaned assignment(s)", orphans)
	}
}

func TestAgentMcpServer_AddRejectsAServerFromAnotherWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "mcp-tenant-agent", nil)

	var otherWorkspaceID, foreignServerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Other MCP WS', 'ws-mcp-tenant', '', 'OMW')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, config, transport)
		VALUES ($1, 'foreign', '{}'::jsonb, 'http')
		RETURNING id
	`, otherWorkspaceID).Scan(&foreignServerID); err != nil {
		t.Fatalf("create foreign server: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, otherWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers", map[string]any{"server_id": foreignServerID})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("assigning across the tenant boundary must 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceMcpServer_AgentActorCannotWrite(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "mcp-actor-agent", nil)
	serverID := createWorkspaceMcpServerForTest(t, "actor-guarded", `{"url":"https://mcp.example"}`)

	asAgentActor := func(req *http.Request) *http.Request {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		return req
	}

	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceMcpServer(w, asAgentActor(newRequest(http.MethodPost, "/api/workspace-mcp-servers",
		map[string]any{"name": "by-agent", "config": map[string]any{"url": "https://evil.example"}})))
	if w.Code != http.StatusForbidden {
		t.Fatalf("an agent actor must not create a library entry, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req := asAgentActor(newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
		map[string]any{"server_id": serverID}))
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("an agent actor must not assign a server to itself, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddAgentMcpServer_PlainMemberAgentOwnerForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID, memberUserID := secretVisibilityFixture(t, "mcp-assign-gate", []byte(`{}`), nil)
	serverID := createWorkspaceMcpServerForTest(t, "member-assign-gated", `{"url":"https://mcp.example"}`)

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
		map[string]any{"server_id": serverID})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a plain member assigning a shared server to their own agent must 403, got %d: %s", w.Code, w.Body.String())
	}
	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("the refused assignment must not exist, got %v", names)
	}

	w = httptest.NewRecorder()
	req = newRequestAs(memberUserID, http.MethodPut, "/api/agents/"+agentID+"/mcp-servers/"+serverID,
		map[string]any{"enabled": true})
	req = withTwoURLParams(req, "id", agentID, "serverId", serverID)
	testHandler.SetAgentMcpServerEnabled(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a plain member toggling an assignment must 403, got %d: %s", w.Code, w.Body.String())
	}
}

func withTwoURLParams(req *http.Request, k1, v1, k2, v2 string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(k1, v1)
	rctx.URLParams.Add(k2, v2)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
