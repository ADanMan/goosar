package importer

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestImport_liveSourceServer гоняет полный Run() против реально запущенного
// исходного сервера (например localhost:8199 из живой проверки T-025) и
// одноразовой базы server2 на локальном Postgres, затем повторяет импорт и
// проверяет, что счётчики строк в целевой базе не изменились (идемпотентность).
//
// Пропускается, если не задан IMPORT_SOURCE_URL — см. задание T-025, п.2.
// Обязательные переменные окружения:
//
//	IMPORT_SOURCE_URL   — например http://localhost:8199
//	IMPORT_SOURCE_TOKEN — Bearer PAT/JWT с ролью owner
//	IMPORT_WORKSPACE    — slug или id воркспейса на исходном сервере
//
// Опциональные:
//
//	IMPORT_TEST_ADMIN_DATABASE_URL — DSN администратора Postgres для создания
//	                                  одноразовой базы (по умолчанию
//	                                  postgres://postgres@localhost:5432/postgres)
func TestImport_liveSourceServer(t *testing.T) {
	sourceURL := os.Getenv("IMPORT_SOURCE_URL")
	if sourceURL == "" {
		t.Skip("IMPORT_SOURCE_URL не задан — интеграционный тест пропущен (см. T-025, п.2)")
	}
	token := os.Getenv("IMPORT_SOURCE_TOKEN")
	workspace := os.Getenv("IMPORT_WORKSPACE")
	if token == "" || workspace == "" {
		t.Fatal("IMPORT_SOURCE_URL задан, но IMPORT_SOURCE_TOKEN/IMPORT_WORKSPACE — нет")
	}

	adminURL := envOrDefault("IMPORT_TEST_ADMIN_DATABASE_URL", "postgres://postgres@localhost:5432/postgres")
	dbName := "server2_import_test_" + time.Now().UTC().Format("20060102150405")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin): %v", err)
	}
	// См. комментарий в internal/migrate/migrate_test.go: Close должен быть
	// зарегистрирован до DROP DATABASE, чтобы LIFO-порядок t.Cleanup выполнил
	// удаление базы, пока admin-пул ещё жив.
	t.Cleanup(admin.Close)
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

	opts := Options{
		SourceURL:     sourceURL,
		Token:         token,
		Workspace:     workspace,
		DatabaseURL:   dbURL,
		Migrate:       true,
		MigrationsDir: "../../migrations",
	}

	report1, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run (1st import): %v", err)
	}
	if report1.Counts["tickets"] == 0 && report1.Counts["operatives"] == 0 {
		t.Log("предупреждение: воркспейс не содержит ни задач, ни агентов — проверьте IMPORT_WORKSPACE")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New(target): %v", err)
	}
	defer pool.Close()

	counts1 := tableCounts(ctx, t, pool)

	report2, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run (2nd import, idempotency check): %v", err)
	}
	counts2 := tableCounts(ctx, t, pool)

	for table, n1 := range counts1 {
		if n2 := counts2[table]; n1 != n2 {
			t.Errorf("идемпотентность нарушена: таблица %s: 1-й запуск %d строк, 2-й запуск %d строк", table, n1, n2)
		}
	}
	for k, v := range report1.Counts {
		if report2.Counts[k] != v {
			t.Errorf("отчёт: категория %s: 1-й запуск %d, 2-й запуск %d (должны совпадать)", k, v, report2.Counts[k])
		}
	}
}

func tableCounts(ctx context.Context, t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	tables := []string{
		"spaces", "space_members", "agent_protocols", "executors", "operatives",
		"capabilities", "capability_files", "crews", "crew_members", "tags",
		"field_defs", "initiatives", "initiative_resources", "tickets",
		"ticket_notes", "ticket_marks", "note_marks", "ticket_subscribers",
		"ticket_pr_links", "sentinels", "sentinel_triggers", "convos",
		"convo_messages", "space_mcp_servers", "space_config",
	}
	out := map[string]int{}
	for _, tbl := range tables {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("count(%s): %v", tbl, err)
		}
		out[tbl] = n
	}
	return out
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
