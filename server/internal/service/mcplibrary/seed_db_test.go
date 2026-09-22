package mcplibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func testQueries(t *testing.T) (*db.Queries, func()) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	return db.New(pool), pool.Close
}

func testBox(t *testing.T) *secretbox.Box {
	t.Helper()
	t.Setenv("GOOSAR_MCP_SECRET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	box := handler.MCPSecretBoxFromEnv()
	if box == nil {
		t.Fatal("expected a usable secretbox from the test key")
	}
	return box
}

func cleanupServer(t *testing.T, queries *db.Queries, name string) {
	t.Helper()
	row, err := queries.GetDeploymentMcpServerByName(context.Background(), name)
	if err != nil {
		return
	}
	_, _ = queries.DeleteDeploymentMcpServer(context.Background(), row.ID)
}

func TestSeedCreatesRowsForEachService(t *testing.T) {
	queries, closePool := testQueries(t)
	defer closePool()
	box := testBox(t)

	env := map[string]string{
		EnvJiraURL:       "https://jira.acceptance.example.com",
		EnvConfluenceURL: "https://wiki.acceptance.example.com",
		EnvEWSURL:        "https://mail.acceptance.example.com/EWS/Exchange.asmx",
		EnvBitrix24URL:   "https://b24.acceptance.example.com/rest/",
		EnvMCPGatewayURL: "https://gw.acceptance.example.com/mcp-proxy",
	}
	specs := BuildSpecs(func(k string) string { return env[k] })
	for _, s := range specs {
		defer cleanupServer(t, queries, s.Name)
	}

	var out bytes.Buffer
	results, err := Seed(context.Background(), queries, box, specs, false, &out)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Outcome != OutcomeCreated {
			t.Fatalf("%s: expected created, got %s (%s)", r.Name, r.Outcome, r.Detail)
		}
	}

	row, err := queries.GetDeploymentMcpServerByName(context.Background(), NameMCPGateway)
	if err != nil {
		t.Fatalf("get mcp-gateway row: %v", err)
	}
	if string(row.CredentialSchema) != "[]" {
		t.Fatalf("mcp-gateway credential_schema must be empty, got %s", row.CredentialSchema)
	}
	jiraRow, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira)
	if err != nil {
		t.Fatalf("get jira row: %v", err)
	}
	if !bytes.Contains(jiraRow.CredentialSchema, []byte(JiraPersonalTokenField)) {
		t.Fatalf("jira credential_schema must mention %s, got %s", JiraPersonalTokenField, jiraRow.CredentialSchema)
	}
	opened, err := handler.OpenConfigDocumentWithBox(box, jiraRow.Config)
	if err != nil {
		t.Fatalf("open jira config: %v", err)
	}
	if !bytes.Contains(opened, []byte("jira.acceptance.example.com")) {
		t.Fatalf("jira config must carry the seeded address, got %s", opened)
	}

	if bytes.Contains(opened, []byte("\"JIRA_PERSONAL_TOKEN\":\"s")) {
		t.Fatal("credential_schema must never carry a value")
	}
}

func TestSeedIdempotentRerunNoChanges(t *testing.T) {
	queries, closePool := testQueries(t)
	defer closePool()
	box := testBox(t)

	env := map[string]string{EnvJiraURL: "https://jira.idem.example.com"}
	specs := BuildSpecs(func(k string) string { return env[k] })
	defer cleanupServer(t, queries, NameJira)

	if _, err := Seed(context.Background(), queries, box, specs, false, nil); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	before, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira)
	if err != nil {
		t.Fatalf("get after first seed: %v", err)
	}

	results, err := Seed(context.Background(), queries, box, specs, false, nil)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if results[0].Outcome != OutcomeUpToDate {
		t.Fatalf("expected up-to-date on rerun, got %s", results[0].Outcome)
	}
	after, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira)
	if err != nil {
		t.Fatalf("get after second seed: %v", err)
	}
	if before.ID != after.ID || before.UpdatedAt.Time != after.UpdatedAt.Time {
		t.Fatal("idempotent rerun must not touch the row")
	}
}

func TestSeedSkipsEmptyEnvVar(t *testing.T) {
	queries, closePool := testQueries(t)
	defer closePool()
	box := testBox(t)

	specs := BuildSpecs(func(string) string { return "" })
	if len(specs) != 0 {
		t.Fatalf("expected no specs built, got %d", len(specs))
	}
	results, err := Seed(context.Background(), queries, box, specs, false, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
	if _, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira); err == nil {
		t.Fatal("no row should exist when the address was never seeded")
	}
}

func TestSeedProtectsHandEditedRow(t *testing.T) {
	queries, closePool := testQueries(t)
	defer closePool()
	box := testBox(t)

	env := map[string]string{EnvJiraURL: "https://jira.protect.example.com"}
	specs := BuildSpecs(func(k string) string { return env[k] })
	defer cleanupServer(t, queries, NameJira)

	if _, err := Seed(context.Background(), queries, box, specs, false, nil); err != nil {
		t.Fatalf("first seed: %v", err)
	}

	existing, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	handEdited, err := handler.OpenConfigDocumentWithBox(box, existing.Config)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(handEdited, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	doc["command"] = "admin-picked-binary"
	editedJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sealed, err := handler.SealConfigDocumentWithBox(box, editedJSON)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := queries.UpdateDeploymentMcpServer(context.Background(), db.UpdateDeploymentMcpServerParams{
		ID:     existing.ID,
		Config: sealed,
	}); err != nil {
		t.Fatalf("simulate hand-edit: %v", err)
	}

	results, err := Seed(context.Background(), queries, box, specs, false, nil)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if results[0].Outcome != OutcomeSkippedManual {
		t.Fatalf("expected the hand-edited row to be protected, got %s", results[0].Outcome)
	}
	after, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira)
	if err != nil {
		t.Fatalf("get after protective seed: %v", err)
	}
	reopened, err := handler.OpenConfigDocumentWithBox(box, after.Config)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !bytes.Contains(reopened, []byte("admin-picked-binary")) {
		t.Fatal("hand-edited command must survive the seed run untouched")
	}
}

func TestSeedDryRunMakesNoWrites(t *testing.T) {
	queries, closePool := testQueries(t)
	defer closePool()
	box := testBox(t)

	env := map[string]string{EnvJiraURL: "https://jira.dryrun.example.com"}
	specs := BuildSpecs(func(k string) string { return env[k] })
	defer cleanupServer(t, queries, NameJira)

	results, err := Seed(context.Background(), queries, box, specs, true, nil)
	if err != nil {
		t.Fatalf("dry-run seed: %v", err)
	}
	if results[0].Outcome != OutcomeDryRun {
		t.Fatalf("expected dry-run outcome, got %s", results[0].Outcome)
	}
	if _, err := queries.GetDeploymentMcpServerByName(context.Background(), NameJira); err == nil {
		t.Fatal("dry-run must not create a row")
	}
}
