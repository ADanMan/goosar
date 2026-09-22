package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func extraWorkspaceMemberFixture(t *testing.T, workspaceID, label string) (userID string) {
	t.Helper()
	ctx := context.Background()
	suffix := randomID()[:8]
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Override List "+label, "override-list-"+label+"-"+suffix+"@goosar.test").Scan(&userID); err != nil {
		t.Fatalf("create extra member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, userID); err != nil {
		t.Fatalf("add extra member: %v", err)
	}
	return userID
}

func deploymentOverrideListTestRoutes() chi.Router {
	r := chi.NewRouter()
	r.Route("/api/deployment", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(WorkspaceIDFromPathParam)
			r.Get("/config/overrides", testHandler.ListWorkspaceUserConfigOverrides)
			r.Put("/config/overrides/{userId}", testHandler.PutWorkspaceUserConfigOverride)
		})
	})
	return r
}

func TestListWorkspaceUserConfigOverrides_RejectNonAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	r := deploymentOverrideListTestRoutes()

	otherWorkspaceID, _ := secondWorkspaceFixture(t)
	auditBefore := adminAuditCount(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: status = %d, want 403: %s", w.Code, w.Body.String())
	}
	if after := adminAuditCount(t); after != auditBefore {
		t.Fatalf("refused listing wrote admin_audit rows: before=%d after=%d", auditBefore, after)
	}
}

func TestListWorkspaceUserConfigOverrides_RejectMachineActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentOverrideListTestRoutes()

	otherWorkspaceID, _ := secondWorkspaceFixture(t)
	for _, source := range []string{"task_token", "cloud_pat"} {
		t.Run(source, func(t *testing.T) {
			req := newRequest(http.MethodGet,
				"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides", nil)
			req.Header.Set("X-Actor-Source", source)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("machine actor %s: status = %d, want 403: %s", source, w.Code, w.Body.String())
			}
		})
	}
}

func TestListWorkspaceUserConfigOverrides_OneAuditRowPerCall(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentOverrideListTestRoutes()

	otherWorkspaceID, otherOwnerID := secondWorkspaceFixture(t)
	secondMemberID := extraWorkspaceMemberFixture(t, otherWorkspaceID, "override-list")
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM user_config_override WHERE workspace_id = $1`, otherWorkspaceID)
	})

	for _, seed := range []struct {
		userID string
		body   map[string]any
	}{
		{otherOwnerID, map[string]any{
			"llm_base_url":  "https://owner.example/v1",
			"llm_model":     "owner-model",
			"llm_api_key":   "sk-owner-plaintext",
			"mcp_overrides": map[string]any{"jira": map[string]any{"enabled": true, "env": map[string]any{"J_USER": "svc-plaintext"}}},
		}},
		{secondMemberID, map[string]any{"llm_model": "second-model"}},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPut,
			"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+seed.userID, seed.body))
		if w.Code != http.StatusOK {
			t.Fatalf("seed override for %s: status = %d: %s", seed.userID, w.Code, w.Body.String())
		}
	}

	auditBefore := adminAuditCount(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200: %s", w.Code, w.Body.String())
	}

	after := adminAuditCount(t)
	if after != auditBefore+1 {
		t.Fatalf("override listing wrote %d admin_audit rows, want exactly 1 (before=%d after=%d)",
			after-auditBefore, auditBefore, after)
	}
	row := lastAdminAuditRow(t, adminAuditActionConfigRead)
	if row.actor != testUserID {
		t.Fatalf("config.read actor = %q, want %q", row.actor, testUserID)
	}

	if row.targetType != "workspace_overrides" || row.targetID != otherWorkspaceID {
		t.Fatalf("config.read target = %s/%s, want workspace_overrides/%s", row.targetType, row.targetID, otherWorkspaceID)
	}
	if row.before == nil || row.after == nil || *row.before != *row.after || *row.after == "" {
		t.Fatalf("config.read must record before == after != \"\": %+v", row)
	}

	var entries []UserConfigOverrideResponse
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("listing length = %d, want 2: %+v", len(entries), entries)
	}
	byUser := map[string]UserConfigOverrideResponse{}
	for _, e := range entries {
		byUser[e.UserID] = e
	}
	owner, ok := byUser[otherOwnerID]
	if !ok {
		t.Fatalf("owner override missing from the listing: %+v", entries)
	}
	if owner.LlmBaseURL != "https://owner.example/v1" || owner.LlmModel != "owner-model" {
		t.Fatalf("owner override coordinates lost: %+v", owner)
	}
	if !owner.HasLlmAPIKey {
		t.Fatalf("owner override must report has_llm_api_key: %+v", owner)
	}
	if _, ok := byUser[secondMemberID]; !ok {
		t.Fatalf("second member's override missing from the listing: %+v", entries)
	}

	body := w.Body.String()
	for _, secret := range []string{"sk-owner-plaintext", "svc-plaintext"} {
		if strings.Contains(body, secret) {
			t.Fatalf("plaintext secret %q leaked into the listing: %s", secret, body)
		}
	}
}

func TestListWorkspaceUserConfigOverrides_EmptyAndUnknown(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentOverrideListTestRoutes()

	otherWorkspaceID, _ := secondWorkspaceFixture(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("empty listing: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "[]\n" && got != "[]" {
		t.Fatalf("empty listing body = %q, want an empty JSON array", got)
	}

	auditBefore := adminAuditCount(t)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet,
		"/api/deployment/workspaces/00000000-0000-4000-8000-000000000243/config/overrides", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace: status = %d, want 404: %s", w.Code, w.Body.String())
	}
	if after := adminAuditCount(t); after != auditBefore {
		t.Fatalf("404 listing wrote admin_audit rows: before=%d after=%d", auditBefore, after)
	}
}
