package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpdateIssue_RejectsProjectFromAnotherWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var otherWsID, foreignProjectID, ownProjectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'XWS') RETURNING id
	`, fmt.Sprintf("XwsProject-%d", suffix), fmt.Sprintf("xws-project-%d", suffix)).Scan(&otherWsID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWsID) })
	if err := testPool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, 'foreign') RETURNING id`, otherWsID).Scan(&foreignProjectID); err != nil {
		t.Fatalf("create foreign project: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, 'own') RETURNING id`, testWorkspaceID).Scan(&ownProjectID); err != nil {
		t.Fatalf("create own project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id IN ($1, $2)`, foreignProjectID, ownProjectID)
	})

	issueID := createTestIssue(t, "xws-project-issue", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	patch := func(projectID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"project_id": projectID}), "id", issueID)
		testHandler.UpdateIssue(w, req)
		return w
	}
	if w := patch(foreignProjectID); w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateIssue with a foreign project: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := patch(ownProjectID); w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue with an own project: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates":   map[string]any{"project_id": foreignProjectID},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		ProjectID *string `json:"project_id"`
	}
	gw := httptest.NewRecorder()
	testHandler.GetIssue(gw, withURLParam(newRequest("GET", "/api/issues/"+issueID, nil), "id", issueID))
	if gw.Code != http.StatusOK {
		t.Fatalf("GetIssue: %d: %s", gw.Code, gw.Body.String())
	}
	_ = json.NewDecoder(gw.Body).Decode(&got)
	if got.ProjectID == nil || *got.ProjectID != ownProjectID {
		t.Fatalf("after a batch with a foreign project the issue's project = %v, want the own project %s", got.ProjectID, ownProjectID)
	}
}
