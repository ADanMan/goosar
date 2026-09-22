package handler

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRedisModelListStore_EnvelopePersistsRunStartedAt(t *testing.T) {
	store := &RedisModelListStore{}
	now := time.Now().UTC().Truncate(time.Microsecond)
	req := &ModelListRequest{
		ID:           "id-1",
		RuntimeID:    "rt-1",
		Status:       ModelListRunning,
		Supported:    true,
		CreatedAt:    now.Add(-time.Second),
		UpdatedAt:    now,
		RunStartedAt: &now,
	}
	data, err := store.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := store.unmarshalRequest(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RunStartedAt == nil {
		t.Fatal("RunStartedAt lost on round trip — running timeout would never fire across nodes")
	}
	if !got.RunStartedAt.Equal(now) {
		t.Errorf("RunStartedAt drifted: got %s, want %s", got.RunStartedAt, now)
	}
	if got.Status != ModelListRunning {
		t.Errorf("Status lost: got %s", got.Status)
	}
	if got.ID != "id-1" || got.RuntimeID != "rt-1" {
		t.Errorf("identifiers lost: %+v", got)
	}
}

func TestRedisModelListStore_CreateGetComplete(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisModelListStore(rdb)

	req, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != ModelListPending {
		t.Fatalf("initial status = %s", req.Status)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != req.ID {
		t.Fatalf("round trip lost id: got=%v", got)
	}

	models := []ModelEntry{
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6", Default: true},
		{ID: "claude-opus-4-7", Label: "Claude Opus 4.7"},
	}
	if err := store.Complete(ctx, req.ID, models, true); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err = store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != ModelListCompleted {
		t.Fatalf("status after complete = %s", got.Status)
	}
	if len(got.Models) != 2 {
		t.Fatalf("models not persisted: %+v", got.Models)
	}
	if !got.Models[0].Default {
		t.Fatalf("default flag lost on round trip: %+v", got.Models[0])
	}
	if !got.Supported {
		t.Fatalf("supported flag lost on round trip")
	}
}

func TestRedisModelListStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisModelListStore(rdb)
	nodeB := NewRedisModelListStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B pop: %v", err)
	}
	if popped == nil {
		t.Fatal("node B did not see node A's pending request")
	}
	if popped.ID != req.ID {
		t.Fatalf("popped id = %s, want %s", popped.ID, req.ID)
	}
	if popped.Status != ModelListRunning {
		t.Fatalf("popped status = %s, want running", popped.Status)
	}
	if popped.RunStartedAt == nil {
		t.Fatal("run_started_at not set after pop")
	}

	again, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no more pending, got %+v", again)
	}
}

func TestRedisModelListStore_PopPendingConcurrent(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisModelListStore(rdb)

	req, err := store.Create(ctx, "runtime-race")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const N = 8
	var wg sync.WaitGroup
	results := make(chan *ModelListRequest, N)
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			popped, err := store.PopPending(ctx, "runtime-race")
			if err != nil {
				errs <- err
				return
			}
			results <- popped
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent pop error: %v", err)
	}

	winners := 0
	for popped := range results {
		if popped != nil {
			winners++
			if popped.ID != req.ID {
				t.Fatalf("winner popped wrong id: %s", popped.ID)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
}

func TestRedisModelListStore_PendingTimeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisModelListStore(rdb)

	req, err := store.Create(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	req.CreatedAt = time.Now().Add(-modelListPendingTimeout - time.Second)
	if err := store.persistRequest(ctx, req); err != nil {
		t.Fatalf("persist rewound: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ModelListTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}

	popped, err := store.PopPending(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("pop after timeout: %v", err)
	}
	if popped != nil {
		t.Fatalf("expected no pending after timeout, got %+v", popped)
	}
}

func TestRedisModelListStore_RunningTimeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisModelListStore(rdb)

	req, err := store.Create(ctx, "runtime-running-timeout")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	popped, err := store.PopPending(ctx, "runtime-running-timeout")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.Status != ModelListRunning {
		t.Fatalf("expected running, got %+v", popped)
	}

	aged := time.Now().Add(-modelListRunningTimeout - time.Second)
	popped.RunStartedAt = &aged
	if err := store.persistRequest(ctx, popped); err != nil {
		t.Fatalf("persist rewound: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ModelListTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}
}

func TestRedisModelListStore_HasPending(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisModelListStore(rdb)

	if has, err := store.HasPending(ctx, "rt-empty"); err != nil || has {
		t.Fatalf("empty store should not report pending: has=%v err=%v", has, err)
	}

	if _, err := store.Create(ctx, "rt-1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if has, err := store.HasPending(ctx, "rt-1"); err != nil || !has {
		t.Fatalf("expected pending=true after Create: has=%v err=%v", has, err)
	}
	if has, err := store.HasPending(ctx, "rt-other"); err != nil || has {
		t.Fatalf("expected pending=false for unrelated runtime: has=%v err=%v", has, err)
	}

	if _, err := store.PopPending(ctx, "rt-1"); err != nil {
		t.Fatalf("pop: %v", err)
	}
	if has, err := store.HasPending(ctx, "rt-1"); err != nil || has {
		t.Fatalf("expected pending=false after PopPending: has=%v err=%v", has, err)
	}
}
