package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func cleanupDeploymentAdminRows(t *testing.T) {
	t.Helper()
	clear := func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM deployment_admin`)
		testPool.Exec(ctx, `DELETE FROM admin_audit`)
	}
	clear()
	t.Cleanup(clear)
}

func grantDeploymentAdminFixture(t *testing.T, userID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO deployment_admin (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		t.Fatalf("grant deployment admin fixture: %v", err)
	}
}

func deploymentUserFixture(t *testing.T, label string) (userID, email string) {
	t.Helper()
	suffix := randomID()[:8]
	email = "deploy-" + label + "-" + suffix + "@goosar.test"
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Deployment "+label, email).Scan(&userID); err != nil {
		t.Fatalf("create deployment user fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID, email
}

func countAdminAuditRows(t *testing.T, action string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM admin_audit WHERE action = $1`, action).Scan(&count); err != nil {
		t.Fatalf("count admin_audit rows: %v", err)
	}
	return count
}

func deploymentAdminTestRoutes() chi.Router {
	r := chi.NewRouter()
	r.Route("/api/deployment", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/admins", testHandler.ListDeploymentAdmins)
		r.Post("/admins", testHandler.AddDeploymentAdmin)
		r.Delete("/admins/{userId}", testHandler.RemoveDeploymentAdmin)
		r.Get("/admins/pending", testHandler.ListDeploymentAdminPending)
		r.Get("/audit", testHandler.ListDeploymentAdminAudit)

		r.Get("/workspaces", testHandler.ListDeploymentWorkspaces)
		r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(WorkspaceIDFromPathParam)
			r.Get("/members", testHandler.ListDeploymentWorkspaceMembers)
		})
	})
	return r
}

func TestDeploymentAdminRoutes_RejectNonAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	r := deploymentAdminTestRoutes()

	requests := []struct {
		name string
		req  *http.Request
	}{
		{"GET admins", newRequest(http.MethodGet, "/api/deployment/admins", nil)},
		{"POST admins", newRequest(http.MethodPost, "/api/deployment/admins", map[string]any{"email": handlerTestEmail})},
		{"DELETE admin", newRequest(http.MethodDelete, "/api/deployment/admins/"+testUserID, nil)},
		{"GET admins pending", newRequest(http.MethodGet, "/api/deployment/admins/pending", nil)},
		{"GET audit", newRequest(http.MethodGet, "/api/deployment/audit", nil)},
		{"GET workspaces", newRequest(http.MethodGet, "/api/deployment/workspaces", nil)},
		{"GET workspace members", newRequest(http.MethodGet, "/api/deployment/workspaces/"+testWorkspaceID+"/members", nil)},
	}
	for _, tc := range requests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, tc.req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("non-admin: status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestDeploymentAdminRoutes_RejectMachineActors(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	requests := []struct {
		name string
		make func() *http.Request
	}{
		{"GET admins", func() *http.Request { return newRequest(http.MethodGet, "/api/deployment/admins", nil) }},
		{"POST admins", func() *http.Request {
			return newRequest(http.MethodPost, "/api/deployment/admins", map[string]any{"email": handlerTestEmail})
		}},
		{"DELETE admin", func() *http.Request {
			return newRequest(http.MethodDelete, "/api/deployment/admins/"+testUserID, nil)
		}},
		{"GET admins pending", func() *http.Request {
			return newRequest(http.MethodGet, "/api/deployment/admins/pending", nil)
		}},
		{"GET audit", func() *http.Request { return newRequest(http.MethodGet, "/api/deployment/audit", nil) }},
		{"GET workspaces", func() *http.Request {
			return newRequest(http.MethodGet, "/api/deployment/workspaces", nil)
		}},
		{"GET workspace members", func() *http.Request {
			return newRequest(http.MethodGet, "/api/deployment/workspaces/"+testWorkspaceID+"/members", nil)
		}},
	}
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, tc := range requests {
			t.Run(source+" "+tc.name, func(t *testing.T) {
				req := tc.make()
				req.Header.Set("X-Actor-Source", source)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("machine actor %s: status = %d, want 403: %s", source, w.Code, w.Body.String())
				}
			})
		}
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM deployment_admin`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("deployment_admin rows = %d, want the 1 fixture row", count)
	}
}

func TestAddDeploymentAdmin_GrantByEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	targetID, targetEmail := deploymentUserFixture(t, "grant")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins",
		map[string]any{"email": "  " + toUpperASCII(targetEmail) + "  "}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("grant filing: status = %d, want 202: %s", w.Code, w.Body.String())
	}
	var pending DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&pending); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pending.TargetUserID != targetID || pending.TargetEmail != targetEmail {
		t.Fatalf("pending = %+v, want target %s / %s", pending, targetID, targetEmail)
	}
	if pending.RequestedBy != testUserID {
		t.Fatalf("requested_by = %q, want the acting admin %q", pending.RequestedBy, testUserID)
	}

	decision, err := ConfirmDeploymentAdminPending(context.Background(), testPool, testHandler.Queries, parseUUID(pending.RequestID))
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if decision.Action != "grant" || decision.TargetUserID != targetID || decision.AlreadyHeld {
		t.Fatalf("decision = %+v", decision)
	}
	var grantedBy string
	if err := testPool.QueryRow(context.Background(),
		`SELECT granted_by::text FROM deployment_admin WHERE user_id = $1`, targetID).Scan(&grantedBy); err != nil {
		t.Fatalf("granted row missing: %v", err)
	}

	if grantedBy != testUserID {
		t.Fatalf("granted_by = %q, want %q", grantedBy, testUserID)
	}
	if got := countAdminAuditRows(t, "deployment_admin.grant.confirmed"); got != 1 {
		t.Fatalf("grant.confirmed audit rows = %d, want 1", got)
	}

	var confirmReqID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT request_id FROM admin_audit WHERE action = 'deployment_admin.grant.confirmed'`).Scan(&confirmReqID); err != nil {
		t.Fatalf("confirmed audit row: %v", err)
	}
	if confirmReqID != pending.RequestID {
		t.Fatalf("confirmed request_id = %q, want pending id %q", confirmReqID, pending.RequestID)
	}

	var left int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_admin_pending`).Scan(&left); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if left != 0 {
		t.Fatalf("confirmed request still pending (count=%d)", left)
	}

	w = httptest.NewRecorder()
	req := newRequestAsUser(targetID, http.MethodGet, "/api/deployment/admins", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("new admin listing: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var admins []DeploymentAdminEntry
	if err := json.NewDecoder(w.Body).Decode(&admins); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(admins) != 2 {
		t.Fatalf("admin list length = %d, want 2: %+v", len(admins), admins)
	}
}

func TestRejectDeploymentAdminPending(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	_, targetEmail := deploymentUserFixture(t, "reject")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins", map[string]any{"email": targetEmail}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("filing: status = %d: %s", w.Code, w.Body.String())
	}
	var pending DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&pending); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, err := RejectDeploymentAdminPending(context.Background(), testPool, testHandler.Queries, parseUUID(pending.RequestID)); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("reject changed the composition (count=%d)", got)
	}
	if got := countAdminAuditRows(t, "deployment_admin.grant.rejected"); got != 1 {
		t.Fatalf("grant.rejected audit rows = %d, want 1", got)
	}

	if _, err := ConfirmDeploymentAdminPending(context.Background(), testPool, testHandler.Queries, parseUUID(pending.RequestID)); !errors.Is(err, ErrDeploymentAdminPendingNotFound) {
		t.Fatalf("confirm after reject: err = %v, want ErrDeploymentAdminPendingNotFound", err)
	}
}

func TestAddDeploymentAdmin_UnknownEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins",
		map[string]any{"email": "nobody-" + randomID()[:8] + "@goosar.test"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown email: status = %d, want 404: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM deployment_admin`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("unknown-email grant wrote a row (count=%d)", count)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins", map[string]any{"email": "   "}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty email: status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestRemoveDeploymentAdmin_LastAdminProtected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/api/deployment/admins/"+testUserID, nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("remove last admin: status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("last admin was removed anyway (count=%d)", got)
	}

	secondID, _ := deploymentUserFixture(t, "revoke")
	grantDeploymentAdminFixture(t, secondID)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/api/deployment/admins/"+secondID, nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("remove second admin: status = %d, want 202: %s", w.Code, w.Body.String())
	}
	var pending DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&pending); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := ConfirmDeploymentAdminPending(context.Background(), testPool, testHandler.Queries, parseUUID(pending.RequestID)); err != nil {
		t.Fatalf("confirm revoke: %v", err)
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_admin WHERE user_id = $1`, secondID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("revoked admin row still present after confirm")
	}
	if got := countAdminAuditRows(t, "deployment_admin.revoke.confirmed"); got != 1 {
		t.Fatalf("revoke.confirmed audit rows = %d, want 1", got)
	}

	grantDeploymentAdminFixture(t, secondID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/api/deployment/admins/"+secondID, nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("re-file revoke: status = %d: %s", w.Code, w.Body.String())
	}
	var stale DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&stale); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM deployment_admin WHERE user_id = $1`, testUserID); err != nil {
		t.Fatalf("drift delete: %v", err)
	}
	if _, err := ConfirmDeploymentAdminPending(context.Background(), testPool, testHandler.Queries, parseUUID(stale.RequestID)); !errors.Is(err, ErrLastDeploymentAdmin) {
		t.Fatalf("confirm of stale revoke: err = %v, want ErrLastDeploymentAdmin", err)
	}
	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("stale confirm removed the last admin (count=%d)", got)
	}

	grantDeploymentAdminFixture(t, testUserID)
	strangerID, _ := deploymentUserFixture(t, "stranger")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/api/deployment/admins/"+strangerID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("remove non-admin: status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func toUpperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
