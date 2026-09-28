package export

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

// newTestDB — своя временная БД на пакет тестов (T-029 работает в параллели
// с другими доменами, поэтому не переиспользует чужой dbtest-файл напрямую;
// тот же приём, что server2/internal/{asset,tagging}/dbtest_test.go).
func newTestDB(t *testing.T) *store.Store {
	t.Helper()
	if testing.Short() {
		t.Skip("нужен живой Postgres, пропущено в -short")
	}
	adminURL := envOr("IMPORT_TEST_ADMIN_DATABASE_URL", "postgres://postgres@localhost:5432/postgres")
	dbName := "server2_export_test_" + time.Now().UTC().Format("20060102150405999999999")

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

// seedAccountAndWorkspace создаёт минимальный набор строк (accounts/spaces/
// space_members), нужный store-тестам этого домена, напрямую по SQL —
// пакет не владеет identity/workspace, поэтому не тянет их Store ради теста.
func seedAccountAndWorkspace(t *testing.T, s *Store) (accountID, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO accounts (acct_email, acct_full_name) VALUES ($1, 'Test User') RETURNING id`,
		"export-test-"+t.Name()+"@example.test").Scan(&accountID)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO spaces (ws_title, ws_slug, ws_ticket_prefix) VALUES ('Test WS', $1, 'TST') RETURNING id`,
		"test-ws-"+t.Name()).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO space_members (workspace_id, account_id, sm_role) VALUES ($1, $2, 'owner')`,
		workspaceID, accountID)
	if err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	return accountID, workspaceID
}

func TestCreateJobRejectsSecondActiveJob(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	_, workspaceID := seedAccountAndWorkspace(t, s)

	if _, err := s.CreateJob(context.Background(), workspaceID); err != nil {
		t.Fatalf("first CreateJob: %v", err)
	}
	if _, err := s.CreateJob(context.Background(), workspaceID); err != ErrActiveJobExists() {
		t.Fatalf("expected ErrActiveJobExists on second CreateJob, got %v", err)
	}
}

func TestCreateJobAllowedAfterPreviousCompleted(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	_, workspaceID := seedAccountAndWorkspace(t, s)

	job, err := s.CreateJob(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := s.MarkCompleted(context.Background(), job.ID, "some/key.tar.gz", 123, map[string]any{"ok": true}); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if _, err := s.CreateJob(context.Background(), workspaceID); err != nil {
		t.Fatalf("expected new job allowed after completion, got %v", err)
	}
}

func TestWorkspaceDataQueries(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	accountID, workspaceID := seedAccountAndWorkspace(t, s)

	if _, err := s.Workspace(context.Background(), workspaceID); err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	members, err := s.MembersJSON(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("MembersJSON: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if _, err := s.TicketsJSON(context.Background(), workspaceID); err != nil {
		t.Fatalf("TicketsJSON: %v", err)
	}
	if _, err := s.NotesJSON(context.Background(), workspaceID); err != nil {
		t.Fatalf("NotesJSON: %v", err)
	}
	if _, err := s.ProjectsJSON(context.Background(), workspaceID); err != nil {
		t.Fatalf("ProjectsJSON: %v", err)
	}

	if _, err := s.Profile(context.Background(), accountID); err != nil {
		t.Fatalf("Profile: %v", err)
	}
	memberships, err := s.MembershipsJSON(context.Background(), accountID)
	if err != nil {
		t.Fatalf("MembershipsJSON: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("expected 1 membership, got %d", len(memberships))
	}
}

func TestRecordAudit(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	accountID, workspaceID := seedAccountAndWorkspace(t, s)
	if err := s.RecordAudit(context.Background(), workspaceID, accountID, "workspace.export", "job-1"); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}
	if err := s.RecordAudit(context.Background(), "", accountID, "me.export", accountID); err != nil {
		t.Fatalf("RecordAudit without workspace: %v", err)
	}
}
