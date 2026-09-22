package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func allowlistFixture(t *testing.T) (agentID, agentOwnerID string) {
	t.Helper()
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Allowlist Owner', 'allowlist-owner@goosar.test')
		RETURNING id
	`).Scan(&agentOwnerID); err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'allowlist-owner@goosar.test'`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, agentOwnerID); err != nil {
		t.Fatalf("add owner as workspace member: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'allowlist-test-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), agentOwnerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID, agentOwnerID
}

func readAllowlistColumn(t *testing.T, agentID string) ([]string, bool) {
	t.Helper()
	var stored []string
	var isNull bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(composio_toolkit_allowlist, '{}'::text[]),
		       composio_toolkit_allowlist IS NULL
		FROM agent WHERE id = $1
	`, agentID).Scan(&stored, &isNull); err != nil {
		t.Fatalf("read allowlist column: %v", err)
	}
	return stored, isNull
}

func TestUpdateAgent_AllowlistRoundtripForOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withComposioMCPAppsFlag(t, testHandler, true)
	agentID, ownerID := allowlistFixture(t)

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequestAs(
		ownerID, http.MethodPut, "/api/agents/"+agentID,
		map[string]any{
			"composio_toolkit_allowlist": []string{" Notion ", "NOTION", "github", ""},
		},
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent owner write: got %d: %s", w.Code, w.Body.String())
	}
	stored, isNull := readAllowlistColumn(t, agentID)
	if isNull {
		t.Fatalf("after owner write, allowlist still NULL")
	}
	if len(stored) != 2 || stored[0] != "notion" || stored[1] != "github" {
		t.Errorf("normalised allowlist = %v; want [notion github]", stored)
	}

	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequestAs(
		ownerID, http.MethodPut, "/api/agents/"+agentID,
		map[string]any{"composio_toolkit_allowlist": []string{}},
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent owner empty write: got %d: %s", w.Code, w.Body.String())
	}
	stored, isNull = readAllowlistColumn(t, agentID)
	if isNull {
		t.Fatalf("empty array must persist as empty TEXT[], not NULL")
	}
	if len(stored) != 0 {
		t.Errorf("after empty write, stored = %v; want empty", stored)
	}

	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequestAs(
		ownerID, http.MethodPut, "/api/agents/"+agentID,
		map[string]any{"composio_toolkit_allowlist": nil},
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent owner null write: got %d: %s", w.Code, w.Body.String())
	}
	_, isNull = readAllowlistColumn(t, agentID)
	if !isNull {
		t.Errorf("explicit null must persist as NULL")
	}
}

func TestUpdateAgent_AllowlistSilentlyDroppedForNonOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withComposioMCPAppsFlag(t, testHandler, true)
	agentID, _ := allowlistFixture(t)

	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET composio_toolkit_allowlist = $2 WHERE id = $1
	`, agentID, []string{"notion"}); err != nil {
		t.Fatalf("seed existing allowlist: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(
		http.MethodPut, "/api/agents/"+agentID,
		map[string]any{

			"description": "admin description sweep",
			"composio_toolkit_allowlist": []string{
				"github", "slack",
			},
		},
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent admin sweep: got %d: %s", w.Code, w.Body.String())
	}

	stored, isNull := readAllowlistColumn(t, agentID)
	if isNull {
		t.Fatalf("allowlist must NOT be wiped by non-owner write; got NULL")
	}
	if len(stored) != 1 || stored[0] != "notion" {
		t.Errorf("non-owner widening landed: allowlist=%v; want unchanged [notion]", stored)
	}

	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ComposioToolkitAllowlist) != 0 {
		t.Errorf("admin response leaked allowlist contents: %v", resp.ComposioToolkitAllowlist)
	}
	if !resp.ComposioToolkitAllowlistRedacted {
		t.Errorf("admin response should mark allowlist redacted, got %+v", resp)
	}
}

func TestGetAgent_AllowlistVisibility(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withComposioMCPAppsFlag(t, testHandler, true)
	agentID, ownerID := allowlistFixture(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET composio_toolkit_allowlist = $2 WHERE id = $1
	`, agentID, []string{"notion", "github"}); err != nil {
		t.Fatalf("seed allowlist: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequestAs(
		ownerID, http.MethodGet, "/api/agents/"+agentID, nil,
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent as owner: got %d: %s", w.Code, w.Body.String())
	}
	var ownerResp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &ownerResp); err != nil {
		t.Fatalf("decode owner response: %v", err)
	}
	if len(ownerResp.ComposioToolkitAllowlist) != 2 {
		t.Errorf("owner: allowlist=%v; want length 2", ownerResp.ComposioToolkitAllowlist)
	}
	if ownerResp.ComposioToolkitAllowlistRedacted {
		t.Errorf("owner should not see redacted flag")
	}

	w = httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequest(
		http.MethodGet, "/api/agents/"+agentID, nil,
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent as ws-owner: got %d: %s", w.Code, w.Body.String())
	}
	var adminResp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &adminResp); err != nil {
		t.Fatalf("decode admin response: %v", err)
	}
	if len(adminResp.ComposioToolkitAllowlist) != 0 {
		t.Errorf("ws-owner response leaked allowlist: %v", adminResp.ComposioToolkitAllowlist)
	}
	if !adminResp.ComposioToolkitAllowlistRedacted {
		t.Errorf("ws-owner response should mark allowlist redacted")
	}
}

func TestAgentAllowlistSuppressedWhenComposioFlagDisabled(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withComposioMCPAppsFlag(t, testHandler, false)
	agentID, ownerID := allowlistFixture(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET composio_toolkit_allowlist = $2 WHERE id = $1
	`, agentID, []string{"notion"}); err != nil {
		t.Fatalf("seed allowlist: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequestAs(
		ownerID, http.MethodPut, "/api/agents/"+agentID,
		map[string]any{"composio_toolkit_allowlist": []string{"github"}},
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent owner write with flag off: got %d: %s", w.Code, w.Body.String())
	}
	stored, isNull := readAllowlistColumn(t, agentID)
	if isNull || len(stored) != 1 || stored[0] != "notion" {
		t.Fatalf("flag-off write changed allowlist: stored=%v isNull=%v; want unchanged [notion]", stored, isNull)
	}
	var updateResp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if len(updateResp.ComposioToolkitAllowlist) != 0 || updateResp.ComposioToolkitAllowlistRedacted {
		t.Fatalf("flag-off update response exposed allowlist state: %+v", updateResp)
	}

	w = httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequestAs(
		ownerID, http.MethodGet, "/api/agents/"+agentID, nil,
	), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent owner with flag off: got %d: %s", w.Code, w.Body.String())
	}
	var getResp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if len(getResp.ComposioToolkitAllowlist) != 0 || getResp.ComposioToolkitAllowlistRedacted {
		t.Fatalf("flag-off get response exposed allowlist state: %+v", getResp)
	}
}

func TestNormaliseComposioToolkitAllowlist_PureFunction(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil-in nil-out", nil, nil},
		{"empty-in empty-out", []string{}, []string{}},
		{"trim", []string{"  notion  "}, []string{"notion"}},
		{"lower", []string{"NOTION", "GitHub"}, []string{"notion", "github"}},
		{"dedupe", []string{"notion", "NOTION", "notion"}, []string{"notion"}},
		{"drop empty", []string{"", "   ", "notion"}, []string{"notion"}},
		{"preserve order of first-seen", []string{"notion", "github", "notion"}, []string{"notion", "github"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normaliseComposioToolkitAllowlist(tc.in)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v; want nil", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v; want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d]=%q; want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
