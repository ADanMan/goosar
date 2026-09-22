package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBatchUpdateNoMutationReturnsZero(t *testing.T) {

	a := createTestIssue(t, "BU-no-mut A", "todo", "low")
	b := createTestIssue(t, "BU-no-mut B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })

	cases := []struct {
		desc string
		body map[string]any
	}{
		{
			desc: "updates_missing",

			body: map[string]any{"issue_ids": []string{a, b}, "status": "in_progress"},
		},
		{
			desc: "updates_empty_object",
			body: map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{}},
		},
		{
			desc: "updates_misnamed",

			body: map[string]any{"issue_ids": []string{a, b}, "update": map[string]any{"status": "done"}},
		},
		{
			desc: "updates_unknown_field_only",

			body: map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{"foo": "bar"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/batch-update", tc.body)
			testHandler.BatchUpdateIssues(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Updated int `json:"updated"`
			}
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Updated != 0 {
				t.Errorf("expected updated=0 when no mutation field present, got %d", resp.Updated)
			}

			for _, id := range []string{a, b} {
				gw := httptest.NewRecorder()
				gr := newRequest("GET", "/api/issues/"+id, nil)
				gr = withURLParam(gr, "id", id)
				testHandler.GetIssue(gw, gr)
				var got IssueResponse
				json.NewDecoder(gw.Body).Decode(&got)
				if got.Status != "todo" {
					t.Errorf("issue %s: status changed to %q despite no-mutation request", id, got.Status)
				}
			}
		})
	}
}

func TestBatchUpdateValidUpdatesPersistAndCount(t *testing.T) {
	a := createTestIssue(t, "BU-ok A", "todo", "low")
	b := createTestIssue(t, "BU-ok B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{a, b},
		"updates":   map[string]any{"status": "in_progress"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 2 {
		t.Errorf("expected updated=2, got %d", resp.Updated)
	}
	for _, id := range []string{a, b} {
		gw := httptest.NewRecorder()
		gr := newRequest("GET", "/api/issues/"+id, nil)
		gr = withURLParam(gr, "id", id)
		testHandler.GetIssue(gw, gr)
		var got IssueResponse
		json.NewDecoder(gw.Body).Decode(&got)
		if got.Status != "in_progress" {
			t.Errorf("issue %s: expected status=in_progress, got %q", id, got.Status)
		}
	}
}

func TestBatchUpdateStageOnly(t *testing.T) {
	a := createTestIssue(t, "BU-stage A", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{a},
		"updates":   map[string]any{"stage": 2},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 1 {
		t.Fatalf("expected updated=1 for a stage-only batch update, got %d", resp.Updated)
	}

	gw := httptest.NewRecorder()
	gr := newRequest("GET", "/api/issues/"+a, nil)
	gr = withURLParam(gr, "id", a)
	testHandler.GetIssue(gw, gr)
	var got IssueResponse
	json.NewDecoder(gw.Body).Decode(&got)
	if got.Stage == nil || *got.Stage != 2 {
		t.Errorf("expected stage=2 to persist, got %v", got.Stage)
	}
}

func createTestIssue(t *testing.T, title, status, priority string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   status,
		"priority": priority,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	return issue.ID
}

func deleteTestIssue(t *testing.T, id string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+id, nil)
	req = withURLParam(req, "id", id)
	testHandler.DeleteIssue(w, req)
}

type stagedBatchFixture struct {
	parent  IssueResponse
	agentID string
	stage1  []IssueResponse
	stage2  []IssueResponse
}

func newStagedBatchFixture(t *testing.T) stagedBatchFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}

	pw := httptest.NewRecorder()
	preq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "batch-stage parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(pw, preq)
	if pw.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", pw.Code, pw.Body.String())
	}
	var parent IssueResponse
	json.NewDecoder(pw.Body).Decode(&parent)

	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	setIssueAssigneeDirect(t, parent.ID, "agent", agentID)

	mkChild := func(stage int32) IssueResponse {
		cw := httptest.NewRecorder()
		creq := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":           "batch-stage child " + time.Now().Format(time.RFC3339Nano),
			"status":          "in_progress",
			"parent_issue_id": parent.ID,
		})
		testHandler.CreateIssue(cw, creq)
		if cw.Code != http.StatusCreated {
			t.Fatalf("create child: expected 201, got %d: %s", cw.Code, cw.Body.String())
		}
		var child IssueResponse
		json.NewDecoder(cw.Body).Decode(&child)

		if _, err := testPool.Exec(context.Background(),
			`UPDATE issue SET stage = $2 WHERE id = $1`, child.ID, stage); err != nil {
			t.Fatalf("set child stage: %v", err)
		}
		return child
	}

	fx := stagedBatchFixture{parent: parent, agentID: agentID}
	fx.stage1 = []IssueResponse{mkChild(1), mkChild(1)}
	fx.stage2 = []IssueResponse{mkChild(2), mkChild(2)}

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, parent.ID)
		for _, c := range append(append([]IssueResponse{}, fx.stage1...), fx.stage2...) {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, c.ID)
		}
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	return fx
}

func batchSetStatus(t *testing.T, ids []string, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": ids,
		"updates":   map[string]any{"status": status},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

func systemCommentIDOn(t *testing.T, issueID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM comment
		   WHERE issue_id = $1 AND author_type = 'system'
		   ORDER BY created_at DESC LIMIT 1`,
		issueID,
	).Scan(&id); err != nil {
		t.Fatalf("read system comment id: %v", err)
	}
	return id
}

func triggerCommentIDForAgentTask(t *testing.T, issueID, agentID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT trigger_comment_id::text FROM agent_task_queue
		   WHERE issue_id = $1 AND agent_id = $2
		     AND status IN ('queued','dispatched','running')
		   ORDER BY created_at DESC LIMIT 1`,
		issueID, agentID,
	).Scan(&id); err != nil {
		t.Fatalf("read task trigger_comment_id: %v", err)
	}
	return id
}

func TestBatchChildDoneCrossStage_OneComment(t *testing.T) {
	assertFinal := func(t *testing.T, parentID, agentID string) {
		t.Helper()
		if got := countSystemCommentsOn(t, parentID); got != 1 {
			t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
		}
		content, _, _, _ := systemCommentOn(t, parentID)
		if !strings.Contains(content, "Stage 2 of this issue is complete") {
			t.Errorf("expected the comment to announce the top closed stage (Stage 2), got: %s", content)
		}
		if !strings.Contains(content, "Stage 1: 2/2 done; Stage 2: 2/2 done") {
			t.Errorf("expected the final-state stage summary, got: %s", content)
		}

		if strings.Contains(content, "is next") || strings.Contains(content, "(next)") {
			t.Errorf("comment must not carry a stale next-stage instruction, got: %s", content)
		}

		if got := countPendingTasksForAgent(t, parentID, agentID); got != 1 {
			t.Fatalf("expected exactly 1 pending parent task, got %d", got)
		}
		if trig, want := triggerCommentIDForAgentTask(t, parentID, agentID), systemCommentIDOn(t, parentID); trig != want {
			t.Errorf("parent wake pinned to %s, want the final comment %s", trig, want)
		}
	}

	t.Run("forward order [stage1, stage2]", func(t *testing.T) {
		fx := newStagedBatchFixture(t)
		batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID, fx.stage2[0].ID, fx.stage2[1].ID}, "done")
		assertFinal(t, fx.parent.ID, fx.agentID)
	})

	t.Run("reverse order [stage2, stage1]", func(t *testing.T) {
		fx := newStagedBatchFixture(t)
		batchSetStatus(t, []string{fx.stage2[0].ID, fx.stage2[1].ID, fx.stage1[0].ID, fx.stage1[1].ID}, "done")
		assertFinal(t, fx.parent.ID, fx.agentID)
	})
}

func TestBatchChildDoneCrossStage_Cancelled(t *testing.T) {
	fx := newStagedBatchFixture(t)
	batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID, fx.stage2[0].ID, fx.stage2[1].ID}, "cancelled")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 2 of this issue is complete") {
		t.Errorf("expected Stage 2 completion announcement, got: %s", content)
	}
	if strings.Contains(content, "is next") || strings.Contains(content, "(next)") {
		t.Errorf("comment must not carry a stale next-stage instruction, got: %s", content)
	}
}

func TestBatchChildDoneClosesLowerStageOnly(t *testing.T) {
	fx := newStagedBatchFixture(t)
	batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID}, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 1 of this issue is complete") {
		t.Errorf("expected Stage 1 completion announcement, got: %s", content)
	}
	if !strings.Contains(content, "Stage 2: 0/2 done (next)") {
		t.Errorf("expected accurate next-stage progress, got: %s", content)
	}
	if !strings.Contains(content, "Stage 2 is next") {
		t.Errorf("expected the advance-to-next-stage instruction, got: %s", content)
	}
}
