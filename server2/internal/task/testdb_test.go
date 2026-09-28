package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

// newTestStore — тот же приём, что и chat/project/feed.newTestStore: своя
// одноразовая БД на запуск теста (не goosar2_t027a — та для ручной/e2e
// проверки), с полным набором миграций.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminURL := "postgres://postgres@localhost:5432/postgres"
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("postgres недоступен (%v) — интеграционный тест пропущен", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("postgres недоступен (%v) — интеграционный тест пропущен", err)
	}
	t.Cleanup(admin.Close)

	dbName := fmt.Sprintf("server2_task_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(cctx, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
	})

	dbURL := adminURL[:len(adminURL)-len("/postgres")] + "/" + dbName
	db, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := migrate.Apply(ctx, db.Pool, "../../migrations"); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return db
}

// fixture — воркспейс + владелец + executor (online) + agent (public_to
// workspace), достаточные для CreateIssue/назначения/автозапуска.
type fixture struct {
	WorkspaceID string
	AccountID   string
	ExecutorID  string
	AgentID     string
}

func seedWorkspace(t *testing.T, db *store.Store) fixture {
	t.Helper()
	ctx := context.Background()
	var f fixture
	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	suffix := time.Now().UnixNano()

	must(db.Pool.QueryRow(ctx, `INSERT INTO accounts (acct_email, acct_full_name) VALUES ($1,'Test Owner') RETURNING id`,
		fmt.Sprintf("owner-%d@example.com", suffix)).Scan(&f.AccountID), "insert account")
	must(db.Pool.QueryRow(ctx, `INSERT INTO spaces (ws_title, ws_slug, ws_ticket_prefix) VALUES ('Test WS',$1,'CTA') RETURNING id`,
		fmt.Sprintf("test-ws-%d", suffix)).Scan(&f.WorkspaceID), "insert space")
	_, err := db.Pool.Exec(ctx, `INSERT INTO space_members (workspace_id, account_id, sm_role) VALUES ($1,$2,'owner')`,
		f.WorkspaceID, f.AccountID)
	must(err, "insert space_member")
	must(db.Pool.QueryRow(ctx, `INSERT INTO executors (workspace_id, ex_title, ex_mode, ex_provider, ex_status)
		VALUES ($1,'Test Runtime','local','claude-code','online') RETURNING id`, f.WorkspaceID).Scan(&f.ExecutorID), "insert executor")
	must(db.Pool.QueryRow(ctx, `INSERT INTO operatives (workspace_id, executor_id, op_title, op_runtime_mode, op_permission_mode, op_owner_account_id)
		VALUES ($1,$2,'Test Agent','local','private',$3) RETURNING id`, f.WorkspaceID, f.ExecutorID, f.AccountID).Scan(&f.AgentID), "insert operative")
	return f
}
