package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

const roleAgentTestTemplateKey = "test485"

func roleAgentTemplateFixture(t *testing.T, enabled bool) {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, roleAgentTestTemplateKey)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, description, pins, mcp_defaults, helper, position, enabled)
		VALUES ($1,
		        '{"ru": "Тест", "en": "Test"}',
		        '{"ru": "Тестовая роль", "en": "Test role"}',
		        '[]', '{}',
		        '{"extra_instructions": {"ru": "МЕТОДИКА-РОЛИ-485", "en": "ROLE-METHOD-485"}}',
		        9485, $2)
	`, roleAgentTestTemplateKey, enabled); err != nil {
		t.Fatalf("insert template fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, roleAgentTestTemplateKey)
	})
}

func roleAgentTestWorkspace(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix, template_key)
		VALUES ($1, $2, 'role agent test', 'ROL', $3)
		RETURNING id
	`, "Role "+slug, slug, roleAgentTestTemplateKey).Scan(&wsID); err != nil {
		t.Fatalf("create role workspace %s: %v", slug, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, testUserID,
	); err != nil {
		t.Fatalf("add owner to %s: %v", slug, err)
	}
	return wsID
}

func roleAgentRows(t *testing.T, wsID string) []helperAgentRow {
	t.Helper()

	rows, err := testPool.Query(context.Background(), `
		SELECT id, name, kind, COALESCE(system_key, ''), status, visibility,
		       permission_mode, max_concurrent_tasks, COALESCE(avatar_url, ''),
		       description, instructions, runtime_id, COALESCE(owner_id::text, '')
		FROM agent
		WHERE workspace_id = $1 AND system_key LIKE 'goosar_role_%'
		ORDER BY created_at ASC
	`, wsID)
	if err != nil {
		t.Fatalf("query role agents: %v", err)
	}
	defer rows.Close()

	var out []helperAgentRow
	for rows.Next() {
		var r helperAgentRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.SystemKey, &r.Status, &r.Visibility,
			&r.PermissionMode, &r.MaxConcurrentTasks, &r.AvatarURL, &r.Description,
			&r.Instructions, &r.RuntimeID, &r.OwnerID); err != nil {
			t.Fatalf("scan role agent: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate role agents: %v", err)
	}
	return out
}

func theRoleAgent(t *testing.T, wsID string) helperAgentRow {
	t.Helper()
	rows := roleAgentRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one role agent, got %d", len(rows))
	}
	return rows[0]
}

func countWorkspaceInvocationTargets(t *testing.T, agentID, targetType, targetID string) int {
	t.Helper()
	var total int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_invocation_target
		WHERE agent_id = $1 AND target_type = $2 AND target_id = $3
	`, agentID, targetType, targetID).Scan(&total); err != nil {
		t.Fatalf("count invocation targets: %v", err)
	}
	return total
}

func TestRoleAgent_CreatedOnFirstPublicRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-first-public-runtime")
	if rows := roleAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("precondition: workspace must start without a role agent, got %d", len(rows))
	}

	runtimeID := helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})

	listAgentsRequest(t, testUserID, wsID)

	agent := theRoleAgent(t, wsID)
	if agent.SystemKey != "goosar_role_"+roleAgentTestTemplateKey {
		t.Errorf("system_key = %q, want goosar_role_%s", agent.SystemKey, roleAgentTestTemplateKey)
	}

	if agent.Kind != "user" {
		t.Errorf("kind = %q, want user — kind 'system' would hide the agent from every list", agent.Kind)
	}
	if agent.OwnerID != "" {
		t.Errorf("owner_id = %q, want NULL: the role agent belongs to the workspace, not to a member", agent.OwnerID)
	}
	if agent.Visibility != "workspace" || agent.PermissionMode != "public_to" {
		t.Errorf("visibility/permission_mode = %q/%q, want workspace/public_to", agent.Visibility, agent.PermissionMode)
	}
	if agent.RuntimeID != runtimeID {
		t.Errorf("runtime_id = %q, want the published runtime %q", agent.RuntimeID, runtimeID)
	}
	if agent.MaxConcurrentTasks != roleAgentMaxConcurrentTasks {
		t.Errorf("max_concurrent_tasks = %d, want %d", agent.MaxConcurrentTasks, roleAgentMaxConcurrentTasks)
	}
	if agent.AvatarURL != helperAgentAvatarURL {
		t.Errorf("avatar_url = %q, want the built-in Hermes mark", agent.AvatarURL)
	}

	if agent.Name != "Агент Тест" {
		t.Errorf("name = %q, want %q derived from the template display name", agent.Name, "Агент Тест")
	}

	if n := countWorkspaceInvocationTargets(t, agent.ID, "workspace", wsID); n != 1 {
		t.Errorf("workspace invocation targets = %d, want 1", n)
	}
}

func TestRoleAgent_PrivateRuntimeProvisionsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-private-runtime")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "private hermes", provider: "runtime-j", status: "online", visibility: "private",
	})

	listAgentsRequest(t, testUserID, wsID)

	if rows := roleAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("a private runtime must not produce a role agent, got %d", len(rows))
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_runtime SET visibility = 'public' WHERE workspace_id = $1`, wsID); err != nil {
		t.Fatalf("publish runtime: %v", err)
	}
	listAgentsRequest(t, testUserID, wsID)
	theRoleAgent(t, wsID)
}

func TestRoleAgent_IsIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-idempotent")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})

	for i := 0; i < 3; i++ {
		listAgentsRequest(t, testUserID, wsID)
	}
	first := theRoleAgent(t, wsID)

	registerDaemonAs(t, testUserID, wsID, "role-agent-idempotent-daemon", "runtime-j")
	if again := theRoleAgent(t, wsID); again.ID != first.ID {
		t.Fatalf("a second entry point created a second role agent: %q then %q", first.ID, again.ID)
	}
}

func TestRoleAgent_OrdinaryMemberSeesItAndItsHelperIsUntouched(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-member-view")
	memberID := helperTestMember(t, wsID, "role-agent-member@example.test", "Роль Участник")

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "member public hermes", provider: "runtime-j", status: "online",
		visibility: "public", ownerID: memberID,
	})

	agents := listAgentsRequest(t, memberID, wsID)

	role := theRoleAgent(t, wsID)
	var sawRole bool
	for _, a := range agents {
		if a.ID == role.ID {
			sawRole = true
		}
	}
	if !sawRole {
		t.Errorf("an ordinary member does not see the shared role agent in ListAgents")
	}

	helper := helperRowFor(t, wsID, memberID)
	if helper.ID == role.ID {
		t.Fatalf("the role agent was returned as the member's Helper")
	}
	if helper.SystemKey != helperAgentSystemKey {
		t.Errorf("Helper system_key = %q, want %q", helper.SystemKey, helperAgentSystemKey)
	}
}

func TestRoleAgent_InstructionAssemblyOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-instructions")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)

	got := theRoleAgent(t, wsID).Instructions

	builtin := helperInstructionsByLang[helperDefaultContentLang]
	method := roleAgentMethodByLang[helperDefaultContentLang]
	const roleText = "МЕТОДИКА-РОЛИ-485"

	want := builtin + "\n\n" + method + "\n\n" + roleText
	if got != want {
		iBuiltin := strings.Index(got, builtin)
		iMethod := strings.Index(got, method)
		iRole := strings.Index(got, roleText)
		t.Fatalf("instructions are not built-in → method → template role text (offsets %d/%d/%d)", iBuiltin, iMethod, iRole)
	}
}

func TestRoleAgentInstructions_TemplateWithoutRoleTextStillGetsTheMethod(t *testing.T) {
	lang := helperDefaultContentLang
	got := roleAgentInstructions(lang, workspaceTemplateHelper{})
	want := helperInstructionsByLang[lang] + "\n\n" + roleAgentMethodByLang[lang]
	if got != want {
		t.Fatalf("instructions without template text = %d bytes, want the built-in prompt plus the method", len(got))
	}
}

func archiveAgentAs(t *testing.T, userID, wsID, agentID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/archive", nil)
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", wsID)
	w := httptest.NewRecorder()
	testHandler.ArchiveAgent(w, withURLParam(req, "id", agentID))
	return w
}

func TestRoleAgent_OnlyADeploymentAdminMayArchiveIt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-archive")
	memberID := helperTestMember(t, wsID, "role-agent-archiver@example.test", "Роль Удалятель")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	if w := archiveAgentAs(t, memberID, wsID, agent.ID); w.Code != http.StatusForbidden {
		t.Errorf("a plain member archiving the role agent: got %d, want 403 (%s)", w.Code, w.Body.String())
	}

	if w := archiveAgentAs(t, testUserID, wsID, agent.ID); w.Code != http.StatusForbidden {
		t.Errorf("a workspace owner who is not a deployment administrator: got %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if rows := roleAgentRows(t, wsID); len(rows) != 1 || rows[0].ID != agent.ID {
		t.Fatalf("the refused archives changed the row anyway")
	}

	grantDeploymentAdminFixture(t, testUserID)
	if w := archiveAgentAs(t, testUserID, wsID, agent.ID); w.Code != http.StatusOK {
		t.Fatalf("a deployment administrator archiving the role agent: got %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestRoleAgent_ArchivedByAnAdminIsNotResurrected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-no-resurrection")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	grantDeploymentAdminFixture(t, testUserID)
	if w := archiveAgentAs(t, testUserID, wsID, agent.ID); w.Code != http.StatusOK {
		t.Fatalf("archive: got %d (%s)", w.Code, w.Body.String())
	}

	listAgentsRequest(t, testUserID, wsID)
	registerDaemonAs(t, testUserID, wsID, "role-agent-no-resurrection-daemon", "runtime-j")

	rows := roleAgentRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("provisioning resurrected the archived role agent: %d rows, want the archived one alone", len(rows))
	}
	if rows[0].ID != agent.ID {
		t.Fatalf("a second role agent replaced the archived one")
	}
}

func TestRoleAgent_DisabledTemplateProvisionsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, false)

	wsID := roleAgentTestWorkspace(t, "role-agent-disabled-template")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)

	if rows := roleAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("a disabled template must provision no role agent, got %d", len(rows))
	}
}

func TestRoleAgent_PlainWorkspaceGetsNone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := helperTestWorkspace(t, "role-agent-plain-workspace")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)

	if rows := roleAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("a workspace without template_key must get no role agent, got %d", len(rows))
	}
}

func TestRoleAgent_OnlyADeploymentAdminMayConfigureIt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-config")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	mcp := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agent.ID+"/mcp-servers",
			strings.NewReader(`{"server_id":"`+agent.ID+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", wsID)
		w := httptest.NewRecorder()
		testHandler.AddAgentMcpServer(w, withURLParam(req, "id", agent.ID))
		return w
	}

	env := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/agents/"+agent.ID+"/env",
			strings.NewReader(`{"custom_env":{"ROLE_AGENT_TEST":"x"}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", wsID)
		w := httptest.NewRecorder()
		testHandler.UpdateAgentEnv(w, withURLParam(req, "id", agent.ID))
		return w
	}

	if w := mcp(); w.Code != http.StatusForbidden {
		t.Errorf("a workspace owner attaching an MCP server to the role agent: got %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if w := env(); w.Code != http.StatusForbidden {
		t.Errorf("a workspace owner rewriting the role agent's env: got %d, want 403 (%s)", w.Code, w.Body.String())
	}

	grantDeploymentAdminFixture(t, testUserID)
	if w := mcp(); w.Code == http.StatusForbidden {
		t.Errorf("a deployment administrator must pass the role-agent gate, got 403 (%s)", w.Body.String())
	}
	if w := env(); w.Code != http.StatusForbidden {
		t.Errorf("an owner-less agent must refuse env values from everybody: got %d, want 403 (%s)", w.Code, w.Body.String())
	}
}

func TestRoleAgent_RevokingItsHostReleasesTheSlot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)

	wsID := roleAgentTestWorkspace(t, "role-agent-revoke-host")
	hostID := helperTestMember(t, wsID, "role-agent-host@example.test", "Роль Хозяин")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "host public hermes", provider: "runtime-j", status: "online",
		visibility: "public", ownerID: hostID,
	})
	listAgentsRequest(t, testUserID, wsID)
	first := theRoleAgent(t, wsID)

	var memberRowID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, wsID, hostID,
	).Scan(&memberRowID); err != nil {
		t.Fatalf("load member row: %v", err)
	}
	if _, err := testHandler.revokeAndRemoveMember(context.Background(),
		util.MustParseUUID(wsID), util.MustParseUUID(hostID),
		util.MustParseUUID(memberRowID), util.MustParseUUID(testUserID),
	); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	if rows := roleAgentRows(t, wsID); len(rows) != 0 {
		t.Fatalf("the archived host agent still carries a role system_key: %d row(s)", len(rows))
	}

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "replacement public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	again := theRoleAgent(t, wsID)
	if again.ID == first.ID {
		t.Fatalf("expected a NEW role agent after revocation, got the archived one back")
	}
}
