package handler

import (
	"context"
	"crypto/rand"
	"strings"
	"sync"
	"testing"

	"github.com/adanman/goosar/server/internal/util/secretbox"
)

func testSecretBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random key: %v", err)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	return box
}

const roleWorkspaceTestLock int64 = 48400484

func lockRoleWorkspaceSingleton(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire role-workspace guard connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, roleWorkspaceTestLock); err != nil {
		conn.Release()
		t.Fatalf("acquire role workspace guard: %v", err)
	}
	t.Cleanup(func() {

		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, roleWorkspaceTestLock); err != nil {
			t.Logf("release role workspace guard: %v", err)
		}
		conn.Release()
	})
}

func cleanupRoleWorkspaces(t *testing.T) {
	t.Helper()
	clear := func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM workspace WHERE template_key IS NOT NULL`)
	}
	clear()
	t.Cleanup(clear)
}

func enabledTemplateCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_template WHERE enabled = true`).Scan(&count); err != nil {
		t.Fatalf("count workspace_template: %v", err)
	}
	return count
}

func TestParseRoleWorkspacesMode(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"", RoleWorkspacesAuto},
		{"  ", RoleWorkspacesAuto},
		{"auto", RoleWorkspacesAuto},
		{" AUTO ", RoleWorkspacesAuto},
		{"off", RoleWorkspacesOff},
		{"Off", RoleWorkspacesOff},
	} {
		got, err := ParseRoleWorkspacesMode(tc.raw)
		if err != nil {
			t.Fatalf("ParseRoleWorkspacesMode(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseRoleWorkspacesMode(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
	for _, raw := range []string{"false", "true", "on", "1", "disabled"} {
		_, err := ParseRoleWorkspacesMode(raw)
		if err == nil {
			t.Fatalf("ParseRoleWorkspacesMode(%q) accepted an unknown value", raw)
		}
		if !strings.Contains(err.Error(), RoleWorkspacesEnvVar) {
			t.Fatalf("error for %q does not name %s: %v", raw, RoleWorkspacesEnvVar, err)
		}
	}
}

func TestProvisionRoleWorkspaces_CreatesThenSkips(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	t.Setenv("ALLOWED_EMAIL_DOMAINS", "example.test")

	ownerID, _ := deploymentUserFixture(t, "role-owner")
	grantDeploymentAdminFixture(t, ownerID)

	laterID, _ := deploymentUserFixture(t, "role-later")
	grantDeploymentAdminFixture(t, laterID)

	want := enabledTemplateCount(t)
	if want == 0 {
		t.Fatal("no enabled workspace_template rows — the seeded role catalog is missing")
	}

	result, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if result.Deferred != "" {
		t.Fatalf("first run deferred: %s", result.Deferred)
	}
	if result.Created != want || result.Skipped != 0 || result.Errors != 0 {
		t.Fatalf("first run = created %d / skipped %d / errors %d, want created %d",
			result.Created, result.Skipped, result.Errors, want)
	}
	if len(result.Outcomes) != want {
		t.Fatalf("first run reported %d outcomes, want %d", len(result.Outcomes), want)
	}

	var rows int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL`).Scan(&rows); err != nil {
		t.Fatalf("count role workspaces: %v", err)
	}
	if rows != want {
		t.Fatalf("role workspaces in the database = %d, want %d", rows, want)
	}

	var owners int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM member m JOIN workspace w ON w.id = m.workspace_id
		 WHERE w.template_key IS NOT NULL AND m.user_id = $1 AND m.role = 'owner'`, ownerID).Scan(&owners); err != nil {
		t.Fatalf("count owners: %v", err)
	}
	if owners != want {
		t.Fatalf("owner memberships = %d, want %d (owner must be the FIRST deployment admin)", owners, want)
	}
	var helperDefaults int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_helper_default d JOIN workspace w ON w.id = d.workspace_id
		 WHERE w.template_key IS NOT NULL`).Scan(&helperDefaults); err != nil {
		t.Fatalf("count helper defaults: %v", err)
	}
	if helperDefaults != want {
		t.Fatalf("helper defaults = %d, want %d (applyWorkspaceTemplate did not run)", helperDefaults, want)
	}
	if got := countAdminAuditRows(t, adminAuditActionRoleWorkspaceProvisioned); got != want {
		t.Fatalf("provisioned audit rows = %d, want %d", got, want)
	}

	var open int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL AND open_join = true`).Scan(&open); err != nil {
		t.Fatalf("count open roles: %v", err)
	}
	if open != want {
		t.Fatalf("open_join role workspaces = %d, want %d", open, want)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET open_join = false WHERE template_key IS NOT NULL`); err != nil {
		t.Fatalf("close roles: %v", err)
	}

	second, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.Created != 0 || second.Skipped != want || second.Errors != 0 {
		t.Fatalf("second run = created %d / skipped %d / errors %d, want created 0 / skipped %d",
			second.Created, second.Skipped, second.Errors, want)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL`).Scan(&rows); err != nil {
		t.Fatalf("count role workspaces: %v", err)
	}
	if rows != want {
		t.Fatalf("second run changed the workspace count to %d, want %d", rows, want)
	}
	if got := countAdminAuditRows(t, adminAuditActionRoleWorkspaceSkipped); got != want {
		t.Fatalf("skipped audit rows = %d, want %d", got, want)
	}
	if got := countAdminAuditRows(t, adminAuditActionRoleWorkspaceProvisioned); got != want {
		t.Fatalf("provisioned audit rows after the second run = %d, want still %d", got, want)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL AND open_join = true`).Scan(&open); err != nil {
		t.Fatalf("count open roles after the second run: %v", err)
	}
	if open != 0 {
		t.Fatalf("the rerun reopened %d closed role(s); open_join is set on creation only", open)
	}
}

func TestProvisionRoleWorkspaces_ConcurrentRuns(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	ownerID, _ := deploymentUserFixture(t, "role-race")
	grantDeploymentAdminFixture(t, ownerID)
	want := enabledTemplateCount(t)
	if want == 0 {
		t.Fatal("no enabled workspace_template rows")
	}

	var wg sync.WaitGroup
	results := make([]RoleWorkspaceProvisionResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = ProvisionRoleWorkspaces(
				context.Background(), testPool, testHandler.Queries, testSecretBox(t))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent run %d failed: %v", i, err)
		}
		if results[i].Errors != 0 {
			t.Fatalf("concurrent run %d reported %d errors: %+v",
				i, results[i].Errors, results[i].Outcomes)
		}
	}
	if total := results[0].Created + results[1].Created; total != want {
		t.Fatalf("the two runs created %d workspaces in total, want %d", total, want)
	}
	var rows int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL`).Scan(&rows); err != nil {
		t.Fatalf("count role workspaces: %v", err)
	}
	if rows != want {
		t.Fatalf("role workspaces after two concurrent runs = %d, want %d", rows, want)
	}
}

func TestProvisionRoleWorkspaces_NoDeploymentAdmin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	result, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("run without administrators returned an error: %v", err)
	}
	if result.Deferred == "" {
		t.Fatal("run without administrators did not report a reason")
	}
	if result.Created != 0 || result.Skipped != 0 || result.Errors != 0 {
		t.Fatalf("deferred run = created %d / skipped %d / errors %d, want all zero",
			result.Created, result.Skipped, result.Errors)
	}
	var rows int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL`).Scan(&rows); err != nil {
		t.Fatalf("count role workspaces: %v", err)
	}
	if rows != 0 {
		t.Fatalf("deferred run created %d workspaces, want 0", rows)
	}

	ownerID, _ := deploymentUserFixture(t, "role-late-admin")
	grantDeploymentAdminFixture(t, ownerID)
	retry, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if retry.Created != enabledTemplateCount(t) {
		t.Fatalf("retry created %d, want %d", retry.Created, enabledTemplateCount(t))
	}
}

func TestProvisionRoleWorkspaces_SecretKeyUnset(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	ownerID, _ := deploymentUserFixture(t, "role-nokey")
	grantDeploymentAdminFixture(t, ownerID)

	_, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, nil)
	if err == nil {
		t.Fatal("run without a secret key succeeded")
	}
	if !strings.Contains(err.Error(), "GOOSAR_MCP_SECRET_KEY") {
		t.Fatalf("error does not name GOOSAR_MCP_SECRET_KEY: %v", err)
	}
	var rows int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL`).Scan(&rows); err != nil {
		t.Fatalf("count role workspaces: %v", err)
	}
	if rows != 0 {
		t.Fatalf("keyless run created %d workspaces, want 0", rows)
	}
	if got := countAdminAuditRows(t, adminAuditActionRoleWorkspaceProvisioned); got != 0 {
		t.Fatalf("keyless run wrote %d audit rows, want 0", got)
	}
}

func TestProvisionRoleWorkspaces_ClosedWhenRegistrationIsUnbounded(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)
	t.Setenv("ALLOW_SIGNUP", "true")
	t.Setenv("ALLOWED_EMAILS", "")
	t.Setenv("ALLOWED_EMAIL_DOMAINS", "")

	ownerID, _ := deploymentUserFixture(t, "role-owner-open")
	grantDeploymentAdminFixture(t, ownerID)

	if _, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t)); err != nil {
		t.Fatalf("provision: %v", err)
	}

	var open int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE template_key IS NOT NULL AND open_join = true`).Scan(&open); err != nil {
		t.Fatalf("count open roles: %v", err)
	}
	if open != 0 {
		t.Fatalf("open_join role workspaces = %d, want 0 while registration is unbounded", open)
	}
}
