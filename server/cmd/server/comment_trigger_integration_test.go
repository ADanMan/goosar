package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/auth"
)

func authRequestWithAgent(t *testing.T, method, path string, body any, agentID string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mintAgentTaskToken(t, agentID, ensureAgentTask(t, agentID)))
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return r
}

func mintAgentTaskToken(t *testing.T, agentID, taskID string) string {
	t.Helper()
	ctx := context.Background()

	token, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatalf("generate agent task token: %v", err)
	}
	hash := auth.HashToken(token)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour')
	`, hash, taskID, agentID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("insert task token: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_token WHERE token_hash = $1`, hash)
	})
	return token
}

func ensureAgentTask(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()
	var taskID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_task_queue WHERE agent_id = $1 LIMIT 1`,
		agentID,
	).Scan(&taskID); err == nil && taskID != "" {
		return taskID
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id::text FROM agent WHERE id = $1`,
		agentID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("ensureAgentTask: load runtime_id for agent %s: %v", agentID, err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'queued', 0)
		RETURNING id::text
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("ensureAgentTask: insert task for agent %s: %v", agentID, err)
	}
	return taskID
}

func countPendingTasks(t *testing.T, issueID string) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued', 'dispatched')`,
		issueID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count pending tasks: %v", err)
	}
	return count
}

func countPendingTasksForAgent(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`,
		issueID, agentID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count pending tasks for agent: %v", err)
	}
	return count
}

func clearTasks(t *testing.T, issueID string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	if err != nil {
		t.Fatalf("failed to clear tasks: %v", err)
	}
}

func latestTriggerCommentID(t *testing.T, issueID string) string {
	t.Helper()
	var triggerID *string
	err := testPool.QueryRow(context.Background(),
		`SELECT trigger_comment_id::text
		   FROM agent_task_queue
		  WHERE issue_id = $1 AND status IN ('queued', 'dispatched')
		  ORDER BY created_at DESC
		  LIMIT 1`,
		issueID).Scan(&triggerID)
	if err != nil {
		t.Fatalf("failed to fetch trigger_comment_id: %v", err)
	}
	if triggerID == nil {
		return ""
	}
	return *triggerID
}

func getAgentID(t *testing.T) string {
	t.Helper()
	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) == 0 {
		t.Fatal("no agents in test workspace")
	}
	return agents[0]["id"].(string)
}

func createSecondAgent(t *testing.T) string {
	t.Helper()

	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) == 0 {
		t.Fatal("no agents in test workspace")
	}
	runtimeID := agents[0]["runtime_id"].(string)

	resp = authRequest(t, "POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       fmt.Sprintf("Second Test Agent %d", time.Now().UnixNano()),
		"runtime_id": runtimeID,
		"visibility": "workspace",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateAgent: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var агент map[string]any
	readJSON(t, resp, &агент)
	id := агент["id"].(string)
	t.Cleanup(func() {
		authRequest(t, "POST", "/api/agents/"+id+"/archive?workspace_id="+testWorkspaceID, nil)
	})
	return id
}

func createIssueAssignedToAgent(t *testing.T, title, agentID string) string {
	t.Helper()
	resp := authRequest(t, "PUT", fmt.Sprintf("/api/issues/%s", createIssue(t, title)), map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	var тикет map[string]any
	readJSON(t, resp, &тикет)
	return тикет["id"].(string)
}

func createIssue(t *testing.T, title string) string {
	t.Helper()
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateIssue: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var тикет map[string]any
	readJSON(t, resp, &тикет)
	return тикет["id"].(string)
}

func postComment(t *testing.T, issueID, content string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"content": content,
		"type":    "comment",
	}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	resp := authRequest(t, "POST", "/api/issues/"+issueID+"/comments", body)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("postComment: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	return comment["id"].(string)
}

func postCommentAsAgent(t *testing.T, issueID, content, agentID string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"content": content,
		"type":    "comment",
	}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	resp := authRequestWithAgent(t, "POST", "/api/issues/"+issueID+"/comments", body, agentID)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("postCommentAsAgent: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	return comment["id"].(string)
}

func strPtr(s string) *string { return &s }

func TestCommentTriggerOnComment(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Comment trigger integration test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("top-level comment without mentions triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		postComment(t, issueID, "Please fix this bug", nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("top-level comment mentioning only others suppresses trigger", func(t *testing.T) {
		clearTasks(t, issueID)

		content := "[@SomeoneElse](mention://agent/00000000-0000-0000-0000-000000000001) what do you think?"
		postComment(t, issueID, content, nil)
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks, got %d", n)
		}
	})

	t.Run("top-level comment mentioning assignee triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)
		content := fmt.Sprintf("[@Agent](mention://agent/%s) fix this", agentID)
		postComment(t, issueID, content, nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("reply to agent thread without mentions triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)

		threadID := postCommentAsAgent(t, issueID, "I analyzed the issue.", agentID, nil)

		postComment(t, issueID, "Looks good, please proceed", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("reply records new comment id (not thread root) as trigger_comment_id", func(t *testing.T) {
		clearTasks(t, issueID)
		threadID := postCommentAsAgent(t, issueID, "First pass analysis.", agentID, nil)
		replyID := postComment(t, issueID, "Please also check the edge case", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Fatalf("expected 1 pending task, got %d", n)
		}
		if got := latestTriggerCommentID(t, issueID); got != replyID {
			t.Errorf("trigger_comment_id = %q, want reply id %q (thread root was %q)",
				got, replyID, threadID)
		}
	})

	t.Run("reply to member thread without mentions falls back to assignee", func(t *testing.T) {
		clearTasks(t, issueID)

		threadID := postComment(t, issueID, "Hey team, what do you think?", nil)

		clearTasks(t, issueID)

		postComment(t, issueID, "I agree with you", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (assignee fallback), got %d", n)
		}
	})

	t.Run("reply to member thread after agent replied triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)

		threadID := postComment(t, issueID, "Please fix this bug", nil)
		clearTasks(t, issueID)

		postCommentAsAgent(t, issueID, "Working on it, found the root cause.", agentID, strPtr(threadID))

		postComment(t, issueID, "Great, please also check the edge case", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (agent participated in thread), got %d", n)
		}
	})

	t.Run("reply to member thread mentioning assignee triggers agent", func(t *testing.T) {
		clearTasks(t, issueID)

		threadID := postComment(t, issueID, "Question about this", nil)
		clearTasks(t, issueID)

		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you help with this?", agentID)
		postComment(t, issueID, content, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {

		}
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (assignee mentioned in member thread), got %d", n)
		}
	})

	t.Run("reply to member thread that @mentioned assignee triggers without re-mention", func(t *testing.T) {
		clearTasks(t, issueID)

		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you review this?", agentID)
		threadID := postComment(t, issueID, content, nil)

		clearTasks(t, issueID)

		postComment(t, issueID, "Here is more context for you", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (assignee mentioned in thread root), got %d", n)
		}
	})
}

func TestCommentTriggerAtAllSuppression(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "@all suppression test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("top-level @all comment suppresses on_comment", func(t *testing.T) {
		clearTasks(t, issueID)
		postComment(t, issueID, "[@All](mention://all/all) heads up everyone", nil)
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (@all should not trigger agent), got %d", n)
		}
	})

	t.Run("@all in agent thread suppresses on_comment", func(t *testing.T) {
		clearTasks(t, issueID)
		threadID := postCommentAsAgent(t, issueID, "Here is my analysis.", agentID, nil)
		postComment(t, issueID, "[@All](mention://all/all) FYI for the team", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (@all in agent thread), got %d", n)
		}
	})
}

func TestCommentTriggerOnAssignNoStatusGate(t *testing.T) {
	agentID := getAgentID(t)

	issueID := createIssue(t, "On-assign status gate test")
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "in_progress",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	resp = authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("assign agent: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task after assigning to in_progress issue, got %d", n)
	}
}

func TestCommentTriggerOnMentionNoStatusGate(t *testing.T) {
	agentID := getAgentID(t)

	issueID := createIssue(t, "On-mention done issue test")
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	content := fmt.Sprintf("[@Agent](mention://agent/%s) found a problem here", agentID)
	postComment(t, issueID, content, nil)

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task after @mention on done issue, got %d", n)
	}
}

func TestCommentTriggerThreadExplicitMentions(t *testing.T) {
	agentID := getAgentID(t)

	issueID := createIssue(t, "Thread-inherited mention test")
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("plain reply in thread routes to root mention owner", func(t *testing.T) {
		clearTasks(t, issueID)

		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you review this?", agentID)
		threadID := postComment(t, issueID, content, nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Fatalf("expected 1 pending task after initial mention, got %d", n)
		}

		clearTasks(t, issueID)

		postComment(t, issueID, "Here is more context for you", strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task from root mention owner, got %d", n)
		}
	})

	t.Run("plain reply to multi-agent root routes only first mention", func(t *testing.T) {
		clearTasks(t, issueID)
		agentB := createSecondAgent(t)
		content := fmt.Sprintf(
			"[@AgentA](mention://agent/%s) [@AgentB](mention://agent/%s) can you both review this?",
			agentID,
			agentB,
		)
		threadID := postComment(t, issueID, content, nil)
		if n := countPendingTasksForAgent(t, issueID, agentID); n != 1 {
			t.Fatalf("expected 1 pending root task for first agent, got %d", n)
		}
		if n := countPendingTasksForAgent(t, issueID, agentB); n != 1 {
			t.Fatalf("expected 1 pending root task for second agent, got %d", n)
		}
		clearTasks(t, issueID)

		postComment(t, issueID, "Here is more context for you both", strPtr(threadID))
		if n := countPendingTasksForAgent(t, issueID, agentID); n != 1 {
			t.Errorf("expected 1 pending reply task for first agent, got %d", n)
		}
		if n := countPendingTasksForAgent(t, issueID, agentB); n != 0 {
			t.Errorf("expected 0 pending reply tasks for second agent, got %d", n)
		}
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected exactly 1 pending task after multi-agent root reply, got %d", n)
		}
	})

	t.Run("reply does not double-trigger when re-mentioning same agent", func(t *testing.T) {
		clearTasks(t, issueID)

		content := fmt.Sprintf("[@Agent](mention://agent/%s) help", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)

		reply := fmt.Sprintf("[@Agent](mention://agent/%s) any update?", agentID)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (no duplicate), got %d", n)
		}
	})

	t.Run("reply mentioning only a member does not inherit agent mention", func(t *testing.T) {
		clearTasks(t, issueID)

		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you help?", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)

		reply := fmt.Sprintf("cc [@Someone](mention://member/%s)", testUserID)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks (member-only reply should not inherit agent mention), got %d", n)
		}
	})

	t.Run("reply mentioning a different agent does not inherit parent agent", func(t *testing.T) {
		clearTasks(t, issueID)
		agentB := createSecondAgent(t)

		content := fmt.Sprintf("[@AgentA](mention://agent/%s) please review", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)

		reply := fmt.Sprintf("[@AgentB](mention://agent/%s) can you also look?", agentB)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (only agent B), got %d", n)
		}
	})

	t.Run("reply mentioning same agent and member triggers via explicit mention", func(t *testing.T) {
		clearTasks(t, issueID)

		content := fmt.Sprintf("[@Agent](mention://agent/%s) review this", agentID)
		threadID := postComment(t, issueID, content, nil)
		clearTasks(t, issueID)

		reply := fmt.Sprintf("[@Agent](mention://agent/%s) and cc [@Someone](mention://member/%s)", agentID, testUserID)
		postComment(t, issueID, reply, strPtr(threadID))
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Errorf("expected 1 pending task (reply mentions agent explicitly), got %d", n)
		}
	})
}

func TestDeleteCommentCancelsTriggeredTasks(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Delete-comment cancels task test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	t.Run("deleting trigger comment cancels its queued task", func(t *testing.T) {
		clearTasks(t, issueID)
		commentID := postComment(t, issueID, "Please fix this bug", nil)
		if n := countPendingTasks(t, issueID); n != 1 {
			t.Fatalf("expected 1 pending task before delete, got %d", n)
		}

		resp := authRequest(t, "DELETE", "/api/comments/"+commentID, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("DeleteComment: expected 204, got %d", resp.StatusCode)
		}

		if n := countPendingTasks(t, issueID); n != 0 {
			t.Errorf("expected 0 pending tasks after deleting trigger comment, got %d", n)
		}
	})
}

func TestCommentTriggerCoalescing(t *testing.T) {
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Coalescing test", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	postComment(t, issueID, "First comment", nil)
	postComment(t, issueID, "Second comment", nil)

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task (coalescing), got %d", n)
	}
}

func TestCommentTriggerMentionAssigneeDoneIssue(t *testing.T) {
	agentID := getAgentID(t)

	issueID := createIssueAssignedToAgent(t, "Mention-assignee-done test", agentID)
	clearTasks(t, issueID)
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	content := fmt.Sprintf("[@Agent](mention://agent/%s) reopen this please", agentID)
	postComment(t, issueID, content, nil)

	if n := countPendingTasks(t, issueID); n != 1 {
		t.Errorf("expected 1 pending task after @mention of assignee on done issue, got %d", n)
	}
}
