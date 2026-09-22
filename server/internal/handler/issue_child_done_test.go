package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type childDoneFixture struct {
	parent IssueResponse
	child  IssueResponse
}

func newChildDoneFixture(t *testing.T, parentStatus string) childDoneFixture {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "child-done parent " + time.Now().Format(time.RFC3339Nano),
		"status": parentStatus,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child-done child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parent.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()

		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, child.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	return childDoneFixture{parent: parent, child: child}
}

func updateChildStatus(t *testing.T, childID, status string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+childID, map[string]any{"status": status})
	req = withURLParam(req, "id", childID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue child status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

func countSystemCommentsOn(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system'`,
		issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count system comments: %v", err)
	}
	return n
}

func systemCommentOn(t *testing.T, issueID string) (content, authorIDStr string, parentNull bool, typeStr string) {
	t.Helper()
	row := testPool.QueryRow(context.Background(),
		`SELECT content, author_id::text, parent_id IS NULL, type
		   FROM comment
		   WHERE issue_id = $1 AND author_type = 'system'
		   ORDER BY created_at DESC
		   LIMIT 1`,
		issueID)
	if err := row.Scan(&content, &authorIDStr, &parentNull, &typeStr); err != nil {
		t.Fatalf("read system comment: %v", err)
	}
	return
}

func TestChildDoneNotifiesParent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, authorID, parentNull, typeStr := systemCommentOn(t, fx.parent.ID)

	if !parentNull {
		t.Errorf("system comment must be top-level (parent_id IS NULL)")
	}
	if typeStr != "system" {
		t.Errorf("system comment type should be 'system', got %q", typeStr)
	}
	if authorID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("system comment author_id should be the zero UUID sentinel, got %q", authorID)
	}

	if !strings.Contains(content, fx.child.Identifier) {
		t.Errorf("expected comment to contain child identifier %q, got: %s", fx.child.Identifier, content)
	}
	if strings.Contains(content, "MUL-") {
		t.Errorf("comment must not hardcode MUL- prefix, got: %s", content)
	}

	if !strings.Contains(content, "mention://issue/"+fx.child.ID) {
		t.Errorf("expected mention://issue/<child-id> link in comment, got: %s", content)
	}
	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("parent has no assignee but comment included %q mention, got: %s", banned, content)
		}
	}
}

func TestChildDoneNotificationIsIdempotent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after first done: expected 1 comment, got %d", got)
	}

	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after second done: expected still 1 comment (idempotent), got %d", got)
	}
}

func TestChildReopenAndDoneFiresAgain(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")
	updateChildStatus(t, fx.child.ID, "in_progress")
	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 2 {
		t.Fatalf("expected 2 system comments after reopen+done cycle, got %d", got)
	}
}

func TestChildDoneSkippedWhenParentDone(t *testing.T) {
	fx := newChildDoneFixture(t, "done")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'done' should not receive notification, got %d comments", got)
	}
}

func TestChildDoneSkippedWhenParentCancelled(t *testing.T) {
	fx := newChildDoneFixture(t, "cancelled")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'cancelled' should not receive notification, got %d comments", got)
	}
}

func TestChildDoneSkippedWhenParentBacklog(t *testing.T) {
	fx := newChildDoneFixture(t, "backlog")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'backlog' should not receive notification, got %d comments", got)
	}
}

func TestChildDoneSkippedWhenNoParent(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "orphan child-done " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create orphan: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var orphan IssueResponse
	json.NewDecoder(w.Body).Decode(&orphan)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, orphan.ID)
	})

	updateChildStatus(t, orphan.ID, "done")

	if got := countSystemCommentsOn(t, orphan.ID); got != 0 {
		t.Errorf("orphan must not receive a self-notification, got %d system comments", got)
	}
}

func setIssueAssigneeDirect(t *testing.T, issueID, assigneeType, assigneeID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET assignee_type = $2, assignee_id = $3 WHERE id = $1`,
		issueID, assigneeType, assigneeID,
	); err != nil {
		t.Fatalf("set parent assignee: %v", err)
	}
}

func parentSystemCommentContent(t *testing.T, issueID string) string {
	t.Helper()
	if got := countSystemCommentsOn(t, issueID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, issueID)
	return content
}

func countPendingTasksForAgent(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue
		   WHERE issue_id = $1 AND agent_id = $2
		     AND status IN ('queued', 'dispatched', 'running')`,
		issueID, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	return n
}

func countInboxItems(t *testing.T, recipientUserID, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item
		   WHERE recipient_id = $1 AND issue_id = $2`,
		recipientUserID, issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	return n
}

func TestChildDoneMentionsParentAssignee_Agent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	wantMention := "mention://agent/" + agentID
	if !strings.Contains(content, wantMention) {
		t.Errorf("expected %q in system comment, got: %s", wantMention, content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, agentID); got != 1 {
		t.Errorf("expected 1 pending task for parent agent, got %d", got)
	}
}

func TestChildDoneSkippedWhenParentMember(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	var userID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT user_id FROM member WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&userID); err != nil {
		t.Fatalf("locate workspace member: %v", err)
	}
	setIssueAssigneeDirect(t, fx.parent.ID, "member", userID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent with member assignee should not receive a system comment, got %d", got)
	}
	if got := countInboxItems(t, userID, fx.parent.ID); got != 0 {
		t.Errorf("parent with member assignee should not receive an inbox row, got %d", got)
	}
}

func TestChildDoneMentionsParentAssignee_Squad(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	wantMention := "mention://squad/" + sq.SquadID
	if !strings.Contains(content, wantMention) {
		t.Errorf("expected %q in system comment, got: %s", wantMention, content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task for parent squad, got %d", got)
	}
}

func TestChildDoneTriggersParentAgentWhenSameAgentOwnsChild(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}

	setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
	setIssueAssigneeDirect(t, fx.child.ID, "agent", agentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://agent/"+agentID) {
		t.Errorf("expected parent-assignee mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, agentID); got != 1 {
		t.Errorf("expected 1 pending task on parent (serial sub-task handoff), got %d", got)
	}
}

func TestChildDoneTriggersParentAgentWhenChildSquadSharesLeader(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	setIssueAssigneeDirect(t, fx.parent.ID, "agent", sq.LeaderID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://agent/"+sq.LeaderID) {
		t.Errorf("expected parent-agent mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending task on parent (serial sub-task handoff), got %d", got)
	}
}

func TestChildDoneWakesLeaderWhenParentAndChildSquadsShareLeader(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	parentSquad := newSquadCommentTriggerFixture(t)

	ctx := context.Background()
	var childSquadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Child Done Shared Leader Squad", parentSquad.LeaderID, testUserID).
		Scan(&childSquadID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, childSquadID)
	})

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", parentSquad.SquadID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", childSquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+parentSquad.SquadID) {
		t.Errorf("expected parent-squad mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, parentSquad.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task on parent (shared-leader guard removed, MUL-3969), got %d", got)
	}
}

func TestChildDoneWakesLeaderWhenChildIsSameSquad(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)

	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", sq.SquadID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`,
			fx.parent.ID, fx.child.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "mention://squad/"+sq.SquadID) {
		t.Errorf("expected parent-squad mention in system comment, got: %s", content)
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, sq.LeaderID); got != 1 {
		t.Errorf("expected 1 pending leader task for same-squad child (MUL-3969), got %d", got)
	}
}

func TestStageLeaderPrepareTimeoutRetryCanAdvanceNextStage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	sq := newSquadCommentTriggerFixture(t)
	setIssueAssigneeDirect(t, fx.parent.ID, "squad", sq.SquadID)
	setIssueAssigneeDirect(t, fx.child.ID, "squad", sq.SquadID)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET stage = 1 WHERE id = $1`, fx.child.ID); err != nil {
		t.Fatalf("set stage 1: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "stage 2 after prepare timeout",
		"status":          "backlog",
		"parent_issue_id": fx.parent.ID,
		"stage":           2,
		"assignee_type":   "squad",
		"assignee_id":     sq.SquadID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create stage 2: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var stage2 IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&stage2); err != nil {
		t.Fatalf("decode stage 2: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`, fx.parent.ID, stage2.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, stage2.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")
	content := parentSystemCommentContent(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 2 is next") {
		t.Fatalf("stage barrier comment does not identify Stage 2: %s", content)
	}

	var originalID, originalSquadID, originalTriggerID string
	var originalLeader bool
	if err := testPool.QueryRow(ctx, `
		SELECT id::text, is_leader_task, squad_id::text, trigger_comment_id::text
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		ORDER BY created_at DESC
		LIMIT 1
	`, fx.parent.ID, sq.LeaderID).Scan(&originalID, &originalLeader, &originalSquadID, &originalTriggerID); err != nil {
		t.Fatalf("load Stage 1 leader wake: %v", err)
	}
	if !originalLeader || originalSquadID != sq.SquadID || originalTriggerID == "" {
		t.Fatalf("leader wake provenance = leader:%v squad:%q trigger:%q", originalLeader, originalSquadID, originalTriggerID)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'dispatched', dispatched_at = now()
		WHERE id = $1
	`, originalID); err != nil {
		t.Fatalf("dispatch original leader task: %v", err)
	}

	if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(originalID), "task preparation timed out after 5m0s", "", "", "timeout", false); err != nil {
		t.Fatalf("fail original leader task: %v", err)
	}

	var retryID, retryStatus, retrySquadID, retryTriggerID string
	var retryLeader bool
	var retryAttempt int32
	if err := testPool.QueryRow(ctx, `
		SELECT id::text, status, is_leader_task, squad_id::text,
		       trigger_comment_id::text, attempt
		FROM agent_task_queue
		WHERE parent_task_id = $1
	`, originalID).Scan(&retryID, &retryStatus, &retryLeader, &retrySquadID, &retryTriggerID, &retryAttempt); err != nil {
		t.Fatalf("load automatic retry: %v", err)
	}
	if retryStatus != "queued" || retryAttempt != 2 || !retryLeader || retrySquadID != originalSquadID || retryTriggerID != originalTriggerID {
		t.Fatalf("retry provenance = status:%q attempt:%d leader:%v squad:%q trigger:%q; want queued attempt 2 with original leader context",
			retryStatus, retryAttempt, retryLeader, retrySquadID, retryTriggerID)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'running', dispatched_at = now(), started_at = now()
		WHERE id = $1
	`, retryID); err != nil {
		t.Fatalf("start retry leader task: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+stage2.ID, map[string]any{"status": "todo"})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", sq.LeaderID)
	req.Header.Set("X-Task-ID", retryID)
	req = withURLParam(req, "id", stage2.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retry leader promote Stage 2: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stage2Status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, stage2.ID).Scan(&stage2Status); err != nil {
		t.Fatalf("load promoted Stage 2: %v", err)
	}
	if stage2Status != "todo" {
		t.Fatalf("Stage 2 status = %q, want todo", stage2Status)
	}
	if got := countPendingTasksForAgent(t, stage2.ID, sq.LeaderID); got != 1 {
		t.Fatalf("promoted Stage 2 queued %d leader tasks, want 1", got)
	}
}
