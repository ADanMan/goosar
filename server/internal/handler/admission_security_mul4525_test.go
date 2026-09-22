package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/dispatch"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func seedSecurityTestOwner(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	var ownerID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, fmt.Sprintf("%s-%d@goosar.test", label, time.Now().UnixNano())).Scan(&ownerID); err != nil {
		t.Fatalf("seed owner user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, ownerID); err != nil {
		t.Fatalf("seed owner member: %v", err)
	}
	return ownerID
}

func readReasonCode(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Error      string `json:"error"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode blocked response: %v (body=%s)", err, string(body))
	}
	return resp.ReasonCode
}

func TestSendChatMessage_InvokeRevokedAfterSessionCreate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	ownerID := seedSecurityTestOwner(t, "chat-agent-owner")

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args)
		VALUES ($1, 'chat-revoke-agent', '', 'cloud', '{}'::jsonb, $2, 'private', 'public_to', 1, $3,
			'', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, testWorkspaceID, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'member', $2)`, agentID, testUserID); err != nil {
		t.Fatalf("seed member invocation target: %v", err)
	}

	createW := httptest.NewRecorder()
	createReq := withChatTestWorkspaceCtx(t, newRequest("POST", "/api/chat/sessions", map[string]any{
		"agent_id": agentID,
		"title":    "revoke test",
	}))
	testHandler.CreateChatSession(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateChatSession while invokable: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, session.ID) })

	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("revoke invoke: %v", err)
	}

	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'unbound.png', 'https://cdn.test/unbound.png', 'image/png', 10)
		RETURNING id`, testWorkspaceID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("seed unbound attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID) })

	countMessages := func() int {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM chat_message WHERE chat_session_id = $1`, session.ID).Scan(&n); err != nil {
			t.Fatalf("count messages: %v", err)
		}
		return n
	}
	countTasks := func() int {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1`, session.ID).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}
	if countMessages() != 0 || countTasks() != 0 {
		t.Fatalf("precondition: session should have no messages/tasks yet")
	}

	sendW := httptest.NewRecorder()
	sendReq := withChatTestWorkspaceCtx(t, newRequest("POST", "/api/chat/sessions/"+session.ID+"/messages", map[string]any{
		"content":        "are you still there?",
		"attachment_ids": []string{attachmentID},
	}))
	sendReq = withURLParam(sendReq, "sessionId", session.ID)
	testHandler.SendChatMessage(sendW, sendReq)

	if sendW.Code != http.StatusForbidden {
		t.Fatalf("SendChatMessage after revoke: expected 403, got %d: %s", sendW.Code, sendW.Body.String())
	}
	if code := readReasonCode(t, sendW.Body.Bytes()); code != string(dispatch.ReasonInvocationNotAllowed) {
		t.Errorf("reason_code = %q, want invocation_not_allowed", code)
	}
	if got := countMessages(); got != 0 {
		t.Errorf("blocked send persisted %d chat messages, want 0", got)
	}
	if got := countTasks(); got != 0 {
		t.Errorf("blocked send persisted %d tasks, want 0", got)
	}

	var boundSession, boundMessage pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT chat_session_id, chat_message_id FROM attachment WHERE id = $1`, attachmentID).
		Scan(&boundSession, &boundMessage); err != nil {
		t.Fatalf("read attachment binding: %v", err)
	}
	if boundSession.Valid {
		t.Errorf("blocked send set attachment chat_session_id = %s, want NULL", util.UUIDToString(boundSession))
	}
	if boundMessage.Valid {
		t.Errorf("blocked send set attachment chat_message_id = %s, want NULL", util.UUIDToString(boundMessage))
	}
}

func TestRerunIssue_PrivateHistoricalAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, ownerID, _ := privateAgentTestFixture(t)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority)
		VALUES ($1, 'rerun private agent', 'member', $2, 'agent', $3, 'medium')
		RETURNING id`, testWorkspaceID, ownerID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	orig, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(ownerID),
		WorkspaceID:  util.MustParseUUID(testWorkspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("enqueue original task: %v", err)
	}
	origID := util.UUIDToString(orig.ID)

	currentAgentID := createHandlerTestAgent(t, "RerunCurrentAssignee", []byte("[]"))
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_id = $1 WHERE id = $2`, currentAgentID, issueID); err != nil {
		t.Fatalf("reassign issue to second agent: %v", err)
	}

	taskCount := func() int {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}
	origStatus := func() string {
		var s string
		if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, origID).Scan(&s); err != nil {
			t.Fatalf("read orig status: %v", err)
		}
		return s
	}
	beforeCount, beforeStatus := taskCount(), origStatus()

	denyW := httptest.NewRecorder()
	denyReq := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/rerun", map[string]any{"task_id": origID}), "id", issueID)
	testHandler.RerunIssue(denyW, denyReq)
	if denyW.Code != http.StatusForbidden {
		t.Fatalf("RerunIssue as non-invoker of historical agent: expected 403, got %d: %s", denyW.Code, denyW.Body.String())
	}
	if code := readReasonCode(t, denyW.Body.Bytes()); code != string(dispatch.ReasonInvocationNotAllowed) {
		t.Errorf("reason_code = %q, want invocation_not_allowed", code)
	}
	if got := taskCount(); got != beforeCount {
		t.Errorf("blocked rerun changed task count: got %d, want %d", got, beforeCount)
	}
	if got := origStatus(); got != beforeStatus {
		t.Errorf("blocked rerun changed original task status: got %q, want %q", got, beforeStatus)
	}

	allowW := httptest.NewRecorder()
	allowReq := withURLParam(newRequestAs(ownerID, "POST", "/api/issues/"+issueID+"/rerun", map[string]any{"task_id": origID}), "id", issueID)
	testHandler.RerunIssue(allowW, allowReq)
	if allowW.Code != http.StatusAccepted {
		t.Fatalf("RerunIssue as historical agent owner: expected 202, got %d: %s", allowW.Code, allowW.Body.String())
	}
	var reran struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(allowW.Body.Bytes(), &reran); err != nil {
		t.Fatalf("decode rerun response: %v", err)
	}
	if reran.AgentID != agentID {
		t.Errorf("reran task agent_id = %q, want historical agent %q (not current assignee %q)", reran.AgentID, agentID, currentAgentID)
	}
	if reran.ID == origID {
		t.Errorf("expected a new task id, got the original %q", origID)
	}
}
