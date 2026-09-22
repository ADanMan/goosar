package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/middleware"
)

func setupPerimeterGrantFixture(t *testing.T) (adminUserID, adminMemberID, plainUserID, plainMemberID, ownerMemberID string) {
	t.Helper()
	ctx := context.Background()

	createUser := func(name, email, role string) (userID, memberID string) {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO "user" (name, email)
			VALUES ($1, $2)
			RETURNING id
		`, name, email).Scan(&userID); err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
			testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
		})
		if err := testPool.QueryRow(ctx, `
			INSERT INTO member (workspace_id, user_id, role)
			VALUES ($1, $2, $3)
			RETURNING id
		`, testWorkspaceID, userID, role).Scan(&memberID); err != nil {
			t.Fatalf("add member %s: %v", email, err)
		}
		return userID, memberID
	}

	adminUserID, adminMemberID = createUser("Perimeter Admin", "perimeter-admin@goosar.test", "admin")
	plainUserID, plainMemberID = createUser("Perimeter Plain", "perimeter-plain@goosar.test", "member")

	if err := testPool.QueryRow(ctx, `
		SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&ownerMemberID); err != nil {
		t.Fatalf("resolve owner member row: %v", err)
	}
	return adminUserID, adminMemberID, plainUserID, plainMemberID, ownerMemberID
}

func patchMemberAs(t *testing.T, userID, memberID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAs(userID, "PATCH", "/api/workspaces/"+testWorkspaceID+"/members/"+memberID, body)
	req = withURLParams(req, "id", testWorkspaceID, "memberId", memberID)
	testHandler.UpdateMember(w, req)
	return w
}

func decodeMemberResponse(t *testing.T, w *httptest.ResponseRecorder) MemberWithUserResponse {
	t.Helper()
	var resp MemberWithUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode member response: %v (%s)", err, w.Body.String())
	}
	return resp
}

func TestUpdateMemberPerimeterAccessGrantPersists(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	_, _, _, plainMemberID, _ := setupPerimeterGrantFixture(t)

	w := patchMemberAs(t, testUserID, plainMemberID, map[string]any{"perimeter_access": true})
	if w.Code != http.StatusOK {
		t.Fatalf("grant: want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMemberResponse(t, w)
	if resp.PerimeterAccess != true {
		t.Errorf("grant response: want perimeter_access=true, got %v", resp.PerimeterAccess)
	}
	if resp.Role != "member" {
		t.Errorf("grant must not change role: want member, got %q", resp.Role)
	}

	lw := httptest.NewRecorder()
	lreq := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/members", nil)
	lreq = withURLParam(lreq, "id", testWorkspaceID)
	testHandler.ListMembersWithUser(lw, lreq)
	if lw.Code != http.StatusOK {
		t.Fatalf("list members: want 200, got %d: %s", lw.Code, lw.Body.String())
	}
	var list []MemberWithUserResponse
	if err := json.Unmarshal(lw.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode member list: %v", err)
	}
	found := false
	for _, m := range list {
		if m.ID == plainMemberID {
			found = true
			if m.PerimeterAccess != true {
				t.Errorf("list: want perimeter_access=true for granted member, got %v", m.PerimeterAccess)
			}
		}
	}
	if !found {
		t.Fatalf("granted member %s missing from list", plainMemberID)
	}

	w = patchMemberAs(t, testUserID, plainMemberID, map[string]any{"perimeter_access": false})
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decodeMemberResponse(t, w); resp.PerimeterAccess != false {
		t.Errorf("revoke response: want perimeter_access=false, got %v", resp.PerimeterAccess)
	}
}

func TestUpdateMemberPerimeterAccessTriState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	_, _, _, plainMemberID, _ := setupPerimeterGrantFixture(t)

	if w := patchMemberAs(t, testUserID, plainMemberID, map[string]any{"perimeter_access": true}); w.Code != http.StatusOK {
		t.Fatalf("grant: want 200, got %d: %s", w.Code, w.Body.String())
	}

	w := patchMemberAs(t, testUserID, plainMemberID, map[string]any{"role": "admin"})
	if w.Code != http.StatusOK {
		t.Fatalf("role change: want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMemberResponse(t, w)
	if resp.Role != "admin" {
		t.Errorf("role change: want admin, got %q", resp.Role)
	}
	if resp.PerimeterAccess != true {
		t.Errorf("role-only PATCH must not clear the grant, got perimeter_access=%v", resp.PerimeterAccess)
	}

	w = patchMemberAs(t, testUserID, plainMemberID, map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty PATCH: want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMemberCombinedRoleAndPerimeterPatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	_, _, plainUserID, plainMemberID, _ := setupPerimeterGrantFixture(t)

	w := patchMemberAs(t, testUserID, plainMemberID, map[string]any{
		"role":             "admin",
		"perimeter_access": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("combined PATCH: want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMemberResponse(t, w)
	if resp.Role != "admin" {
		t.Errorf("combined PATCH: want role=admin, got %q", resp.Role)
	}
	if resp.PerimeterAccess != true {
		t.Errorf("combined PATCH: want perimeter_access=true, got %v", resp.PerimeterAccess)
	}

	var role string
	var granted bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT role, perimeter_access FROM member WHERE id = $1`, plainMemberID).Scan(&role, &granted); err != nil {
		t.Fatalf("read member: %v", err)
	}
	if role != "admin" || !granted {
		t.Errorf("combined PATCH persisted (role=%q granted=%v), want (admin, true)", role, granted)
	}

	w = patchMemberAs(t, plainUserID, plainMemberID, map[string]any{
		"role":             "owner",
		"perimeter_access": false,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("combined PATCH promoting to owner by non-owner: want 403, got %d: %s", w.Code, w.Body.String())
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT role, perimeter_access FROM member WHERE id = $1`, plainMemberID).Scan(&role, &granted); err != nil {
		t.Fatalf("re-read member: %v", err)
	}
	if role != "admin" || !granted {
		t.Errorf("rejected combined PATCH must not write (role=%q granted=%v), want (admin, true)", role, granted)
	}
}

func TestUpdateMemberPerimeterAccessOwnerProtection(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	adminUserID, adminMemberID, _, _, ownerMemberID := setupPerimeterGrantFixture(t)

	w := patchMemberAs(t, adminUserID, ownerMemberID, map[string]any{"perimeter_access": true})
	if w.Code != http.StatusForbidden {
		t.Errorf("admin changing owner grant: want 403, got %d: %s", w.Code, w.Body.String())
	}

	var granted bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT perimeter_access FROM member WHERE id = $1`, ownerMemberID).Scan(&granted); err != nil {
		t.Fatalf("read owner grant: %v", err)
	}
	if granted {
		t.Errorf("owner grant flipped despite 403")
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE member SET perimeter_access = false WHERE id = $1`, ownerMemberID)
	})

	if w := patchMemberAs(t, adminUserID, adminMemberID, map[string]any{"perimeter_access": true}); w.Code != http.StatusOK {
		t.Errorf("admin self-grant: want 200, got %d: %s", w.Code, w.Body.String())
	}

	w = patchMemberAs(t, testUserID, ownerMemberID, map[string]any{"perimeter_access": true})
	if w.Code != http.StatusOK {
		t.Errorf("owner self-grant: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMemberPerimeterAccessRouteRequiresAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	adminUserID, _, plainUserID, plainMemberID, _ := setupPerimeterGrantFixture(t)

	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Patch("/members/{memberId}", testHandler.UpdateMember)
		})
	})

	exercise := func(userID string) *httptest.ResponseRecorder {
		req := newRequestAs(userID, "PATCH",
			"/api/workspaces/"+testWorkspaceID+"/members/"+plainMemberID,
			map[string]any{"perimeter_access": true})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := exercise(plainUserID); rec.Code != http.StatusForbidden {
		t.Errorf("member PATCH: want 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := exercise(adminUserID); rec.Code != http.StatusOK {
		t.Errorf("admin PATCH: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
