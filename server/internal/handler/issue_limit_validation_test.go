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

func TestListIssues_LimitValidation(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Limit Validation %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssue := func(title string) string {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5) RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}
	_ = insertIssue(fmt.Sprintf("limit-val-1-%d", suffix))
	_ = insertIssue(fmt.Sprintf("limit-val-2-%d", suffix))
	_ = insertIssue(fmt.Sprintf("limit-val-3-%d", suffix))

	type listResp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}

	call := func(query string) (int, listResp, string) {
		path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s%s",
			testWorkspaceID, projectID, query)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		var resp listResp
		body := w.Body.String()
		if w.Code == http.StatusOK {
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode list response (q=%q): %v\nbody: %s", query, err, body)
			}
		}
		return w.Code, resp, body
	}

	cases := []struct {
		name  string
		query string
	}{
		{"negative limit falls back to default", "&limit=-1"},
		{"negative offset falls back to 0", "&offset=-1"},
		{"negative limit and offset", "&limit=-1&offset=-1"},
		{"non-numeric limit falls back to default", "&limit=abc"},
		{"non-numeric offset falls back to default", "&offset=abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, body := call(tc.query)
			if code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", code, body)
			}

			if resp.Total != 3 {
				t.Fatalf("total: want 3, got %d", resp.Total)
			}
		})
	}

	t.Run("explicit limit below clamp is honored", func(t *testing.T) {
		code, resp, body := call("&limit=1")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if len(resp.Issues) != 1 {
			t.Fatalf("limit=1: want 1 issue, got %d", len(resp.Issues))
		}
		if resp.Total != 3 {
			t.Fatalf("total: want 3, got %d", resp.Total)
		}
	})

	t.Run("positive offset is honored", func(t *testing.T) {
		code, resp, body := call("&limit=2&offset=2")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if len(resp.Issues) != 1 {
			t.Fatalf("limit=2 offset=2: want 1 issue (the 3rd of 3), got %d", len(resp.Issues))
		}
		if resp.Total != 3 {
			t.Fatalf("total: want 3, got %d", resp.Total)
		}
	})
}

func TestListIssues_LimitClamp(t *testing.T) {
	const seeded = 101
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Limit Clamp %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssue := func(idx int) {
		title := fmt.Sprintf("clamp-%d-%d", suffix, idx)
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5)
		`, testWorkspaceID, title, testUserID, number, projectID); err != nil {
			t.Fatalf("create issue #%d: %v", idx, err)
		}
	}
	for i := 0; i < seeded; i++ {
		insertIssue(i)
	}

	type listResp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}

	call := func(query string) (int, listResp, string) {
		path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s%s",
			testWorkspaceID, projectID, query)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		var resp listResp
		body := w.Body.String()
		if w.Code == http.StatusOK {
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode list response (q=%q): %v\nbody: %s", query, err, body)
			}
		}
		return w.Code, resp, body
	}

	t.Run("no limit returns default page of 100", func(t *testing.T) {
		code, resp, body := call("")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if got := len(resp.Issues); got != 100 {
			t.Fatalf("default page size: want 100, got %d", got)
		}
		if resp.Total != seeded {
			t.Fatalf("total: want %d, got %d", seeded, resp.Total)
		}
	})

	clampCases := []struct {
		name  string
		query string
		want  int
	}{
		{"huge limit is clamped to 100", "&limit=100000000", 100},
		{"one above the clamp", "&limit=101", 100},
		{"well above the clamp", "&limit=200", 100},
		{"at the clamp boundary", "&limit=100", 100},
	}
	for _, tc := range clampCases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, body := call(tc.query)
			if code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", code, body)
			}
			if got := len(resp.Issues); got != tc.want {
				t.Fatalf("len(issues) for %q: want %d, got %d", tc.query, tc.want, got)
			}
			if resp.Total != seeded {
				t.Fatalf("total: want %d, got %d", seeded, resp.Total)
			}
		})
	}

	t.Run("limit below clamp is honored", func(t *testing.T) {
		code, resp, body := call("&limit=50")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if got := len(resp.Issues); got != 50 {
			t.Fatalf("limit=50: want 50 issues, got %d", got)
		}
		if resp.Total != seeded {
			t.Fatalf("total: want %d, got %d", seeded, resp.Total)
		}
	})

	t.Run("offset and clamp compose against the full result set", func(t *testing.T) {
		code, resp, body := call("&limit=200&offset=50")
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if got := len(resp.Issues); got != 51 {
			t.Fatalf("limit=200 offset=50: want 51 issues, got %d", got)
		}
		if resp.Total != seeded {
			t.Fatalf("total: want %d, got %d", seeded, resp.Total)
		}
	})
}
