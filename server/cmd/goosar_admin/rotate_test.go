package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/util/secretbox"
)

func randomKeyB64(t *testing.T) (string, []byte) {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key), key
}

func sealEnvelope(t *testing.T, key []byte, plaintext string) []byte {
	t.Helper()
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := box.Seal([]byte(plaintext))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	env, err := json.Marshal(map[string]string{"__goosar_sealed__": base64.StdEncoding.EncodeToString(sealed)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return env
}

func TestRotateSecrets_RefusesWithoutCurrentKey(t *testing.T) {
	t.Setenv("GOOSAR_MCP_SECRET_KEY", "")
	t.Setenv("GOOSAR_MCP_SECRET_KEY_PREVIOUS", "")
	var out bytes.Buffer
	err := run(context.Background(), []string{"rotate-secrets", "--mcp", "--dry-run"}, &out)
	if err == nil || !strings.Contains(err.Error(), "GOOSAR_MCP_SECRET_KEY") {
		t.Fatalf("rotation without a current key must refuse, got %v", err)
	}
}

func TestRotateSecrets_RequiresADomainFlag(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"rotate-secrets"}, &out); err == nil {
		t.Fatal("rotate-secrets without a domain flag must refuse")
	}
}

func TestRotateSecrets_ResealsMcpConfig(t *testing.T) {
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

	suffix := uuid.NewString()
	agentPrefix := "rotate-secrets-" + suffix
	defer func() {
		clean := context.Background()
		_, _ = pool.Exec(clean, `DELETE FROM agent WHERE name LIKE $1`, agentPrefix+"%")
		_, _ = pool.Exec(clean, `DELETE FROM agent_runtime WHERE name = $1`, "rotate-secrets-runtime-"+suffix)
		_, _ = pool.Exec(clean, `DELETE FROM workspace WHERE slug = $1`, "rotate-secrets-cli-ws-"+suffix)
	}()

	oldB64, oldKey := randomKeyB64(t)
	newB64, newKey := randomKeyB64(t)

	var wsID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ('Rotate WS', $1) RETURNING id`,
		"rotate-secrets-cli-ws-"+suffix).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		VALUES ($1, $2, 'local', 'claude_code')
		RETURNING id`, wsID, "rotate-secrets-runtime-"+suffix).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	stale := sealEnvelope(t, oldKey, `{"servers":{"jira":{"token":"secret-pat"}}}`)
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, mcp_config)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id`, wsID, agentPrefix+"-agent", runtimeID, stale).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT mcp_config FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read stored: %v", err)
	}

	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("GOOSAR_MCP_SECRET_KEY", newB64)
	t.Setenv("GOOSAR_MCP_SECRET_KEY_PREVIOUS", oldB64)

	var dry bytes.Buffer
	if err := runIgnoringForeignUnreadable(ctx, []string{"rotate-secrets", "--mcp", "--dry-run"}, &dry); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(dry.String(), "dry run") {
		t.Fatalf("dry run output does not say so: %s", dry.String())
	}
	var afterDry []byte
	if err := pool.QueryRow(ctx, `SELECT mcp_config FROM agent WHERE id = $1`, agentID).Scan(&afterDry); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(afterDry, stored) {
		t.Fatal("dry run rewrote the row")
	}

	auditBefore := countRotateAudits(ctx, t, pool)

	var buf bytes.Buffer
	if err := runIgnoringForeignUnreadable(ctx, []string{"rotate-secrets", "--mcp"}, &buf); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	var rotated []byte
	if err := pool.QueryRow(ctx, `SELECT mcp_config FROM agent WHERE id = $1`, agentID).Scan(&rotated); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if bytes.Equal(rotated, stored) {
		t.Fatal("row was not re-sealed")
	}

	newOnly, _ := secretbox.New(newKey)
	var env map[string]string
	if err := json.Unmarshal(rotated, &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(env["__goosar_sealed__"])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	plaintext, err := newOnly.Open(raw)
	if err != nil {
		t.Fatalf("re-sealed row does not open under the current key: %v", err)
	}
	if !strings.Contains(string(plaintext), "secret-pat") {
		t.Fatalf("plaintext lost in rotation: %s", plaintext)
	}

	oldOnly, _ := secretbox.New(oldKey)
	if _, err := oldOnly.Open(raw); err == nil {
		t.Fatal("re-sealed row still opens under the retired key")
	}

	var second bytes.Buffer
	if err := runIgnoringForeignUnreadable(ctx, []string{"rotate-secrets", "--mcp"}, &second); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if strings.Contains(second.String(), agentID+": mcp_config resealed") {
		t.Fatalf("second pass re-sealed an already-current row: %s", second.String())
	}

	var afterSecond []byte
	if err := pool.QueryRow(ctx, `SELECT mcp_config FROM agent WHERE id = $1`, agentID).Scan(&afterSecond); err != nil {
		t.Fatalf("read back after second pass: %v", err)
	}
	if !bytes.Equal(afterSecond, rotated) {
		t.Fatal("second pass rewrote an already-current row")
	}

	_, strangerKey := randomKeyB64(t)
	orphan := sealEnvelope(t, strangerKey, `{"servers":{"gitlab":{"token":"lost"}}}`)
	var orphanID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, mcp_config)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id`, wsID, agentPrefix+"-orphan", runtimeID, orphan).Scan(&orphanID); err != nil {
		t.Fatalf("create orphan agent: %v", err)
	}
	var orphanStored []byte
	if err := pool.QueryRow(ctx, `SELECT mcp_config FROM agent WHERE id = $1`, orphanID).Scan(&orphanStored); err != nil {
		t.Fatalf("read orphan: %v", err)
	}
	var third bytes.Buffer
	if err := run(ctx, []string{"rotate-secrets", "--mcp"}, &third); err == nil {
		t.Fatalf("a pass that left undecryptable rows exited 0: %s", third.String())
	}

	if !strings.Contains(third.String(), orphanID+": mcp_config unreadable") {
		t.Fatalf("the orphan row was not reported as unreadable: %s", third.String())
	}
	var orphanAfter []byte
	if err := pool.QueryRow(ctx, `SELECT mcp_config FROM agent WHERE id = $1`, orphanID).Scan(&orphanAfter); err != nil {
		t.Fatalf("read orphan back: %v", err)
	}
	if !bytes.Equal(orphanAfter, orphanStored) {
		t.Fatal("an unreadable row was overwritten")
	}

	if after := countRotateAudits(ctx, t, pool); after <= auditBefore {
		t.Fatalf("no admin_audit row for the completed rotation: %d before, %d after", auditBefore, after)
	}
}

func countRotateAudits(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM admin_audit WHERE action = 'secret.rotate' AND target_id = 'mcp'`,
	).Scan(&n); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	return n
}

func runIgnoringForeignUnreadable(ctx context.Context, args []string, out *bytes.Buffer) error {
	err := run(ctx, args, out)
	if err != nil && strings.Contains(err.Error(), "could not be opened by any key in the ring") {
		return nil
	}
	return err
}

func TestRotateSecretsNamesTheEstatesItDoesNotCover(t *testing.T) {
	if !strings.Contains(uncoveredMcpEstatesWarning, "workspace_mcp_server.config") ||
		!strings.Contains(uncoveredMcpEstatesWarning, "GOOSAR_MCP_SECRET_KEY_PREVIOUS") {
		t.Fatal("the warning must name an uncovered column and the variable that must not be emptied")
	}
}
