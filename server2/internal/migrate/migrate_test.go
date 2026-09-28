package migrate

import (
	"context"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLoadFS_ordersByVersionAndIgnoresOtherFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/002_workspace.up.sql":    &fstest.MapFile{Data: []byte("-- two")},
		"migrations/001_identity.up.sql":     &fstest.MapFile{Data: []byte("-- one")},
		"migrations/001_identity.down.sql":   &fstest.MapFile{Data: []byte("-- down, must be ignored")},
		"migrations/check_names.py":          &fstest.MapFile{Data: []byte("# not a migration")},
		"migrations/010_integrations.up.sql": &fstest.MapFile{Data: []byte("-- ten")},
	}
	files, err := LoadFS(fsys, "migrations")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 .up.sql files, got %d: %+v", len(files), files)
	}
	wantOrder := []int{1, 2, 10}
	for i, f := range files {
		if f.Version != wantOrder[i] {
			t.Errorf("position %d: version = %d, want %d", i, f.Version, wantOrder[i])
		}
	}
	if files[0].Name != "001_identity" {
		t.Errorf("Name = %q, want 001_identity", files[0].Name)
	}
	if files[0].SQL != "-- one" {
		t.Errorf("SQL = %q, want %q", files[0].SQL, "-- one")
	}
}

func TestLoad_realMigrationsDirParsesAllTwelve(t *testing.T) {
	// Название теста сохранено (историческое из T-025); T-026 добавил
	// 013_authn_token_epoch (см. server2/docs/decisions.md), отсюда 13.
	files, err := Load("../../migrations")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(files) != 13 {
		t.Fatalf("expected 13 migration files in server2/migrations, got %d", len(files))
	}
	for i, f := range files {
		if f.Version != i+1 {
			t.Errorf("position %d: version = %d, want %d (files must be contiguous and sorted)", i, f.Version, i+1)
		}
	}
}

// TestApply_isIdempotentAndSequential применяет реальные миграции server2 к
// одноразовой базе на локальном Postgres (localhost:5432, user postgres, без
// пароля — см. cleanroom.txt) и проверяет, что повторный вызов не падает и не
// применяет ничего заново.
func TestApply_isIdempotentAndSequential(t *testing.T) {
	if testing.Short() {
		t.Skip("нужен живой Postgres, пропущено в -short")
	}
	adminURL := envOr("IMPORT_TEST_ADMIN_DATABASE_URL", "postgres://postgres@localhost:5432/postgres")
	dbName := "server2_migrate_test_" + time.Now().UTC().Format("20060102150405")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("нет доступа к локальному Postgres (%s): %v", adminURL, err)
	}
	// t.Cleanup выполняется после возврата из теста (и после его defer'ов) в
	// порядке LIFO — поэтому Close() регистрируется ПЕРВЫМ (сработает
	// последним), а DROP DATABASE, которому нужен ещё живой admin-пул, —
	// ВТОРЫМ (сработает первым). Раньше DROP был обычным defer'ом рядом с
	// Close() и из-за порядка выполнения тела функции никогда не успевал
	// отработать до закрытия пула.
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Skipf("нет доступа к локальному Postgres (%s): %v", adminURL, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cctx, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)"); err != nil {
			t.Logf("не удалось удалить временную базу %s: %v", dbName, err)
		}
	})

	dbURL := adminURL[:len(adminURL)-len("/postgres")] + "/" + dbName
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	applied, err := Apply(ctx, pool, "../../migrations")
	if err != nil {
		t.Fatalf("Apply (1st run): %v", err)
	}
	if len(applied) != 13 {
		t.Fatalf("1st run applied %d migrations, want 13: %v", len(applied), applied)
	}

	var tickets int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tickets").Scan(&tickets); err != nil {
		t.Fatalf("tickets table should exist after migration: %v", err)
	}

	appliedAgain, err := Apply(ctx, pool, "../../migrations")
	if err != nil {
		t.Fatalf("Apply (2nd run): %v", err)
	}
	if len(appliedAgain) != 0 {
		t.Fatalf("2nd run should be a no-op, applied: %v", appliedAgain)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
