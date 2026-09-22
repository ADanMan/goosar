package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsSearchStatementTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"57014 pgx error", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}, true},
		{"57014 wrapped", errors.Join(errors.New("outer"), &pgconn.PgError{Code: "57014"}), true},
		{"different pg code", &pgconn.PgError{Code: "42P01"}, false},
		{"plain error", errors.New("boom"), false},
		{"context canceled", context.Canceled, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSearchStatementTimeout(tc.err); got != tc.want {
				t.Errorf("isSearchStatementTimeout(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRunSearchQuery_StatementTimeoutFires(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping live-Postgres search timeout test")
	}

	oldTimeout := searchStatementTimeout
	setSearchStatementTimeoutForTest(t, 200*time.Millisecond)
	t.Cleanup(func() { setSearchStatementTimeoutForTest(t, oldTimeout) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := runSearchQuery(ctx, testPool, "SELECT pg_sleep(2)", nil, func(rows pgx.Rows) error {
		for rows.Next() {

		}
		return rows.Err()
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected statement_timeout error, got nil")
	}
	if !isSearchStatementTimeout(err) {
		t.Fatalf("expected SQLSTATE 57014 (statement_timeout), got: %v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("statement_timeout did not cut hung query fast enough: elapsed=%s (want <1.5s)", elapsed)
	}
}

func setSearchStatementTimeoutForTest(t *testing.T, v time.Duration) {
	t.Helper()
	searchStatementTimeoutOverride = v
}
