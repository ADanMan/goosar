package service

import (
	"context"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type stubWakeup struct {
	calls []struct{ runtimeID, taskID string }
}

func (s *stubWakeup) NotifyTaskAvailable(runtimeID, taskID string) {
	s.calls = append(s.calls, struct{ runtimeID, taskID string }{runtimeID, taskID})
}

func TestNotifyTaskAvailable_BumpsBeforeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	runtimeID := testUUID(7)
	taskID := testUUID(8)
	runtimeKey := util.UUIDToString(runtimeID)

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, v0)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty after MarkEmpty under current version")
	}

	svc.notifyTaskAvailable(db.AgentTaskQueue{
		ID:        taskID,
		RuntimeID: runtimeID,
	})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("notifyTaskAvailable must Bump the version so the prior empty verdict is rejected")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 wakeup call, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].taskID != util.UUIDToString(taskID) {
		t.Fatalf("wakeup task mismatch: got %q want %q", wakeup.calls[0].taskID, util.UUIDToString(taskID))
	}
}

func TestNotifyTaskAvailable_InvalidWithoutRuntimeIsNoOp(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, "rt-stays")
	cache.MarkEmpty(ctx, "rt-stays", v0)

	svc.notifyTaskAvailable(db.AgentTaskQueue{

		ID: testUUID(9),
	})

	if !cache.IsEmpty(ctx, "rt-stays") {
		t.Fatal("notifyTaskAvailable with invalid RuntimeID must not touch cache")
	}
	if got := len(wakeup.calls); got != 0 {
		t.Fatalf("expected 0 wakeup calls when RuntimeID is invalid, got %d", got)
	}
}

func TestNotifyTaskFinished_BumpsBeforeRuntimeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}
	svc := &TaskService{EmptyClaim: cache, Wakeup: wakeup}

	runtimeID := testUUID(10)
	runtimeKey := util.UUIDToString(runtimeID)
	ctx := context.Background()
	version := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, version)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty")
	}

	svc.NotifyTaskFinished(db.AgentTaskQueue{ID: testUUID(11), RuntimeID: runtimeID})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("terminal wakeup must invalidate the prior empty verdict")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 terminal wakeup, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].taskID != "" {
		t.Fatalf("terminal wakeup must omit completed task id, got %q", wakeup.calls[0].taskID)
	}
}

func TestNotifyTasksFinished_CoalescesByRuntime(t *testing.T) {
	wakeup := &stubWakeup{}
	svc := &TaskService{Wakeup: wakeup}
	runtimeA := testUUID(12)
	runtimeB := testUUID(13)

	svc.notifyTasksFinished([]db.AgentTaskQueue{
		{ID: testUUID(14), RuntimeID: runtimeA},
		{ID: testUUID(15), RuntimeID: runtimeA},
		{ID: testUUID(16), RuntimeID: runtimeB},
		{ID: testUUID(17)},
	})

	if got := len(wakeup.calls); got != 2 {
		t.Fatalf("expected one wakeup per runtime, got %d", got)
	}
	if wakeup.calls[0].runtimeID != util.UUIDToString(runtimeA) || wakeup.calls[1].runtimeID != util.UUIDToString(runtimeB) {
		t.Fatalf("unexpected runtime wakeups: %#v", wakeup.calls)
	}
	for _, call := range wakeup.calls {
		if call.taskID != "" {
			t.Fatalf("terminal wakeup must omit completed task id, got %q", call.taskID)
		}
	}
}
