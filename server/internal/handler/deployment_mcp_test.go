package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func createDeploymentMcpServerForTest(t *testing.T, name, config string) (id, body string) {
	t.Helper()
	withTestMcpBox(t, newTestMcpBox(t))

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/deployment/mcp-servers", map[string]any{
		"name":   name,
		"config": json.RawMessage(config),
	})
	testHandler.CreateDeploymentMcpServer(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDeploymentMcpServer: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DeploymentMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM agent_mcp_server WHERE server_id = $1`, resp.ID)
		testPool.Exec(bg, `DELETE FROM workspace_deployment_mcp_server WHERE server_id = $1`, resp.ID)
		testPool.Exec(bg, `DELETE FROM deployment_mcp_server WHERE id = $1`, resp.ID)
	})
	return resp.ID, w.Body.String()
}

func setDeploymentMcpEnabledForTest(t *testing.T, serverID string, enabled bool) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/deployment-mcp-servers/"+serverID+"/enabled", map[string]any{"enabled": enabled})
	req = withURLParam(req, "serverId", serverID)
	testHandler.SetWorkspaceDeploymentMcpServerEnabled(w, req)
	return w.Code
}

func workspaceLibraryEntries(t *testing.T) []WorkspaceMcpServerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListWorkspaceMcpServers(w, newRequest(http.MethodGet, "/api/workspace-mcp-servers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceMcpServers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode library response: %v", err)
	}
	return resp
}

func findLibraryEntry(entries []WorkspaceMcpServerResponse, id string) *WorkspaceMcpServerResponse {
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i]
		}
	}
	return nil
}

func TestDeploymentMcpServer_AuthoredRecordReachesNoWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	serverID, _ := createDeploymentMcpServerForTest(t, "offered-not-taken", `{"url":"https://mcp.example/offered"}`)
	agentID := createHandlerTestAgent(t, "deployment-mcp-untaken-agent", nil)

	if entry := findLibraryEntry(workspaceLibraryEntries(t), serverID); entry != nil {
		t.Fatalf("an unenabled deployment record must not appear in the workspace library, got %+v", entry)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers", map[string]any{"server_id": serverID})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("assigning an unenabled deployment record: status = %d, want 404: %s", w.Code, w.Body.String())
	}
	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("claim path saw %v, want nothing", names)
	}
}

func TestDeploymentMcpServer_EnabledThenAssignedReachesAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	serverID, _ := createDeploymentMcpServerForTest(t, "taken-record", `{"url":"https://mcp.example/taken"}`)
	agentID := createHandlerTestAgent(t, "deployment-mcp-taken-agent", nil)

	if code := setDeploymentMcpEnabledForTest(t, serverID, true); code != http.StatusNoContent {
		t.Fatalf("enable: status = %d, want 204", code)
	}
	entry := findLibraryEntry(workspaceLibraryEntries(t), serverID)
	if entry == nil {
		t.Fatal("an enabled deployment record must appear in the workspace library")
	}
	if entry.Source != mcpSourceDeployment {
		t.Fatalf("source = %q, want %q", entry.Source, mcpSourceDeployment)
	}

	assigned := assignMcpServerToAgent(t, agentID, serverID)
	if len(assigned) != 1 || assigned[0].Source != mcpSourceDeployment {
		t.Fatalf("agent assignment list = %+v, want one deployment entry", assigned)
	}
	names := enabledAssignmentNames(t, agentID)
	if len(names) != 1 || names[0] != "taken-record" {
		t.Fatalf("claim path saw %v, want [taken-record]", names)
	}
}

func TestDeploymentMcpServer_DisableRevokesFromAgents(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	serverID, _ := createDeploymentMcpServerForTest(t, "revoked-record", `{"url":"https://mcp.example/revoked"}`)
	agentID := createHandlerTestAgent(t, "deployment-mcp-revoked-agent", nil)
	if code := setDeploymentMcpEnabledForTest(t, serverID, true); code != http.StatusNoContent {
		t.Fatalf("enable: status = %d, want 204", code)
	}
	assignMcpServerToAgent(t, agentID, serverID)

	if code := setDeploymentMcpEnabledForTest(t, serverID, false); code != http.StatusNoContent {
		t.Fatalf("disable: status = %d, want 204", code)
	}
	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("claim path saw %v after disable, want nothing", names)
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, serverID).Scan(&count); err != nil {
		t.Fatalf("count assignments: %v", err)
	}
	if count != 0 {
		t.Fatalf("assignments left after disable = %d, want 0", count)
	}
	if entry := findLibraryEntry(workspaceLibraryEntries(t), serverID); entry != nil {
		t.Fatalf("a disabled record must leave the workspace library, got %+v", entry)
	}
}

func TestDeploymentMcpServer_DeleteSweepsEnablementsAndAssignments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	serverID, _ := createDeploymentMcpServerForTest(t, "deleted-record", `{"url":"https://mcp.example/deleted"}`)
	agentID := createHandlerTestAgent(t, "deployment-mcp-deleted-agent", nil)
	setDeploymentMcpEnabledForTest(t, serverID, true)
	assignMcpServerToAgent(t, agentID, serverID)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/deployment/mcp-servers/"+serverID, nil), "serverId", serverID)
	testHandler.DeleteDeploymentMcpServer(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204: %s", w.Code, w.Body.String())
	}

	for table, query := range map[string]string{
		"workspace_deployment_mcp_server": `SELECT count(*) FROM workspace_deployment_mcp_server WHERE server_id = $1`,
		"agent_mcp_server":                `SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`,
	} {
		var count int
		if err := testPool.QueryRow(context.Background(), query, serverID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows left after delete = %d, want 0", table, count)
		}
	}
	if names := enabledAssignmentNames(t, agentID); len(names) != 0 {
		t.Fatalf("claim path saw %v after delete, want nothing", names)
	}
}

func TestDeploymentMcpServer_ConfigNeverLeavesTheDatabase(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	const secret = "https://mcp.example/s3cr3t-token"
	serverID, createBody := createDeploymentMcpServerForTest(t, "sealed-record", `{"url":"`+secret+`","headers":{"Authorization":"Bearer nope"}}`)
	setDeploymentMcpEnabledForTest(t, serverID, true)

	bodies := map[string]string{"create": createBody}

	w := httptest.NewRecorder()
	testHandler.ListDeploymentMcpServers(w, newRequest(http.MethodGet, "/api/deployment/mcp-servers", nil))
	bodies["deployment list"] = w.Body.String()

	w = httptest.NewRecorder()
	testHandler.ListWorkspaceDeploymentMcpServers(w, newRequest(http.MethodGet, "/api/deployment-mcp-servers", nil))
	bodies["workspace offers"] = w.Body.String()

	w = httptest.NewRecorder()
	testHandler.ListWorkspaceMcpServers(w, newRequest(http.MethodGet, "/api/workspace-mcp-servers", nil))
	bodies["workspace library"] = w.Body.String()

	for surface, body := range bodies {
		for _, needle := range []string{secret, "Bearer", "headers", "url"} {
			if strings.Contains(body, needle) {
				t.Fatalf("%s response leaked %q: %s", surface, needle, body)
			}
		}
	}

	var stored []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT config FROM deployment_mcp_server WHERE id = $1`, serverID).Scan(&stored); err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if strings.Contains(string(stored), secret) {
		t.Fatal("stored config is plaintext; it must be sealed at rest")
	}
}

func TestDeploymentMcpServer_NonDeploymentAdminRefused(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)

	cases := map[string]func(w http.ResponseWriter, r *http.Request){
		"list":   testHandler.ListDeploymentMcpServers,
		"create": testHandler.CreateDeploymentMcpServer,
		"update": testHandler.UpdateDeploymentMcpServer,
		"delete": testHandler.DeleteDeploymentMcpServer,
	}
	for name, handle := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest(http.MethodPost, "/api/deployment/mcp-servers", map[string]any{
				"name": "nope", "config": json.RawMessage(`{"url":"https://mcp.example"}`),
			})
			req = withURLParam(req, "serverId", testWorkspaceID)
			handle(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestDeploymentMcpServer_MutationsAreAudited(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	serverID, _ := createDeploymentMcpServerForTest(t, "audited-record", `{"url":"https://mcp.example/audited"}`)
	if got := countAdminAuditRows(t, adminAuditActionDeploymentMcpCreate); got != 1 {
		t.Fatalf("create audit rows = %d, want 1", got)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/deployment/mcp-servers/"+serverID, map[string]any{
		"name": "audited-record-renamed",
	})
	req = withURLParam(req, "serverId", serverID)
	testHandler.UpdateDeploymentMcpServer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := countAdminAuditRows(t, adminAuditActionDeploymentMcpUpdate); got != 1 {
		t.Fatalf("update audit rows = %d, want 1", got)
	}

	var afterHash string
	if err := testPool.QueryRow(context.Background(),
		`SELECT coalesce(after_hash, '') FROM admin_audit WHERE action = $1`,
		adminAuditActionDeploymentMcpCreate).Scan(&afterHash); err != nil {
		t.Fatalf("read audit hash: %v", err)
	}
	if strings.Contains(afterHash, "mcp.example") {
		t.Fatal("audit journal must carry a hash, never a value")
	}
}

func TestDeploymentMcpServer_NameCollisionResolvesToWorkspaceEntry(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	const shared = "collide"
	deploymentID, _ := createDeploymentMcpServerForTest(t, shared, `{"url":"https://mcp.example/from-deployment"}`)
	workspaceServerID := createWorkspaceMcpServerForTest(t, shared, `{"url":"https://mcp.example/from-workspace"}`)
	agentID := createHandlerTestAgent(t, "deployment-mcp-collision-agent", nil)

	if code := setDeploymentMcpEnabledForTest(t, deploymentID, true); code != http.StatusNoContent {
		t.Fatalf("enable: status = %d, want 204", code)
	}
	assignMcpServerToAgent(t, agentID, deploymentID)
	assignMcpServerToAgent(t, agentID, workspaceServerID)

	rows, err := testHandler.Queries.ListEnabledAgentMcpServers(context.Background(), db.ListEnabledAgentMcpServersParams{
		AgentID:     parseUUID(agentID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("ListEnabledAgentMcpServers: %v", err)
	}
	var collisions []string
	for _, row := range rows {
		if row.Name == shared {
			collisions = append(collisions, row.Source)
		}
	}
	if len(collisions) != 2 {
		t.Fatalf("rows named %q = %d, want 2", shared, len(collisions))
	}

	if collisions[0] != mcpSourceWorkspace || collisions[1] != mcpSourceDeployment {
		t.Fatalf("collision order = %v, want [workspace deployment]", collisions)
	}

	resolved, err := ResolveAgentMcpConfig([]WorkspaceMcpAssignment{
		{Name: shared, Config: json.RawMessage(`{"url":"https://mcp.example/from-workspace"}`)},
		{Name: shared, Config: json.RawMessage(`{"url":"https://mcp.example/from-deployment"}`)},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	if !strings.Contains(string(resolved), "from-workspace") || strings.Contains(string(resolved), "from-deployment") {
		t.Fatalf("resolved config = %s, want the first (workspace) entry to win", resolved)
	}
}

func TestDeploymentMcpServer_UserCredentialsLayerOverDeploymentRecord(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	withTestMcpBox(t, newTestMcpBox(t))

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/deployment/mcp-servers", map[string]any{
		"name":   "deployment-with-schema",
		"config": json.RawMessage(`{"command":"jira-mcp","args":["serve"]}`),
		"credential_schema": []McpCredentialField{
			{Key: "JIRA_TOKEN", Label: "Jira token", Hint: "Profile - Security - API tokens", Required: true},
		},
	})
	testHandler.CreateDeploymentMcpServer(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDeploymentMcpServer: status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var created DeploymentMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.CredentialSchema) != 1 {
		t.Fatalf("credential_schema = %+v, want one field", created.CredentialSchema)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_user_credential WHERE server_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM agent_mcp_server WHERE server_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM workspace_deployment_mcp_server WHERE server_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM deployment_mcp_server WHERE id = $1`, created.ID)
	})

	if code := setDeploymentMcpEnabledForTest(t, created.ID, true); code != http.StatusNoContent {
		t.Fatalf("enable: status = %d, want 204", code)
	}
	agentID := createHandlerTestAgent(t, "deployment-mcp-credential-agent", nil)
	assignMcpServerToAgent(t, agentID, created.ID)

	claim := func() db.ListEnabledAgentMcpServersRow {
		t.Helper()
		rows, err := testHandler.Queries.ListEnabledAgentMcpServers(context.Background(), db.ListEnabledAgentMcpServersParams{
			AgentID:     parseUUID(agentID),
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		})
		if err != nil {
			t.Fatalf("ListEnabledAgentMcpServers: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("assigned rows = %d, want 1", len(rows))
		}
		return rows[0]
	}

	row := claim()
	opened, err := testHandler.openConfigDocument(row.Config)
	if err != nil {
		t.Fatalf("openConfigDocument: %v", err)
	}
	if _, missing, err := testHandler.layerOwnerMcpCredentials(row.CredentialSchema, row.SealedValues, opened); err != nil {
		t.Fatalf("layerOwnerMcpCredentials: %v", err)
	} else if len(missing) != 1 || missing[0] != "JIRA_TOKEN" {
		t.Fatalf("missing = %v, want [JIRA_TOKEN]", missing)
	}

	if resp := setMcpCredentialsAs(t, testUserID, created.ID, map[string]string{"JIRA_TOKEN": "deployment-secret"}); resp.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceMcpCredentials on a deployment record: status = %d, want 200: %s", resp.Code, resp.Body.String())
	}

	row = claim()
	opened, err = testHandler.openConfigDocument(row.Config)
	if err != nil {
		t.Fatalf("openConfigDocument: %v", err)
	}
	layered, missing, err := testHandler.layerOwnerMcpCredentials(row.CredentialSchema, row.SealedValues, opened)
	if err != nil {
		t.Fatalf("layerOwnerMcpCredentials: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	if !strings.Contains(string(layered), "deployment-secret") {
		t.Fatalf("layered entry does not carry the user's value: %s", layered)
	}

	entry := findServerResponse(t, listWorkspaceMcpServersAs(t, testUserID), created.ID)
	if entry.Source != mcpSourceDeployment {
		t.Fatalf("source = %q, want %q", entry.Source, mcpSourceDeployment)
	}
	if len(entry.ProvidedCredentials) != 1 || entry.ProvidedCredentials[0] != "JIRA_TOKEN" {
		t.Fatalf("provided = %v, want [JIRA_TOKEN]", entry.ProvidedCredentials)
	}
	if len(entry.MissingCredentials) != 0 {
		t.Fatalf("missing = %v, want none", entry.MissingCredentials)
	}
}
