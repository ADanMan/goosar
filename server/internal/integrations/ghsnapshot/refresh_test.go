package ghsnapshot

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"
)

func enabledClient(t *testing.T) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{appID: "1", privateKey: key, tokens: map[int64]cachedToken{}, now: time.Now}
}

func TestManagerDisabledNoOps(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)
	if m.Enabled() {
		t.Fatal("nil-client manager must be disabled")
	}
	m.Enqueue(1, "o", "r", 2)
	m.MaybeEnqueueOnView(1, "o", "r", 2, time.Time{}, false)
	if len(m.queue) != 0 {
		t.Fatalf("disabled manager enqueued %d items, want 0", len(m.queue))
	}

	m.Start(context.Background())
}

func TestEnqueueCoalesces(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)

	m.Enqueue(1, "o", "r", 7)
	m.Enqueue(1, "o", "r", 7)
	m.Enqueue(1, "o", "r", 7)
	if len(m.queue) != 1 {
		t.Fatalf("same address enqueued 3× produced %d queued items, want 1", len(m.queue))
	}
	m.Enqueue(1, "o", "r", 8)
	m.Enqueue(1, "o", "other", 7)
	if len(m.queue) != 3 {
		t.Fatalf("queue length = %d, want 3 distinct", len(m.queue))
	}
}

func TestMaybeEnqueueOnViewRespectsTTL(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(10000, 0)
	m.now = func() time.Time { return now }

	m.MaybeEnqueueOnView(1, "o", "r", 1, now.Add(-10*time.Second), true)
	if len(m.queue) != 0 {
		t.Fatal("fresh snapshot should not refresh on view")
	}
	m.MaybeEnqueueOnView(1, "o", "r", 2, now.Add(-5*time.Minute), true)
	m.MaybeEnqueueOnView(1, "o", "r", 3, time.Time{}, false)
	if len(m.queue) != 2 {
		t.Fatalf("stale/missing snapshots enqueued %d, want 2", len(m.queue))
	}
}

func TestProcessRateLimitedSetsPause(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(20000, 0)
	m.now = func() time.Time { return now }
	m.jitter = func() time.Duration { return 0 }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		return nil, &RateLimitError{RetryAfter: 90 * time.Second}
	}

	m.process(ctx, address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1})

	if got := m.rateUntil[1]; !got.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("rateUntil = %v, want %v", got, now.Add(90*time.Second))
	}
}

func TestRateLimitedInstallationDoesNotOccupyWorkers(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.concurrency = 12
	m.sweepInterval = time.Hour
	m.jitter = func() time.Duration { return 0 }
	m.extendRateLimit(1, 2*time.Second)

	limitedFetched := make(chan struct{}, 1)
	otherFetched := make(chan struct{}, 1)
	m.fetch = func(_ context.Context, _ *Client, installationID int64, _ string, _ string, _ int32) (*PRSnapshot, error) {
		switch installationID {
		case 1:
			limitedFetched <- struct{}{}
		case 2:
			otherFetched <- struct{}{}
		}
		return nil, errors.New("stop after fetch")
	}

	for number := int32(1); number <= int32(m.concurrency); number++ {
		m.Enqueue(1, "o", "r", number)
	}
	m.Enqueue(2, "o", "r", 1)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx)

	select {
	case <-otherFetched:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("rate-limited installation occupied the global worker pool")
	}
	select {
	case <-limitedFetched:
		t.Fatal("paused installation fetched before Retry-After")
	default:
	}
}

func TestRateLimitDeadlineNeverShortens(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(21000, 0)
	m.now = func() time.Time { return now }

	m.extendRateLimit(1, 90*time.Second)
	m.extendRateLimit(1, 30*time.Second)
	if got := m.rateUntil[1]; !got.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("shorter Retry-After replaced deadline: %v", got)
	}

	m.extendRateLimit(1, 2*time.Minute)
	if got := m.rateUntil[1]; !got.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("later Retry-After was not retained: %v", got)
	}
}

func TestRateLimitIsolatedByInstallation(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(22000, 0)
	m.now = func() time.Time { return now }
	m.jitter = func() time.Duration { return 0 }
	m.extendRateLimit(1, time.Hour)

	called := false
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		called = true
		return nil, errors.New("stop after fetch")
	}
	m.process(context.Background(), address{InstallationID: 2, Owner: "o", Repo: "r", Number: 1})
	if !called {
		t.Fatal("installation 1 rate limit blocked installation 2")
	}
	if pause := m.rateLimitPause(2); pause > 0 {
		t.Fatalf("installation 2 unexpectedly paused for %v", pause)
	}
}

func TestPersistentRateLimitReturnsToTTLSweep(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.jitter = func() time.Duration { return 0 }
	m.ctx = context.Background()
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		return nil, &RateLimitError{RetryAfter: time.Millisecond}
	}

	m.process(context.Background(), address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1})

	time.Sleep(10 * time.Millisecond)
	if len(m.queue) != 0 {
		t.Fatalf("rate-limited fetch scheduled %d direct retries, want 0", len(m.queue))
	}
}

func TestScheduleChaseBounded(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	addr := address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1}
	for i := 0; i < maxChaseAttempts; i++ {
		m.scheduleChase(addr)
		if m.attempts[addr] != i+1 {
			t.Fatalf("attempt %d recorded as %d", i+1, m.attempts[addr])
		}
	}

	m.scheduleChase(addr)
	if _, ok := m.attempts[addr]; ok {
		t.Fatal("chase past the cap must stop and clear attempts")
	}
}
