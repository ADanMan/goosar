// integration_test.go — прогоняет каждую команду goosar_admin против
// одноразовой базы на локальном Postgres (тот же приём, что
// internal/migrate.TestApply_isIdempotentAndSequential: своя БД с меткой
// времени в имени, миграции применяются один раз, БД удаляется по
// завершении). Пропускается в -short и когда локальный Postgres недоступен.
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/deployment"
	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/seal"
	"github.com/adanman/goosar/server2/internal/store"
)

func setupTestDB(t *testing.T) *store.Store {
	t.Helper()
	if testing.Short() {
		t.Skip("нужен живой Postgres, пропущено в -short")
	}
	adminURL := "postgres://postgres@localhost:5432/postgres"
	dbName := "goosar2_admin_cli_test_" + time.Now().UTC().Format("20060102150405999999")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("нет доступа к локальному Postgres: %v", err)
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Skipf("нет доступа к локальному Postgres: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(cctx, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
	})

	dsn := adminURL[:len(adminURL)-len("/postgres")] + "/" + dbName
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := migrate.Apply(ctx, pool, "../../migrations"); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return &store.Store{Pool: pool}
}

func insertAccount(t *testing.T, db *store.Store, email string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(), `
		INSERT INTO accounts (acct_email, acct_full_name) VALUES ($1, $2) RETURNING id`, email, email).Scan(&id)
	if err != nil {
		t.Fatalf("insertAccount(%s): %v", email, err)
	}
	return id
}

func TestIntegration_AdminsLifecycle(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	var out testBuf

	alice := insertAccount(t, db, "alice@example.test")
	bob := insertAccount(t, db, "bob@example.test")
	carol := insertAccount(t, db, "carol@example.test")

	if err := runGrant(ctx, &out, db, "alice@example.test"); err != nil {
		t.Fatalf("runGrant(alice): %v", err)
	}
	out = testBuf{}
	if err := runGrant(ctx, &out, db, "bob@example.test"); err != nil {
		t.Fatalf("runGrant(bob): %v", err)
	}
	isAdmin, err := deployment.IsAdmin(ctx, db, alice)
	if err != nil || !isAdmin {
		t.Fatalf("alice should be deployment-admin after grant: isAdmin=%v err=%v", isAdmin, err)
	}

	// Повторная выдача уже-admin — no-op, но не ошибка.
	out = testBuf{}
	if err := runGrant(ctx, &out, db, "alice@example.test"); err != nil {
		t.Fatalf("runGrant(alice) again: %v", err)
	}

	// Заявка на отзыв роли bob, поданная как будто через API alice.
	audit := deployment.AuditWrite{Source: "admin", Action: "deployment_admin.revoke.requested"}
	pending, err := deployment.CreatePendingRequest(ctx, db, "revoke", &bob, nil, alice, audit)
	if err != nil {
		t.Fatalf("CreatePendingRequest(revoke bob): %v", err)
	}

	out = testBuf{}
	if err := runListPending(ctx, &out, db); err != nil {
		t.Fatalf("runListPending: %v", err)
	}
	if !strings.Contains(out.String(), pending.RequestID) {
		t.Errorf("runListPending output = %q, want to contain request id %s", out.String(), pending.RequestID)
	}

	out = testBuf{}
	if err := runConfirm(ctx, &out, db, pending.RequestID); err != nil {
		t.Fatalf("runConfirm(%s): %v", pending.RequestID, err)
	}
	isAdmin, err = deployment.IsAdmin(ctx, db, bob)
	if err != nil || isAdmin {
		t.Fatalf("bob should no longer be deployment-admin after confirm: isAdmin=%v err=%v", isAdmin, err)
	}

	// Заявка на выдачу роли carol, потом отклонена — carol не должна стать admin.
	pending2, err := deployment.CreatePendingRequest(ctx, db, "grant", &carol, nil, alice, deployment.AuditWrite{Source: "admin", Action: "deployment_admin.grant.requested"})
	if err != nil {
		t.Fatalf("CreatePendingRequest(grant carol): %v", err)
	}
	out = testBuf{}
	if err := runReject(ctx, &out, db, pending2.RequestID); err != nil {
		t.Fatalf("runReject: %v", err)
	}
	isAdmin, err = deployment.IsAdmin(ctx, db, carol)
	if err != nil || isAdmin {
		t.Fatalf("carol should not be deployment-admin after reject: isAdmin=%v err=%v", isAdmin, err)
	}
}

func TestIntegration_MfaReset(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	acctID := insertAccount(t, db, "mfa-user@example.test")

	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO mfa_factors (account_id, mfa_secret_sealed, mfa_enabled_at) VALUES ($1, $2, now())`,
		acctID, []byte("fake-sealed-secret")); err != nil {
		t.Fatalf("insert mfa_factors: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO login_sessions (account_id, sess_secret_digest) VALUES ($1, 'digest')`, acctID); err != nil {
		t.Fatalf("insert login_sessions: %v", err)
	}

	var out testBuf
	if err := runMfaReset(ctx, &out, db, authn.NewStore(db), "mfa-user@example.test"); err != nil {
		t.Fatalf("runMfaReset: %v", err)
	}

	var factors int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM mfa_factors WHERE account_id = $1`, acctID).Scan(&factors); err != nil {
		t.Fatalf("count mfa_factors: %v", err)
	}
	if factors != 0 {
		t.Errorf("mfa_factors count = %d, want 0", factors)
	}
	var liveSessions int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM login_sessions WHERE account_id = $1 AND sess_invalidated_at IS NULL`, acctID).Scan(&liveSessions); err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	if liveSessions != 0 {
		t.Errorf("live sessions = %d, want 0 (all revoked)", liveSessions)
	}
}

func TestIntegration_GCUploadsDryRunDoesNotDelete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	wsID := insertWorkspace(t, db, "gc-uploads-ws")

	var assetID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO assets (workspace_id, as_uploader_type, as_uploader_id, as_filename, as_storage_uri,
			as_download_path, as_markdown_ref, as_content_type, as_size_bytes, created_at)
		VALUES ($1, 'member', $1, 'old.txt', 'local://ws/old.txt', '/uploads/old.txt', '[old.txt](/uploads/old.txt)', 'text/plain', 10, now() - interval '1000 hours')
		RETURNING id`, wsID).Scan(&assetID)
	if err != nil {
		t.Fatalf("insert orphaned asset: %v", err)
	}

	var out testBuf
	cfg := config.Config{}
	if err := runGCUploads(ctx, &out, db, cfg, gcUploadsOptions{dryRun: true, grace: time.Hour, limit: 500}); err != nil {
		t.Fatalf("runGCUploads(dry-run): %v", err)
	}
	if !strings.Contains(out.String(), assetID) {
		t.Errorf("gc-uploads --dry-run output = %q, want to list asset %s", out.String(), assetID)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM assets WHERE id = $1`, assetID).Scan(&count); err != nil {
		t.Fatalf("count assets: %v", err)
	}
	if count != 1 {
		t.Errorf("--dry-run must not delete rows; assets count = %d, want 1", count)
	}
}

func TestIntegration_PurgeDryRunDoesNotDelete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	wsID := insertWorkspace(t, db, "purge-ws")

	var ticketID string
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO tickets (workspace_id, tk_headline, tk_status, tk_seq_number, tk_display_key,
			tk_creator_type, tk_creator_id, updated_at)
		VALUES ($1, 'old closed ticket', 'done', 1, 'PRG-1', 'member', $1, now() - interval '10000 hours')
		RETURNING id`, wsID).Scan(&ticketID)
	if err != nil {
		t.Fatalf("insert closed ticket: %v", err)
	}

	var out testBuf
	opts := purgeOptions{dryRun: true, chat: 720 * time.Hour, tasks: 720 * time.Hour, closedIssues: time.Hour, activity: 720 * time.Hour, attachmentGrace: 168 * time.Hour}
	if err := runPurge(ctx, &out, db, opts); err != nil {
		t.Fatalf("runPurge(dry-run): %v", err)
	}
	if !strings.Contains(out.String(), "closed-issues") {
		t.Errorf("purge --dry-run output = %q, want to mention closed-issues", out.String())
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE id = $1`, ticketID).Scan(&count); err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	if count != 1 {
		t.Errorf("--dry-run must not delete rows; tickets count = %d, want 1", count)
	}
}

func TestIntegration_ProvisionRolesIdempotent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	admin := insertAccount(t, db, "owner@example.test")
	var out testBuf
	if err := runGrant(ctx, &out, db, "owner@example.test"); err != nil {
		t.Fatalf("runGrant: %v", err)
	}
	_ = admin

	cfg := config.Config{RoleWorkspaces: "support"}
	out = testBuf{}
	if err := runProvisionRoles(ctx, &out, db, cfg); err != nil {
		t.Fatalf("runProvisionRoles (1st): %v", err)
	}
	if !strings.Contains(out.String(), "создано") {
		t.Errorf("first provision-roles run output = %q, want to report a created workspace", out.String())
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM spaces WHERE ws_template_key = 'support'`).Scan(&count); err != nil {
		t.Fatalf("count spaces: %v", err)
	}
	if count != 1 {
		t.Fatalf("spaces with ws_template_key=support = %d, want 1", count)
	}

	out = testBuf{}
	if err := runProvisionRoles(ctx, &out, db, cfg); err != nil {
		t.Fatalf("runProvisionRoles (2nd): %v", err)
	}
	if !strings.Contains(out.String(), "пропущено") {
		t.Errorf("second provision-roles run output = %q, want to report skipped", out.String())
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM spaces WHERE ws_template_key = 'support'`).Scan(&count); err != nil {
		t.Fatalf("count spaces (2nd): %v", err)
	}
	if count != 1 {
		t.Fatalf("re-running provision-roles must be idempotent; spaces with ws_template_key=support = %d, want 1", count)
	}
}

func TestIntegration_RotateSecretsDryRunDoesNotWrite(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cfg := config.Config{McpSecretKey: "test-mcp-secret-key-0123456789ab"}

	sealed, err := seal.SealJSON(cfg.McpSecretKey, map[string]any{"url": "https://mcp.example.test"})
	if err != nil {
		t.Fatalf("seal.SealJSON: %v", err)
	}
	var id string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO platform_mcp_servers (pmcp_name, pmcp_transport, pmcp_config_sealed) VALUES ('rotate-test', 'http', $1) RETURNING id`,
		sealed).Scan(&id); err != nil {
		t.Fatalf("insert platform_mcp_servers: %v", err)
	}

	var out testBuf
	if err := runRotateSecrets(ctx, &out, db, cfg, rotateSecretsOptions{mcp: true, dryRun: true}); err != nil {
		t.Fatalf("runRotateSecrets(dry-run): %v", err)
	}
	var after []byte
	if err := db.Pool.QueryRow(ctx, `SELECT pmcp_config_sealed FROM platform_mcp_servers WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("read back sealed value: %v", err)
	}
	if string(after) != string(sealed) {
		t.Errorf("--dry-run must not rewrite sealed values")
	}
}

func TestIntegration_McpLibrarySeedDryRunDoesNotWrite(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cfg := config.Config{McpSecretKey: "test-mcp-secret-key-0123456789ab", DeploymentJiraURL: "https://jira.example.test"}

	var out testBuf
	if err := runMcpLibrarySeed(ctx, &out, db, cfg, mcpLibrarySeedOptions{dryRun: true}); err != nil {
		t.Fatalf("runMcpLibrarySeed(dry-run): %v", err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM platform_mcp_servers WHERE pmcp_name = 'jira'`).Scan(&count); err != nil {
		t.Fatalf("count platform_mcp_servers: %v", err)
	}
	if count != 0 {
		t.Errorf("--dry-run must not create rows; count = %d, want 0", count)
	}

	// Без --dry-run — идемпотентно по имени.
	out = testBuf{}
	if err := runMcpLibrarySeed(ctx, &out, db, cfg, mcpLibrarySeedOptions{}); err != nil {
		t.Fatalf("runMcpLibrarySeed: %v", err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM platform_mcp_servers WHERE pmcp_name = 'jira'`).Scan(&count); err != nil {
		t.Fatalf("count platform_mcp_servers after seed: %v", err)
	}
	if count != 1 {
		t.Fatalf("count after seed = %d, want 1", count)
	}
	out = testBuf{}
	if err := runMcpLibrarySeed(ctx, &out, db, cfg, mcpLibrarySeedOptions{}); err != nil {
		t.Fatalf("runMcpLibrarySeed (2nd): %v", err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM platform_mcp_servers WHERE pmcp_name = 'jira'`).Scan(&count); err != nil {
		t.Fatalf("count platform_mcp_servers after 2nd seed: %v", err)
	}
	if count != 1 {
		t.Fatalf("re-running mcp-library seed must be idempotent by name; count = %d, want 1", count)
	}
}

func insertWorkspace(t *testing.T, db *store.Store, slug string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(), `
		INSERT INTO spaces (ws_title, ws_slug, ws_ticket_prefix) VALUES ($1, $2, 'TST') RETURNING id`, slug, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insertWorkspace(%s): %v", slug, err)
	}
	return id
}

// TestIntegration_RotateSecretsCoversAgentAutopilotAndMfa — T-029 доводка:
// до неё rotate-secrets --mcp касался только четырёх таблиц domain deployment
// (platform_mcp_servers/space_mcp_servers/space_mcp_credentials/space_config)
// — operatives.op_*_sealed (agent) и sentinel_triggers.strig_signing_secret_sealed
// (autopilot) были запечатаны собственными копиями AES-256-GCM тех пакетов,
// не internal/seal, и оставались вне охвата команды (см.
// server2/docs/decisions.md, раздел T-029, «rotate-secrets --mcp»). Доводка
// T-029 перевела agent/autopilot на internal/seal — этот тест проверяет, что
// rotate-secrets теперь и правда перешифровывает их значения новым ключом (и
// что --mfa делает то же для mfa_factors), а данные остаются читаемыми.
func TestIntegration_RotateSecretsCoversAgentAutopilotAndMfa(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	oldKey := "old-mcp-secret-key-0123456789ab"
	newKey := "new-mcp-secret-key-fedcba987654"
	cfg := config.Config{McpSecretKey: newKey, McpSecretKeyPrevious: oldKey}

	wsID := insertWorkspace(t, db, "rotate-agent-ws")
	var executorID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO executors (workspace_id, ex_title, ex_mode, ex_provider)
		VALUES ($1, 'Rotate test runtime', 'local', 'claude') RETURNING id`, wsID).Scan(&executorID); err != nil {
		t.Fatalf("insert executor: %v", err)
	}

	runtimeSealed, err := seal.Seal(oldKey, []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("seal.Seal(runtime): %v", err)
	}
	mcpSealed, err := seal.Seal(oldKey, []byte(`{"mcp":true}`))
	if err != nil {
		t.Fatalf("seal.Seal(mcp): %v", err)
	}
	envSealed, err := seal.Seal(oldKey, []byte(`{"K":"v"}`))
	if err != nil {
		t.Fatalf("seal.Seal(env): %v", err)
	}
	var agentID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO operatives (workspace_id, executor_id, op_title, op_runtime_mode,
			op_runtime_config_sealed, op_mcp_config_sealed, op_custom_env_sealed)
		VALUES ($1, $2, 'Rotate test agent', 'local', $3, $4, $5) RETURNING id`,
		wsID, executorID, runtimeSealed, mcpSealed, envSealed).Scan(&agentID); err != nil {
		t.Fatalf("insert operative: %v", err)
	}

	var sentinelID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO sentinels (workspace_id, sen_title, sen_assignee_type, sen_assignee_id,
			sen_execution_mode, sen_created_by_type, sen_created_by_id)
		VALUES ($1, 'Rotate test autopilot', 'agent', $2, 'run_only', 'member', $2) RETURNING id`,
		wsID, agentID).Scan(&sentinelID); err != nil {
		t.Fatalf("insert sentinel: %v", err)
	}
	signingSealed, err := seal.Seal(oldKey, []byte("webhook-signing-secret"))
	if err != nil {
		t.Fatalf("seal.Seal(signing): %v", err)
	}
	var triggerID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO sentinel_triggers (sentinel_id, strig_kind, strig_signing_secret_sealed)
		VALUES ($1, 'webhook', $2) RETURNING id`, sentinelID, signingSealed).Scan(&triggerID); err != nil {
		t.Fatalf("insert sentinel_trigger: %v", err)
	}

	accountID := insertAccount(t, db, "rotate-mfa@example.test")
	mfaSealed, err := seal.Seal(oldKey, []byte("totp-secret-bytes"))
	if err != nil {
		t.Fatalf("seal.Seal(mfa): %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO mfa_factors (account_id, mfa_secret_sealed) VALUES ($1, $2)`,
		accountID, mfaSealed); err != nil {
		t.Fatalf("insert mfa_factors: %v", err)
	}

	var out testBuf
	if err := runRotateSecrets(ctx, &out, db, cfg, rotateSecretsOptions{mcp: true, mfa: true}); err != nil {
		t.Fatalf("runRotateSecrets: %v", err)
	}

	assertRotated := func(table, idCol, sealedCol, id string, want string) {
		t.Helper()
		var got []byte
		if err := db.Pool.QueryRow(ctx,
			"SELECT "+sealedCol+" FROM "+table+" WHERE "+idCol+" = $1", id).Scan(&got); err != nil {
			t.Fatalf("read back %s.%s: %v", table, sealedCol, err)
		}
		// перешифровано новым ключом — открывается без обращения к prevKey.
		plain, ok := seal.Open(newKey, "", got)
		if !ok || string(plain) != want {
			t.Errorf("%s.%s: содержимое после ротации = %q, want %q", table, sealedCol, plain, want)
		}
	}
	assertRotated("operatives", "id", "op_runtime_config_sealed", agentID, `{"a":1}`)
	assertRotated("operatives", "id", "op_mcp_config_sealed", agentID, `{"mcp":true}`)
	assertRotated("operatives", "id", "op_custom_env_sealed", agentID, `{"K":"v"}`)
	assertRotated("sentinel_triggers", "id", "strig_signing_secret_sealed", triggerID, "webhook-signing-secret")
	assertRotated("mfa_factors", "account_id", "mfa_secret_sealed", accountID, "totp-secret-bytes")
}
