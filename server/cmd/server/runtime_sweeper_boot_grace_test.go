package main

import (
	"context"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/service"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestWithinSweeperBootGrace(t *testing.T) {
	boot := time.Now()

	if !withinSweeperBootGrace(boot, 150*time.Second, boot.Add(30*time.Second)) {
		t.Error("first tick after boot must still be inside the grace period")
	}
	if !withinSweeperBootGrace(boot, 150*time.Second, boot.Add(149*time.Second)) {
		t.Error("grace must hold right up to the boundary")
	}
	if withinSweeperBootGrace(boot, 150*time.Second, boot.Add(151*time.Second)) {
		t.Error("grace must expire once the window has passed")
	}

	if withinSweeperBootGrace(boot, 0, boot) {
		t.Error("zero grace must not suppress any sweep")
	}
}

func TestSweepTickBootGraceProtectsLiveTaskAfterRestart(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, taskID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, issueID, agentID) })

	ageOutAgentRuntime(t, agentID, defaultRuntimeReconnectGrace+time.Hour)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET status = 'online'
			WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
		`, agentID)
	})

	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	liveness := &fakeLiveness{available: false}

	sweepTick(ctx, queries, liveness, taskSvc, bus, sweepTickOptions{
		ReconnectGrace: defaultRuntimeReconnectGrace,
		BootedAt:       time.Now().Add(-30 * time.Second),
		Grace:          defaultSweeperBootGrace,
	})

	if status := taskStatus(t, taskID); status != "running" {
		t.Fatalf("a live task must survive a restart longer than the stale window, got status %q", status)
	}
	if status := runtimeStatusForAgent(t, agentID); status != "online" {
		t.Fatalf("a runtime that has not had time to reconnect must not be flipped offline, got %q", status)
	}

	sweepTick(ctx, queries, liveness, taskSvc, bus, sweepTickOptions{
		ReconnectGrace: defaultRuntimeReconnectGrace,
		BootedAt:       time.Now().Add(-10 * time.Minute),
		Grace:          defaultSweeperBootGrace,
	})

	if status := taskStatus(t, taskID); status != "failed" {
		t.Fatalf("after the grace expires a dead runtime's task must be reclaimed, got status %q", status)
	}
}

func TestSweeperBootGraceFromEnv(t *testing.T) {
	t.Setenv("GOOSAR_SWEEPER_BOOT_GRACE", "")
	if got := sweeperBootGraceFromEnv(); got != defaultSweeperBootGrace {
		t.Errorf("default = %s, want %s", got, defaultSweeperBootGrace)
	}

	t.Setenv("GOOSAR_SWEEPER_BOOT_GRACE", "7m")
	if got := sweeperBootGraceFromEnv(); got != 7*time.Minute {
		t.Errorf("override = %s, want 7m", got)
	}

	t.Setenv("GOOSAR_SWEEPER_BOOT_GRACE", "0")
	if got := sweeperBootGraceFromEnv(); got != 0 {
		t.Errorf("explicit 0 must disable the grace, got %s", got)
	}

	t.Setenv("GOOSAR_SWEEPER_BOOT_GRACE", "banana")
	if got := sweeperBootGraceFromEnv(); got != defaultSweeperBootGrace {
		t.Errorf("invalid value must fall back to the default, got %s", got)
	}
}

func taskStatus(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	return status
}

func runtimeStatusForAgent(t *testing.T, agentID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `
		SELECT r.status FROM agent_runtime r
		JOIN agent a ON a.runtime_id = r.id
		WHERE a.id = $1
	`, agentID).Scan(&status); err != nil {
		t.Fatalf("query runtime status: %v", err)
	}
	return status
}
