package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type preMigrationHook func(ctx context.Context, pool *pgxpool.Pool) error

var preMigrationHooks = map[string]preMigrationHook{}

const migrationAdvisoryLockKey int64 = 7244554146635925501

const defaultSchemaMigrationsTable = "schema_migrations"

type runOptions struct {
	Direction string

	Files []string

	SchemaMigrationsTable string

	AdvisoryLockKey int64

	Lock lockPolicy

	Hooks map[string]preMigrationHook
}

const (
	defaultMigrationLockTimeout = 5 * time.Second

	defaultMigrationLockRetries = 5

	defaultMigrationLockBackoff = 2 * time.Second

	lockNotAvailableCode = "55P03"
)

type lockPolicy struct {
	LockTimeout time.Duration

	StatementTimeout time.Duration

	Retries int

	Backoff time.Duration
}

func (p lockPolicy) withDefaults() lockPolicy {
	if p.LockTimeout <= 0 {
		p.LockTimeout = defaultMigrationLockTimeout
	}
	if p.Retries < 0 {
		p.Retries = defaultMigrationLockRetries
	}
	if p.Backoff <= 0 {
		p.Backoff = defaultMigrationLockBackoff
	}
	return p
}

func migrationLockPolicyFromEnv() lockPolicy {
	return lockPolicy{
		LockTimeout:      envDuration("GOOSAR_MIGRATION_LOCK_TIMEOUT", defaultMigrationLockTimeout),
		StatementTimeout: envDuration("GOOSAR_MIGRATION_STATEMENT_TIMEOUT", 0),
		Retries:          envCount("GOOSAR_MIGRATION_LOCK_RETRIES", defaultMigrationLockRetries),
		Backoff:          defaultMigrationLockBackoff,
	}
}

func envDuration(name string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid migration env var, using default", "name", name, "value", raw, "default", def.String())
		return def
	}
	return v
}

func envCount(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid migration env var, using default", "name", name, "value", raw, "default", def)
		return def
	}
	return v
}

var (
	concurrentIndexRe = regexp.MustCompile(`(?i)\b(create|drop)\s+(unique\s+)?index\s+concurrently\b`)
	sqlLineCommentRe  = regexp.MustCompile(`--[^\n]*`)
)

func buildsIndexConcurrently(sql string) bool {
	return concurrentIndexRe.MatchString(sqlLineCommentRe.ReplaceAllString(sql, " "))
}

func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == lockNotAvailableCode
}

func describeLockBlockers(ctx context.Context, pool *pgxpool.Pool) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, `
		SELECT pid,
		       coalesce(state, '')                                     AS state,
		       coalesce(left(query, 120), '')                          AS query,
		       coalesce(round(extract(epoch FROM (now() - xact_start))), 0) AS xact_age
		FROM pg_stat_activity
		WHERE datname = current_database()
		  AND pid <> pg_backend_pid()
		  AND state IS DISTINCT FROM 'idle'
		ORDER BY xact_start NULLS LAST
		LIMIT 5
	`)
	if err != nil {
		return fmt.Sprintf("unavailable (%v)", err)
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var pid int32
		var state, query string
		var xactAge float64
		if err := rows.Scan(&pid, &state, &query, &xactAge); err != nil {
			return fmt.Sprintf("unavailable (%v)", err)
		}
		parts = append(parts, fmt.Sprintf("pid=%d state=%q xact_age=%.0fs query=%q",
			pid, state, xactAge, strings.Join(strings.Fields(query), " ")))
	}
	if len(parts) == 0 {
		return "none visible in pg_stat_activity (blocker may have finished)"
	}
	return strings.Join(parts, "; ")
}

func setTimeouts(ctx context.Context, conn *pgxpool.Conn, lockTimeout, statementTimeout time.Duration) error {
	for _, s := range []struct {
		name  string
		value time.Duration
	}{{"lock_timeout", lockTimeout}, {"statement_timeout", statementTimeout}} {
		ms := s.value.Milliseconds()
		if s.value > 0 && ms == 0 {
			ms = 1
		}
		if _, err := conn.Exec(ctx, fmt.Sprintf("SET %s = %d", s.name, ms)); err != nil {
			return fmt.Errorf("set %s: %w", s.name, err)
		}
	}
	return nil
}

func execMigrationWithLockRetry(ctx context.Context, conn *pgxpool.Conn, pool *pgxpool.Pool, version, sql string, p lockPolicy) error {
	if buildsIndexConcurrently(sql) {
		slog.Info("migration builds an index concurrently, running it without timeouts",
			"version", version)
		if err := setTimeouts(ctx, conn, 0, 0); err != nil {
			return err
		}
		defer func() {
			if err := setTimeouts(ctx, conn, p.LockTimeout, p.StatementTimeout); err != nil {
				slog.Warn("failed to restore migration timeouts", "version", version, "error", err)
			}
		}()
		_, err := conn.Exec(ctx, sql)
		return err
	}

	backoff := p.Backoff
	for attempt := 1; ; attempt++ {
		_, err := conn.Exec(ctx, sql)
		if err == nil {
			return nil
		}
		if !isLockTimeout(err) {
			return err
		}

		blockers := describeLockBlockers(ctx, pool)
		if attempt > p.Retries {
			return fmt.Errorf("blocked on a lock for %s across %d attempt(s); blocking sessions: %s: %w",
				p.LockTimeout, attempt, blockers, err)
		}

		slog.Warn("migration blocked on a lock, retrying",
			"version", version,
			"attempt", attempt,
			"max_attempts", p.Retries+1,
			"lock_timeout", p.LockTimeout.String(),
			"retry_in", backoff.String(),
			"blocking_sessions", blockers)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

func main() {
	logger.Init()

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run ./cmd/migrate <up|down>")
		os.Exit(1)
	}

	direction := os.Args[1]
	if direction != "up" && direction != "down" {
		fmt.Println("Usage: go run ./cmd/migrate <up|down>")
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}

	files, err := migrations.Files(direction)
	if err != nil {
		slog.Error("failed to find migration files", "error", err)
		os.Exit(1)
	}

	if err := runMigrations(ctx, pool, runOptions{
		Direction: direction,
		Files:     files,
		Lock:      migrationLockPolicyFromEnv(),
		Hooks:     preMigrationHooks,
	}); err != nil {
		slog.Error("migration run failed", "error", err)
		os.Exit(1)
	}

	fmt.Println("Done.")
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, opts runOptions) error {
	switch opts.Direction {
	case "up", "down":

	default:
		return fmt.Errorf("invalid direction %q (want \"up\" or \"down\")", opts.Direction)
	}

	table := opts.SchemaMigrationsTable
	if table == "" {
		table = defaultSchemaMigrationsTable
	}
	tableIdent, err := quoteQualifiedIdentifier(table)
	if err != nil {
		return fmt.Errorf("invalid schema migrations table %q: %w", table, err)
	}
	lockKey := opts.AdvisoryLockKey
	if lockKey == 0 {
		lockKey = migrationAdvisoryLockKey
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}

	defer func() {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			slog.Warn("failed to release migration advisory lock", "error", err)
		}
	}()

	lock := opts.Lock.withDefaults()
	if err := setTimeouts(ctx, conn, lock.LockTimeout, lock.StatementTimeout); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`, tableIdent)); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	existsSQL := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE version = $1)", tableIdent)
	insertSQL := fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", tableIdent)
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = $1", tableIdent)

	for _, file := range opts.Files {
		version := migrations.ExtractVersion(file)

		var exists bool
		if err := conn.QueryRow(ctx, existsSQL, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %q: %w", version, err)
		}

		if opts.Direction == "up" {
			if exists {
				fmt.Printf("  skip  %s (already applied)\n", version)
				continue
			}
		} else {
			if !exists {
				fmt.Printf("  skip  %s (not applied)\n", version)
				continue
			}
		}

		sql, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", file, err)
		}

		if opts.Direction == "up" {
			if hook, ok := opts.Hooks[version]; ok && hook != nil {
				slog.Info("running pre-migration hook", "version", version)
				if err := hook(ctx, pool); err != nil {
					return fmt.Errorf("pre-migration hook for %q: %w", version, err)
				}
			}
		}

		if err := execMigrationWithLockRetry(ctx, conn, pool, version, string(sql), lock); err != nil {
			return fmt.Errorf("apply migration %q: %w", file, err)
		}

		if opts.Direction == "up" {
			_, err = conn.Exec(ctx, insertSQL, version)
		} else {
			_, err = conn.Exec(ctx, deleteSQL, version)
		}
		if err != nil {
			return fmt.Errorf("record migration %q: %w", version, err)
		}

		fmt.Printf("  %s  %s\n", opts.Direction, version)
	}

	return nil
}

func quoteQualifiedIdentifier(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("identifier %q has more than one dot; only schema.table is supported", name)
	}
	for _, p := range parts {
		if p == "" {
			return "", fmt.Errorf("empty component in %q", name)
		}
	}
	return pgx.Identifier(parts).Sanitize(), nil
}
