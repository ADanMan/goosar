package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestMergeCommentIntoPendingTask_FailClosedKeepsOriginalSnapshot(t *testing.T) {
	ctx := context.Background()
	agentID := createWebhookTestAgent(t, "MergeFailClosed Agent")

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority)
		VALUES ($1, 'merge fail-closed', 'member', $2, 'agent', $3, 'medium') RETURNING id`,
		testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	var cidA string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please do X') RETURNING id`,
		issueID, testWorkspaceID, testUserID).Scan(&cidA); err != nil {
		t.Fatalf("seed comment A: %v", err)
	}

	var cidB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'autonomous follow-up') RETURNING id`,
		issueID, testWorkspaceID, agentID).Scan(&cidB); err != nil {
		t.Fatalf("seed comment B: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, issue_id, trigger_comment_id, status, priority,
			 originator_user_id, accountable_user_id, originator_source,
			 trigger_evidence_kind, trigger_evidence_ref_id)
		VALUES ($1, $2, $3, $4, 'queued', 0, $5, $5, 'comment_source', 'comment', $4)`,
		agentID, runtimeID, issueID, cidA, testUserID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}

	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	issue := db.Issue{ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID)}
	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee}

	readSnapshot := func() (triggerComment, originator, accountable, source string) {
		if err := testPool.QueryRow(ctx, `
			SELECT COALESCE(trigger_comment_id::text,''), COALESCE(originator_user_id::text,''),
			       COALESCE(accountable_user_id::text,''), COALESCE(originator_source,'')
			  FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
			issueID, agentID).Scan(&triggerComment, &originator, &accountable, &source); err != nil {
			t.Fatalf("read snapshot: %v", err)
		}
		return
	}

	if _, err := testPool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = true WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("set fail-closed: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET attribution_fail_closed = false WHERE id = $1`, testWorkspaceID)
	})

	countTasks := func() int {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, agentID).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}
	before := countTasks()

	if result := testHandler.mergeCommentIntoPendingTask(ctx, issue, trigger, parseUUID(cidB), pgtype.Text{}); result != commentMergeAttributionBlocked {
		t.Fatalf("fail-closed merge result = %d, want commentMergeAttributionBlocked (refused, non-success)", result)
	}
	if after := countTasks(); after != before {
		t.Errorf("task count changed %d -> %d on refused merge; a fail-closed refusal must not spawn a task", before, after)
	}

	if status, reason, terminal := commentMergeTerminalOutcome(commentMergeAttributionBlocked); !terminal || status != DispatchBlocked || reason != ReasonAttributionBlocked {
		t.Errorf("attribution-blocked outcome = %s/%s (terminal=%v), want blocked/attribution_blocked", status, reason, terminal)
	}
	tc, orig, acc, src := readSnapshot()
	if tc != cidA {
		t.Errorf("trigger_comment_id = %s, want unchanged %s (fail-closed must keep the original trigger)", tc, cidA)
	}
	if orig != testUserID || acc != testUserID {
		t.Errorf("originator/accountable = %s/%s, want unchanged %s (fail-closed must not re-stamp)", orig, acc, testUserID)
	}
	if src != "comment_source" {
		t.Errorf("originator_source = %s, want unchanged comment_source (fail-closed must not degrade)", src)
	}

	if _, err := testPool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = false WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("clear fail-closed: %v", err)
	}
	if result := testHandler.mergeCommentIntoPendingTask(ctx, issue, trigger, parseUUID(cidB), pgtype.Text{}); result != commentMergeSucceeded {
		t.Fatalf("fail-open merge result = %d, want commentMergeSucceeded", result)
	}
	tc2, orig2, _, src2 := readSnapshot()
	if tc2 != cidB {
		t.Errorf("fail-open: trigger_comment_id = %s, want repointed to %s (merge should proceed)", tc2, cidB)
	}
	if src2 != "owner_fallback" {
		t.Errorf("fail-open: originator_source = %s, want owner_fallback (merge proceeded)", src2)
	}
	if orig2 != "" {
		t.Errorf("fail-open owner_fallback: originator = %s, want NULL (authorization carries no human)", orig2)
	}
}
