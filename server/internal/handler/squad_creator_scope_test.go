package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func squadScopeReq(userID, method, path string, body any, params map[string]string) *http.Request {
	var req *http.Request
	if userID == "" {
		req = newRequest(method, path, body)
	} else {
		req = newRequestAs(userID, method, path, body)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", testWorkspaceID)
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func createSquadAs(t *testing.T, userID, name, leaderID string) SquadResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := squadScopeReq(userID, "POST", "/api/squads", map[string]any{
		"name":      name,
		"leader_id": leaderID,
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
	return resp
}

func TestCreateSquad_PlainMemberBecomesCreator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "squad-creator@goosar.test")
	leaderID := createHandlerTestAgent(t, "squad-creator-leader", nil)

	squad := createSquadAs(t, memberID, "Member Owned Squad", leaderID)
	if squad.CreatorID != memberID {
		t.Fatalf("expected creator_id=%s, got %s", memberID, squad.CreatorID)
	}
}

func TestManageSquad_CreatorCanManageOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "squad-owner-manage@goosar.test")
	leaderID := createHandlerTestAgent(t, "squad-owner-manage-leader", nil)
	worker := createHandlerTestAgent(t, "squad-owner-manage-worker", nil)

	squad := createSquadAs(t, memberID, "Manage Own Squad", leaderID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq(memberID, "PATCH", "/api/squads", map[string]any{
		"name": "Renamed By Creator",
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as creator: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq(memberID, "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   worker,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember as creator: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.DeleteSquad(w, squadScopeReq(memberID, "DELETE", "/api/squads", nil,
		map[string]string{"id": squad.ID}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSquad as creator: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManageSquad_StrangerMemberForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "squad-stranger-creator@goosar.test")
	strangerID := createPlainMember(t, "squad-stranger-other@goosar.test")
	leaderID := createHandlerTestAgent(t, "squad-stranger-leader", nil)

	squad := createSquadAs(t, creatorID, "Stranger Test Squad", leaderID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq(strangerID, "PATCH", "/api/squads", map[string]any{
		"name": "Hijacked",
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSquad as stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.DeleteSquad(w, squadScopeReq(strangerID, "DELETE", "/api/squads", nil,
		map[string]string{"id": squad.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteSquad as stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq("", "PATCH", "/api/squads", map[string]any{
		"name": "Renamed By Admin",
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddSquadMember_CreatorAgentAccessGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicLeaderID := createHandlerTestAgent(t, "squad-gate-leader", nil)
	publicWorkerID := createHandlerTestAgent(t, "squad-gate-worker", nil)

	squad := createSquadAs(t, memberID, "Agent Gate Squad", publicLeaderID)

	w := httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq(memberID, "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   publicWorkerID,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember public agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq(memberID, "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   privateAgentID,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("AddSquadMember private agent as creator: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq("", "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   privateAgentID,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember private agent as owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSquad_CreatorPrivateLeaderForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, memberID := privateAgentTestFixture(t)

	w := httptest.NewRecorder()
	r := squadScopeReq(memberID, "POST", "/api/squads", map[string]any{
		"name":      "Private Leader Squad",
		"leader_id": privateAgentID,
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusForbidden {

		if w.Code == http.StatusCreated {
			var resp SquadResponse
			if json.NewDecoder(w.Body).Decode(&resp) == nil {
				testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
				testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
			}
		}
		t.Fatalf("CreateSquad with private leader: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
