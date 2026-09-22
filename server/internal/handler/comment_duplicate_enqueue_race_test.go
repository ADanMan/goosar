package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	dupRaceHeadA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dupRaceHeadB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func dupRaceFixture(t *testing.T, agentName string, issueNumber int) (agentID, issueID, runtimeID string) {
	t.Helper()
	ctx := context.Background()
	agentID = createHandlerTestAgent(t, agentName, nil)
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'dup-enqueue-race fixture', 'in_progress', 'none', $2, 'member', $3, 0, 'agent', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, issueNumber, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })
	return agentID, issueID, runtimeID
}

func insertDupRaceComment(t *testing.T, issueID, content, age string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - $5::interval)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content, age).Scan(&id); err != nil {
		t.Fatalf("insert comment %q: %v", content, err)
	}
	return id
}

func commentCovered(t *testing.T, issueID, agentID, commentID, statusFilter string) bool {
	t.Helper()
	var ok bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT $3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids)
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = $4
	`, issueID, agentID, commentID, statusFilter).Scan(&ok); err != nil {
		t.Fatalf("check covered %s: %v", commentID, err)
	}
	return ok
}

func TestCommentEnqueueRaceQueuedWinnerFoldsLoser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, _ := dupRaceFixture(t, "dup-race-queued", 999311)
	agentUUID := util.MustParseUUID(agentID)
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	winnerCommentID := insertDupRaceComment(t, issueID, "first instruction", "6 minutes")
	if _, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, agentUUID, util.MustParseUUID(winnerCommentID)); err != nil {
		t.Fatalf("enqueue winning task: %v", err)
	}
	loserCommentID := insertDupRaceComment(t, issueID, "second distinct instruction", "1 minute")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	if res := results[agentID]; res.status != DispatchCoalesced {
		t.Fatalf("queued-winner race: got status %q reason %q, want coalesced", res.status, res.reason)
	}
	if !commentCovered(t, issueID, agentID, loserCommentID, "queued") {
		t.Fatal("losing comment was NOT folded into the queued winner — its instruction would be dropped")
	}
	if !commentCovered(t, issueID, agentID, winnerCommentID, "queued") {
		t.Fatal("winner comment is no longer covered after the fold")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1", n)
	}
	for _, leak := range []string{"idx_one_pending_task_per_issue_agent", "level=WARN", "level=ERROR"} {
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("benign enqueue race leaked %q into logs:\n%s", leak, logs.String())
		}
	}
}

func TestCommentEnqueueRaceDispatchedWinnerDurablyCoversLoser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-dispatched", 999312)
	agentUUID := util.MustParseUUID(agentID)
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	loserCommentID := insertDupRaceComment(t, issueID, "losing instruction (predates winner)", "10 minutes")
	winnerCommentID := insertDupRaceComment(t, issueID, "winning instruction", "6 minutes")
	var winnerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, dispatched_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'dispatched', 0, now() - interval '5 minutes', now() - interval '4 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, winnerCommentID).Scan(&winnerTaskID); err != nil {
		t.Fatalf("insert dispatched winner: %v", err)
	}

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	if res := results[agentID]; res.status != DispatchDeferred {
		t.Fatalf("dispatched-winner race: got status %q reason %q, want deferred", res.status, res.reason)
	}

	var plannedHasLoser, deliveredHasLoser bool
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(coalesced_comment_ids), $2::uuid = ANY(delivered_comment_ids)
		FROM agent_task_queue WHERE id = $1
	`, winnerTaskID, loserCommentID).Scan(&plannedHasLoser, &deliveredHasLoser); err != nil {
		t.Fatalf("read winner planned/delivered: %v", err)
	}
	if !plannedHasLoser {
		t.Fatal("losing comment was NOT registered as a planned input on the dispatched winner")
	}
	if deliveredHasLoser {
		t.Fatal("losing comment must NOT be faked as delivered on the dispatched winner")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1 (the dispatched winner)", n)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '1 minute' WHERE id = $1`, winnerTaskID); err != nil {
		t.Fatalf("advance winner to running: %v", err)
	}
	if w := completeTaskViaHandler(t, winnerTaskID, "done"); w.Code != 200 {
		t.Fatalf("complete winner: got %d: %s", w.Code, w.Body.String())
	}
	if n := queuedTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up covering the loser after completion, got %d", n)
	}
	if !commentCovered(t, issueID, agentID, loserCommentID, "queued") {
		t.Fatal("follow-up does not cover the losing comment — the instruction was dropped")
	}
}

func TestCommentEnqueueRaceDifferentHeadNotCoalesced(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-headshift", 999313)
	agentUUID := util.MustParseUUID(agentID)

	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha)
		VALUES ($1, 1, 'adanman', 'goosar', 999313, 'review PR', 'open', 'https://example.test/pr', now(), now(), $2)
		RETURNING id
	`, testWorkspaceID, dupRaceHeadB).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	headAcommentID := insertDupRaceComment(t, issueID, "review head A", "6 minutes")
	var winnerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'queued', 0, now() - interval '5 minutes', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, headAcommentID, dupRaceHeadA).Scan(&winnerTaskID); err != nil {
		t.Fatalf("insert head-A winner: %v", err)
	}
	headBcommentID := insertDupRaceComment(t, issueID, "please review head B", "1 minute")

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(headBcommentID), []commentAgentTrigger{trigger})

	if res := results[agentID]; res.status == DispatchDeferred || res.status == DispatchCoalesced || res.status == DispatchQueued {
		t.Fatalf("different-head race: got success-shaped %q/%q, want a truthful non-success", res.status, res.reason)
	}
	if res := results[agentID]; res.status != DispatchBlocked || res.reason != ReasonInternalError {
		t.Fatalf("different-head race: got %q/%q, want blocked/internal_error", res.status, res.reason)
	}

	var headBinA bool
	var winnerHead string
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(coalesced_comment_ids), COALESCE(context->>'head_sha','')
		FROM agent_task_queue WHERE id = $1
	`, winnerTaskID, headBcommentID).Scan(&headBinA, &winnerHead); err != nil {
		t.Fatalf("read head-A winner: %v", err)
	}
	if headBinA {
		t.Fatal("head-B comment was wrongly folded into the head-A run (TEN-356 violation)")
	}
	if winnerHead != dupRaceHeadA {
		t.Fatalf("head-A winner head_sha changed to %q, want unchanged %q", winnerHead, dupRaceHeadA)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '1 minute' WHERE id = $1`, winnerTaskID); err != nil {
		t.Fatalf("advance head-A winner to running: %v", err)
	}
	if w := completeTaskViaHandler(t, winnerTaskID, "done"); w.Code != 200 {
		t.Fatalf("complete head-A winner: got %d: %s", w.Code, w.Body.String())
	}
	var followupHead string
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(context->>'head_sha','')
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		ORDER BY created_at DESC LIMIT 1
	`, issueID, agentID).Scan(&followupHead); err != nil {
		t.Fatalf("read follow-up head: %v", err)
	}
	if followupHead != dupRaceHeadB {
		t.Fatalf("follow-up head_sha = %q, want head B %q (head B must earn its own coverage)", followupHead, dupRaceHeadB)
	}
}

func TestRegisterPlannedCommentForActiveTaskExcludesQueued(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-register-scope", 999314)
	commentID := insertDupRaceComment(t, issueID, "planned candidate", "1 minute")
	triggerID := insertDupRaceComment(t, issueID, "winner trigger", "6 minutes")

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at)
		VALUES ($1, $2, $3, $4, 'queued', 0, now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerID).Scan(&taskID); err != nil {
		t.Fatalf("insert queued task: %v", err)
	}

	params := db.RegisterPlannedCommentForActiveTaskParams{
		CommentID: util.MustParseUUID(commentID),
		IssueID:   util.MustParseUUID(issueID),
		AgentID:   util.MustParseUUID(agentID),
	}

	if _, err := testHandler.Queries.RegisterPlannedCommentForActiveTask(ctx, params); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("register against a QUEUED task: err = %v, want pgx.ErrNoRows (queued excluded)", err)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatalf("flip to dispatched: %v", err)
	}
	row, err := testHandler.Queries.RegisterPlannedCommentForActiveTask(ctx, params)
	if err != nil {
		t.Fatalf("register against a DISPATCHED task: %v", err)
	}
	var found bool
	for _, id := range row.CoalescedCommentIds {
		if uuidToString(id) == commentID {
			found = true
		}
	}
	if !found {
		t.Fatalf("dispatched register did not add the planned comment: %v", row.CoalescedCommentIds)
	}
}

func TestCommentEnqueueRaceQueuedWinnerReattributesOriginator(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, _ := dupRaceFixture(t, "dup-race-reattr", 999315)
	agentUUID := util.MustParseUUID(agentID)
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	var m2 string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Race M2', 'race-m2-999315@goosar.test') RETURNING id`).Scan(&m2); err != nil {
		t.Fatalf("create M2 user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, m2) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, m2); err != nil {
		t.Fatalf("create M2 member: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, m2) })

	winnerCommentID := insertDupRaceComment(t, issueID, "M1 instruction", "6 minutes")
	if _, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, agentUUID, util.MustParseUUID(winnerCommentID)); err != nil {
		t.Fatalf("enqueue winning task: %v", err)
	}

	var loserCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'M2 instruction', 'comment', now() - interval '1 minute')
		RETURNING id
	`, issueID, testWorkspaceID, m2).Scan(&loserCommentID); err != nil {
		t.Fatalf("insert M2 loser comment: %v", err)
	}

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})
	if res := results[agentID]; res.status != DispatchCoalesced {
		t.Fatalf("queued-winner reattribution race: got status %q reason %q, want coalesced", res.status, res.reason)
	}

	var trig, orig string
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(trigger_comment_id::text,''), COALESCE(originator_user_id::text,'')
		FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, agentID).Scan(&trig, &orig); err != nil {
		t.Fatalf("read winner attribution: %v", err)
	}
	if trig != loserCommentID {
		t.Fatalf("trigger_comment_id = %s, want repointed to M2's comment %s", trig, loserCommentID)
	}
	if orig != m2 {
		t.Fatalf("originator_user_id = %s, want re-attributed to M2 %s (bare append would leave M1)", orig, m2)
	}
}

func TestCommentEnqueueRaceNewerDifferentHeadNotDeferred(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-newer-head", 999317)
	agentUUID := util.MustParseUUID(agentID)

	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha)
		VALUES ($1, 1, 'adanman', 'goosar', 999317, 'review PR', 'open', 'https://example.test/pr', now(), now(), $2)
		RETURNING id
	`, testWorkspaceID, dupRaceHeadB).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	loserCommentID := insertDupRaceComment(t, issueID, "older losing comment", "5 minutes")
	winnerCommentID := insertDupRaceComment(t, issueID, "head A trigger", "4 minutes")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'queued', 0, now() - interval '1 minute', jsonb_build_object('head_sha', $5::text))
	`, agentID, runtimeID, issueID, winnerCommentID, dupRaceHeadA); err != nil {
		t.Fatalf("insert newer head-A winner: %v", err)
	}

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	res := results[agentID]
	if res.status == DispatchDeferred || res.status == DispatchCoalesced || res.status == DispatchQueued {
		t.Fatalf("newer different-head: got success-shaped %q/%q, want a truthful non-success (not deferred/coalesced/queued)", res.status, res.reason)
	}
	if res.status != DispatchBlocked || res.reason != ReasonInternalError {
		t.Fatalf("newer different-head: got %q/%q, want blocked/internal_error", res.status, res.reason)
	}

	var folded bool
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(coalesced_comment_ids) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $3 AND context->>'head_sha' = $4
	`, issueID, loserCommentID, agentID, dupRaceHeadA).Scan(&folded); err != nil {
		t.Fatalf("read winner coalesced: %v", err)
	}
	if folded {
		t.Fatal("losing comment was wrongly attached to the newer different-head task")
	}
}

func TestCommentEnqueueRaceMixedCoveringAndNewerNotDeferred(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-mixed", 999318)
	agentUUID := util.MustParseUUID(agentID)

	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha)
		VALUES ($1, 1, 'adanman', 'goosar', 999318, 'review PR', 'open', 'https://example.test/pr', now(), now(), $2)
		RETURNING id
	`, testWorkspaceID, dupRaceHeadB).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	aTrigger := insertDupRaceComment(t, issueID, "A trigger", "11 minutes")
	var taskA string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '9 minutes', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, aTrigger, dupRaceHeadA).Scan(&taskA); err != nil {
		t.Fatalf("insert running task A: %v", err)
	}
	loserCommentID := insertDupRaceComment(t, issueID, "losing comment C", "5 minutes")
	bTrigger := insertDupRaceComment(t, issueID, "B trigger", "2 minutes")
	var taskB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'queued', 0, now() - interval '1 minute', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, bTrigger, dupRaceHeadA).Scan(&taskB); err != nil {
		t.Fatalf("insert queued task B: %v", err)
	}

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	res := results[agentID]
	if res.status == DispatchDeferred || res.status == DispatchCoalesced || res.status == DispatchQueued {
		t.Fatalf("mixed covering+newer: got success-shaped %q/%q, want a truthful non-success", res.status, res.reason)
	}
	if res.status != DispatchBlocked || res.reason != ReasonInternalError {
		t.Fatalf("mixed covering+newer: got %q/%q, want blocked/internal_error", res.status, res.reason)
	}

	if w := completeTaskViaHandler(t, taskA, "done"); w.Code != 200 {
		t.Fatalf("complete A: %d: %s", w.Code, w.Body.String())
	}
	if commentCovered(t, issueID, agentID, loserCommentID, "queued") {
		t.Fatal("C was folded into the different-head queued blocker (TEN-356 violation)")
	}
}

func commentCoveredByAnyActiveTask(t *testing.T, issueID, agentID, commentID string) bool {
	t.Helper()
	var ok bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2
			  AND status IN ('queued','dispatched','running','waiting_local_directory')
			  AND ($3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids))
		)
	`, issueID, agentID, commentID).Scan(&ok); err != nil {
		t.Fatalf("check coverage: %v", err)
	}
	return ok
}

func coveringTaskHead(t *testing.T, issueID, agentID, commentID string) string {
	t.Helper()
	var head string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(context->>'head_sha','')
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND status IN ('queued','dispatched','running','waiting_local_directory')
		  AND ($3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids))
		ORDER BY created_at DESC LIMIT 1
	`, issueID, agentID, commentID).Scan(&head); err != nil {
		t.Fatalf("read covering task head: %v", err)
	}
	return head
}

func seedDupRacePR(t *testing.T, issueID string, prNumber int) {
	t.Helper()
	ctx := context.Background()
	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha)
		VALUES ($1, 1, 'adanman', 'goosar', $2, 'review PR', 'open', 'https://example.test/pr', now(), now(), $3)
		RETURNING id
	`, testWorkspaceID, prNumber, dupRaceHeadB).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}
}

func TestReconcileBlockedReplayPropagatesToClaimedBlocker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-propagate-claimed", 999319)
	seedDupRacePR(t, issueID, 999319)

	aTrigger := insertDupRaceComment(t, issueID, "A trigger", "11 minutes")
	loserCommentID := insertDupRaceComment(t, issueID, "comment C needing coverage", "8 minutes")
	bTrigger := insertDupRaceComment(t, issueID, "B trigger", "6 minutes")

	var taskA string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, coalesced_comment_ids, delivered_comment_ids, status, priority, created_at, started_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$5::uuid], ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '9 minutes', jsonb_build_object('head_sha', $6::text))
		RETURNING id
	`, agentID, runtimeID, issueID, aTrigger, loserCommentID, dupRaceHeadA).Scan(&taskA); err != nil {
		t.Fatalf("insert running task A: %v", err)
	}

	var taskB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, dispatched_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'dispatched', 0, now() - interval '5 minutes', now() - interval '4 minutes', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, bTrigger, dupRaceHeadA).Scan(&taskB); err != nil {
		t.Fatalf("insert dispatched blocker B: %v", err)
	}

	if w := completeTaskViaHandler(t, taskA, "done"); w.Code != 200 {
		t.Fatalf("complete A: %d: %s", w.Code, w.Body.String())
	}
	var plannedOnB, deliveredOnB bool
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(coalesced_comment_ids), $2::uuid = ANY(delivered_comment_ids)
		FROM agent_task_queue WHERE id = $1
	`, taskB, loserCommentID).Scan(&plannedOnB, &deliveredOnB); err != nil {
		t.Fatalf("read blocker B: %v", err)
	}
	if !plannedOnB {
		t.Fatal("blocked replay was discarded — C was not handed to the blocker, so it is lost")
	}
	if deliveredOnB {
		t.Fatal("C must not be faked as delivered on the already-claimed blocker")
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskB); err != nil {
		t.Fatalf("advance B to running: %v", err)
	}
	if w := completeTaskViaHandler(t, taskB, "done"); w.Code != 200 {
		t.Fatalf("complete B: %d: %s", w.Code, w.Body.String())
	}
	if !commentCoveredByAnyActiveTask(t, issueID, agentID, loserCommentID) {
		t.Fatal("after the blocker chain drained, no task covers C — the deferral was not durable")
	}

	if head := coveringTaskHead(t, issueID, agentID, loserCommentID); head != dupRaceHeadB {
		t.Fatalf("covering task head_sha = %q, want the current head %q", head, dupRaceHeadB)
	}
}

func TestReconcileBlockedReplayPropagatesToQueuedBlocker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-propagate-queued", 999320)
	seedDupRacePR(t, issueID, 999320)

	aTrigger := insertDupRaceComment(t, issueID, "A trigger", "11 minutes")

	loserCommentID := insertDupRaceComment(t, issueID, "comment C posted during the run", "5 minutes")
	bTrigger := insertDupRaceComment(t, issueID, "B trigger", "2 minutes")

	var taskA string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '9 minutes', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, aTrigger, dupRaceHeadA).Scan(&taskA); err != nil {
		t.Fatalf("insert running task A: %v", err)
	}

	var taskB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, context)
		VALUES ($1, $2, $3, $4, 'queued', 0, now() - interval '1 minute', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, bTrigger, dupRaceHeadB).Scan(&taskB); err != nil {
		t.Fatalf("insert queued blocker B: %v", err)
	}

	if w := completeTaskViaHandler(t, taskA, "done"); w.Code != 200 {
		t.Fatalf("complete A: %d: %s", w.Code, w.Body.String())
	}

	if !commentCoveredByAnyActiveTask(t, issueID, agentID, loserCommentID) {
		t.Fatal("blocked replay was discarded — C is covered by no active task, so it is lost")
	}

	if !commentCoveredByAnyActiveTask(t, issueID, agentID, bTrigger) {
		t.Fatal("the blocker's own trigger comment lost coverage during the hand-off")
	}
	if head := coveringTaskHead(t, issueID, agentID, loserCommentID); head != dupRaceHeadB {
		t.Fatalf("covering task head_sha = %q, want the current head %q", head, dupRaceHeadB)
	}
}

func TestReconcileBlockedReplayNeverFoldsIntoDifferentHeadQueuedBlocker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-propagate-crosshead", 999321)
	seedDupRacePR(t, issueID, 999321)

	aTrigger := insertDupRaceComment(t, issueID, "A trigger", "11 minutes")
	loserCommentID := insertDupRaceComment(t, issueID, "comment C posted during the run", "5 minutes")
	bTrigger := insertDupRaceComment(t, issueID, "B trigger", "2 minutes")

	var taskA string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, started_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'running', 0, now() - interval '10 minutes', now() - interval '9 minutes', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, aTrigger, dupRaceHeadA).Scan(&taskA); err != nil {
		t.Fatalf("insert running task A: %v", err)
	}

	var taskB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, context)
		VALUES ($1, $2, $3, $4, 'queued', 0, now() - interval '1 minute', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, bTrigger, dupRaceHeadA).Scan(&taskB); err != nil {
		t.Fatalf("insert queued cross-head blocker B: %v", err)
	}

	if w := completeTaskViaHandler(t, taskA, "done"); w.Code != 200 {
		t.Fatalf("complete A: %d: %s", w.Code, w.Body.String())
	}

	var folded bool
	var bHead string
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = trigger_comment_id OR $2::uuid = ANY(coalesced_comment_ids), COALESCE(context->>'head_sha','')
		FROM agent_task_queue WHERE id = $1
	`, taskB, loserCommentID).Scan(&folded, &bHead); err != nil {
		t.Fatalf("read cross-head blocker: %v", err)
	}
	if folded {
		t.Fatal("C was folded into a DIFFERENT-head queued run — an old-head run would consume a current-head request (TEN-356)")
	}
	if bHead != dupRaceHeadA {
		t.Fatalf("blocker head_sha = %q, want unchanged %q", bHead, dupRaceHeadA)
	}
}
