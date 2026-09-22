package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

const (
	concurrentRunners = 16

	raceTestTimeout = 60 * time.Second
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable at %s: %v", dbURL, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type fixture struct {
	pool       *pgxpool.Pool
	schema     string
	tableFQN   string
	lockKey    int64
	files      []string
	versions   []string
	tableNames []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := openTestPool(t)

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_test_" + suffix
	tableFQN := schema + ".schema_migrations"

	lockKey := int64(rand.Uint64()&0x7fffffffffffffff) | 1

	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, pgx.Identifier{schema}.Sanitize())); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, pgx.Identifier{schema}.Sanitize())); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	dir := t.TempDir()
	const numFiles = 5
	files := make([]string, 0, numFiles)
	versions := make([]string, 0, numFiles)
	tableNames := make([]string, 0, numFiles)
	for i := 0; i < numFiles; i++ {
		version := fmt.Sprintf("%03d_test_%s", i+1, suffix)
		tableName := fmt.Sprintf("t_%s_%d", suffix, i+1)

		body := fmt.Sprintf(
			"CREATE TABLE %s.%s (id BIGSERIAL PRIMARY KEY);\n"+
				"ALTER TABLE %s.%s ADD COLUMN payload TEXT NOT NULL DEFAULT '';\n",
			pgx.Identifier{schema}.Sanitize(), pgx.Identifier{tableName}.Sanitize(),
			pgx.Identifier{schema}.Sanitize(), pgx.Identifier{tableName}.Sanitize(),
		)
		path := filepath.Join(dir, version+".up.sql")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write migration: %v", err)
		}
		files = append(files, path)
		versions = append(versions, version)
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(files)

	return &fixture{
		pool:       pool,
		schema:     schema,
		tableFQN:   tableFQN,
		lockKey:    lockKey,
		files:      files,
		versions:   versions,
		tableNames: tableNames,
	}
}

func (f *fixture) opts() runOptions {
	return runOptions{
		Direction:             "up",
		Files:                 f.files,
		SchemaMigrationsTable: f.tableFQN,
		AdvisoryLockKey:       f.lockKey,
	}
}

func (f *fixture) appliedVersions(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := f.pool.Query(ctx,
		fmt.Sprintf(`SELECT version FROM %s ORDER BY version`,
			pgx.Identifier{f.schema, "schema_migrations"}.Sanitize()))
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return got
}

func (f *fixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := f.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`, f.schema, name).Scan(&exists); err != nil {
		t.Fatalf("check table %s.%s: %v", f.schema, name, err)
	}
	return exists
}

func TestRunMigrationsConcurrentPending(t *testing.T) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			return runMigrations(gctx, f.pool, f.opts())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent runMigrations(up) on pending schema returned error: %v", err)
	}

	got := f.appliedVersions(t)
	if want := f.versions; !equalStrings(got, want) {
		t.Fatalf("schema_migrations contents = %v, want %v", got, want)
	}
	for _, tbl := range f.tableNames {
		if !f.tableExists(t, tbl) {
			t.Fatalf("expected table %s.%s to exist after concurrent up, missing", f.schema, tbl)
		}
	}
}

func TestRunMigrationsConcurrentAlreadyApplied(t *testing.T) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	if err := runMigrations(ctx, f.pool, f.opts()); err != nil {
		t.Fatalf("baseline runMigrations: %v", err)
	}
	baseline := f.appliedVersions(t)
	if !equalStrings(baseline, f.versions) {
		t.Fatalf("baseline schema_migrations = %v, want %v", baseline, f.versions)
	}

	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			return runMigrations(gctx, f.pool, f.opts())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent runMigrations(up) on already-applied schema returned error: %v", err)
	}

	got := f.appliedVersions(t)
	if !equalStrings(got, baseline) {
		t.Fatalf("schema_migrations changed after concurrent re-run: got %v, want %v", got, baseline)
	}
}

func TestRunMigrationsAdvisoryLockSerializes(t *testing.T) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	holder, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("side connect: %v", err)
	}
	defer holder.Close(context.Background())
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock($1)", f.lockKey); err != nil {
		t.Fatalf("side acquire lock: %v", err)
	}

	var done int64
	startedAt := time.Now()
	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			err := runMigrations(gctx, f.pool, f.opts())
			atomic.AddInt64(&done, 1)
			return err
		})
	}

	const observeWindow = 1 * time.Second
	const observeStep = 50 * time.Millisecond
	deadline := time.Now().Add(observeWindow)
	for time.Now().Before(deadline) {
		if n := atomic.LoadInt64(&done); n != 0 {
			t.Fatalf("advisory lock did not block: %d/%d goroutines finished while side connection held the lock for %s",
				n, concurrentRunners, time.Since(startedAt))
		}
		time.Sleep(observeStep)
	}

	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock($1)", f.lockKey); err != nil {
		t.Fatalf("side release lock: %v", err)
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent runMigrations after lock release returned error: %v", err)
	}

	if got, want := f.appliedVersions(t), f.versions; !equalStrings(got, want) {
		t.Fatalf("schema_migrations after lock-release race = %v, want %v", got, want)
	}
}

func TestRunMigrationsConcurrentMixedPoolStress(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("parse DATABASE_URL: %v", err)
	}

	cfg.MaxConns = int32(concurrentRunners / 2)
	if cfg.MaxConns < 2 {
		cfg.MaxConns = 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("could not open small pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("small pool not reachable: %v", err)
	}

	big := newFixture(t)
	f := *big
	f.pool = pool

	g, gctx := errgroup.WithContext(ctx)
	var startedOnce sync.Once
	startedAt := time.Time{}
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			startedOnce.Do(func() { startedAt = time.Now() })
			return runMigrations(gctx, f.pool, f.opts())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("small-pool concurrent runMigrations error after %s: %v", time.Since(startedAt), err)
	}
	if got, want := big.appliedVersions(t), big.versions; !equalStrings(got, want) {
		t.Fatalf("small-pool schema_migrations = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunMigrationsRejectsInvalidDirection(t *testing.T) {
	bad := []string{"", "UP", "DOWN", "rollback", "x", " up "}
	for _, dir := range bad {
		err := runMigrations(context.Background(), nil, runOptions{Direction: dir})
		if err == nil {
			t.Errorf("direction %q: want error, got nil", dir)
			continue
		}
		if !strings.Contains(err.Error(), "invalid direction") {
			t.Errorf("direction %q: error %q does not mention 'invalid direction'", dir, err)
		}
	}
}
