package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestParseMcpLibraryArgs(t *testing.T) {
	if _, err := parseMcpLibraryArgs(nil); err == nil {
		t.Fatal("expected an error with no sub-subcommand")
	}
	if _, err := parseMcpLibraryArgs([]string{"wat"}); err == nil {
		t.Fatal("expected an error for an unknown sub-subcommand")
	}
	dryRun, err := parseMcpLibraryArgs([]string{"seed"})
	if err != nil || dryRun {
		t.Fatalf("seed: got dryRun=%v err=%v", dryRun, err)
	}
	dryRun, err = parseMcpLibraryArgs([]string{"seed", "--dry-run"})
	if err != nil || !dryRun {
		t.Fatalf("seed --dry-run: got dryRun=%v err=%v", dryRun, err)
	}
	if _, err := parseMcpLibraryArgs([]string{"seed", "--wat"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestMcpLibrarySeedRequiresSecretKey(t *testing.T) {
	t.Setenv("GOOSAR_MCP_SECRET_KEY", "")
	t.Setenv("GOOSAR_DEPLOYMENT_JIRA_URL", "https://jira.example.com")
	var out bytes.Buffer
	err := mcpLibrarySeed(context.Background(), nil, &out, true)
	if err == nil {
		t.Fatal("expected an error with no secret key configured")
	}
}

func TestMcpLibrarySeedEndToEnd(t *testing.T) {
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

	t.Setenv("GOOSAR_MCP_SECRET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("GOOSAR_DEPLOYMENT_JIRA_URL", "https://jira.cli-e2e.example.com")
	t.Setenv("GOOSAR_DEPLOYMENT_CONFLUENCE_URL", "")
	t.Setenv("GOOSAR_DEPLOYMENT_EWS_URL", "")
	t.Setenv("GOOSAR_DEPLOYMENT_BITRIX24_URL", "")
	t.Setenv("GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL", "")
	defer func() {
		row, err := queries.GetDeploymentMcpServerByName(ctx, "jira")
		if err == nil {
			_, _ = queries.DeleteDeploymentMcpServer(ctx, row.ID)
		}
	}()

	var out bytes.Buffer
	if err := mcpLibrarySeed(ctx, queries, &out, false); err != nil {
		t.Fatalf("mcpLibrarySeed: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("jira")) {
		t.Fatalf("expected the report to mention jira, got:\n%s", out.String())
	}
	if _, err := queries.GetDeploymentMcpServerByName(ctx, "jira"); err != nil {
		t.Fatalf("expected a jira row to exist: %v", err)
	}
}
