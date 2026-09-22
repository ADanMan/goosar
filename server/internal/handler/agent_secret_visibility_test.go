package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func secretVisibilityFixture(t *testing.T, label string, mcpConfig []byte, env map[string]string) (agentID, ownerUserID string) {
	t.Helper()
	ctx := context.Background()

	email := "secret-vis-" + label + "@goosar.test"
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Secret Vis Owner "+label, email,
	).Scan(&ownerUserID); err != nil {
		t.Fatalf("create agent owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerUserID)
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, ownerUserID,
	); err != nil {
		t.Fatalf("add agent owner as workspace member: %v", err)
	}

	envBytes := []byte(`{}`)
	if env != nil {
		var err error
		envBytes, err = json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal custom_env: %v", err)
		}
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4,
		        '', $5::jsonb, '[]'::jsonb, $6)
		RETURNING id
	`, testWorkspaceID, "secret-vis-agent-"+label, handlerTestRuntimeID(t), ownerUserID,
		string(envBytes), mcpConfig).Scan(&agentID); err != nil {
		t.Fatalf("create foreign-owned agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("seed workspace invocation target: %v", err)
	}

	return agentID, ownerUserID
}

const secretVisMcpConfig = `{"mcpServers":{"jira":{"env":{"JIRA_PERSONAL_TOKEN":"member-personal-token"}}}}`

func TestListAgents_WorkspaceOwnerCannotReadForeignMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "list", []byte(secretVisMcpConfig), nil)

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "member-personal-token") {
		t.Fatalf("workspace owner list response leaked another member's MCP secret: %s", w.Body.String())
	}

	var list []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, a := range list {
		if a.ID != agentID {
			continue
		}
		found = true

		if bytes.Contains(a.McpConfig, []byte("member-personal-token")) {
			t.Errorf("foreign agent mcp_config must not carry values, got %s", a.McpConfig)
		}
		if !a.McpConfigRedacted {
			t.Error("foreign agent must carry mcp_config_redacted=true")
		}
	}
	if !found {
		t.Fatal("foreign agent missing from list response")
	}

	w2 := httptest.NewRecorder()
	testHandler.ListAgents(w2, newRequestAs(ownerUserID, http.MethodGet, "/api/agents", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("ListAgents as owner: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "member-personal-token") {
		t.Error("the agent owner must still read their own mcp_config from the list")
	}
}

func TestGetAgent_WorkspaceOwnerCannotReadForeignMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "detail", []byte(secretVisMcpConfig), nil)

	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "member-personal-token") {
		t.Fatalf("workspace owner detail response leaked another member's MCP secret: %s", w.Body.String())
	}
	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.McpConfigRedacted {
		t.Error("foreign agent detail must carry mcp_config_redacted=true")
	}

	req2 := withURLParam(newRequestAs(ownerUserID, http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID)
	w2 := httptest.NewRecorder()
	testHandler.GetAgent(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GetAgent as owner: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "member-personal-token") {
		t.Error("the agent owner must still read their own mcp_config")
	}
}

func TestMutationResponses_WorkspaceOwnerCannotReadForeignMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "mutation", []byte(secretVisMcpConfig), nil)

	assertRedacted := func(t *testing.T, w *httptest.ResponseRecorder, label string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", label, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "member-personal-token") {
			t.Fatalf("%s response leaked another member's MCP secret: %s", label, w.Body.String())
		}
		var resp AgentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", label, err)
		}
		if !resp.McpConfigRedacted {
			t.Errorf("%s response must carry mcp_config_redacted=true", label)
		}
	}

	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID,
		map[string]any{"description": "admin touched this"}), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	assertRedacted(t, w, "UpdateAgent")

	reqA := withURLParam(newRequest(http.MethodPost, "/api/agents/"+agentID+"/archive", nil), "id", agentID)
	wA := httptest.NewRecorder()
	testHandler.ArchiveAgent(wA, reqA)
	assertRedacted(t, wA, "ArchiveAgent")

	reqR := withURLParam(newRequest(http.MethodPost, "/api/agents/"+agentID+"/restore", nil), "id", agentID)
	wR := httptest.NewRecorder()
	testHandler.RestoreAgent(wR, reqR)
	assertRedacted(t, wR, "RestoreAgent")
}

func TestGetAgentEnv_WorkspaceOwnerGetsMaskedValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _ := secretVisibilityFixture(t, "envmask", nil, map[string]string{
		"JIRA_PERSONAL_TOKEN": "pat-live-value",
		"OTHER_KEY":           "second-live-value",
	})

	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "pat-live-value") || strings.Contains(body, "second-live-value") {
		t.Fatalf("workspace owner env response leaked another member's values: %s", body)
	}

	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.ValuesMasked {
		t.Error("non-owner response must set values_masked=true")
	}
	want := map[string]string{"JIRA_PERSONAL_TOKEN": envSentinel, "OTHER_KEY": envSentinel}
	if !reflect.DeepEqual(resp.CustomEnv, want) {
		t.Errorf("masked env mismatch: got %v, want %v", resp.CustomEnv, want)
	}

	var action, details string
	if err := testPool.QueryRow(ctx, `
		SELECT action, details::text FROM activity_log
		WHERE workspace_id = $1 AND details->>'agent_id' = $2
		  AND action LIKE 'agent_env_%'
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, agentID).Scan(&action, &details); err != nil {
		t.Fatalf("no agent_env activity row found: %v", err)
	}
	if action == agentEnvActivityRevealed {
		t.Errorf("masked read must not be logged as %q", agentEnvActivityRevealed)
	}
	if action != agentEnvActivityListed {
		t.Errorf("masked read action = %q, want %q", action, agentEnvActivityListed)
	}
	if strings.Contains(details, "revealed_keys") {
		t.Errorf("masked read must not record revealed_keys: %s", details)
	}
	if !strings.Contains(details, `"JIRA_PERSONAL_TOKEN"`) {
		t.Errorf("masked read should still record which key names were listed: %s", details)
	}
	if strings.Contains(details, "pat-live-value") {
		t.Errorf("activity details must never contain env values: %s", details)
	}
}

func TestGetAgentEnv_AgentOwnerWithoutAdminRoleReads(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "ownerread", nil, map[string]string{
		"MY_TOKEN": "my-own-secret",
	})

	req := withURLParam(newRequestAs(ownerUserID, http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv as agent owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ValuesMasked {
		t.Error("the agent owner must not be served a masked response")
	}
	if resp.CustomEnv["MY_TOKEN"] != "my-own-secret" {
		t.Errorf("agent owner must read their own value, got %q", resp.CustomEnv["MY_TOKEN"])
	}
}

func TestGetAgentEnv_PlainMemberOfForeignAgentForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "stranger", nil, map[string]string{"K": "v"})
	strangerID := createSecretVisPlainMember(t, "secret-vis-outsider@goosar.test")

	req := withURLParam(newRequestAs(strangerID, http.MethodGet, "/api/agents/"+agentID+"/env", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an unrelated member, got %d: %s", w.Code, w.Body.String())
	}
}

func createSecretVisPlainMember(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, email, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create plain member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add plain member: %v", err)
	}
	return userID
}

func TestUpdateAgentEnv_WorkspaceOwnerManagesWithoutReading(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _ := secretVisibilityFixture(t, "envwrite", nil, map[string]string{
		"KEEP_ME": "untouched-secret",
		"DROP_ME": "doomed-secret",
	})

	body := map[string]any{"custom_env": map[string]string{
		"KEEP_ME": envSentinel,
	}}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "untouched-secret") {
		t.Fatalf("PUT response handed the preserved secret back to a non-owner: %s", w.Body.String())
	}

	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.ValuesMasked {
		t.Error("PUT response to a non-owner must set values_masked=true")
	}
	wantKeys := map[string]string{"KEEP_ME": envSentinel}
	if !reflect.DeepEqual(resp.CustomEnv, wantKeys) {
		t.Errorf("masked PUT response mismatch: got %v, want %v", resp.CustomEnv, wantKeys)
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back custom_env: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatalf("decode stored custom_env: %v", err)
	}
	want := map[string]string{"KEEP_ME": "untouched-secret"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored custom_env mismatch:\n got:  %v\n want: %v", got, want)
	}
}

func TestUpdateAgentEnv_NonOwnerCannotAddOrReplaceValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	cases := []struct {
		label string
		env   map[string]string
	}{
		{"envadd", map[string]string{"KEEP_ME": envSentinel, "ADD_ME": "injected"}},
		{"envreplace", map[string]string{"KEEP_ME": "attacker-value"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			agentID, _ := secretVisibilityFixture(t, tc.label, nil, map[string]string{
				"KEEP_ME": "untouched-secret",
			})
			body := map[string]any{"custom_env": tc.env}
			req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", body), "id", agentID)
			w := httptest.NewRecorder()
			testHandler.UpdateAgentEnv(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for a non-owner env value write, got %d: %s", w.Code, w.Body.String())
			}
			var stored string
			if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
				t.Fatalf("read back custom_env: %v", err)
			}
			var got map[string]string
			if err := json.Unmarshal([]byte(stored), &got); err != nil {
				t.Fatalf("decode stored custom_env: %v", err)
			}
			want := map[string]string{"KEEP_ME": "untouched-secret"}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("rejected request changed stored env:\n got:  %v\n want: %v", got, want)
			}
		})
	}
}

func TestUpdateAgentEnv_AgentOwnerSeesPlaintextResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, ownerUserID := secretVisibilityFixture(t, "envwriteowner", nil, map[string]string{
		"KEEP_ME": "owner-secret",
	})

	body := map[string]any{"custom_env": map[string]string{
		"KEEP_ME": envSentinel,
		"ADD_ME":  "owner-new",
	}}
	req := withURLParam(newRequestAs(ownerUserID, http.MethodPut, "/api/agents/"+agentID+"/env", body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentEnv as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ValuesMasked {
		t.Error("owner PUT response must not be masked")
	}
	if resp.CustomEnv["KEEP_ME"] != "owner-secret" || resp.CustomEnv["ADD_ME"] != "owner-new" {
		t.Errorf("owner PUT response should carry real values, got %v", resp.CustomEnv)
	}
}

func TestUpdateAgentEnv_RejectsPlaceholderForUnknownKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _ := secretVisibilityFixture(t, "envrename", nil, map[string]string{
		"JIRA_PERSONAL_TOKEN": "live-value",
	})

	body := map[string]any{"custom_env": map[string]string{"JIRA_TOKEN": envSentinel}}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a placeholder under an unknown key, got %d: %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back custom_env: %v", err)
	}
	if !strings.Contains(stored, "live-value") {
		t.Errorf("a rejected request must leave the stored value alone, got %s", stored)
	}
}

func TestUpdateAgentEnv_RejectsPlaceholderPrefixedValue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _ := secretVisibilityFixture(t, "envappend", nil, map[string]string{
		"JIRA_PERSONAL_TOKEN": "live-value",
	})

	body := map[string]any{"custom_env": map[string]string{
		"JIRA_PERSONAL_TOKEN": envSentinel + "rotated-secret",
	}}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a value typed onto the placeholder, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "rotated-secret") {
		t.Fatalf("the error message must not echo the submitted value: %s", w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back custom_env: %v", err)
	}
	if strings.Contains(stored, envSentinel) {
		t.Errorf("a corrupted value reached the database: %s", stored)
	}
}

func TestMutationResponses_AlwaysRedactEnvAppliesToTheAgentOwnerToo(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, ownerUserID := secretVisibilityFixture(t, "kioskmutation", []byte(secretVisMcpConfig), nil)

	var previousSettings []byte
	if err := testPool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previousSettings); err != nil {
		t.Fatalf("load workspace settings: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"always_redact_env": true}'::jsonb WHERE id = $1`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("set always_redact_env: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previousSettings, testWorkspaceID)
	})

	req := withURLParam(newRequestAs(ownerUserID, http.MethodPut, "/api/agents/"+agentID,
		map[string]any{"description": "owner touched this"}), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "member-personal-token") {
		t.Fatalf("kiosk workspace leaked mcp_config through a mutation response: %s", w.Body.String())
	}
	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.McpConfig) > 0 && !bytes.Equal(bytes.TrimSpace(resp.McpConfig), []byte("null")) {
		t.Errorf("kiosk mode must strip the document entirely, got %s", resp.McpConfig)
	}
	if !resp.McpConfigRedacted {
		t.Error("kiosk mutation response must carry mcp_config_redacted=true")
	}
}
