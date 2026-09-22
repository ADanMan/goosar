package handler

import (
	"context"
	"testing"
)

func countDeploymentAdminRows(t *testing.T) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_admin`).Scan(&count); err != nil {
		t.Fatalf("count deployment_admin: %v", err)
	}
	return count
}

func TestSeedDeploymentAdmins_SeedsOnlyWhenEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)

	aID, aEmail := deploymentUserFixture(t, "seed-a")
	bID, bEmail := deploymentUserFixture(t, "seed-b")
	cID, cEmail := deploymentUserFixture(t, "seed-c")

	raw := " " + toUpperASCII(aEmail) + " , nobody-" + randomID()[:8] + "@goosar.test, " + bEmail + " "
	if err := SeedDeploymentAdmins(context.Background(), testPool, testHandler.Queries, raw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := countDeploymentAdminRows(t); got != 2 {
		t.Fatalf("seeded rows = %d, want 2", got)
	}
	for _, id := range []string{aID, bID} {
		var grantedBy *string
		if err := testPool.QueryRow(context.Background(),
			`SELECT granted_by::text FROM deployment_admin WHERE user_id = $1`, id).Scan(&grantedBy); err != nil {
			t.Fatalf("seeded row for %s missing: %v", id, err)
		}
		if grantedBy != nil {
			t.Fatalf("bootstrap grant has granted_by = %v, want NULL (system)", *grantedBy)
		}
	}
	if got := countAdminAuditRows(t, "deployment_admin.bootstrap_grant"); got != 2 {
		t.Fatalf("bootstrap audit rows = %d, want 2", got)
	}

	if err := SeedDeploymentAdmins(context.Background(), testPool, testHandler.Queries, cEmail); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if got := countDeploymentAdminRows(t); got != 2 {
		t.Fatalf("rows after non-empty-table seed = %d, want still 2", got)
	}
	var cIsAdmin int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_admin WHERE user_id = $1`, cID).Scan(&cIsAdmin); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cIsAdmin != 0 {
		t.Fatalf("env list granted a role over a non-empty table")
	}

	if err := SeedDeploymentAdmins(context.Background(), testPool, testHandler.Queries, raw); err != nil {
		t.Fatalf("repeat seed: %v", err)
	}
	if got := countDeploymentAdminRows(t); got != 2 {
		t.Fatalf("rows after repeat seed = %d, want 2", got)
	}
	if got := countAdminAuditRows(t, "deployment_admin.bootstrap_grant"); got != 2 {
		t.Fatalf("bootstrap audit rows after repeat = %d, want still 2", got)
	}
}

func TestSeedDeploymentAdmins_RetriesWhileEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)

	pendingEmail := "deploy-pending-" + randomID()[:8] + "@goosar.test"
	if err := SeedDeploymentAdmins(context.Background(), testPool, testHandler.Queries, pendingEmail); err != nil {
		t.Fatalf("seed with unresolvable email: %v", err)
	}
	if got := countDeploymentAdminRows(t); got != 0 {
		t.Fatalf("rows = %d, want 0 (nothing resolvable yet)", got)
	}

	var userID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ('Pending Admin', $1) RETURNING id`,
		pendingEmail).Scan(&userID); err != nil {
		t.Fatalf("create pending user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if err := SeedDeploymentAdmins(context.Background(), testPool, testHandler.Queries, pendingEmail); err != nil {
		t.Fatalf("retry seed: %v", err)
	}
	if got := countDeploymentAdminRows(t); got != 1 {
		t.Fatalf("rows after retry = %d, want 1", got)
	}
}

func TestSeedDeploymentAdmins_EmptyEnvIsNoop(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	if err := SeedDeploymentAdmins(context.Background(), testPool, testHandler.Queries, "   "); err != nil {
		t.Fatalf("seed with empty env: %v", err)
	}
	if got := countDeploymentAdminRows(t); got != 0 {
		t.Fatalf("rows = %d, want 0", got)
	}
}
