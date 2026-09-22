package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type batchClaimReceiptResponse struct {
	Tasks []struct {
		ID                  string   `json:"id"`
		RuntimeID           string   `json:"runtime_id"`
		AuthToken           string   `json:"auth_token"`
		DeliveredCommentIDs []string `json:"delivered_comment_ids"`
	} `json:"tasks"`
}

func TestClaimTasksByRuntime_MaxTasksZeroClaimsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch max0 rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch max0 agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("max_tasks=0 claimed %d tasks, want 0", len(resp.Tasks))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued", status)
	}
}

func TestClaimTasksByRuntime_MaxTasksNegativeIsBadRequest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	rt := createClaimReclaimRuntime(t, context.Background(), "Batch neg rt")
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, -1)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative max_tasks, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClaimTasksByRuntime_SkipsInvalidRuntimeID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch badid rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch badid agent")
	seedQueuedIssueTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{"not-a-uuid", rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (invalid id skipped, not 500), got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].RuntimeID != rt {
		t.Fatalf("expected the valid runtime's task to be claimed despite the invalid id, got %+v", resp.Tasks)
	}
}

func seedCommentBackedQueuedTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string) (string, string) {
	t.Helper()
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please handle this')
		RETURNING id
	`, testWorkspaceID, issueID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority)
		VALUES ($1, $2, $3, $4, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID, commentID).Scan(&taskID); err != nil {
		t.Fatalf("seed comment-backed task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID, commentID
}

func assertCommentDelivered(t *testing.T, ctx context.Context, taskID, commentID string) {
	t.Helper()
	var member bool
	if err := testPool.QueryRow(ctx, `
		SELECT $1 = ANY(delivered_comment_ids) FROM agent_task_queue WHERE id = $2
	`, commentID, taskID).Scan(&member); err != nil {
		t.Fatalf("read delivered_comment_ids: %v", err)
	}
	if !member {
		t.Fatalf("trigger comment %s not persisted in task %s delivered_comment_ids", commentID, taskID)
	}
}

func TestClaimTasksByRuntime_PersistsCommentDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch receipt rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch receipt agent")
	taskID, commentID := seedCommentBackedQueuedTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("claimed %d tasks, want 1: %s", len(resp.Tasks), w.Body.String())
	}
	found := false
	for _, id := range resp.Tasks[0].DeliveredCommentIDs {
		if id == commentID {
			found = true
		}
	}
	if !found {
		t.Fatalf("response delivered_comment_ids %v missing trigger comment %s", resp.Tasks[0].DeliveredCommentIDs, commentID)
	}
	assertCommentDelivered(t, ctx, taskID, commentID)
}

func TestClaimTasksByRuntime_StaleReclaimRecordsDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch stale-receipt rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch stale-receipt agent")

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'stale reclaim comment')
		RETURNING id
	`, testWorkspaceID, i, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'dispatched', 0, now() - interval '120 seconds', NULL)
		RETURNING id
	`, a, rt, i, commentID).Scan(&taskID); err != nil {
		t.Fatalf("seed stale dispatched comment task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != taskID {
		t.Fatalf("expected the stale task %s reclaimed, got %+v", taskID, resp.Tasks)
	}
	assertCommentDelivered(t, ctx, taskID, commentID)
}

func TestClaimTasksByRuntime_SkipsRuntimeOwnedByAnotherDaemon(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var rt string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'other-daemon-machine', 'Other daemon RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&rt); err != nil {
		t.Fatalf("create other-daemon runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rt) })

	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Other daemon agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("daemon-A claimed %d tasks from a runtime owned by daemon-B, want 0", len(resp.Tasks))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued for the owning daemon", status)
	}
}

func TestClaimTasksByRuntime_RequiresDaemonID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/claim",
		map[string]any{"runtime_ids": []string{}, "max_tasks": 5},
		testWorkspaceID, batchClaimTestDaemonID)
	testHandler.ClaimTasksByRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when daemon_id is missing, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClaimTasksByRuntime_RepairsStaleCommentPlan(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Stale plan rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rt, "Stale plan agent")

	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type='agent', assignee_id=$1 WHERE id=$2`, agentID, issueID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}

	var survivorID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please still handle this')
		RETURNING id
	`, testWorkspaceID, issueID, testUserID).Scan(&survivorID); err != nil {
		t.Fatalf("seed survivor comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, survivorID) })

	var staleID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, coalesced_comment_ids)
		VALUES ($1, $2, $3, 'queued', 0, ARRAY[$4]::uuid[])
		RETURNING id
	`, agentID, rt, issueID, survivorID).Scan(&staleID); err != nil {
		t.Fatalf("seed stale task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, staleID) })
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, task := range resp.Tasks {
		if task.ID == staleID {
			t.Fatalf("stale-plan task %s was dispatched by the batch path; want it repaired/omitted", staleID)
		}
	}

	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, staleID).Scan(&status); err != nil {
		t.Fatalf("read stale status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("stale task status = %s, want cancelled", status)
	}

	var rebuilt int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND trigger_comment_id = $2 AND id <> $3
	`, issueID, survivorID, staleID).Scan(&rebuilt); err != nil {
		t.Fatalf("count rebuilt: %v", err)
	}
	if rebuilt < 1 {
		t.Fatalf("expected the surviving comment rebuilt into a new trigger task, found %d", rebuilt)
	}
}
