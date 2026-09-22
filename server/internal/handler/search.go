package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const searchStatementTimeout = 3 * time.Second

var searchStatementTimeoutOverride time.Duration

func effectiveSearchStatementTimeout() time.Duration {
	if searchStatementTimeoutOverride > 0 {
		return searchStatementTimeoutOverride
	}
	return searchStatementTimeout
}

func runSearchQuery(
	ctx context.Context,
	txStarter txStarter,
	sql string,
	args []any,
	rowsFn func(pgx.Rows) error,
) error {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin search tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {

			_ = tx.Rollback(context.Background())
		}
	}()

	timeoutMs := int(effectiveSearchStatementTimeout() / time.Millisecond)
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return fmt.Errorf("set search statement_timeout: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
		return fmt.Errorf("set search transaction_read_only: %w", err)
	}

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}

	if err := rowsFn(rows); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func isSearchStatementTimeout(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "57014"
	}
	return false
}
