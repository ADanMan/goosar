package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func seedChannelBinding(t *testing.T, ctx context.Context, agentID, sessionID, channelType, lastMessageID, lastThreadID string) {
	t.Helper()
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, installer_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, channelType, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("seed %s installation: %v", channelType, err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_chat_session_binding
			(chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, last_message_id, last_thread_id)
		VALUES ($1, $2, $3, $4, 'group', $5, $6)
	`, sessionID, installationID, channelType, "C-TEST-"+channelType, lastMessageID, lastThreadID); err != nil {
		t.Fatalf("seed %s binding: %v", channelType, err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
}

func claimChatChannelFields(t *testing.T, runtimeID string) (channelType string, inThread bool) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-channel-type")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			ChatChannelType string `json:"chat_channel_type"`
			ChatInThread    bool   `json:"chat_in_thread"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a claimed task")
	}
	return resp.Task.ChatChannelType, resp.Task.ChatInThread
}

func TestClaim_FeishuBoundSessionReportsChannelType(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "feishu-backed chat")

	seedChannelBinding(t, ctx, agentID, sessionID, "feishu", "msg-1", "thread-9")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	channelType, inThread := claimChatChannelFields(t, runtimeID)
	if channelType != "feishu" {
		t.Errorf("chat_channel_type = %q, want %q — a Feishu session must not look like a web chat", channelType, "feishu")
	}
	if inThread {
		t.Error("chat_in_thread must stay false on Feishu: it selects between two Slack-only read commands")
	}
}

func TestClaim_SlackBoundSessionStillReportsThreadState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "slack-backed chat")
	seedChannelBinding(t, ctx, agentID, sessionID, "slack", "msg-1", "thread-9")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	channelType, inThread := claimChatChannelFields(t, runtimeID)
	if channelType != "slack" {
		t.Errorf("chat_channel_type = %q, want %q", channelType, "slack")
	}
	if !inThread {
		t.Error("chat_in_thread should be true when last_thread_id differs from last_message_id")
	}
}

func TestClaim_UnboundSessionReportsNoChannelType(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "web chat")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	channelType, inThread := claimChatChannelFields(t, runtimeID)
	if channelType != "" {
		t.Errorf("chat_channel_type = %q, want empty for a web-only session", channelType)
	}
	if inThread {
		t.Error("chat_in_thread must be false for a web-only session")
	}
}

func requeueTaskForClaim(t *testing.T, ctx context.Context, sessionID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'queued', started_at = NULL, dispatched_at = NULL
		WHERE chat_session_id = $1
	`, sessionID); err != nil {
		t.Fatalf("requeue task: %v", err)
	}
}
