package handler

import (
	"context"
	"net/http"
	"testing"
)

func TestSignupResolvesPendingDeploymentAdminSeed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)

	email := linkTestEmail(t, "seed-on-signup")
	t.Setenv(DeploymentAdminEmailsEnvVar, email)

	sender := sendCodeWithSpy(t, email, "")
	if w := postVerifyCode(t, email, sender.code); w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deployment_admin da JOIN "user" u ON u.id = da.user_id WHERE u.email = $1`,
		email).Scan(&count); err != nil {
		t.Fatalf("count seeded admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("the seed email signed up but holds no deployment role (rows = %d) — the deployment stays administrator-less until a restart", count)
	}
	if got := countAdminAuditRows(t, adminAuditActionBootstrapGrant); got != 1 {
		t.Fatalf("bootstrap_grant audit rows = %d, want 1", got)
	}
}

func TestSignupSeedDoesNotReopenAClosedRoleTable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	grantDeploymentAdminFixture(t, testUserID)

	email := linkTestEmail(t, "seed-closed-table")
	t.Setenv(DeploymentAdminEmailsEnvVar, email)

	sender := sendCodeWithSpy(t, email, "")
	if w := postVerifyCode(t, email, sender.code); w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("deployment_admin rows = %d, want 1 (the env list must not re-seed a non-empty table)", got)
	}
}
