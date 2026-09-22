package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func updateWorkspaceMcpServer(t *testing.T, serverID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/workspace-mcp-servers/"+serverID, body)
	req = withURLParam(req, "serverId", serverID)
	testHandler.UpdateWorkspaceMcpServer(w, req)
	return w
}

func storedMcpConfig(t *testing.T, serverID string) string {
	t.Helper()
	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT config::text FROM workspace_mcp_server WHERE id = $1`, serverID).Scan(&stored); err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	return stored
}

func TestUpdateWorkspaceMcpServer_RenameKeepsConfigAndTransport(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	serverID := createWorkspaceMcpServerForTest(t, "rename-before", `{"url":"https://mcp.example"}`)
	configBefore := storedMcpConfig(t, serverID)

	w := updateWorkspaceMcpServer(t, serverID, map[string]any{"name": "rename-after"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "rename-after" || resp.Transport != "http" {
		t.Fatalf("expected renamed entry with its transport intact, got %+v", resp)
	}
	if got := storedMcpConfig(t, serverID); got != configBefore {
		t.Fatalf("a name-only update must not touch the sealed body:\nbefore %s\nafter  %s", configBefore, got)
	}
}

func TestUpdateWorkspaceMcpServer_ConfigReplaceRecomputesTransport(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	serverID := createWorkspaceMcpServerForTest(t, "transport-flip", `{"url":"https://mcp.example"}`)

	w := updateWorkspaceMcpServer(t, serverID, map[string]any{
		"config": map[string]any{"command": "npx", "args": []string{"-y", "pkg"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("config replace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Transport != "stdio" {
		t.Fatalf("expected the transport recomputed to stdio, got %q", resp.Transport)
	}
}

func TestUpdateWorkspaceMcpServer_MissingOrForeignIs404(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := updateWorkspaceMcpServer(t, "00000000-0000-0000-0000-000000000001",
		map[string]any{"name": "ghost"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var otherWorkspaceID, foreignServerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Other MCP Update WS', 'ws-mcp-update-tenant', '', 'OMU')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, config, transport)
		VALUES ($1, 'foreign-update', '{}'::jsonb, 'http')
		RETURNING id
	`, otherWorkspaceID).Scan(&foreignServerID); err != nil {
		t.Fatalf("create foreign server: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, otherWorkspaceID)
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	w = updateWorkspaceMcpServer(t, foreignServerID, map[string]any{"name": "hijack"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("foreign id: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceMcpServer_QueryFailureIs500(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	serverID := createWorkspaceMcpServerForTest(t, "boom-target", `{"url":"https://mcp.example"}`)

	if _, err := testPool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_mcp_update_boom() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'injected update failure'; END $$ LANGUAGE plpgsql;
	`); err != nil {
		t.Fatalf("create boom function: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		CREATE TRIGGER test_mcp_update_boom_trg BEFORE UPDATE ON workspace_mcp_server
		FOR EACH ROW WHEN (NEW.name = 'boom-now') EXECUTE FUNCTION test_mcp_update_boom();
	`); err != nil {
		t.Fatalf("create boom trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DROP TRIGGER IF EXISTS test_mcp_update_boom_trg ON workspace_mcp_server`)
		testPool.Exec(bg, `DROP FUNCTION IF EXISTS test_mcp_update_boom()`)
	})

	w := updateWorkspaceMcpServer(t, serverID, map[string]any{"name": "boom-now"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("a non-ErrNoRows query failure must be 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceMcpServer_DuplicateNameConflictsOnCreateAndUpdate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	createWorkspaceMcpServerForTest(t, "dup-taken", `{"url":"https://mcp.example"}`)
	otherID := createWorkspaceMcpServerForTest(t, "dup-other", `{"url":"https://mcp.example"}`)

	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceMcpServer(w, newRequest(http.MethodPost, "/api/workspace-mcp-servers", map[string]any{
		"name":   "dup-taken",
		"config": map[string]any{"url": "https://mcp.example"},
	}))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	w = updateWorkspaceMcpServer(t, otherID, map[string]any{"name": "dup-taken"})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate rename: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRemoveAgentMcpServer_RemovesAssignmentAndIsNotIdempotentlySilent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "mcp-remove-agent", nil)
	serverID := createWorkspaceMcpServerForTest(t, "remove-me", `{"url":"https://mcp.example"}`)
	assignMcpServerToAgent(t, agentID, serverID)

	remove := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodDelete, "/api/agents/"+agentID+"/mcp-servers/"+serverID, nil)
		req = withTwoURLParams(req, "id", agentID, "serverId", serverID)
		testHandler.RemoveAgentMcpServer(w, req)
		return w
	}

	w := remove()
	if w.Code != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("expected the assignment list emptied, got %+v", resp)
	}
	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("a removed assignment must not be delivered, got %v", names)
	}

	var libraryRows int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_mcp_server WHERE id = $1`, serverID).Scan(&libraryRows); err != nil {
		t.Fatalf("count library rows: %v", err)
	}
	if libraryRows != 1 {
		t.Fatalf("removing an assignment must not delete the library entry, found %d row(s)", libraryRows)
	}

	if w := remove(); w.Code != http.StatusNotFound {
		t.Fatalf("second remove: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
