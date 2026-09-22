package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func blockTable(t *testing.T, pool *pgxpool.Pool, qualified string) (release func()) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker conn: %v", err)
	}
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		conn.Release()
		t.Fatalf("blocker BEGIN: %v", err)
	}
	if _, err := conn.Exec(ctx, "LOCK TABLE "+qualified+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		conn.Release()
		t.Fatalf("blocker LOCK: %v", err)
	}

	var once bool
	release = func() {
		if once {
			return
		}
		once = true
		if _, err := conn.Exec(context.Background(), "ROLLBACK"); err != nil {
			t.Logf("blocker ROLLBACK: %v", err)
		}
		conn.Release()
	}
	t.Cleanup(release)
	return release
}

func lockTestFixture(t *testing.T) (f *fixture, qualified string, opts runOptions) {
	t.Helper()
	f = newFixture(t)

	ctx := context.Background()
	table := "lock_target"
	qualified = pgx.Identifier{f.schema, table}.Sanitize()
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (id BIGSERIAL PRIMARY KEY)`, qualified)); err != nil {
		t.Fatalf("create lock target: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "001_lock_target.up.sql")
	body := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS payload TEXT;\n", qualified)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	opts = f.opts()
	opts.Files = []string{path}
	return f, qualified, opts
}

func TestRunMigrations_LockTimeoutFailsFastAndNamesBlocker(t *testing.T) {
	f, qualified, opts := lockTestFixture(t)
	blockTable(t, f.pool, qualified)

	opts.Lock = lockPolicy{
		LockTimeout: 200 * time.Millisecond,
		Retries:     2,
		Backoff:     50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	start := time.Now()
	err := runMigrations(ctx, f.pool, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the migration to fail while a blocker holds ACCESS EXCLUSIVE, got nil")
	}

	if elapsed > 10*time.Second {
		t.Fatalf("migration waited %s on a held lock; lock_timeout is not being applied", elapsed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "001_lock_target") {
		t.Errorf("error must name the migration that was blocked, got: %v", err)
	}
	if !strings.Contains(msg, "lock") {
		t.Errorf("error must explain that it was a lock wait, got: %v", err)
	}

	if !strings.Contains(msg, "blocking") {
		t.Errorf("error must describe the blocking session(s), got: %v", err)
	}

	if applied := f.appliedVersions(t); len(applied) != 0 {
		t.Errorf("a migration that never applied must not be recorded, got %v", applied)
	}
}

func TestRunMigrations_LockTimeoutRetrySucceedsWhenBlockerReleases(t *testing.T) {
	f, qualified, opts := lockTestFixture(t)
	release := blockTable(t, f.pool, qualified)

	opts.Lock = lockPolicy{
		LockTimeout: 200 * time.Millisecond,
		Retries:     10,
		Backoff:     100 * time.Millisecond,
	}

	go func() {
		time.Sleep(700 * time.Millisecond)
		release()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()
	if err := runMigrations(ctx, f.pool, opts); err != nil {
		t.Fatalf("migration must succeed once the blocker releases, got: %v", err)
	}

	applied := f.appliedVersions(t)
	if len(applied) != 1 || applied[0] != "001_lock_target" {
		t.Fatalf("expected 001_lock_target recorded once, got %v", applied)
	}
}

func TestRunMigrations_AppliesLockTimeoutToSession(t *testing.T) {
	f, _, opts := lockTestFixture(t)
	opts.Lock = lockPolicy{LockTimeout: 1234 * time.Millisecond, Retries: 0, Backoff: time.Millisecond}

	dir := t.TempDir()
	path := filepath.Join(dir, "001_probe.up.sql")
	body := fmt.Sprintf(
		"CREATE TABLE %s (lock_timeout TEXT NOT NULL, statement_timeout TEXT NOT NULL);\n"+
			"INSERT INTO %s VALUES (current_setting('lock_timeout'), current_setting('statement_timeout'));\n",
		pgx.Identifier{f.schema, "probe"}.Sanitize(), pgx.Identifier{f.schema, "probe"}.Sanitize())
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	opts.Files = []string{path}

	if err := runMigrations(context.Background(), f.pool, opts); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var lockTimeout, statementTimeout string
	if err := f.pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT lock_timeout, statement_timeout FROM %s`, pgx.Identifier{f.schema, "probe"}.Sanitize()),
	).Scan(&lockTimeout, &statementTimeout); err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if lockTimeout != "1234ms" {
		t.Errorf("lock_timeout = %q, want 1234ms", lockTimeout)
	}

	if statementTimeout != "0" {
		t.Errorf("statement_timeout = %q, want 0 (unlimited) by default", statementTimeout)
	}
}

func TestMigrationLockPolicyFromEnv(t *testing.T) {
	t.Setenv("GOOSAR_MIGRATION_LOCK_TIMEOUT", "")
	t.Setenv("GOOSAR_MIGRATION_LOCK_RETRIES", "")
	t.Setenv("GOOSAR_MIGRATION_STATEMENT_TIMEOUT", "")

	got := migrationLockPolicyFromEnv()
	if got.LockTimeout != defaultMigrationLockTimeout {
		t.Errorf("default LockTimeout = %s, want %s", got.LockTimeout, defaultMigrationLockTimeout)
	}
	if got.Retries != defaultMigrationLockRetries {
		t.Errorf("default Retries = %d, want %d", got.Retries, defaultMigrationLockRetries)
	}
	if got.StatementTimeout != 0 {
		t.Errorf("default StatementTimeout = %s, want 0", got.StatementTimeout)
	}

	t.Setenv("GOOSAR_MIGRATION_LOCK_TIMEOUT", "3s")
	t.Setenv("GOOSAR_MIGRATION_LOCK_RETRIES", "9")
	t.Setenv("GOOSAR_MIGRATION_STATEMENT_TIMEOUT", "30m")
	got = migrationLockPolicyFromEnv()
	if got.LockTimeout != 3*time.Second || got.Retries != 9 || got.StatementTimeout != 30*time.Minute {
		t.Errorf("env override not honoured: %+v", got)
	}

	t.Setenv("GOOSAR_MIGRATION_LOCK_TIMEOUT", "banana")
	t.Setenv("GOOSAR_MIGRATION_LOCK_RETRIES", "-1")
	got = migrationLockPolicyFromEnv()
	if got.LockTimeout != defaultMigrationLockTimeout || got.Retries != defaultMigrationLockRetries {
		t.Errorf("invalid env must fall back to defaults, got %+v", got)
	}
}

func TestBuildsIndexConcurrently(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want bool
	}{
		{"create concurrently", "CREATE INDEX CONCURRENTLY i ON t (a);", true},
		{"create unique concurrently", "CREATE UNIQUE INDEX CONCURRENTLY i ON t (a);", true},
		{"create concurrently if not exists", "CREATE INDEX CONCURRENTLY IF NOT EXISTS i ON t (a);", true},
		{"drop concurrently", "DROP INDEX CONCURRENTLY IF EXISTS i;", true},
		{"plain create index", "CREATE INDEX i ON t (a);", false},
		{"prose only", "-- Keep this separate from CREATE INDEX CONCURRENTLY files.\nCREATE EXTENSION pg_trgm;", false},
		{"alter table", "ALTER TABLE t ADD COLUMN a int;", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildsIndexConcurrently(tc.sql); got != tc.want {
				t.Errorf("buildsIndexConcurrently = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunMigrations_ConcurrentIndexIgnoresLockTimeout(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	qualified := pgx.Identifier{f.schema, "cic_target"}.Sanitize()
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (id BIGSERIAL PRIMARY KEY, payload TEXT)`, qualified)); err != nil {
		t.Fatalf("create target: %v", err)
	}

	blocker, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker: %v", err)
	}

	if _, err := blocker.Exec(ctx, "BEGIN ISOLATION LEVEL REPEATABLE READ"); err != nil {
		blocker.Release()
		t.Fatalf("blocker BEGIN: %v", err)
	}
	if _, err := blocker.Exec(ctx, "SELECT 1"); err != nil {
		blocker.Release()
		t.Fatalf("blocker statement: %v", err)
	}

	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(time.Second)
		blocker.Exec(context.Background(), "ROLLBACK")
		blocker.Release()
	}()
	t.Cleanup(func() { <-released })

	dir := t.TempDir()
	path := filepath.Join(dir, "001_cic.up.sql")
	body := fmt.Sprintf("-- Single statement: CREATE INDEX CONCURRENTLY cannot run in a transaction.\nCREATE INDEX CONCURRENTLY cic_target_payload_idx ON %s (payload);\n", qualified)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	opts := f.opts()
	opts.Files = []string{path}
	opts.Lock = lockPolicy{LockTimeout: 200 * time.Millisecond, Retries: 3, Backoff: 50 * time.Millisecond}

	runCtx, cancel := context.WithTimeout(ctx, raceTestTimeout)
	defer cancel()
	if err := runMigrations(runCtx, f.pool, opts); err != nil {
		t.Fatalf("a concurrent index build must not be cancelled by lock_timeout: %v", err)
	}

	var valid bool
	if err := f.pool.QueryRow(ctx, `
		SELECT indisvalid FROM pg_index
		WHERE indexrelid = ($1 || '.cic_target_payload_idx')::regclass
	`, f.schema).Scan(&valid); err != nil {
		t.Fatalf("read index validity: %v", err)
	}
	if !valid {
		t.Error("index was left INVALID — the build was cancelled mid-flight")
	}
}

func TestRunMigrations_StatementTimeoutIsSetExplicitly(t *testing.T) {
	f, _, opts := lockTestFixture(t)
	ctx := context.Background()

	var role string
	if err := f.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatalf("current_user: %v", err)
	}
	roleIdent := pgx.Identifier{role}.Sanitize()
	if _, err := f.pool.Exec(ctx, fmt.Sprintf("ALTER ROLE %s SET statement_timeout = '900ms'", roleIdent)); err != nil {
		t.Skipf("cannot set a role-level statement_timeout in this environment: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), fmt.Sprintf("ALTER ROLE %s RESET statement_timeout", roleIdent))
	})

	dir := t.TempDir()
	probe := pgx.Identifier{f.schema, "probe_stmt"}.Sanitize()
	path := filepath.Join(dir, "001_probe.up.sql")
	body := fmt.Sprintf(
		"CREATE TABLE %s (statement_timeout TEXT NOT NULL);\n"+
			"INSERT INTO %s SELECT current_setting('statement_timeout') FROM pg_sleep(1.2);\n", probe, probe)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	opts.Files = []string{path}
	opts.Lock = lockPolicy{LockTimeout: time.Second, Retries: 0, Backoff: time.Millisecond}

	pool := openTestPool(t)
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("a migration slower than the role's statement_timeout must still run: %v", err)
	}

	var got string
	if err := f.pool.QueryRow(ctx, fmt.Sprintf("SELECT statement_timeout FROM %s", probe)).Scan(&got); err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if got != "0" {
		t.Errorf("statement_timeout = %q, want 0 (explicitly unlimited)", got)
	}
}
