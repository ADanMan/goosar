package dispatch

// Тест на CancelActiveForConvo — единственную функцию, добавленную в этот
// (чужой для T-027-chat) пакет доменом chat (server2/internal/chat), см.
// server2/docs/decisions.md. Остальной dispatch тестами не покрывается
// здесь — это ответственность его собственной сессии (task/dispatch).

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

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

	dbName := fmt.Sprintf("server2_dispatch_test_%d", time.Now().UnixNano())
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

func TestCancelActiveForConvo(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	var wsID, acctID, execID, opID, convoID string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO accounts (acct_email, acct_full_name) VALUES ('u@example.com','U') RETURNING id`).Scan(&acctID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO spaces (ws_title, ws_slug, ws_ticket_prefix) VALUES ('WS','ws-1','TST') RETURNING id`).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO executors (workspace_id, ex_title, ex_mode, ex_provider) VALUES ($1,'R','local','claude-code') RETURNING id`, wsID).Scan(&execID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO operatives (workspace_id, executor_id, op_title, op_runtime_mode) VALUES ($1,$2,'A','local') RETURNING id`, wsID, execID).Scan(&opID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO convos (workspace_id, operative_id, cv_creator_account_id) VALUES ($1,$2,$3) RETURNING id`, wsID, opID, acctID).Scan(&convoID); err != nil {
		t.Fatal(err)
	}

	deps := New(nil, nil)
	jobID, err := deps.Enqueue(ctx, db.Pool, JobSpec{WorkspaceID: wsID, OperativeID: opID, ExecutorID: execID, ConvoID: convoID, Kind: KindChat})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	jobs, err := deps.CancelActiveForConvo(ctx, db.Pool, wsID, convoID)
	if err != nil {
		t.Fatalf("CancelActiveForConvo: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != jobID || jobs[0].Status != StatusCancelled {
		t.Fatalf("unexpected cancelled jobs: %+v", jobs)
	}

	again, err := deps.CancelActiveForConvo(ctx, db.Pool, wsID, convoID)
	if err != nil {
		t.Fatalf("CancelActiveForConvo (idempotent call): %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected no more active jobs to cancel, got %+v", again)
	}
}
