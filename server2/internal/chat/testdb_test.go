package chat

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

// newTestStore — тот же приём, что и project.newTestStore/feed.newTestStore.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminURL := "postgres://postgres@localhost:5432/postgres"
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin): %v", err)
	}
	t.Cleanup(admin.Close)

	dbName := fmt.Sprintf("server2_chat_test_%d", time.Now().UnixNano())
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

type fixture struct {
	WorkspaceID string
	AccountID   string
	ExecutorID  string
	AgentID     string
}

// seedAgent — воркспейс + участник + executor + operative "public_to
// workspace", достаточные, чтобы создать чат-сессию и отправить сообщение.
func seedAgent(t *testing.T, db *store.Store) fixture {
	t.Helper()
	ctx := context.Background()
	var f fixture
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO accounts (acct_email, acct_full_name) VALUES ($1, 'Test User') RETURNING id`,
		fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())).Scan(&f.AccountID)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO spaces (ws_title, ws_slug, ws_ticket_prefix) VALUES ('Test WS', $1, 'TST') RETURNING id`,
		fmt.Sprintf("test-ws-%d", time.Now().UnixNano())).Scan(&f.WorkspaceID)
	if err != nil {
		t.Fatalf("insert space: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO space_members (workspace_id, account_id, sm_role) VALUES ($1, $2, 'owner')`,
		f.WorkspaceID, f.AccountID); err != nil {
		t.Fatalf("insert space_member: %v", err)
	}
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO executors (workspace_id, ex_title, ex_mode, ex_provider) VALUES ($1, 'Test Runtime', 'local', 'claude-code') RETURNING id`,
		f.WorkspaceID).Scan(&f.ExecutorID)
	if err != nil {
		t.Fatalf("insert executor: %v", err)
	}
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO operatives (workspace_id, executor_id, op_title, op_runtime_mode, op_permission_mode, op_owner_account_id)
		VALUES ($1, $2, 'Test Agent', 'local', 'private', $3) RETURNING id`,
		f.WorkspaceID, f.ExecutorID, f.AccountID).Scan(&f.AgentID)
	if err != nil {
		t.Fatalf("insert operative: %v", err)
	}
	return f
}
