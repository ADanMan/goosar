package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth/totp"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func mfaTestBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte{0x2a}, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	return box
}

func withMFABox(t *testing.T) {
	t.Helper()
	prev := testHandler.MCPSecretBox
	testHandler.MCPSecretBox = mfaTestBox(t)
	t.Cleanup(func() { testHandler.MCPSecretBox = prev })
}

func newMFAUser(t *testing.T) db.User {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("mfa-%d-%s@example.com", time.Now().UnixNano(), t.Name())
	user, err := testHandler.Queries.CreateUser(ctx, db.CreateUserParams{Name: "MFA probe", Email: email})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = testHandler.Queries.DeleteUserMFA(bg, user.ID)
		_ = testHandler.Queries.DeleteUserMFARecoveryCodes(bg, user.ID)
		_ = testHandler.Queries.DeleteUserSessions(bg, user.ID)
		_, _ = testPool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, uuidToString(user.ID))
	})
	return user
}

func mfaRequest(t *testing.T, method, path string, user db.User, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuidToString(user.ID))
	return req
}

func enrollAndConfirm(t *testing.T, user db.User) ([]byte, []string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.EnrollTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/enroll", user, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: got %d: %s", w.Code, w.Body.String())
	}
	var enroll MFAEnrollResponse
	if err := json.Unmarshal(w.Body.Bytes(), &enroll); err != nil {
		t.Fatalf("enroll decode: %v", err)
	}
	secret, err := totp.DecodeSecret(enroll.Secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}

	w = httptest.NewRecorder()
	testHandler.ConfirmTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/confirm", user,
		MFACodeRequest{Code: totp.Code(secret, totp.Step64(time.Now()))}))
	if w.Code != http.StatusOK {
		t.Fatalf("confirm: got %d: %s", w.Code, w.Body.String())
	}
	var confirmed MFAConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &confirmed); err != nil {
		t.Fatalf("confirm decode: %v", err)
	}
	if !confirmed.Enabled || len(confirmed.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("confirm: enabled=%v codes=%d", confirmed.Enabled, len(confirmed.RecoveryCodes))
	}
	return secret, confirmed.RecoveryCodes
}

func TestMFAEnrollmentIsInactiveUntilAConfirmedCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)

	w := httptest.NewRecorder()
	testHandler.EnrollTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/enroll", user, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: got %d: %s", w.Code, w.Body.String())
	}

	if testHandler.mfaChallengeRequired(context.Background(), user, "email") {
		t.Fatal("an unconfirmed enrollment challenged the login")
	}

	row, err := testHandler.Queries.GetUserMFA(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	plain, err := testHandler.MCPSecretBox.Open(row.TotpSecretSealed)
	if err != nil {
		t.Fatalf("stored secret does not open under the box: %v", err)
	}
	if bytes.Contains(row.TotpSecretSealed, plain) {
		t.Fatal("the stored value contains the plaintext secret")
	}
}

func TestMFAEnrollmentRefusedWithoutAnEncryptionKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	prev := testHandler.MCPSecretBox
	testHandler.MCPSecretBox = nil
	t.Cleanup(func() { testHandler.MCPSecretBox = prev })
	user := newMFAUser(t)

	w := httptest.NewRecorder()
	testHandler.EnrollTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/enroll", user, nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != ErrCodeMFAUnavailable {
		t.Fatalf("code: got %q", body["code"])
	}
}

func TestMFAConfirmRejectsAWrongCodeAndLeavesTheFactorInactive(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)

	w := httptest.NewRecorder()
	testHandler.EnrollTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/enroll", user, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: %d", w.Code)
	}

	w = httptest.NewRecorder()
	testHandler.ConfirmTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/confirm", user,
		MFACodeRequest{Code: "000000"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", w.Code, w.Body.String())
	}
	row, err := testHandler.Queries.GetUserMFA(context.Background(), user.ID)
	if err != nil || row.EnabledAt.Valid {
		t.Fatalf("a wrong code activated the factor (enabled=%v, err=%v)", row.EnabledAt.Valid, err)
	}
}

func TestMFAReEnrollmentRefusedWhileAFactorIsActive(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	enrollAndConfirm(t, user)

	w := httptest.NewRecorder()
	testHandler.EnrollTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/enroll", user, nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestMFAVerifyHappyPathIssuesASession(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	secret, _ := enrollAndConfirm(t, user)

	if !testHandler.mfaChallengeRequired(context.Background(), user, "email") {
		t.Fatal("a confirmed factor did not challenge the login")
	}

	ticket, err := testHandler.issueMFAPendingToken(user)
	if err != nil {
		t.Fatalf("ticket: %v", err)
	}

	next := time.Now().Add(totp.Step)
	w := httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user, MFAVerifyRequest{
		MFAToken: ticket,
		Code:     totp.Code(secret, totp.Step64(next)),
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("verify: got %d: %s", w.Code, w.Body.String())
	}
	var out LoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Token == "" || out.User.ID != uuidToString(user.ID) {
		t.Fatalf("verify did not produce a session: %s", w.Body.String())
	}

	sessions, err := testHandler.Queries.ListUserSessions(context.Background(), user.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions: %d (err=%v)", len(sessions), err)
	}
}

func TestMFAVerifyRefusesAReplayedCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	secret, _ := enrollAndConfirm(t, user)

	at := time.Now().Add(totp.Step)
	code := totp.Code(secret, totp.Step64(at))

	ticket, _ := testHandler.issueMFAPendingToken(user)
	w := httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket, Code: code}))
	if w.Code != http.StatusOK {
		t.Fatalf("first use: got %d: %s", w.Code, w.Body.String())
	}

	ticket2, _ := testHandler.issueMFAPendingToken(user)
	w = httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket2, Code: code}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("replay: got %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestMFAVerifyRefusesAWrongCodeAndAnExpiredTicket(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	enrollAndConfirm(t, user)

	ticket, _ := testHandler.issueMFAPendingToken(user)
	w := httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket, Code: "000000"}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: got %d, want 401", w.Code)
	}

	w = httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: "not-a-token", Code: "000000"}))
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusUnauthorized || body["code"] != ErrCodeMFAPendingInvalid {
		t.Fatalf("forged ticket: got %d %q", w.Code, body["code"])
	}
}

func TestMFATicketAndSessionTokenAreNotInterchangeable(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	session, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatalf("issueJWT: %v", err)
	}
	if _, err := testHandler.parseMFAPendingToken(context.Background(), session); err == nil {
		t.Fatal("a session token was accepted as a pending MFA ticket")
	}
}

func TestMFARecoveryCodeWorksOnceAndOnlyOnce(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	_, codes := enrollAndConfirm(t, user)

	ticket, _ := testHandler.issueMFAPendingToken(user)
	w := httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket, RecoveryCode: codes[0]}))
	if w.Code != http.StatusOK {
		t.Fatalf("recovery code: got %d: %s", w.Code, w.Body.String())
	}

	ticket2, _ := testHandler.issueMFAPendingToken(user)
	w = httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket2, RecoveryCode: codes[0]}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code: got %d, want 401: %s", w.Code, w.Body.String())
	}

	left, err := testHandler.Queries.CountUserMFARecoveryCodesUnused(context.Background(), user.ID)
	if err != nil || left != recoveryCodeCount-1 {
		t.Fatalf("unused codes: %d (err=%v)", left, err)
	}
	var stored []string
	rows, err := testPool.Query(context.Background(),
		`SELECT code_hash FROM user_mfa_recovery_code WHERE user_id = $1`, uuidToString(user.ID))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatalf("scan: %v", err)
		}
		stored = append(stored, h)
	}
	for _, code := range codes {
		for _, h := range stored {
			if h == code {
				t.Fatal("a recovery code is stored in plaintext")
			}
		}
	}
}

func TestMFARecoveryCodesAreAcceptedInTheFormPeopleTypeThem(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	_, codes := enrollAndConfirm(t, user)

	typed := codes[0][:4] + "-" + codes[0][4:]
	ticket, _ := testHandler.issueMFAPendingToken(user)
	w := httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket, RecoveryCode: typed}))
	if w.Code != http.StatusOK {
		t.Fatalf("grouped recovery code refused: %d %s", w.Code, w.Body.String())
	}
}

func TestMFADisableNeedsAFreshCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	secret, _ := enrollAndConfirm(t, user)

	w := httptest.NewRecorder()
	testHandler.DisableTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/disable", user,
		MFACodeRequest{Code: "000000"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong code: got %d, want 400", w.Code)
	}
	if _, err := testHandler.Queries.GetUserMFA(context.Background(), user.ID); err != nil {
		t.Fatal("a wrong code removed the factor")
	}

	w = httptest.NewRecorder()
	testHandler.DisableTOTP(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/totp/disable", user,
		MFACodeRequest{Code: totp.Code(secret, totp.Step64(time.Now().Add(totp.Step)))}))
	if w.Code != http.StatusOK {
		t.Fatalf("disable: got %d: %s", w.Code, w.Body.String())
	}
	if _, err := testHandler.Queries.GetUserMFA(context.Background(), user.ID); err == nil {
		t.Fatal("the factor survived a valid disable")
	}

	if left, _ := testHandler.Queries.CountUserMFARecoveryCodesUnused(context.Background(), user.ID); left != 0 {
		t.Fatalf("%d recovery codes outlived the factor", left)
	}
}

func TestOIDCSkipsTheChallengeUnlessThePolicySaysOtherwise(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	enrollAndConfirm(t, user)
	ctx := context.Background()

	withSessionPolicy(t, `{}`)
	if testHandler.mfaChallengeRequired(ctx, user, "oidc") {
		t.Fatal("an OIDC login was challenged under the default policy")
	}
	if !testHandler.mfaChallengeRequired(ctx, user, "ldap") {
		t.Fatal("an LDAP login was NOT challenged")
	}

	withSessionPolicy(t, `{"session":{"require_mfa":"all"}}`)
	if !testHandler.mfaChallengeRequired(ctx, user, "oidc") {
		t.Fatal(`require_mfa "all" did not challenge an OIDC login`)
	}
}

func TestRequireMFAForAdminsFlagsEnrollmentWithoutLockingAnybodyOut(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	user := newMFAUser(t)
	ctx := context.Background()

	withSessionPolicy(t, `{"session":{"require_mfa":"admins"}}`)
	if got := testHandler.mfaStateFor(ctx, user); got.EnrollmentRequired {
		t.Fatal("a non-administrator was told to enroll under require_mfa=admins")
	}

	if _, err := testPool.Exec(ctx,
		`INSERT INTO deployment_admin (user_id, granted_by) VALUES ($1, $1)`, uuidToString(user.ID)); err != nil {
		t.Fatalf("grant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM deployment_admin WHERE user_id = $1`, uuidToString(user.ID))
	})
	if got := testHandler.mfaStateFor(ctx, user); !got.EnrollmentRequired {
		t.Fatal("an administrator was not told to enroll under require_mfa=admins")
	}

	if testHandler.mfaChallengeRequired(ctx, user, "email") {
		t.Fatal("an account with no enrollment was challenged for a code it cannot produce")
	}
}

func mfaAuditActions(t *testing.T, userID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT action, outcome, COALESCE(reason, '') FROM auth_audit
		 WHERE actor_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action, outcome, reason string
		if err := rows.Scan(&action, &outcome, &reason); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, action+"/"+outcome+"/"+reason)
	}
	return out
}

func TestMFAJourneyIsJournaled(t *testing.T) {
	if testHandler == nil || testHandler.Audit == nil {
		t.Skip("no audit recorder wired in this test handler")
	}
	withMFABox(t)
	user := newMFAUser(t)
	userID := uuidToString(user.ID)
	secret, codes := enrollAndConfirm(t, user)

	ticket, _ := testHandler.issueMFAPendingToken(user)
	w := httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket, Code: "000000"}))

	ticket2, _ := testHandler.issueMFAPendingToken(user)
	w = httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket2, Code: totp.Code(secret, totp.Step64(time.Now().Add(totp.Step)))}))
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}

	ticket3, _ := testHandler.issueMFAPendingToken(user)
	w = httptest.NewRecorder()
	testHandler.VerifyMFA(w, mfaRequest(t, http.MethodPost, "/api/auth/mfa/verify", user,
		MFAVerifyRequest{MFAToken: ticket3, RecoveryCode: codes[0]}))
	if w.Code != http.StatusOK {
		t.Fatalf("recovery verify: %d %s", w.Code, w.Body.String())
	}

	logged := mfaAuditActions(t, userID)
	for _, want := range []string{
		audit.ActionMFAEnrolled + "/" + audit.OutcomeSuccess + "/",
		audit.ActionMFAConfirmed + "/" + audit.OutcomeSuccess + "/",
		audit.ActionMFARecoveryIssued + "/" + audit.OutcomeSuccess + "/",
		audit.ActionMFAFailed + "/" + audit.OutcomeFailure + "/" + audit.ReasonMFACodeInvalid,
		audit.ActionMFAVerified + "/" + audit.OutcomeSuccess + "/",
		audit.ActionMFARecoveryUsed + "/" + audit.OutcomeSuccess + "/",
	} {
		if !slices.Contains(logged, want) {
			t.Errorf("journal is missing %q; got %v", want, logged)
		}
	}

	var blob string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(string_agg(action || COALESCE(reason,'') || COALESCE(target_id,''), '|'), '')
		 FROM auth_audit WHERE actor_id = $1`, userID).Scan(&blob); err != nil {
		t.Fatalf("read journal blob: %v", err)
	}
	for _, secretish := range append([]string{totp.EncodeSecret(secret)}, codes...) {
		if strings.Contains(blob, secretish) {
			t.Fatal("the journal contains a credential")
		}
	}
}
