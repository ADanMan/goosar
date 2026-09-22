package taskusagebackfill_test

import (
	"context"
	"errors"
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
	"github.com/adanman/goosar/server/internal/taskusagebackfill"
)

func TestHook_DirectV034Upgrade(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}

	ctx := context.Background()
	if !databaseReachable(ctx, adminURL) {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	tmpDB := fmt.Sprintf("goosar_v034_upgrade_%d", time.Now().UnixNano())
	if err := createDatabase(ctx, adminURL, tmpDB); err != nil {
		t.Fatalf("create temp database %s: %v", tmpDB, err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), adminURL, tmpDB); err != nil {
			t.Logf("drop temp database %s: %v", tmpDB, err)
		}
	})

	tmpURL := replaceDatabase(adminURL, tmpDB)
	pool, err := pgxpool.New(ctx, tmpURL)
	if err != nil {
		t.Fatalf("connect to temp database: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := applyMigrationsUpTo(ctx, pool, "102_task_usage_hourly_pipeline"); err != nil {
		t.Fatalf("apply migrations to 102: %v", err)
	}

	wsID, runtimeID, agentID, taskID := seedTaskUsageFixture(t, ctx, pool)

	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_usage (
			task_id, provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			created_at
		) VALUES ($1, 'openai', 'gpt-test', 10, 20, 0, 0, $2)
	`, taskID, twoHoursAgo); err != nil {
		t.Fatalf("seed task_usage: %v", err)
	}
	_ = wsID
	_ = runtimeID
	_ = agentID

	pendingErr := tryApplyMigration(ctx, pool, "103_drop_legacy_daily_rollups")
	if pendingErr == nil {
		t.Fatalf("migration 103 guard did not trip with stale watermark; preconditions invalid")
	}
	if !strings.Contains(pendingErr.Error(), "refusing to drop legacy daily rollups") {
		t.Fatalf("expected fail-closed guard error, got %v", pendingErr)
	}

	res, err := taskusagebackfill.Hook(ctx, pool, taskusagebackfill.HookOptions{})
	if err != nil {
		t.Fatalf("Hook returned error: %v", err)
	}
	if res.Skipped != "" {
		t.Fatalf("Hook should have run a backfill; got skipped=%q", res.Skipped)
	}
	if !res.WatermarkStamped {
		t.Fatalf("Hook should have stamped the watermark")
	}
	if res.SlicesProcessed < 1 {
		t.Fatalf("expected at least 1 slice processed, got %d", res.SlicesProcessed)
	}

	var hourlyRows int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM task_usage_hourly
	`).Scan(&hourlyRows); err != nil {
		t.Fatalf("count task_usage_hourly: %v", err)
	}
	if hourlyRows == 0 {
		t.Fatalf("backfill did not produce any hourly rows; check seed fixture")
	}

	var watermark time.Time
	if err := pool.QueryRow(ctx, `
		SELECT watermark_at FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&watermark); err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	expected := time.Now().UTC().Add(-5 * time.Minute)
	if delta := watermark.UTC().Sub(expected); delta > time.Minute || delta < -2*time.Minute {
		t.Fatalf("watermark %s far from expected %s (delta=%s)",
			watermark.Format(time.RFC3339), expected.Format(time.RFC3339), delta)
	}

	if err := tryApplyMigration(ctx, pool, "103_drop_legacy_daily_rollups"); err != nil {
		t.Fatalf("migration 103 still fails after hook: %v", err)
	}
}

func TestHook_FreshDatabaseStampsWatermarkOnly(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	ctx := context.Background()
	if !databaseReachable(ctx, adminURL) {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	tmpDB := fmt.Sprintf("goosar_v034_fresh_%d", time.Now().UnixNano())
	if err := createDatabase(ctx, adminURL, tmpDB); err != nil {
		t.Fatalf("create temp database: %v", err)
	}
	t.Cleanup(func() {
		_ = dropDatabase(context.Background(), adminURL, tmpDB)
	})

	pool, err := pgxpool.New(ctx, replaceDatabase(adminURL, tmpDB))
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := applyMigrationsUpTo(ctx, pool, "102_task_usage_hourly_pipeline"); err != nil {
		t.Fatalf("apply migrations to 102: %v", err)
	}

	res, err := taskusagebackfill.Hook(ctx, pool, taskusagebackfill.HookOptions{})
	if err != nil {
		t.Fatalf("Hook on empty DB: %v", err)
	}
	if res.Skipped != "task_usage_empty" {
		t.Fatalf("expected skipped=task_usage_empty, got %q", res.Skipped)
	}
	if !res.WatermarkStamped {
		t.Fatalf("watermark must still be stamped on empty DB so 103 can pass")
	}

	if err := tryApplyMigration(ctx, pool, "103_drop_legacy_daily_rollups"); err != nil {
		t.Fatalf("migration 103 fresh-DB path failed: %v", err)
	}
}

func resolveMigrationsDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed")
	}

	dir := filepath.Join(filepath.Dir(here), "..", "..", "migrations")
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("migrations dir not at %s", dir)
	}
	return dir, nil
}

func databaseReachable(ctx context.Context, url string) bool {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return false
	}
	defer pool.Close()
	return pool.Ping(ctx) == nil
}

func createDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		return err
	}
	return nil
}

func dropDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name)); err != nil {
		return err
	}
	return nil
}

func replaceDatabase(url, name string) string {
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return url
	}
	rest := url[idx+1:]
	q := strings.Index(rest, "?")
	if q < 0 {
		return url[:idx+1] + name
	}
	return url[:idx+1] + name + rest[q:]
}

func applyMigrationsUpTo(ctx context.Context, pool *pgxpool.Pool, lastVersion string) error {
	dir, err := resolveMigrationsDir()
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

func tryApplyMigration(ctx context.Context, pool *pgxpool.Pool, version string) error {
	dir, err := resolveMigrationsDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, version+".up.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`,
		version)
	return err
}

func seedTaskUsageFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string, string) {
	t.Helper()

	var wsID, runtimeID, agentID, taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('upgrade-test', 'upgrade-test')
		RETURNING id
	`).Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'upgrade-runtime', 'cloud', 'p', 'online',
		        '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks
		)
		VALUES ($1, 'upgrade-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, payload
		)
		VALUES ($1, $2, 'queued', '{}'::jsonb)
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {

		var altErr error
		altErr = pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id)
			VALUES ($1, $2)
			RETURNING id
		`, agentID, runtimeID).Scan(&taskID)
		if altErr != nil {
			t.Fatalf("seed agent_task_queue: %v / %v", err, altErr)
		}
	}
	return wsID, runtimeID, agentID, taskID
}

var _ = errors.New
