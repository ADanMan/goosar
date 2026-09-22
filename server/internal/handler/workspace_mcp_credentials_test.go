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

const credSecret = "user-a-personal-token-9f3c"

func createMcpServerWithSchemaForTest(t *testing.T, name, config string, schema []McpCredentialField) string {
	t.Helper()
	withTestMcpBox(t, newTestMcpBox(t))

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/workspace-mcp-servers", map[string]any{
		"name":              name,
		"config":            json.RawMessage(config),
		"credential_schema": schema,
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
		testPool.Exec(bg, `DELETE FROM workspace_mcp_user_credential WHERE server_id = $1`, resp.ID)
		testPool.Exec(bg, `DELETE FROM agent_mcp_server WHERE server_id = $1`, resp.ID)
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE id = $1`, resp.ID)
	})
	return resp.ID
}

func setMcpCredentialsAs(t *testing.T, userID, serverID string, values map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, http.MethodPut,
		"/api/workspace-mcp-servers/"+serverID+"/credentials", map[string]any{"values": values})
	req = withURLParam(req, "serverId", serverID)
	testHandler.SetWorkspaceMcpCredentials(w, req)
	return w
}

func listWorkspaceMcpServersAs(t *testing.T, userID string) []WorkspaceMcpServerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListWorkspaceMcpServers(w, newRequestAs(userID, http.MethodGet, "/api/workspace-mcp-servers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceMcpServers as %s: expected 200, got %d: %s", userID, w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode library listing: %v", err)
	}
	return resp
}

func findServerResponse(t *testing.T, list []WorkspaceMcpServerResponse, serverID string) WorkspaceMcpServerResponse {
	t.Helper()
	for _, item := range list {
		if item.ID == serverID {
			return item
		}
	}
	t.Fatalf("server %s not present in %+v", serverID, list)
	return WorkspaceMcpServerResponse{}
}

func TestWorkspaceMcpCredentials_AnotherUsersValuesAreUnreadable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ownerUserID := createMcpCredentialMember(t, "creds-isolation-owner")
	otherMemberID := createMcpCredentialMember(t, "creds-isolation-other")

	serverID := createMcpServerWithSchemaForTest(t, "creds-isolation", `{"command":"jira-mcp"}`,
		[]McpCredentialField{{Key: "JIRA_TOKEN", Hint: "Profile → Security → API tokens", Required: true}})

	if w := setMcpCredentialsAs(t, ownerUserID, serverID, map[string]string{"JIRA_TOKEN": credSecret}); w.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceMcpCredentials: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	bodies := map[string][]WorkspaceMcpServerResponse{
		"workspace owner": listWorkspaceMcpServersAs(t, testUserID),
		"other member":    listWorkspaceMcpServersAs(t, otherMemberID),
	}
	for who, list := range bodies {
		got := findServerResponse(t, list, serverID)
		if len(got.ProvidedCredentials) != 0 {
			t.Fatalf("%s sees another user's presence markers: %+v", who, got.ProvidedCredentials)
		}
		if len(got.MissingCredentials) != 1 || got.MissingCredentials[0] != "JIRA_TOKEN" {
			t.Fatalf("%s must be told THEIR OWN field is missing, got %+v", who, got.MissingCredentials)
		}
	}

	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatalf("marshal listings: %v", err)
	}
	if strings.Contains(string(raw), credSecret) {
		t.Fatalf("a listing returned another user's credential value: %s", raw)
	}

	own := findServerResponse(t, listWorkspaceMcpServersAs(t, ownerUserID), serverID)
	if len(own.ProvidedCredentials) != 1 || own.ProvidedCredentials[0] != "JIRA_TOKEN" {
		t.Fatalf("the author must see their own presence marker, got %+v", own.ProvidedCredentials)
	}
	if len(own.MissingCredentials) != 0 {
		t.Fatalf("nothing is missing once the field is supplied, got %+v", own.MissingCredentials)
	}
	ownRaw, err := json.Marshal(own)
	if err != nil {
		t.Fatalf("marshal own listing: %v", err)
	}
	if strings.Contains(string(ownRaw), credSecret) {
		t.Fatalf("the author's own listing returned the value back: %s", ownRaw)
	}
}

func TestWorkspaceMcpCredentials_StoredValueIsSealed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	serverID := createMcpServerWithSchemaForTest(t, "creds-sealed", `{"command":"jira-mcp"}`,
		[]McpCredentialField{{Key: "API_TOKEN", Required: true}})
	if w := setMcpCredentialsAs(t, testUserID, serverID, map[string]string{"API_TOKEN": credSecret}); w.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceMcpCredentials: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT sealed_values::text FROM workspace_mcp_user_credential WHERE server_id = $1`, serverID).Scan(&stored); err != nil {
		t.Fatalf("read stored credentials: %v", err)
	}
	if strings.Contains(stored, credSecret) {
		t.Fatalf("credential value stored in plaintext: %s", stored)
	}
	if !isSealedMcpConfig([]byte(stored)) {
		t.Fatalf("stored credentials are not a sealed envelope: %s", stored)
	}
}

func TestWorkspaceMcpCredentials_RejectsUndeclaredField(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	serverID := createMcpServerWithSchemaForTest(t, "creds-undeclared", `{"command":"jira-mcp"}`,
		[]McpCredentialField{{Key: "DECLARED", Required: true}})
	w := setMcpCredentialsAs(t, testUserID, serverID, map[string]string{"NOT_DECLARED": "x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an undeclared field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceMcpCredentials_LayeringIsScopedToTheAgentOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	ownerUserID := createMcpCredentialMember(t, "creds-layering-owner")
	otherMemberID := createMcpCredentialMember(t, "creds-layering-other")
	serverID := createMcpServerWithSchemaForTest(t, "creds-layering", `{"command":"jira-mcp"}`,
		[]McpCredentialField{{Key: "JIRA_TOKEN", Required: true}})

	ownerAgentID := createHandlerTestAgentOwnedBy(t, "creds-owner-agent", ownerUserID)
	otherAgentID := createHandlerTestAgentOwnedBy(t, "creds-other-agent", otherMemberID)
	assignMcpServerToAgent(t, ownerAgentID, serverID)
	assignMcpServerToAgent(t, otherAgentID, serverID)

	if w := setMcpCredentialsAs(t, ownerUserID, serverID, map[string]string{"JIRA_TOKEN": credSecret}); w.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceMcpCredentials: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	entry, missing := resolveClaimEntryForTest(t, ownerAgentID, ownerUserID)
	if len(missing) != 0 {
		t.Fatalf("the owner supplied the field; nothing may be missing, got %v", missing)
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(entry, &doc); err != nil {
		t.Fatalf("parse layered entry: %v", err)
	}
	if doc.Env["JIRA_TOKEN"] != credSecret {
		t.Fatalf("the owner's own value must reach their agent, got env %+v", doc.Env)
	}

	entry, missing = resolveClaimEntryForTest(t, otherAgentID, otherMemberID)
	if len(missing) != 1 || missing[0] != "JIRA_TOKEN" {
		t.Fatalf("a user who supplied nothing must be reported as missing the field, got %v", missing)
	}
	if strings.Contains(string(entry), credSecret) {
		t.Fatalf("another user's value leaked into this agent's claim: %s", entry)
	}
}

func resolveClaimEntryForTest(t *testing.T, agentID, ownerID string) (json.RawMessage, []string) {
	t.Helper()
	rows, err := testHandler.Queries.ListEnabledAgentMcpServers(context.Background(), db.ListEnabledAgentMcpServersParams{
		AgentID: parseUUID(agentID),
		UserID:  parseUUID(ownerID),
	})
	if err != nil {
		t.Fatalf("ListEnabledAgentMcpServers: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one assigned server, got %d", len(rows))
	}
	opened, err := testHandler.openConfigDocument(rows[0].Config)
	if err != nil {
		t.Fatalf("openConfigDocument: %v", err)
	}
	layered, missing, err := testHandler.layerOwnerMcpCredentials(rows[0].CredentialSchema, rows[0].SealedValues, opened)
	if err != nil {
		t.Fatalf("layerOwnerMcpCredentials: %v", err)
	}
	if len(missing) > 0 {
		return opened, missing
	}
	return layered, nil
}

func TestWorkspaceMcpCredentials_AgentListingReportsOwnerPresence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ownerUserID := createMcpCredentialMember(t, "creds-agent-view-owner")
	serverID := createMcpServerWithSchemaForTest(t, "creds-agent-view", `{"command":"jira-mcp"}`,
		[]McpCredentialField{{Key: "JIRA_TOKEN", Hint: "Profile → Security", Required: true}})
	agentID := createHandlerTestAgentOwnedBy(t, "creds-agent-view-agent", ownerUserID)
	assignMcpServerToAgent(t, agentID, serverID)

	listed := listAgentMcpServers(t, agentID)
	if len(listed) != 1 {
		t.Fatalf("expected one assignment, got %+v", listed)
	}
	if len(listed[0].MissingCredentials) != 1 || listed[0].MissingCredentials[0] != "JIRA_TOKEN" {
		t.Fatalf("the agent owner has supplied nothing, so the field must read as missing, got %+v", listed[0])
	}
	if len(listed[0].CredentialSchema) != 1 || listed[0].CredentialSchema[0].Hint == "" {
		t.Fatalf("A5 needs the admin's where-to-find-it text on this surface, got %+v", listed[0].CredentialSchema)
	}

	if w := setMcpCredentialsAs(t, ownerUserID, serverID, map[string]string{"JIRA_TOKEN": credSecret}); w.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceMcpCredentials: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	listed = listAgentMcpServers(t, agentID)
	if len(listed[0].MissingCredentials) != 0 {
		t.Fatalf("once the owner supplies the field nothing is missing, got %+v", listed[0].MissingCredentials)
	}
	if len(listed[0].ProvidedCredentials) != 1 {
		t.Fatalf("expected the owner's presence marker, got %+v", listed[0].ProvidedCredentials)
	}
	body, err := json.Marshal(listed)
	if err != nil {
		t.Fatalf("marshal agent listing: %v", err)
	}
	if strings.Contains(string(body), credSecret) {
		t.Fatalf("the agent listing returned the owner's value: %s", body)
	}
}

func TestApplyMcpCredentialValues_UserValueWinsOverTheAdminsBakedInEnv(t *testing.T) {
	schema := []McpCredentialField{{Key: "TOKEN", Required: true}}
	out, err := applyMcpCredentialValues(
		json.RawMessage(`{"command":"x","env":{"TOKEN":"admins-own","KEEP":"me"}}`),
		schema, map[string]string{"TOKEN": "mine"})
	if err != nil {
		t.Fatalf("applyMcpCredentialValues: %v", err)
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Env["TOKEN"] != "mine" {
		t.Fatalf("the user's own value must win, got %+v", doc.Env)
	}
	if doc.Env["KEEP"] != "me" {
		t.Fatalf("unrelated env must survive, got %+v", doc.Env)
	}
}

func TestApplyMcpCredentialValues_IgnoresValuesTheSchemaNoLongerDeclares(t *testing.T) {
	out, err := applyMcpCredentialValues(
		json.RawMessage(`{"command":"x"}`),
		[]McpCredentialField{{Key: "STILL_HERE"}},
		map[string]string{"STILL_HERE": "a", "REMOVED": "b"})
	if err != nil {
		t.Fatalf("applyMcpCredentialValues: %v", err)
	}
	if strings.Contains(string(out), "REMOVED") {
		t.Fatalf("an undeclared leftover value was injected: %s", out)
	}
}

func TestMissingMcpCredentials_OnlyRequiredFieldsCount(t *testing.T) {
	schema := []McpCredentialField{
		{Key: "NEEDED", Required: true},
		{Key: "OPTIONAL"},
	}
	if missing := missingMcpCredentials(schema, []string{"NEEDED"}); missing != nil {
		t.Fatalf("an unsupplied optional field is not missing, got %v", missing)
	}
	if missing := missingMcpCredentials(schema, nil); len(missing) != 1 || missing[0] != "NEEDED" {
		t.Fatalf("expected [NEEDED], got %v", missing)
	}
}

func TestMcpCredentialKeys_BlankValueIsNotAPresenceMarker(t *testing.T) {
	keys := mcpCredentialKeys(map[string]string{"A": "v", "B": "   ", "C": ""})
	if len(keys) != 1 || keys[0] != "A" {
		t.Fatalf("expected only A to count as supplied, got %v", keys)
	}
}

func createMcpCredentialMember(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, name, name+"@mcp-credentials.goosar.test").Scan(&userID); err != nil {
		t.Fatalf("create member %s: %v", name, err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add member %s: %v", name, err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_user_credential WHERE user_id = $1`, userID)
		testPool.Exec(bg, `DELETE FROM agent WHERE owner_id = $1`, userID)
		testPool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func createHandlerTestAgentOwnedBy(t *testing.T, name, ownerID string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4, '', '{}'::jsonb, '[]'::jsonb, NULL)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent owned by %s: %v", ownerID, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func TestWorkspaceMcpCredentials_PartialWriteKeepsTheOtherFields(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	userID := createMcpCredentialMember(t, "creds-partial")
	serverID := createMcpServerWithSchemaForTest(t, "creds-partial", `{"command":"jira-mcp"}`,
		[]McpCredentialField{
			{Key: "JIRA_URL", Required: true},
			{Key: "JIRA_TOKEN", Required: true},
		})

	if w := setMcpCredentialsAs(t, userID, serverID, map[string]string{
		"JIRA_URL": "https://jira.example", "JIRA_TOKEN": "first",
	}); w.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceMcpCredentials: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if w := setMcpCredentialsAs(t, userID, serverID, map[string]string{"JIRA_TOKEN": credSecret}); w.Code != http.StatusOK {
		t.Fatalf("rotate token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := findServerResponse(t, listWorkspaceMcpServersAs(t, userID), serverID)
	if len(got.ProvidedCredentials) != 2 {
		t.Fatalf("a partial write dropped the other field, markers: %+v", got.ProvidedCredentials)
	}
	if len(got.MissingCredentials) != 0 {
		t.Fatalf("nothing may go missing on a partial write, got %+v", got.MissingCredentials)
	}

	agentID := createHandlerTestAgentOwnedBy(t, "creds-partial-agent", userID)
	assignMcpServerToAgent(t, agentID, serverID)
	entry, missing := resolveClaimEntryForTest(t, agentID, userID)
	if len(missing) != 0 {
		t.Fatalf("expected a complete credential set, missing %v", missing)
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(entry, &doc); err != nil {
		t.Fatalf("parse layered entry: %v", err)
	}
	if doc.Env["JIRA_URL"] != "https://jira.example" || doc.Env["JIRA_TOKEN"] != credSecret {
		t.Fatalf("the merged set must reach the agent, got env %+v", doc.Env)
	}

	if w := setMcpCredentialsAs(t, userID, serverID, map[string]string{"JIRA_TOKEN": ""}); w.Code != http.StatusOK {
		t.Fatalf("clear one field: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got = findServerResponse(t, listWorkspaceMcpServersAs(t, userID), serverID)
	if len(got.MissingCredentials) != 1 || got.MissingCredentials[0] != "JIRA_TOKEN" {
		t.Fatalf("expected only JIRA_TOKEN to be cleared, got %+v", got)
	}
}

func TestWorkspaceMcpCredentials_RefusedOnAnHttpEntry(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceMcpServer(w, newRequest(http.MethodPost, "/api/workspace-mcp-servers", map[string]any{
		"name":              "creds-http",
		"config":            json.RawMessage(`{"url":"https://mcp.example"}`),
		"credential_schema": []McpCredentialField{{Key: "JIRA_TOKEN", Required: true}},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a credential schema on an http entry, got %d: %s", w.Code, w.Body.String())
	}

	serverID := createMcpServerWithSchemaForTest(t, "creds-http-update", `{"command":"jira-mcp"}`,
		[]McpCredentialField{{Key: "JIRA_TOKEN", Required: true}})
	w = httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/workspace-mcp-servers/"+serverID, map[string]any{
		"config": json.RawMessage(`{"url":"https://mcp.example"}`),
	})
	req = withURLParam(req, "serverId", serverID)
	testHandler.UpdateWorkspaceMcpServer(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when a schema-carrying entry becomes http, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMergeMcpCredentialValues_DropsUndeclaredAndBlankKeys(t *testing.T) {
	schema := []McpCredentialField{{Key: "A"}, {Key: "B"}}
	merged := mergeMcpCredentialValues(schema,
		map[string]string{"A": "keep", "B": "old", "GONE": "stale"},
		map[string]string{"B": "new", "NOT_DECLARED": "x"})
	if merged["A"] != "keep" || merged["B"] != "new" {
		t.Fatalf("expected A kept and B replaced, got %+v", merged)
	}
	if len(merged) != 2 {
		t.Fatalf("undeclared keys must not survive a merge, got %+v", merged)
	}
}
