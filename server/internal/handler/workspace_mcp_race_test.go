package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWorkspaceMcpServerCreate_CannotLandAfterWorkspaceTeardownCommits(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	var victimID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('MCP Race WS', 'ws-mcp-race-create', '', 'MRC')
		RETURNING id
	`).Scan(&victimID); err != nil {
		t.Fatalf("create victim workspace: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, victimID)
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, victimID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		victimID, testUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	teardown, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = teardown.Rollback(ctx)
		}
	}()
	holderPID := holderBackendPID(t, ctx, teardown)

	if _, err := teardown.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, victimID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	var wg sync.WaitGroup
	var writerCode int
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		conn, err := testPool.Acquire(bg)
		if err != nil {
			return
		}
		defer conn.Release()

		h := *testHandler
		h.TxStarter = conn

		req := newRequest(http.MethodPost, "/api/workspace-mcp-servers", map[string]any{
			"name":   "raced",
			"config": map[string]any{"url": "https://mcp.example"},
		})
		req.Header.Set("X-Workspace-ID", victimID)
		w := httptest.NewRecorder()
		h.CreateWorkspaceMcpServer(w, req)
		writerCode = w.Code
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 5*time.Second) {
		wg.Wait()
		t.Fatal("the create never blocked on the workspace row: it does not join the teardown fence")
	}

	if _, err := teardown.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, victimID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	if err := teardown.Commit(ctx); err != nil {
		t.Fatalf("commit teardown: %v", err)
	}
	committed = true

	wg.Wait()

	if writerCode == http.StatusCreated {
		t.Fatalf("the queued create landed after teardown committed (status %d); the fence did not hold", writerCode)
	}
	var orphans int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1`, victimID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("teardown left %d MCP server row(s) behind for a deleted workspace", orphans)
	}
}

func TestAgentMcpServerAdd_CannotLandAfterServerDeleteCommits(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	serverID := createWorkspaceMcpServerForTest(t, "raced-server", `{"url":"https://mcp.example"}`)
	agentID := createHandlerTestAgent(t, "ws-mcp-race-agent", nil)

	deleter, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deleter: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = deleter.Rollback(ctx)
		}
	}()
	holderPID := holderBackendPID(t, ctx, deleter)

	if _, err := deleter.Exec(ctx,
		`SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE`, testWorkspaceID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}
	if _, err := deleter.Exec(ctx,
		`SELECT id FROM workspace_mcp_server WHERE id = $1 FOR UPDATE`, serverID); err != nil {
		t.Fatalf("lock server: %v", err)
	}

	var wg sync.WaitGroup
	var writerCode int
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		conn, err := testPool.Acquire(bg)
		if err != nil {
			return
		}
		defer conn.Release()

		h := *testHandler
		h.TxStarter = conn

		req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
			map[string]any{"server_id": serverID})
		req = withURLParam(req, "id", agentID)
		w := httptest.NewRecorder()
		h.AddAgentMcpServer(w, req)
		writerCode = w.Code
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 5*time.Second) {
		wg.Wait()
		t.Fatal("the assignment never blocked on the server row: it does not take the shared lock")
	}

	if _, err := deleter.Exec(ctx, `DELETE FROM agent_mcp_server WHERE server_id = $1`, serverID); err != nil {
		t.Fatalf("sweep assignments: %v", err)
	}
	if _, err := deleter.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE id = $1`, serverID); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	if err := deleter.Commit(ctx); err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	committed = true

	wg.Wait()

	if writerCode == http.StatusOK {
		t.Fatalf("the queued assignment landed after the server was deleted (status %d)", writerCode)
	}
	var orphans int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, serverID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("delete left %d orphaned assignment(s)", orphans)
	}
}

func TestDeleteWorkspace_SweepsMcpLibraryAndAssignments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var victimID, serverID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('MCP Teardown WS', 'ws-mcp-teardown', '', 'MTW')
		RETURNING id
	`).Scan(&victimID); err != nil {
		t.Fatalf("create victim workspace: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, victimID)
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, victimID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, config, transport)
		VALUES ($1, 'doomed-with-workspace', '{}'::jsonb, 'http')
		RETURNING id
	`, victimID).Scan(&serverID); err != nil {
		t.Fatalf("create library entry: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_mcp_server (agent_id, server_id) VALUES (gen_random_uuid(), $1)
	`, serverID); err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	if err := testHandler.Queries.DeleteWorkspace(ctx, parseUUID(victimID)); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	var servers, assignments int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1`, victimID).Scan(&servers); err != nil {
		t.Fatalf("count servers: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, serverID).Scan(&assignments); err != nil {
		t.Fatalf("count assignments: %v", err)
	}
	if servers != 0 {
		t.Fatalf("%d MCP library row(s) outlived the workspace", servers)
	}
	if assignments != 0 {
		t.Fatalf("%d MCP assignment row(s) outlived the workspace", assignments)
	}
}

func TestWorkspaceMcpServerDelete_JoinsWorkspaceTeardownFence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var victimID, serverID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('MCP Race Delete WS', 'ws-mcp-race-delete', '', 'MRD')
		RETURNING id
	`).Scan(&victimID); err != nil {
		t.Fatalf("create victim workspace: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, victimID)
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, victimID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		victimID, testUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, config, transport)
		VALUES ($1, 'raced-delete', '{}'::jsonb, 'http')
		RETURNING id
	`, victimID).Scan(&serverID); err != nil {
		t.Fatalf("create library entry: %v", err)
	}

	teardown, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = teardown.Rollback(ctx)
		}
	}()
	holderPID := holderBackendPID(t, ctx, teardown)

	if _, err := teardown.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, victimID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	var wg sync.WaitGroup
	var deleterCode int
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		conn, err := testPool.Acquire(bg)
		if err != nil {
			return
		}
		defer conn.Release()

		h := *testHandler
		h.TxStarter = conn

		req := newRequest(http.MethodDelete, "/api/workspace-mcp-servers/"+serverID, nil)
		req.Header.Set("X-Workspace-ID", victimID)
		req = withURLParam(req, "serverId", serverID)
		w := httptest.NewRecorder()
		h.DeleteWorkspaceMcpServer(w, req)
		deleterCode = w.Code
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 5*time.Second) {
		wg.Wait()
		t.Fatal("the entry delete never blocked on the workspace row: it does not take the workspace lock first, so it can ABBA-deadlock with DeleteWorkspace")
	}

	if _, err := teardown.Exec(ctx,
		`DELETE FROM agent_mcp_server WHERE server_id IN (SELECT id FROM workspace_mcp_server WHERE workspace_id = $1)`,
		victimID); err != nil {
		t.Fatalf("sweep assignments: %v", err)
	}
	if _, err := teardown.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, victimID); err != nil {
		t.Fatalf("sweep library: %v", err)
	}
	if _, err := teardown.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, victimID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	if err := teardown.Commit(ctx); err != nil {
		t.Fatalf("commit teardown: %v", err)
	}
	committed = true

	wg.Wait()

	if deleterCode != http.StatusNotFound {
		t.Fatalf("a delete resumed after teardown must find nothing (404), got %d", deleterCode)
	}
}
