package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/pkg/agent"
)

func TestQuickCreateIssueParentTrustBoundary(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE id = $1`,
		agentID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch agent runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET metadata = jsonb_build_object('cli_version', $1::text) WHERE id = $2`,
		agent.MinQuickCreateFieldsCLIVersion, runtimeID,
	); err != nil {
		t.Fatalf("bump runtime cli_version: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE agent_runtime SET metadata = '{}'::jsonb WHERE id = $1`, runtimeID)
	})

	var localParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'quick-create parent (local)', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&localParentID); err != nil {
		t.Fatalf("create local parent issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, localParentID)
	})

	var foreignWorkspaceID, foreignUserID, foreignParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "QuickCreate Foreign", "quickcreate-foreign@goosar.ru").Scan(&foreignUserID); err != nil {
		t.Fatalf("create foreign user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "QuickCreate Foreign WS", "quickcreate-foreign-ws", "", "QCF").Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'quick-create parent (foreign)', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, foreignWorkspaceID, foreignUserID).Scan(&foreignParentID); err != nil {
		t.Fatalf("create foreign parent issue: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, foreignParentID)
	})

	countQuickCreateTasks := func(t *testing.T) int {
		t.Helper()
		var count int
		if err := testPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1 AND context->>'type' = 'quick_create'`,
			agentID,
		).Scan(&count); err != nil {
			t.Fatalf("count quick-create tasks: %v", err)
		}
		return count
	}

	t.Run("same workspace parent enqueues with context", func(t *testing.T) {
		attachmentID := "019ec09d-6222-722b-bdfa-427b105d80be"
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":        agentID,
			"prompt":          "Create a follow-up issue for the local parent",
			"priority":        " HIGH ",
			"due_date":        " 2026-08-01 ",
			"parent_issue_id": localParentID,
			"attachment_ids":  []string{attachmentID},
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
		}
		var resp QuickCreateIssueResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
		})

		var contextJSON []byte
		if err := testPool.QueryRow(context.Background(),
			`SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID,
		).Scan(&contextJSON); err != nil {
			t.Fatalf("load task context: %v", err)
		}
		var qc service.QuickCreateContext
		if err := json.Unmarshal(contextJSON, &qc); err != nil {
			t.Fatalf("unmarshal context: %v", err)
		}
		if qc.Type != service.QuickCreateContextType {
			t.Fatalf("expected type=%q, got %q", service.QuickCreateContextType, qc.Type)
		}
		if qc.ParentIssueID != localParentID {
			t.Fatalf("expected parent_issue_id=%q in context, got %q", localParentID, qc.ParentIssueID)
		}
		if qc.Priority != "high" {
			t.Fatalf("expected priority=high in context, got %q", qc.Priority)
		}
		if qc.DueDate != "2026-08-01" {
			t.Fatalf("expected due_date=2026-08-01 in context, got %q", qc.DueDate)
		}
		if len(qc.AttachmentIDs) != 1 || qc.AttachmentIDs[0] != attachmentID {
			t.Fatalf("expected attachment_ids=[%q] in context, got %#v", attachmentID, qc.AttachmentIDs)
		}
	})

	t.Run("foreign workspace parent is rejected", func(t *testing.T) {
		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":        agentID,
			"prompt":          "Try to smuggle a foreign parent",
			"parent_issue_id": foreignParentID,
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for foreign parent, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {

			t.Fatalf("foreign parent must not enqueue a task: expected %d quick-create tasks, got %d", before, got)
		}
	})

	t.Run("bogus uuid parent is rejected", func(t *testing.T) {
		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":        agentID,
			"prompt":          "Try a malformed parent UUID",
			"parent_issue_id": "not-a-uuid",
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bogus parent, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {
			t.Fatalf("bogus parent must not enqueue a task: expected %d quick-create tasks, got %d", before, got)
		}
	})

	t.Run("bogus uuid attachment id is rejected", func(t *testing.T) {
		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id":       agentID,
			"prompt":         "Try a malformed attachment UUID",
			"attachment_ids": []string{"not-a-uuid"},
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bogus attachment id, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {
			t.Fatalf("bogus attachment id must not enqueue a task: expected %d quick-create tasks, got %d", before, got)
		}
	})

	for _, tc := range []struct {
		name  string
		field string
		value string
	}{
		{name: "invalid priority", field: "priority", value: "none"},
		{name: "invalid due date", field: "due_date", value: "tomorrow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := countQuickCreateTasks(t)
			body := map[string]any{
				"agent_id": agentID,
				"prompt":   "Try an invalid explicit field",
				tc.field:   tc.value,
			}
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/quick-create", body)
			testHandler.QuickCreateIssue(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if got := countQuickCreateTasks(t); got != before {
				t.Fatalf("invalid field must not enqueue a task: expected %d, got %d", before, got)
			}
		})
	}

	t.Run("legacy daemon only rejects requests using explicit fields", func(t *testing.T) {
		if _, err := testPool.Exec(ctx,
			`UPDATE agent_runtime SET metadata = jsonb_build_object('cli_version', '0.4.2') WHERE id = $1`,
			runtimeID,
		); err != nil {
			t.Fatalf("set legacy daemon version: %v", err)
		}
		defer testPool.Exec(ctx,
			`UPDATE agent_runtime SET metadata = jsonb_build_object('cli_version', $1::text) WHERE id = $2`,
			agent.MinQuickCreateFieldsCLIVersion, runtimeID,
		)

		before := countQuickCreateTasks(t)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id": agentID,
			"prompt":   "Explicit priority needs the new daemon transport",
			"priority": "high",
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("explicit fields on 0.4.2: expected 422, got %d: %s", w.Code, w.Body.String())
		}
		if got := countQuickCreateTasks(t); got != before {
			t.Fatalf("unsupported explicit fields must not enqueue: expected %d, got %d", before, got)
		}

		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/quick-create", map[string]any{
			"agent_id": agentID,
			"prompt":   "Basic quick create remains backward compatible",
		})
		testHandler.QuickCreateIssue(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("basic quick create on 0.4.2: expected 202, got %d: %s", w.Code, w.Body.String())
		}
		var resp QuickCreateIssueResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
		})
	})
}
