package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestQuickCreateCompletion_SubscribesRequester(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"please file a bug",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	number, err := queries.IncrementIssueCounter(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("IncrementIssueCounter: %v", err)
	}
	тикет, err := queries.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       "agent-filed bug",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "agent",
		CreatorID:   parseUUID(agentID),
		Number:      number,
		OriginType:  pgtype.Text{String: "quick_create", Valid: true},
		OriginID:    task.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssueWithOrigin: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, тикет.ID)
	})

	if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", "", false); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	if !isSubscribed(t, queries, util.UUIDToString(тикет.ID), "member", testUserID) {
		t.Fatal("expected requester to be subscribed after quick-create completion")
	}
}

func TestQuickCreateFailure_DoesNotSubscribeRequester(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"another bug",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", "", false); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	var leaked int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM issue_subscriber s
		JOIN issue i ON i.id = s.issue_id
		WHERE s.user_type = 'member' AND s.user_id = $1
		  AND i.origin_type = 'quick_create' AND i.origin_id = $2
	`, testUserID, task.ID).Scan(&leaked); err != nil {
		t.Fatalf("count leaked subscribers: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("expected no subscriber rows for failed quick-create, got %d", leaked)
	}
}

func TestQuickCreateFailure_SurfacesAgentOutput(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file that same bug again",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	const agentErr = "Error: an active issue already exists: JKY-30 (blocked). Pass --allow-duplicate to override."
	result, _ := json.Marshal(map[string]any{"output": agentErr})
	if _, err := taskSvc.CompleteTask(ctx, task.ID, result, "", "", false); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	body, errDetail, _ := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_failed")

	if !strings.Contains(errDetail, "JKY-30") {
		t.Fatalf("expected failure detail to carry the agent's real error, got %q", errDetail)
	}
	if strings.Contains(errDetail, "agent finished without creating an issue") {
		t.Fatalf("failure detail regressed to the generic message: %q", errDetail)
	}
	if !strings.Contains(body, "JKY-30") {
		t.Fatalf("expected inbox body to carry the agent's real error, got %q", body)
	}
}

func TestQuickCreateLookupFault_WritesUnconfirmedInbox(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file a bug while the db is flaky",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	faulting := service.NewTaskService(
		db.New(failGetIssueByOriginDB{DBTX: testPool, err: errors.New("simulated transient db fault")}),
		testPool, nil, bus,
	)

	result, _ := json.Marshal(map[string]any{
		"output": "Error: an active issue already exists: JKY-30 (blocked).",
	})
	if _, err := faulting.CompleteTask(ctx, task.ID, result, "", "", false); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	body, errDetail, title := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_unconfirmed")

	if !strings.Contains(body, "Couldn't confirm") {
		t.Fatalf("expected the neutral unconfirmed wording, got body %q", body)
	}
	if title == "Quick create failed" {
		t.Fatalf("unconfirmed outcome must not be titled as a definite failure, got %q", title)
	}

	if strings.Contains(body, "JKY-30") || strings.Contains(errDetail, "JKY-30") {
		t.Fatalf("unconfirmed outcome must not reuse the agent output as the reason: body=%q detail=%q", body, errDetail)
	}
}

func TestQuickCreateFailure_RedactsAgentOutput(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file a bug and leak a token",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	const fakeToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"
	result, _ := json.Marshal(map[string]any{
		"output": "Error: create failed while authenticating with " + fakeToken,
	})
	if _, err := taskSvc.CompleteTask(ctx, task.ID, result, "", "", false); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	body, errDetail, _ := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_failed")

	if strings.Contains(body, fakeToken) || strings.Contains(errDetail, fakeToken) {
		t.Fatal("agent output reached the inbox row unredacted")
	}
	if !strings.Contains(body, "[REDACTED GITHUB TOKEN]") {
		t.Fatalf("expected the token to be replaced by a redaction placeholder, got %q", body)
	}
}

func TestQuickCreateLookupCancelled_StillWritesUnconfirmedInbox(t *testing.T) {
	setupCtx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(setupCtx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(setupCtx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file a bug while the request is cancelled",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(setupCtx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(setupCtx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	faulting := service.NewTaskService(
		db.New(cancelOnGetIssueByOriginDB{DBTX: testPool, cancel: cancel}),
		testPool, nil, bus,
	)

	result, _ := json.Marshal(map[string]any{"output": "Error: something went wrong"})
	if _, err := faulting.CompleteTask(ctx, task.ID, result, "", "", false); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("test setup is wrong: the context should have been cancelled during the lookup")
	}

	body, _, _ := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_unconfirmed")
	if !strings.Contains(body, "Couldn't confirm") {
		t.Fatalf("expected the neutral unconfirmed wording, got %q", body)
	}
}

func requireQuickCreateOutcomeInbox(t *testing.T, taskID pgtype.UUID, inboxType string) (string, string, string) {
	t.Helper()
	var body, errDetail, title string
	err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(body, ''), COALESCE(details->>'error', ''), title
		FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1
		  AND type = $3
		  AND details->>'task_id' = $2::text
		ORDER BY created_at DESC
		LIMIT 1
	`, testUserID, taskID, inboxType).Scan(&body, &errDetail, &title)
	if err != nil {
		t.Fatalf("expected a %s notification for the task, got none: %v", inboxType, err)
	}
	return body, errDetail, title
}

func deleteInboxForTask(taskID pgtype.UUID) {
	testPool.Exec(context.Background(),
		`DELETE FROM inbox_item WHERE details->>'task_id' = $1::text`, taskID)
}

type failGetIssueByOriginDB struct {
	db.DBTX
	err error
}

func (f failGetIssueByOriginDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {

	if strings.Contains(sql, "name: GetIssueByOrigin") {
		return errRow{err: f.err}
	}
	return f.DBTX.QueryRow(ctx, sql, args...)
}

type cancelOnGetIssueByOriginDB struct {
	db.DBTX
	cancel context.CancelFunc
}

func (f cancelOnGetIssueByOriginDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, "name: GetIssueByOrigin") {
		f.cancel()
		return errRow{err: context.Canceled}
	}
	return f.DBTX.QueryRow(ctx, sql, args...)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }
