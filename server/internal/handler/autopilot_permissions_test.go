package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createPlainMember(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('AP Perm Member', $1) RETURNING id`,
		email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return userID
}

func createAutopilotAs(t *testing.T, userID, title string) string {
	t.Helper()
	agentID := createHandlerTestAgent(t, title+"-agent", nil)

	body := map[string]any{
		"title":          title,
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	}
	w := httptest.NewRecorder()
	path := "/api/autopilots?workspace_id=" + testWorkspaceID
	var r *http.Request
	if userID == "" {
		r = newRequest("POST", path, body)
	} else {
		r = newRequestAs(userID, "POST", path, body)
	}
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ap AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&ap); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_collaborator WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})
	return ap.ID
}

func grantAutopilotAccess(t *testing.T, caller, apID, targetUserID string, wantStatus int) {
	t.Helper()
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apID + "/collaborators?workspace_id=" + testWorkspaceID
	body := map[string]any{"user_id": targetUserID}
	var r *http.Request
	if caller == "" {
		r = newRequest("POST", path, body)
	} else {
		r = newRequestAs(caller, "POST", path, body)
	}
	r = withURLParam(r, "id", apID)
	testHandler.AddAutopilotCollaborator(w, r)
	if w.Code != wantStatus {
		t.Fatalf("AddAutopilotCollaborator: expected %d, got %d: %s", wantStatus, w.Code, w.Body.String())
	}
}

func autopilotCanWrite(t *testing.T, caller, apID string) bool {
	t.Helper()
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apID + "?workspace_id=" + testWorkspaceID
	var r *http.Request
	if caller == "" {
		r = newRequest("GET", path, nil)
	} else {
		r = newRequestAs(caller, "GET", path, nil)
	}
	r = withURLParam(r, "id", apID)
	testHandler.GetAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Autopilot AutopilotResponse `json:"autopilot"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	if got.Autopilot.CanWrite == nil {
		t.Fatalf("expected can_write to be set on detail response")
	}
	return *got.Autopilot.CanWrite
}

func autopilotCanManageAccess(t *testing.T, caller, apID string) bool {
	t.Helper()
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apID + "?workspace_id=" + testWorkspaceID
	var r *http.Request
	if caller == "" {
		r = newRequest("GET", path, nil)
	} else {
		r = newRequestAs(caller, "GET", path, nil)
	}
	r = withURLParam(r, "id", apID)
	testHandler.GetAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Autopilot AutopilotResponse `json:"autopilot"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	if got.Autopilot.CanManageAccess == nil {
		t.Fatalf("expected can_manage_access to be set on detail response")
	}
	return *got.Autopilot.CanManageAccess
}

func TestAutopilotCollaborator_GrantedMemberCanWrite(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	apID := createAutopilotAs(t, "", "ap-collab-grant")
	member := createPlainMember(t, "ap-collab-grantee@goosar.test")

	updateAs := func(caller string) int {
		w := httptest.NewRecorder()
		r := newRequestAs(caller, "PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "edited by " + caller})
		r = withURLParam(r, "id", apID)
		testHandler.UpdateAutopilot(w, r)
		return w.Code
	}

	if code := updateAs(member); code != http.StatusForbidden {
		t.Fatalf("pre-grant update: expected 403, got %d", code)
	}
	if autopilotCanWrite(t, member, apID) {
		t.Fatalf("pre-grant: expected can_write=false for member")
	}

	grantAutopilotAccess(t, "", apID, member, http.StatusCreated)

	if !autopilotCanWrite(t, member, apID) {
		t.Fatalf("post-grant: expected can_write=true for collaborator")
	}
	if code := updateAs(member); code != http.StatusOK {
		t.Fatalf("post-grant update: expected 200, got %d", code)
	}

	w := httptest.NewRecorder()
	r := newRequest("DELETE", "/api/autopilots/"+apID+"/collaborators/"+member+"?workspace_id="+testWorkspaceID, nil)
	r = withURLParams(r, "id", apID, "userId", member)
	testHandler.RemoveAutopilotCollaborator(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("RemoveAutopilotCollaborator: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if code := updateAs(member); code != http.StatusForbidden {
		t.Fatalf("post-revoke update: expected 403, got %d", code)
	}
}

func TestAutopilotCollaborator_NonWriterCannotGrant(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	apID := createAutopilotAs(t, "", "ap-collab-guard")
	stranger := createPlainMember(t, "ap-collab-stranger@goosar.test")
	victim := createPlainMember(t, "ap-collab-victim@goosar.test")

	grantAutopilotAccess(t, stranger, apID, victim, http.StatusForbidden)

	grantAutopilotAccess(t, "", apID, "00000000-0000-0000-0000-000000000000", http.StatusBadRequest)
}

func TestAutopilotCollaborator_CannotManageAccessList(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	apID := createAutopilotAs(t, "", "ap-collab-noescalate")
	carol := createPlainMember(t, "ap-collab-carol@goosar.test")
	dave := createPlainMember(t, "ap-collab-dave@goosar.test")
	bob := createPlainMember(t, "ap-collab-bob2@goosar.test")

	grantAutopilotAccess(t, "", apID, carol, http.StatusCreated)
	grantAutopilotAccess(t, "", apID, dave, http.StatusCreated)

	w := httptest.NewRecorder()
	r := newRequestAs(carol, "PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "carol edit"})
	r = withURLParam(r, "id", apID)
	testHandler.UpdateAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("collaborator update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	grantAutopilotAccess(t, carol, apID, bob, http.StatusForbidden)

	w = httptest.NewRecorder()
	r = newRequestAs(carol, "DELETE", "/api/autopilots/"+apID+"/collaborators/"+dave+"?workspace_id="+testWorkspaceID, nil)
	r = withURLParams(r, "id", apID, "userId", dave)
	testHandler.RemoveAutopilotCollaborator(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("collaborator revoke peer: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	if autopilotCanManageAccess(t, carol, apID) {
		t.Fatalf("carol can_manage_access: expected false")
	}
	if !autopilotCanManageAccess(t, "", apID) {
		t.Fatalf("owner can_manage_access: expected true")
	}
}

func TestAutopilotWrite_PlainMemberCannotMutateOthers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	apID := createAutopilotAs(t, "", "ap-perm-owner-created")
	member := createPlainMember(t, "ap-perm-stranger@goosar.test")

	w := httptest.NewRecorder()
	r := newRequestAs(member, "PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "hijacked"})
	r = withURLParam(r, "id", apID)
	testHandler.UpdateAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateAutopilot by stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = newRequestAs(member, "POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("TriggerAutopilot by stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = newRequestAs(member, "DELETE", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.DeleteAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteAutopilot by stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutopilotWrite_CreatorCanMutateOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	member := createPlainMember(t, "ap-perm-creator@goosar.test")
	apID := createAutopilotAs(t, member, "ap-perm-member-created")

	w := httptest.NewRecorder()
	r := newRequestAs(member, "PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "creator edit"})
	r = withURLParam(r, "id", apID)
	testHandler.UpdateAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot by creator: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutopilotWrite_AdminCanMutateMembersAutopilot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	member := createPlainMember(t, "ap-perm-admin-target@goosar.test")
	apID := createAutopilotAs(t, member, "ap-perm-admin-target")

	w := httptest.NewRecorder()
	r := newRequest("PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "admin edit"})
	r = withURLParam(r, "id", apID)
	testHandler.UpdateAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot by owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutopilotWrite_WebhookSecretRedactedForNonWriter(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	apID := createAutopilotAs(t, "", "ap-perm-secret")
	stranger := createPlainMember(t, "ap-perm-secret-stranger@goosar.test")

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/triggers?workspace_id="+testWorkspaceID, map[string]any{"kind": "webhook"})
	r = withURLParam(r, "id", apID)
	testHandler.CreateAutopilotTrigger(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTrigger: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	type getResp struct {
		Triggers []AutopilotTriggerResponse `json:"triggers"`
	}

	w = httptest.NewRecorder()
	r = withURLParam(newRequest("GET", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, nil), "id", apID)
	testHandler.GetAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ownerView getResp
	if err := json.NewDecoder(w.Body).Decode(&ownerView); err != nil {
		t.Fatalf("decode owner view: %v", err)
	}
	if len(ownerView.Triggers) != 1 {
		t.Fatalf("owner view: expected 1 trigger, got %d", len(ownerView.Triggers))
	}
	if ownerView.Triggers[0].WebhookToken == nil || *ownerView.Triggers[0].WebhookToken == "" {
		t.Fatalf("owner view: expected webhook_token to be present")
	}
	if ownerView.Triggers[0].WebhookPath == nil {
		t.Fatalf("owner view: expected webhook_path to be present")
	}

	w = httptest.NewRecorder()
	r = withURLParam(newRequestAs(stranger, "GET", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, nil), "id", apID)
	testHandler.GetAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot as stranger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var strangerView getResp
	if err := json.NewDecoder(w.Body).Decode(&strangerView); err != nil {
		t.Fatalf("decode stranger view: %v", err)
	}
	if len(strangerView.Triggers) != 1 {
		t.Fatalf("stranger view: expected 1 trigger, got %d", len(strangerView.Triggers))
	}
	if strangerView.Triggers[0].Kind != "webhook" {
		t.Fatalf("stranger view: expected webhook trigger to remain visible, got kind %q", strangerView.Triggers[0].Kind)
	}
	if strangerView.Triggers[0].WebhookToken != nil {
		t.Fatalf("stranger view: webhook_token leaked: %v", *strangerView.Triggers[0].WebhookToken)
	}
	if strangerView.Triggers[0].WebhookPath != nil {
		t.Fatalf("stranger view: webhook_path leaked: %v", *strangerView.Triggers[0].WebhookPath)
	}
	if strangerView.Triggers[0].WebhookURL != nil {
		t.Fatalf("stranger view: webhook_url leaked: %v", *strangerView.Triggers[0].WebhookURL)
	}
}
