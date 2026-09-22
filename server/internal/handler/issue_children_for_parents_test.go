package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type childrenBatchFixture struct {
	parentA   IssueResponse
	parentB   IssueResponse
	childrenA []IssueResponse
	childrenB []IssueResponse
}

func newChildrenBatchFixture(t *testing.T) childrenBatchFixture {
	t.Helper()

	mkIssue := func(title, status, parentID string) IssueResponse {
		w := httptest.NewRecorder()
		body := map[string]any{
			"title":  title + " " + time.Now().Format(time.RFC3339Nano),
			"status": status,
		}
		if parentID != "" {
			body["parent_issue_id"] = parentID
		}
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var out IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode %q: %v", title, err)
		}
		return out
	}

	parentA := mkIssue("children-batch parent A", "in_progress", "")
	parentB := mkIssue("children-batch parent B", "in_progress", "")
	a1 := mkIssue("children-batch a1", "todo", parentA.ID)
	a2 := mkIssue("children-batch a2", "done", parentA.ID)
	b1 := mkIssue("children-batch b1", "todo", parentB.ID)

	t.Cleanup(func() {
		ctx := context.Background()
		for _, id := range []string{a1.ID, a2.ID, b1.ID, parentA.ID, parentB.ID} {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
	})

	return childrenBatchFixture{
		parentA:   parentA,
		parentB:   parentB,
		childrenA: []IssueResponse{a1, a2},
		childrenB: []IssueResponse{b1},
	}
}

func decodeIssueBatch(t *testing.T, w *httptest.ResponseRecorder) []IssueResponse {
	t.Helper()
	var body struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Issues
}

func TestListChildrenByParents_ReturnsChildrenForBothParentsInOneCall(t *testing.T) {
	fx := newChildrenBatchFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+fx.parentA.ID+","+fx.parentB.ID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := decodeIssueBatch(t, w)
	if len(got) != 3 {
		t.Fatalf("expected 3 children, got %d", len(got))
	}

	wantParents := map[string]int{fx.parentA.ID: 2, fx.parentB.ID: 1}
	have := map[string]int{}
	for _, issue := range got {
		if issue.ParentIssueID == nil {
			t.Fatalf("child %q is missing parent_issue_id", issue.ID)
		}
		have[*issue.ParentIssueID]++
	}
	for parent, want := range wantParents {
		if have[parent] != want {
			t.Fatalf("parent %s: want %d children, got %d", parent, want, have[parent])
		}
	}
}

func TestListChildrenByParents_EmptyParentIdsReturnsEmptyList(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeIssueBatch(t, w); len(got) != 0 {
		t.Fatalf("expected 0 children, got %d", len(got))
	}
}

func TestListChildrenByParents_UnknownParentYieldsNoChildren(t *testing.T) {

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids=00000000-0000-0000-0000-000000000000", nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeIssueBatch(t, w); len(got) != 0 {
		t.Fatalf("expected 0 children, got %d", len(got))
	}
}

func TestListChildrenByParents_RejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids=not-a-uuid", nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListChildrenByParents_RejectsTooManyParents(t *testing.T) {

	ids := make([]string, listChildrenByParentsLimit+1)
	for i := range ids {
		ids[i] = "00000000-0000-0000-0000-000000000000"
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+strings.Join(ids, ","), nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListChildrenByParents_IgnoresForeignWorkspaceParents(t *testing.T) {
	ctx := context.Background()

	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Foreign Children Workspace", "foreign-children-"+uuid.New().String()[:8],
		"Cross-tenant test workspace", "FCW").Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("setup: create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE workspace_id = $1`, foreignWorkspaceID)
		testPool.Exec(context.Background(),
			`DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	var foreignParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, number, title, status, position, creator_type, creator_id)
		VALUES ($1, 1, 'foreign parent', 'todo', 1, 'member', $2)
		RETURNING id
	`, foreignWorkspaceID, testUserID).Scan(&foreignParentID); err != nil {
		t.Fatalf("setup: insert foreign parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (workspace_id, number, title, status, position, parent_issue_id, creator_type, creator_id)
		VALUES ($1, 2, 'foreign child', 'todo', 2, $2, 'member', $3)
	`, foreignWorkspaceID, foreignParentID, testUserID); err != nil {
		t.Fatalf("setup: insert foreign child: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+foreignParentID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeIssueBatch(t, w)
	if len(got) != 0 {
		t.Fatalf("expected 0 children (foreign workspace isolation), got %d (first child id=%s)",
			len(got), got[0].ID)
	}
}

func createChildIssue(t *testing.T, title, status, parentID string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	body := map[string]any{
		"title":  title + " " + time.Now().Format(time.RFC3339Nano),
		"status": status,
	}
	if parentID != "" {
		body["parent_issue_id"] = parentID
	}
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var out IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode %q: %v", title, err)
	}
	return out
}

func newScrambledChildren(t *testing.T) (IssueResponse, []IssueResponse) {
	t.Helper()

	parent := createChildIssue(t, "ordering parent", "in_progress", "")
	c1 := createChildIssue(t, "ordering c1", "todo", parent.ID)
	c2 := createChildIssue(t, "ordering c2", "in_progress", parent.ID)
	c3 := createChildIssue(t, "ordering c3", "done", parent.ID)
	c4 := createChildIssue(t, "ordering c4", "todo", parent.ID)
	children := []IssueResponse{c1, c2, c3, c4}

	t.Cleanup(func() {
		ctx := context.Background()
		for _, c := range append(children, parent) {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, c.ID)
		}
	})

	scrambled := map[string]float64{c1.ID: -1, c2.ID: -8, c3.ID: -4, c4.ID: -16}
	ctx := context.Background()
	for id, pos := range scrambled {
		if _, err := testPool.Exec(ctx,
			`UPDATE issue SET position = $1 WHERE id = $2`, pos, id); err != nil {
			t.Fatalf("scramble position for %s: %v", id, err)
		}
	}

	return parent, children
}

func assertNumberAscending(t *testing.T, got, want []IssueResponse) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d children, got %d", len(want), len(got))
	}
	for i := range got {
		if got[i].ID != want[i].ID {
			t.Fatalf("index %d: expected child %s (number %d), got %s (number %d)",
				i, want[i].ID, want[i].Number, got[i].ID, got[i].Number)
		}
		if i > 0 && got[i].Number <= got[i-1].Number {
			t.Fatalf("children not strictly ascending by number: index %d number %d <= index %d number %d",
				i, got[i].Number, i-1, got[i-1].Number)
		}
	}
}

func TestListChildIssues_OrdersByNumberAsc(t *testing.T) {
	parent, children := newScrambledChildren(t)

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("GET", "/api/issues/"+parent.ID+"/children", nil),
		"id", parent.ID,
	)
	testHandler.ListChildIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	assertNumberAscending(t, decodeIssueBatch(t, w), children)
}

func TestListChildrenByParents_OrdersByNumberAscWithinParent(t *testing.T) {
	parent, children := newScrambledChildren(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+parent.ID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	assertNumberAscending(t, decodeIssueBatch(t, w), children)
}

func TestListChildIssues_IncludesLabels(t *testing.T) {
	fx := newChildrenBatchFixture(t)
	labelID := createTestIssueLabel(t, "children-labels-"+uuid.NewString()[:8])

	labeled := fx.childrenA[0]
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/issues/"+labeled.ID+"/labels", map[string]any{
			"label_id": labelID,
		}),
		"id", labeled.ID,
	)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Fatalf("attach label: unexpected status %d: %s", w.Code, w.Body.String())
	}

	assertChildLabels := func(t *testing.T, got []IssueResponse) {
		t.Helper()
		for _, child := range got {
			if child.Labels == nil {
				t.Fatalf("child %s missing labels field", child.ID)
			}
			want := 0
			if child.ID == labeled.ID {
				want = 1
			}
			if len(*child.Labels) != want {
				t.Errorf("child %s: want %d labels, got %d", child.ID, want, len(*child.Labels))
			}
			if child.ID == labeled.ID && len(*child.Labels) == 1 && (*child.Labels)[0].ID != labelID {
				t.Errorf("child %s: want label %s, got %s", child.ID, labelID, (*child.Labels)[0].ID)
			}
		}
	}

	w = httptest.NewRecorder()
	req = withURLParam(
		newRequest("GET", "/api/issues/"+fx.parentA.ID+"/children", nil),
		"id", fx.parentA.ID,
	)
	testHandler.ListChildIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChildIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertChildLabels(t, decodeIssueBatch(t, w))

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/children?workspace_id="+testWorkspaceID+
		"&parent_ids="+fx.parentA.ID, nil)
	testHandler.ListChildrenByParents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChildrenByParents: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertChildLabels(t, decodeIssueBatch(t, w))
}
