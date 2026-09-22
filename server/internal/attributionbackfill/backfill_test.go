package attributionbackfill

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	strictCheck = `originator_user_id IS NULL
		OR (accountable_user_id IS NOT NULL AND accountable_user_id = originator_user_id)`
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable at %s: %v", dbURL, err)
	}
	return pool
}

func newFixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	schema := fmt.Sprintf("attr_backfill_test_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA %q CASCADE`, schema))
	})
	table := fmt.Sprintf("%q.agent_task_queue", schema)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			originator_user_id  UUID NULL,
			accountable_user_id UUID NULL,
			originator_source   TEXT NULL
		)`, table)); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	return table
}

func addStrictConstraint(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`
		ALTER TABLE %s
		ADD CONSTRAINT agent_task_queue_accountable_matches_originator_strict
		CHECK (%s) NOT VALID`, table, strictCheck)); err != nil {
		t.Fatalf("add strict constraint: %v", err)
	}
}

func TestHook_ReconcilesViolatingRowsSoValidatePasses(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)

	const (
		u1 = "11111111-1111-1111-1111-111111111111"
		u2 = "22222222-2222-2222-2222-222222222222"
		u3 = "33333333-3333-3333-3333-333333333333"
		u4 = "44444444-4444-4444-4444-444444444444"
		u5 = "55555555-5555-5555-5555-555555555555"
		u6 = "66666666-6666-6666-6666-666666666666"
	)

	rows := [][4]any{
		{"aaaaaaaa-0000-0000-0000-000000000001", u1, nil, nil},
		{"aaaaaaaa-0000-0000-0000-000000000002", u2, nil, nil},
		{"aaaaaaaa-0000-0000-0000-000000000003", u3, u3, "direct_human"},
		{"aaaaaaaa-0000-0000-0000-000000000004", u4, u5, "direct_human"},
		{"aaaaaaaa-0000-0000-0000-000000000005", nil, u6, "rule_owner"},
		{"aaaaaaaa-0000-0000-0000-000000000006", nil, nil, "unattributed"},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx,
			fmt.Sprintf(`INSERT INTO %s (id, originator_user_id, accountable_user_id, originator_source) VALUES ($1,$2,$3,$4)`, table),
			r[0], r[1], r[2], r[3]); err != nil {
			t.Fatalf("insert fixture row %v: %v", r[0], err)
		}
	}
	addStrictConstraint(t, pool, table)

	res, err := Hook(ctx, pool, HookOptions{Table: table})
	if err != nil {
		t.Fatalf("Hook: %v", err)
	}

	if res.RowsBackfilled != 3 {
		t.Errorf("RowsBackfilled = %d, want 3", res.RowsBackfilled)
	}
	if res.MismatchNormalized != 1 {
		t.Errorf("MismatchNormalized = %d, want 1", res.MismatchNormalized)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE after hook should pass, got: %v", err)
	}

	assertRow := func(id, wantAccountable, wantSource string) {
		t.Helper()
		var acc, src *string
		if err := pool.QueryRow(ctx, fmt.Sprintf(
			`SELECT accountable_user_id::text, originator_source FROM %s WHERE id=$1`, table), id).Scan(&acc, &src); err != nil {
			t.Fatalf("read row %s: %v", id, err)
		}
		gotAcc, gotSrc := "<nil>", "<nil>"
		if acc != nil {
			gotAcc = *acc
		}
		if src != nil {
			gotSrc = *src
		}
		if gotAcc != wantAccountable || gotSrc != wantSource {
			t.Errorf("row %s = (accountable %s, source %s), want (%s, %s)", id, gotAcc, gotSrc, wantAccountable, wantSource)
		}
	}
	assertRow("aaaaaaaa-0000-0000-0000-000000000001", u1, "backfill")
	assertRow("aaaaaaaa-0000-0000-0000-000000000002", u2, "backfill")
	assertRow("aaaaaaaa-0000-0000-0000-000000000003", u3, "direct_human")
	assertRow("aaaaaaaa-0000-0000-0000-000000000004", u4, "direct_human")
	assertRow("aaaaaaaa-0000-0000-0000-000000000005", u6, "rule_owner")
	assertRow("aaaaaaaa-0000-0000-0000-000000000006", "<nil>", "unattributed")

	res2, err := Hook(ctx, pool, HookOptions{Table: table})
	if err != nil {
		t.Fatalf("second Hook: %v", err)
	}
	if res2.RowsBackfilled != 0 || res2.Batches != 0 {
		t.Errorf("second run not a no-op: %+v", res2)
	}
}

func TestHook_EmptyTableIsNoOp(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)
	addStrictConstraint(t, pool, table)

	res, err := Hook(ctx, pool, HookOptions{Table: table})
	if err != nil {
		t.Fatalf("Hook on empty table: %v", err)
	}
	if res.RowsBackfilled != 0 || res.MismatchNormalized != 0 || res.Batches != 0 {
		t.Errorf("empty-table run should be a no-op, got %+v", res)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE on empty table should pass, got: %v", err)
	}
}

func TestHook_ConcurrentForkNotClobbered(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)

	const (
		u1 = "11111111-1111-1111-1111-111111111111"
		u2 = "22222222-2222-2222-2222-222222222222"
		u3 = "33333333-3333-3333-3333-333333333333"
	)
	rID := "aaaaaaaa-0000-0000-0000-0000000000f1"
	r2ID := "aaaaaaaa-0000-0000-0000-0000000000f2"
	for _, r := range [][2]any{{rID, u1}, {r2ID, u3}} {
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (id, originator_user_id, accountable_user_id, originator_source) VALUES ($1,$2,NULL,NULL)`, table),
			r[0], r[1]); err != nil {
			t.Fatalf("seed row %v: %v", r[0], err)
		}
	}
	addStrictConstraint(t, pool, table)

	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire writer conn: %v", err)
	}
	defer writer.Release()
	tx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatalf("begin writer tx: %v", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET originator_user_id = NULL, accountable_user_id = $2, originator_source = 'rule_owner' WHERE id = $1`, table),
		rID, u2); err != nil {
		t.Fatalf("writer flip: %v", err)
	}

	type hookResult struct {
		res Result
		err error
	}
	done := make(chan hookResult, 1)
	go func() {
		res, err := Hook(ctx, pool, HookOptions{Table: table})
		done <- hookResult{res, err}
	}()

	waitUntilBlocked(t, pool, table)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit writer: %v", err)
	}

	hr := <-done
	if hr.err != nil {
		t.Fatalf("hook: %v", hr.err)
	}

	if hr.res.RowsBackfilled != 1 {
		t.Errorf("RowsBackfilled = %d, want 1 (R2 only; R must be skipped)", hr.res.RowsBackfilled)
	}

	var origR, accR, srcR *string
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT originator_user_id::text, accountable_user_id::text, originator_source FROM %s WHERE id=$1`, table), rID).
		Scan(&origR, &accR, &srcR); err != nil {
		t.Fatalf("read R: %v", err)
	}
	if origR != nil || accR == nil || *accR != u2 || srcR == nil || *srcR != "rule_owner" {
		t.Errorf("R was clobbered: originator=%v accountable=%v source=%v; want (NULL, %s, rule_owner)", deref(origR), deref(accR), deref(srcR), u2)
	}

	var accR2, srcR2 *string
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT accountable_user_id::text, originator_source FROM %s WHERE id=$1`, table), r2ID).Scan(&accR2, &srcR2); err != nil {
		t.Fatalf("read R2: %v", err)
	}
	if accR2 == nil || *accR2 != u3 || srcR2 == nil || *srcR2 != "backfill" {
		t.Errorf("R2 not reconciled: accountable=%v source=%v; want (%s, backfill)", deref(accR2), deref(srcR2), u3)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE after concurrent run should pass, got: %v", err)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func waitUntilBlocked(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND state = 'active'
			  AND query ILIKE '%' || $1 || '%'`, table).Scan(&n); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("hook never blocked on the row lock; FOR UPDATE guard likely missing")
}

func TestHook_BatchingReconcilesAll(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)

	const n = 25
	for i := 0; i < n; i++ {
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (originator_user_id, accountable_user_id, originator_source) VALUES (gen_random_uuid(), NULL, NULL)`, table)); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	addStrictConstraint(t, pool, table)

	res, err := Hook(ctx, pool, HookOptions{Table: table, BatchSize: 10})
	if err != nil {
		t.Fatalf("Hook: %v", err)
	}
	if res.RowsBackfilled != n {
		t.Errorf("RowsBackfilled = %d, want %d", res.RowsBackfilled, n)
	}
	if res.Batches != 3 {
		t.Errorf("Batches = %d, want 3", res.Batches)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE after batched backfill should pass, got: %v", err)
	}
}
