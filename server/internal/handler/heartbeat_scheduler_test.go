package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestBatchedHeartbeatScheduler_CoalescesAndFlushes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)

	const callers = 50
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			if err := sched.Schedule(context.Background(), rt); err != nil {
				t.Errorf("Schedule: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected coalesced pending=1, got %d", got)
	}

	_, lastSeenBefore, _ := readRuntimeRow(t, runtimeID)
	if !lastSeenBefore.Equal(stale) {

		if lastSeenBefore.After(stale.Add(time.Second)) {
			t.Fatalf("DB unexpectedly bumped before flush: %s", lastSeenBefore)
		}
	}

	sched.FlushNow(context.Background())

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after flush, got %d", got)
	}

	_, lastSeenAfter, _ := readRuntimeRow(t, runtimeID)
	if !lastSeenAfter.After(stale.Add(time.Hour)) {
		t.Fatalf("flush did not bump last_seen_at: stale=%s after=%s", stale, lastSeenAfter)
	}
}

func TestBatchedHeartbeatScheduler_OfflineFallsBackSync(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeStatus(t, runtimeID, "offline")
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "offline" {
		t.Fatalf("setup: status=%q want offline", rt.Status)
	}

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("offline row should not have been queued, pending=%d", got)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected status=online after sync flip, got %q", status)
	}
}

func TestBatchedHeartbeatScheduler_StopDrains(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Run(ctx)

	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected pending=1 before Stop, got %d", got)
	}

	sched.Stop()

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after Stop drain, got %d", got)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Hour)) {
		t.Fatalf("Stop did not drain pending bump: stale=%s after=%s", stale, lastSeen)
	}
}

func TestBatchedHeartbeatScheduler_StopFlushesLateSchedule(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour)

	runCtx, runCancel := context.WithCancel(context.Background())
	go sched.Run(runCtx)

	runCancel()
	time.Sleep(50 * time.Millisecond)

	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected pending=1 before Stop, got %d", got)
	}

	sched.Stop()

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after Stop's defensive flush, got %d", got)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Hour)) {
		t.Fatalf("Stop did not flush late Schedule: stale=%s after=%s", stale, lastSeen)
	}
}

func TestBatchedHeartbeatScheduler_FlushIgnoresEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)

	sched.FlushNow(context.Background())
	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("pending should remain 0, got %d", got)
	}
}

func TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	setRuntimeStatus(t, runtimeID, "offline")

	sched.FlushNow(context.Background())

	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "offline" {
		t.Fatalf("expected status=offline after raced flush, got %q", status)
	}

	rt2 := loadRuntime(t, runtimeID)
	if err := sched.Schedule(context.Background(), rt2); err != nil {
		t.Fatalf("recovery Schedule: %v", err)
	}
	status2, _, _ := readRuntimeRow(t, runtimeID)
	if status2 != "online" {
		t.Fatalf("expected sync recovery to flip back to online, got %q", status2)
	}
}

func TestPassthroughHeartbeatScheduler_TouchAndRaceRecovery(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewPassthroughHeartbeatScheduler(testHandler.Queries)

	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Minute)) {
		t.Fatalf("passthrough did not bump last_seen_at: stale=%s after=%s", stale, lastSeen)
	}

	rt2 := loadRuntime(t, runtimeID)
	setRuntimeStatus(t, runtimeID, "offline")
	if err := sched.Schedule(context.Background(), rt2); err != nil {
		t.Fatalf("Schedule under race: %v", err)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected race recovery via MarkAgentRuntimeOnline, got %q", status)
	}
}

var _ = pgtype.UUID{}
