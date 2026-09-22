package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func deploymentJoinTestRoutes() chi.Router {
	r := chi.NewRouter()
	r.Route("/api/deployment", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/join-targets", testHandler.ListJoinTargets)
		r.Post("/join-targets/{workspaceId}/join", testHandler.JoinTarget)
		r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
			r.Patch("/", testHandler.SetDeploymentWorkspaceOpenJoin)
		})
	})
	return r
}

func roleWorkspaceFixture(t *testing.T, role string, openJoin bool) (id, templateKey string) {
	t.Helper()
	suffix := randomID()[:8]
	templateKey = role + "-" + suffix
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO workspace (name, slug, description, issue_prefix, template_key, open_join)
		 VALUES ($1, $2, $3, 'ROLE', $4, $5) RETURNING id`,
		"Role "+role, "role-"+templateKey, "Роль "+role, templateKey, openJoin,
	).Scan(&id); err != nil {
		t.Fatalf("create role workspace fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id, templateKey
}

func workspaceTemplateFixture(t *testing.T, key string, position int32) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO workspace_template (key, display_name, description, position)
		 VALUES ($1, $2, '{}', $3)`,
		key, `{"ru":"`+key+`"}`, position); err != nil {
		t.Fatalf("create workspace_template fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
}

func joinTargets(t *testing.T, userID string) []JoinTargetEntry {
	t.Helper()
	w := httptest.NewRecorder()
	deploymentJoinTestRoutes().ServeHTTP(w, newRequestAs(userID, http.MethodGet, "/api/deployment/join-targets", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list join targets: status = %d: %s", w.Code, w.Body.String())
	}
	var entries []JoinTargetEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode join targets: %v", err)
	}
	return entries
}

func findTarget(entries []JoinTargetEntry, id string) *JoinTargetEntry {
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i]
		}
	}
	return nil
}

func joinRequest(t *testing.T, userID, workspaceID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	deploymentJoinTestRoutes().ServeHTTP(w,
		newRequestAs(userID, http.MethodPost, "/api/deployment/join-targets/"+workspaceID+"/join", nil))
	return w
}

func memberRole(t *testing.T, workspaceID, userID string) (string, bool) {
	t.Helper()
	var role string
	err := testPool.QueryRow(context.Background(),
		`SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID).Scan(&role)
	if err != nil {
		return "", false
	}
	return role, true
}

func TestJoinTargets_OnlyOpenRolesTheCallerIsNotIn(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, _ := deploymentUserFixture(t, "joiner")
	open, _ := roleWorkspaceFixture(t, "hr", true)
	closed, _ := roleWorkspaceFixture(t, "finance", false)

	entries := joinTargets(t, userID)
	if findTarget(entries, open) == nil {
		t.Fatalf("open role missing from join targets: %+v", entries)
	}
	if findTarget(entries, closed) != nil {
		t.Fatal("closed role (open_join = false) must not be listed")
	}

	if findTarget(entries, testWorkspaceID) != nil {
		t.Fatal("an ordinary workspace must never be a join target")
	}

	if w := joinRequest(t, userID, open); w.Code != http.StatusOK {
		t.Fatalf("join: status = %d: %s", w.Code, w.Body.String())
	}
	if findTarget(joinTargets(t, userID), open) != nil {
		t.Fatal("a role the caller already joined must drop off the list")
	}
}

func TestJoinTargets_OrderedByTemplatePosition(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, _ := deploymentUserFixture(t, "ordering")
	finance, financeKey := roleWorkspaceFixture(t, "finance", true)
	workspaceTemplateFixture(t, financeKey, 20)
	hr, hrKey := roleWorkspaceFixture(t, "hr", true)
	workspaceTemplateFixture(t, hrKey, 10)

	hrIdx, financeIdx := -1, -1
	for i, e := range joinTargets(t, userID) {
		switch e.ID {
		case hr:
			hrIdx = i
		case finance:
			financeIdx = i
		}
	}
	if hrIdx < 0 || financeIdx < 0 {
		t.Fatalf("both roles must be listed: hr=%d finance=%d", hrIdx, financeIdx)
	}
	if hrIdx > financeIdx {
		t.Fatalf("hr (template position 10) must precede finance (20): hr=%d finance=%d", hrIdx, financeIdx)
	}
}

func TestJoinTarget_AddsMemberAndIsIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, _ := deploymentUserFixture(t, "idempotent")
	wsID, _ := roleWorkspaceFixture(t, "legal", true)

	first := joinRequest(t, userID, wsID)
	if first.Code != http.StatusOK {
		t.Fatalf("first join: status = %d: %s", first.Code, first.Body.String())
	}
	var firstBody JoinTargetResult
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode join result: %v", err)
	}
	if firstBody.AlreadyMember {
		t.Fatal("first join must not report already_member")
	}
	if firstBody.Slug == "" {
		t.Fatal("join result must carry the slug the client navigates to")
	}

	role, ok := memberRole(t, wsID, userID)
	if !ok || role != "member" {
		t.Fatalf("membership role = %q (found=%v), want \"member\"", role, ok)
	}

	second := joinRequest(t, userID, wsID)
	if second.Code != http.StatusOK {
		t.Fatalf("repeat join: status = %d, want 200: %s", second.Code, second.Body.String())
	}
	var secondBody JoinTargetResult
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatalf("decode repeat join result: %v", err)
	}
	if !secondBody.AlreadyMember {
		t.Fatal("repeat join must report already_member")
	}
	if secondBody.ID != firstBody.ID || secondBody.Slug != firstBody.Slug {
		t.Fatalf("repeat join changed the body: %+v vs %+v", secondBody, firstBody)
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, wsID, userID).Scan(&count); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if count != 1 {
		t.Fatalf("membership count = %d, want 1", count)
	}
}

func TestJoinTarget_RefusesClosedAndOrdinaryWorkspaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, _ := deploymentUserFixture(t, "refused")
	closed, _ := roleWorkspaceFixture(t, "sales", false)

	for name, wsID := range map[string]string{
		"closed role":        closed,
		"ordinary workspace": testWorkspaceID,
	} {
		t.Run(name, func(t *testing.T) {
			w := joinRequest(t, userID, wsID)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
			}
			if _, ok := memberRole(t, wsID, userID); ok {
				t.Fatal("a refused join must not create a membership")
			}
		})
	}
}

func TestJoinTargets_RejectMachineActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, _ := deploymentUserFixture(t, "machine")
	wsID, _ := roleWorkspaceFixture(t, "hr", true)
	r := deploymentJoinTestRoutes()

	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, req := range []*http.Request{
			newRequestAs(userID, http.MethodGet, "/api/deployment/join-targets", nil),
			newRequestAs(userID, http.MethodPost, "/api/deployment/join-targets/"+wsID+"/join", nil),
		} {
			req.Header.Set("X-Actor-Source", source)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s as %s: status = %d, want 403", req.Method, req.URL.Path, source, w.Code)
			}
		}
	}
	if _, ok := memberRole(t, wsID, userID); ok {
		t.Fatal("a machine actor must not have joined anything")
	}
}

func TestSetWorkspaceOpenJoin_ClosesTheRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "toggle-admin")
	grantDeploymentAdminFixture(t, adminID)
	userID, _ := deploymentUserFixture(t, "toggle-user")
	wsID, _ := roleWorkspaceFixture(t, "hr", true)
	r := deploymentJoinTestRoutes()

	if findTarget(joinTargets(t, userID), wsID) == nil {
		t.Fatal("precondition: the open role must be listed")
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequestAs(adminID, http.MethodPatch,
		"/api/deployment/workspaces/"+wsID, map[string]any{"open_join": false}))
	if w.Code != http.StatusOK {
		t.Fatalf("patch: status = %d: %s", w.Code, w.Body.String())
	}

	if findTarget(joinTargets(t, userID), wsID) != nil {
		t.Fatal("a closed role must disappear from the join targets")
	}
	if got := joinRequest(t, userID, wsID); got.Code != http.StatusNotFound {
		t.Fatalf("join after close: status = %d, want 404: %s", got.Code, got.Body.String())
	}
	if n := countAdminAuditRows(t, adminAuditActionWorkspaceOpenJoinSet); n != 1 {
		t.Fatalf("admin_audit rows for the toggle = %d, want 1", n)
	}
}

func TestSetWorkspaceOpenJoin_RefusesOrdinaryWorkspaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "ordinary-admin")
	grantDeploymentAdminFixture(t, adminID)

	w := httptest.NewRecorder()
	deploymentJoinTestRoutes().ServeHTTP(w, newRequestAs(adminID, http.MethodPatch,
		"/api/deployment/workspaces/"+testWorkspaceID, map[string]any{"open_join": true}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var openJoin bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT open_join FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&openJoin); err != nil {
		t.Fatalf("read open_join: %v", err)
	}
	if openJoin {
		t.Fatal("an ordinary workspace must stay closed")
	}
	if n := countAdminAuditRows(t, adminAuditActionWorkspaceOpenJoinSet); n != 0 {
		t.Fatalf("refused toggle wrote %d admin_audit row(s), want 0", n)
	}
}

func TestSetWorkspaceOpenJoin_Gate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	userID, _ := deploymentUserFixture(t, "not-admin")
	wsID, _ := roleWorkspaceFixture(t, "hr", true)
	r := deploymentJoinTestRoutes()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequestAs(userID, http.MethodPatch,
		"/api/deployment/workspaces/"+wsID, map[string]any{"open_join": false}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin patch: status = %d, want 403: %s", w.Code, w.Body.String())
	}

	adminID, _ := deploymentUserFixture(t, "empty-body-admin")
	grantDeploymentAdminFixture(t, adminID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequestAs(adminID, http.MethodPatch,
		"/api/deployment/workspaces/"+wsID, map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("patch without open_join: status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var openJoin bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT open_join FROM workspace WHERE id = $1`, wsID).Scan(&openJoin); err != nil {
		t.Fatalf("read open_join: %v", err)
	}
	if !openJoin {
		t.Fatal("a body without open_join must leave the flag untouched")
	}
}
