package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const roleWorkspaceCleanupSQL = `DELETE FROM workspace WHERE template_key IS NOT NULL`

const roleWorkspaceTestLock int64 = 48400484

func TestProvisionRoles_OutputFormat(t *testing.T) {
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
	queries := db.New(pool)

	guard, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire role-workspace guard connection: %v", err)
	}
	if _, err := guard.Exec(ctx, `SELECT pg_advisory_lock($1)`, roleWorkspaceTestLock); err != nil {
		guard.Release()
		t.Fatalf("acquire role workspace guard: %v", err)
	}

	defer func() {
		guard.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, roleWorkspaceTestLock)
		guard.Release()
	}()

	if _, err := pool.Exec(ctx, roleWorkspaceCleanupSQL); err != nil {
		t.Fatalf("clean role workspaces: %v", err)
	}
	var adminID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('CLI Role Owner', 'goosar-admin-cli-roles@goosar.test')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&adminID); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO deployment_admin (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, adminID); err != nil {
		t.Fatalf("grant deployment admin: %v", err)
	}

	defer func() {
		pool.Exec(ctx, roleWorkspaceCleanupSQL)
		pool.Exec(ctx, `DELETE FROM deployment_admin WHERE user_id = $1`, adminID)
		pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, adminID)
	}()

	t.Setenv("GOOSAR_MCP_SECRET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQ=")

	var templates int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM workspace_template WHERE enabled = true`).Scan(&templates); err != nil {
		t.Fatalf("count templates: %v", err)
	}
	if templates == 0 {
		t.Skip("no enabled workspace templates in this database")
	}

	var first bytes.Buffer
	if err := provisionRoles(ctx, pool, queries, &first); err != nil {
		t.Fatalf("first run: %v\n%s", err, first.String())
	}
	if got := strings.Count(first.String(), "created"); got < templates {
		t.Fatalf("first run printed %d created lines, want %d:\n%s", got, templates, first.String())
	}
	if !strings.Contains(first.String(), "errors 0") {
		t.Fatalf("first run summary missing or reported errors:\n%s", first.String())
	}
	var second bytes.Buffer
	if err := provisionRoles(ctx, pool, queries, &second); err != nil {
		t.Fatalf("second run: %v\n%s", err, second.String())
	}
	if got := strings.Count(second.String(), "skipped"); got < templates {
		t.Fatalf("second run printed %d skipped lines, want %d:\n%s", got, templates, second.String())
	}
	if !strings.Contains(second.String(), "created 0") {
		t.Fatalf("second run created something:\n%s", second.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(second.String()), "\n") {
		if strings.HasPrefix(line, "error") {
			t.Fatalf("second run reported an error line: %s", line)
		}
	}
}

func TestRun_ProvisionRolesArgs(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"provision-roles", "extra"}, &out); err == nil {
		t.Fatal("provision-roles with an argument must be an error")
	}
	if !strings.Contains(usage, "provision-roles") {
		t.Fatal("provision-roles is missing from the usage text")
	}
}
