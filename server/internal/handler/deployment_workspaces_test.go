package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func deploymentWorkspacesTestRoutes() chi.Router {
	r := chi.NewRouter()
	r.Route("/api/deployment", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/workspaces", testHandler.ListDeploymentWorkspaces)
		r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(WorkspaceIDFromPathParam)
			r.Get("/members", testHandler.ListDeploymentWorkspaceMembers)
		})
	})
	return r
}

func TestDeploymentWorkspaceDirectory_RejectNonAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	r := deploymentWorkspacesTestRoutes()

	requests := []struct {
		name string
		req  *http.Request
	}{
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

func TestDeploymentWorkspaceDirectory_AdminSeesForeignWorkspaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	r := deploymentWorkspacesTestRoutes()

	adminID, _ := deploymentUserFixture(t, "directory")
	grantDeploymentAdminFixture(t, adminID)

	otherWorkspaceID, otherOwnerID := secondWorkspaceFixture(t)

	auditBefore := adminAuditCount(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequestAsUser(adminID, http.MethodGet, "/api/deployment/workspaces", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var workspaces []DeploymentWorkspaceEntry
	if err := json.NewDecoder(w.Body).Decode(&workspaces); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	byID := map[string]DeploymentWorkspaceEntry{}
	for _, ws := range workspaces {
		byID[ws.ID] = ws
	}
	foreign, ok := byID[otherWorkspaceID]
	if !ok {
		t.Fatalf("foreign workspace %s missing from the directory: %+v", otherWorkspaceID, workspaces)
	}
	if foreign.Name == "" || foreign.Slug == "" {
		t.Fatalf("foreign workspace entry incomplete: %+v", foreign)
	}
	if foreign.MemberCount != 1 {
		t.Fatalf("foreign workspace member_count = %d, want 1", foreign.MemberCount)
	}
	if _, ok := byID[testWorkspaceID]; !ok {
		t.Fatalf("shared test workspace missing from the directory")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequestAsUser(adminID, http.MethodGet,
		"/api/deployment/workspaces/"+otherWorkspaceID+"/members", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("members: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var members []DeploymentWorkspaceMemberEntry
	if err := json.NewDecoder(w.Body).Decode(&members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members length = %d, want 1: %+v", len(members), members)
	}
	got := members[0]
	if got.UserID != otherOwnerID {
		t.Fatalf("member user_id = %q, want %q", got.UserID, otherOwnerID)
	}
	if got.Role != "owner" {
		t.Fatalf("member role = %q, want owner", got.Role)
	}
	if got.Name == "" || got.Email == "" {
		t.Fatalf("member entry incomplete (card needs name+email): %+v", got)
	}

	if after := adminAuditCount(t); after != auditBefore {
		t.Fatalf("L0 directory reads wrote admin_audit rows: before=%d after=%d", auditBefore, after)
	}
}

func TestDeploymentWorkspaceMembers_BadAndUnknownWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)
	r := deploymentWorkspacesTestRoutes()

	auditBefore := adminAuditCount(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/api/deployment/workspaces/not-a-uuid/members", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed workspaceId: status = %d, want 400: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet,
		"/api/deployment/workspaces/00000000-0000-4000-8000-000000000243/members", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace: status = %d, want 404: %s", w.Code, w.Body.String())
	}

	if after := adminAuditCount(t); after != auditBefore {
		t.Fatalf("rejected reads wrote admin_audit rows: before=%d after=%d", auditBefore, after)
	}
}

func TestDeploymentWorkspaceMembers_ReportsDeactivatedState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "roster-deactivated")
	grantDeploymentAdminFixture(t, adminID)
	workspaceID, ownerID := secondWorkspaceFixture(t)

	routes := deploymentWorkspacesTestRoutes()
	rosterEntry := func() DeploymentWorkspaceMemberEntry {
		t.Helper()
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, newRequestAsUser(adminID, http.MethodGet,
			"/api/deployment/workspaces/"+workspaceID+"/members", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("members: status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var members []DeploymentWorkspaceMemberEntry
		if err := json.NewDecoder(w.Body).Decode(&members); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		for _, m := range members {
			if m.UserID == ownerID {
				return m
			}
		}
		t.Fatalf("owner %s missing from the roster: %+v", ownerID, members)
		return DeploymentWorkspaceMemberEntry{}
	}

	if rosterEntry().Deactivated {
		t.Fatal("a fresh account must not read as deactivated")
	}

	callUsers := chi.NewRouter()
	callUsers.Use(RequireHumanActor)
	callUsers.Post("/api/deployment/users/{userId}/deactivate", testHandler.DeactivateDeploymentUser)
	callUsers.Post("/api/deployment/users/{userId}/reactivate", testHandler.ReactivateDeploymentUser)

	w := httptest.NewRecorder()
	callUsers.ServeHTTP(w, newRequestAsUser(adminID, http.MethodPost,
		"/api/deployment/users/"+ownerID+"/deactivate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("deactivate: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !rosterEntry().Deactivated {
		t.Fatal("roster must report the account as deactivated after the endpoint ran")
	}

	w = httptest.NewRecorder()
	callUsers.ServeHTTP(w, newRequestAsUser(adminID, http.MethodPost,
		"/api/deployment/users/"+ownerID+"/reactivate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("reactivate: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if rosterEntry().Deactivated {
		t.Fatal("roster must report the account as active again after reactivation")
	}
}
