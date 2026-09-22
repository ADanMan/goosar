package handler

import (
	"context"
	"encoding/json"
	"testing"
)

func claimAgentMcpServers(t *testing.T, ctx context.Context, agentID, runtimeID, daemonID string) map[string]json.RawMessage {
	t.Helper()
	return claimAgentMcpServersFor(t, ctx, agentID, runtimeID, daemonID, "")
}

func TestClaim_WorkspaceMcpServersFollowTheAssignment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	serverID := createWorkspaceMcpServerForTest(t, "claimed-server", `{"url":"https://shared.example"}`)

	if servers := claimAgentMcpServers(t, ctx, agentID, runtimeID, daemonID); len(servers) != 0 {
		t.Fatalf("an unassigned library entry must not reach a claim, got %v", keysOf(servers))
	}

	assignMcpServerToAgent(t, agentID, serverID)
	servers := claimAgentMcpServers(t, ctx, agentID, runtimeID, daemonID)
	if _, ok := servers["claimed-server"]; !ok {
		t.Fatalf("an assigned server must reach the agent's next claim, got %v", keysOf(servers))
	}

	if got := string(servers["claimed-server"]); got != `{"url":"https://shared.example"}` {
		t.Fatalf("the claim must carry the decrypted entry, got %s", got)
	}

	setAssignmentEnabled(t, agentID, serverID, false)
	if servers := claimAgentMcpServers(t, ctx, agentID, runtimeID, daemonID); len(servers) != 0 {
		t.Fatalf("switching the assignment off must take effect on the NEXT claim, got %v", keysOf(servers))
	}
}

func TestClaim_AgentOwnMcpEntryWinsOverAnAssignedServer(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withTestMcpBox(t, newTestMcpBox(t))

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	own := []byte(`{"mcpServers":{"collide":{"url":"https://agent-own.example"}}}`)
	sealedOwn, err := testHandler.sealMcpConfig(own)
	if err != nil {
		t.Fatalf("seal agent mcp_config: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, sealedOwn, agentID); err != nil {
		t.Fatalf("set agent mcp_config: %v", err)
	}
	serverID := createWorkspaceMcpServerForTest(t, "collide", `{"url":"https://shared.example"}`)
	assignMcpServerToAgent(t, agentID, serverID)

	servers := claimAgentMcpServers(t, ctx, agentID, runtimeID, daemonID)
	if got, want := string(servers["collide"]), `{"url":"https://agent-own.example"}`; got != want {
		t.Fatalf("the agent's own entry must win on the claim: got %s, want %s", got, want)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}
