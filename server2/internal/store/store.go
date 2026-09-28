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

const (
	pgCodeUniqueViolation     = "23505"
	pgCodeForeignKeyViolation = "23503"
)

// Store — точка входа доменов к Postgres: держит один общий пул на весь процесс.
type Store struct {
	Pool *pgxpool.Pool
}

// Open разбирает dsn и заводит пул. pgxpool ленив — соединение не
// открывается прямо здесь, поэтому Open может вернуть *Store даже когда БД
// временно недоступна; фактическую готовность проверяет отдельно Ping (его
// вызывает /readyz).
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, parseErr := pgxpool.ParseConfig(dsn)
	if parseErr != nil {
		return nil, fmt.Errorf("store: %s: %w", "разбор DSN", parseErr)
	}
	pool, poolErr := pgxpool.NewWithConfig(ctx, cfg)
	if poolErr != nil {
		return nil, fmt.Errorf("store: %s: %w", "создание пула", poolErr)
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Ping — быстрая проверка доступности БД, используется /readyz.
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }

// WithTx оборачивает fn единой транзакцией: паника прокатывается дальше
// после отката, обычная ошибка тоже откатывает, а чистое завершение — коммитит.
func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, beginErr := s.Pool.Begin(ctx)
	if beginErr != nil {
		return fmt.Errorf("store: начало транзакции: %w", beginErr)
	}

	result := runInTx(ctx, tx, fn)
	if result != nil {
		_ = tx.Rollback(ctx)
		return result
	}
	return tx.Commit(ctx)
}

func runInTx(ctx context.Context, tx pgx.Tx, fn func(pgx.Tx) error) (outcome error) {
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	outcome = fn(tx)
	return outcome
}

// IsUniqueViolation — err нарушает уникальный индекс/ограничение (Postgres 23505).
func IsUniqueViolation(err error) bool { return pgErrCode(err) == pgCodeUniqueViolation }

// IsForeignKeyViolation — err нарушает внешний ключ (Postgres 23503).
func IsForeignKeyViolation(err error) bool { return pgErrCode(err) == pgCodeForeignKeyViolation }

func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ""
	}
	return pgErr.Code
}

// IsNoRows — err — это в точности "запрошенной строки нет" (pgx.ErrNoRows),
// а не какая-то другая ошибка БД. Домены оборачивают это в свой собственный
// ErrNotFound на границе Store — этот хелпер экономит один errors.Is на
// каждый такой переход.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// RowExists выполняет query (обычно "SELECT EXISTS(SELECT 1 FROM ... WHERE ...)")
// и возвращает булев результат — общий паттерн проверки "уже занято"/"уже
// существует" перед вставкой, которым иначе каждый домен обзаводился бы
// заново почти дословно.
func (s *Store) RowExists(ctx context.Context, query string, args ...any) (bool, error) {
	var exists bool
	if err := s.Pool.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: проверка существования строки: %w", err)
	}
	return exists, nil
}

// PoolStats — снимок загрузки пула соединений, достаточный для диагностики
// без похода в pgxpool.Stat напрямую из каждого места, которому это нужно
// (например будущего расширения /health/realtime метриками БД, не только
// WebSocket-хаба).
type PoolStats struct {
	AcquiredConns   int32
	IdleConns       int32
	MaxConns        int32
	TotalConns      int32
	NewConnsCount   int64
	CanceledAcquire int64
}

// Stats снимает текущее состояние пула.
func (s *Store) Stats() PoolStats {
	st := s.Pool.Stat()
	return PoolStats{
		AcquiredConns:   st.AcquiredConns(),
		IdleConns:       st.IdleConns(),
		MaxConns:        st.MaxConns(),
		TotalConns:      st.TotalConns(),
		NewConnsCount:   st.NewConnsCount(),
		CanceledAcquire: st.CanceledAcquireCount(),
	}
}

// Saturated — true, если пул уже выдал все разрешённые соединения и не
// осталось простаивающих; полезно как дешёвая (без запроса к БД) грубая
// оценка перегрузки перед тем, как решать, стоит ли открывать ещё одно
// дорогое соединение (например при каждом новом /ws handshake).
func (s PoolStats) Saturated() bool {
	return s.MaxConns > 0 && s.AcquiredConns >= s.MaxConns && s.IdleConns == 0
}
