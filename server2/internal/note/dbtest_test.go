package note

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

// newTestDB — см. server2/internal/tagging/dbtest_test.go (тот же приём,
// независимая копия — пакеты друг о друге не знают).
func newTestDB(t *testing.T) *store.Store {
	t.Helper()
	if testing.Short() {
		t.Skip("нужен живой Postgres, пропущено в -short")
	}
	adminURL := envOr("IMPORT_TEST_ADMIN_DATABASE_URL", "postgres://postgres@localhost:5432/postgres")
	dbName := "server2_note_test_" + time.Now().UTC().Format("20060102150405999999999")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("нет доступа к локальному Postgres (%s): %v", adminURL, err)
	}
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
	t.Cleanup(pool.Close)

	if _, err := migrate.Apply(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("Apply migrations: %v", err)
	}
	return &store.Store{Pool: pool}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func seedWorkspace(t *testing.T, db *store.Store) (workspaceID, accountID string) {
	t.Helper()
	ctx := context.Background()
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO accounts (acct_email, acct_full_name) VALUES ($1, 'Test User') RETURNING id`,
		"note-test-"+time.Now().Format("150405.000000")+"@example.test").Scan(&accountID); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO spaces (ws_title, ws_slug, ws_ticket_prefix) VALUES ('Test WS', $1, 'TST') RETURNING id`,
		"test-ws-"+time.Now().Format("150405.000000")).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO space_members (workspace_id, account_id, sm_role) VALUES ($1, $2, 'owner')`,
		workspaceID, accountID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	return workspaceID, accountID
}

func seedTicket(t *testing.T, db *store.Store, workspaceID, creatorAccountID string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(), `
		INSERT INTO tickets (workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_creator_type, tk_creator_id)
		VALUES ($1, 1, 'TST-1', 'Test ticket', 'member', $2) RETURNING id`,
		workspaceID, creatorAccountID).Scan(&id)
	if err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	return id
}
