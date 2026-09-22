package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRun_ArgumentValidation(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer

	if err := run(ctx, nil, &out); err == nil {
		t.Fatal("no command must be an error")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("usage not printed: %s", out.String())
	}
	if err := run(ctx, []string{"frobnicate"}, &out); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown command: err = %v", err)
	}
	if err := run(ctx, []string{"confirm"}, &out); err == nil || !strings.Contains(err.Error(), "<id>") {
		t.Fatalf("confirm without id: err = %v", err)
	}
	if err := run(ctx, []string{"help"}, &out); err != nil {
		t.Fatalf("help: %v", err)
	}
}

func TestRun_ConfirmGrantEndToEnd(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database not reachable: %v", err)
	}

	var requesterID, targetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('CLI Requester', 'goosar-admin-cli-req@goosar.test')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`).Scan(&requesterID); err != nil {
		t.Fatalf("create requester: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('CLI Target', 'goosar-admin-cli-target@goosar.test')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`).Scan(&targetID); err != nil {
		t.Fatalf("create target: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM deployment_admin WHERE user_id IN ($1, $2)`, requesterID, targetID)
		pool.Exec(ctx, `DELETE FROM deployment_admin_pending WHERE target_user_id IN ($1, $2)`, requesterID, targetID)
		pool.Exec(ctx, `DELETE FROM admin_audit WHERE target_id IN ($1, $2)`, requesterID, targetID)
		pool.Exec(ctx, `DELETE FROM "user" WHERE id IN ($1, $2)`, requesterID, targetID)
	})

	var pendingID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO deployment_admin_pending (action, target_user_id, requested_by)
		VALUES ('grant', $1, $2) RETURNING id
	`, targetID, requesterID).Scan(&pendingID); err != nil {
		t.Fatalf("file pending: %v", err)
	}

	var out bytes.Buffer
	if err := run(ctx, []string{"list-pending"}, &out); err != nil {
		t.Fatalf("list-pending: %v", err)
	}
	if !strings.Contains(out.String(), pendingID) {
		t.Fatalf("list-pending does not show the request: %s", out.String())
	}

	out.Reset()
	if err := run(ctx, []string{"confirm", pendingID}, &out); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.Contains(out.String(), "confirmed grant") {
		t.Fatalf("confirm output: %s", out.String())
	}

	var isAdmin int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM deployment_admin WHERE user_id = $1`, targetID).Scan(&isAdmin); err != nil {
		t.Fatalf("check role: %v", err)
	}
	if isAdmin != 1 {
		t.Fatal("confirm did not grant the role")
	}
	var audited int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM admin_audit
		WHERE action = 'deployment_admin.grant.confirmed' AND target_id = $1 AND request_id = $2
	`, targetID, pendingID).Scan(&audited); err != nil {
		t.Fatalf("check audit: %v", err)
	}
	if audited != 1 {
		t.Fatal("confirm not journaled with the pending id")
	}

	out.Reset()
	if err := run(ctx, []string{"confirm", pendingID}, &out); err == nil || !strings.Contains(err.Error(), "no pending") {
		t.Fatalf("re-confirm: err = %v", err)
	}
}
