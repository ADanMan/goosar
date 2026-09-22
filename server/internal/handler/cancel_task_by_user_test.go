package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adanman/goosar/server/pkg/protocol"
)

func taskStatus(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	return status
}

func createAutopilotRunOnlyTask(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT workspace_id, runtime_id FROM agent WHERE id = $1`, agentID,
	).Scan(&workspaceID, &runtimeID); err != nil {
		t.Fatalf("load agent workspace/runtime: %v", err)
	}

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'cancel-runonly-ap', $2, 'run_only', 'member', $3)
		RETURNING id
	`, workspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("create autopilot_run: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, autopilot_run_id)
		VALUES ($1, $2, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, runID).Scan(&taskID); err != nil {
		t.Fatalf("create run_only task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

func createForeignWorkspaceAgent(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Foreign Cancel WS', 'foreign-cancel-ws', 'cross-tenant cancel test', 'FCW')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Foreign Cancel Runtime', 'cloud', 'foreign_runtime', 'online', 'Foreign runtime', '{}'::jsonb, now())
		RETURNING id
	`, workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
		VALUES ($1, 'Foreign Cancel Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}
	return agentID
}

func createWorkspaceMemberUser(t *testing.T, name, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, name, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

func cancelTaskByUserRequest(t *testing.T, userID, taskID string) *http.Request {
	t.Helper()
	req := newRequestAs(userID, "POST", "/api/tasks/"+taskID+"/cancel", nil)
	req = withURLParam(req, "taskId", taskID)
	return withChatTestWorkspaceCtx(t, req)
}

func createStartedEmptyChatTask(t *testing.T, sessionID, agentID, content string) (taskID, userMessageID string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id, started_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2, now())
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create started chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, content, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}
	return taskID, userMessageID
}

func chatFinalizeDeferredAt(t *testing.T, taskID string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT chat_finalize_deferred_at FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&at); err != nil {
		t.Fatalf("read chat_finalize_deferred_at: %v", err)
	}
	return at
}

func TestCancelTaskByUser_StartedEmptyChat_WithDraftRestoreCapability_Defers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatDeferCapableAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID, userMessageID := createStartedEmptyChatTask(t, sessionID, agentID, "defer this prompt")

	req := cancelTaskByUserRequest(t, testUserID, taskID)
	req.Header.Set("X-Client-Capabilities", protocol.AppCapabilityChatDraftRestoreV1)
	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage != nil {
		t.Fatalf("capable client must get no synchronous restore, got %#v", resp.CancelledChatMessage)
	}
	if chatFinalizeDeferredAt(t, taskID) == nil {
		t.Fatal("expected the finalize-deferred marker to be set")
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID).Scan(&count); err != nil {
		t.Fatalf("count user chat message: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the user message to survive the deferral, got %d rows", count)
	}
}

func TestCancelTaskByUser_StartedEmptyChat_LegacyClient_StillGetsSynchronousRestore(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatDeferLegacyAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	const userContent = "an old client must not lose this"
	taskID, userMessageID := createStartedEmptyChatTask(t, sessionID, agentID, userContent)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("legacy client must still get the synchronous restore payload")
	}
	if resp.CancelledChatMessage.MessageID != userMessageID ||
		resp.CancelledChatMessage.Content != userContent ||
		!resp.CancelledChatMessage.RestoreToInput {
		t.Fatalf("restore payload mismatch: %#v", resp.CancelledChatMessage)
	}
	if at := chatFinalizeDeferredAt(t, taskID); at != nil {
		t.Fatalf("legacy cancel must not defer, marker set at %v", at)
	}

	var messages int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID).Scan(&messages); err != nil {
		t.Fatalf("count user chat message: %v", err)
	}
	if messages != 0 {
		t.Fatalf("expected the user message to be deleted synchronously, got %d rows", messages)
	}
	var restores int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID).Scan(&restores); err != nil {
		t.Fatalf("count draft restores: %v", err)
	}
	if restores != 0 {
		t.Fatalf("expected no durable restore for a legacy cancel, got %d", restores)
	}
}

func TestCancelTaskByUser_RunOnlyAutopilot_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelRunOnlyAgent", []byte("[]"))
	taskID := createAutopilotRunOnlyTask(t, agentID)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

func TestCancelTaskByUser_RunOnlyAutopilot_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignAgentID := createForeignWorkspaceAgent(t)
	taskID := createAutopilotRunOnlyTask(t, foreignAgentID)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "queued" {
		t.Fatalf("foreign task was mutated: status = %q", got)
	}
}

func TestCancelTaskByUser_QuickCreate_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelQuickCreateAgent", []byte("[]"))

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, context)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL,
		        '{"type":"quick_create","workspace_id":"ws","prompt":"do a thing"}'::jsonb)
		RETURNING id
	`, agentID).Scan(&taskID); err != nil {
		t.Fatalf("create quick_create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

func TestCancelTaskByUser_RetryClone_Autopilot_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelRetryCloneAgent", []byte("[]"))
	parentID := createAutopilotRunOnlyTask(t, agentID)

	var cloneID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, autopilot_run_id, parent_task_id, attempt)
		SELECT agent_id, runtime_id, 'queued', priority, autopilot_run_id, id, 1
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, parentID).Scan(&cloneID); err != nil {
		t.Fatalf("create retry clone: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, cloneID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, cloneID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, cloneID); got != "cancelled" {
		t.Fatalf("clone not cancelled: status = %q", got)
	}
}

func TestCancelTaskByUser_IssueTask_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelIssueTaskAgent", []byte("[]"))

	var issueID, taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cancel-byid-issue', 'todo', 'medium', $2, 'member', 92001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'queued', 0, $2)
		RETURNING id
	`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create issue task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

func TestCancelTaskByUser_ChatTask_NonCreator_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	otherUserID := createWorkspaceMemberUser(t, "Chat Bystander", "cancel-chat-bystander@goosar.test")

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, otherUserID, taskID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "running" {
		t.Fatalf("chat task was mutated: status = %q", got)
	}
}

func TestCancelTaskByUser_ChatTaskWithTranscript_PersistsAssistantSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id, created_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2, now() - interval '5 seconds')
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', 'please answer', $2)
	`, sessionID, taskID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'text', 'partial answer')
	`, taskID); err != nil {
		t.Fatalf("create task message: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage != nil {
		t.Fatalf("expected no restore payload when transcript exists, got %#v", resp.CancelledChatMessage)
	}
	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}

	var role, content, messageTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role, content, COALESCE(task_id::text, '')
		FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant'
	`, sessionID).Scan(&role, &content, &messageTaskID); err != nil {
		t.Fatalf("read cancelled assistant chat message: %v", err)
	}
	if role != "assistant" || content != "Stopped." || messageTaskID != taskID {
		t.Fatalf("assistant snapshot mismatch: role=%q content=%q task_id=%q", role, content, messageTaskID)
	}
}

func TestCancelTaskByUser_ChatTaskWithoutTranscript_RestoresUserDraft(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatNoTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	var userMessageID string
	const userContent = "keep this prompt"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, userContent, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("expected restore payload for empty transcript cancel")
	}
	if resp.CancelledChatMessage.MessageID != userMessageID ||
		resp.CancelledChatMessage.Content != userContent ||
		!resp.CancelledChatMessage.RestoreToInput {
		t.Fatalf("restore payload mismatch: %#v", resp.CancelledChatMessage)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant'
	`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count assistant chat messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no assistant snapshot for empty transcript, got %d", count)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE id = $1
	`, userMessageID).Scan(&count); err != nil {
		t.Fatalf("count deleted user chat message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected linked user message to be deleted, got %d", count)
	}
}

func TestCancelTaskByUser_ChatTaskWithBoundAttachment_SurvivesCancelAndRebinds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatAttachAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	var userMessageID string
	const userContent = "look at this attachment"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, userContent, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}

	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, chat_session_id, chat_message_id)
		VALUES ($1, 'member', $2, 'cancel-survive.png', 'https://cdn.example.com/cancel-survive.png', 'image/png', 9, $3, $4)
		RETURNING id::text
	`, testWorkspaceID, testUserID, sessionID, userMessageID).Scan(&attachmentID); err != nil {
		t.Fatalf("seed bound attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID) })

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelTaskByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("expected restore payload for empty transcript cancel")
	}

	var returned *AttachmentResponse
	for i := range resp.CancelledChatMessage.Attachments {
		if resp.CancelledChatMessage.Attachments[i].ID == attachmentID {
			returned = &resp.CancelledChatMessage.Attachments[i]
			break
		}
	}
	if returned == nil {
		t.Fatalf("cancel response did not return the detached attachment: %#v", resp.CancelledChatMessage.Attachments)
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&count); err != nil {
		t.Fatalf("count attachment: %v", err)
	}
	if count != 1 {
		t.Fatalf("attachment was cascade-deleted on cancel: count = %d", count)
	}
	var dbMessageID, dbSessionID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text, chat_session_id::text FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&dbMessageID, &dbSessionID); err != nil {
		t.Fatalf("read attachment after cancel: %v", err)
	}
	if dbMessageID != nil {
		t.Fatalf("expected chat_message_id detached to NULL, got %q", *dbMessageID)
	}
	if dbSessionID == nil || *dbSessionID != sessionID {
		t.Fatalf("expected chat_session_id retained as %q, got %v", sessionID, dbSessionID)
	}

	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID,
	).Scan(&count); err != nil {
		t.Fatalf("count user message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected linked user message to be deleted, got %d", count)
	}

	sendReq := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content":        userContent,
		"attachment_ids": []string{attachmentID},
	})
	sendReq = withURLParam(sendReq, "sessionId", sessionID)
	sendReq = withChatTestWorkspaceCtx(t, sendReq)
	sendW := httptest.NewRecorder()
	testHandler.SendChatMessage(sendW, sendReq)
	if sendW.Code != http.StatusCreated {
		t.Fatalf("resend: expected 201, got %d: %s", sendW.Code, sendW.Body.String())
	}
	var sendResp SendChatMessageResponse
	if err := json.Unmarshal(sendW.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("decode resend response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, sendResp.TaskID)
	})

	rebound := false
	for _, id := range sendResp.AttachmentIDs {
		if id == attachmentID {
			rebound = true
			break
		}
	}
	if !rebound {
		t.Fatalf("attachment not re-bound on resend: %#v", sendResp.AttachmentIDs)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&dbMessageID); err != nil {
		t.Fatalf("read attachment after resend: %v", err)
	}
	if dbMessageID == nil || *dbMessageID != sendResp.MessageID {
		t.Fatalf("expected attachment re-bound to new message %q, got %v", sendResp.MessageID, dbMessageID)
	}
}

func TestCancelTaskByUser_PrivateAgent_PlainMember_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, _, memberID := privateAgentTestFixture(t)
	taskID := createAutopilotRunOnlyTask(t, agentID)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, memberID, taskID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskStatus(t, taskID); got != "queued" {
		t.Fatalf("task was mutated: status = %q", got)
	}
}
