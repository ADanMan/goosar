package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type claimChatResumeProbe struct {
	Task *struct {
		ID             string `json:"id"`
		PriorSessionID string `json:"prior_session_id"`
		PriorWorkDir   string `json:"prior_work_dir"`
	} `json:"task"`
}

func claimOneChatTaskForRuntime(t *testing.T, runtimeID, daemonID string) claimChatResumeProbe {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var probe claimChatResumeProbe
	if err := json.NewDecoder(w.Body).Decode(&probe); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if probe.Task == nil {
		t.Fatal("expected a claimed task in the response")
	}
	return probe
}

func TestClaimTaskByRuntime_ChatResumesCancelledTurnSession(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "ChatStopResumeAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, runtime_id)
		VALUES ($1, $2, $3, 'stopped first turn', 'active', $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, completed_at, chat_session_id, session_id, work_dir)
		VALUES ($1, $2, 'cancelled', 0, now() - interval '2 minutes', now() - interval '1 minute', $3, 'STOPPED-TURN-SESSION', '/tmp/chat-stopped-workdir')
	`, agentID, runtimeID, sessionID); err != nil {
		t.Fatalf("insert cancelled chat task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'follow-up after stop')
	`, sessionID); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'queued', 1000, $3) RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat follow-up task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	probe := claimOneChatTaskForRuntime(t, runtimeID, "chat-stop-resume-test")
	if probe.Task.PriorSessionID != "STOPPED-TURN-SESSION" {
		t.Fatalf("expected the claim to resume the cancelled turn's session, got prior_session_id=%q", probe.Task.PriorSessionID)
	}
	if probe.Task.PriorWorkDir != "/tmp/chat-stopped-workdir" {
		t.Fatalf("expected the claim to reuse the cancelled turn's workdir, got prior_work_dir=%q", probe.Task.PriorWorkDir)
	}
}

func TestClaimTaskByRuntime_ChatSessionPointerBeatsCancelledFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "ChatStopPointerAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, runtime_id, session_id, work_dir)
		VALUES ($1, $2, $3, 'stopped later turn', 'active', $4, 'POINTER-SESSION', '/tmp/chat-pointer-workdir')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, completed_at, chat_session_id, session_id, work_dir)
		VALUES ($1, $2, 'cancelled', 0, now() - interval '2 minutes', now() - interval '1 minute', $3, 'CANCELLED-TURN-SESSION', '/tmp/chat-cancelled-workdir')
	`, agentID, runtimeID, sessionID); err != nil {
		t.Fatalf("insert cancelled chat task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'follow-up after stop')
	`, sessionID); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'queued', 1000, $3) RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat follow-up task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	probe := claimOneChatTaskForRuntime(t, runtimeID, "chat-stop-pointer-test")
	if probe.Task.PriorSessionID != "POINTER-SESSION" {
		t.Fatalf("chat_session pointer must win over the cancelled fallback, got prior_session_id=%q", probe.Task.PriorSessionID)
	}
	if probe.Task.PriorWorkDir != "/tmp/chat-pointer-workdir" {
		t.Fatalf("chat_session workdir must win over the cancelled fallback, got prior_work_dir=%q", probe.Task.PriorWorkDir)
	}
}
