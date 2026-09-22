package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {

	reserved := make([]string, 0, len(reservedSlugs))
	for slug := range reservedSlugs {
		reserved = append(reserved, slug)
	}
	sort.Strings(reserved)

	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			testHandler.CreateWorkspace(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateWorkspace_DoesNotMarkOnboarded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-tests-onboarded-null"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	_, _ = testPool.Exec(ctx, `UPDATE "user" SET onboarded_at = NULL WHERE id = $1`, testUserID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
		_, _ = testPool.Exec(context.Background(), `UPDATE "user" SET onboarded_at = NULL WHERE id = $1`, testUserID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Onboarding Invariant Probe",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var onboardedAt *string
	if err := testPool.QueryRow(ctx, `SELECT onboarded_at FROM "user" WHERE id = $1`, testUserID).Scan(&onboardedAt); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if onboardedAt != nil {
		t.Fatalf("CreateWorkspace marked user as onboarded; expected NULL, got %q. The workspace layout hard gate relies on this staying NULL until Step 3 CompleteOnboarding fires.", *onboardedAt)
	}
}

func TestCreateWorkspace_DisabledByConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const slug = "handler-tests-disabled-create"
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
	})

	prev := testHandler.cfg
	testHandler.cfg = Config{
		AllowSignup:              prev.AllowSignup,
		DisableWorkspaceCreation: true,
	}
	t.Cleanup(func() { testHandler.cfg = prev })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Disabled Create",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateWorkspace: expected 403 with flag on, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, slug).Scan(&count); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no workspace row to be written when gate fires, found %d", count)
	}
}

func TestDeleteWorkspace_RequiresOwner(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-403"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete 403", slug, "DeleteWorkspace handler permission test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'admin')
`, wsID, testUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from DeleteWorkspace handler for admin (non-owner), got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if !exists {
		t.Fatal("workspace was deleted despite non-owner request — handler-level check did not fire")
	}
}

func TestDeleteWorkspace_OwnerSucceeds(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-ok"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete OK", slug, "DeleteWorkspace handler owner test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO github_pending_check_suite (
	workspace_id, installation_id, repo_owner, repo_name, pr_number,
	suite_id, head_sha, app_id, status, suite_updated_at
)
VALUES ($1, 123456789, 'adanman', 'goosar', 3366, 987654321, 'abc123', 15368, 'completed', now())
`, wsID); err != nil {
		t.Fatalf("create pending check suite: %v", err)
	}
	var githubPRID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO github_pull_request (
	workspace_id, installation_id, repo_owner, repo_name, pr_number,
	title, state, html_url, pr_created_at, pr_updated_at, head_sha
)
VALUES ($1, 123456789, 'adanman', 'goosar', 5265,
	'Workspace cleanup snapshot', 'open', 'https://github.com/adanman/goosar/pull/5265',
	now(), now(), 'head-a')
RETURNING id
`, wsID).Scan(&githubPRID); err != nil {
		t.Fatalf("create github PR snapshot parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO github_pull_request_check_run (
	pr_id, head_sha, ordinal, name, status, conclusion, is_status_context
)
VALUES ($1, 'head-a', 0, 'backend', 'completed', 'success', false)
`, githubPRID); err != nil {
		t.Fatalf("create github PR check run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id = $1`, githubPRID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, githubPRID)
	})
	var propertyID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue_property (workspace_id, name, type)
VALUES ($1, 'Delete cleanup property', 'text')
RETURNING id
`, wsID).Scan(&propertyID); err != nil {
		t.Fatalf("create issue property: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, propertyID)
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from DeleteWorkspace handler for owner, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after owner DELETE")
	}

	var pendingCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM github_pending_check_suite WHERE workspace_id = $1`, wsID).Scan(&pendingCount); err != nil {
		t.Fatalf("verify pending check suites: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending check suites were not cleaned up for deleted workspace: %d", pendingCount)
	}

	var checkRunCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM github_pull_request_check_run WHERE pr_id = $1`, githubPRID).Scan(&checkRunCount); err != nil {
		t.Fatalf("verify github PR check-run cleanup: %v", err)
	}
	if checkRunCount != 0 {
		t.Fatalf("github PR check runs were not cleaned up for deleted workspace: %d", checkRunCount)
	}

	var propertyCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue_property WHERE id = $1`, propertyID).Scan(&propertyCount); err != nil {
		t.Fatalf("verify issue property cleanup: %v", err)
	}
	if propertyCount != 0 {
		t.Fatalf("issue properties were not cleaned up for deleted workspace: %d", propertyCount)
	}
}

func TestUpdateWorkspace_AvatarURL(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-avatar-url"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Avatar URL", slug, "UpdateWorkspace avatar_url test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	const avatarURL = "https://cdn.example.com/workspaces/abc/logo.png"

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
		"avatar_url": avatarURL,
	})
	req = withURLParam(req, "id", wsID)
	testHandler.UpdateWorkspace(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from UpdateWorkspace, got %d: %s", w.Code, w.Body.String())
	}

	var resp WorkspaceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AvatarURL == nil || *resp.AvatarURL != avatarURL {
		t.Fatalf("expected avatar_url %q in response, got %v", avatarURL, resp.AvatarURL)
	}
	if resp.Name != "Handler Test Avatar URL" {
		t.Fatalf("name should be unchanged by avatar-only update, got %q", resp.Name)
	}

	var dbAvatar *string
	if err := testPool.QueryRow(ctx, `SELECT avatar_url FROM workspace WHERE id = $1`, wsID).Scan(&dbAvatar); err != nil {
		t.Fatalf("read avatar_url back: %v", err)
	}
	if dbAvatar == nil || *dbAvatar != avatarURL {
		t.Fatalf("expected avatar_url %q persisted, got %v", avatarURL, dbAvatar)
	}

	w2 := httptest.NewRecorder()
	req2 := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
		"description": "new description",
	})
	req2 = withURLParam(req2, "id", wsID)
	testHandler.UpdateWorkspace(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 from second UpdateWorkspace, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 WorkspaceResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.AvatarURL == nil || *resp2.AvatarURL != avatarURL {
		t.Fatalf("avatar_url should be preserved by partial update, got %v", resp2.AvatarURL)
	}
}

func TestUpdateWorkspace_ReposValidation(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-repos-validation"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Repos Validation", slug, "UpdateWorkspace repos validation test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	t.Run("rejects invalid repo URLs without persisting", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
			"repos": []map[string]any{
				{"url": "not-a-url"},
			},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 from invalid repos update, got %d: %s", w.Code, w.Body.String())
		}

		var raw []byte
		if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, wsID).Scan(&raw); err != nil {
			t.Fatalf("read repos: %v", err)
		}
		if string(raw) != "[]" {
			t.Fatalf("invalid repos update should not persist, got %s", raw)
		}
	})

	t.Run("normalizes valid repos", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
			"repos": []map[string]any{
				{
					"url":         "  https://github.com/adanman/goosar.git  ",
					"description": "  main monorepo  ",
				},
				{
					"url": "https://github.com/adanman/goosar.git",
				},
				{
					"url": "git@github.com:adanman/goosar-cloud.git",
				},
			},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 from valid repos update, got %d: %s", w.Code, w.Body.String())
		}

		var raw []byte
		if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, wsID).Scan(&raw); err != nil {
			t.Fatalf("read repos: %v", err)
		}
		var repos []workspaceRepoRef
		if err := json.Unmarshal(raw, &repos); err != nil {
			t.Fatalf("decode repos: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("expected duplicate URL to be deduped, got %d repos: %s", len(repos), raw)
		}
		if repos[0].URL != "https://github.com/adanman/goosar.git" || repos[0].Description != "main monorepo" {
			t.Fatalf("first repo not normalized: %+v", repos[0])
		}
		if repos[1].URL != "git@github.com:adanman/goosar-cloud.git" {
			t.Fatalf("second repo not preserved: %+v", repos[1])
		}
	})
}

type revocationFixture struct {
	WorkspaceID  string
	TargetUserID string
	MemberID     string
	RuntimeID    string
	AgentID      string
	TaskID       string
	DaemonID     string
	TokenHash    string
}

func setupRevocationFixture(t *testing.T, slug, daemonID string) revocationFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Revocation "+slug, slug, "revocation test", "REV").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create requester member: %v", err)
	}

	targetEmail := fmt.Sprintf("revocation-%s@goosar.ru", slug)
	var targetUserID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Revocation Target "+slug, targetEmail).Scan(&targetUserID); err != nil {
		t.Fatalf("create target user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner') RETURNING id
`, wsID, targetUserID).Scan(&memberID); err != nil {
		t.Fatalf("create target member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status,
    device_info, metadata, owner_id, last_seen_at
)
VALUES ($1, $2, 'Target Runtime', 'local', 'goosar_daemon', 'online', '', '{}'::jsonb, $3, now())
RETURNING id
`, wsID, daemonID, targetUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (
    workspace_id, name, description, runtime_mode, runtime_config,
    runtime_id, visibility, max_concurrent_tasks, owner_id
)
VALUES ($1, 'Target Agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
RETURNING id
`, wsID, runtimeID, targetUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
VALUES ($1, $2, 'queued', 0)
RETURNING id
`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	rawToken := "mdt_test_" + slug
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	if _, err := testPool.Exec(ctx, `
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, now() + interval '1 day')
`, tokenHash, wsID, daemonID); err != nil {
		t.Fatalf("insert daemon_token: %v", err)
	}

	return revocationFixture{
		WorkspaceID:  wsID,
		TargetUserID: targetUserID,
		MemberID:     memberID,
		RuntimeID:    runtimeID,
		AgentID:      agentID,
		TaskID:       taskID,
		DaemonID:     daemonID,
		TokenHash:    tokenHash,
	}
}

func assertRevoked(t *testing.T, fx revocationFixture) {
	t.Helper()
	ctx := context.Background()

	var memberExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fx.MemberID).Scan(&memberExists); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberExists {
		t.Fatal("member row was not deleted")
	}

	var runtimeStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, fx.RuntimeID).Scan(&runtimeStatus); err != nil {
		t.Fatalf("query runtime: %v", err)
	}
	if runtimeStatus != "offline" {
		t.Fatalf("expected runtime offline, got %q", runtimeStatus)
	}

	var archivedAt *string
	if err := testPool.QueryRow(ctx, `SELECT archived_at::text FROM agent WHERE id = $1`, fx.AgentID).Scan(&archivedAt); err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if archivedAt == nil {
		t.Fatal("agent was not archived")
	}

	var taskStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fx.TaskID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if taskStatus != "cancelled" {
		t.Fatalf("expected task cancelled, got %q", taskStatus)
	}

	var tokenExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM daemon_token WHERE token_hash = $1)`, fx.TokenHash).Scan(&tokenExists); err != nil {
		t.Fatalf("query daemon_token: %v", err)
	}
	if tokenExists {
		t.Fatal("daemon_token row was not deleted")
	}
}

func TestDeleteMember_RevokesTargetRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-kick", "daemon-revoke-kick")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)
}

func TestDeleteMember_PrunesChannelUserBindings(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-binding", "daemon-revoke-binding")
	ctx := context.Background()

	const appID = "cli_revoke_binding"
	const removedOpenID = "ou_revoke_binding_removed"
	const keepOpenID = "ou_revoke_binding_keep"

	cleanChannel := func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_user_binding WHERE channel_user_id = ANY($1)`,
			[]string{removedOpenID, keepOpenID})
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE channel_type = 'feishu' AND config->>'app_id' = $1`, appID)
	}
	cleanChannel()
	t.Cleanup(cleanChannel)

	var installID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4)
RETURNING id
`, fx.WorkspaceID, fx.AgentID, appID, testUserID).Scan(&installID); err != nil {
		t.Fatalf("insert channel_installation: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_user_binding (workspace_id, goosar_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', $4)
`, fx.WorkspaceID, fx.TargetUserID, installID, removedOpenID); err != nil {
		t.Fatalf("insert removed-member binding: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_user_binding (workspace_id, goosar_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', $4)
`, fx.WorkspaceID, testUserID, installID, keepOpenID); err != nil {
		t.Fatalf("insert remaining-member binding: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var removedExists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_user_binding WHERE channel_user_id = $1)`, removedOpenID).Scan(&removedExists); err != nil {
		t.Fatalf("query removed-member binding: %v", err)
	}
	if removedExists {
		t.Fatal("removed member's channel_user_binding was not pruned")
	}

	var keepExists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_user_binding WHERE channel_user_id = $1)`, keepOpenID).Scan(&keepExists); err != nil {
		t.Fatalf("query remaining-member binding: %v", err)
	}
	if !keepExists {
		t.Fatal("remaining member's channel_user_binding was wrongly pruned")
	}
}

func TestLeaveWorkspace_RevokesOwnRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-leave", "daemon-revoke-leave")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/leave", nil)
	req.Header.Set("X-User-ID", fx.TargetUserID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParam(req, "id", fx.WorkspaceID)
	testHandler.LeaveWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("LeaveWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)
}

func TestDeleteMember_CancelsTasksFromAgentReassignment(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-reassign", "daemon-revoke-reassign")
	ctx := context.Background()

	var otherRuntimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status,
    device_info, metadata, owner_id, last_seen_at
)
VALUES ($1, $2, 'Other Runtime', 'local', 'goosar_daemon', 'online', '', '{}'::jsonb, $3, now())
RETURNING id
`, fx.WorkspaceID, "daemon-revoke-reassign-other", testUserID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("insert other runtime: %v", err)
	}

	var orphanTaskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
VALUES ($1, $2, 'queued', 0)
RETURNING id
`, fx.AgentID, otherRuntimeID).Scan(&orphanTaskID); err != nil {
		t.Fatalf("insert orphan task: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)

	var orphanStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, orphanTaskID).Scan(&orphanStatus); err != nil {
		t.Fatalf("query orphan task: %v", err)
	}
	if orphanStatus != "cancelled" {
		t.Fatalf("expected orphan task cancelled (archived agent leftover on other runtime), got %q", orphanStatus)
	}

	var otherStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, otherRuntimeID).Scan(&otherStatus); err != nil {
		t.Fatalf("query other runtime: %v", err)
	}
	if otherStatus != "online" {
		t.Fatalf("expected other-member runtime to stay online, got %q", otherStatus)
	}
}

func TestDeleteMember_NoRuntimes_DeletesMember(t *testing.T) {
	ctx := context.Background()
	const slug = "handler-tests-revoke-no-runtimes"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Revocation no runtimes", slug, "revocation no-runtimes test", "REV").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create requester member: %v", err)
	}

	var targetUserID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Revocation No Runtimes Target", "revocation-no-runtimes@goosar.ru").Scan(&targetUserID); err != nil {
		t.Fatalf("create target user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin') RETURNING id
`, wsID, targetUserID).Scan(&memberID); err != nil {
		t.Fatalf("create target member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID+"/members/"+memberID, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParams(req, "id", wsID, "memberId", memberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var memberExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, memberID).Scan(&memberExists); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberExists {
		t.Fatal("member row was not deleted")
	}
}

func TestCreateWorkspace_DeploymentAdminBypassesTheDisableSwitch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	const slug = "handler-tests-admin-bypass-create"
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
	})

	prev := testHandler.cfg
	testHandler.cfg = Config{
		AllowSignup:              prev.AllowSignup,
		DisableWorkspaceCreation: true,
	}
	t.Cleanup(func() { testHandler.cfg = prev })

	w := httptest.NewRecorder()
	testHandler.CreateWorkspace(w, newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Admin Bypass",
		"slug": slug,
	}))
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("CreateWorkspace as deployment admin: expected success, got %d: %s", w.Code, w.Body.String())
	}

	if got := countAdminAuditRows(t, adminAuditActionWorkspaceCreateBypass); got != 1 {
		t.Fatalf("workspace-create bypass audit rows = %d, want 1", got)
	}
}
