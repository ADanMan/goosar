package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/audit"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func withSessionPolicy(t *testing.T, doc string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO deployment_policy (singleton, policy) VALUES (TRUE, $1)
		 ON CONFLICT (singleton) DO UPDATE SET policy = EXCLUDED.policy`, doc); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	InvalidateSessionPolicyCache()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM deployment_policy`)
		InvalidateSessionPolicyCache()
	})
}

func sessionRequest(t *testing.T, method, path string, user db.User) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-User-ID", uuidToString(user.ID))
	return req
}

func TestLoginRecordsASessionTheUserCanSee(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/verify-code", nil)
	req.Header.Set("User-Agent", "Hermes Desktop/1.0 (probe)")
	w := httptest.NewRecorder()
	testHandler.writeLoginSession(w, req, user, audit.ActionLoginCodeVerified)
	if w.Code != http.StatusOK {
		t.Fatalf("login: got %d: %s", w.Code, w.Body.String())
	}

	lw := httptest.NewRecorder()
	testHandler.ListMySessions(lw, sessionRequest(t, http.MethodGet, "/api/auth/sessions", user))
	var list []SessionResponse
	if err := json.Unmarshal(lw.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("sessions: %d, want 1", len(list))
	}
	if list[0].UserAgent != "Hermes Desktop/1.0 (probe)" {
		t.Fatalf("user agent: %q", list[0].UserAgent)
	}

	if strings.Contains(lw.Body.String(), "192.0.2.1") {
		t.Fatalf("the session list leaked a client address: %s", lw.Body.String())
	}
}

func TestRevokeAllSessionsBumpsTheEpochAndMarksTheRows(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	for range 3 {
		if _, _, err := testHandler.issueSessionJWT(
			httptest.NewRequest(http.MethodGet, "/", nil), user); err != nil {
			t.Fatalf("issue: %v", err)
		}
	}

	w := httptest.NewRecorder()
	testHandler.RevokeAllMySessions(w, sessionRequest(t, http.MethodPost, "/api/auth/sessions/revoke-all", user))
	if w.Code != http.StatusOK {
		t.Fatalf("revoke-all: got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Revoked      int   `json:"revoked"`
		TokenVersion int32 `json:"token_version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Revoked != 3 {
		t.Fatalf("revoked %d, want 3", out.Revoked)
	}

	if out.TokenVersion <= user.TokenVersion {
		t.Fatalf("token_version did not move: %d -> %d", user.TokenVersion, out.TokenVersion)
	}
	live, err := testHandler.Queries.ListUserSessions(context.Background(), user.ID)
	if err != nil || len(live) != 0 {
		t.Fatalf("live sessions after revoke-all: %d (err=%v)", len(live), err)
	}
}

func TestRevokeOneSessionOnlyEverTouchesTheCallersOwn(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	owner := newMFAUser(t)
	stranger := newMFAUser(t)
	victim, err := testHandler.Queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{
		UserID: owner.ID,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := sessionRequest(t, http.MethodDelete, "/api/auth/sessions/"+uuidToString(victim.ID), stranger)
	req = withURLParam(req, "sessionId", uuidToString(victim.ID))
	w := httptest.NewRecorder()
	testHandler.RevokeMySession(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user revoke: got %d, want 404: %s", w.Code, w.Body.String())
	}
	row, err := testHandler.Queries.GetUserSession(context.Background(), victim.ID)
	if err != nil || row.RevokedAt.Valid {
		t.Fatal("a stranger revoked someone else's session")
	}
}

func TestSessionIdleTimeoutEndsASession(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	withSessionPolicy(t, `{"session":{"idle_timeout_hours":1}}`)

	session, err := testHandler.Queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{UserID: user.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE user_session SET last_seen_at = now() - interval '2 hours' WHERE id = $1`,
		uuidToString(session.ID)); err != nil {
		t.Fatalf("age: %v", err)
	}
	verdict := testHandler.CheckSession(context.Background(), uuidToString(session.ID))
	if verdict.Allowed || verdict.Reason != audit.ReasonSessionIdle {
		t.Fatalf("idle verdict: allowed=%v reason=%q", verdict.Allowed, verdict.Reason)
	}
}

func TestRevokedSessionStopsBeingAllowed(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	session, err := testHandler.Queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{UserID: user.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v := testHandler.CheckSession(context.Background(), uuidToString(session.ID)); !v.Allowed {
		t.Fatalf("a fresh session was refused: %q", v.Reason)
	}

	req := sessionRequest(t, http.MethodDelete, "/api/auth/sessions/"+uuidToString(session.ID), user)
	req = withURLParam(req, "sessionId", uuidToString(session.ID))
	w := httptest.NewRecorder()
	testHandler.RevokeMySession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: got %d: %s", w.Code, w.Body.String())
	}

	verdict := testHandler.CheckSession(context.Background(), uuidToString(session.ID))
	if verdict.Allowed || verdict.Reason != audit.ReasonSessionRevoked {
		t.Fatalf("revoked verdict: allowed=%v reason=%q", verdict.Allowed, verdict.Reason)
	}
}

func TestSessionAbsoluteLifetimeEndsASessionThatIsStillActive(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	withSessionPolicy(t, `{"session":{"absolute_lifetime_days":1}}`)

	session, err := testHandler.Queries.CreateUserSession(context.Background(), db.CreateUserSessionParams{UserID: user.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE user_session SET created_at = now() - interval '2 days' WHERE id = $1`,
		uuidToString(session.ID)); err != nil {
		t.Fatalf("age: %v", err)
	}
	verdict := testHandler.CheckSession(context.Background(), uuidToString(session.ID))
	if verdict.Allowed || verdict.Reason != audit.ReasonSessionExpired {
		t.Fatalf("absolute verdict: allowed=%v reason=%q", verdict.Allowed, verdict.Reason)
	}
}

func TestUnknownSessionIsAllowedSoPreFeatureTokensKeepWorking(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}

	for _, sid := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if v := testHandler.CheckSession(context.Background(), sid); !v.Allowed {
			t.Fatalf("sid %q was refused: %q", sid, v.Reason)
		}
	}
}

func TestConcurrentSessionCapRevokesTheOldest(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	withSessionPolicy(t, `{"session":{"max_concurrent_sessions":2}}`)

	for range 3 {
		if _, _, err := testHandler.issueSessionJWT(httptest.NewRequest(http.MethodGet, "/", nil), user); err != nil {
			t.Fatalf("issue: %v", err)
		}

		time.Sleep(5 * time.Millisecond)
	}
	live, err := testHandler.Queries.ListUserSessions(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("live sessions: %d, want 2 (the cap)", len(live))
	}
}

func TestSessionCapOfZeroMeansUnlimited(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	withSessionPolicy(t, `{"session":{"max_concurrent_sessions":0}}`)
	for range 4 {
		if _, _, err := testHandler.issueSessionJWT(httptest.NewRequest(http.MethodGet, "/", nil), user); err != nil {
			t.Fatalf("issue: %v", err)
		}
	}
	live, _ := testHandler.Queries.ListUserSessions(context.Background(), user.ID)
	if len(live) != 4 {
		t.Fatalf("live sessions: %d, want 4 (0 = unlimited)", len(live))
	}
}

func TestSessionPolicyDefaultsApplyWithNoDocument(t *testing.T) {
	got := parseSessionPolicy(nil)
	want := DefaultSessionPolicy()
	if got != want {
		t.Fatalf("defaults drifted: %+v vs %+v", got, want)
	}
	if want.IdleTimeoutHours != 12 || want.AbsoluteLifetimeDays != 30 ||
		want.MaxConcurrent != 0 || want.RequireMFA != RequireMFANone {
		t.Fatalf("the documented defaults changed without the documentation: %+v", want)
	}
}

func TestSessionPolicyValidationRefusesWhatWouldBreakTheDeployment(t *testing.T) {
	valid := []string{
		`{}`,
		`{"session":{}}`,
		`{"session":{"idle_timeout_hours":8,"absolute_lifetime_days":7,"max_concurrent_sessions":3,"require_mfa":"admins"}}`,
	}
	for _, doc := range valid {
		if err := validateDeploymentPolicyDoc([]byte(doc)); err != nil {
			t.Errorf("valid policy refused: %s: %v", doc, err)
		}
	}
	invalid := []string{
		`{"session":{"idle_timeout_hours":0}}`,
		`{"session":{"absolute_lifetime_days":100000}}`,
		`{"session":{"max_concurrent_sessions":-1}}`,
		`{"session":{"require_mfa":"sometimes"}}`,
		`{"session":{"idle_timeout_minutes":30}}`,
	}
	for _, doc := range invalid {
		if err := validateDeploymentPolicyDoc([]byte(doc)); err == nil {
			t.Errorf("invalid policy accepted: %s", doc)
		}
	}
}

func TestSessionPolicyPartialBlockKeepsTheOtherDefaults(t *testing.T) {
	got := parseSessionPolicy([]byte(`{"session":{"idle_timeout_hours":4}}`))
	if got.IdleTimeoutHours != 4 {
		t.Fatalf("idle: %d", got.IdleTimeoutHours)
	}
	if got.AbsoluteLifetimeDays != DefaultSessionAbsoluteLifetimeDays ||
		got.MaxConcurrent != DefaultSessionMaxConcurrent ||
		got.RequireMFA != RequireMFANone {
		t.Fatalf("a partial block reset the other knobs: %+v", got)
	}
}

func TestUnreadablePolicyFallsBackToTheDefaults(t *testing.T) {
	if got := parseSessionPolicy([]byte("{not json")); got != DefaultSessionPolicy() {
		t.Fatalf("garbage policy did not fall back: %+v", got)
	}
}
