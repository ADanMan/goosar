// Package store — тонкая обёртка над пулом pgx/v5: подключение и общие
// транзакционные хелперы, которые переиспользуют все домены. Никакой
// генерации кода (sqlc и т.п.) — SQL пишется руками в каждом домене, здесь
// только общая инфраструктура.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store держит пул соединений с Postgres.
type Store struct {
	Pool *pgxpool.Pool
}

// Open создаёт пул по DSN. Подключение ленивое (pgxpool не пингует БД сразу),
// поэтому Open может успешно вернуться даже если БД временно недоступна —
// готовность проверяется отдельно через Ping (см. /readyz).
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: разбор DSN: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: создание пула: %w", err)
	}
	return &Store{Pool: pool}, nil
}

// Close закрывает пул.
func (s *Store) Close() { s.Pool.Close() }

// Ping — быстрая проверка доступности БД для /readyz.
func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

// WithTx выполняет fn в транзакции, коммитя при успехе и откатывая при
// ошибке или панике.
func (s *Store) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	err = fn(tx)
	return err
}

// IsUniqueViolation — true, если err — нарушение уникального индекса/ограничения Postgres (23505).
func IsUniqueViolation(err error) bool {
	return pgErrCode(err) == "23505"
}

// IsForeignKeyViolation — true для 23503.
func IsForeignKeyViolation(err error) bool {
	return pgErrCode(err) == "23503"
}

func pgErrCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}
