package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/audit"
)

func auditFixture(t *testing.T) (adminID string) {
	t.Helper()
	cleanupDeploymentAdminRows(t)
	clearAuth := func() { testPool.Exec(context.Background(), `DELETE FROM auth_audit`) }
	clearAuth()
	t.Cleanup(clearAuth)

	adminID, _ = deploymentUserFixture(t, "auditor")
	grantDeploymentAdminFixture(t, adminID)
	return adminID
}

func seedAuthAudit(t *testing.T, action, actorID, outcome string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO auth_audit (actor_type, actor_id, action, target_type, target_id, outcome)
		VALUES ('user', $1, $2, 'user', $1, $3)`, actorID, action, outcome); err != nil {
		t.Fatalf("seed auth_audit: %v", err)
	}
}

func auditRequest(t *testing.T, adminID, query string) []DeploymentAuditEntry {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/deployment/audit"+query, nil)
	req.Header.Set("X-User-ID", adminID)
	rec := httptest.NewRecorder()
	deploymentAdminTestRoutes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/deployment/audit%s = %d: %s", query, rec.Code, rec.Body.String())
	}
	var entries []DeploymentAuditEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	return entries
}

func TestDeploymentAudit_IncludesAuthEvents(t *testing.T) {
	adminID := auditFixture(t)
	seedAuthAudit(t, audit.ActionLoginCodeFailed, adminID, audit.OutcomeFailure)

	entries := auditRequest(t, adminID, "?action="+audit.ActionLoginCodeFailed)
	if len(entries) == 0 {
		t.Fatal("no entries for a seeded auth event")
	}
	for _, e := range entries {
		if e.Action != audit.ActionLoginCodeFailed {
			t.Fatalf("action filter leaked %q", e.Action)
		}
		if e.Source != auditSourceAuth {
			t.Fatalf("source = %q, want %q", e.Source, auditSourceAuth)
		}
		if e.Outcome != audit.OutcomeFailure {
			t.Fatalf("outcome = %q", e.Outcome)
		}
	}
}

func TestDeploymentAudit_CursorWalksForwardWithoutRepeats(t *testing.T) {
	adminID := auditFixture(t)
	for range 3 {
		seedAuthAudit(t, audit.ActionCliTokenIssued, adminID, audit.OutcomeSuccess)
		time.Sleep(2 * time.Millisecond)
	}

	first := auditRequest(t, adminID, "?action="+audit.ActionCliTokenIssued+"&cursor=&limit=2")
	if len(first) != 2 {
		t.Fatalf("first page has %d entries, want 2", len(first))
	}
	if first[1].CreatedAt.Before(first[0].CreatedAt) {
		t.Fatal("a cursor page must be oldest-first")
	}
	if first[1].Cursor == "" {
		t.Fatal("entries in a cursor page must carry a cursor to resume from")
	}

	second := auditRequest(t, adminID, "?action="+audit.ActionCliTokenIssued+"&cursor="+first[1].Cursor+"&limit=2")
	if len(second) == 0 {
		t.Fatal("the third row was not returned after the cursor")
	}
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Fatalf("row %s returned on both pages", a.ID)
			}
		}
	}
}

func TestDeploymentAudit_DefaultsToNewestFirst(t *testing.T) {
	adminID := auditFixture(t)
	seedAuthAudit(t, audit.ActionLogout, adminID, audit.OutcomeSuccess)
	time.Sleep(3 * time.Millisecond)
	seedAuthAudit(t, audit.ActionLogout, adminID, audit.OutcomeSuccess)

	entries := auditRequest(t, adminID, "?action="+audit.ActionLogout+"&limit=2")
	if len(entries) < 2 {
		t.Fatalf("want at least 2 entries, got %d", len(entries))
	}
	if entries[0].CreatedAt.Before(entries[1].CreatedAt) {
		t.Fatal("default order must be newest-first")
	}
}

func TestDeploymentAudit_RejectsAMalformedCursor(t *testing.T) {
	adminID := auditFixture(t)
	req := httptest.NewRequest("GET", "/api/deployment/audit?cursor=not-a-cursor", nil)
	req.Header.Set("X-User-ID", adminID)
	rec := httptest.NewRecorder()
	deploymentAdminTestRoutes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed cursor = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestDeploymentAudit_NeverReturnsSecrets(t *testing.T) {
	adminID := auditFixture(t)
	seedAuthAudit(t, audit.ActionLoginCodeFailed, adminID, audit.OutcomeFailure)

	entries := auditRequest(t, adminID, "?action="+audit.ActionLoginCodeFailed+"&limit=1")
	if len(entries) == 0 {
		t.Fatal("no rows to inspect")
	}
	raw, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, forbidden := range []string{"code", "token", "token_hash", "password", "secret", "value"} {
		if _, present := fields[forbidden]; present {
			t.Fatalf("audit entry exposes a %q field", forbidden)
		}
	}
}

func TestVerifyCode_FailedLoginIsJournaled(t *testing.T) {
	adminID := auditFixture(t)

	email := "audit-failed-login-" + randomID()[:8] + "@goosar.test"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO verification_code (email, code, expires_at)
		VALUES ($1, '123456', now() + interval '10 minutes')`, email); err != nil {
		t.Fatalf("seed verification code: %v", err)
	}

	if rec := postVerifyCode(t, email, "999999"); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong code = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	entries := auditRequest(t, adminID, "?action="+audit.ActionLoginCodeFailed)
	if len(entries) == 0 {
		t.Fatal("a failed login did not reach the journal")
	}
	found := false
	for _, e := range entries {
		if e.Reason == audit.ReasonInvalidCode {
			found = true

			if e.ActorID == email || e.TargetID == email {
				t.Fatal("the journal stored the raw email address instead of the pseudonym")
			}
		}
	}
	if !found {
		t.Fatalf("no invalid_code row: %+v", entries)
	}
}
