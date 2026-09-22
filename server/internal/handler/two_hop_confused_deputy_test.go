package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type twoHopFixture struct {
	MemberM     string
	AgentA      string
	AllowListed string
	IssueX      string
	BareTaskID  string
	IssueTaskID string
}

func repointIssueCreator(t *testing.T, issueID, creatorID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET creator_id = $1 WHERE id = $2`, creatorID, issueID); err != nil {
		t.Fatalf("repoint issue creator: %v", err)
	}
}

func seedBareTask(t *testing.T, agentID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at)
		VALUES ($1, $2, 'running', 0, now())
		RETURNING id
	`, agentID, handlerTestRuntimeID(t)).Scan(&taskID); err != nil {
		t.Fatalf("seed bare task: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	return taskID
}

func seedTwoHopFixture(t *testing.T) twoHopFixture {
	t.Helper()
	memberM := createPermissionTestMember(t, "two-hop-m@goosar.test")
	agentA := createHandlerTestAgent(t, "two-hop-agent-a", nil)
	allowListed := createPublicToAgentWithTargets(t, "two-hop-allowlisted", []map[string]any{
		{"target_type": "member", "target_id": memberM},
	})
	issueX := createCommentTriggerPreviewIssue(t, "two-hop pre-existing issue X", "", "")
	repointIssueCreator(t, issueX, memberM)
	bareTaskID := seedBareTask(t, agentA)
	issueTaskID := seedLiveDeputyTask(t, agentA, "issue_id", issueX, memberM)

	return twoHopFixture{
		MemberM:     memberM,
		AgentA:      agentA,
		AllowListed: allowListed,
		IssueX:      issueX,
		BareTaskID:  bareTaskID,
		IssueTaskID: issueTaskID,
	}
}

func assignAgentToIssue(issueID, actingAgentID, actingTaskID, assigneeAgentID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := newRequest(http.MethodPatch, "/api/issues/"+issueID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   assigneeAgentID,
	})
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", actingAgentID)
	r.Header.Set("X-Task-ID", actingTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.UpdateIssue(w, r)
	return w
}

func TestTwoHopBypass_UnscopedAgentCannotAttachItselfToStrangersIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := seedTwoHopFixture(t)
	tasksBefore := agentTaskCount(t, fx.AgentA)

	w := assignAgentToIssue(fx.IssueX, fx.AgentA, fx.BareTaskID, fx.AgentA)
	if w.Code != http.StatusForbidden {
		t.Fatalf("hop 1 (self-assign onto a stranger's issue from an unscoped task) = %d, want 403: %s",
			w.Code, w.Body.String())
	}

	var assigneeType, assigneeID string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(assignee_type, ''), COALESCE(assignee_id::text, '') FROM issue WHERE id = $1`, fx.IssueX,
	).Scan(&assigneeType, &assigneeID); err != nil {
		t.Fatalf("load issue assignee: %v", err)
	}
	if assigneeType != "" || assigneeID != "" {
		t.Fatalf("issue X assignee = (%q,%q), want unassigned -- the refused PATCH must not have mutated anything", assigneeType, assigneeID)
	}
	if after := agentTaskCount(t, fx.AgentA); after != tasksBefore {
		t.Fatalf("AgentA task count went %d -> %d; a refused attach must mint no task (closing hop 2 before it can start)", tasksBefore, after)
	}
}

func TestTwoHopBypass_ScopedReassignmentStillWorks(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := seedTwoHopFixture(t)

	w := assignAgentToIssue(fx.IssueX, fx.AgentA, fx.IssueTaskID, fx.AgentA)
	if w.Code != http.StatusOK {
		t.Fatalf("reassign from a task genuinely scoped to X = %d, want 200: %s", w.Code, w.Body.String())
	}
}

type subIssueFixture struct {
	MemberH     string
	AgentA      string
	AllowListed string
	ParentIssue string
	ParentTask  string
}

func seedSubIssueFixture(t *testing.T) subIssueFixture {
	t.Helper()
	memberH := createPermissionTestMember(t, "sub-issue-h@goosar.test")
	agentA := createHandlerTestAgent(t, "sub-issue-agent-a", nil)
	allowListed := createPublicToAgentWithTargets(t, "sub-issue-allowlisted", []map[string]any{
		{"target_type": "member", "target_id": memberH},
	})
	parentIssue := createCommentTriggerPreviewIssue(t, "sub-issue parent", "", "")
	repointIssueCreator(t, parentIssue, memberH)
	parentTask := seedLiveDeputyTask(t, agentA, "issue_id", parentIssue, memberH)

	return subIssueFixture{
		MemberH:     memberH,
		AgentA:      agentA,
		AllowListed: allowListed,
		ParentIssue: parentIssue,
		ParentTask:  parentTask,
	}
}

func createSubIssue(actingAgentID, actingTaskID string, parentIssueID, assigneeAgentID *string) *httptest.ResponseRecorder {
	body := map[string]any{"title": "sub-issue create/assign restore test"}
	if parentIssueID != nil {
		body["parent_issue_id"] = *parentIssueID
	}
	if assigneeAgentID != nil {
		body["assignee_type"] = "agent"
		body["assignee_id"] = *assigneeAgentID
	}
	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", actingAgentID)
	r.Header.Set("X-Task-ID", actingTaskID)
	testHandler.CreateIssue(w, r)
	return w
}

func TestCreateIssue_SubIssueOfScopedParent_RestoresDelegationFlow(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := seedSubIssueFixture(t)

	w := createSubIssue(fx.AgentA, fx.ParentTask, &fx.ParentIssue, &fx.AllowListed)
	if w.Code != http.StatusCreated {
		t.Fatalf("sub-issue create+assign under a scoped parent = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssue_BareCreateStaysUnscoped(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := seedSubIssueFixture(t)

	w := createSubIssue(fx.AgentA, fx.ParentTask, nil, &fx.AllowListed)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bare create (no parent_issue_id) assigning a member-target agent = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestSendChatMessage_ScopedToOwnChatSessionOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	memberH := createPermissionTestMember(t, "chat-scope-h@goosar.test")
	allowListed := createPublicToAgentWithTargets(t, "chat-scope-allowlisted", []map[string]any{
		{"target_type": "member", "target_id": memberH},
	})

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, status)
		VALUES ($1, $2, $3, 'active')
		RETURNING id
	`, testWorkspaceID, allowListed, memberH).Scan(&sessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM chat_session WHERE id = $1`, sessionID)

	unrelatedIssue := createCommentTriggerPreviewIssue(t, "chat-scope unrelated issue", "", "")
	repointIssueCreator(t, unrelatedIssue, memberH)
	issueTaskID := seedLiveDeputyTask(t, allowListed, "issue_id", unrelatedIssue, memberH)
	chatTaskID := seedLiveDeputyTask(t, allowListed, "chat_session_id", sessionID, memberH)

	send := func(taskID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest(http.MethodPost, "/api/chat/sessions/"+sessionID+"/messages", map[string]any{
			"content": "hello from a borrowed authority",
		})
		r.Header.Set("X-User-ID", memberH)
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Agent-ID", allowListed)
		r.Header.Set("X-Task-ID", taskID)
		r = withURLParam(withChatTestWorkspaceCtx(t, r), "sessionId", sessionID)
		testHandler.SendChatMessage(w, r)
		return w
	}

	t.Run("issue-bound task is NOT scoped to this session", func(t *testing.T) {
		w := send(issueTaskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("send from an issue-bound (not this session's) task = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("this session's own task is scoped", func(t *testing.T) {
		w := send(chatTaskID)
		if w.Code != http.StatusCreated {
			t.Fatalf("send from this session's own task = %d, want 201: %s", w.Code, w.Body.String())
		}
	})
}
