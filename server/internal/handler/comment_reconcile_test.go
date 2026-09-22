package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func completeTaskViaHandler(t *testing.T, taskID, output string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	return w
}

func pendingTaskCountForAgentIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`,
		issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	return n
}

func queuedTaskCountForAgentIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return n
}

func TestCompleteTask_ReconcilesMemberCommentPostedDuringRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-e2e fixture', 'in_progress', 'none', $2, 'member', 999001, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'wait, also handle this', 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: mid-run member comment: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up task after reconciliation, got %d", n)
	}
}

func TestCompleteTask_NoReconcileWhenNoNewMemberComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-negative fixture', 'in_progress', 'none', $2, 'member', 999002, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'the only request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 0 {
		t.Fatalf("expected no follow-up task when no new member comment, got %d", n)
	}
}

func TestCompleteTask_DoesNotReTriggerOtherAgentMentionedDuringRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentA, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentA, &runtimeID); err != nil {
		t.Fatalf("setup: get agent A: %v", err)
	}

	agentB := createHandlerTestAgent(t, "Reconcile Other Agent B", nil)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-other-agent fixture', 'in_progress', 'none', $2, 'member', 999003, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentA).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentA, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	mention := "[@B](mention://agent/" + agentB + ") please take a look"
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, testUserID, mention); err != nil {
		t.Fatalf("setup: mid-run @B comment: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("agent B must not be re-triggered by agent A's completion, got %d B task(s)", n)
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("agent A must not enqueue a follow-up for a comment addressed to B, got %d A task(s)", n)
	}
}

func TestCompleteTask_ReconcilesAgentAuthoredMentionToCompletedAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("setup: get runtime: %v", err)
	}

	agentA := createHandlerTestAgent(t, "Reconcile A2A Author A", nil)
	agentB := createHandlerTestAgent(t, "Reconcile A2A Target B", nil)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-a2a-mention fixture', 'in_progress', 'none', $2, 'member', 999007, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentB).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID) })

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("setup: load issue: %v", err)
	}

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, dispatched_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'dispatched', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentB, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: dispatched task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	var mentionCommentID string
	mention := "[@B](mention://agent/" + agentB + ") please also handle this"
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'agent', $3, $4, 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, agentA, mention).Scan(&mentionCommentID); err != nil {
		t.Fatalf("setup: agent @B comment: %v", err)
	}
	mentionComment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(mentionCommentID))
	if err != nil {
		t.Fatalf("setup: load mention comment: %v", err)
	}
	testHandler.triggerTasksForComment(ctx, issue, mentionComment, nil, "agent", agentA, invokeAuthority{}, "", nil)

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("expected the mention to be dropped at creation (0 queued follow-up), got %d", n)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '1 minute' WHERE id = $1`, taskID); err != nil {
		t.Fatalf("advance task to running: %v", err)
	}
	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for B from the agent-authored @B mention after completion, got %d", n)
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

func TestCompleteTask_DoesNotReconcilePlainAgentReply(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("setup: get runtime: %v", err)
	}
	agentA := createHandlerTestAgent(t, "Reconcile PlainReply Author A", nil)
	agentB := createHandlerTestAgent(t, "Reconcile PlainReply Target B", nil)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-plain-agent-reply fixture', 'in_progress', 'none', $2, 'member', 999008, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentB).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentB, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'agent', $3, 'thanks, looks good to me', 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, agentA); err != nil {
		t.Fatalf("setup: plain agent reply: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("a plain agent reply (no mention) must not enqueue a follow-up, got %d B task(s)", n)
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("a plain agent reply (no mention) must not enqueue a follow-up, got %d A task(s)", n)
	}
}

func TestCompleteTask_DoesNotReconcilePlainWorkerReplyOnSquadIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("setup: load leader runtime: %v", err)
	}

	var leaderTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, squad_id, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', TRUE, $4, 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, fx.LeaderID, leaderRuntimeID, issueID, fx.SquadID).Scan(&leaderTaskID); err != nil {
		t.Fatalf("setup: leader task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'agent', $3, 'done — pushed the change', 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, fx.OtherID); err != nil {
		t.Fatalf("setup: plain worker reply: %v", err)
	}

	if w := completeTaskViaHandler(t, leaderTaskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, fx.LeaderID); n != 0 {
		t.Fatalf("plain worker reply must not reconcile-wake the squad leader, got %d leader task(s)", n)
	}
}

func handlerWorkspaceMember(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := slug + "-" + time.Now().Format("150405.000000") + "@example.test"
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reconcile Test "+slug, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`,
		testWorkspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func TestConsecutiveCommentsDifferentOriginatorsFullEnqueuePath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}
	userB := handlerWorkspaceMember(t, "originatorB")

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'diff-originator fixture', 'in_progress', 'none', $2, 'member', 999004, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	insertMemberComment := func(authorID, content string) db.Comment {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'member', $3, $4, 'comment') RETURNING id
		`, issueID, testWorkspaceID, authorID, content).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		c, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(id))
		if err != nil {
			t.Fatalf("load comment: %v", err)
		}
		return c
	}

	cA := insertMemberComment(testUserID, "first, from A")
	testHandler.triggerTasksForComment(ctx, issue, cA, nil, "member", testUserID, scopedInvokeAuthority(testUserID), "", nil)
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("after A's comment expected exactly 1 queued task, got %d", n)
	}

	cB := insertMemberComment(userB, "second, from B — different user")
	testHandler.triggerTasksForComment(ctx, issue, cB, nil, "member", userB, scopedInvokeAuthority(userB), "", nil)

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("after B's comment expected still exactly 1 task (folded in, not dropped/duplicated), got %d", n)
	}

	trigger, originator, coalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
	if trigger != uuidToString(cB.ID) {
		t.Errorf("expected trigger repointed to B's comment %s, got %s", uuidToString(cB.ID), trigger)
	}
	if originator != userB {
		t.Errorf("expected originator re-stamped to B (%s), got %s", userB, originator)
	}
	if !containsUUID(coalesced, uuidToString(cA.ID)) {
		t.Errorf("expected A's comment %s preserved as coalesced, got %v", uuidToString(cA.ID), coalesced)
	}
}

func TestCompleteTask_ReconcilesDispatchedWindowComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'dispatched-window fixture', 'in_progress', 'none', $2, 'member', 999005, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes', now() - interval '2 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'squeezed in before start', 'comment', now() - interval '3 minutes')
	`, issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: dispatch-window comment: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for the dispatch-window comment, got %d", n)
	}
}

func taskTriggerOriginatorCoalesced(t *testing.T, issueID, agentID string) (string, string, []string) {
	t.Helper()
	var trigger, originator string
	var coalesced []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(trigger_comment_id::text, ''),
		       COALESCE(originator_user_id::text, ''),
		       coalesced_comment_ids::text[]
		  FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2
		 ORDER BY created_at DESC
		 LIMIT 1
	`, issueID, agentID).Scan(&trigger, &originator, &coalesced); err != nil {
		t.Fatalf("read task trigger/originator/coalesced: %v", err)
	}
	return trigger, originator, coalesced
}

func containsUUID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestCompleteTask_ReconcilesPreDispatchMergeRaceComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'pre-dispatch race fixture', 'in_progress', 'none', $2, 'member', 999006, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	insertComment := func(content, age string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', now() - $5::interval) RETURNING id
		`, issueID, testWorkspaceID, testUserID, content, age).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	triggerCommentID := insertComment("initial request", "10 minutes")
	deliveredCoalescedID := insertComment("folded in while queued (delivered)", "8 minutes")
	raceCommentID := insertComment("posted before dispatch, merge lost the race", "7 minutes")

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, issue_id, trigger_comment_id, coalesced_comment_ids, delivered_comment_ids, status, priority, created_at, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$5::uuid], ARRAY[$4::uuid, $5::uuid], 'running', 0,
			now() - interval '10 minutes', now() - interval '6 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID, deliveredCoalescedID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for the pre-dispatch race comment, got %d", n)
	}
	trigger, _, coalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
	if trigger != raceCommentID {
		t.Errorf("follow-up trigger must be the race comment %s, got %s", raceCommentID, trigger)
	}
	if containsUUID(coalesced, deliveredCoalescedID) || trigger == deliveredCoalescedID {
		t.Errorf("the already-delivered coalesced comment %s must be excluded from the follow-up, got trigger=%s coalesced=%v",
			deliveredCoalescedID, trigger, coalesced)
	}
}

func TestCompleteTask_ReconcilesPlannedButUndeliveredComments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name              string
		deliveredOldCount int
		wantFollowup      int
	}{
		{name: "partial receipt replays only omitted suffix", deliveredOldCount: 1, wantFollowup: 1},
		{name: "legacy trigger-only receipt replays whole coalesced batch", deliveredOldCount: 0, wantFollowup: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Planned undelivered runtime "+tc.name)
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Planned undelivered agent "+tc.name)
			if _, err := testPool.Exec(ctx, `
				UPDATE issue
				SET assignee_type = 'agent', assignee_id = $2
				WHERE id = $1
			`, issueID, agentID); err != nil {
				t.Fatalf("assign issue: %v", err)
			}

			insertComment := func(content, age string) string {
				t.Helper()
				var id string
				if err := testPool.QueryRow(ctx, `
					INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
					VALUES ($1, $2, 'member', $3, $4, 'comment', now() - $5::interval)
					RETURNING id
				`, issueID, testWorkspaceID, testUserID, content, age).Scan(&id); err != nil {
					t.Fatalf("insert comment: %v", err)
				}
				return id
			}
			old1 := insertComment("planned old one", "10 minutes")
			old2 := insertComment("planned old two", "9 minutes")
			trigger := insertComment("planned trigger", "8 minutes")
			delivered := []string{trigger}
			if tc.deliveredOldCount > 0 {
				delivered = append([]string{old1}, delivered...)
			}

			var taskID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (
					agent_id, runtime_id, issue_id, trigger_comment_id,
					coalesced_comment_ids, delivered_comment_ids,
					status, priority, created_at, dispatched_at, started_at
				)
				VALUES (
					$1, $2, $3, $4,
					ARRAY[$5::uuid, $6::uuid], $7::uuid[],
					'running', 0, now() - interval '5 minutes', now() - interval '4 minutes', now() - interval '3 minutes'
				)
				RETURNING id
			`, agentID, runtimeID, issueID, trigger, old1, old2, delivered).Scan(&taskID); err != nil {
				t.Fatalf("insert running task: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			})

			if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
				t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
				t.Fatalf("expected one bounded follow-up, got %d", n)
			}
			followupTrigger, _, followupCoalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
			covered := append([]string{}, followupCoalesced...)
			covered = append(covered, followupTrigger)
			slices.Sort(covered)
			want := []string{old2}
			if tc.wantFollowup == 2 {
				want = []string{old1, old2}
			}
			slices.Sort(want)
			if !slices.Equal(covered, want) {
				t.Fatalf("follow-up coverage = %v, want %v", covered, want)
			}
		})
	}
}
