package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/realtime"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func setupRerunTestFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		  AND a.archived_at IS NULL
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var issueID string

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		SELECT $1, 'Rerun test issue', 'todo', 'none', 'member', m.user_id, 'agent', $2,
		       (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	return issueID, agentID, runtimeID
}

func cleanupRerunFixture(t *testing.T, issueID string) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
}

func TestGetLastTaskSessionExcludesPoisonedFailures(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'HEALTHY-SESSION', '/tmp/healthy', 'timeout')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert healthy failed task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISONED-SESSION', '/tmp/poisoned', 'iteration_limit')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid {
		t.Fatal("expected to fall back to the healthy failed session, got no session")
	}
	if prior.SessionID.String == "POISONED-SESSION" {
		t.Fatal("rerun would inherit poisoned session — filter is not active")
	}
	if prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("expected HEALTHY-SESSION, got %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionFallsBackWhenLatestSessionBlanked(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'OLD-GOOD-SESSION', '/tmp/good')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert older completed task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', NULL, '/tmp/blanked', 'timeout')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert newer blanked task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid {
		t.Fatal("expected fallback to the older recorded session, got no session")
	}
	if prior.SessionID.String != "OLD-GOOD-SESSION" {
		t.Fatalf("expected OLD-GOOD-SESSION, got %q", prior.SessionID.String)
	}
}

func TestCompletedTaskRolloutMissingWithholdsAndDisclosesGap(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	queries := db.New(testPool)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'OLD-GOOD-SESSION', '/tmp/good')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert older completed task: %v", err)
	}

	var newerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '1 minute', 'NEW-ROLLOUT-MISSING', '/tmp/newer')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&newerTaskID); err != nil {
		t.Fatalf("insert newer running task: %v", err)
	}

	done, err := queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
		ID:                    pgtype.UUID{Bytes: parseUUIDBytes(newerTaskID), Valid: true},
		Result:                []byte(`{"output":"done"}`),
		SessionID:             pgtype.Text{String: "NEW-ROLLOUT-MISSING", Valid: true},
		WorkDir:               pgtype.Text{String: "/tmp/newer", Valid: true},
		SessionRolloutMissing: true,
	})
	if err != nil {
		t.Fatalf("CompleteAgentTask: %v", err)
	}

	if done.SessionID.Valid {
		t.Fatalf("expected withheld session_id to be NULL, got %q", done.SessionID.String)
	}
	if !done.SessionRolloutMissing {
		t.Fatal("expected session_rollout_missing to be set on the terminal row")
	}

	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "OLD-GOOD-SESSION" {
		t.Fatalf("expected fallback to OLD-GOOD-SESSION, got valid=%v %q", prior.SessionID.Valid, prior.SessionID.String)
	}

	missing, err := queries.GetLatestTaskRolloutMissing(ctx, db.GetLatestTaskRolloutMissingParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLatestTaskRolloutMissing failed: %v", err)
	}
	if !missing {
		t.Fatal("expected the continuity gap to be flagged so the next claim discloses it")
	}
}

func TestFailedTaskRolloutMissingForcesNullOverMidFlightPin(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	queries := db.New(testPool)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '1 minute', 'PINNED-BUT-NO-ROLLOUT', '/tmp/wd')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}

	failed, err := queries.FailAgentTask(ctx, db.FailAgentTaskParams{
		ID:                    pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
		Error:                 pgtype.Text{String: "runtime went offline", Valid: true},
		FailureReason:         pgtype.Text{String: "timeout", Valid: true},
		SessionID:             pgtype.Text{Valid: false},
		WorkDir:               pgtype.Text{Valid: false},
		SessionRolloutMissing: true,
	})
	if err != nil {
		t.Fatalf("FailAgentTask: %v", err)
	}
	if failed.SessionID.Valid {
		t.Fatalf("expected the mid-flight pin to be forced NULL, got %q", failed.SessionID.String)
	}
	if !failed.SessionRolloutMissing {
		t.Fatal("expected session_rollout_missing to be set on the failed row")
	}
}

func TestGetLastTaskSessionFallbackPoisonedClassifier(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 seconds', now() - interval '5 seconds', 'POISONED-FALLBACK', '/tmp/poisoned', 'agent_fallback_message')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected no resumable session, got %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionExcludesAPIInvalidRequest(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 seconds', now() - interval '5 seconds', 'POISONED-API400', '/tmp/poisoned', 'api_invalid_request')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected no resumable session for api_invalid_request, got %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionExcludesCodexSemanticInactivity(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'HEALTHY-SESSION', '/tmp/healthy', 'timeout')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert healthy failed task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'CODEX-STUCK-SESSION', '/tmp/codex-stuck', 'codex_semantic_inactivity',
		        'codex semantic inactivity timeout after 10m0s without agent progress (last activity: tool-result:exec_command)')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert codex semantic inactivity task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("expected HEALTHY-SESSION, got %q", prior.SessionID.String)
	}
}

func TestCreateRetryTaskFreshensCodexSemanticInactivity(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			started_at, completed_at, session_id, work_dir, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute',
		        'CODEX-STUCK-SESSION', '/tmp/codex-stuck', 'codex_semantic_inactivity', 1, 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert codex semantic inactivity parent task: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true}})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}
	if child.SessionID.Valid {
		t.Fatalf("expected retry child to drop poisoned session_id, got %q", child.SessionID.String)
	}
	if child.WorkDir.Valid {
		t.Fatalf("expected retry child to drop poisoned work_dir, got %q", child.WorkDir.String)
	}
	if !child.ForceFreshSession {
		t.Fatal("expected retry child to force a fresh session")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}
}

func TestCreateRetryTaskKeepsOrdinaryTimeoutSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			started_at, completed_at, session_id, work_dir, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute',
		        'ORDINARY-TIMEOUT-SESSION', '/tmp/ordinary-timeout', 'timeout', 1, 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert ordinary timeout parent task: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true}})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}
	if !child.SessionID.Valid || child.SessionID.String != "ORDINARY-TIMEOUT-SESSION" {
		t.Fatalf("expected retry child to inherit session_id, got %+v", child.SessionID)
	}
	if !child.WorkDir.Valid || child.WorkDir.String != "/tmp/ordinary-timeout" {
		t.Fatalf("expected retry child to inherit work_dir, got %+v", child.WorkDir)
	}
	if child.ForceFreshSession {
		t.Fatal("expected ordinary timeout retry child to keep resume enabled")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}
}

func TestGetLastTaskSessionExcludesLegacyAPI400(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'LEGACY-POISONED', '/tmp/legacy', 'agent_error',
		        'API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert legacy poisoned task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'NEW-POISONED', '/tmp/new', 'api_invalid_request',
		        'API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert new poisoned task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected no resumable session, but query fell back to %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionKeepsBenignAgentErrorWithSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '30 seconds', now() - interval '30 seconds', 'HEALTHY-RESUMABLE', '/tmp/healthy', 'agent_error',
		        'tool execution failed: connection refused')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert benign failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "HEALTHY-RESUMABLE" {
		t.Fatalf("expected to resume HEALTHY-RESUMABLE, got %q (valid=%v)", prior.SessionID.String, prior.SessionID.Valid)
	}
}

func TestRerunIssueSetsForceFreshSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, _, _ := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(ctx, pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, nil)
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}
	if !task.ForceFreshSession {
		t.Fatal("expected manual rerun to set force_fresh_session=true")
	}
}

func TestRerunIssueTargetsSourceTaskAgent(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, primaryAgentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	var secondaryAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		SELECT a.workspace_id, 'Rerun Secondary Agent', '', 'cloud', '{}'::jsonb,
		       a.runtime_id, 'workspace', 1, a.owner_id
		FROM agent a WHERE a.id = $1
		RETURNING id
	`, primaryAgentID).Scan(&secondaryAgentID); err != nil {
		t.Fatalf("create secondary agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, secondaryAgentID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, secondaryAgentID)
	})

	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              started_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error')
		RETURNING id
	`, secondaryAgentID, runtimeID, issueID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceTaskID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}

	gotAgent := util.UUIDToString(task.AgentID)
	if gotAgent != secondaryAgentID {
		t.Fatalf("rerun targeted wrong agent: got %s, want %s (issue assignee is %s — must not be picked)",
			gotAgent, secondaryAgentID, primaryAgentID)
	}
	if !task.ForceFreshSession {
		t.Fatal("expected per-row rerun to also set force_fresh_session=true")
	}
}

func TestRerunIssueRejectsCrossIssueTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueAID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueAID) })

	ctx := context.Background()

	var issueBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		SELECT $1, 'Rerun cross-issue test', 'todo', 'none', 'member', m.user_id, 'agent', $2,
		       (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueBID); err != nil {
		t.Fatalf("create second issue: %v", err)
	}
	t.Cleanup(func() { cleanupRerunFixture(t, issueBID) })

	var crossTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              started_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error')
		RETURNING id
	`, agentID, runtimeID, issueBID).Scan(&crossTaskID); err != nil {
		t.Fatalf("insert cross task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	_, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueAID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(crossTaskID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err == nil {
		t.Fatal("expected RerunIssue to reject a source task from a different issue")
	}
}

func TestRerunIssueInheritsTriggerCommentFromSourceTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		SELECT $1, $2, 'member', m.user_id, 'please retry this', 'comment'
		FROM member m WHERE m.workspace_id = $2 LIMIT 1
		RETURNING id
	`, issueID, testWorkspaceID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("insert trigger comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, triggerCommentID)
	})

	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              started_at, completed_at, failure_reason,
		                              trigger_comment_id)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error',
		        $4)
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceTaskID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}
	if !task.TriggerCommentID.Valid {
		t.Fatal("expected per-row rerun to inherit trigger_comment_id from source task, got NULL")
	}
	if got := util.UUIDToString(task.TriggerCommentID); got != triggerCommentID {
		t.Fatalf("trigger_comment_id mismatch: got %s, want %s", got, triggerCommentID)
	}
}

func TestEnqueueTaskForIssueDoesNotForceFreshSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, _, _ := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	тикет, err := queries.GetIssue(ctx, pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	task, err := taskService.EnqueueTaskForIssue(ctx, тикет)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue failed: %v", err)
	}
	if task.ForceFreshSession {
		t.Fatal("expected normal enqueue to leave force_fresh_session=false")
	}
}

func TestGetLastTaskSessionExcludesPoisonedOriginSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'POISON-ORIGIN', '/tmp/origin')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert completed origin task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISON-ORIGIN', '/tmp/origin', 'api_invalid_request',
		        'kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned resume task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected the poisoned session to be fully invalidated, but query returned %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionFallsBackToHealthyDistinctSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '3 minutes', now() - interval '3 minutes', 'POISON-ORIGIN', '/tmp/origin')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert completed origin task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'POISON-ORIGIN', '/tmp/origin', 'api_invalid_request',
		        'session/prompt: Internal error (code=-32603, data=messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned resume task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', 'HEALTHY-DISTINCT', '/tmp/healthy')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert healthy distinct task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "HEALTHY-DISTINCT" {
		t.Fatalf("expected fallback to HEALTHY-DISTINCT, got %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionExcludesKiroOversizedImageByText(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'KIRO-OVERSIZED', '/tmp/kiro', 'agent_error',
		        'kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert unclassified kiro oversized task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected the oversized-image session to be filtered by text, got %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionRestoresRecoveredSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'RECOVER-SESS', '/tmp/recover', 'api_invalid_request',
		        'session/prompt: Internal error (code=-32603, data=messages.0.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert earlier poisoned task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', 'RECOVER-SESS', '/tmp/recover')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert later completed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "RECOVER-SESS" {
		t.Fatalf("expected the recovered session to be resumable again, got %q", prior.SessionID.String)
	}
}

func TestGetLastTaskSessionKeepsDimensionPhraseWithoutImageMarker(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '30 seconds', now() - interval '30 seconds', 'DIM-ONLY-RESUMABLE', '/tmp/dim', 'agent_error',
		        'tool reported: image dimensions exceed max allowed size: 8000 pixels while generating a thumbnail')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert dimension-phrase-only task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "DIM-ONLY-RESUMABLE" {
		t.Fatalf("expected the dimension-phrase-only session to stay resumable, got %q (valid=%v)", prior.SessionID.String, prior.SessionID.Valid)
	}
}
