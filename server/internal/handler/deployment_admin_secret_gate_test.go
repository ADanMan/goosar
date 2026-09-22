package handler

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestRequireSecretKeyForAdminSurface(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	ctx := context.Background()

	if err := RequireSecretKeyForAdminSurface(ctx, testHandler.Queries, "", false); err != nil {
		t.Fatalf("disabled surface without key must not refuse startup: %v", err)
	}

	err := RequireSecretKeyForAdminSurface(ctx, testHandler.Queries, "ops@corp.example", false)
	if err == nil {
		t.Fatal("env-enabled surface without key must refuse startup")
	}
	if !strings.Contains(err.Error(), "openssl rand -base64 32") {
		t.Fatalf("refusal must carry the openssl hint: %v", err)
	}
	if !strings.Contains(err.Error(), "GOOSAR_MCP_SECRET_KEY") {
		t.Fatalf("refusal must name the missing variable: %v", err)
	}

	grantDeploymentAdminFixture(t, testUserID)
	if err := RequireSecretKeyForAdminSurface(ctx, testHandler.Queries, "", false); err == nil {
		t.Fatal("table-enabled surface without key must refuse startup")
	}

	if err := RequireSecretKeyForAdminSurface(ctx, testHandler.Queries, "ops@corp.example", true); err != nil {
		t.Fatalf("key present must always start: %v", err)
	}
}

func TestConfirmDeploymentAdminPending_ConcurrentLastTwoRevokes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	cleanupDeploymentAdminPendingRows(t)
	ctx := context.Background()

	grantDeploymentAdminFixture(t, testUserID)
	secondID, _ := deploymentUserFixture(t, "race")
	grantDeploymentAdminFixture(t, secondID)

	filePendingRevoke := func(target string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO deployment_admin_pending (action, target_user_id, requested_by)
			VALUES ('revoke', $1, $2) RETURNING id
		`, target, testUserID).Scan(&id); err != nil {
			t.Fatalf("file pending revoke: %v", err)
		}
		return id
	}
	pendingA := filePendingRevoke(testUserID)
	pendingB := filePendingRevoke(secondID)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{pendingA, pendingB} {
		wg.Add(1)
		go func(slot int, pendingID string) {
			defer wg.Done()
			_, errs[slot] = ConfirmDeploymentAdminPending(ctx, testPool, testHandler.Queries, parseUUID(pendingID))
		}(i, id)
	}
	wg.Wait()

	failures := 0
	for _, err := range errs {
		if err != nil {
			failures++
			if !strings.Contains(err.Error(), "last deployment administrator") {
				t.Fatalf("unexpected confirm error: %v", err)
			}
		}
	}
	if failures != 1 {
		t.Fatalf("concurrent last-two revokes: %d failed, want exactly 1 (errs: %v)", failures, errs)
	}
	if got := deploymentAdminCount(t); got != 1 {
		t.Fatalf("concurrent revokes left %d admins, want exactly 1", got)
	}
}
