package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func authProbe(t *testing.T, token string) (int, string) {
	t.Helper()
	handler := middleware.Auth(testHandler.Queries, nil, nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func offboardingTestUser(t *testing.T, email string) db.User {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, email, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	cleanupCheckedExec(t, `DELETE FROM "user" WHERE id = $1`, userID)
	user, err := testHandler.Queries.GetUser(ctx, parseUUID(userID))
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	return user
}

func TestOffboarding_DeactivationKillsLiveJWT(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-deactivate@goosar.test")

	token, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatalf("issue jwt: %v", err)
	}
	if code, body := authProbe(t, token); code != http.StatusOK {
		t.Fatalf("fresh session: expected 200, got %d: %s", code, body)
	}

	if _, err := testHandler.Queries.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	if code, _ := authProbe(t, token); code != http.StatusUnauthorized {
		t.Fatalf("after deactivation the already-issued JWT must be refused, got %d", code)
	}
}

func TestOffboarding_TokenVersionBumpKillsLiveJWTWhileAccountStaysActive(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-epoch@goosar.test")

	token, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatalf("issue jwt: %v", err)
	}

	if _, err := testHandler.Queries.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	reactivated, err := testHandler.Queries.ReactivateUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	if code, _ := authProbe(t, token); code != http.StatusUnauthorized {
		t.Fatalf("session from before the epoch bump must stay dead, got %d", code)
	}
	fresh, err := testHandler.issueJWT(reactivated)
	if err != nil {
		t.Fatalf("issue fresh jwt: %v", err)
	}
	if code, body := authProbe(t, fresh); code != http.StatusOK {
		t.Fatalf("a session minted after reactivation must work, got %d: %s", code, body)
	}
}

func TestOffboarding_BulkRevokeKillsPersonalAccessToken(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-pat@goosar.test")

	plain := "gsl_offboarding_test_token_390"
	if _, err := testHandler.Queries.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{
		UserID:      user.ID,
		Name:        "offboarding probe",
		TokenHash:   auth.HashToken(plain),
		TokenPrefix: "gsl_offb",
	}); err != nil {
		t.Fatalf("create pat: %v", err)
	}

	if code, body := authProbe(t, plain); code != http.StatusOK {
		t.Fatalf("fresh PAT: expected 200, got %d: %s", code, body)
	}

	hashes, err := testHandler.Queries.RevokeAllPersonalAccessTokensByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if len(hashes) != 1 {
		t.Fatalf("expected one revoked token hash, got %d", len(hashes))
	}
	if code, _ := authProbe(t, plain); code != http.StatusUnauthorized {
		t.Fatalf("revoked PAT must be refused immediately, got %d", code)
	}
}

func TestOffboarding_DeactivationRefusesUntouchedPAT(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-pat-gate@goosar.test")

	plain := "gsl_offboarding_gate_token_390"
	if _, err := testHandler.Queries.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{
		UserID:      user.ID,
		Name:        "offboarding gate probe",
		TokenHash:   auth.HashToken(plain),
		TokenPrefix: "gsl_offb",
	}); err != nil {
		t.Fatalf("create pat: %v", err)
	}
	if code, body := authProbe(t, plain); code != http.StatusOK {
		t.Fatalf("fresh PAT: expected 200, got %d: %s", code, body)
	}
	if _, err := testHandler.Queries.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if code, _ := authProbe(t, plain); code != http.StatusUnauthorized {
		t.Fatalf("PAT of a deactivated user must be refused, got %d", code)
	}
}

func TestOffboarding_MemberRemovalCutsWorkspaceAccessImmediately(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	memberID := createPermissionTestMember(t, "offboard-member@goosar.test")
	user, err := testHandler.Queries.GetUser(ctx, parseUUID(memberID))
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	token, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatalf("issue jwt: %v", err)
	}
	if code, body := authProbe(t, token); code != http.StatusOK {
		t.Fatalf("member session: expected 200, got %d: %s", code, body)
	}

	guarded := middleware.RequireWorkspaceMember(testHandler.Queries)(
		http.HandlerFunc(testHandler.ListIssues))
	listIssues := func() int {
		w := httptest.NewRecorder()
		r := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID, nil)
		r.Header.Set("X-User-ID", memberID)
		r.Header.Set("X-Workspace-ID", testWorkspaceID)
		guarded.ServeHTTP(w, r)
		return w.Code
	}
	if code := listIssues(); code != http.StatusOK {
		t.Fatalf("member should read workspace issues, got %d", code)
	}

	if _, err := testPool.Exec(ctx,
		`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberID,
	); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	if code := listIssues(); code == http.StatusOK {
		t.Fatal("removed member still reads workspace issues with the same session")
	}

	if code, _ := authProbe(t, token); code != http.StatusOK {
		t.Fatal("workspace removal must not invalidate the account session")
	}
}

func TestOffboarding_DeactivateEndpointRequiresDeploymentAdmin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	target := offboardingTestUser(t, "offboard-endpoint-target@goosar.test")

	w := httptest.NewRecorder()
	r := newRequest("POST", fmt.Sprintf("/api/deployment/users/%s/deactivate", uuidToString(target.ID)), nil)
	r = withURLParam(r, "userId", uuidToString(target.ID))
	testHandler.DeactivateDeploymentUser(w, r)
	if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("non-admin must not deactivate anyone, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["error"]; !ok {
		t.Fatalf("expected a structured error body, got %s", w.Body.String())
	}
}

func TestOffboarding_DeactivationStopsOwnedDaemonRuntimes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	memberID := createPermissionTestMember(t, "offboard-runtime-owner@goosar.test")

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, owner_id, name, runtime_mode, provider, status,
			daemon_id, device_info, metadata, last_seen_at
		)
		VALUES ($1, $2, 'offboarding runtime', 'local', 'runtime-c', 'online',
			'offboarding-daemon-390', 'offboarding device', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, memberID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
		VALUES ('offboarding-390-hash', $1, 'offboarding-daemon-390', now() + interval '1 day')
	`, testWorkspaceID); err != nil {
		t.Fatalf("seed daemon token: %v", err)
	}
	cleanupCheckedExec(t, `DELETE FROM daemon_token WHERE token_hash = 'offboarding-390-hash'`)

	if _, err := testHandler.Queries.DeactivateUser(ctx, parseUUID(memberID)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	testHandler.cutLiveCredentials(ctx, parseUUID(memberID))

	var tokens int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM daemon_token WHERE token_hash = 'offboarding-390-hash'`,
	).Scan(&tokens); err != nil {
		t.Fatalf("count daemon tokens: %v", err)
	}
	if tokens != 0 {
		t.Fatal("daemon token of a deactivated owner survived — the machine keeps claiming tasks")
	}

	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&status); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if status != "offline" {
		t.Fatalf("runtime of a deactivated owner is %q, want offline", status)
	}
}

func TestOffboarding_PreUpgradeJWTWithoutTVDiesAtFirstBump(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-legacy-jwt@goosar.test")

	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   uuidToString(user.ID),
		"email": user.Email,
		"name":  user.Name,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	token, err := legacy.SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign legacy jwt: %v", err)
	}
	if code, body := authProbe(t, token); code != http.StatusOK {
		t.Fatalf("a pre-upgrade session must keep working until it is revoked, got %d: %s", code, body)
	}

	if _, err := testHandler.Queries.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := testHandler.Queries.ReactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if code, _ := authProbe(t, token); code != http.StatusUnauthorized {
		t.Fatalf("a pre-upgrade session must die at the first epoch bump, got %d", code)
	}
}

func TestOffboarding_DeactivatedAccountCannotLogIn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-login@goosar.test")
	if _, err := testHandler.Queries.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.completeLogin(w, newRequest("POST", "/auth/verify-code", nil), user.Email, audit.ActionLoginCodeVerified)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a deactivated account must not receive a session, got %d: %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a refused login must not set auth cookies")
	}
}

func TestOffboarding_DeactivateRefusesYourOwnAccount(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "selfdeactivate")
	grantDeploymentAdminFixture(t, adminID)

	routes := chi.NewRouter()
	routes.Use(RequireHumanActor)
	routes.Post("/api/deployment/users/{userId}/deactivate", testHandler.DeactivateDeploymentUser)

	auditBefore := countAdminAuditRows(t, adminAuditActionUserDeactivate)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, newRequestAsUser(adminID, http.MethodPost,
		"/api/deployment/users/"+adminID+"/deactivate", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("self-deactivation = %d, want 409 (%s)", w.Code, w.Body.String())
	}

	var deactivated bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT deactivated_at IS NOT NULL FROM "user" WHERE id = $1`, adminID).Scan(&deactivated); err != nil {
		t.Fatalf("read deactivation: %v", err)
	}
	if deactivated {
		t.Fatal("the refused call blocked the account anyway")
	}
	if after := countAdminAuditRows(t, adminAuditActionUserDeactivate); after != auditBefore {
		t.Fatalf("a refused deactivation wrote an admin_audit row: before=%d after=%d", auditBefore, after)
	}
}

func daemonAuthProbe(t *testing.T, token string) (int, string) {
	t.Helper()
	handler := middleware.DaemonAuth(testHandler.Queries, nil, nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	req := httptest.NewRequest(http.MethodGet, "/api/daemon/workspaces", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestOffboarding_DeactivationKillsLiveJWTOnDaemonSurface(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	user := offboardingTestUser(t, "offboard-daemon@goosar.test")

	token, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatalf("issue jwt: %v", err)
	}
	if code, body := daemonAuthProbe(t, token); code != http.StatusOK {
		t.Fatalf("fresh session on the daemon path: expected 200, got %d: %s", code, body)
	}

	if _, err := testHandler.Queries.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	if code, body := daemonAuthProbe(t, token); code != http.StatusUnauthorized {
		t.Fatalf("after deactivation the JWT must be refused on the daemon path, got %d: %s", code, body)
	}
}
