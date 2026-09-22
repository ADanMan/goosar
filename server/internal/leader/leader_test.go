package leader

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("leader tests require Postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("leader tests require Postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testKey() int64 { return int64(rand.Int31()) + 1 }

func TestTryRunRunsWhenLockIsFree(t *testing.T) {
	pool := integrationPool(t)
	key := testKey()

	ran := false
	won, err := TryRun(context.Background(), pool, key, func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("TryRun: %v", err)
	}
	if !won || !ran {
		t.Fatalf("expected the only caller to win the lock: won=%v ran=%v", won, ran)
	}
}

func TestTryRunSkipsWhileAnotherHolderRuns(t *testing.T) {
	pool := integrationPool(t)
	key := testKey()

	entered := make(chan struct{})
	release := make(chan struct{})
	var contenderWon atomic.Bool
	var contenderRan atomic.Bool

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = TryRun(context.Background(), pool, key, func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()

	<-entered
	won, err := TryRun(context.Background(), pool, key, func(context.Context) error {
		contenderRan.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("contender TryRun: %v", err)
	}
	contenderWon.Store(won)
	close(release)
	wg.Wait()

	if contenderWon.Load() || contenderRan.Load() {
		t.Fatalf("a second replica must not run the job while the leader holds the lock: won=%v ran=%v",
			contenderWon.Load(), contenderRan.Load())
	}
}

func TestTryRunReleasesLockAfterFailure(t *testing.T) {
	pool := integrationPool(t)
	key := testKey()

	boom := errors.New("boom")
	won, err := TryRun(context.Background(), pool, key, func(context.Context) error { return boom })
	if !won {
		t.Fatalf("expected to win the free lock")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("TryRun must surface the job error, got %v", err)
	}

	won, err = TryRun(context.Background(), pool, key, func(context.Context) error { return nil })
	if err != nil || !won {
		t.Fatalf("lock was not released after a failing job: won=%v err=%v", won, err)
	}
}
