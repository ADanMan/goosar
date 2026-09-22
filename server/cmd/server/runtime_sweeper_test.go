package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func setupSweeperTestFixture(t *testing.T, taskStatus string) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Sweeper test issue', 'todo', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	var taskID string
	switch taskStatus {
	case "running":
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
			VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
			RETURNING id
		`, agentID, runtimeID, issueID).Scan(&taskID)
	case "dispatched":
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at)
			VALUES ($1, $2, $3, 'dispatched', 0, now() - interval '10 minutes')
			RETURNING id
		`, agentID, runtimeID, issueID).Scan(&taskID)
	}
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}

	_, err = testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID)
	if err != nil {
		t.Fatalf("failed to set agent status: %v", err)
	}

	return issueID, agentID, taskID
}

func cleanupSweeperFixture(t *testing.T, issueID, agentID string) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID)
}

func ageOutAgentRuntime(t *testing.T, agentID string, staleAgo time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime SET last_seen_at = now() - make_interval(secs => $1)
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $2)
	`, staleAgo.Seconds(), agentID); err != nil {
		t.Fatalf("failed to age out agent runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET last_seen_at = now()
			WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
		`, agentID)
	})
}

func TestRefreshAgentStatusFromTasks(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)

	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed idle agent status: %v", err)
	}

	агент, err := queries.RefreshAgentStatusFromTasks(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("RefreshAgentStatusFromTasks with dispatched task failed: %v", err)
	}
	if агент.Status != "working" {
		t.Fatalf("expected dispatched task to refresh agent status to working, got %q", агент.Status)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'cancelled', completed_at = now()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("failed to cancel seeded task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to reseed working agent status: %v", err)
	}

	агент, err = queries.RefreshAgentStatusFromTasks(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("RefreshAgentStatusFromTasks with no active tasks failed: %v", err)
	}
	if агент.Status != "idle" {
		t.Fatalf("expected cancelled-only task set to refresh agent status to idle, got %q", агент.Status)
	}
}

func TestSweepStaleTasksBroadcastsWithWorkspaceID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	queries := db.New(testPool)
	bus := events.New()

	var taskEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("task:failed", func(e events.Event) {
		mu.Lock()
		taskEvents = append(taskEvents, e)
		mu.Unlock()
	})

	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks query failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale task to be failed")
	}

	found := false
	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task %s to be in failed tasks list", taskID)
	}

	broadcastFailedTasks(context.Background(), queries, nil, bus, failedTasks)

	mu.Lock()
	defer mu.Unlock()
	var foundEvent bool
	for _, e := range taskEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["task_id"] == taskID {
			if e.WorkspaceID == "" {
				t.Fatal("task:failed event is missing WorkspaceID — this was the original bug")
			}
			if e.WorkspaceID != testWorkspaceID {
				t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, e.WorkspaceID)
			}
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("expected task:failed event for task %s", taskID)
	}

	var status string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got '%s'", status)
	}
}

func TestSweepStaleTasksReconcileAgentStatus(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, _ := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	queries := db.New(testPool)
	bus := events.New()

	var agentStatusEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("agent:status", func(e events.Event) {
		mu.Lock()
		agentStatusEvents = append(agentStatusEvents, e)
		mu.Unlock()
	})

	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale task")
	}

	broadcastFailedTasks(context.Background(), queries, nil, bus, failedTasks)

	var agentStatus string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus)
	if err != nil {
		t.Fatalf("failed to query agent status: %v", err)
	}
	if agentStatus != "idle" {
		t.Fatalf("expected agent status 'idle', got '%s'", agentStatus)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(agentStatusEvents) == 0 {
		t.Fatal("expected agent:status event to be published")
	}
	lastEvent := agentStatusEvents[len(agentStatusEvents)-1]
	if lastEvent.WorkspaceID == "" {
		t.Fatal("agent:status event should have WorkspaceID set")
	}
	if lastEvent.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, lastEvent.WorkspaceID)
	}
}

func TestSweepDispatchedStaleTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	bus := events.New()

	var taskEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("task:failed", func(e events.Event) {
		mu.Lock()
		taskEvents = append(taskEvents, e)
		mu.Unlock()
	})

	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs: 1.0,
		RunningTimeoutSecs:  9000.0,

		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedTasks) == 0 {
		t.Fatal("expected at least 1 stale dispatched task")
	}

	broadcastFailedTasks(context.Background(), queries, nil, bus, failedTasks)

	var status string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got '%s'", status)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, e := range taskEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["task_id"] == taskID {
			if e.WorkspaceID == "" {
				t.Fatal("task:failed event is missing WorkspaceID — this was the bug")
			}
			if e.WorkspaceID != testWorkspaceID {
				t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, e.WorkspaceID)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task:failed event for task %s", taskID)
	}

	var agentStatus string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus)
	if err != nil {
		t.Fatalf("failed to query agent: %v", err)
	}
	if agentStatus != "idle" {
		t.Fatalf("expected agent status 'idle' after sweep, got '%s'", agentStatus)
	}
}

func TestSweepRunningTaskSkippedWhenRuntimeFresh(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	queries := db.New(testPool)
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatalf("healthy long-running task on live daemon must NOT be swept — that was the MUL-4107 bug")
		}
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected task to stay 'running', got %q", status)
	}
}

func TestSweepRunningTaskKilledWhenRuntimeStale(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	queries := db.New(testPool)
	failedTasks, err := queries.FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	found := false
	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected wall clock to fire when runtime heartbeat is stale, but task %s was not swept", taskID)
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status); err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got %q", status)
	}
}

func TestSweepResetsInProgressIssueToTodo(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Stuck in_progress issue', 'in_progress', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create stale task: %v", err)
	}

	queries := db.New(testPool)
	bus := events.New()

	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	found := false
	for _, ft := range failedTasks {
		if ft.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task %s to be in failed tasks, got %v", taskID, failedTasks)
	}

	broadcastFailedTasks(ctx, queries, nil, bus, failedTasks)

	var issueStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus)
	if err != nil {
		t.Fatalf("failed to query issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("expected issue status 'todo' after sweep, got '%s' — issue is stuck", issueStatus)
	}
}

func TestSweepDoesNotResetIssueAlreadyInReview(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Already in_review issue', 'in_review', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create stale task: %v", err)
	}

	queries := db.New(testPool)
	bus := events.New()

	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	broadcastFailedTasks(ctx, queries, nil, bus, failedTasks)

	var issueStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus)
	if err != nil {
		t.Fatalf("failed to query issue status: %v", err)
	}
	if issueStatus != "in_review" {
		t.Fatalf("expected issue status 'in_review' to be preserved, got '%s'", issueStatus)
	}
}

func TestExpireStaleQueuedTasks(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	mkIssue := func(label string) string {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			WITH bumped AS (
				UPDATE workspace SET issue_counter = issue_counter + 1
				WHERE id = $1 RETURNING issue_counter
			)
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
			SELECT $1, $3, 'todo', 'none', 'member', m.user_id, 'agent', $2, (SELECT issue_counter FROM bumped)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, agentID, label).Scan(&issueID); err != nil {
			t.Fatalf("failed to create %s issue: %v", label, err)
		}
		return issueID
	}
	oldIssueID := mkIssue("Queued TTL test (old)")
	freshIssueID := mkIssue("Queued TTL test (fresh)")
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN ($1, $2)`, oldIssueID, freshIssueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id IN ($1, $2)`, oldIssueID, freshIssueID)
	})

	var oldTaskID, freshTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours')
		RETURNING id
	`, agentID, runtimeID, oldIssueID).Scan(&oldTaskID); err != nil {
		t.Fatalf("failed to insert old queued task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'queued', 0, now())
		RETURNING id
	`, agentID, runtimeID, freshIssueID).Scan(&freshTaskID); err != nil {
		t.Fatalf("failed to insert fresh queued task: %v", err)
	}

	queries := db.New(testPool)
	failed, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    3600.0,
		MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected exactly 1 expired task, got %d", len(failed))
	}
	if failed[0].ID.Bytes != parseUUIDBytes(oldTaskID) {
		t.Fatalf("expired the wrong task: got %x", failed[0].ID.Bytes)
	}

	var oldStatus, oldReason, oldErr string
	if err := testPool.QueryRow(ctx, `
		SELECT status, COALESCE(failure_reason, ''), COALESCE(error, '')
		FROM agent_task_queue WHERE id = $1
	`, oldTaskID).Scan(&oldStatus, &oldReason, &oldErr); err != nil {
		t.Fatalf("failed to read old task: %v", err)
	}
	if oldStatus != "failed" {
		t.Fatalf("old task: expected status=failed, got %q", oldStatus)
	}
	if oldReason != "queued_expired" {
		t.Fatalf("old task: expected failure_reason=queued_expired, got %q", oldReason)
	}
	if !strings.Contains(oldErr, "expired in queue") {
		t.Fatalf("old task: expected error to mention expiry, got %q", oldErr)
	}

	var freshStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT status FROM agent_task_queue WHERE id = $1
	`, freshTaskID).Scan(&freshStatus); err != nil {
		t.Fatalf("failed to read fresh task: %v", err)
	}
	if freshStatus != "queued" {
		t.Fatalf("fresh task: expected status=queued, got %q", freshStatus)
	}
}

func TestExpireStaleQueuedTasksRespectsBatchLimit(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var issueIDs []string
	t.Cleanup(func() {
		for _, id := range issueIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
	})
	for i := 0; i < 5; i++ {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			WITH bumped AS (
				UPDATE workspace SET issue_counter = issue_counter + 1
				WHERE id = $1 RETURNING issue_counter
			)
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
			SELECT $1, 'Queued TTL batch test', 'todo', 'none', 'member', m.user_id, 'agent', $2, (SELECT issue_counter FROM bumped)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, agentID).Scan(&issueID); err != nil {
			t.Fatalf("failed to create issue %d: %v", i, err)
		}
		issueIDs = append(issueIDs, issueID)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
			VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours')
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("failed to insert backlog task %d: %v", i, err)
		}
	}

	queries := db.New(testPool)
	failed, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    3600.0,
		MaxPerTick: 2,
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	if len(failed) != 2 {
		t.Fatalf("expected batch cap of 2, got %d", len(failed))
	}

	var remaining int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE issue_id = ANY($1::uuid[]) AND status = 'queued'
	`, issueIDs).Scan(&remaining); err != nil {
		t.Fatalf("failed to count remaining queued: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("expected 3 queued tasks remaining after batched sweep, got %d", remaining)
	}
}

func parseUUIDBytes(s string) [16]byte {
	s = strings.ReplaceAll(s, "-", "")
	var b [16]byte
	for i := 0; i < 16; i++ {
		hi := unhex(s[i*2])
		lo := unhex(s[i*2+1])
		b[i] = hi<<4 | lo
	}
	return b
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func setAgentRuntimeOffline(t *testing.T, agentID string, lastSeenAgo time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime
		SET status = 'offline', last_seen_at = now() - make_interval(secs => $1)
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $2)
	`, lastSeenAgo.Seconds(), agentID); err != nil {
		t.Fatalf("failed to mark agent runtime offline: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET status = 'online', last_seen_at = now()
			WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
		`, agentID)
	})
}

func TestRuntimeClaimFreshnessMatchesSweeper(t *testing.T) {
	if service.RuntimeClaimFreshnessSeconds != staleThresholdSeconds || staleThresholdSeconds != handler.RuntimeStaleGraceSeconds {
		t.Fatalf("service=%v sweeper=%v handler=%v: the three freshness windows must agree",
			service.RuntimeClaimFreshnessSeconds, staleThresholdSeconds, handler.RuntimeStaleGraceSeconds)
	}
	if minimumRuntimeReconnectGrace != time.Duration(staleThresholdSeconds)*time.Second {
		t.Fatalf("reconnect grace floor %s must equal the stale window", minimumRuntimeReconnectGrace)
	}
	t.Setenv("GOOSAR_RUNTIME_RECONNECT_GRACE", "10s")
	if got := runtimeReconnectGraceFromEnv(); got != minimumRuntimeReconnectGrace {
		t.Fatalf("grace below the stale window must clamp: got %s", got)
	}
	t.Setenv("GOOSAR_RUNTIME_RECONNECT_GRACE", "45m")
	if got := runtimeReconnectGraceFromEnv(); got != 45*time.Minute {
		t.Fatalf("configured grace not honoured: got %s", got)
	}
}

func TestSweepDispatchedTaskWaitsThroughReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	issueID, agentID, taskID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)

	failedTasks, err := db.New(testPool).FailStaleTasks(context.Background(), db.FailStaleTasksParams{
		DispatchTimeoutSecs:       1.0,
		RunningTimeoutSecs:        9000.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	for _, task := range failedTasks {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatal("dispatched task was failed inside reconnect grace")
		}
	}
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "dispatched" {
		t.Fatalf("task status = %q, want dispatched", status)
	}
}

func TestSweepRunningTaskThroughReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)
	queries := db.New(testPool)
	params := db.FailStaleTasksParams{
		DispatchTimeoutSecs:       300.0,
		RunningTimeoutSecs:        1.0,
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
	}

	failedTasks, err := queries.FailStaleTasks(context.Background(), params)
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	for _, task := range failedTasks {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatal("long-running task was failed inside reconnect grace")
		}
	}

	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)
	failedTasks, err = queries.FailStaleTasks(context.Background(), params)
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	found := false
	for _, task := range failedTasks {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
		}
	}
	if !found {
		t.Fatal("running task was not failed once the reconnect grace elapsed")
	}
}

func TestOfflineRuntimeTasksRespectReconnectGrace(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)
	queries := db.New(testPool)
	params := db.FailTasksForOfflineRuntimesParams{
		ReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
		MaxPerTick:         offlineTaskFailBatchSize,
	}

	failed, err := queries.FailTasksForOfflineRuntimes(ctx, params)
	if err != nil {
		t.Fatalf("FailTasksForOfflineRuntimes inside grace: %v", err)
	}
	for _, task := range failed {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			t.Fatal("running task was failed inside reconnect grace")
		}
	}

	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)
	failed, err = queries.FailTasksForOfflineRuntimes(ctx, params)
	if err != nil {
		t.Fatalf("FailTasksForOfflineRuntimes beyond grace: %v", err)
	}
	found := false
	for _, task := range failed {
		if task.ID.Bytes == parseUUIDBytes(taskID) {
			found = true
			if !task.FailureReason.Valid || task.FailureReason.String != "runtime_offline" {
				t.Fatalf("failure reason = %q, want runtime_offline", task.FailureReason.String)
			}
		}
	}
	if !found {
		t.Fatal("task was not failed after reconnect grace elapsed")
	}
}

func TestRuntimeOfflineRetryOnlyPromotesOntoHealthyRuntime(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, agentID, parentID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, 10*time.Minute)

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read runtime id: %v", err)
	}
	var retryID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, fire_at, parent_task_id, retry_of_task_id, attempt, max_attempts)
		SELECT agent_id, runtime_id, issue_id, 'deferred', priority, now() - interval '1 minute', id, id, attempt + 1, max_attempts
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, parentID).Scan(&retryID); err != nil {
		t.Fatalf("insert deferred retry: %v", err)
	}

	taskSvc := service.NewTaskService(db.New(testPool), testPool, nil, events.New())
	if err := taskSvc.PromoteDueDeferredTasksForRuntime(ctx, util.MustParseUUID(runtimeID)); err != nil {
		t.Fatalf("promote on offline runtime: %v", err)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, retryID).Scan(&status); err != nil {
		t.Fatalf("read retry: %v", err)
	}
	if status != "deferred" {
		t.Fatalf("retry promoted onto an offline runtime: status = %q", status)
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("restore runtime: %v", err)
	}
	if err := taskSvc.PromoteDueDeferredTasksForRuntime(ctx, util.MustParseUUID(runtimeID)); err != nil {
		t.Fatalf("promote on healthy runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, retryID).Scan(&status); err != nil {
		t.Fatalf("read retry: %v", err)
	}
	if status != "queued" {
		t.Fatalf("retry not promoted once the runtime came back: status = %q", status)
	}
}

func TestRuntimeReconnectRetryHasBoundedTerminalPath(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, agentID, parentID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'failed', completed_at = now(), failure_reason = 'runtime_offline'
		WHERE id = $1
	`, parentID); err != nil {
		t.Fatalf("fail runtime_offline parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("mark issue in progress: %v", err)
	}
	var retryID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, fire_at, parent_task_id, retry_of_task_id, attempt, max_attempts)
		SELECT agent_id, runtime_id, issue_id, 'deferred', priority, now() - interval '10 minutes', id, id, attempt + 1, max_attempts
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, parentID).Scan(&retryID); err != nil {
		t.Fatalf("insert deferred retry: %v", err)
	}

	queries := db.New(testPool)
	params := db.FailExpiredRuntimeReconnectRetriesParams{
		ReconnectGraceSecs: defaultRuntimeReconnectGrace.Seconds(),
		RuntimeStaleSecs:   staleThresholdSeconds,
		MaxPerTick:         reconnectRetryExpireBatchSize,
	}
	failed, err := queries.FailExpiredRuntimeReconnectRetries(ctx, params)
	if err != nil {
		t.Fatalf("expire retry inside grace: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("retry failed inside reconnect grace: got %d rows", len(failed))
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET fire_at = now() - make_interval(secs => $1) WHERE id = $2`,
		(defaultRuntimeReconnectGrace + time.Hour).Seconds(), retryID); err != nil {
		t.Fatalf("age retry beyond reconnect grace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)`, agentID); err != nil {
		t.Fatalf("restore healthy runtime: %v", err)
	}
	failed, err = queries.FailExpiredRuntimeReconnectRetries(ctx, params)
	if err != nil {
		t.Fatalf("expire retry after healthy reconnect: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("healthy runtime lost reconnect race: got %d rows", len(failed))
	}

	setAgentRuntimeOffline(t, agentID, defaultRuntimeReconnectGrace+time.Hour)
	failed, err = queries.FailExpiredRuntimeReconnectRetries(ctx, params)
	if err != nil {
		t.Fatalf("expire retry beyond grace: %v", err)
	}
	if len(failed) != 1 || failed[0].ID.Bytes != parseUUIDBytes(retryID) {
		t.Fatalf("expired retries = %d, want retry %s", len(failed), retryID)
	}
	if !failed[0].FailureReason.Valid || failed[0].FailureReason.String != "runtime_reconnect_timeout" {
		t.Fatalf("failure reason = %q, want runtime_reconnect_timeout", failed[0].FailureReason.String)
	}

	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())
	taskSvc.HandleFailedTasks(ctx, failed)

	var issueStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("issue status = %q, want todo after terminal reconnect timeout", issueStatus)
	}
	var undrained, retryChildren int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE completed_at IS NULL),
		       count(*) FILTER (WHERE parent_task_id = $1)
		FROM agent_task_queue WHERE issue_id = $2
	`, retryID, issueID).Scan(&undrained, &retryChildren); err != nil {
		t.Fatalf("read terminal retry state: %v", err)
	}
	if undrained != 0 || retryChildren != 0 {
		t.Fatalf("terminal retry leaked work: undrained=%d child_retries=%d", undrained, retryChildren)
	}
}

func TestQueuedTTLExemptsRuntimeOfflineRetryLineage(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, agentID, parentID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'failed', completed_at = now() - interval '5 hours', failure_reason = 'runtime_offline'
		WHERE id = $1
	`, parentID); err != nil {
		t.Fatalf("fail runtime_offline parent: %v", err)
	}
	var retryID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, parent_task_id, retry_of_task_id)
		SELECT agent_id, runtime_id, issue_id, 'queued', priority, now() - interval '5 hours', id, id
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, parentID).Scan(&retryID); err != nil {
		t.Fatalf("insert queued recovery retry: %v", err)
	}

	failed, err := db.New(testPool).ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    3600.0,
		MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	for _, task := range failed {
		if task.ID.Bytes == parseUUIDBytes(retryID) {
			t.Fatal("queued TTL expired a runtime_offline recovery retry; it must wait for the reconnect grace")
		}
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, retryID).Scan(&status); err != nil {
		t.Fatalf("read retry: %v", err)
	}
	if status != "queued" {
		t.Fatalf("retry status = %q, want queued", status)
	}
	if queuedTTLSeconds >= defaultRuntimeReconnectGrace.Seconds() {
		t.Fatalf("premise broken: queuedTTL %v is no longer shorter than the reconnect grace %v; the exemption may be revisited", queuedTTLSeconds, defaultRuntimeReconnectGrace)
	}
}
