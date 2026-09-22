package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/scheduler"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func setupAutopilotScheduleJob(t *testing.T, cron string) (db.AutopilotTrigger, *scheduler.Manager, *service.AutopilotService) {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Schedule dispatch fixture",
		Description:        pgtype.Text{String: "schedule dispatch test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}

	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: cron, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}

	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET created_at = now() - INTERVAL '2 minute' WHERE id = $1`,
		trigger.ID,
	); err != nil {
		t.Fatalf("backdate trigger.created_at: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg,
			`DELETE FROM sys_cron_executions WHERE scope_kind = $1 AND scope_id = $2`,
			scheduler.ScopeKindAutopilotTrigger, util.UUIDToString(trigger.ID),
		)
		_, _ = testPool.Exec(bg, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	mgr := scheduler.NewManager(testPool, scheduler.Options{
		RunnerID: "autopilot-job-test",
	})
	if err := mgr.Register(scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)); err != nil {
		t.Fatalf("register autopilot_schedule_dispatch job: %v", err)
	}

	return trigger, mgr, autopilotSvc
}

func TestAutopilotScheduleJobDispatchesOnce(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	var execRows int
	var status string
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MAX(status), '')
		  FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows, &status); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows != 1 || status != "SUCCESS" {
		t.Fatalf("expected 1 SUCCESS exec row, got %d rows with status %q", execRows, status)
	}

	var runRows int
	var plannedAtValid bool
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*), bool_or(planned_at IS NOT NULL)
		  FROM autopilot_run
		 WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows, &plannedAtValid); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 1 || !plannedAtValid {
		t.Fatalf("expected 1 autopilot_run with planned_at set, got %d rows planned_at_valid=%v", runRows, plannedAtValid)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	var execRowsAfter int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRowsAfter); err != nil {
		t.Fatalf("count exec rows after 2nd tick: %v", err)
	}
	if execRowsAfter < execRows {
		t.Fatalf("second tick should never delete rows; before=%d after=%d", execRows, execRowsAfter)
	}
}

func TestAutopilotScheduleJobMissedSchedulesCollapse(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/5 * * * *")

	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET created_at = now() - INTERVAL '1 hour' WHERE id = $1`,
		trigger.ID,
	); err != nil {
		t.Fatalf("backdate trigger.created_at: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var rows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&rows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("CatchUpLatestOnly must collapse missed fires to 1 row per tick, got %d", rows)
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 1 {
		t.Fatalf("missed schedules must collapse to a single autopilot_run, got %d", runRows)
	}
}

func TestAutopilotScheduleJobCrashRecovery(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	var execID, leaseToken string
	var planTime time.Time
	var attempt int
	if err := testPool.QueryRow(ctx, `
		SELECT id, lease_token, plan_time, attempt
		  FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execID, &leaseToken, &planTime, &attempt); err != nil {
		t.Fatalf("read first exec row: %v", err)
	}
	if attempt != 1 {
		t.Fatalf("first attempt must be attempt=1, got %d", attempt)
	}

	var firstRunID pgtype.UUID
	var firstRunTaskValid bool
	if err := testPool.QueryRow(ctx, `
		SELECT id, task_id IS NOT NULL FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&firstRunID, &firstRunTaskValid); err != nil {
		t.Fatalf("read first run: %v", err)
	}
	if !firstRunTaskValid {
		t.Fatalf("first attempt must have created a real downstream task; task_id is NULL")
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status      = 'RUNNING',
		       runner_id   = 'ghost-runner',
		       lease_token = gen_random_uuid(),
		       stale_after = now() - INTERVAL '10 minutes',
		       finished_at = NULL,
		       duration_ms = NULL,
		       updated_at  = now()
		 WHERE id = $1
	`, execID); err != nil {
		t.Fatalf("simulate crash mid-dispatch: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick 2 (recovery): %v", err)
	}

	var recoveredStatus string
	var recoveredAttempt int
	var recoveredPlan time.Time
	if err := testPool.QueryRow(ctx, `
		SELECT status, attempt, plan_time
		  FROM sys_cron_executions
		 WHERE id = $1
	`, execID).Scan(&recoveredStatus, &recoveredAttempt, &recoveredPlan); err != nil {
		t.Fatalf("read recovered exec row: %v", err)
	}
	if !recoveredPlan.Equal(planTime) {
		t.Fatalf("retry must stay on the same plan_time: tick1=%s tick2=%s",
			planTime.Format(time.RFC3339), recoveredPlan.Format(time.RFC3339))
	}
	if recoveredAttempt != 2 {
		t.Fatalf("retry must increment attempt (the FAILED-retry branch fired); got attempt=%d", recoveredAttempt)
	}
	if recoveredStatus != "SUCCESS" {
		t.Fatalf("retry must reach terminal SUCCESS on the same row; got status=%q", recoveredStatus)
	}

	rows, err := testPool.Query(ctx, `SELECT id FROM autopilot_run WHERE trigger_id = $1`, trigger.ID)
	if err != nil {
		t.Fatalf("list run rows: %v", err)
	}
	defer rows.Close()
	var seenIDs []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seenIDs = append(seenIDs, id)
	}
	if len(seenIDs) != 1 {
		t.Fatalf("crash recovery must keep autopilot_run at exactly one row, got %d", len(seenIDs))
	}
	if seenIDs[0] != firstRunID {
		t.Fatalf("retry must reuse tick-1's run row; got new id %s",
			util.UUIDToString(seenIDs[0]))
	}
}

func TestAutopilotScheduleJobTwoRunnersSingleWinner(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	trigger, _, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	mgrA := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "runner-A"})
	mgrB := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "runner-B"})
	if err := mgrA.Register(scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if err := mgrB.Register(scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)); err != nil {
		t.Fatalf("register B: %v", err)
	}

	type result struct {
		err error
	}
	results := make(chan result, 2)
	go func() { results <- result{err: mgrA.RunOnce(ctx)} }()
	go func() { results <- result{err: mgrB.RunOnce(ctx)} }()

	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("runOnce: %v", r.err)
		}
	}

	var execRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows < 1 || execRows > 2 {

		t.Fatalf("expected 1 or 2 exec rows (one per plan_time), got %d", execRows)
	}

	rowsByPlan, err := testPool.Query(ctx, `
		SELECT plan_time, runner_id FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
		 ORDER BY plan_time
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID))
	if err != nil {
		t.Fatalf("query per-plan rows: %v", err)
	}
	defer rowsByPlan.Close()
	seen := map[string]string{}
	for rowsByPlan.Next() {
		var plan time.Time
		var runner string
		if err := rowsByPlan.Scan(&plan, &runner); err != nil {
			t.Fatalf("scan: %v", err)
		}
		key := plan.Format(time.RFC3339Nano)
		if prev, ok := seen[key]; ok && prev != runner {
			t.Fatalf("plan_time %s claimed by both %s and %s — uniqueness broke", key, prev, runner)
		}
		seen[key] = runner
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != execRows {
		t.Fatalf("expected 1 autopilot_run per exec row, got exec=%d run=%d", execRows, runRows)
	}
}

func TestAutopilotScheduleJobDisabledTriggerSkips(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET enabled = FALSE WHERE id = $1`, trigger.ID,
	); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var execRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows != 0 {
		t.Fatalf("disabled trigger must not produce sys_cron_executions rows, got %d", execRows)
	}
}

func TestAutopilotScheduleJobPausedAutopilotSkipsAtHandler(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	trigger, _, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if _, err := queries.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
		ID:     trigger.AutopilotID,
		Status: pgtype.Text{String: "paused", Valid: true},
	}); err != nil {
		t.Fatalf("pause autopilot: %v", err)
	}

	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)

	planTime := time.Now().UTC().Truncate(time.Minute)
	result, err := job.Handler(ctx, scheduler.HandlerInput{
		Job:       &job,
		Scope:     scheduler.Scope{Kind: scheduler.ScopeKindAutopilotTrigger, ID: util.UUIDToString(trigger.ID)},
		PlanTime:  planTime,
		Attempt:   1,
		RunnerID:  "handler-guard-test",
		Heartbeat: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.RowsAffected != 0 {
		t.Fatalf("paused autopilot must produce a no-op (rows_affected=0), got %d", result.RowsAffected)
	}
	if got, _ := result.Result["skipped_reason"].(string); got != "autopilot_inactive" {
		t.Fatalf("expected skipped_reason=autopilot_inactive, got %q", got)
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 0 {
		t.Fatalf("paused-autopilot handler guard must not create a run, got %d", runRows)
	}
}

func TestAutopilotScheduleJobBadCronStaysSilent(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET cron_expression = $2 WHERE id = $1`,
		trigger.ID, "garbage not a cron",
	); err != nil {
		t.Fatalf("set bad cron: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var execRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows != 0 {
		t.Fatalf("bad cron must not produce sys_cron_executions rows (no claim happens), got %d", execRows)
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 0 {
		t.Fatalf("bad cron must not fire dispatch, got %d run rows", runRows)
	}
}

func seedColdStartTrigger(t *testing.T, cron string) (db.AutopilotTrigger, *db.Queries, *service.AutopilotService) {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Cold-start regression",
		Description:        pgtype.Text{String: "deterministic cold-start", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})

	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: cron, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}
	return trigger, queries, autopilotSvc
}

func TestAutopilotScheduleJobColdStartHonorsLastFiredAt(t *testing.T) {
	ctx := context.Background()
	trigger, queries, autopilotSvc := seedColdStartTrigger(t, "0 17 * * 1-5")

	createdAt := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	lastFiredAt := time.Date(2026, 6, 22, 17, 0, 0, 0, time.UTC)
	pinnedNow := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_trigger
		   SET created_at    = $2,
		       last_fired_at = $3
		 WHERE id = $1
	`, trigger.ID, createdAt, lastFiredAt); err != nil {
		t.Fatalf("seed deterministic timestamps: %v", err)
	}

	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)
	if _, err := job.Scopes(ctx, pinnedNow); err != nil {
		t.Fatalf("populate scope cache: %v", err)
	}

	scope := scheduler.Scope{
		Kind: scheduler.ScopeKindAutopilotTrigger,
		ID:   util.UUIDToString(trigger.ID),
	}

	plans, err := job.PlansForScope(ctx, scope, pinnedNow, scheduler.LatestPlanInfo{Found: false})
	if err != nil {
		t.Fatalf("planner hook: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("cold-start cron=%q with last_fired_at=%s and now=%s must yield no plans (next fire is Tue 17:00 UTC, in the future); got %v",
			"0 17 * * 1-5",
			lastFiredAt.Format(time.RFC3339),
			pinnedNow.Format(time.RFC3339),
			plans,
		)
	}
}

func TestAutopilotScheduleJobColdStartBrandNewTriggerStillFires(t *testing.T) {
	ctx := context.Background()
	trigger, queries, autopilotSvc := seedColdStartTrigger(t, "0 12 * * *")

	createdAt := time.Date(2026, 6, 23, 11, 50, 0, 0, time.UTC)
	pinnedNow := time.Date(2026, 6, 23, 12, 5, 0, 0, time.UTC)
	expectedFire := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_trigger
		   SET created_at    = $2,
		       last_fired_at = NULL
		 WHERE id = $1
	`, trigger.ID, createdAt); err != nil {
		t.Fatalf("seed deterministic timestamps: %v", err)
	}

	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)
	if _, err := job.Scopes(ctx, pinnedNow); err != nil {
		t.Fatalf("populate scope cache: %v", err)
	}

	scope := scheduler.Scope{
		Kind: scheduler.ScopeKindAutopilotTrigger,
		ID:   util.UUIDToString(trigger.ID),
	}

	plans, err := job.PlansForScope(ctx, scope, pinnedNow, scheduler.LatestPlanInfo{Found: false})
	if err != nil {
		t.Fatalf("planner hook: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("brand-new trigger should fire its first due occurrence; got %d plans: %v", len(plans), plans)
	}
	if !plans[0].Equal(expectedFire) {
		t.Fatalf("plan_time mismatch: got %s want %s",
			plans[0].Format(time.RFC3339), expectedFire.Format(time.RFC3339))
	}
}

func TestAutopilotScheduleJobColdStartSkipsStaleMissedOccurrence(t *testing.T) {
	ctx := context.Background()
	trigger, queries, autopilotSvc := seedColdStartTrigger(t, "7 9 * * *")

	createdAt := time.Date(2026, 7, 2, 15, 54, 0, 0, time.UTC)
	pinnedNow := time.Date(2026, 7, 4, 13, 4, 27, 0, time.UTC)

	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_trigger
		   SET created_at    = $2,
		       last_fired_at = NULL
		 WHERE id = $1
	`, trigger.ID, createdAt); err != nil {
		t.Fatalf("seed deterministic timestamps: %v", err)
	}

	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)
	if _, err := job.Scopes(ctx, pinnedNow); err != nil {
		t.Fatalf("populate scope cache: %v", err)
	}

	scope := scheduler.Scope{
		Kind: scheduler.ScopeKindAutopilotTrigger,
		ID:   util.UUIDToString(trigger.ID),
	}

	plans, err := job.PlansForScope(ctx, scope, pinnedNow, scheduler.LatestPlanInfo{Found: false})
	if err != nil {
		t.Fatalf("planner hook: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("stale cold-start occurrence must be skipped; got plans %v", plans)
	}
}
