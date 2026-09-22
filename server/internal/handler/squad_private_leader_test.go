package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateIssue_SquadPrivateLeader_PlainMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Create Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Should be blocked",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateIssue_SquadPrivateLeader_PlainMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Update Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'update target')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "PATCH", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	r = withURLParam(r, "id", issueID)
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssue_SquadPrivateLeader_OwnerAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Owner Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Owner assigns private-leader squad",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})
}

func TestComment_SquadPrivateLeader_PlainMemberNoEnqueue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Comment Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'private leader comment test', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, squadID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "any update on this?",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private leader got %d queued tasks from plain member comment; want 0", count)
	}
}

func TestChildDone_SquadPrivateLeader_PlainMemberWakesLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader ChildDone Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "parent with private-leader squad",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE parent_issue_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, parent.ID)

	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child task",
		"parent_issue_id": parent.ID,
		"assignee_type":   "member",
		"assignee_id":     memberID,
		"status":          "in_progress",
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, child.ID)
	})

	w = httptest.NewRecorder()
	r = newRequestAs(memberID, "PATCH", "/api/issues/"+child.ID, map[string]any{
		"status": "done",
	})
	r = withURLParam(r, "id", child.ID)
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue (child done): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		parent.ID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count == 0 {
		t.Fatalf("private leader got 0 queued tasks from plain member child-done; want >=1")
	}
}

func TestChildDone_SquadPrivateLeader_AgentActorWakesLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, memberID := privateAgentTestFixture(t)
	workerAgentID := createHandlerTestAgent(t, "squad-private-leader-childdone-worker", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader ChildDone AgentActor Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "parent with private-leader squad (agent child-done)",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE parent_issue_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, parent.ID)

	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child task worked by an agent",
		"parent_issue_id": parent.ID,
		"assignee_type":   "agent",
		"assignee_id":     workerAgentID,
		"status":          "in_progress",
	})
	testHandler.CreateIssue(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, child.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, child.ID)
	})

	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $3, $3)
		RETURNING id
	`, workerAgentID, child.ID, memberID).Scan(&workerTaskID); err != nil {
		t.Fatalf("create worker task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, workerTaskID)
	})

	w = httptest.NewRecorder()
	r = newRequest("PATCH", "/api/issues/"+child.ID, map[string]any{
		"status": "done",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", workerAgentID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", child.ID)
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue (child done): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		parent.ID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count == 0 {
		t.Fatalf("private leader got 0 queued tasks from agent child-done; want >=1 (MUL-4063)")
	}
}

func TestComment_SquadPrivateLeader_AgentActorAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)
	otherAgentID := createHandlerTestAgent(t, "squad-private-leader-agent-actor", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Agent Actor Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'private leader agent actor test', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, squadID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $3, $3)
		RETURNING id
	`, otherAgentID, issueID, ownerID).Scan(&taskID); err != nil {
		t.Fatalf("create agent task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") agent reporting in",
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", otherAgentID)
	r.Header.Set("X-Task-ID", taskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count == 0 {
		t.Fatalf("private leader got 0 queued tasks; want >=1 (A2A originator is the leader owner)")
	}
}
