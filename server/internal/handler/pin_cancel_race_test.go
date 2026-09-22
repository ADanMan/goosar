package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pinTaskSessionForTest(t *testing.T, taskID, sessionID, workDir string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/tasks/"+taskID+"/session", PinTaskSessionRequest{
		SessionID: sessionID,
		WorkDir:   workDir,
	}, testWorkspaceID, "pin-race-test")
	req = withURLParam(req, "taskId", taskID)
	testHandler.PinTaskSession(w, req)
	return w.Code
}

func taskSessionPointer(t *testing.T, taskID string) (sessionID, workDir string) {
	t.Helper()
	var sid, wd *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT session_id, work_dir FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&sid, &wd); err != nil {
		t.Fatalf("read task session pointer: %v", err)
	}
	if sid != nil {
		sessionID = *sid
	}
	if wd != nil {
		workDir = *wd
	}
	return sessionID, workDir
}

func TestPinTaskSession_InstantStopRaceLandsOnCancelledChatTurn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "PinRaceChatAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, runtime_id)
		VALUES ($1, $2, $3, 'instant stop', 'active', $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID) })

	var stoppedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, completed_at, chat_session_id)
		VALUES ($1, $2, 'cancelled', 0, now() - interval '2 minutes', now() - interval '1 minute', $3)
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&stoppedTaskID); err != nil {
		t.Fatalf("insert cancelled chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, stoppedTaskID)
	})

	if code := pinTaskSessionForTest(t, stoppedTaskID, "LATE-PINNED-SESSION", "/tmp/pin-race-workdir"); code != http.StatusNoContent {
		t.Fatalf("PinTaskSession on a cancelled chat row: expected 204, got %d", code)
	}

	sid, wd := taskSessionPointer(t, stoppedTaskID)
	if sid != "LATE-PINNED-SESSION" || wd != "/tmp/pin-race-workdir" {
		t.Fatalf("expected the late pin to land on the cancelled chat row, got session_id=%q work_dir=%q", sid, wd)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, stoppedTaskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("the pin must not resurrect the task; expected status cancelled, got %q", status)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'follow-up after instant stop')
	`, sessionID); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}
	var followUpID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'queued', 1000, $3) RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&followUpID); err != nil {
		t.Fatalf("create chat follow-up task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, followUpID) })

	probe := claimOneChatTaskForRuntime(t, runtimeID, "pin-race-claim-test")
	if probe.Task.PriorSessionID != "LATE-PINNED-SESSION" {
		t.Fatalf("expected the claim to resume the late-pinned session, got prior_session_id=%q", probe.Task.PriorSessionID)
	}
	if probe.Task.PriorWorkDir != "/tmp/pin-race-workdir" {
		t.Fatalf("expected the claim to reuse the late-pinned workdir, got prior_work_dir=%q", probe.Task.PriorWorkDir)
	}
}

func TestPinTaskSession_CancelledIssueTaskStaysUnpinned(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "PinRaceIssueAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority)
		VALUES ($1, 'pin race issue', 'member', $2, 'agent', $3, 'medium')
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at)
		VALUES ($1, $2, $3, 'cancelled', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert cancelled issue task: %v", err)
	}

	if code := pinTaskSessionForTest(t, taskID, "ISSUE-LATE-PIN", "/tmp/pin-race-issue"); code != http.StatusNoContent {
		t.Fatalf("PinTaskSession on a cancelled issue row: expected 204 (harmless no-op), got %d", code)
	}

	sid, wd := taskSessionPointer(t, taskID)
	if sid != "" || wd != "" {
		t.Fatalf("cancelled issue rows must stay unpinned, got session_id=%q work_dir=%q", sid, wd)
	}
}

func TestPinTaskSession_TerminalChatTaskStaysUnpinned(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "PinRaceTerminalAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, runtime_id)
		VALUES ($1, $2, $3, 'terminal pin', 'active', $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, completed_at, chat_session_id)
		VALUES ($1, $2, 'completed', 0, now() - interval '2 minutes', now() - interval '1 minute', $3)
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert completed chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if code := pinTaskSessionForTest(t, taskID, "STRAGGLER-PIN", "/tmp/pin-race-terminal"); code != http.StatusNoContent {
		t.Fatalf("PinTaskSession on a completed row: expected 204 (harmless no-op), got %d", code)
	}

	sid, wd := taskSessionPointer(t, taskID)
	if sid != "" || wd != "" {
		t.Fatalf("terminal rows must stay owned by the terminal report, got session_id=%q work_dir=%q", sid, wd)
	}
}
