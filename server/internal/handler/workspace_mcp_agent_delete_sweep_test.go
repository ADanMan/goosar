package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func seedAgentMcpAssignment(t *testing.T, ctx context.Context, agentID string) string {
	t.Helper()
	var serverID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, config, transport)
		VALUES ($1, $2, '{}'::jsonb, 'http')
		RETURNING id
	`, testWorkspaceID, "sweep-"+uuid.NewString()[:8]).Scan(&serverID); err != nil {
		t.Fatalf("insert workspace_mcp_server: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_mcp_server (agent_id, server_id) VALUES ($1, $2)`,
		agentID, serverID); err != nil {
		t.Fatalf("insert agent_mcp_server: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM agent_mcp_server WHERE server_id = $1`, serverID)
		_, _ = testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE id = $1`, serverID)
	})
	return serverID
}

func countAgentMcpAssignments(t *testing.T, ctx context.Context, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_mcp_server WHERE agent_id = $1`, agentID).Scan(&n); err != nil {
		t.Fatalf("count agent_mcp_server: %v", err)
	}
	return n
}

func TestDeleteAgentRuntime_SweepsAgentMcpAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := seedIsolatedRuntime(t, "MCP Sweep Runtime")
	agentID := seedAgentOnRuntime(t, runtimeID, "MCP Sweep Archived Agent", true)
	seedAgentMcpAssignment(t, ctx, agentID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if agentExists(t, agentID) {
		t.Fatalf("archived agent should have been hard-deleted with its runtime")
	}
	if n := countAgentMcpAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_mcp_server rows survived runtime delete: %d orphan(s)", n)
	}
}

func TestArchiveAgentsAndDeleteRuntime_SweepsAgentMcpAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "MCP Sweep Cascade Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "MCP Sweep Cascade Agent")
	seedAgentMcpAssignment(t, ctx, agentID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/archive-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{agentID}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ArchiveAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveAgentsAndDeleteRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := countAgentMcpAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_mcp_server rows survived cascade runtime delete: %d orphan(s)", n)
	}
}

func TestDeleteRuntimeProfile_SweepsAgentMcpAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	profileID := insertRuntimeProfileFixture(t, ctx, "MCP Sweep Profile", "runtime-e", "company-codex-mcp-sweep")
	runtimeID := insertProfileRuntimeFixture(t, ctx, profileID, "MCP Sweep Profile Runtime", "runtime-e")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "MCP Sweep Profile Agent")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive agent: %v", err)
	}
	seedAgentMcpAssignment(t, ctx, agentID)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", profileID)
	testHandler.DeleteRuntimeProfile(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteRuntimeProfile: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if agentExists(t, agentID) {
		t.Fatalf("archived agent should have been hard-deleted with its runtime profile")
	}
	if n := countAgentMcpAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_mcp_server rows survived runtime-profile delete: %d orphan(s)", n)
	}
}
