package metrics

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBusinessSamplerStatementTimeoutCutsHungQuery(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres statement_timeout test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database not reachable at %s: %v", dbURL, err)
	}

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{
		Pool:         pool,
		CacheTTL:     time.Second,
		QueryTimeout: 500 * time.Millisecond,
	})
	if c == nil {
		t.Fatal("NewBusinessSamplerCollector returned nil for live pool")
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for hung-query test: %v", err)
	}
	defer conn.Release()

	const queryName = "pg_sleep_canary"
	var capturedErr error
	start := time.Now()
	c.runQuery(ctx, conn, queryName, func(ctx context.Context, tx pgx.Tx) error {

		_, err := tx.Exec(ctx, "SELECT pg_sleep(2)")
		capturedErr = err
		return err
	})
	elapsed := time.Since(start)

	if elapsed >= 1500*time.Millisecond {
		t.Fatalf("statement_timeout did not cut the hung query: elapsed %s", elapsed)
	}
	if elapsed <= 250*time.Millisecond {
		t.Fatalf("query returned suspiciously fast (%s); SET LOCAL statement_timeout may not be in force", elapsed)
	}

	if capturedErr == nil {
		t.Fatal("expected pg_sleep to return an error; got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(capturedErr, &pgErr) {
		t.Fatalf("expected *pgconn.PgError from pg_sleep cancellation; got %T: %v", capturedErr, capturedErr)
	}
	if pgErr.Code != "57014" {
		t.Fatalf("expected SQLSTATE 57014 (query_canceled); got %q (%s)", pgErr.Code, pgErr.Message)
	}

	if got := testutil.ToFloat64(c.queryErrors.WithLabelValues(queryName)); got < 1 {
		t.Fatalf("query_errors_total{name=%q} = %v, want >= 1", queryName, got)
	}

	if got := testutil.CollectAndCount(c.queryDuration); got < 1 {
		t.Fatalf("query_seconds histogram saw 0 observations after pg_sleep cancellation")
	}
}
