package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

func createPermissionTestMember(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create member user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

func TestCreateAgent_LegacyVisibilityMapsToPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	create := func(name, visibility string) AgentResponse {
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name":       name,
			"runtime_id": runtimeID,
			"visibility": visibility,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q (visibility=%s): expected 201, got %d: %s", name, visibility, w.Code, w.Body.String())
		}
		var resp AgentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
		return resp
	}

	ws := create("legacy-visibility-workspace", "workspace")
	if ws.PermissionMode != "public_to" {
		t.Errorf("workspace agent permission_mode = %q, want public_to", ws.PermissionMode)
	}
	if ws.Visibility != "workspace" {
		t.Errorf("workspace agent derived visibility = %q, want workspace", ws.Visibility)
	}
	foundWorkspaceTarget := false
	for _, tgt := range ws.InvocationTargets {
		if tgt.TargetType == "workspace" {
			foundWorkspaceTarget = true
		}
	}
	if !foundWorkspaceTarget {
		t.Errorf("workspace agent invocation_targets = %+v, want a workspace target", ws.InvocationTargets)
	}

	priv := create("legacy-visibility-private", "private")
	if priv.PermissionMode != "private" {
		t.Errorf("private agent permission_mode = %q, want private", priv.PermissionMode)
	}
	if priv.Visibility != "private" {
		t.Errorf("private agent derived visibility = %q, want private", priv.Visibility)
	}
	if len(priv.InvocationTargets) != 0 {
		t.Errorf("private agent invocation_targets = %+v, want none", priv.InvocationTargets)
	}
}

func TestMigrationBackfill_VisibilityToPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'backfill-legacy-workspace-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert pre-migration agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'public_to' WHERE visibility = 'workspace'`); err != nil {
		t.Fatalf("backfill update: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		SELECT id, 'workspace', workspace_id, NULL FROM agent WHERE visibility = 'workspace'
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`); err != nil {
		t.Fatalf("backfill insert targets: %v", err)
	}

	var mode string
	if err := testPool.QueryRow(ctx, `SELECT permission_mode FROM agent WHERE id = $1`, agentID).Scan(&mode); err != nil {
		t.Fatalf("read permission_mode: %v", err)
	}
	if mode != "public_to" {
		t.Errorf("after backfill permission_mode = %q, want public_to", mode)
	}
	var targetCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_invocation_target
		WHERE agent_id = $1 AND target_type = 'workspace' AND target_id = $2
	`, agentID, testWorkspaceID).Scan(&targetCount); err != nil {
		t.Fatalf("count targets: %v", err)
	}
	if targetCount != 1 {
		t.Errorf("workspace target count = %d, want 1", targetCount)
	}
}

func TestCanInvokeAgent_PublicToMemberWhitelist(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	allowedMember := createPermissionTestMember(t, "perm-allowed-member@goosar.test")
	otherMember := createPermissionTestMember(t, "perm-other-member@goosar.test")

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":            "public-to-specific-member-agent",
		"runtime_id":      runtimeID,
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": allowedMember},
		},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var agent AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agent.ID) })

	if agent.Visibility != "private" {
		t.Errorf("member-only public_to derived visibility = %q, want private", agent.Visibility)
	}

	assignAs := func(actorID string) int {
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, newRequestAs(actorID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         "assign to member-scoped agent",
			"status":        "todo",
			"assignee_type": "agent",
			"assignee_id":   agent.ID,
		}))
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent.ID)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = 'assign to member-scoped agent'`, testWorkspaceID)
		})
		return rec.Code
	}

	if code := assignAs(allowedMember); code != http.StatusCreated {
		t.Errorf("allow-listed member assign: expected 201, got %d", code)
	}
	if code := assignAs(otherMember); code != http.StatusForbidden {
		t.Errorf("non-allow-listed member assign: expected 403, got %d", code)
	}
}

func createPublicToAgentWithTargets(t *testing.T, name string, targets []map[string]any) string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":               name,
		"runtime_id":         handlerTestRuntimeID(t),
		"permission_mode":    "public_to",
		"invocation_targets": targets,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
	return resp.ID
}

func canMemberInvoke(t *testing.T, agentID, userID string) bool {
	t.Helper()
	agent, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	return testHandler.canInvokeAgent(context.Background(), agent, "member", userID, scopedInvokeAuthority(userID), testWorkspaceID)
}

func invocationTargetCount(t *testing.T, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_invocation_target WHERE agent_id = $1`, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count targets: %v", err)
	}
	return n
}

func TestCanInvokeAgent_MixedMemberAndTeamTargets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberA := createPermissionTestMember(t, "perm-mix-a@goosar.test")
	memberB := createPermissionTestMember(t, "perm-mix-b@goosar.test")
	teamID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	agentID := createPublicToAgentWithTargets(t, "mixed-member-team-agent", []map[string]any{
		{"target_type": "member", "target_id": memberA},
		{"target_type": "team", "target_id": teamID},
	})

	if n := invocationTargetCount(t, agentID); n != 2 {
		t.Errorf("expected 2 mixed targets persisted, got %d", n)
	}
	if !canMemberInvoke(t, agentID, memberA) {
		t.Errorf("member A (on member target) should be able to invoke")
	}
	if canMemberInvoke(t, agentID, memberB) {
		t.Errorf("member B should NOT invoke — only a (inert) team target applies to them")
	}
}

func TestUpdateAgent_BatchReplaceOverlappingMembers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberA := createPermissionTestMember(t, "perm-batch-a@goosar.test")
	memberB := createPermissionTestMember(t, "perm-batch-b@goosar.test")
	memberC := createPermissionTestMember(t, "perm-batch-c@goosar.test")

	agentID := createPublicToAgentWithTargets(t, "batch-replace-agent", []map[string]any{
		{"target_type": "member", "target_id": memberA},
		{"target_type": "member", "target_id": memberB},
	})
	if !canMemberInvoke(t, agentID, memberA) || !canMemberInvoke(t, agentID, memberB) {
		t.Fatalf("initial: A and B should both invoke")
	}
	if canMemberInvoke(t, agentID, memberC) {
		t.Fatalf("initial: C should not invoke")
	}

	w := httptest.NewRecorder()
	r := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": memberB},
			{"target_type": "member", "target_id": memberC},
		},
	})
	r = withURLParam(r, "id", agentID)
	testHandler.UpdateAgent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := invocationTargetCount(t, agentID); n != 2 {
		t.Errorf("after batch replace expected exactly 2 targets, got %d (stale rows not cleared?)", n)
	}
	if canMemberInvoke(t, agentID, memberA) {
		t.Errorf("A was removed and must no longer invoke")
	}
	if !canMemberInvoke(t, agentID, memberB) {
		t.Errorf("B overlapped both sets and must still invoke")
	}
	if !canMemberInvoke(t, agentID, memberC) {
		t.Errorf("C was added and must now invoke")
	}
}

func TestUpdateAgent_WorkspaceStacksWithMembersThenNarrowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberA := createPermissionTestMember(t, "perm-stack-a@goosar.test")
	memberB := createPermissionTestMember(t, "perm-stack-b@goosar.test")

	agentID := createPublicToAgentWithTargets(t, "workspace-plus-member-agent", []map[string]any{
		{"target_type": "workspace"},
		{"target_type": "member", "target_id": memberA},
	})

	if !canMemberInvoke(t, agentID, memberA) || !canMemberInvoke(t, agentID, memberB) {
		t.Fatalf("workspace target should admit any member (A and B)")
	}

	memberC := createPermissionTestMember(t, "perm-stack-c@goosar.test")
	w := httptest.NewRecorder()
	r := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": memberC},
		},
	})
	r = withURLParam(r, "id", agentID)
	testHandler.UpdateAgent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("narrow update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if canMemberInvoke(t, agentID, memberB) {
		t.Errorf("after dropping workspace target, non-listed member B must lose access")
	}
	if !canMemberInvoke(t, agentID, memberC) {
		t.Errorf("member C must have access after the replace")
	}
}

func TestCreateAgent_EmptyPublicToNormalizesToWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":            "empty-public-to-agent",
		"runtime_id":      handlerTestRuntimeID(t),
		"permission_mode": "public_to",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })

	if resp.PermissionMode != "public_to" {
		t.Errorf("permission_mode = %q, want public_to", resp.PermissionMode)
	}
	if resp.Visibility != "workspace" {
		t.Errorf("empty public_to should derive visibility=workspace, got %q", resp.Visibility)
	}
	foundWorkspace := false
	for _, tgt := range resp.InvocationTargets {
		if tgt.TargetType == "workspace" {
			foundWorkspace = true
		}
	}
	if !foundWorkspace {
		t.Errorf("empty public_to must normalise to a workspace target, got %+v", resp.InvocationTargets)
	}

	someMember := createPermissionTestMember(t, "perm-emptypublic-m@goosar.test")
	if !canMemberInvoke(t, resp.ID, someMember) {
		t.Errorf("a workspace member should be able to invoke the normalised public_to-workspace agent")
	}
}

func TestCanInvokeAgent_SystemWorkspaceExceptionAndMemberFailClosed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wsAgentID := createPublicToAgentWithTargets(t, "sys-exception-workspace-agent", []map[string]any{
		{"target_type": "workspace"},
	})
	wsAgent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(wsAgentID))
	if err != nil {
		t.Fatalf("load ws agent: %v", err)
	}
	if !testHandler.canInvokeAgent(ctx, wsAgent, "system", "", invokeAuthority{}, testWorkspaceID) {
		t.Errorf("system trigger should hit a workspace target (product-approved exception)")
	}
	if !testHandler.canInvokeAgent(ctx, wsAgent, "agent", "", invokeAuthority{}, testWorkspaceID) {
		t.Errorf("agent trigger with no originator should still hit a workspace target")
	}

	memberX := createPermissionTestMember(t, "perm-sys-failclosed@goosar.test")
	memAgentID := createPublicToAgentWithTargets(t, "sys-exception-member-agent", []map[string]any{
		{"target_type": "member", "target_id": memberX},
	})
	memAgent, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(memAgentID))
	if err != nil {
		t.Fatalf("load member agent: %v", err)
	}
	if testHandler.canInvokeAgent(ctx, memAgent, "system", "", invokeAuthority{}, testWorkspaceID) {
		t.Errorf("system trigger must FAIL CLOSED against a member target with no originator")
	}
	if testHandler.canInvokeAgent(ctx, memAgent, "agent", "", invokeAuthority{}, testWorkspaceID) {
		t.Errorf("agent trigger with no originator must FAIL CLOSED against a member target")
	}

	if !testHandler.canInvokeAgent(ctx, memAgent, "agent", "", scopedInvokeAuthority(memberX), testWorkspaceID) {
		t.Errorf("agent trigger whose originator IS the targeted member should be admitted")
	}

	if testHandler.canInvokeAgent(ctx, memAgent, "agent", "", unscopedInvokeAuthority(memberX), testWorkspaceID) {
		t.Errorf("agent trigger with an UNSCOPED originator must FAIL CLOSED against a member target (#133)")
	}
}

func TestRevokeMember_ClearsInvocationTargets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	memberX := createPermissionTestMember(t, "perm-revoke-x@goosar.test")
	agentID := createPublicToAgentWithTargets(t, "revoke-member-target-agent", []map[string]any{
		{"target_type": "member", "target_id": memberX},
	})
	if !canMemberInvoke(t, agentID, memberX) {
		t.Fatalf("member X should be able to invoke before removal")
	}

	var memberRowID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, memberX,
	).Scan(&memberRowID); err != nil {
		t.Fatalf("load member row: %v", err)
	}

	if _, err := testHandler.revokeAndRemoveMember(ctx,
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(memberX),
		util.MustParseUUID(memberRowID),
		util.MustParseUUID(testUserID),
	); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	if n := invocationTargetCount(t, agentID); n != 0 {
		t.Errorf("member target should be pruned on removal, still have %d", n)
	}
	if canMemberInvoke(t, agentID, memberX) {
		t.Errorf("removed member must no longer invoke the agent")
	}

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberX,
	); err != nil {
		t.Fatalf("re-invite member: %v", err)
	}
	if canMemberInvoke(t, agentID, memberX) {
		t.Errorf("re-invited member must NOT reclaim the stale invocation grant")
	}
}

func TestRevokeMember_InvocationTargetCleanupIsWorkspaceScoped(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	userX := createPermissionTestMember(t, "perm-xws@goosar.test")

	agentA := createPublicToAgentWithTargets(t, "xws-agent-a", []map[string]any{
		{"target_type": "member", "target_id": userX},
	})

	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'xws-b-perm-test'`)
	var wsB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('XWS B', 'xws-b-perm-test', '', 'XWB')
		RETURNING id
	`).Scan(&wsB); err != nil {
		t.Fatalf("create workspace B: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsB) })

	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsB, testUserID); err != nil {
		t.Fatalf("add owner to B: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, wsB, userX); err != nil {
		t.Fatalf("add userX to B: %v", err)
	}
	var rtB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'xws-b-rt', 'cloud', 'handler_test_runtime', 'online', 'dev', '{}'::jsonb, $2, now())
		RETURNING id
	`, wsB, testUserID).Scan(&rtB); err != nil {
		t.Fatalf("create runtime B: %v", err)
	}
	var agentB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'xws-agent-b', '', 'cloud', '{}'::jsonb, $2, 'private', 'public_to', 1, $3)
		RETURNING id
	`, wsB, rtB, testUserID).Scan(&agentB); err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'member', $2)
	`, agentB, userX); err != nil {
		t.Fatalf("seed B member target: %v", err)
	}

	if invocationTargetCount(t, agentA) != 1 || invocationTargetCount(t, agentB) != 1 {
		t.Fatalf("setup: expected one member target on each of A and B")
	}

	var memberRowA string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userX,
	).Scan(&memberRowA); err != nil {
		t.Fatalf("load member row A: %v", err)
	}
	if _, err := testHandler.revokeAndRemoveMember(ctx,
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(userX),
		util.MustParseUUID(memberRowA),
		util.MustParseUUID(testUserID),
	); err != nil {
		t.Fatalf("revokeAndRemoveMember(A): %v", err)
	}

	if n := invocationTargetCount(t, agentA); n != 0 {
		t.Errorf("workspace A target should be pruned on removal, got %d", n)
	}
	if n := invocationTargetCount(t, agentB); n != 1 {
		t.Errorf("workspace B target MUST survive removal from A (cross-workspace collateral), got %d", n)
	}
}

func createPermissionTestAdmin(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create admin user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add admin %s: %v", email, err)
	}
	return userID
}

func TestUpdateAgent_AccessChangeIsOwnerOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "owner-only-access-agent", nil)
	adminID := createPermissionTestAdmin(t, "perm-access-admin@goosar.test")

	put := func(actorID string, body map[string]any) int {
		rec := httptest.NewRecorder()
		r := newRequestAs(actorID, "PUT", "/api/agents/"+agentID, body)
		r = withURLParam(r, "id", agentID)
		testHandler.UpdateAgent(rec, r)
		return rec.Code
	}

	rec := httptest.NewRecorder()
	r := newRequestAs(adminID, "PUT", "/api/agents/"+agentID, map[string]any{"permission_mode": "private"})
	r = withURLParam(r, "id", agentID)
	testHandler.UpdateAgent(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin access change: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.PermissionMode != "public_to" {
		t.Errorf("access must be unchanged after rejected admin write, got %q", a.PermissionMode)
	}

	if code := put(adminID, map[string]any{
		"permission_mode":    "public_to",
		"invocation_targets": []map[string]any{{"target_type": "workspace"}},
	}); code != http.StatusOK {
		t.Errorf("admin no-op permission resubmit: expected 200, got %d", code)
	}

	if code := put(adminID, map[string]any{"description": "renamed by admin"}); code != http.StatusOK {
		t.Errorf("admin editing other fields: expected 200, got %d", code)
	}

	if code := put(testUserID, map[string]any{"permission_mode": "private"}); code != http.StatusOK {
		t.Errorf("owner access change: expected 200, got %d", code)
	}
	if n := invocationTargetCount(t, agentID); n != 0 {
		t.Errorf("owner set private: expected 0 targets, got %d", n)
	}
}

func TestUpdateAgent_LegacyVisibilityNoOpForMemberOnlyPublicTo(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	memberX := createPermissionTestMember(t, "perm-legacyvis-x@goosar.test")
	agentID := createPublicToAgentWithTargets(t, "legacy-vis-member-only-agent", []map[string]any{
		{"target_type": "member", "target_id": memberX},
	})
	adminID := createPermissionTestAdmin(t, "perm-legacyvis-admin@goosar.test")

	put := func(actorID string, body map[string]any) int {
		rec := httptest.NewRecorder()
		r := newRequestAs(actorID, "PUT", "/api/agents/"+agentID, body)
		r = withURLParam(r, "id", agentID)
		testHandler.UpdateAgent(rec, r)
		return rec.Code
	}

	if code := put(adminID, map[string]any{"visibility": "private", "description": "admin note"}); code != http.StatusOK {
		t.Fatalf("admin legacy visibility=private no-op: expected 200, got %d", code)
	}

	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.PermissionMode != "public_to" {
		t.Errorf("permission_mode must stay public_to after legacy no-op, got %q", a.PermissionMode)
	}
	if n := invocationTargetCount(t, agentID); n != 1 {
		t.Errorf("member target must be intact after legacy no-op, got %d targets", n)
	}

	if code := put(adminID, map[string]any{"visibility": "workspace"}); code != http.StatusForbidden {
		t.Errorf("admin legacy visibility=workspace (real change): expected 403, got %d", code)
	}
}
