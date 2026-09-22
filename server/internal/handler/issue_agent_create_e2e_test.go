package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createPrivateAgentOwnedBy(t *testing.T, name, ownerID string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb,
		        $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create private agent %q: %v", name, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })
	return agentID
}

func TestAgentCreateOriginator_E2E_CreateAssignSquad_PrivateWorkerTriggered(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	workerJID, ownerH, _ := privateAgentTestFixture(t)

	leaderID := createPrivateAgentOwnedBy(t, "mul4305-e2e-private-leader", ownerH)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'MUL-4305 E2E Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	creatorAID := createHandlerTestAgent(t, "mul4305-e2e-creator-agent", nil)
	var creatorTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $2)
		RETURNING id
	`, creatorAID, ownerH).Scan(&creatorTaskID); err != nil {
		t.Fatalf("create A's acting task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, creatorTaskID)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "MUL-4305 E2E agent-created + squad-assigned",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", creatorAID)
	r.Header.Set("X-Task-ID", creatorTaskID)
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	var originType, originID string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(origin_type, ''), COALESCE(origin_id::text, '') FROM issue WHERE id = $1`, created.ID,
	).Scan(&originType, &originID); err != nil {
		t.Fatalf("load issue origin: %v", err)
	}
	if originType != "agent_create" || originID != creatorTaskID {
		t.Fatalf("issue origin = (%q,%q), want (agent_create,%s)", originType, originID, creatorTaskID)
	}

	var leaderTaskID, leaderOriginator string
	if err := testPool.QueryRow(ctx, `
		SELECT id, COALESCE(originator_user_id::text, '')
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND is_leader_task
		ORDER BY created_at DESC LIMIT 1
	`, created.ID, leaderID).Scan(&leaderTaskID, &leaderOriginator); err != nil {
		t.Fatalf("load squad-leader task (expected one enqueued on create): %v", err)
	}
	if leaderOriginator != ownerH {
		t.Fatalf("squad-leader task originator = %q, want the original human H %q", leaderOriginator, ownerH)
	}

	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues/"+created.ID+"/comments", map[string]any{
		"content": "handing the private part to [@Worker](mention://agent/" + workerJID + ")",
	})
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", leaderTaskID)

	r.Header.Set("X-Actor-Source", "task_token")
	r = withURLParam(r, "id", created.ID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment (leader mentions worker): expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var queuedForHuman int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND originator_user_id = $3
	`, created.ID, workerJID, ownerH).Scan(&queuedForHuman); err != nil {
		t.Fatalf("count worker tasks: %v", err)
	}
	if queuedForHuman == 0 {
		t.Fatalf("private worker got 0 queued tasks attributed to H; the A2A mention was denied (MUL-4305 regression)")
	}
}

func TestAgentCreateOriginator_E2E_UpdateAssignSquad_HandlerGateAdmitsPrivateLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID, ownerH, _ := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'MUL-4305 E2E Update-Assign Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	creatorAID := createHandlerTestAgent(t, "mul4305-e2e-update-creator", nil)
	var creatorTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $2)
		RETURNING id
	`, creatorAID, ownerH).Scan(&creatorTaskID); err != nil {
		t.Fatalf("create A's acting task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, creatorTaskID)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "MUL-4305 E2E unassigned then squad-assigned",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", creatorAID)
	r.Header.Set("X-Task-ID", creatorTaskID)
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	w = httptest.NewRecorder()
	r = newRequest("PATCH", "/api/issues/"+created.ID, map[string]any{
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", creatorAID)
	r.Header.Set("X-Task-ID", creatorTaskID)
	r = withURLParam(r, "id", created.ID)
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue (assign squad): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var leaderCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND is_leader_task AND originator_user_id = $3
	`, created.ID, leaderID, ownerH).Scan(&leaderCount); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderCount == 0 {
		t.Fatalf("private squad leader got 0 tasks attributed to H after agent-triggered assign; the enqueue gate denied it (MUL-4305 gate regression)")
	}
}
