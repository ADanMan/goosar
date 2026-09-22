package dataexport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil || pool.Ping(ctx) != nil {
		if os.Getenv("GOOSAR_REQUIRE_TEST_DB") == "1" || os.Getenv("CI") != "" {
			fmt.Printf("FATAL: dataexport tests need a database: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Skipping dataexport tests: no database")
		os.Exit(0)
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

type archive map[string][]byte

func unpack(t *testing.T, raw []byte) archive {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	out := archive{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = body
	}
	return out
}

func (a archive) rows(t *testing.T, name string) []map[string]any {
	t.Helper()
	body, ok := a["data/"+name+".json"]
	if !ok {
		t.Fatalf("archive has no data/%s.json; entries: %v", name, a.names())
	}
	var out []map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return out
}

func (a archive) names() []string {
	names := make([]string, 0, len(a))
	for name := range a {
		names = append(names, name)
	}
	return names
}

type fixture struct {
	workspaceID string
	userID      string
	issueID     string
	commentID   string
	agentID     string
	sessionID   string
	messageID   string
	mcpID       string
}

var runID = fmt.Sprintf("%d", time.Now().UnixNano())

func newFixture(t *testing.T, label string) fixture {
	t.Helper()
	ctx := context.Background()
	suffix := label
	unique := label + "-" + runID
	var f fixture

	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture %s: %v", what, err)
		}
	}

	must(testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Export Subject "+suffix, "export-"+unique+"@goosar.ru").Scan(&f.userID), "user")
	must(testPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"Export WS "+suffix, "export-ws-"+unique).Scan(&f.workspaceID), "workspace")
	must(testPool.QueryRow(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner') RETURNING id`,
		f.workspaceID, f.userID).Scan(new(string)), "member")
	must(testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, description, status, creator_type, creator_id)
		 VALUES ($1, $2, $3, 'todo', 'member', $4) RETURNING id`,
		f.workspaceID, "Issue "+suffix, "secret business text "+suffix, f.userID).Scan(&f.issueID), "issue")
	must(testPool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		 VALUES ($1, $2, 'member', $3, $4) RETURNING id`,
		f.workspaceID, f.issueID, f.userID, "comment "+suffix).Scan(&f.commentID), "comment")
	var runtimeID string
	must(testPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		 VALUES ($1, $2, 'local', 'claude') RETURNING id`,
		f.workspaceID, "Runtime "+suffix).Scan(&runtimeID), "agent_runtime")
	must(testPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, custom_env, mcp_config)
		 VALUES ($1, $2, 'local', $5, $3, $4) RETURNING id`,
		f.workspaceID, "Agent "+suffix,
		`{"ANTHROPIC_API_KEY":"sk-must-not-appear-`+suffix+`"}`,
		`{"mcpServers":{"x":{"env":{"GH_PAT":"ghp-must-not-appear-`+suffix+`"}}}}`,
		runtimeID,
	).Scan(&f.agentID), "agent")
	must(testPool.QueryRow(ctx,
		`INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		f.workspaceID, f.agentID, f.userID, "Chat "+suffix).Scan(&f.sessionID), "chat_session")
	must(testPool.QueryRow(ctx,
		`INSERT INTO chat_message (chat_session_id, role, content)
		 VALUES ($1, 'user', $2) RETURNING id`,
		f.sessionID, "chat body "+suffix).Scan(&f.messageID), "chat_message")
	must(testPool.QueryRow(ctx,
		`INSERT INTO workspace_mcp_server (workspace_id, name, config, transport, created_by)
		 VALUES ($1, $2, $3, 'stdio', $4) RETURNING id`,
		f.workspaceID, "mcp-"+suffix,

		`{"__goosar_sealed__":"sealed-must-not-appear-`+suffix+`"}`,
		f.userID).Scan(&f.mcpID), "workspace_mcp_server")

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, f.userID)
	})
	return f
}

func exportWorkspace(t *testing.T, workspaceID string) (archive, Manifest) {
	t.Helper()
	var buf bytes.Buffer
	manifest, err := WriteWorkspace(context.Background(), testPool, nil, workspaceID, &buf, Options{})
	if err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	return unpack(t, buf.Bytes()), manifest
}

func TestWorkspaceExportCarriesTheWorkspaceContent(t *testing.T) {
	f := newFixture(t, "content")
	a, manifest := exportWorkspace(t, f.workspaceID)

	if manifest.SchemaVersion != SchemaVersion {
		t.Errorf("manifest schema_version = %d, want %d", manifest.SchemaVersion, SchemaVersion)
	}
	if manifest.Kind != "workspace" || manifest.WorkspaceID != f.workspaceID {
		t.Errorf("manifest scope = %q/%q, want workspace/%s", manifest.Kind, manifest.WorkspaceID, f.workspaceID)
	}
	if _, ok := a["manifest.json"]; !ok {
		t.Fatalf("archive has no manifest.json; entries: %v", a.names())
	}

	for _, tc := range []struct{ file, wantID string }{
		{"issues", f.issueID},
		{"comments", f.commentID},
		{"agents", f.agentID},
		{"chat_sessions", f.sessionID},
		{"chat_messages", f.messageID},
		{"mcp_servers", f.mcpID},
		{"members", ""},
	} {
		rows := a.rows(t, tc.file)
		if len(rows) == 0 {
			t.Errorf("data/%s.json is empty", tc.file)
			continue
		}
		if tc.wantID == "" {
			continue
		}
		found := false
		for _, row := range rows {
			if row["id"] == tc.wantID {
				found = true
			}
		}
		if !found {
			t.Errorf("data/%s.json does not contain %s", tc.file, tc.wantID)
		}
		if got := manifest.Counts[tc.file]; got != int64(len(rows)) {
			t.Errorf("manifest count for %s = %d, rows = %d", tc.file, got, len(rows))
		}
	}
}

func TestWorkspaceExportIsScopedToOneWorkspace(t *testing.T) {
	mine := newFixture(t, "mine")
	theirs := newFixture(t, "theirs")

	a, _ := exportWorkspace(t, mine.workspaceID)
	whole := bytes.Join([][]byte{}, nil)
	for _, body := range a {
		whole = append(whole, body...)
	}
	if bytes.Contains(whole, []byte(theirs.issueID)) {
		t.Error("the archive of one workspace contains an issue id from another workspace")
	}
	if bytes.Contains(whole, []byte("secret business text theirs")) {
		t.Error("the archive of one workspace contains issue text from another workspace")
	}
}

func TestWorkspaceExportCarriesNoCredentials(t *testing.T) {
	f := newFixture(t, "creds")
	a, manifest := exportWorkspace(t, f.workspaceID)

	whole := []byte{}
	for _, body := range a {
		whole = append(whole, body...)
	}
	for _, secret := range []string{
		"sk-must-not-appear-creds",
		"ghp-must-not-appear-creds",
		"sealed-must-not-appear-creds",
	} {
		if bytes.Contains(whole, []byte(secret)) {
			t.Errorf("the archive contains the credential %q", secret)
		}
	}

	agents := a.rows(t, "agents")
	for _, agent := range agents {
		if agent["id"] != f.agentID {
			continue
		}
		if _, present := agent["custom_env"]; present {
			t.Error("agents.json still carries custom_env")
		}
		if _, present := agent["mcp_config"]; present {
			t.Error("agents.json still carries mcp_config")
		}
	}

	seen := false
	for _, server := range a.rows(t, "mcp_servers") {
		if server["id"] != f.mcpID {
			continue
		}
		seen = true
		if _, present := server["config"]; present {
			t.Errorf("mcp_servers.json still carries the sealed config: %v", server["config"])
		}
		if server["name"] == nil {
			t.Error("mcp_servers.json lost the definition it is supposed to carry")
		}
	}
	if !seen {
		t.Error("mcp_servers.json does not contain the workspace's server")
	}

	if len(manifest.Redacted) == 0 {
		t.Error("manifest does not declare what it redacted")
	}
	declared := strings.Join(manifest.Redacted, " ")
	for _, want := range []string{"agents.custom_env", "agents.mcp_config", "mcp_servers.config"} {
		if !strings.Contains(declared, want) {
			t.Errorf("manifest.redacted does not mention %s: %v", want, manifest.Redacted)
		}
	}
}

func TestUserExportCarriesOnlyThatSubject(t *testing.T) {
	mine := newFixture(t, "subject")
	theirs := newFixture(t, "other")

	var buf bytes.Buffer
	manifest, err := WriteUser(context.Background(), testPool, mine.userID, &buf, Options{})
	if err != nil {
		t.Fatalf("WriteUser: %v", err)
	}
	if manifest.Kind != "user" || manifest.UserID != mine.userID {
		t.Errorf("manifest scope = %q/%q, want user/%s", manifest.Kind, manifest.UserID, mine.userID)
	}

	a := unpack(t, buf.Bytes())
	profile := a.rows(t, "profile")
	if len(profile) != 1 || profile[0]["id"] != mine.userID {
		t.Fatalf("profile.json = %v, want exactly the subject", profile)
	}
	if _, present := profile[0]["token_version"]; present {
		t.Error("profile.json carries token_version, the session epoch")
	}

	authored := a.rows(t, "issues_authored")
	if len(authored) != 1 || authored[0]["id"] != mine.issueID {
		t.Errorf("issues_authored = %v, want only %s", authored, mine.issueID)
	}

	whole := []byte{}
	for _, body := range a {
		whole = append(whole, body...)
	}
	if bytes.Contains(whole, []byte(theirs.userID)) {
		t.Error("a subject-access archive contains another subject's id")
	}
	if bytes.Contains(whole, []byte("secret business text other")) {
		t.Error("a subject-access archive contains another subject's content")
	}
}

func TestUserExportNeverCarriesTokenHashes(t *testing.T) {
	f := newFixture(t, "pat")
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix)
		 VALUES ($1, 'cli', $2, 'gsl_')`,
		f.userID, "hash-must-not-appear-pat"); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	var buf bytes.Buffer
	if _, err := WriteUser(ctx, testPool, f.userID, &buf, Options{}); err != nil {
		t.Fatalf("WriteUser: %v", err)
	}
	a := unpack(t, buf.Bytes())
	tokens := a.rows(t, "access_tokens")
	if len(tokens) != 1 {
		t.Fatalf("access_tokens = %v, want the one token", tokens)
	}
	if _, present := tokens[0]["token_hash"]; present {
		t.Error("access_tokens.json carries token_hash — a live credential verifier")
	}
	if tokens[0]["name"] != "cli" {
		t.Errorf("access_tokens lost its metadata: %v", tokens[0])
	}
}

func TestScrubReplacesCredentialKeysAtAnyDepth(t *testing.T) {
	input := map[string]any{
		"command": "npx",
		"args":    []any{"--flag", "value"},
		"env":     map[string]any{"API_KEY": "leak", "HOME": "/home/x"},
		"nested":  []any{map[string]any{"password": "leak"}},
		"count":   float64(3),
	}
	out, ok := Scrub(input).(map[string]any)
	if !ok {
		t.Fatalf("Scrub returned %T", Scrub(input))
	}
	if out["command"] != "npx" {
		t.Errorf("Scrub changed a non-credential value: %v", out["command"])
	}
	if out["count"] != float64(3) {
		t.Errorf("Scrub changed a number: %v", out["count"])
	}
	env := out["env"].(map[string]any)
	if env["API_KEY"] != RedactedPlaceholder {
		t.Errorf("env.API_KEY = %v, want redacted", env["API_KEY"])
	}
	if env["HOME"] != "/home/x" {
		t.Errorf("env.HOME = %v, want kept", env["HOME"])
	}
	nested := out["nested"].([]any)[0].(map[string]any)
	if nested["password"] != RedactedPlaceholder {
		t.Errorf("nested password = %v, want redacted", nested["password"])
	}
}

func TestSafeNameCannotEscapeTheArchive(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32`, "system32"},
		{"/absolute/path.txt", "path.txt"},
		{"", "file"},
		{"..", "file"},
		{"with\nnewline.txt", "with_newline.txt"},
	} {
		if got := safeName(tc.in); got != tc.want {
			t.Errorf("safeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := safeName(strings.Repeat("a", 500)); len(got) > 120 {
		t.Errorf("safeName did not bound the length: %d", len(got))
	}
}

func TestUserExportCarriesCorporateIdentityAndMFA(t *testing.T) {
	f := newFixture(t, "identity")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`INSERT INTO user_identity (user_id, provider, subject, email_at_link)
		 VALUES ($1, 'oidc', $2, $3)`,
		f.userID, "https://sso.example.test|subject-"+f.userID, "linked@example.test"); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO user_mfa (user_id, totp_secret_sealed, enabled_at)
		 VALUES ($1, $2, now())`, f.userID, []byte("sealed-must-not-appear")); err != nil {
		t.Fatalf("insert mfa: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO user_mfa_recovery_code (user_id, code_hash) VALUES ($1, $2)`,
		f.userID, "hash-must-not-appear-mfa"); err != nil {
		t.Fatalf("insert recovery code: %v", err)
	}

	var buf bytes.Buffer
	manifest, err := WriteUser(ctx, testPool, f.userID, &buf, Options{})
	if err != nil {
		t.Fatalf("WriteUser: %v", err)
	}
	a := unpack(t, buf.Bytes())

	identities := a.rows(t, "corporate_identities")
	if len(identities) != 1 || identities[0]["email_at_link"] != "linked@example.test" {
		t.Fatalf("corporate_identities = %v, want the subject's directory link", identities)
	}
	mfa := a.rows(t, "mfa")
	if len(mfa) != 1 {
		t.Fatalf("mfa = %v, want the enrollment row", mfa)
	}
	if _, present := mfa[0]["totp_secret_sealed"]; present {
		t.Error("mfa.json carries the sealed TOTP secret")
	}
	codes := a.rows(t, "mfa_recovery_codes")
	if len(codes) != 1 {
		t.Fatalf("mfa_recovery_codes = %v, want the one code", codes)
	}
	if _, present := codes[0]["code_hash"]; present {
		t.Error("mfa_recovery_codes.json carries the code hash")
	}

	declared := strings.Join(manifest.Redacted, " ")
	for _, want := range []string{"mfa.totp_secret_sealed", "mfa_recovery_codes.code_hash"} {
		if !strings.Contains(declared, want) {
			t.Errorf("manifest.redacted does not mention %s: %v", want, manifest.Redacted)
		}
	}
}
