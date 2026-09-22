package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/migrations"
)

func TestStampWatermark(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}

	ctx := context.Background()
	if !testDatabaseReachable(ctx, adminURL) {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	tmpDB := fmt.Sprintf("goosar_hourly_backfill_%d", time.Now().UnixNano())
	if err := testCreateDatabase(ctx, adminURL, tmpDB); err != nil {
		t.Fatalf("create temp database %s: %v", tmpDB, err)
	}
	t.Cleanup(func() {
		if err := testDropDatabase(context.Background(), adminURL, tmpDB); err != nil {
			t.Logf("drop temp database %s: %v", tmpDB, err)
		}
	})

	pool, err := pgxpool.New(ctx, testReplaceDatabase(adminURL, tmpDB))
	if err != nil {
		t.Fatalf("connect to temp database: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := testApplyMigrationsUpTo(ctx, pool, "101_task_usage_hourly_schema"); err != nil {
		t.Fatalf("apply migrations to 101: %v", err)
	}

	if err := stampWatermark(ctx, pool); err != nil {
		t.Fatalf("stampWatermark: %v", err)
	}

	var watermark, dbNow time.Time
	if err := pool.QueryRow(ctx,
		`SELECT watermark_at, now() FROM task_usage_hourly_rollup_state WHERE id = 1`,
	).Scan(&watermark, &dbNow); err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	lag := dbNow.Sub(watermark)
	if lag < 4*time.Minute+30*time.Second || lag > 5*time.Minute+30*time.Second {
		t.Fatalf("watermark lag = %s, want ~5m (watermark=%s now=%s)", lag, watermark, dbNow)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM task_usage_hourly_rollup_state WHERE id = 1`); err != nil {
		t.Fatalf("delete state row: %v", err)
	}
	if err := stampWatermark(ctx, pool); err != nil {
		t.Fatalf("stampWatermark without state row: %v", err)
	}
}

func testDatabaseReachable(ctx context.Context, url string) bool {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return false
	}
	defer pool.Close()
	return pool.Ping(ctx) == nil
}

func testCreateDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name))
	return err
}

func testDropDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name))
	return err
}

func testReplaceDatabase(url, name string) string {
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return url
	}
	rest := url[idx+1:]
	if q := strings.Index(rest, "?"); q >= 0 {
		return url[:idx+1] + name + rest[q:]
	}
	return url[:idx+1] + name
}

func testResolveMigrationsDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", "migrations"))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("migrations dir not at %s", dir)
	}
	return dir, nil
}

func testApplyMigrationsUpTo(ctx context.Context, pool *pgxpool.Pool, lastVersion string) error {
	dir, err := testResolveMigrationsDir()
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}

	for _, f := range files {
		v := migrations.ExtractVersion(f)
		sql, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply %s: %w", v, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`,
			v); err != nil {
			return err
		}
		if v == lastVersion {
			return nil
		}
	}
	return fmt.Errorf("migration %q not found", lastVersion)
}
