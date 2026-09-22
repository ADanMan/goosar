package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateIssue_AgentCreate_StampsActingTaskOrigin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		 VALUES ($1, $2, 'running', 0, $3, $3) RETURNING id`,
		agentID, runtimeID, testUserID,
	).Scan(&taskID); err != nil {
		t.Fatalf("seed acting task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Agent-created via normal create (MUL-4305)",
	})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanup := withURLParam(newRequest("DELETE", "/api/issues/"+created.ID, nil), "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanup)
	})

	var originType, originID string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(origin_type, ''), COALESCE(origin_id::text, '') FROM issue WHERE id = $1`, created.ID,
	).Scan(&originType, &originID); err != nil {
		t.Fatalf("load issue origin: %v", err)
	}
	if originType != "agent_create" {
		t.Fatalf("origin_type = %q, want agent_create", originType)
	}
	if originID != taskID {
		t.Fatalf("origin_id = %q, want acting task %q", originID, taskID)
	}
}

func TestCreateIssue_NoAgentCreateStampForMemberOrForgedAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	assertNoAgentOrigin := func(t *testing.T, mutate func(*http.Request)) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": "No agent_create stamp expected (MUL-4305)",
		})
		if mutate != nil {
			mutate(req)
		}
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var created IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		t.Cleanup(func() {
			cleanup := withURLParam(newRequest("DELETE", "/api/issues/"+created.ID, nil), "id", created.ID)
			testHandler.DeleteIssue(httptest.NewRecorder(), cleanup)
		})
		var originType string
		if err := testPool.QueryRow(ctx,
			`SELECT COALESCE(origin_type, '') FROM issue WHERE id = $1`, created.ID,
		).Scan(&originType); err != nil {
			t.Fatalf("load issue origin: %v", err)
		}
		if originType != "" {
			t.Fatalf("origin_type = %q, want empty (no agent_create stamp)", originType)
		}
	}

	t.Run("plain member create", func(t *testing.T) {
		assertNoAgentOrigin(t, nil)
	})

	t.Run("forged X-Agent-ID without X-Task-ID", func(t *testing.T) {

		assertNoAgentOrigin(t, func(req *http.Request) {
			req.Header.Set("X-Agent-ID", agentID)
		})
	})
}
