package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func deploymentAdminCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_admin`).Scan(&count); err != nil {
		t.Fatalf("count deployment_admin: %v", err)
	}
	return count
}

func cleanupDeploymentAdminPendingRows(t *testing.T) {
	t.Helper()
	clear := func() {
		testPool.Exec(context.Background(), `DELETE FROM deployment_admin_pending`)
	}
	clear()
	t.Cleanup(clear)
}

func TestAddDeploymentAdmin_FilesPendingRequest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	targetID, targetEmail := deploymentUserFixture(t, "pending-grant")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins",
		map[string]any{"email": targetEmail}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST admins: status = %d, want 202: %s", w.Code, w.Body.String())
	}

	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("POST changed the acting composition (count=%d, want 1)", got)
	}

	var resp DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "pending" || resp.Action != "grant" {
		t.Fatalf("response must say pending grant: %+v", resp)
	}
	if resp.TargetUserID != targetID {
		t.Fatalf("target = %q, want %q", resp.TargetUserID, targetID)
	}
	if resp.RequestID == "" || !strings.Contains(resp.ConfirmHint, resp.RequestID) {
		t.Fatalf("response must name the request id in the confirm hint: %+v", resp)
	}
	if !strings.Contains(resp.ConfirmHint, "goosar_admin") {
		t.Fatalf("confirm hint must point at the server CLI: %q", resp.ConfirmHint)
	}

	if got := countAdminAuditRows(t, "deployment_admin.grant.requested"); got != 1 {
		t.Fatalf("grant.requested audit rows = %d, want 1", got)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins",
		map[string]any{"email": targetEmail}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("repeat POST: status = %d, want 202: %s", w.Code, w.Body.String())
	}
	var again DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&again); err != nil {
		t.Fatalf("decode repeat: %v", err)
	}
	if again.RequestID != resp.RequestID {
		t.Fatalf("repeat filing created a second pending request")
	}
	if got := countAdminAuditRows(t, "deployment_admin.grant.requested"); got != 1 {
		t.Fatalf("repeat filing wrote a second audit row")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins",
		map[string]any{"email": handlerTestEmail}))
	if w.Code != http.StatusOK {
		t.Fatalf("grant of existing admin: status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestRemoveDeploymentAdmin_FilesPendingRequest(t *testing.T) {
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
		t.Fatalf("revoke last admin: status = %d, want 409: %s", w.Code, w.Body.String())
	}

	secondID, _ := deploymentUserFixture(t, "pending-revoke")
	grantDeploymentAdminFixture(t, secondID)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/api/deployment/admins/"+secondID, nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("revoke: status = %d, want 202: %s", w.Code, w.Body.String())
	}

	if got := deploymentAdminCount(t); got != 2 {
		t.Fatalf("DELETE changed the acting composition (count=%d, want 2)", got)
	}
	var resp DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "pending" || resp.Action != "revoke" || resp.TargetUserID != secondID {
		t.Fatalf("response must say pending revoke of %s: %+v", secondID, resp)
	}
	if got := countAdminAuditRows(t, "deployment_admin.revoke.requested"); got != 1 {
		t.Fatalf("revoke.requested audit rows = %d, want 1", got)
	}

	strangerID, _ := deploymentUserFixture(t, "pending-stranger")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/api/deployment/admins/"+strangerID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("revoke non-admin: status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestDeploymentAdminConfirmHint_IsRunnableInsideTheContainer(t *testing.T) {
	hint := deploymentAdminConfirmHint("abc-123")
	if strings.Contains(hint, "go run") {
		t.Fatalf("hint must not tell the operator to run `go run` — there is no Go in the image: %q", hint)
	}
	for _, want := range []string{
		"docker compose exec backend ./goosar_admin confirm abc-123",
		"kubectl exec",
		"./goosar_admin confirm abc-123",
	} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q must contain %q", hint, want)
		}
	}
}

func TestGrantDeploymentAdminByEmail_BreakGlass(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)

	_, email := deploymentUserFixture(t, "break-glass")

	granted, err := GrantDeploymentAdminByEmail(context.Background(), testPool, testHandler.Queries, email)
	if err != nil {
		t.Fatalf("grant by email: %v", err)
	}
	if !granted.Created {
		t.Fatalf("first grant must report a created role row: %+v", granted)
	}
	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("deployment_admin rows = %d, want 1", got)
	}

	if got := countAdminAuditRows(t, adminAuditActionGrantBreakGlass); got != 1 {
		t.Fatalf("grant.break_glass audit rows = %d, want 1", got)
	}
	if got := countAdminAuditRows(t, adminAuditActionGrantConfirmed); got != 0 {
		t.Fatalf("a break-glass grant must not masquerade as a confirmed one (rows = %d)", got)
	}

	again, err := GrantDeploymentAdminByEmail(context.Background(), testPool, testHandler.Queries, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("second grant by email: %v", err)
	}
	if again.Created {
		t.Fatalf("second grant must be a no-op: %+v", again)
	}
	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("deployment_admin rows after re-grant = %d, want 1", got)
	}

	if _, err := GrantDeploymentAdminByEmail(context.Background(), testPool, testHandler.Queries,
		"nobody-"+randomID()[:8]+"@goosar.test"); !errors.Is(err, ErrDeploymentAdminUserNotFound) {
		t.Fatalf("unknown email: err = %v, want ErrDeploymentAdminUserNotFound", err)
	}
}

func TestListDeploymentAdminPending_ServesFiledRequests(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentAdminTestRoutes()

	_, targetEmail := deploymentUserFixture(t, "pending-list")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/api/deployment/admins",
		map[string]any{"email": targetEmail}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST admins: status = %d, want 202: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/api/deployment/admins/pending", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET admins/pending: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var list []DeploymentAdminPendingResponse
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("pending entries = %d, want 1: %s", len(list), w.Body.String())
	}
	got := list[0]
	if got.Action != "grant" || got.TargetEmail != targetEmail {
		t.Fatalf("unexpected pending entry: %+v", got)
	}
	if got.ConfirmHint == "" || got.RequestID == "" {
		t.Fatalf("pending entry must carry the request id and confirm hint: %+v", got)
	}
}
