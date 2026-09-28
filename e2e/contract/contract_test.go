package contract

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func urlEscape(s string) string {
	return url.QueryEscape(s)
}

// TestContract is the single entry point for the whole suite. Every domain
// gets its own named subtest so a single one can be re-run in isolation,
// e.g.:
//
//	go test ./e2e/contract/... -run 'TestContract/auth' -v
//
// Each subtest calls the ensureX() fixture helpers it needs, so it is
// self-sufficient even when run alone: it will transparently create the
// workspace/runtime/agent/etc. it depends on if a fuller run hasn't already
// done so.
func TestContract(t *testing.T) {
	f := getFixture(t)

	t.Run("auth", testAuth(f))
	t.Run("workspaces", testWorkspaces(f))
	t.Run("me", testMe(f))
	t.Run("issues", testIssues(f))
	t.Run("projects", testProjects(f))
	t.Run("labels", testLabels(f))
	t.Run("comments", testComments(f))
	t.Run("squads", testSquads(f))
	t.Run("autopilots", testAutopilots(f))
	t.Run("agents", testAgents(f))
	t.Run("runtimes", testRuntimes(f))
	t.Run("skills", testSkills(f))
	t.Run("chat", testChat(f))
	t.Run("inbox", testInbox(f))
	t.Run("daemon", testDaemon(f))
	t.Run("deployment", testDeployment(f))
	t.Run("admin", testAdmin(f))
	t.Run("integrations", testIntegrations(f))
	t.Run("public", testPublic(f))
}

// ---------------------------------------------------------------------------
// auth
// ---------------------------------------------------------------------------

func testAuth(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAuth(t)

		status, _ := f.call(t, http.MethodGet, "/api/auth/methods", "/api/auth/methods", nil)
		if status != 200 {
			t.Errorf("GET /api/auth/methods: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/me", "/api/me", nil)
		if status != 200 {
			t.Errorf("GET /api/me: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/auth/mfa", "/api/auth/mfa", nil)
		if status != 200 {
			t.Errorf("GET /api/auth/mfa: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/auth/sessions", "/api/auth/sessions", nil)
		if status != 200 {
			t.Errorf("GET /api/auth/sessions: expected 200, got %d", status)
		}

		// Wrong verification code: a documented failure path, not just the
		// happy path.
		status, _ = f.call(t, http.MethodPost, "/auth/verify-code", "/auth/verify-code", map[string]any{
			"email": fmt.Sprintf("nobody-%s@example.test", uniqueSuffix()),
			"code":  "000000",
		})
		if status != 400 {
			t.Errorf("POST /auth/verify-code with a bad code: expected 400, got %d", status)
		}

		// No Authorization header at all: unauthenticated /api/me.
		anon := newAPIClient(f.baseURL)
		status, _ = f.callAs(t, anon, http.MethodGet, "/api/me", "/api/me", nil)
		if status != 401 {
			t.Errorf("GET /api/me with no token: expected 401, got %d", status)
		}

		// authVerifyLoginLink: an invalid/expired magic-link token is a
		// documented failure, any status the contract lists is fine.
		f.call(t, http.MethodPost, "/auth/verify-link", "/auth/verify-link", map[string]any{
			"link_token": "not-a-real-token-" + uniqueSuffix(),
		})

		// OIDC/LDAP are unconfigured on this deployment (no OIDC_ISSUER_URL/
		// LDAP_URL) — contract documents 404 for both in that case.
		f.call(t, http.MethodGet, "/api/auth/oidc/start", "/api/auth/oidc/start", nil)
		f.call(t, http.MethodGet, "/api/auth/oidc/callback", "/api/auth/oidc/callback?state=x&code=y", nil)
		f.call(t, http.MethodPost, "/api/auth/ldap/login", "/api/auth/ldap/login", map[string]any{
			"username": "someone", "password": "whatever",
		})

		// authRevokeSession: an id that isn't this user's session is a
		// documented 404 (not "someone else's data leaked").
		status, _ = f.call(t, http.MethodDelete, "/api/auth/sessions/{sessionId}",
			"/api/auth/sessions/00000000-0000-0000-0000-000000000000", nil)
		if status != 404 {
			t.Errorf("DELETE /api/auth/sessions/{sessionId} for an unknown id: expected 404, got %d", status)
		}

		// Full MFA lifecycle (enroll -> confirm -> login now requires MFA ->
		// verify -> regenerate recovery codes -> disable), on a dedicated
		// ephemeral user so it never affects the shared fixture's own login.
		mfaClient, mfaEmail, _ := loginFreshUser(t, f)

		status, body := f.callAs(t, mfaClient, http.MethodPost, "/api/auth/mfa/totp/enroll", "/api/auth/mfa/totp/enroll", nil)
		if status == 503 {
			// Documented alternate path (docs/50-api-contract.yaml,
			// authEnrollTotp '503'): MFA secrets can only be sealed and
			// stored when the deployment's encryption key is configured.
			// This deployment doesn't have one, so there is nothing left to
			// exercise in the rest of the MFA lifecycle below.
			t.Logf("POST /api/auth/mfa/totp/enroll: got documented 503 (MFA storage not configured on " +
				"this deployment); skipping the rest of the MFA lifecycle")
			return
		}
		if status != 200 {
			t.Fatalf("POST /api/auth/mfa/totp/enroll: expected 200, got %d, body=%s", status, truncate(body))
		}
		enroll := decodeJSON(t, body)
		secret := mustStr(t, enroll, "secret")

		// confirm/verify below must land on distinct TOTP steps: the server's
		// anti-replay guard (T-029 доводка) rejects a step already accepted,
		// even by a different operation, within the same ~90s window (±1
		// step) that RFC 6238 leaves mathematically valid — but that window
		// only has 3 distinct steps (-1/0/+1) in total, so the *remaining*
		// two calls below (recovery-codes, disable) deliberately use one-time
		// recovery codes instead of a third/fourth TOTP step: recovery codes
		// aren't time-windowed, and this also exercises verifyCurrentFactor's
		// recovery-code fallback path (docs/50-api-contract.yaml,
		// MFACodeRequest — "текущий код/recovery-код").
		status, body = f.callAs(t, mfaClient, http.MethodPost, "/api/auth/mfa/totp/confirm", "/api/auth/mfa/totp/confirm",
			map[string]any{"code": computeTOTPAtOffset(secret, -1)})
		if status != 200 {
			t.Fatalf("POST /api/auth/mfa/totp/confirm: expected 200, got %d, body=%s", status, truncate(body))
		}
		confirm := decodeJSON(t, body)
		recoveryCodes, _ := confirm["recovery_codes"].([]any)
		if len(recoveryCodes) == 0 {
			t.Fatalf("POST /api/auth/mfa/totp/confirm: expected non-empty recovery_codes")
		}

		// Logging in again now requires the second factor.
		status, _ = f.callAs(t, mfaClient, http.MethodPost, "/auth/send-code", "/auth/send-code",
			map[string]any{"email": mfaEmail})
		if status != 200 {
			t.Errorf("POST /auth/send-code (MFA user, re-login): expected 200, got %d", status)
		}
		status, body = f.callAs(t, mfaClient, http.MethodPost, "/auth/verify-code", "/auth/verify-code",
			map[string]any{"email": mfaEmail, "code": f.devCode})
		if status != 200 {
			t.Fatalf("POST /auth/verify-code (MFA user, re-login): expected 200, got %d, body=%s", status, truncate(body))
		}
		login := decodeJSON(t, body)
		if required, _ := login["mfa_required"].(bool); !required {
			t.Errorf("POST /auth/verify-code: expected mfa_required=true once TOTP is enabled")
		}
		mfaToken := str(login, "mfa_token")

		status, body = f.callAs(t, mfaClient, http.MethodPost, "/api/auth/mfa/verify", "/api/auth/mfa/verify",
			map[string]any{"mfa_token": mfaToken, "code": computeTOTPAtOffset(secret, 0)})
		if status != 200 {
			t.Fatalf("POST /api/auth/mfa/verify: expected 200, got %d, body=%s", status, truncate(body))
		}
		verified := decodeJSON(t, body)
		mfaClient.token = mustStr(t, verified, "token")

		status, body = f.callAs(t, mfaClient, http.MethodPost, "/api/auth/mfa/recovery-codes", "/api/auth/mfa/recovery-codes",
			map[string]any{"code": recoveryCodes[0].(string)})
		if status != 200 {
			t.Fatalf("POST /api/auth/mfa/recovery-codes: expected 200, got %d, body=%s", status, truncate(body))
		}
		regenerated := decodeJSON(t, body)
		newRecoveryCodes, _ := regenerated["recovery_codes"].([]any)
		if len(newRecoveryCodes) == 0 {
			t.Fatalf("POST /api/auth/mfa/recovery-codes: expected non-empty recovery_codes")
		}

		status, _ = f.callAs(t, mfaClient, http.MethodPost, "/api/auth/mfa/totp/disable", "/api/auth/mfa/totp/disable",
			map[string]any{"code": newRecoveryCodes[0].(string)})
		if status != 200 {
			t.Errorf("POST /api/auth/mfa/totp/disable: expected 200, got %d", status)
		}

		status, _ = f.callAs(t, mfaClient, http.MethodPost, "/api/auth/sessions/revoke-all", "/api/auth/sessions/revoke-all", nil)
		if status != 200 {
			t.Errorf("POST /api/auth/sessions/revoke-all: expected 200, got %d", status)
		}

		status, _ = f.callAs(t, mfaClient, http.MethodPost, "/auth/logout", "/auth/logout", nil)
		if status != 200 && status != 204 {
			t.Errorf("POST /auth/logout: expected 200/204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// workspaces
// ---------------------------------------------------------------------------

func testWorkspaces(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		status, _ := f.call(t, http.MethodGet, "/api/workspaces", "/api/workspaces", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}", "/api/workspaces/"+f.workspaceID, nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/capabilities",
			"/api/workspaces/"+f.workspaceID+"/capabilities", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/capabilities: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/members: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/workspaces/{id}", "/api/workspaces/"+f.workspaceID,
			map[string]any{"description": "updated by contract suite " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/workspaces/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspace-templates", "/api/workspace-templates", nil)
		if status != 200 {
			t.Errorf("GET /api/workspace-templates: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/invitations",
			"/api/workspaces/"+f.workspaceID+"/invitations", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/invitations: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", map[string]any{
				"email": fmt.Sprintf("contract-invite-%s@example.test", uniqueSuffix()),
			})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/workspaces/{id}/members: expected 200/201, got %d", status)
		}

		status, body := f.call(t, http.MethodPost, "/api/workspaces/{id}/runtime-profiles",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles", map[string]any{
				"display_name":    "Contract profile " + uniqueSuffix(),
				"protocol_family": "runtime-a",
				"command_name":    "contract-runtime-cmd",
			})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/workspaces/{id}/runtime-profiles: expected 200/201, got %d, body=%s", status, truncate(body))
		} else {
			profile := decodeJSON(t, body)
			profileID := str(profile, "id")
			if profileID != "" {
				status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/runtime-profiles/{profileId}",
					"/api/workspaces/"+f.workspaceID+"/runtime-profiles/"+profileID, nil)
				if status != 200 {
					t.Errorf("GET /api/workspaces/{id}/runtime-profiles/{profileId}: expected 200, got %d", status)
				}
			}
		}

		// A workspace that does not exist: documented 404.
		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}",
			"/api/workspaces/00000000-0000-0000-0000-000000000000", nil)
		if status != 404 {
			t.Errorf("GET /api/workspaces/{id} for an unknown id: expected 404, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/workspaces/{id}", "/api/workspaces/"+f.workspaceID,
			map[string]any{"description": "replaced by contract suite " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/workspaces/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/assignee-frequency", "/api/assignee-frequency", nil)
		if status != 200 {
			t.Errorf("GET /api/assignee-frequency: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/github/connect",
			"/api/workspaces/"+f.workspaceID+"/github/connect", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/github/connect: expected 200, got %d", status)
		}

		// VCS/Slack integrations are (likely) unconfigured on this deployment;
		// the contract documents 400/502/503 alongside 200 for exactly that.
		f.call(t, http.MethodPost, "/api/workspaces/{id}/vcs/connections",
			"/api/workspaces/"+f.workspaceID+"/vcs/connections", map[string]any{
				"provider": "gitlab", "instance_url": "https://gitlab.contract-test.invalid",
				"token": "fake-token-" + uniqueSuffix(),
			})
		f.ensureAgent(t)
		f.call(t, http.MethodPost, "/api/workspaces/{id}/slack/install/byo",
			"/api/workspaces/"+f.workspaceID+"/slack/install/byo?agent_id="+f.agentID, map[string]any{
				"bot_token": "xoxb-fake", "app_token": "xapp-fake",
			})
		// An installation/connection id that does not exist: documented 404 for
		// both, without needing a real GitHub App/VCS connection.
		f.call(t, http.MethodDelete, "/api/workspaces/{id}/github/installations/{installationId}",
			"/api/workspaces/"+f.workspaceID+"/github/installations/00000000-0000-0000-0000-000000000000", nil)
		f.call(t, http.MethodGet, "/api/workspaces/{id}/github/installations/{installationId}/repositories",
			"/api/workspaces/"+f.workspaceID+"/github/installations/00000000-0000-0000-0000-000000000000/repositories", nil)
		f.call(t, http.MethodDelete, "/api/workspaces/{id}/slack/installations/{installationId}",
			"/api/workspaces/"+f.workspaceID+"/slack/installations/00000000-0000-0000-0000-000000000000", nil)

		// Runtime profile lifecycle on a profile of its own (PATCH already
		// exercised above on the shared one): PUT then DELETE.
		status, body = f.call(t, http.MethodPost, "/api/workspaces/{id}/runtime-profiles",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles", map[string]any{
				"display_name":    "Contract profile lifecycle " + uniqueSuffix(),
				"protocol_family": "runtime-a",
				"command_name":    "contract-runtime-cmd",
			})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/workspaces/{id}/runtime-profiles (lifecycle): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		lifecycleProfile := decodeJSON(t, body)
		lifecycleProfileID := mustStr(t, lifecycleProfile, "id")

		status, _ = f.call(t, http.MethodPut, "/api/workspaces/{id}/runtime-profiles/{profileId}",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles/"+lifecycleProfileID,
			map[string]any{"display_name": "Contract profile lifecycle (renamed) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/workspaces/{id}/runtime-profiles/{profileId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodDelete, "/api/workspaces/{id}/runtime-profiles/{profileId}",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles/"+lifecycleProfileID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/workspaces/{id}/runtime-profiles/{profileId}: expected 204, got %d", status)
		}

		// Workspace export: async job started -> its status can be read
		// immediately (download is documented 409 before it finishes).
		status, body = f.call(t, http.MethodPost, "/api/workspaces/{id}/export",
			"/api/workspaces/"+f.workspaceID+"/export", nil)
		if status == 202 {
			job := decodeJSON(t, body)
			jobID := mustStr(t, job, "id")
			status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/export/{jobId}",
				"/api/workspaces/"+f.workspaceID+"/export/"+jobID, nil)
			if status != 200 {
				t.Errorf("GET /api/workspaces/{id}/export/{jobId}: expected 200, got %d", status)
			}
			f.call(t, http.MethodGet, "/api/workspaces/{id}/export/{jobId}/download",
				"/api/workspaces/"+f.workspaceID+"/export/"+jobID+"/download", nil)
		} else if status != 503 && status != 409 {
			t.Errorf("POST /api/workspaces/{id}/export: expected 202/409/503, got %d, body=%s", status, truncate(body))
		}

		// Invitation lifecycle with two independent, freshly logged-in users:
		// one accepts (then gets updated/removed as a real member), one
		// declines.
		secondClient, secondEmail := f.ensureSecondUser(t)
		thirdClient, thirdEmail := f.ensureThirdUser(t)

		status, body = f.call(t, http.MethodPost, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", map[string]any{"email": secondEmail})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/workspaces/{id}/members (second user): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		secondInvite := decodeJSON(t, body)
		secondInviteID := mustStr(t, secondInvite, "id")

		status, _ = f.call(t, http.MethodPost, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", map[string]any{"email": thirdEmail})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/workspaces/{id}/members (third user): expected 200/201, got %d", status)
		}

		status, _ = f.callAs(t, secondClient, http.MethodGet, "/api/invitations", "/api/invitations", nil)
		if status != 200 {
			t.Errorf("GET /api/invitations (second user): expected 200, got %d", status)
		}
		status, _ = f.callAs(t, secondClient, http.MethodGet, "/api/invitations/{id}", "/api/invitations/"+secondInviteID, nil)
		if status != 200 {
			t.Errorf("GET /api/invitations/{id} (second user): expected 200, got %d", status)
		}
		status, body = f.callAs(t, secondClient, http.MethodPost, "/api/invitations/{id}/accept",
			"/api/invitations/"+secondInviteID+"/accept", nil)
		if status != 200 {
			t.Fatalf("POST /api/invitations/{id}/accept: expected 200, got %d, body=%s", status, truncate(body))
		}
		membership := decodeJSON(t, body)
		secondMemberID := mustStr(t, membership, "id")

		status, _ = f.call(t, http.MethodPatch, "/api/workspaces/{id}/members/{memberId}",
			"/api/workspaces/"+f.workspaceID+"/members/"+secondMemberID, map[string]any{"role": "admin"})
		if status != 200 {
			t.Errorf("PATCH /api/workspaces/{id}/members/{memberId}: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/workspaces/{id}/members/{memberId}",
			"/api/workspaces/"+f.workspaceID+"/members/"+secondMemberID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/workspaces/{id}/members/{memberId}: expected 204, got %d", status)
		}

		// Third user declines their own invite instead of accepting.
		invites := decodeJSONArray(t, func() []byte {
			_, b := f.callAs(t, thirdClient, http.MethodGet, "/api/invitations", "/api/invitations", nil)
			return b
		}())
		var thirdInviteID string
		for _, raw := range invites {
			if inv, ok := raw.(map[string]any); ok && str(inv, "id") != "" {
				thirdInviteID = str(inv, "id")
				break
			}
		}
		if thirdInviteID != "" {
			status, _ = f.callAs(t, thirdClient, http.MethodPost, "/api/invitations/{id}/decline",
				"/api/invitations/"+thirdInviteID+"/decline", nil)
			if status != 204 {
				t.Errorf("POST /api/invitations/{id}/decline: expected 204, got %d", status)
			}
		} else {
			t.Errorf("expected at least one pending invitation for the third contract user")
		}
		f.call(t, http.MethodDelete, "/api/workspaces/{id}/invitations/{invitationId}",
			"/api/workspaces/"+f.workspaceID+"/invitations/00000000-0000-0000-0000-000000000000", nil)

		// A brand-new, disposable workspace exercises leaveWorkspace and
		// deleteWorkspace without touching the shared fixture's own one.
		suffix := uniqueSuffix()
		status, body = f.call(t, http.MethodPost, "/api/workspaces", "/api/workspaces", map[string]any{
			"name": "Contract disposable " + suffix, "slug": "contract-disposable-" + suffix,
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/workspaces (disposable): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		disposable := decodeJSON(t, body)
		disposableID := mustStr(t, disposable, "id")

		disposableClient := f.client.withWorkspace(disposableID)
		status, _ = f.callAs(t, disposableClient, http.MethodPost, "/api/workspaces/{id}/members",
			"/api/workspaces/"+disposableID+"/members", map[string]any{"email": secondEmail})
		if status == 201 || status == 200 {
			// Re-use the second user (already logged in) to join and leave.
			status, body = f.callAs(t, secondClient, http.MethodGet, "/api/invitations", "/api/invitations", nil)
			if status == 200 {
				for _, raw := range decodeJSONArray(t, body) {
					inv, ok := raw.(map[string]any)
					if !ok || str(inv, "workspace_id") != disposableID {
						continue
					}
					invID := str(inv, "id")
					if invID == "" {
						continue
					}
					f.callAs(t, secondClient, http.MethodPost, "/api/invitations/{id}/accept", "/api/invitations/"+invID+"/accept", nil)
					secondScoped := secondClient.withWorkspace(disposableID)
					status, _ = f.callAs(t, secondScoped, http.MethodPost, "/api/workspaces/{id}/leave",
						"/api/workspaces/"+disposableID+"/leave", nil)
					if status != 204 {
						t.Errorf("POST /api/workspaces/{id}/leave: expected 204, got %d", status)
					}
					break
				}
			}
		}
		status, _ = f.callAs(t, disposableClient, http.MethodDelete, "/api/workspaces/{id}",
			"/api/workspaces/"+disposableID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/workspaces/{id}: expected 204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// me
// ---------------------------------------------------------------------------

func testMe(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAuth(t)

		status, body := f.call(t, http.MethodGet, "/api/me", "/api/me", nil)
		if status != 200 {
			t.Fatalf("GET /api/me: expected 200, got %d", status)
		}
		me := decodeJSON(t, body)
		if mustStr(t, me, "id") != f.userID {
			t.Errorf("GET /api/me: id %q does not match logged-in user %q", me["id"], f.userID)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/me", "/api/me",
			map[string]any{"name": "Contract Tester " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/me: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/me/onboarding", "/api/me/onboarding",
			map[string]any{"questionnaire": map[string]any{"role": "engineer"}})
		if status != 200 {
			t.Errorf("PATCH /api/me/onboarding: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/me/onboarding/complete", "/api/me/onboarding/complete",
			map[string]any{"completion_path": "full"})
		if status != 200 {
			t.Errorf("POST /api/me/onboarding/complete: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/me/onboarding/cloud-waitlist", "/api/me/onboarding/cloud-waitlist",
			map[string]any{"email": f.email})
		if status != 200 {
			t.Errorf("POST /api/me/onboarding/cloud-waitlist: expected 200, got %d", status)
		}

		f.ensureAgent(t)
		f.call(t, http.MethodPost, "/api/me/onboarding/runtime-bootstrap", "/api/me/onboarding/runtime-bootstrap",
			map[string]any{"workspace_id": f.workspaceID, "runtime_id": f.runtimeID})
		f.call(t, http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", "/api/me/onboarding/no-runtime-bootstrap",
			map[string]any{"workspace_id": f.workspaceID})

		status, _ = f.call(t, http.MethodPost, "/api/cli-token", "/api/cli-token", nil)
		if status != 200 {
			t.Errorf("POST /api/cli-token: expected 200, got %d", status)
		}

		status, body = f.callMultipart(t, "/api/upload-file", "/api/upload-file",
			nil, "file", "contract-test.txt", []byte("hello from the contract suite"))
		if status != 200 && status != 503 {
			t.Errorf("POST /api/upload-file: expected 200/503 (no storage backend configured), got %d, body=%s", status, truncate(body))
		} else if status == 200 {
			uploaded := decodeJSON(t, body)
			f.uploadedAttachmentID = str(uploaded, "id")
		}

		status, _ = f.call(t, http.MethodPost, "/api/feedback", "/api/feedback",
			map[string]any{"message": "Contract suite feedback " + uniqueSuffix()})
		if status != 201 {
			t.Errorf("POST /api/feedback: expected 201, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/client-usage", "/api/client-usage",
			map[string]any{"install_id": randomUUID()})
		if status != 204 {
			t.Errorf("POST /api/client-usage: expected 204, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/me/export", "/api/me/export", nil)
		if status != 200 {
			t.Errorf("GET /api/me/export: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// issues
// ---------------------------------------------------------------------------

func testIssues(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureIssue(t)

		status, _ := f.call(t, http.MethodGet, "/api/issues", "/api/issues", nil)
		if status != 200 {
			t.Errorf("GET /api/issues: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}", "/api/issues/"+f.issueID, nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/issues/{id}", "/api/issues/"+f.issueID,
			map[string]any{"title": "Contract issue (updated) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/issues/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/search", "/api/issues/search?q=contract", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/search: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/attachments",
			"/api/issues/"+f.issueID+"/attachments", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/attachments: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/subscribers",
			"/api/issues/"+f.issueID+"/subscribers", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/subscribers: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}", "/api/issues/00000000-0000-0000-0000-000000000000", nil)
		if status != 404 {
			t.Errorf("GET /api/issues/{id} for an unknown id: expected 404, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues", "/api/issues", map[string]any{})
		if status != 400 {
			t.Errorf("POST /api/issues with no title: expected 400, got %d", status)
		}

		// T-027 доводка: IssueTable (/api/issues/table/{groups,rows,facets})
		// и /api/issues/children (набор родителей через query).
		status, _ = f.call(t, http.MethodGet, "/api/issues/children",
			"/api/issues/children?parent_ids="+f.issueID, nil)
		if status != 200 {
			t.Errorf("GET /api/issues/children: expected 200, got %d", status)
		}

		status, groupsBody := f.call(t, http.MethodPost, "/api/issues/table/groups", "/api/issues/table/groups",
			map[string]any{
				"query": map[string]any{"scope": map[string]any{"kind": "workspace"}},
				"group": map[string]any{"kind": "status"},
			})
		if status != 200 {
			t.Fatalf("POST /api/issues/table/groups: expected 200, got %d, body=%s", status, truncate(groupsBody))
		}
		groups := decodeJSON(t, groupsBody)
		groupList, _ := groups["groups"].([]any)
		if len(groupList) == 0 {
			t.Errorf("POST /api/issues/table/groups: expected at least one group (issue was created above), got 0")
		}

		status, rowsBody := f.call(t, http.MethodPost, "/api/issues/table/rows", "/api/issues/table/rows",
			map[string]any{
				"query": map[string]any{"scope": map[string]any{"kind": "workspace"}},
				"group": map[string]any{"kind": "none"},
				"page":  map[string]any{"limit": 10},
			})
		if status != 200 {
			t.Fatalf("POST /api/issues/table/rows: expected 200, got %d, body=%s", status, truncate(rowsBody))
		}
		rows := decodeJSON(t, rowsBody)
		rowList, _ := rows["rows"].([]any)
		if len(rowList) == 0 {
			t.Errorf("POST /api/issues/table/rows: expected at least one row, got 0")
		}
		fingerprint, _ := rows["query_fingerprint"].(string)
		if fingerprint == "" {
			t.Errorf("POST /api/issues/table/rows: expected a non-empty query_fingerprint")
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues/table/facets", "/api/issues/table/facets",
			map[string]any{
				"query":  map[string]any{"scope": map[string]any{"kind": "workspace"}},
				"facets": []map[string]any{{"kind": "status"}, {"kind": "priority"}},
			})
		if status != 200 {
			t.Errorf("POST /api/issues/table/facets: expected 200, got %d", status)
		}

		// A cursor whose fingerprint does not match the current query is a
		// documented 400, not a silently-wrong page.
		status, _ = f.call(t, http.MethodPost, "/api/issues/table/rows", "/api/issues/table/rows",
			map[string]any{
				"query": map[string]any{"scope": map[string]any{"kind": "workspace"}},
				"group": map[string]any{"kind": "none"},
				"page":  map[string]any{"limit": 10, "cursor": "not-a-valid-cursor"},
			})
		if status != 400 {
			t.Errorf("POST /api/issues/table/rows with an invalid cursor: expected 400, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/children", "/api/issues/"+f.issueID+"/children", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/children: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/issues/child-progress", "/api/issues/child-progress", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/child-progress: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/issues/grouped", "/api/issues/grouped", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/grouped: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/pull-requests", "/api/issues/"+f.issueID+"/pull-requests", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/pull-requests: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/issues/query", "/api/issues/query", map[string]any{})
		if status != 200 {
			t.Errorf("POST /api/issues/query: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/issues/preview-trigger", "/api/issues/preview-trigger",
			map[string]any{"issue_ids": []string{f.issueID}})
		if status != 200 {
			t.Errorf("POST /api/issues/preview-trigger: expected 200, got %d", status)
		}

		f.ensureAgent(t)
		status, body := f.call(t, http.MethodPost, "/api/issues/quick-create", "/api/issues/quick-create",
			map[string]any{"agent_id": f.agentID, "prompt": "Contract quick-create " + uniqueSuffix()})
		if status != 202 && status != 422 {
			t.Errorf("POST /api/issues/quick-create: expected 202/422, got %d, body=%s", status, truncate(body))
		}

		// IssueMetadata: set -> list -> delete.
		status, _ = f.call(t, http.MethodPut, "/api/issues/{id}/metadata/{key}",
			"/api/issues/"+f.issueID+"/metadata/contract_key", map[string]any{"value": "contract-value"})
		if status != 200 {
			t.Errorf("PUT /api/issues/{id}/metadata/{key}: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/metadata", "/api/issues/"+f.issueID+"/metadata", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/metadata: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/issues/{id}/metadata/{key}",
			"/api/issues/"+f.issueID+"/metadata/contract_key", nil)
		if status != 200 {
			t.Errorf("DELETE /api/issues/{id}/metadata/{key}: expected 200, got %d", status)
		}

		// IssueProperties: create a text property of its own, set then clear
		// its value on the fixture issue.
		status, body = f.call(t, http.MethodPost, "/api/properties", "/api/properties", map[string]any{
			"name": "contract-issue-prop-" + uniqueSuffix(), "type": "text",
		})
		if status == 200 || status == 201 {
			prop := decodeJSON(t, body)
			propID := str(prop, "id")
			if propID != "" {
				status, _ = f.call(t, http.MethodPut, "/api/issues/{id}/properties/{propertyId}",
					"/api/issues/"+f.issueID+"/properties/"+propID, map[string]any{"value": "contract"})
				if status != 200 {
					t.Errorf("PUT /api/issues/{id}/properties/{propertyId}: expected 200, got %d", status)
				}
				status, _ = f.call(t, http.MethodDelete, "/api/issues/{id}/properties/{propertyId}",
					"/api/issues/"+f.issueID+"/properties/"+propID, nil)
				if status != 200 {
					t.Errorf("DELETE /api/issues/{id}/properties/{propertyId}: expected 200, got %d", status)
				}
			}
		}

		// IssueReactions: add -> remove the same emoji.
		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/reactions", "/api/issues/"+f.issueID+"/reactions",
			map[string]any{"emoji": "🎉"})
		if status != 201 {
			t.Errorf("POST /api/issues/{id}/reactions: expected 201, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/issues/{id}/reactions", "/api/issues/"+f.issueID+"/reactions",
			map[string]any{"emoji": "🎉"})
		if status != 204 {
			t.Errorf("DELETE /api/issues/{id}/reactions: expected 204, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/unsubscribe", "/api/issues/"+f.issueID+"/unsubscribe", nil)
		if status != 200 {
			t.Errorf("POST /api/issues/{id}/unsubscribe: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/active-task", "/api/issues/"+f.issueID+"/active-task", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/active-task: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/task-runs", "/api/issues/"+f.issueID+"/task-runs", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/task-runs: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/usage", "/api/issues/"+f.issueID+"/usage", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/usage: expected 200, got %d", status)
		}
		f.call(t, http.MethodPost, "/api/issues/{id}/tasks/{taskId}/cancel",
			"/api/issues/"+f.issueID+"/tasks/00000000-0000-0000-0000-000000000000/cancel", nil)
		f.call(t, http.MethodPost, "/api/tasks/{taskId}/cancel",
			"/api/tasks/00000000-0000-0000-0000-000000000000/cancel", nil)
		f.call(t, http.MethodGet, "/api/tasks/{taskId}/messages",
			"/api/tasks/00000000-0000-0000-0000-000000000000/messages", nil)

		status, body = f.call(t, http.MethodPost, "/api/issues/{id}/rerun", "/api/issues/"+f.issueID+"/rerun", nil)
		if status != 202 && status != 400 && status != 403 {
			t.Errorf("POST /api/issues/{id}/rerun: expected 202/400/403, got %d, body=%s", status, truncate(body))
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/comments/trigger-preview",
			"/api/issues/"+f.issueID+"/comments/trigger-preview", map[string]any{"content": "@agent contract preview"})
		if status != 200 {
			t.Errorf("POST /api/issues/{id}/comments/trigger-preview: expected 200, got %d", status)
		}

		// moveIssue: both anchors null means "keep current position" — a
		// documented, side-effect-light way to exercise the operation.
		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/move", "/api/issues/"+f.issueID+"/move",
			map[string]any{"before_id": nil, "after_id": nil})
		if status != 200 {
			t.Errorf("POST /api/issues/{id}/move: expected 200, got %d", status)
		}

		// Batch operations and delete run against disposable issues of their
		// own, never the shared f.issueID other subtests still depend on.
		status, body = f.call(t, http.MethodPost, "/api/issues", "/api/issues", map[string]any{
			"title": "Contract batch target " + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/issues (batch target): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		batchTarget := decodeJSON(t, body)
		batchTargetID := mustStr(t, batchTarget, "id")

		status, _ = f.call(t, http.MethodPost, "/api/issues/batch-update", "/api/issues/batch-update",
			map[string]any{"issue_ids": []string{batchTargetID}, "updates": map[string]any{"priority": "high"}})
		if status != 200 {
			t.Errorf("POST /api/issues/batch-update: expected 200, got %d", status)
		}

		status, body = f.call(t, http.MethodPost, "/api/issues", "/api/issues", map[string]any{
			"title": "Contract delete target " + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/issues (delete target): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		deleteTarget := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/issues/{id}", "/api/issues/"+mustStr(t, deleteTarget, "id"), nil)
		if status != 204 {
			t.Errorf("DELETE /api/issues/{id}: expected 204, got %d", status)
		}

		status, body = f.call(t, http.MethodPost, "/api/issues", "/api/issues", map[string]any{
			"title": "Contract batch-delete target " + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/issues (batch-delete target): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		batchDeleteTarget := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodPost, "/api/issues/batch-delete", "/api/issues/batch-delete",
			map[string]any{"issue_ids": []string{mustStr(t, batchDeleteTarget, "id")}})
		if status != 200 {
			t.Errorf("POST /api/issues/batch-delete: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// projects
// ---------------------------------------------------------------------------

func testProjects(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureProject(t)

		status, _ := f.call(t, http.MethodGet, "/api/projects", "/api/projects", nil)
		if status != 200 {
			t.Errorf("GET /api/projects: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/projects/{id}", "/api/projects/"+f.projectID, nil)
		if status != 200 {
			t.Errorf("GET /api/projects/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/projects/{id}", "/api/projects/"+f.projectID,
			map[string]any{"title": "Contract project (updated) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/projects/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/projects/{id}/resources",
			"/api/projects/"+f.projectID+"/resources", nil)
		if status != 200 {
			t.Errorf("GET /api/projects/{id}/resources: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/projects/search", "/api/projects/search?q=contract", nil)
		if status != 200 {
			t.Errorf("GET /api/projects/search: expected 200, got %d", status)
		}

		status, body := f.call(t, http.MethodPost, "/api/projects/{id}/resources",
			"/api/projects/"+f.projectID+"/resources", map[string]any{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": "https://github.com/example/contract-test-" + uniqueSuffix()},
			})
		if status != 201 && status != 200 && status != 400 {
			t.Errorf("POST /api/projects/{id}/resources: expected 200/201/400, got %d", status)
		}
		if status == 200 || status == 201 {
			res := decodeJSON(t, body)
			resID := str(res, "id")
			if resID != "" {
				status, _ = f.call(t, http.MethodPut, "/api/projects/{id}/resources/{resourceId}",
					"/api/projects/"+f.projectID+"/resources/"+resID, map[string]any{"label": "renamed by contract suite"})
				if status != 200 {
					t.Errorf("PUT /api/projects/{id}/resources/{resourceId}: expected 200, got %d", status)
				}
				status, _ = f.call(t, http.MethodDelete, "/api/projects/{id}/resources/{resourceId}",
					"/api/projects/"+f.projectID+"/resources/"+resID, nil)
				if status != 204 {
					t.Errorf("DELETE /api/projects/{id}/resources/{resourceId}: expected 204, got %d", status)
				}
			}
		}

		// createProject/deleteProject on a disposable project of their own.
		status, body = f.call(t, http.MethodPost, "/api/projects", "/api/projects", map[string]any{
			"title": "Contract disposable project " + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/projects (disposable): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		disposableProject := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/projects/{id}", "/api/projects/"+mustStr(t, disposableProject, "id"), nil)
		if status != 204 {
			t.Errorf("DELETE /api/projects/{id}: expected 204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// labels
// ---------------------------------------------------------------------------

func testLabels(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureLabel(t)

		status, _ := f.call(t, http.MethodGet, "/api/labels", "/api/labels", nil)
		if status != 200 {
			t.Errorf("GET /api/labels: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/labels/{id}", "/api/labels/"+f.labelID, nil)
		if status != 200 {
			t.Errorf("GET /api/labels/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/labels/{id}", "/api/labels/"+f.labelID,
			map[string]any{"name": "contract-updated-" + uniqueSuffix(), "color": "#112233"})
		if status != 200 {
			t.Errorf("PUT /api/labels/{id}: expected 200, got %d", status)
		}

		f.ensureIssue(t)
		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/labels", "/api/issues/"+f.issueID+"/labels",
			map[string]any{"label_id": f.labelID})
		if status != 200 && status != 201 {
			t.Errorf("POST /api/issues/{id}/labels: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/labels", "/api/issues/"+f.issueID+"/labels", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/labels: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodDelete, "/api/issues/{id}/labels/{labelId}",
			"/api/issues/"+f.issueID+"/labels/"+f.labelID, nil)
		if status != 200 && status != 204 {
			t.Errorf("DELETE /api/issues/{id}/labels/{labelId}: expected 200/204, got %d", status)
		}

		// createLabel/deleteLabel on a disposable label of their own (the
		// shared f.labelID above stays attached/detached, other subtests may
		// still read it).
		status, body := f.call(t, http.MethodPost, "/api/labels", "/api/labels", map[string]any{
			"name": "contract-disp-" + uniqueSuffix(), "color": "#ABCDEF",
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/labels (disposable): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		disposableLabel := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/labels/{id}", "/api/labels/"+mustStr(t, disposableLabel, "id"), nil)
		if status != 200 && status != 204 {
			t.Errorf("DELETE /api/labels/{id}: expected 200/204, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/properties", "/api/properties", nil)
		if status != 200 {
			t.Errorf("GET /api/properties: expected 200, got %d", status)
		}

		status, body = f.call(t, http.MethodPost, "/api/properties", "/api/properties", map[string]any{
			"name": "contract-prop-" + uniqueSuffix(),
			"type": "text",
		})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/properties: expected 200/201, got %d, body=%s", status, truncate(body))
		} else {
			prop := decodeJSON(t, body)
			propID := str(prop, "id")
			if propID != "" {
				status, _ = f.call(t, http.MethodGet, "/api/properties/{id}", "/api/properties/"+propID, nil)
				if status != 200 {
					t.Errorf("GET /api/properties/{id}: expected 200, got %d", status)
				}
				status, _ = f.call(t, http.MethodPatch, "/api/properties/{id}", "/api/properties/"+propID,
					map[string]any{"name": "contract-prop-renamed-" + uniqueSuffix()})
				if status != 200 {
					t.Errorf("PATCH /api/properties/{id}: expected 200, got %d", status)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// comments
// ---------------------------------------------------------------------------

func testComments(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureIssue(t)

		status, body := f.call(t, http.MethodPost, "/api/issues/{id}/comments", "/api/issues/"+f.issueID+"/comments",
			map[string]any{"content": "Contract comment " + uniqueSuffix()})
		if status != 201 {
			t.Fatalf("POST /api/issues/{id}/comments: expected 201, got %d, body=%s", status, truncate(body))
		}
		comment := decodeJSON(t, body)
		commentID := mustStr(t, comment, "id")

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/comments", "/api/issues/"+f.issueID+"/comments", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/comments: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/comments/{commentId}", "/api/comments/"+commentID,
			map[string]any{"content": "Contract comment (edited) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/comments/{commentId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/comments/{commentId}/reactions",
			"/api/comments/"+commentID+"/reactions", map[string]any{"emoji": "👍"})
		if status != 200 && status != 201 {
			t.Errorf("POST /api/comments/{commentId}/reactions: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodDelete, "/api/comments/{commentId}/reactions",
			"/api/comments/"+commentID+"/reactions", map[string]any{"emoji": "👍"})
		if status != 200 && status != 204 {
			t.Errorf("DELETE /api/comments/{commentId}/reactions: expected 200/204, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/comments/trigger-preview",
			"/api/issues/"+f.issueID+"/comments/trigger-preview", map[string]any{"content": "no mentions here"})
		if status != 200 {
			t.Errorf("POST /api/issues/{id}/comments/trigger-preview: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/issues/{id}/timeline", "/api/issues/"+f.issueID+"/timeline", nil)
		if status != 200 {
			t.Errorf("GET /api/issues/{id}/timeline: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/comments/{commentId}/resolve",
			"/api/comments/"+commentID+"/resolve", nil)
		if status != 200 {
			t.Errorf("POST /api/comments/{commentId}/resolve: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodDelete, "/api/comments/{commentId}/resolve",
			"/api/comments/"+commentID+"/resolve", nil)
		if status != 200 {
			t.Errorf("DELETE /api/comments/{commentId}/resolve: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/issues/{id}/subscribe", "/api/issues/"+f.issueID+"/subscribe", nil)
		if status != 200 {
			t.Errorf("POST /api/issues/{id}/subscribe: expected 200, got %d", status)
		}

		// deleteComment on a disposable comment of its own.
		status, body = f.call(t, http.MethodPost, "/api/issues/{id}/comments", "/api/issues/"+f.issueID+"/comments",
			map[string]any{"content": "Contract comment to delete " + uniqueSuffix()})
		if status != 201 {
			t.Fatalf("POST /api/issues/{id}/comments (disposable): expected 201, got %d, body=%s", status, truncate(body))
		}
		disposableComment := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/comments/{commentId}", "/api/comments/"+mustStr(t, disposableComment, "id"), nil)
		if status != 204 {
			t.Errorf("DELETE /api/comments/{commentId}: expected 204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// squads
// ---------------------------------------------------------------------------

func testSquads(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureSquad(t)

		status, _ := f.call(t, http.MethodGet, "/api/squads", "/api/squads", nil)
		if status != 200 {
			t.Errorf("GET /api/squads: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/squads/{id}", "/api/squads/"+f.squadID, nil)
		if status != 200 {
			t.Errorf("GET /api/squads/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/squads/{id}/members", "/api/squads/"+f.squadID+"/members", nil)
		if status != 200 {
			t.Errorf("GET /api/squads/{id}/members: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/squads/{id}/members/status",
			"/api/squads/"+f.squadID+"/members/status", nil)
		if status != 200 {
			t.Errorf("GET /api/squads/{id}/members/status: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/squads/{id}", "/api/squads/"+f.squadID,
			map[string]any{"name": "Contract squad (renamed) " + uniqueSuffix(), "leader_id": f.agentID})
		if status != 200 {
			t.Errorf("PUT /api/squads/{id}: expected 200, got %d", status)
		}

		// The caller (owner) joins their own squad as a human member, then has
		// their role/membership changed/removed — no second agent needed.
		status, _ = f.call(t, http.MethodPost, "/api/squads/{id}/members", "/api/squads/"+f.squadID+"/members",
			map[string]any{"member_type": "member", "member_id": f.userID})
		if status != 201 && status != 409 {
			t.Errorf("POST /api/squads/{id}/members: expected 201/409, got %d", status)
		}
		status, _ = f.call(t, http.MethodPatch, "/api/squads/{id}/members/role", "/api/squads/"+f.squadID+"/members/role",
			map[string]any{"member_type": "member", "member_id": f.userID, "role": "contributor"})
		if status != 200 {
			t.Errorf("PATCH /api/squads/{id}/members/role: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/squads/{id}/members", "/api/squads/"+f.squadID+"/members",
			map[string]any{"member_type": "member", "member_id": f.userID})
		if status != 204 {
			t.Errorf("DELETE /api/squads/{id}/members: expected 204, got %d", status)
		}

		// createSquad/deleteSquad on a disposable squad of their own.
		status, body := f.call(t, http.MethodPost, "/api/squads", "/api/squads", map[string]any{
			"name": "Contract disposable squad " + uniqueSuffix(), "leader_id": f.agentID,
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/squads (disposable): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		disposableSquad := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/squads/{id}", "/api/squads/"+mustStr(t, disposableSquad, "id"), nil)
		if status != 204 {
			t.Errorf("DELETE /api/squads/{id}: expected 204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// autopilots
// ---------------------------------------------------------------------------

func testAutopilots(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAgent(t)

		status, body := f.call(t, http.MethodPost, "/api/autopilots", "/api/autopilots", map[string]any{
			"title":          "Contract autopilot " + uniqueSuffix(),
			"assignee_id":    f.agentID,
			"execution_mode": "run_only",
		})
		if status != 201 {
			t.Fatalf("POST /api/autopilots: expected 201, got %d, body=%s", status, truncate(body))
		}
		autopilot := decodeJSON(t, body)
		autopilotID := mustStr(t, autopilot, "id")

		status, _ = f.call(t, http.MethodGet, "/api/autopilots", "/api/autopilots", nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}", "/api/autopilots/"+autopilotID, nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}/runs", "/api/autopilots/"+autopilotID+"/runs", nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/{id}/runs: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/cron-preview",
			"/api/autopilots/cron-preview?expr="+urlEscape("0 9 * * *"), nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/cron-preview: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/autopilots/{id}", "/api/autopilots/"+autopilotID,
			map[string]any{"title": "Contract autopilot (renamed) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/autopilots/{id}: expected 200, got %d", status)
		}

		status, body = f.call(t, http.MethodPost, "/api/autopilots/{id}/trigger", "/api/autopilots/"+autopilotID+"/trigger", nil)
		if status != 200 {
			t.Errorf("POST /api/autopilots/{id}/trigger: expected 200, got %d, body=%s", status, truncate(body))
		} else {
			run := decodeJSON(t, body)
			if runID := str(run, "id"); runID != "" {
				status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}/runs/{runId}",
					"/api/autopilots/"+autopilotID+"/runs/"+runID, nil)
				if status != 200 {
					t.Errorf("GET /api/autopilots/{id}/runs/{runId}: expected 200, got %d", status)
				}
			}
		}

		secondClient, secondEmail := f.ensureSecondUser(t)
		// addAutopilotCollaborator requires user_id to already be a workspace
		// member (docs/50-api-contract.yaml, addAutopilotCollaborator '400').
		// The shared second user may have been invited-and-removed again by
		// the "workspaces" subtest by the time this one runs, so (re-)invite
		// and accept here rather than assuming membership survived.
		if status, body := f.call(t, http.MethodPost, "/api/workspaces/{id}/members",
			"/api/workspaces/"+f.workspaceID+"/members", map[string]any{"email": secondEmail}); status == 201 || status == 200 {
			invite := decodeJSON(t, body)
			if inviteID := str(invite, "id"); inviteID != "" {
				f.callAs(t, secondClient, http.MethodPost, "/api/invitations/{id}/accept",
					"/api/invitations/"+inviteID+"/accept", nil)
			}
		}
		status, _ = f.call(t, http.MethodPost, "/api/autopilots/{id}/collaborators",
			"/api/autopilots/"+autopilotID+"/collaborators", map[string]any{"user_id": f.secondUserID})
		if status != 201 {
			t.Errorf("POST /api/autopilots/{id}/collaborators: expected 201, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/autopilots/{id}/collaborators/{userId}",
			"/api/autopilots/"+autopilotID+"/collaborators/"+f.secondUserID, nil)
		if status != 200 && status != 204 {
			t.Errorf("DELETE /api/autopilots/{id}/collaborators/{userId}: expected 200/204, got %d", status)
		}

		// A webhook trigger exercises the full AutopilotTriggers surface:
		// create -> update -> signing secret -> rotate token -> deliveries ->
		// receive a real webhook (invalid signature -> documented rejection)
		// -> delete.
		provider := "generic"
		status, body = f.call(t, http.MethodPost, "/api/autopilots/{id}/triggers", "/api/autopilots/"+autopilotID+"/triggers",
			map[string]any{"kind": "webhook", "provider": provider})
		if status != 201 {
			t.Fatalf("POST /api/autopilots/{id}/triggers: expected 201, got %d, body=%s", status, truncate(body))
		}
		trigger := decodeJSON(t, body)
		triggerID := mustStr(t, trigger, "id")
		webhookToken := str(trigger, "webhook_token")

		status, _ = f.call(t, http.MethodPatch, "/api/autopilots/{id}/triggers/{triggerId}",
			"/api/autopilots/"+autopilotID+"/triggers/"+triggerID, map[string]any{"label": "contract webhook trigger"})
		if status != 200 {
			t.Errorf("PATCH /api/autopilots/{id}/triggers/{triggerId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/autopilots/{id}/triggers/{triggerId}/signing-secret",
			"/api/autopilots/"+autopilotID+"/triggers/"+triggerID+"/signing-secret",
			map[string]any{"signing_secret": "contract-signing-secret-16plus"})
		if status != 200 {
			t.Errorf("PUT /api/autopilots/{id}/triggers/{triggerId}/signing-secret: expected 200, got %d", status)
		}

		status, body = f.call(t, http.MethodPost, "/api/autopilots/{id}/triggers/{triggerId}/rotate-webhook-token",
			"/api/autopilots/"+autopilotID+"/triggers/"+triggerID+"/rotate-webhook-token", nil)
		if status != 200 {
			t.Errorf("POST /api/autopilots/{id}/triggers/{triggerId}/rotate-webhook-token: expected 200, got %d", status)
		} else {
			rotated := decodeJSON(t, body)
			if newToken := str(rotated, "webhook_token"); newToken != "" {
				webhookToken = newToken
			}
		}

		if webhookToken != "" {
			// An unsigned body against a trigger that now requires a
			// signature: contract documents 401 "rejected" for exactly this
			// case, not 200.
			status, _ = f.call(t, http.MethodPost, "/api/webhooks/autopilots/{token}",
				"/api/webhooks/autopilots/"+webhookToken, map[string]any{"contract": "test"})
			if status != 401 {
				t.Errorf("POST /api/webhooks/autopilots/{token} (unsigned, secret set): expected 401, got %d", status)
			}
		}

		status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}/deliveries", "/api/autopilots/"+autopilotID+"/deliveries", nil)
		if status != 200 {
			t.Errorf("GET /api/autopilots/{id}/deliveries: expected 200, got %d", status)
		} else {
			_, delBody := f.call(t, http.MethodGet, "/api/autopilots/{id}/deliveries", "/api/autopilots/"+autopilotID+"/deliveries", nil)
			deliveries := decodeJSON(t, delBody)
			list, _ := deliveries["deliveries"].([]any)
			if len(list) > 0 {
				if d0, ok := list[0].(map[string]any); ok {
					deliveryID := str(d0, "id")
					if deliveryID != "" {
						status, _ = f.call(t, http.MethodGet, "/api/autopilots/{id}/deliveries/{deliveryId}",
							"/api/autopilots/"+autopilotID+"/deliveries/"+deliveryID, nil)
						if status != 200 {
							t.Errorf("GET /api/autopilots/{id}/deliveries/{deliveryId}: expected 200, got %d", status)
						}
						f.call(t, http.MethodPost, "/api/autopilots/{id}/deliveries/{deliveryId}/replay",
							"/api/autopilots/"+autopilotID+"/deliveries/"+deliveryID+"/replay", nil)
					}
				}
			}
		}

		status, _ = f.call(t, http.MethodDelete, "/api/autopilots/{id}/triggers/{triggerId}",
			"/api/autopilots/"+autopilotID+"/triggers/"+triggerID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/autopilots/{id}/triggers/{triggerId}: expected 204, got %d", status)
		}

		status, _ = f.call(t, http.MethodDelete, "/api/autopilots/{id}", "/api/autopilots/"+autopilotID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/autopilots/{id}: expected 204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// agents
// ---------------------------------------------------------------------------

func testAgents(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAgent(t)

		status, _ := f.call(t, http.MethodGet, "/api/agents", "/api/agents", nil)
		if status != 200 {
			t.Errorf("GET /api/agents: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}", "/api/agents/"+f.agentID, nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/skills", "/api/agents/"+f.agentID+"/skills", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/skills: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/mcp-servers", "/api/agents/"+f.agentID+"/mcp-servers", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/mcp-servers: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/tasks", "/api/agents/"+f.agentID+"/tasks", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/tasks: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agent-templates", "/api/agent-templates", nil)
		if status != 200 {
			t.Errorf("GET /api/agent-templates: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/agents/{id}/env", "/api/agents/"+f.agentID+"/env", nil)
		if status != 200 {
			t.Errorf("GET /api/agents/{id}/env: expected 200, got %d", status)
		}

		// Resource labels on agents are behind a feature flag that defaults
		// off, in which case the server documents 404 instead of 200.
		f.call(t, http.MethodGet, "/api/agents/{id}/labels", "/api/agents/"+f.agentID+"/labels", nil)
		f.call(t, http.MethodPost, "/api/agents/{id}/labels", "/api/agents/"+f.agentID+"/labels",
			map[string]any{"label_id": "00000000-0000-0000-0000-000000000000"})
		f.call(t, http.MethodDelete, "/api/agents/{id}/labels/{labelId}",
			"/api/agents/"+f.agentID+"/labels/00000000-0000-0000-0000-000000000000", nil)

		status, body := f.call(t, http.MethodPost, "/api/agent-builder/sessions", "/api/agent-builder/sessions",
			map[string]any{"runtime_id": f.runtimeID})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/agent-builder/sessions: expected 200/201, got %d", status)
		} else {
			session := decodeJSON(t, body)
			if sessionID := str(session, "id"); sessionID != "" {
				f.call(t, http.MethodPatch, "/api/agent-builder/sessions/{sessionId}/runtime",
					"/api/agent-builder/sessions/"+sessionID+"/runtime", map[string]any{"runtime_id": f.runtimeID})
			}
		}

		status, _ = f.call(t, http.MethodGet, "/api/agent-templates/{slug}", "/api/agent-templates/general-assistant", nil)
		if status != 200 && status != 404 {
			t.Errorf("GET /api/agent-templates/{slug}: expected 200/404, got %d", status)
		}

		// Non-destructive mutations on the shared fixture agent: skills,
		// runtime-skills, mcp-servers, env — other subtests only read
		// f.agentID's identity/runtime, not this state.
		status, body = f.call(t, http.MethodPost, "/api/skills", "/api/skills", map[string]any{
			"name": "contract-agent-skill-" + uniqueSuffix(),
		})
		var agentSkillID string
		if status == 200 || status == 201 {
			agentSkillID = str(decodeJSON(t, body), "id")
		}
		if agentSkillID != "" {
			status, _ = f.call(t, http.MethodPost, "/api/agents/{id}/skills/add", "/api/agents/"+f.agentID+"/skills/add",
				map[string]any{"skill_ids": []string{agentSkillID}})
			if status != 200 {
				t.Errorf("POST /api/agents/{id}/skills/add: expected 200, got %d", status)
			}
			status, _ = f.call(t, http.MethodPut, "/api/agents/{id}/skills/{skillId}/enabled",
				"/api/agents/"+f.agentID+"/skills/"+agentSkillID+"/enabled", map[string]any{"enabled": false})
			if status != 200 {
				t.Errorf("PUT /api/agents/{id}/skills/{skillId}/enabled: expected 200, got %d", status)
			}
			status, _ = f.call(t, http.MethodPut, "/api/agents/{id}/skills", "/api/agents/"+f.agentID+"/skills",
				map[string]any{"skill_ids": []string{agentSkillID}})
			if status != 200 {
				t.Errorf("PUT /api/agents/{id}/skills: expected 200, got %d", status)
			}
			status, _ = f.call(t, http.MethodDelete, "/api/agents/{id}/skills/{skillId}",
				"/api/agents/"+f.agentID+"/skills/"+agentSkillID, nil)
			if status != 200 {
				t.Errorf("DELETE /api/agents/{id}/skills/{skillId}: expected 200, got %d", status)
			}
		}

		// setAgentRuntimeSkillEnabled only accepts runtimes on one of the two
		// providers that actually ship built-in runtime skills
		// (docs/50-api-contract.yaml, setAgentRuntimeSkillEnabled description
		// - "провайдеры runtime-c/runtime-e"), so f.runtimeID (a generic
		// contract-test runtime) doesn't qualify: register a dedicated
		// runtime-c runtime and an agent bound to it just for this call.
		status, body = f.call(t, http.MethodPost, "/api/daemon/register", "/api/daemon/register", map[string]any{
			"workspace_id": f.workspaceID,
			"daemon_id":    "contract-daemon-rtc-" + uniqueSuffix(),
			"device_name":  "contract-test-runtime-c",
			"runtimes": []map[string]any{
				{"name": "contract-runtime-c", "type": "runtime-c", "status": "online"},
			},
		})
		if status != 200 {
			t.Fatalf("POST /api/daemon/register (runtime-c): expected 200, got %d, body=%s", status, truncate(body))
		}
		rtcResp := decodeJSON(t, body)
		rtcRuntimes, _ := rtcResp["runtimes"].([]any)
		if len(rtcRuntimes) == 0 {
			t.Fatalf("POST /api/daemon/register (runtime-c): expected at least one runtime, got %s", truncate(body))
		}
		rtcRuntime, _ := rtcRuntimes[0].(map[string]any)
		rtcRuntimeID := mustStr(t, rtcRuntime, "id")

		status, body = f.call(t, http.MethodPost, "/api/agents", "/api/agents", map[string]any{
			"name":       "Contract Agent runtime-c " + uniqueSuffix(),
			"runtime_id": rtcRuntimeID,
		})
		if status != 200 && status != 201 {
			t.Fatalf("POST /api/agents (runtime-c): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		rtcAgentID := mustStr(t, decodeJSON(t, body), "id")

		status, _ = f.call(t, http.MethodPut, "/api/agents/{id}/runtime-skills/enabled",
			"/api/agents/"+rtcAgentID+"/runtime-skills/enabled", map[string]any{
				"runtime_id": rtcRuntimeID, "root": "universal", "key": "contract-runtime-skill", "enabled": false,
			})
		if status != 204 {
			t.Errorf("PUT /api/agents/{id}/runtime-skills/enabled: expected 204, got %d", status)
		}

		status, body = f.call(t, http.MethodPost, "/api/workspace-mcp-servers", "/api/workspace-mcp-servers",
			map[string]any{
				"name":   "contract-mcp-" + uniqueSuffix(),
				"config": map[string]any{"transport": "http", "url": "https://mcp.contract-test.invalid"},
			})
		if status == 200 || status == 201 {
			mcpServerID := str(decodeJSON(t, body), "id")
			if mcpServerID != "" {
				status, _ = f.call(t, http.MethodPost, "/api/agents/{id}/mcp-servers", "/api/agents/"+f.agentID+"/mcp-servers",
					map[string]any{"server_id": mcpServerID})
				if status != 200 {
					t.Errorf("POST /api/agents/{id}/mcp-servers: expected 200, got %d", status)
				}
				status, _ = f.call(t, http.MethodPut, "/api/agents/{id}/mcp-servers/{serverId}/enabled",
					"/api/agents/"+f.agentID+"/mcp-servers/"+mcpServerID+"/enabled", map[string]any{"enabled": false})
				if status != 200 {
					t.Errorf("PUT /api/agents/{id}/mcp-servers/{serverId}/enabled: expected 200, got %d", status)
				}
				status, _ = f.call(t, http.MethodDelete, "/api/agents/{id}/mcp-servers/{serverId}",
					"/api/agents/"+f.agentID+"/mcp-servers/"+mcpServerID, nil)
				if status != 200 {
					t.Errorf("DELETE /api/agents/{id}/mcp-servers/{serverId}: expected 200, got %d", status)
				}
				f.call(t, http.MethodDelete, "/api/workspace-mcp-servers/{serverId}", "/api/workspace-mcp-servers/"+mcpServerID, nil)
			}
		}

		status, _ = f.call(t, http.MethodPut, "/api/agents/{id}/env", "/api/agents/"+f.agentID+"/env",
			map[string]any{"custom_env": map[string]any{"CONTRACT_VAR": "contract-value"}})
		if status != 200 {
			t.Errorf("PUT /api/agents/{id}/env: expected 200, got %d", status)
		}

		// A disposable agent of its own for the destructive/identity-changing
		// lifecycle (update/archive/restore/cancel-tasks) — f.agentID stays
		// untouched for chat/squads/autopilots subtests that reuse it.
		status, body = f.call(t, http.MethodPost, "/api/agents", "/api/agents", map[string]any{
			"name": "Contract lifecycle agent " + uniqueSuffix(), "runtime_id": f.runtimeID,
		})
		if status != 201 {
			t.Fatalf("POST /api/agents (lifecycle): expected 201, got %d, body=%s", status, truncate(body))
		}
		lifecycleAgent := decodeJSON(t, body)
		lifecycleAgentID := mustStr(t, lifecycleAgent, "id")

		status, _ = f.call(t, http.MethodPut, "/api/agents/{id}", "/api/agents/"+lifecycleAgentID,
			map[string]any{"description": "updated by contract suite"})
		if status != 200 {
			t.Errorf("PUT /api/agents/{id}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/agents/{id}/cancel-tasks", "/api/agents/"+lifecycleAgentID+"/cancel-tasks", nil)
		if status != 200 {
			t.Errorf("POST /api/agents/{id}/cancel-tasks: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/agents/{id}/archive", "/api/agents/"+lifecycleAgentID+"/archive", nil)
		if status != 200 {
			t.Errorf("POST /api/agents/{id}/archive: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/agents/{id}/restore", "/api/agents/"+lifecycleAgentID+"/restore", nil)
		if status != 200 {
			t.Errorf("POST /api/agents/{id}/restore: expected 200, got %d", status)
		}

		// createAgentFromTemplate: vendored templates ship without external
		// skill sources (see server2/docs/decisions.md), so this should
		// succeed without a network dependency.
		status, body = f.call(t, http.MethodPost, "/api/agents/from-template", "/api/agents/from-template",
			map[string]any{"template_slug": "general-assistant", "name": "Contract from template " + uniqueSuffix(),
				"runtime_id": f.runtimeID})
		if status != 201 && status != 400 && status != 422 {
			t.Errorf("POST /api/agents/from-template: expected 201/400/422, got %d, body=%s", status, truncate(body))
		}
	}
}

// ---------------------------------------------------------------------------
// runtimes
// ---------------------------------------------------------------------------

func testRuntimes(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureRuntime(t)

		status, _ := f.call(t, http.MethodGet, "/api/runtimes", "/api/runtimes", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/runtime-profiles",
			"/api/workspaces/"+f.workspaceID+"/runtime-profiles", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/runtime-profiles: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/usage",
			"/api/runtimes/"+f.runtimeID+"/usage", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes/{runtimeId}/usage: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/usage/by-agent",
			"/api/runtimes/"+f.runtimeID+"/usage/by-agent", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes/{runtimeId}/usage/by-agent: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/usage/by-hour",
			"/api/runtimes/"+f.runtimeID+"/usage/by-hour", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes/{runtimeId}/usage/by-hour: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/activity", "/api/runtimes/"+f.runtimeID+"/activity", nil)
		if status != 200 {
			t.Errorf("GET /api/runtimes/{runtimeId}/activity: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/runtimes/{runtimeId}", "/api/runtimes/"+f.runtimeID,
			map[string]any{"custom_name": "Contract runtime " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/runtimes/{runtimeId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/runtimes/{runtimeId}/mcp-verified",
			"/api/runtimes/"+f.runtimeID+"/mcp-verified", map[string]any{"server": "contract-mcp", "status": "ok"})
		if status != 204 {
			t.Errorf("POST /api/runtimes/{runtimeId}/mcp-verified: expected 204, got %d", status)
		}

		// Async runtime probes (self-update / model list / local skills /
		// local skill import): each initiate call gets a real request id
		// (the runtime registered by ensureRuntime is online), which is then
		// fed straight back through the matching daemon report-result
		// endpoint and read back via the matching get-status endpoint — the
		// same round trip a real daemon would perform.
		status, body := f.call(t, http.MethodPost, "/api/runtimes/{runtimeId}/update", "/api/runtimes/"+f.runtimeID+"/update",
			map[string]any{"target_version": "9.9.9-contract"})
		if status == 200 {
			updateID := str(decodeJSON(t, body), "id")
			if updateID != "" {
				f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/update/{updateId}/result",
					"/api/daemon/runtimes/"+f.runtimeID+"/update/"+updateID+"/result",
					map[string]any{"status": "completed"})
				status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/update/{updateId}",
					"/api/runtimes/"+f.runtimeID+"/update/"+updateID, nil)
				if status != 200 {
					t.Errorf("GET /api/runtimes/{runtimeId}/update/{updateId}: expected 200, got %d", status)
				}
			}
		} else if status != 403 && status != 409 {
			t.Errorf("POST /api/runtimes/{runtimeId}/update: expected 200/403/409, got %d, body=%s", status, truncate(body))
		}

		status, body = f.call(t, http.MethodPost, "/api/runtimes/{runtimeId}/models", "/api/runtimes/"+f.runtimeID+"/models", nil)
		if status == 200 {
			reqID := str(decodeJSON(t, body), "id")
			if reqID != "" {
				f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/models/{requestId}/result",
					"/api/daemon/runtimes/"+f.runtimeID+"/models/"+reqID+"/result",
					map[string]any{"status": "completed", "models": []map[string]any{}})
				status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/models/{requestId}",
					"/api/runtimes/"+f.runtimeID+"/models/"+reqID, nil)
				if status != 200 {
					t.Errorf("GET /api/runtimes/{runtimeId}/models/{requestId}: expected 200, got %d", status)
				}
			}
		} else if status != 503 {
			t.Errorf("POST /api/runtimes/{runtimeId}/models: expected 200/503, got %d, body=%s", status, truncate(body))
		}

		status, body = f.call(t, http.MethodPost, "/api/runtimes/{runtimeId}/local-skills",
			"/api/runtimes/"+f.runtimeID+"/local-skills", nil)
		var localSkillKey string
		if status == 200 {
			reqID := str(decodeJSON(t, body), "id")
			if reqID != "" {
				localSkillKey = "contract-local-skill-" + uniqueSuffix()
				f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/local-skills/{requestId}/result",
					"/api/daemon/runtimes/"+f.runtimeID+"/local-skills/"+reqID+"/result",
					map[string]any{"status": "completed", "skills": []map[string]any{
						{"key": localSkillKey, "name": "Contract Local Skill"},
					}})
				status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/local-skills/{requestId}",
					"/api/runtimes/"+f.runtimeID+"/local-skills/"+reqID, nil)
				if status != 200 {
					t.Errorf("GET /api/runtimes/{runtimeId}/local-skills/{requestId}: expected 200, got %d", status)
				}
			}
		} else if status != 503 {
			t.Errorf("POST /api/runtimes/{runtimeId}/local-skills: expected 200/503, got %d, body=%s", status, truncate(body))
		}

		if localSkillKey != "" {
			status, body = f.call(t, http.MethodPost, "/api/runtimes/{runtimeId}/local-skills/import",
				"/api/runtimes/"+f.runtimeID+"/local-skills/import", map[string]any{"skill_key": localSkillKey})
			if status == 200 {
				importID := str(decodeJSON(t, body), "id")
				if importID != "" {
					f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/local-skills/import/{requestId}/result",
						"/api/daemon/runtimes/"+f.runtimeID+"/local-skills/import/"+importID+"/result",
						map[string]any{"status": "completed", "skill": map[string]any{
							"key": localSkillKey, "name": "Contract Local Skill", "content": "# Contract Local Skill",
						}})
					status, _ = f.call(t, http.MethodGet, "/api/runtimes/{runtimeId}/local-skills/import/{requestId}",
						"/api/runtimes/"+f.runtimeID+"/local-skills/import/"+importID, nil)
					if status != 200 {
						t.Errorf("GET /api/runtimes/{runtimeId}/local-skills/import/{requestId}: expected 200, got %d", status)
					}
				}
			} else if status != 503 {
				t.Errorf("POST /api/runtimes/{runtimeId}/local-skills/import: expected 200/503, got %d, body=%s", status, truncate(body))
			}
		}

		// A disposable runtime (its own daemon registration) for
		// deleteRuntime/archiveAgentsAndDeleteRuntime — never f.runtimeID,
		// which chat/agents/daemon subtests still depend on.
		daemonID := "contract-daemon-disposable-" + uniqueSuffix()
		status, body = f.call(t, http.MethodPost, "/api/daemon/register", "/api/daemon/register", map[string]any{
			"workspace_id": f.workspaceID, "daemon_id": daemonID, "device_name": "contract-disposable",
			"runtimes": []map[string]any{{"name": "contract-disposable-runtime", "type": "claude", "status": "online"}},
		})
		if status == 200 {
			resp := decodeJSON(t, body)
			runtimes, _ := resp["runtimes"].([]any)
			if len(runtimes) > 0 {
				if rt, ok := runtimes[0].(map[string]any); ok {
					disposableRuntimeID := str(rt, "id")
					if disposableRuntimeID != "" {
						status, _ = f.call(t, http.MethodDelete, "/api/runtimes/{runtimeId}", "/api/runtimes/"+disposableRuntimeID, nil)
						if status != 200 {
							t.Errorf("DELETE /api/runtimes/{runtimeId}: expected 200, got %d", status)
						}
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// skills
// ---------------------------------------------------------------------------

func testSkills(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureSkill(t)

		status, _ := f.call(t, http.MethodGet, "/api/skills", "/api/skills", nil)
		if status != 200 {
			t.Errorf("GET /api/skills: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/skills/{id}", "/api/skills/"+f.skillID, nil)
		if status != 200 {
			t.Errorf("GET /api/skills/{id}: expected 200, got %d", status)
		}

		// Skill search can legitimately answer 502 in an environment with no
		// search backend configured; any documented status is fine here; the
		// schema check inside f.call already covers correctness.
		f.call(t, http.MethodGet, "/api/skills/search", "/api/skills/search?q=contract", nil)

		status, _ = f.call(t, http.MethodGet, "/api/skills/{id}/files", "/api/skills/"+f.skillID+"/files", nil)
		if status != 200 {
			t.Errorf("GET /api/skills/{id}/files: expected 200, got %d", status)
		}

		// Same feature-flagged behavior as agent labels.
		f.call(t, http.MethodGet, "/api/skills/{id}/labels", "/api/skills/"+f.skillID+"/labels", nil)
		f.call(t, http.MethodPost, "/api/skills/{id}/labels", "/api/skills/"+f.skillID+"/labels",
			map[string]any{"label_id": "00000000-0000-0000-0000-000000000000"})
		f.call(t, http.MethodDelete, "/api/skills/{id}/labels/{labelId}",
			"/api/skills/"+f.skillID+"/labels/00000000-0000-0000-0000-000000000000", nil)

		status, _ = f.call(t, http.MethodPut, "/api/skills/{id}", "/api/skills/"+f.skillID,
			map[string]any{"name": "contract-skill-renamed-" + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PUT /api/skills/{id}: expected 200, got %d", status)
		}

		status, body := f.call(t, http.MethodPut, "/api/skills/{id}/files", "/api/skills/"+f.skillID+"/files",
			map[string]any{"path": "notes/contract.md", "content": "# Contract note"})
		if status != 200 {
			t.Errorf("PUT /api/skills/{id}/files: expected 200, got %d", status)
		} else {
			skillFile := decodeJSON(t, body)
			if fileID := str(skillFile, "id"); fileID != "" {
				status, _ = f.call(t, http.MethodDelete, "/api/skills/{id}/files/{fileId}",
					"/api/skills/"+f.skillID+"/files/"+fileID, nil)
				if status != 204 {
					t.Errorf("DELETE /api/skills/{id}/files/{fileId}: expected 204, got %d", status)
				}
			}
		}

		// importSkill: an unreachable/unsupported source is a documented
		// failure (400/403/502), not a crash — no network dependency needed.
		f.call(t, http.MethodPost, "/api/skills/import", "/api/skills/import",
			map[string]any{"url": "https://example.invalid/not-a-real-skill"})

		// createSkill/deleteSkill on a disposable skill of their own.
		status, body = f.call(t, http.MethodPost, "/api/skills", "/api/skills", map[string]any{
			"name": "contract-disposable-skill-" + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/skills (disposable): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		disposableSkill := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/skills/{id}", "/api/skills/"+mustStr(t, disposableSkill, "id"), nil)
		if status != 200 && status != 204 {
			t.Errorf("DELETE /api/skills/{id}: expected 200/204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// chat
// ---------------------------------------------------------------------------

func testChat(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureChatSession(t)

		status, _ := f.call(t, http.MethodGet, "/api/chat/sessions", "/api/chat/sessions", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}",
			"/api/chat/sessions/"+f.chatSessionID, nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/chat/sessions/{sessionId}/messages",
			"/api/chat/sessions/"+f.chatSessionID+"/messages", map[string]any{
				"content": "Hello from the contract suite " + uniqueSuffix(),
			})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/chat/sessions/{sessionId}/messages: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}/messages",
			"/api/chat/sessions/"+f.chatSessionID+"/messages", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}/messages: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/pending-tasks", "/api/chat/pending-tasks", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/pending-tasks: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/pinned-agents", "/api/chat/pinned-agents", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/pinned-agents: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/chat/pending-tasks/has-any", "/api/chat/pending-tasks/has-any", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/pending-tasks/has-any: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}/pending-task",
			"/api/chat/sessions/"+f.chatSessionID+"/pending-task", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}/pending-task: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}/messages/page",
			"/api/chat/sessions/"+f.chatSessionID+"/messages/page", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}/messages/page: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/chat/sessions/{sessionId}/read",
			"/api/chat/sessions/"+f.chatSessionID+"/read", nil)
		if status != 204 {
			t.Errorf("POST /api/chat/sessions/{sessionId}/read: expected 204, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/chat/sessions/{sessionId}/draft-restores",
			"/api/chat/sessions/"+f.chatSessionID+"/draft-restores", nil)
		if status != 200 {
			t.Errorf("GET /api/chat/sessions/{sessionId}/draft-restores: expected 200, got %d", status)
		}
		f.call(t, http.MethodDelete, "/api/chat/sessions/{sessionId}/draft-restores/{restoreId}",
			"/api/chat/sessions/"+f.chatSessionID+"/draft-restores/00000000-0000-0000-0000-000000000000", nil)

		status, _ = f.call(t, http.MethodPatch, "/api/chat/sessions/{sessionId}", "/api/chat/sessions/"+f.chatSessionID,
			map[string]any{"title": "Contract chat (renamed) " + uniqueSuffix()})
		if status != 200 {
			t.Errorf("PATCH /api/chat/sessions/{sessionId}: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPatch, "/api/chat/sessions/{sessionId}/pin", "/api/chat/sessions/"+f.chatSessionID+"/pin",
			map[string]any{"pinned": true})
		if status != 200 {
			t.Errorf("PATCH /api/chat/sessions/{sessionId}/pin: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPatch, "/api/chat/sessions/{sessionId}/archive", "/api/chat/sessions/"+f.chatSessionID+"/archive",
			map[string]any{"archived": true})
		if status != 200 {
			t.Errorf("PATCH /api/chat/sessions/{sessionId}/archive: expected 200, got %d", status)
		}
		f.call(t, http.MethodPatch, "/api/chat/sessions/{sessionId}/archive", "/api/chat/sessions/"+f.chatSessionID+"/archive",
			map[string]any{"archived": false})

		status, _ = f.call(t, http.MethodPost, "/api/chat/pinned-agents", "/api/chat/pinned-agents",
			map[string]any{"agent_id": f.agentID})
		if status != 200 {
			t.Errorf("POST /api/chat/pinned-agents: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/chat/pinned-agents/{agentId}", "/api/chat/pinned-agents/"+f.agentID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/chat/pinned-agents/{agentId}: expected 204, got %d", status)
		}

		// deleteChatSession on a disposable session of its own.
		status, body := f.call(t, http.MethodPost, "/api/chat/sessions", "/api/chat/sessions",
			map[string]any{"agent_id": f.agentID})
		if status != 201 && status != 200 {
			t.Fatalf("POST /api/chat/sessions (disposable): expected 200/201, got %d, body=%s", status, truncate(body))
		}
		disposableSession := decodeJSON(t, body)
		status, _ = f.call(t, http.MethodDelete, "/api/chat/sessions/{sessionId}",
			"/api/chat/sessions/"+mustStr(t, disposableSession, "id"), nil)
		if status != 204 {
			t.Errorf("DELETE /api/chat/sessions/{sessionId}: expected 204, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// inbox
// ---------------------------------------------------------------------------

func testInbox(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		status, _ := f.call(t, http.MethodGet, "/api/inbox", "/api/inbox", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/inbox/unread-count", "/api/inbox/unread-count", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox/unread-count: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/inbox/archived", "/api/inbox/archived", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox/archived: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/notification-preferences", "/api/notification-preferences", nil)
		if status != 200 {
			t.Errorf("GET /api/notification-preferences: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPatch, "/api/notification-preferences", "/api/notification-preferences",
			map[string]any{"preferences": map[string]any{"comments": "muted"}})
		if status != 200 {
			t.Errorf("PATCH /api/notification-preferences: expected 200, got %d", status)
		}

		f.ensureIssue(t)
		status, _ = f.call(t, http.MethodPost, "/api/pins", "/api/pins", map[string]any{
			"item_type": "issue",
			"item_id":   f.issueID,
		})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/pins: expected 200/201, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/pins", "/api/pins", nil)
		if status != 200 {
			t.Errorf("GET /api/pins: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/notification-preferences", "/api/notification-preferences",
			map[string]any{"preferences": map[string]any{"comments": "all"}})
		if status != 200 {
			t.Errorf("PUT /api/notification-preferences: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/inbox/unread-summary", "/api/inbox/unread-summary", nil)
		if status != 200 {
			t.Errorf("GET /api/inbox/unread-summary: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/inbox/mark-all-read", "/api/inbox/mark-all-read", nil)
		if status != 200 {
			t.Errorf("POST /api/inbox/mark-all-read: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/inbox/archive-all-read", "/api/inbox/archive-all-read", nil)
		if status != 200 {
			t.Errorf("POST /api/inbox/archive-all-read: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/inbox/archive-completed", "/api/inbox/archive-completed", nil)
		if status != 200 {
			t.Errorf("POST /api/inbox/archive-completed: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPost, "/api/inbox/archive-all", "/api/inbox/archive-all", nil)
		if status != 200 {
			t.Errorf("POST /api/inbox/archive-all: expected 200, got %d", status)
		}
		// No inbox items of our own are guaranteed to exist (a single-user
		// workspace rarely notifies its own owner) — a random id still
		// exercises the operation against its documented 404.
		f.call(t, http.MethodPost, "/api/inbox/{id}/read", "/api/inbox/00000000-0000-0000-0000-000000000000/read", nil)
		f.call(t, http.MethodPost, "/api/inbox/{id}/archive", "/api/inbox/00000000-0000-0000-0000-000000000000/archive", nil)
		f.call(t, http.MethodPost, "/api/inbox/{id}/unarchive", "/api/inbox/00000000-0000-0000-0000-000000000000/unarchive", nil)

		status, _ = f.call(t, http.MethodPut, "/api/pins/reorder", "/api/pins/reorder",
			map[string]any{"items": []map[string]any{{"id": f.issueID, "position": 1}}})
		if status != 204 {
			t.Errorf("PUT /api/pins/reorder: expected 204, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/pins/{itemType}/{itemId}", "/api/pins/issue/"+f.issueID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/pins/{itemType}/{itemId}: expected 204, got %d", status)
		}

		// Attachments, driven off the file testMe uploaded (if it succeeded).
		if f.uploadedAttachmentID != "" {
			status, _ = f.call(t, http.MethodGet, "/api/attachments/{id}", "/api/attachments/"+f.uploadedAttachmentID, nil)
			if status != 200 {
				t.Errorf("GET /api/attachments/{id}: expected 200, got %d", status)
			}
			f.call(t, http.MethodGet, "/api/attachments/{id}/content", "/api/attachments/"+f.uploadedAttachmentID+"/content", nil)
			f.call(t, http.MethodGet, "/api/attachments/{id}/download", "/api/attachments/"+f.uploadedAttachmentID+"/download", nil)
			status, _ = f.call(t, http.MethodDelete, "/api/attachments/{id}", "/api/attachments/"+f.uploadedAttachmentID, nil)
			if status != 204 {
				t.Errorf("DELETE /api/attachments/{id}: expected 204, got %d", status)
			}
		}
		f.call(t, http.MethodGet, "/uploads/{key}", "/uploads/contract-test-nonexistent-key.txt", nil)
	}
}

// ---------------------------------------------------------------------------
// daemon
// ---------------------------------------------------------------------------

func testDaemon(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureRuntime(t)

		status, _ := f.call(t, http.MethodPost, "/api/daemon/heartbeat", "/api/daemon/heartbeat",
			map[string]any{"runtime_id": f.runtimeID})
		if status != 200 {
			t.Errorf("POST /api/daemon/heartbeat: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/workspaces", "/api/daemon/workspaces", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/workspaces: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/workspaces/{workspaceId}/repos",
			"/api/daemon/workspaces/"+f.workspaceID+"/repos", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/workspaces/{workspaceId}/repos: expected 200, got %d", status)
		}

		// Claim with an empty queue: still a fully valid, schema-checked
		// 200 response ({"tasks": []}), without needing a real task queued.
		status, _ = f.call(t, http.MethodPost, "/api/daemon/tasks/claim", "/api/daemon/tasks/claim",
			map[string]any{
				"daemon_id":   "contract-daemon-" + uniqueSuffix(),
				"runtime_ids": []string{f.runtimeID},
				"max_tasks":   1,
			})
		if status != 200 {
			t.Errorf("POST /api/daemon/tasks/claim: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/runtimes/{runtimeId}/tasks/pending",
			"/api/daemon/runtimes/"+f.runtimeID+"/tasks/pending", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/runtimes/{runtimeId}/tasks/pending: expected 200, got %d", status)
		}

		// A daemon call with an invalid token: no Authorization at all is a
		// documented 401.
		anon := newAPIClient(f.baseURL)
		status, _ = f.callAs(t, anon, http.MethodPost, "/api/daemon/heartbeat", "/api/daemon/heartbeat",
			map[string]any{"runtime_id": f.runtimeID})
		if status != 401 {
			t.Errorf("POST /api/daemon/heartbeat with no auth: expected 401, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/daemon/workspaces/{workspaceId}/runtime-profiles",
			"/api/daemon/workspaces/"+f.workspaceID+"/runtime-profiles", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/workspaces/{workspaceId}/runtime-profiles: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/daemon/claim", "/api/daemon/claim", map[string]any{
			"daemon_id": "contract-daemon-" + uniqueSuffix(), "runtime_ids": []string{f.runtimeID}, "max_tasks": 1,
		})
		if status != 200 {
			t.Errorf("POST /api/daemon/claim: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/tasks/claim",
			"/api/daemon/runtimes/"+f.runtimeID+"/tasks/claim", nil)
		if status != 200 {
			t.Errorf("POST /api/daemon/runtimes/{runtimeId}/tasks/claim: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/recover-orphans",
			"/api/daemon/runtimes/"+f.runtimeID+"/recover-orphans", nil)
		if status != 200 {
			t.Errorf("POST /api/daemon/runtimes/{runtimeId}/recover-orphans: expected 200, got %d", status)
		}

		f.ensureIssue(t)
		status, _ = f.call(t, http.MethodPost, "/api/daemon/workspaces/{workspaceId}/issues/gc-check",
			"/api/daemon/workspaces/"+f.workspaceID+"/issues/gc-check", map[string]any{"issue_ids": []string{f.issueID}})
		if status != 200 {
			t.Errorf("POST /api/daemon/workspaces/{workspaceId}/issues/gc-check: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/daemon/issues/{issueId}/gc-check", "/api/daemon/issues/"+f.issueID+"/gc-check", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/issues/{issueId}/gc-check: expected 200, got %d", status)
		}

		f.ensureChatSession(t)
		status, _ = f.call(t, http.MethodGet, "/api/daemon/chat-sessions/{sessionId}/gc-check",
			"/api/daemon/chat-sessions/"+f.chatSessionID+"/gc-check", nil)
		if status != 200 {
			t.Errorf("GET /api/daemon/chat-sessions/{sessionId}/gc-check: expected 200, got %d", status)
		}

		const unknownID = "00000000-0000-0000-0000-000000000000"
		f.call(t, http.MethodGet, "/api/daemon/autopilot-runs/{runId}/gc-check", "/api/daemon/autopilot-runs/"+unknownID+"/gc-check", nil)
		f.call(t, http.MethodGet, "/api/daemon/tasks/{taskId}/gc-check", "/api/daemon/tasks/"+unknownID+"/gc-check", nil)
		f.call(t, http.MethodGet, "/api/daemon/tasks/{taskId}/status", "/api/daemon/tasks/"+unknownID+"/status", nil)
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/start", "/api/daemon/tasks/"+unknownID+"/start", nil)
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/wait-local-directory",
			"/api/daemon/tasks/"+unknownID+"/wait-local-directory", map[string]any{"reason": "waiting for a local checkout"})
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/progress", "/api/daemon/tasks/"+unknownID+"/progress",
			map[string]any{"summary": "contract progress"})
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/complete", "/api/daemon/tasks/"+unknownID+"/complete",
			map[string]any{"output": "contract output"})
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/fail", "/api/daemon/tasks/"+unknownID+"/fail",
			map[string]any{"error": "contract error"})
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/usage", "/api/daemon/tasks/"+unknownID+"/usage",
			map[string]any{"usage": []map[string]any{}})
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/messages", "/api/daemon/tasks/"+unknownID+"/messages",
			map[string]any{"messages": []map[string]any{}})
		f.call(t, http.MethodGet, "/api/daemon/tasks/{taskId}/messages", "/api/daemon/tasks/"+unknownID+"/messages", nil)
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/cancel-ack", "/api/daemon/tasks/"+unknownID+"/cancel-ack", nil)
		f.call(t, http.MethodPost, "/api/daemon/tasks/{taskId}/session", "/api/daemon/tasks/"+unknownID+"/session",
			map[string]any{"session_id": "contract-session"})
		f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/tasks/{taskId}/prepare-lease",
			"/api/daemon/runtimes/"+f.runtimeID+"/tasks/"+unknownID+"/prepare-lease", nil)
		f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/tasks/{taskId}/skill-bundles/resolve",
			"/api/daemon/runtimes/"+f.runtimeID+"/tasks/"+unknownID+"/skill-bundles/resolve", map[string]any{"skills": []map[string]any{}})
		f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/update/{updateId}/result",
			"/api/daemon/runtimes/"+f.runtimeID+"/update/unknown-request-id/result", map[string]any{"status": "completed"})
		f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/models/{requestId}/result",
			"/api/daemon/runtimes/"+f.runtimeID+"/models/unknown-request-id/result", map[string]any{"status": "completed"})
		f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/local-skills/{requestId}/result",
			"/api/daemon/runtimes/"+f.runtimeID+"/local-skills/unknown-request-id/result", map[string]any{"status": "completed"})
		f.call(t, http.MethodPost, "/api/daemon/runtimes/{runtimeId}/local-skills/import/{requestId}/result",
			"/api/daemon/runtimes/"+f.runtimeID+"/local-skills/import/unknown-request-id/result", map[string]any{"status": "completed"})

		// register/deregister on a daemon of their own — never f.runtimeID,
		// which the rest of this suite (and the "runtimes" subtest above)
		// still depends on.
		disposableDaemonID := "contract-daemon-reg-" + uniqueSuffix()
		status, body := f.call(t, http.MethodPost, "/api/daemon/register", "/api/daemon/register", map[string]any{
			"workspace_id": f.workspaceID, "daemon_id": disposableDaemonID, "device_name": "contract-reg-test",
			"runtimes": []map[string]any{{"name": "contract-reg-runtime", "type": "claude", "status": "online"}},
		})
		if status != 200 {
			t.Fatalf("POST /api/daemon/register (disposable): expected 200, got %d, body=%s", status, truncate(body))
		}
		regResp := decodeJSON(t, body)
		var disposableRuntimeIDs []string
		if runtimes, ok := regResp["runtimes"].([]any); ok {
			for _, rt := range runtimes {
				if m, ok := rt.(map[string]any); ok {
					if id := str(m, "id"); id != "" {
						disposableRuntimeIDs = append(disposableRuntimeIDs, id)
					}
				}
			}
		}
		status, _ = f.call(t, http.MethodPost, "/api/daemon/deregister", "/api/daemon/deregister",
			map[string]any{"runtime_ids": disposableRuntimeIDs})
		if status != 200 {
			t.Errorf("POST /api/daemon/deregister: expected 200, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// deployment (deployment-admin-only surface; this suite's user is never a
// deployment admin, so these calls exercise the documented 403 path -- see
// docs/50-api-contract.yaml, which documents 403 on all of them precisely
// because of this check).
// ---------------------------------------------------------------------------

func testDeployment(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureAuth(t)

		status, _ := f.call(t, http.MethodGet, "/api/deployment/admins", "/api/deployment/admins", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/admins: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/admins/pending", "/api/deployment/admins/pending", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/admins/pending: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/audit", "/api/deployment/audit", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/audit: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/workspaces", "/api/deployment/workspaces", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/workspaces: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/fleet", "/api/deployment/fleet", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/fleet: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment/mcp-servers", "/api/deployment/mcp-servers", nil)
		if status != 403 && status != 200 {
			t.Errorf("GET /api/deployment/mcp-servers: expected 200 or 403, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment-policy", "/api/deployment-policy", nil)
		if status != 200 {
			t.Errorf("GET /api/deployment-policy: expected 200, got %d", status)
		}

		// The rest of DeploymentAdmin/DeploymentMcp: this suite's user is
		// never a deployment admin, so every one of these is exercised on
		// its documented 403 path (or another documented failure, such as
		// 404 for a userId that also isn't a deployment admin) — the schema
		// check inside f.call is what actually matters here.
		f.call(t, http.MethodGet, "/api/deployment/policy", "/api/deployment/policy", nil)
		f.call(t, http.MethodPut, "/api/deployment/policy", "/api/deployment/policy", map[string]any{"body": map[string]any{}})
		f.call(t, http.MethodGet, "/api/deployment/client-secrets", "/api/deployment/client-secrets", nil)
		f.call(t, http.MethodGet, "/api/deployment/join-targets", "/api/deployment/join-targets", nil)
		f.call(t, http.MethodPost, "/api/deployment/join-targets/{workspaceId}/join",
			"/api/deployment/join-targets/"+f.workspaceID+"/join", nil)
		f.call(t, http.MethodPatch, "/api/deployment/workspaces/{workspaceId}", "/api/deployment/workspaces/"+f.workspaceID,
			map[string]any{"open_join": true})
		f.call(t, http.MethodGet, "/api/deployment/workspaces/{workspaceId}/members",
			"/api/deployment/workspaces/"+f.workspaceID+"/members", nil)
		f.call(t, http.MethodGet, "/api/deployment/workspaces/{workspaceId}/config",
			"/api/deployment/workspaces/"+f.workspaceID+"/config", nil)
		f.call(t, http.MethodPut, "/api/deployment/workspaces/{workspaceId}/config",
			"/api/deployment/workspaces/"+f.workspaceID+"/config", map[string]any{})
		f.call(t, http.MethodGet, "/api/deployment/workspaces/{workspaceId}/config/overrides",
			"/api/deployment/workspaces/"+f.workspaceID+"/config/overrides", nil)
		f.call(t, http.MethodGet, "/api/deployment/workspaces/{workspaceId}/config/overrides/{userId}",
			"/api/deployment/workspaces/"+f.workspaceID+"/config/overrides/"+f.userID, nil)
		f.call(t, http.MethodPut, "/api/deployment/workspaces/{workspaceId}/config/overrides/{userId}",
			"/api/deployment/workspaces/"+f.workspaceID+"/config/overrides/"+f.userID, map[string]any{})
		f.call(t, http.MethodDelete, "/api/deployment/workspaces/{workspaceId}/config/overrides/{userId}",
			"/api/deployment/workspaces/"+f.workspaceID+"/config/overrides/"+f.userID, nil)
		f.call(t, http.MethodPost, "/api/deployment/admins", "/api/deployment/admins", map[string]any{"email": f.email})
		f.call(t, http.MethodDelete, "/api/deployment/admins/{userId}", "/api/deployment/admins/"+f.userID, nil)
		f.call(t, http.MethodPost, "/api/deployment/users/{userId}/deactivate", "/api/deployment/users/"+f.userID+"/deactivate", nil)
		f.call(t, http.MethodPost, "/api/deployment/users/{userId}/reactivate", "/api/deployment/users/"+f.userID+"/reactivate", nil)
		f.call(t, http.MethodDelete, "/api/deployment/users/{userId}", "/api/deployment/users/"+f.userID, nil)
		f.call(t, http.MethodPost, "/api/deployment/users/{userId}/revoke-sessions", "/api/deployment/users/"+f.userID+"/revoke-sessions", nil)
		f.call(t, http.MethodPost, "/api/deployment/mcp-servers", "/api/deployment/mcp-servers",
			map[string]any{"name": "contract-deployment-mcp", "config": map[string]any{"transport": "http"}})
		f.call(t, http.MethodPut, "/api/deployment/mcp-servers/{serverId}",
			"/api/deployment/mcp-servers/00000000-0000-0000-0000-000000000000", map[string]any{"name": "contract"})
		f.call(t, http.MethodDelete, "/api/deployment/mcp-servers/{serverId}",
			"/api/deployment/mcp-servers/00000000-0000-0000-0000-000000000000", nil)
	}
}

// ---------------------------------------------------------------------------
// admin (workspace-scoped provisioning/config admin surface)
// ---------------------------------------------------------------------------

func testAdmin(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		status, _ := f.call(t, http.MethodGet, "/api/status", "/api/status", nil)
		if status != 200 {
			t.Errorf("GET /api/status: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/effective-config", "/api/effective-config", nil)
		if status != 200 {
			t.Errorf("GET /api/effective-config: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspace-config", "/api/workspace-config", nil)
		if status != 200 {
			t.Errorf("GET /api/workspace-config: expected 200, got %d", status)
		}

		// Provisioning can legitimately answer 503 when no provisioning
		// store is configured for this deployment; any documented status is
		// fine, the schema check inside f.call already covers correctness.
		f.call(t, http.MethodGet, "/api/provisioning/manifest", "/api/provisioning/manifest", nil)
		f.call(t, http.MethodGet, "/api/provisioning/catalog", "/api/provisioning/catalog", nil)
		f.call(t, http.MethodGet, "/api/provisioning/blob/{name}/{version}",
			"/api/provisioning/blob/contract-pkg/1.0.0?platform=linux-x64", nil)
		status, _ = f.call(t, http.MethodGet, "/api/provisioning/pins", "/api/provisioning/pins", nil)
		if status != 200 {
			t.Errorf("GET /api/provisioning/pins: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodPut, "/api/provisioning/pins", "/api/provisioning/pins",
			map[string]any{"pins": []map[string]any{}})
		if status != 200 {
			t.Errorf("PUT /api/provisioning/pins: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodPut, "/api/workspace-config", "/api/workspace-config",
			map[string]any{"llm_model": "gpt-contract-test"})
		if status != 200 {
			t.Errorf("PUT /api/workspace-config: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodGet, "/api/llm/health", "/api/llm/health", nil)
		if status != 200 {
			t.Errorf("GET /api/llm/health: expected 200, got %d", status)
		}

		f.call(t, http.MethodGet, "/api/workspace-config/overrides/{userId}", "/api/workspace-config/overrides/"+f.userID, nil)
		status, _ = f.call(t, http.MethodPut, "/api/workspace-config/overrides/{userId}",
			"/api/workspace-config/overrides/"+f.userID, map[string]any{"llm_model": "gpt-contract-override"})
		if status != 200 {
			t.Errorf("PUT /api/workspace-config/overrides/{userId}: expected 200, got %d", status)
		}
		status, _ = f.call(t, http.MethodDelete, "/api/workspace-config/overrides/{userId}",
			"/api/workspace-config/overrides/"+f.userID, nil)
		if status != 204 {
			t.Errorf("DELETE /api/workspace-config/overrides/{userId}: expected 204, got %d", status)
		}

		status, body := f.call(t, http.MethodPost, "/api/workspace-mcp-servers", "/api/workspace-mcp-servers",
			map[string]any{
				"name":   "contract-admin-mcp-" + uniqueSuffix(),
				"config": map[string]any{"transport": "http", "url": "https://mcp.contract-test.invalid"},
			})
		if status == 200 || status == 201 {
			serverID := str(decodeJSON(t, body), "id")
			if serverID != "" {
				status, _ = f.call(t, http.MethodPut, "/api/workspace-mcp-servers/{serverId}",
					"/api/workspace-mcp-servers/"+serverID, map[string]any{
						"name":   "contract-admin-mcp-renamed-" + uniqueSuffix(),
						"config": map[string]any{"transport": "http", "url": "https://mcp.contract-test.invalid/2"},
					})
				if status != 200 {
					t.Errorf("PUT /api/workspace-mcp-servers/{serverId}: expected 200, got %d", status)
				}
				f.call(t, http.MethodPut, "/api/workspace-mcp-servers/{serverId}/credentials",
					"/api/workspace-mcp-servers/"+serverID+"/credentials", map[string]any{"values": map[string]any{}})
				f.call(t, http.MethodDelete, "/api/workspace-mcp-servers/{serverId}/credentials",
					"/api/workspace-mcp-servers/"+serverID+"/credentials", nil)
				status, _ = f.call(t, http.MethodDelete, "/api/workspace-mcp-servers/{serverId}", "/api/workspace-mcp-servers/"+serverID, nil)
				if status != 204 {
					t.Errorf("DELETE /api/workspace-mcp-servers/{serverId}: expected 204, got %d", status)
				}
			}
		}

		status, _ = f.call(t, http.MethodGet, "/api/deployment-mcp-servers", "/api/deployment-mcp-servers", nil)
		if status != 200 {
			t.Errorf("GET /api/deployment-mcp-servers: expected 200, got %d", status)
		}
		f.call(t, http.MethodPut, "/api/deployment-mcp-servers/{serverId}/enabled",
			"/api/deployment-mcp-servers/00000000-0000-0000-0000-000000000000/enabled", map[string]any{"enabled": true})

		for _, path := range []string{
			"/api/dashboard/agent-runtime", "/api/dashboard/failures/by-agent", "/api/dashboard/failures/daily",
			"/api/dashboard/runtime/daily", "/api/dashboard/usage/by-agent",
			"/api/working-agents", "/api/agent-activity-30d", "/api/agent-run-counts", "/api/agent-task-snapshot",
		} {
			status, _ = f.call(t, http.MethodGet, path, path, nil)
			if status != 200 {
				t.Errorf("GET %s: expected 200, got %d", path, status)
			}
		}

		status, _ = f.call(t, http.MethodGet, "/health/realtime", "/health/realtime", nil)
		if status != 200 && status != 401 && status != 404 {
			t.Errorf("GET /health/realtime: expected 200/401/404, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// public (no-auth: health probes, public config, contact-sales lead form)
// ---------------------------------------------------------------------------

func testPublic(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		anon := newAPIClient(f.baseURL)

		status, _ := f.callAs(t, anon, http.MethodGet, "/health", "/health", nil)
		if status != 200 {
			t.Errorf("GET /health: expected 200, got %d", status)
		}
		status, _ = f.callAs(t, anon, http.MethodGet, "/readyz", "/readyz", nil)
		if status != 200 && status != 503 {
			t.Errorf("GET /readyz: expected 200/503, got %d", status)
		}
		status, _ = f.callAs(t, anon, http.MethodGet, "/healthz", "/healthz", nil)
		if status != 200 && status != 503 {
			t.Errorf("GET /healthz: expected 200/503, got %d", status)
		}
		status, _ = f.callAs(t, anon, http.MethodGet, "/api/config", "/api/config", nil)
		if status != 200 {
			t.Errorf("GET /api/config: expected 200, got %d", status)
		}

		status, _ = f.callAs(t, anon, http.MethodPost, "/api/contact-sales", "/api/contact-sales", map[string]any{
			"first_name": "Contract", "last_name": "Suite",
			"business_email": fmt.Sprintf("contract-sales-%s@contract-test-company.example", uniqueSuffix()),
			"company_name":   "Contract Test Co", "company_size": "1-10",
			"country_region": "Nowhere", "use_case": "evaluate",
		})
		if status != 201 && status != 429 {
			t.Errorf("POST /api/contact-sales: expected 201/429, got %d", status)
		}
	}
}

// ---------------------------------------------------------------------------
// integrations
// ---------------------------------------------------------------------------

func testIntegrations(f *fixture) func(t *testing.T) {
	return func(t *testing.T) {
		f.ensureWorkspace(t)

		// Composio is an optional integration; when it is not configured on
		// this deployment the server documents 502/503 here instead of 200.
		// Any documented status is fine, the schema check inside f.call
		// already covers correctness.
		f.call(t, http.MethodGet, "/api/integrations/composio/toolkits",
			"/api/integrations/composio/toolkits", nil)
		f.call(t, http.MethodGet, "/api/integrations/composio/connections",
			"/api/integrations/composio/connections", nil)

		status, _ := f.call(t, http.MethodGet, "/api/workspaces/{id}/github/installations",
			"/api/workspaces/"+f.workspaceID+"/github/installations", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/github/installations: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/vcs/connections",
			"/api/workspaces/"+f.workspaceID+"/vcs/connections", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/vcs/connections: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/workspaces/{id}/slack/installations",
			"/api/workspaces/"+f.workspaceID+"/slack/installations", nil)
		if status != 200 {
			t.Errorf("GET /api/workspaces/{id}/slack/installations: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/tokens", "/api/tokens", nil)
		if status != 200 {
			t.Errorf("GET /api/tokens: expected 200, got %d", status)
		}

		status, _ = f.call(t, http.MethodGet, "/api/invitations", "/api/invitations", nil)
		if status != 200 {
			t.Errorf("GET /api/invitations: expected 200, got %d", status)
		}

		// Cloud billing depends on an external billing service; 502/503/504
		// are documented alternatives to 200 when it is unreachable or not
		// configured.
		f.call(t, http.MethodGet, "/api/cloud-billing/balance", "/api/cloud-billing/balance", nil)

		status, body := f.call(t, http.MethodPost, "/api/tokens", "/api/tokens", map[string]any{
			"name": "contract-pat-" + uniqueSuffix(),
		})
		if status != 201 && status != 200 {
			t.Errorf("POST /api/tokens: expected 200/201, got %d, body=%s", status, truncate(body))
		} else {
			pat := decodeJSON(t, body)
			patID := str(pat, "id")
			if patID != "" {
				status, _ = f.call(t, http.MethodDelete, "/api/tokens/{id}", "/api/tokens/"+patID, nil)
				if status != 200 && status != 204 {
					t.Errorf("DELETE /api/tokens/{id}: expected 200/204, got %d", status)
				}
			}
		}

		// The cloud-runtime fleet is an optional external service; 502/503/504
		// are documented alternatives to 200 when it is not configured.
		f.call(t, http.MethodGet, "/api/cloud-runtime", "/api/cloud-runtime", nil)

		status, _ = f.call(t, http.MethodGet, "/api/dashboard/usage/daily", "/api/dashboard/usage/daily", nil)
		if status != 200 {
			t.Errorf("GET /api/dashboard/usage/daily: expected 200, got %d", status)
		}

		// Webhooks: public, credential is the signature (or the upstream's
		// own verification for Stripe) — this suite has none of those
		// secrets, so every one of these is exercised on its documented
		// "not configured"/"invalid signature" path.
		f.call(t, http.MethodPost, "/api/webhooks/github", "/api/webhooks/github", map[string]any{"zen": "contract"})
		f.call(t, http.MethodPost, "/api/webhooks/vcs/{connectionId}",
			"/api/webhooks/vcs/00000000-0000-0000-0000-000000000000", map[string]any{"contract": true})
		f.call(t, http.MethodPost, "/api/webhooks/stripe", "/api/webhooks/stripe", map[string]any{"contract": true})
		f.call(t, http.MethodGet, "/api/github/setup", "/api/github/setup?state=not-a-real-state", nil)
		f.call(t, http.MethodGet, "/api/integrations/composio/callback",
			"/api/integrations/composio/callback?state=not-a-real-state", nil)

		status, _ = f.call(t, http.MethodPost, "/api/integrations/composio/connect/init",
			"/api/integrations/composio/connect/init", map[string]any{"toolkit_slug": "contract-toolkit"})
		if status != 200 && status != 400 && status != 502 && status != 503 {
			t.Errorf("POST /api/integrations/composio/connect/init: expected 200/400/502/503, got %d", status)
		}
		f.call(t, http.MethodDelete, "/api/integrations/composio/connections/{id}",
			"/api/integrations/composio/connections/contract-nonexistent", nil)

		f.call(t, http.MethodPost, "/api/slack/binding/redeem", "/api/slack/binding/redeem",
			map[string]any{"token": "not-a-real-slack-binding-token"})

		status, _ = f.call(t, http.MethodPost, "/api/tokens/current/renew", "/api/tokens/current/renew", nil)
		if status != 400 {
			t.Errorf("POST /api/tokens/current/renew: expected 400 (session, not a PAT), got %d", status)
		}
	}
}
