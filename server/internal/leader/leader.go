// Пакет leader сериализует периодическую работу между репликами backend через
// сессионную advisory-блокировку Postgres: тик выполняет ровно одна реплика,
// остальные его пропускают.
package leader

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	KeyHygieneSweep int64 = 4397
)

func TryRun(ctx context.Context, pool *pgxpool.Pool, key int64, fn func(context.Context) error) (bool, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("leader: acquire connection: %w", err)
	}
	defer conn.Release()

	var won bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&won); err != nil {
		return false, fmt.Errorf("leader: try advisory lock %d: %w", key, err)
	}
	if !won {
		return false, nil
	}

	defer func() {
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key); err != nil {

			conn.Conn().Close(context.Background())
		}
	}()

	return true, fn(ctx)
}
