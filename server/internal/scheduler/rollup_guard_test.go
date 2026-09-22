package scheduler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const rollupSingletonTestLock int64 = 42463980

func lockRollupSingleton(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire rollup-guard connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, rollupSingletonTestLock); err != nil {
		conn.Release()
		t.Fatalf("acquire rollup singleton guard: %v", err)
	}
	t.Cleanup(func() {

		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, rollupSingletonTestLock); err != nil {
			t.Logf("release rollup singleton guard: %v", err)
		}
		conn.Release()
	})
}
