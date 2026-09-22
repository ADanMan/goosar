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

func deploymentPolicyTestRoutes() chi.Router {
	r := chi.NewRouter()
	r.Route("/api/deployment", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/policy", testHandler.GetDeploymentPolicyAsAdmin)
		r.Put("/policy", testHandler.PutDeploymentPolicy)
		r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(WorkspaceIDFromPathParam)
			r.Get("/config", testHandler.GetWorkspaceConfig)
			r.Put("/config", testHandler.PutWorkspaceConfig)
			r.Route("/config/overrides/{userId}", func(r chi.Router) {
				r.Get("/", testHandler.GetWorkspaceUserConfigOverride)
				r.Put("/", testHandler.PutWorkspaceUserConfigOverride)
				r.Delete("/", testHandler.DeleteWorkspaceUserConfigOverride)
			})
		})
	})
	return r
}

func seedWorkspaceMcpDefaultsFor(t *testing.T, workspaceID, doc string) {
	t.Helper()
	sealed, err := testHandler.sealConfigDocument([]byte(doc))
	if err != nil {
		t.Fatalf("seal mcp defaults: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_config (workspace_id, mcp_defaults, updated_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id) DO UPDATE SET
			mcp_defaults = EXCLUDED.mcp_defaults, updated_at = now()
	`, workspaceID, sealed, testUserID); err != nil {
		t.Fatalf("seed workspace mcp defaults: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_config WHERE workspace_id = $1`, workspaceID)
	})
}

func deploymentPolicyRowCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_policy`).Scan(&count); err != nil {
		t.Fatalf("count deployment_policy: %v", err)
	}
	return count
}

func TestDeploymentPolicyRoutes_RejectNonAdminAndMachineActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	r := deploymentPolicyTestRoutes()

	otherWorkspaceID, otherOwnerID := secondWorkspaceFixture(t)

	policyBody := map[string]any{"policy": map[string]any{"llm": map[string]any{"model": "m"}}}
	requests := []struct {
		name string
		make func() *http.Request
	}{
		{"GET policy", func() *http.Request { return newRequest(http.MethodGet, "/api/deployment/policy", nil) }},
		{"PUT policy", func() *http.Request { return newRequest(http.MethodPut, "/api/deployment/policy", policyBody) }},
		{"GET foreign workspace config", func() *http.Request {
			return newRequest(http.MethodGet, "/api/deployment/workspaces/"+otherWorkspaceID+"/config", nil)
		}},
		{"PUT foreign workspace config", func() *http.Request {
			return newRequest(http.MethodPut, "/api/deployment/workspaces/"+otherWorkspaceID+"/config", map[string]any{"llm_model": "m"})
		}},
		{"GET foreign override", func() *http.Request {
			return newRequest(http.MethodGet, "/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID, nil)
		}},
		{"PUT foreign override", func() *http.Request {
			return newRequest(http.MethodPut, "/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID, map[string]any{"llm_model": "m"})
		}},
		{"DELETE foreign override", func() *http.Request {
			return newRequest(http.MethodDelete, "/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID, nil)
		}},
	}

	for _, tc := range requests {
		t.Run("non-admin "+tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, tc.make())
			if w.Code != http.StatusForbidden {
				t.Fatalf("non-admin: status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}

	grantDeploymentAdminFixture(t, testUserID)
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, tc := range requests {
			t.Run(source+" "+tc.name, func(t *testing.T) {
				req := tc.make()
				req.Header.Set("X-Actor-Source", source)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("machine actor: status = %d, want 403: %s", w.Code, w.Body.String())
				}
			})
		}
	}
	if got := deploymentPolicyRowCount(t); got != 0 {
		t.Fatalf("rejected requests wrote a policy row (count=%d)", got)
	}
}

func TestPutDeploymentPolicy_WritesAndAudits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentPolicyTestRoutes()

	put := func(policy map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPut, "/api/deployment/policy", map[string]any{"policy": policy}))
		return w
	}

	w := put(map[string]any{
		"llm": map[string]any{"base_url": "https://gw.corp.example/v1", "model": "coding-medium", "locked": true},
		"mcp": map[string]any{"jira": map[string]any{"enabled": false, "locked": true}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp DeploymentPolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(string(resp.Policy), "gw.corp.example") {
		t.Fatalf("response policy missing content: %s", resp.Policy)
	}
	if got := deploymentPolicyRowCount(t); got != 1 {
		t.Fatalf("policy rows = %d, want 1", got)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/api/deployment/policy", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var firstBefore, firstAfter *string
	if err := testPool.QueryRow(context.Background(), `
		SELECT before_hash, after_hash FROM admin_audit
		WHERE action = 'deployment_policy.set'
		ORDER BY created_at ASC LIMIT 1
	`).Scan(&firstBefore, &firstAfter); err != nil {
		t.Fatalf("first audit row: %v", err)
	}
	if firstBefore != nil && *firstBefore != "" {
		t.Fatalf("first write before_hash = %v, want empty", *firstBefore)
	}
	if firstAfter == nil || *firstAfter == "" {
		t.Fatalf("first write after_hash missing")
	}

	w = put(map[string]any{"mcp": map[string]any{"jira": map[string]any{"enabled": true}}})
	if w.Code != http.StatusOK {
		t.Fatalf("second PUT: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var rowsCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM admin_audit WHERE action = 'deployment_policy.set'`).Scan(&rowsCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if rowsCount != 2 {
		t.Fatalf("policy audit rows = %d, want 2", rowsCount)
	}
	var secondBefore *string
	if err := testPool.QueryRow(context.Background(), `
		SELECT before_hash FROM admin_audit
		WHERE action = 'deployment_policy.set'
		ORDER BY created_at DESC LIMIT 1
	`).Scan(&secondBefore); err != nil {
		t.Fatalf("second audit row: %v", err)
	}
	if secondBefore == nil || *secondBefore != *firstAfter {
		t.Fatalf("second write before_hash does not chain to the first after_hash")
	}
}

func TestPutDeploymentPolicy_RejectsSecretsAndUnknownFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentPolicyTestRoutes()

	cases := []struct {
		name   string
		policy map[string]any
	}{
		{"llm api_key", map[string]any{"llm": map[string]any{"api_key": "sk-x"}}},
		{"mcp env token", map[string]any{"mcp": map[string]any{"jira": map[string]any{"env": map[string]any{"JIRA_API_TOKEN": "x"}}}}},
		{"mcp env password", map[string]any{"mcp": map[string]any{"jira": map[string]any{"env": map[string]any{"DB_PASSWORD": "x"}}}}},
		{"mcp env secret", map[string]any{"mcp": map[string]any{"jira": map[string]any{"env": map[string]any{"CLIENT_SECRET": "x"}}}}},
		{"llm stray secret field", map[string]any{"llm": map[string]any{"secret": "x"}}},
		{"unknown top-level block", map[string]any{"credentials": map[string]any{}}},
		{"bad base_url", map[string]any{"llm": map[string]any{"base_url": "not-a-url"}}},
		{"mcp entry unknown field", map[string]any{"mcp": map[string]any{"jira": map[string]any{"command": "rm"}}}},

		{"mcp env innocent name", map[string]any{"mcp": map[string]any{"jira": map[string]any{"env": map[string]any{"JIRA_HOST": "jira.corp"}}}}},
		{"mcp env empty object", map[string]any{"mcp": map[string]any{"jira": map[string]any{"env": map[string]any{}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, newRequest(http.MethodPut, "/api/deployment/policy", map[string]any{"policy": tc.policy}))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
	if got := deploymentPolicyRowCount(t); got != 0 {
		t.Fatalf("rejected policy still wrote a row (count=%d)", got)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/api/deployment/policy", map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing policy: status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestPutDeploymentPolicy_WildcardValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentPolicyTestRoutes()

	put := func(policy map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPut, "/api/deployment/policy", map[string]any{"policy": policy}))
		return w
	}

	rejected := []struct {
		name  string
		entry map[string]any
	}{
		{"enabled true", map[string]any{"enabled": true, "locked": true}},
		{"locked false", map[string]any{"enabled": false, "locked": false}},
		{"enabled absent", map[string]any{"locked": true}},
		{"locked absent", map[string]any{"enabled": false}},
		{"empty object", map[string]any{}},
	}
	for _, tc := range rejected {
		t.Run("reject "+tc.name, func(t *testing.T) {
			w := put(map[string]any{"mcp": map[string]any{"*": tc.entry}})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "enabled") || !strings.Contains(w.Body.String(), "locked") {
				t.Fatalf("wildcard rejection must explain the accepted form: %s", w.Body.String())
			}
		})
	}
	if got := deploymentPolicyRowCount(t); got != 0 {
		t.Fatalf("rejected wildcard still wrote a row (count=%d)", got)
	}

	w := put(map[string]any{"mcp": map[string]any{"*": map[string]any{"enabled": false, "locked": true}}})
	if w.Code != http.StatusOK {
		t.Fatalf("valid wildcard: status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestDeploymentPolicy_LockedLlmPinsAgainstUserOverride(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentPolicyTestRoutes()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/api/deployment/policy", map[string]any{
		"policy": map[string]any{"llm": map[string]any{"base_url": "https://locked.example/v1", "model": "pinned-model", "locked": true}},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT policy: status = %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut,
		"/api/deployment/workspaces/"+testWorkspaceID+"/config/overrides/"+testUserID,
		map[string]any{"llm_base_url": "https://personal.example/v1", "llm_model": "personal-model"}))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT override: status = %d: %s", w.Code, w.Body.String())
	}

	view := httptest.NewRecorder()
	testHandler.GetEffectiveConfigView(view, newRequest(http.MethodGet, "/api/effective-config", nil))
	if view.Code != http.StatusOK {
		t.Fatalf("effective view: status = %d: %s", view.Code, view.Body.String())
	}
	var resolved EffectiveConfigViewResponse
	if err := json.NewDecoder(view.Body).Decode(&resolved); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if resolved.LLM == nil {
		t.Fatalf("resolved view has no llm block")
	}
	if resolved.LLM.BaseURL != "https://locked.example/v1" || resolved.LLM.Model != "pinned-model" {
		t.Fatalf("locked policy was pierced by a user override: %+v", resolved.LLM)
	}
	if resolved.LLM.Origin != ConfigOriginPolicy || !resolved.LLM.Locked {
		t.Fatalf("resolved llm origin/locked = %q/%v, want policy/true", resolved.LLM.Origin, resolved.LLM.Locked)
	}
}

func TestDeploymentAdmin_CrossWorkspaceOverride(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentPolicyTestRoutes()

	otherWorkspaceID, otherOwnerID := secondWorkspaceFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM user_config_override WHERE workspace_id = $1`, otherWorkspaceID)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID,
		map[string]any{"llm_model": "their-personal-model"}))
	if w.Code != http.StatusOK {
		t.Fatalf("cross-workspace override: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var updatedBy string
	if err := testPool.QueryRow(context.Background(), `
		SELECT updated_by::text FROM user_config_override
		WHERE workspace_id = $1 AND user_id = $2
	`, otherWorkspaceID, otherOwnerID).Scan(&updatedBy); err != nil {
		t.Fatalf("override row missing: %v", err)
	}
	if updatedBy != testUserID {
		t.Fatalf("override updated_by = %q, want the deployment admin %q", updatedBy, testUserID)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+testUserID,
		map[string]any{"llm_model": "m"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("override for non-member target: status = %d, want 404: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequestAsUser(otherOwnerID, http.MethodPut,
		"/api/deployment/workspaces/"+testWorkspaceID+"/config/overrides/"+testUserID,
		map[string]any{"llm_model": "m"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign owner cross-workspace write: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

type adminAuditRow struct {
	actor      string
	targetType string
	targetID   string
	before     *string
	after      *string
}

func lastAdminAuditRow(t *testing.T, action string) adminAuditRow {
	t.Helper()
	var row adminAuditRow
	if err := testPool.QueryRow(context.Background(), `
		SELECT actor_user_id::text, target_type, target_id, before_hash, after_hash
		FROM admin_audit WHERE action = $1
		ORDER BY created_at DESC LIMIT 1
	`, action).Scan(&row.actor, &row.targetType, &row.targetID, &row.before, &row.after); err != nil {
		t.Fatalf("admin_audit row for %q: %v", action, err)
	}
	return row
}

func adminAuditCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM admin_audit`).Scan(&count); err != nil {
		t.Fatalf("count admin_audit: %v", err)
	}
	return count
}

func TestDeploymentAdmin_CrossWorkspaceConfigAudited(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentPolicyTestRoutes()

	otherWorkspaceID, otherOwnerID := secondWorkspaceFixture(t)
	seedWorkspaceMcpDefaultsFor(t, otherWorkspaceID, `{"jira":{"enabled":true,"env":{"J_USER":"svc"}}}`)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM user_config_override WHERE workspace_id = $1`, otherWorkspaceID)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/api/deployment/workspaces/"+otherWorkspaceID+"/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET config: %d: %s", w.Code, w.Body.String())
	}
	read := lastAdminAuditRow(t, "config.read")
	if read.actor != testUserID {
		t.Fatalf("config.read actor = %q, want %q", read.actor, testUserID)
	}
	if read.targetType != "workspace" || read.targetID != otherWorkspaceID {
		t.Fatalf("config.read target = %s/%s, want workspace/%s", read.targetType, read.targetID, otherWorkspaceID)
	}
	if read.before == nil || read.after == nil || *read.before != *read.after || *read.after == "" {
		t.Fatalf("config.read must record before == after != \"\": %+v", read)
	}

	if *read.after == deploymentPolicyHash([]byte("svc")) {
		t.Fatalf("audit hash derived from a bare secret value")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/api/deployment/workspaces/"+otherWorkspaceID+"/config",
		map[string]any{"llm_model": "corp-model"}))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT config: %d: %s", w.Code, w.Body.String())
	}
	set := lastAdminAuditRow(t, "workspace_config.set")
	if set.targetType != "workspace" || set.targetID != otherWorkspaceID {
		t.Fatalf("workspace_config.set target = %s/%s", set.targetType, set.targetID)
	}
	if set.before == nil || set.after == nil || *set.before == *set.after {
		t.Fatalf("workspace_config.set must record a hash transition: %+v", set)
	}
	if *set.before != *read.after {
		t.Fatalf("workspace_config.set before_hash must chain to the read's hash of the same state")
	}

	overrideTarget := otherWorkspaceID + "/" + otherOwnerID
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID,
		map[string]any{"llm_model": "their-model"}))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT override: %d: %s", w.Code, w.Body.String())
	}
	oset := lastAdminAuditRow(t, "user_config_override.set")
	if oset.targetType != "workspace_user" || oset.targetID != overrideTarget {
		t.Fatalf("override.set target = %s/%s, want workspace_user/%s", oset.targetType, oset.targetID, overrideTarget)
	}
	if oset.after == nil || *oset.after == "" {
		t.Fatalf("override.set after_hash missing: %+v", oset)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET override: %d: %s", w.Code, w.Body.String())
	}
	oread := lastAdminAuditRow(t, "config.read")
	if oread.targetType != "workspace_user" || oread.targetID != overrideTarget {
		t.Fatalf("override config.read target = %s/%s", oread.targetType, oread.targetID)
	}
	if oread.before == nil || oread.after == nil || *oread.before != *oread.after {
		t.Fatalf("override config.read must record before == after: %+v", oread)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/config/overrides/"+otherOwnerID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE override: %d: %s", w.Code, w.Body.String())
	}
	odel := lastAdminAuditRow(t, "user_config_override.delete")
	if odel.targetType != "workspace_user" || odel.targetID != overrideTarget {
		t.Fatalf("override.delete target = %s/%s", odel.targetType, odel.targetID)
	}
	if odel.before == nil || *odel.before == "" {
		t.Fatalf("override.delete must record the state it destroyed: %+v", odel)
	}
	if odel.after != nil && *odel.after != "" {
		t.Fatalf("override.delete after_hash must be empty: %+v", odel)
	}
}

func TestWorkspaceOwner_OwnConfigNotInAdminAudit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))

	before := adminAuditCount(t)

	w := httptest.NewRecorder()
	testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config",
		map[string]any{"llm_model": "own-model"}))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT own config: %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.GetWorkspaceConfig(w, newRequest(http.MethodGet, "/api/workspace-config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET own config: %d: %s", w.Code, w.Body.String())
	}

	if got := adminAuditCount(t); got != before {
		t.Fatalf("own-workspace administration wrote %d admin_audit rows", got-before)
	}
}

func TestDeploymentAdmin_MemberButNotOwnerIsAuditedToo(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))
	r := deploymentPolicyTestRoutes()

	otherWorkspaceID, _ := secondWorkspaceFixture(t)

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, otherWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, otherWorkspaceID, testUserID)
	})
	grantDeploymentAdminFixture(t, testUserID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/api/deployment/workspaces/"+otherWorkspaceID+"/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET config as member+deployment-admin: %d: %s", w.Code, w.Body.String())
	}
	read := lastAdminAuditRow(t, "config.read")
	if read.targetID != otherWorkspaceID {
		t.Fatalf("config.read target = %q, want %q", read.targetID, otherWorkspaceID)
	}
}
