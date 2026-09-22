package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/corpauth"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type stubDirectory struct {
	identity corpauth.Identity
	err      error
	calls    int
}

func (s *stubDirectory) Authenticate(_ context.Context, username, password string) (corpauth.Identity, error) {
	s.calls++
	if s.err != nil {
		return corpauth.Identity{}, s.err
	}
	return s.identity, nil
}

const (
	corpTestEmail   = "corp.person@example.test"
	corpTestSubject = "https://sso.example.test|subject-abc"
)

func corpIdentity(email, subject string) corpauth.Identity {
	return corpauth.Identity{
		Provider: corpauth.MethodOIDC,
		Subject:  subject,
		Email:    email,
		Name:     "Corporate Person",

		EmailVerified: true,
	}
}

func corpTestHandler(t *testing.T, cfg Config, dir corpauth.Directory) *Handler {
	t.Helper()
	if testHandler == nil {
		t.Skip("no database")
	}
	prevCfg, prevDir := testHandler.cfg, testHandler.Directory
	testHandler.cfg, testHandler.Directory = cfg, dir
	t.Cleanup(func() { testHandler.cfg, testHandler.Directory = prevCfg, prevDir })
	return testHandler
}

func dropCorpUser(t *testing.T, email string) {
	t.Helper()
	ctx := context.Background()
	var id string
	err := testPool.QueryRow(ctx, `SELECT id::text FROM "user" WHERE lower(email) = lower($1)`, email).Scan(&id)
	if err != nil {
		return
	}
	_, _ = testPool.Exec(ctx, `DELETE FROM user_identity WHERE user_id = $1`, id)
	_, _ = testPool.Exec(ctx, `DELETE FROM deployment_admin_pending WHERE target_user_id = $1`, id)
	_, _ = testPool.Exec(ctx, `DELETE FROM deployment_admin WHERE user_id = $1`, id)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, id)
}

func corpRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/auth/ldap/login", strings.NewReader("{}"))
}

func TestCorporateLogin_CreatesAccountThenReusesIt(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	dropCorpUser(t, corpTestEmail)
	t.Cleanup(func() { dropCorpUser(t, corpTestEmail) })

	id := corpIdentity(corpTestEmail, corpTestSubject)

	w := httptest.NewRecorder()
	first, refusal := h.completeCorporateLogin(w, corpRequest(), id)
	if refusal != nil {
		t.Fatalf("first login refused: %+v", refusal)
	}
	if first.Token == "" {
		t.Fatal("first login minted no session")
	}
	if first.User.Email != corpTestEmail {
		t.Fatalf("email: got %q", first.User.Email)
	}

	link, err := h.Queries.GetUserIdentity(context.Background(), db.GetUserIdentityParams{
		Provider: corpauth.MethodOIDC, Subject: corpTestSubject,
	})
	if err != nil {
		t.Fatalf("no identity link written on first login: %v", err)
	}
	if link.EmailAtLink != corpTestEmail {
		t.Fatalf("email_at_link: got %q", link.EmailAtLink)
	}

	w2 := httptest.NewRecorder()
	second, refusal := h.completeCorporateLogin(w2, corpRequest(), id)
	if refusal != nil {
		t.Fatalf("second login refused: %+v", refusal)
	}
	if uuidToString(second.User.ID) != uuidToString(first.User.ID) {
		t.Fatalf("second login produced a different account (%s vs %s)",
			uuidToString(second.User.ID), uuidToString(first.User.ID))
	}

	var linkCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_identity WHERE provider = $1 AND subject = $2`,
		corpauth.MethodOIDC, corpTestSubject).Scan(&linkCount); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("identity links for one subject: got %d, want exactly 1", linkCount)
	}
}

func TestCorporateLogin_RenamedMailboxKeepsTheSameAccount(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	const oldEmail = "old.name@example.test"
	const newEmail = "new.name@example.test"
	dropCorpUser(t, oldEmail)
	dropCorpUser(t, newEmail)
	t.Cleanup(func() { dropCorpUser(t, oldEmail); dropCorpUser(t, newEmail) })

	subject := "https://sso.example.test|renamed-person"
	first, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), corpIdentity(oldEmail, subject))
	if refusal != nil {
		t.Fatalf("first login refused: %+v", refusal)
	}

	second, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), corpIdentity(newEmail, subject))
	if refusal != nil {
		t.Fatalf("login after rename refused: %+v", refusal)
	}
	if uuidToString(second.User.ID) != uuidToString(first.User.ID) {
		t.Fatal("a renamed mailbox produced a second account: the subject lookup is not taking precedence over the address")
	}

	link, err := h.Queries.GetUserIdentity(context.Background(), db.GetUserIdentityParams{
		Provider: corpauth.MethodOIDC, Subject: subject,
	})
	if err != nil {
		t.Fatalf("GetUserIdentity: %v", err)
	}
	if link.EmailAtLink != newEmail {
		t.Fatalf("email_at_link after rename: got %q, want %q", link.EmailAtLink, newEmail)
	}
}

func TestCorporateLogin_DifferentSubjectAtAKnownAddressReusesTheAddressAccount(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	const email = "shared.address@example.test"
	dropCorpUser(t, email)
	t.Cleanup(func() { dropCorpUser(t, email) })

	first, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(),
		corpIdentity(email, "https://sso.example.test|subject-one"))
	if refusal != nil {
		t.Fatalf("first login refused: %+v", refusal)
	}

	second, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(),
		corpIdentity(email, "https://sso.example.test|subject-two"))
	if refusal != nil {
		t.Fatalf("second login refused: %+v", refusal)
	}
	if uuidToString(second.User.ID) != uuidToString(first.User.ID) {
		t.Fatal("one address grew two accounts")
	}
}

func TestCorporateLogin_SignupAllowlistsApply(t *testing.T) {
	const outsider = "outsider@notallowed.test"

	t.Run("domain not on the list is refused", func(t *testing.T) {
		h := corpTestHandler(t, Config{
			AllowSignup:         false,
			AllowedEmailDomains: []string{"example.test"},
		}, nil)
		dropCorpUser(t, outsider)
		t.Cleanup(func() { dropCorpUser(t, outsider) })

		_, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(),
			corpIdentity(outsider, "https://sso.example.test|outsider"))
		if refusal == nil {
			t.Fatal("an address outside the allowlist was admitted")
		}
		if refusal.Status != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", refusal.Status)
		}
		if refusal.Code != ErrCodeSignupDisabled && refusal.Code != ErrCodeEmailDomainNotAllows {
			t.Fatalf("code: got %q, want a signup refusal code", refusal.Code)
		}

		var exists bool
		_ = testPool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM "user" WHERE lower(email) = lower($1))`, outsider).Scan(&exists)
		if exists {
			t.Fatal("the refused login created the account anyway")
		}
	})

	t.Run("domain on the list is admitted", func(t *testing.T) {
		h := corpTestHandler(t, Config{
			AllowSignup:         false,
			AllowedEmailDomains: []string{"example.test"},
		}, nil)
		dropCorpUser(t, corpTestEmail)
		t.Cleanup(func() { dropCorpUser(t, corpTestEmail) })

		if _, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(),
			corpIdentity(corpTestEmail, corpTestSubject)); refusal != nil {
			t.Fatalf("an allowlisted address was refused: %+v", refusal)
		}
	})
}

func TestCorporateLogin_DeactivatedAccountIsRefused(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	dropCorpUser(t, corpTestEmail)
	t.Cleanup(func() { dropCorpUser(t, corpTestEmail) })

	id := corpIdentity(corpTestEmail, corpTestSubject)
	first, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), id)
	if refusal != nil {
		t.Fatalf("setup login refused: %+v", refusal)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET deactivated_at = now() WHERE id = $1`, first.User.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	w := httptest.NewRecorder()
	_, refusal = h.completeCorporateLogin(w, corpRequest(), id)
	if refusal == nil {
		t.Fatal("a deactivated account signed in through the corporate door")
	}
	if refusal.Code != ErrCodeAccountDeactivated {
		t.Fatalf("code: got %q, want %q", refusal.Code, ErrCodeAccountDeactivated)
	}
	if refusal.Status != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403", refusal.Status)
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "goosar_auth" && c.Value != "" {
			t.Fatal("a session cookie was set for a deactivated account")
		}
	}
}

func TestCorporateLogin_AdminClaimFilesPendingGrantAndNeverGrants(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	dropCorpUser(t, corpTestEmail)
	t.Cleanup(func() { dropCorpUser(t, corpTestEmail) })

	id := corpIdentity(corpTestEmail, corpTestSubject)
	id.IsAdmin = true

	result, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), id)
	if refusal != nil {
		t.Fatalf("login refused: %+v", refusal)
	}

	ctx := context.Background()
	var pending int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM deployment_admin_pending WHERE target_user_id = $1 AND action = 'grant'`,
		result.User.ID).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending grants: got %d, want exactly 1", pending)
	}

	var granted bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM deployment_admin WHERE user_id = $1)`, result.User.ID).Scan(&granted); err != nil {
		t.Fatalf("check role: %v", err)
	}
	if granted {
		t.Fatal("a directory group granted deployment_admin directly: the second channel was bypassed")
	}

	if _, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), id); refusal != nil {
		t.Fatalf("second login refused: %+v", refusal)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM deployment_admin_pending WHERE target_user_id = $1 AND action = 'grant'`,
		result.User.ID).Scan(&pending); err != nil {
		t.Fatalf("recount pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending grants after a second login: got %d, want still 1", pending)
	}
}

func TestCorporateLogin_RefusesIdentityWithoutEmail(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)

	_, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(),
		corpauth.Identity{Provider: corpauth.MethodOIDC, Subject: "https://sso.example.test|no-mail"})
	if refusal == nil {
		t.Fatal("an identity with no address was admitted")
	}
	if refusal.Code != ErrCodeCorporateEmailMissing {
		t.Fatalf("code: got %q, want %q", refusal.Code, ErrCodeCorporateEmailMissing)
	}
}

func TestGetAuthMethods(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}

	call := func(t *testing.T) AuthMethodsResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.GetAuthMethods(w, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", w.Code)
		}
		var resp AuthMethodsResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	has := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}

	t.Run("default is email only", func(t *testing.T) {
		t.Setenv(corpauth.MethodsEnvVar, "")
		resp := call(t)
		if len(resp.Methods) != 1 || resp.Methods[0] != corpauth.MethodEmail {
			t.Fatalf("methods: got %v, want [email]", resp.Methods)
		}
	})

	t.Run("oidc enabled without configuration is not offered", func(t *testing.T) {
		t.Setenv(corpauth.MethodsEnvVar, "email,oidc")
		prev := testHandler.OIDC
		testHandler.OIDC = nil
		t.Cleanup(func() { testHandler.OIDC = prev })

		if resp := call(t); has(resp.Methods, corpauth.MethodOIDC) {
			t.Fatalf("methods: got %v, want oidc withheld while unconfigured", resp.Methods)
		}
	})

	t.Run("ldap enabled and configured is offered with the operator's own label", func(t *testing.T) {
		t.Setenv(corpauth.MethodsEnvVar, "email,ldap")
		t.Setenv(corpauth.LDAPDisplayNameEnvVar, "Вход по учётной записи домена")
		prev := testHandler.Directory
		testHandler.Directory = &stubDirectory{}
		t.Cleanup(func() { testHandler.Directory = prev })

		resp := call(t)
		if !has(resp.Methods, corpauth.MethodLDAP) {
			t.Fatalf("methods: got %v, want ldap offered", resp.Methods)
		}
		if resp.LDAPDisplayName != "Вход по учётной записи домена" {
			t.Fatalf("display name: got %q", resp.LDAPDisplayName)
		}
	})

	t.Run("never answers with an empty method list", func(t *testing.T) {
		t.Setenv(corpauth.MethodsEnvVar, "oidc")
		prev := testHandler.OIDC
		testHandler.OIDC = nil
		t.Cleanup(func() { testHandler.OIDC = prev })

		resp := call(t)
		if len(resp.Methods) == 0 {
			t.Fatal("no sign-in method at all is offered: the deployment is unreachable")
		}
		if !has(resp.Methods, corpauth.MethodEmail) {
			t.Fatalf("methods: got %v, want the e-mail form as the recoverable fallback", resp.Methods)
		}
	})

	t.Run("carries no secret", func(t *testing.T) {
		t.Setenv(corpauth.MethodsEnvVar, "email,ldap")
		t.Setenv(corpauth.LDAPBindPasswordEnvVar, "super-secret-bind-password")
		t.Setenv(corpauth.OIDCClientSecretEnvVar, "super-secret-client-secret")
		prev := testHandler.Directory
		testHandler.Directory = &stubDirectory{}
		t.Cleanup(func() { testHandler.Directory = prev })

		w := httptest.NewRecorder()
		testHandler.GetAuthMethods(w, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
		body := w.Body.String()
		for _, secret := range []string{"super-secret-bind-password", "super-secret-client-secret"} {
			if strings.Contains(body, secret) {
				t.Fatalf("the methods response carries a secret: %s", body)
			}
		}
	})
}

func postLDAP(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/ldap/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	testHandler.LoginLDAP(w, r)
	return w
}

func decodeCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	code, _ := body["code"].(string)
	return code
}

func TestLoginLDAP(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database")
	}

	t.Run("a good bind mints a session", func(t *testing.T) {
		dir := &stubDirectory{identity: corpauth.Identity{
			Provider: corpauth.MethodLDAP,
			Subject:  "cn=john doe,ou=users,dc=example,dc=test",
			Email:    corpTestEmail,
			Name:     "John Doe",
		}}
		corpTestHandler(t, Config{AllowSignup: true}, dir)
		t.Setenv(corpauth.MethodsEnvVar, "ldap")
		dropCorpUser(t, corpTestEmail)
		t.Cleanup(func() { dropCorpUser(t, corpTestEmail) })

		w := postLDAP(t, `{"username":"jdoe","password":"correct-horse"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200 (%s)", w.Code, w.Body.String())
		}
		var resp LoginResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Token == "" {
			t.Fatal("no token in the response")
		}
		if resp.User.Email != corpTestEmail {
			t.Fatalf("user email: got %q", resp.User.Email)
		}

		if strings.Contains(w.Body.String(), "correct-horse") {
			t.Fatal("the response echoes the password")
		}
	})

	t.Run("a bad password is refused with one code", func(t *testing.T) {
		corpTestHandler(t, Config{AllowSignup: true}, &stubDirectory{err: corpauth.ErrInvalidCredentials})
		t.Setenv(corpauth.MethodsEnvVar, "ldap")

		w := postLDAP(t, `{"username":"jdoe","password":"wrong"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", w.Code)
		}
		if code := decodeCode(t, w); code != ErrCodeLDAPInvalidCredentials {
			t.Fatalf("code: got %q, want %q", code, ErrCodeLDAPInvalidCredentials)
		}
	})

	t.Run("an unknown user is indistinguishable from a bad password", func(t *testing.T) {
		corpTestHandler(t, Config{AllowSignup: true}, &stubDirectory{err: corpauth.ErrInvalidCredentials})
		t.Setenv(corpauth.MethodsEnvVar, "ldap")

		unknown := postLDAP(t, `{"username":"nobody","password":"whatever"}`)
		wrongPw := postLDAP(t, `{"username":"jdoe","password":"wrong"}`)
		if unknown.Code != wrongPw.Code || unknown.Body.String() != wrongPw.Body.String() {
			t.Fatalf("unknown user and wrong password answer differently:\n  %d %s\n  %d %s",
				unknown.Code, unknown.Body.String(), wrongPw.Code, wrongPw.Body.String())
		}
	})

	t.Run("an empty password is refused before the directory is dialled", func(t *testing.T) {
		dir := &stubDirectory{identity: corpIdentity(corpTestEmail, "cn=x")}
		corpTestHandler(t, Config{AllowSignup: true}, dir)
		t.Setenv(corpauth.MethodsEnvVar, "ldap")

		w := postLDAP(t, `{"username":"jdoe","password":""}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", w.Code)
		}
		if dir.calls != 0 {
			t.Fatal("an empty password was carried to the directory: a conforming server answers that with SUCCESS")
		}
	})

	t.Run("a directory that is down is a named 502, not a crash", func(t *testing.T) {
		corpTestHandler(t, Config{AllowSignup: true}, &stubDirectory{err: corpauth.ErrUnavailable})
		t.Setenv(corpauth.MethodsEnvVar, "ldap")

		w := postLDAP(t, `{"username":"jdoe","password":"whatever"}`)
		if w.Code != http.StatusBadGateway {
			t.Fatalf("status: got %d, want 502 — the fault is upstream and the access log must say so", w.Code)
		}
		if code := decodeCode(t, w); code != ErrCodeLDAPUnavailable {
			t.Fatalf("code: got %q, want %q", code, ErrCodeLDAPUnavailable)
		}
	})

	t.Run("a misconfigured directory is a named 503", func(t *testing.T) {
		corpTestHandler(t, Config{AllowSignup: true}, &stubDirectory{err: corpauth.ErrNotConfigured})
		t.Setenv(corpauth.MethodsEnvVar, "ldap")

		w := postLDAP(t, `{"username":"jdoe","password":"whatever"}`)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status: got %d, want 503", w.Code)
		}
		if code := decodeCode(t, w); code != ErrCodeLDAPNotConfigured {
			t.Fatalf("code: got %q, want %q", code, ErrCodeLDAPNotConfigured)
		}
	})

	t.Run("the method is closed when it is not enabled", func(t *testing.T) {
		dir := &stubDirectory{identity: corpIdentity(corpTestEmail, "cn=x")}
		corpTestHandler(t, Config{AllowSignup: true}, dir)
		t.Setenv(corpauth.MethodsEnvVar, "email")

		w := postLDAP(t, `{"username":"jdoe","password":"correct-horse"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", w.Code)
		}
		if code := decodeCode(t, w); code != ErrCodeAuthMethodDisabled {
			t.Fatalf("code: got %q, want %q", code, ErrCodeAuthMethodDisabled)
		}
		if dir.calls != 0 {
			t.Fatal("a disabled method still reached the directory")
		}
	})

	t.Run("a deactivated account is refused whatever the directory says", func(t *testing.T) {
		dir := &stubDirectory{identity: corpauth.Identity{
			Provider: corpauth.MethodLDAP,
			Subject:  "cn=deactivated,ou=users,dc=example,dc=test",
			Email:    corpTestEmail,
		}}
		h := corpTestHandler(t, Config{AllowSignup: true}, dir)
		t.Setenv(corpauth.MethodsEnvVar, "ldap")
		dropCorpUser(t, corpTestEmail)
		t.Cleanup(func() { dropCorpUser(t, corpTestEmail) })

		first, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), dir.identity)
		if refusal != nil {
			t.Fatalf("setup login refused: %+v", refusal)
		}
		if _, err := testPool.Exec(context.Background(),
			`UPDATE "user" SET deactivated_at = now() WHERE id = $1`, first.User.ID); err != nil {
			t.Fatalf("deactivate: %v", err)
		}

		w := postLDAP(t, `{"username":"jdoe","password":"correct-horse"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", w.Code)
		}
		if code := decodeCode(t, w); code != ErrCodeAccountDeactivated {
			t.Fatalf("code: got %q, want %q", code, ErrCodeAccountDeactivated)
		}
	})
}

func TestCorporateLogin_UnverifiedAddressDoesNotInheritAnExistingAccount(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	const email = "victim.inherit@example.test"
	dropCorpUser(t, email)
	t.Cleanup(func() { dropCorpUser(t, email) })

	first, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(),
		corpIdentity(email, "https://sso.example.test|victim-subject"))
	if refusal != nil {
		t.Fatalf("first login refused: %+v", refusal)
	}

	unverified := corpIdentity(email, "https://sso.example.test|attacker-subject")
	unverified.EmailVerified = false
	_, refusal = h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), unverified)
	if refusal == nil {
		t.Fatal("an unverified address inherited an existing account")
	}
	if refusal.Code != ErrCodeCorporateEmailUnverified {
		t.Fatalf("refusal code = %q, want %q", refusal.Code, ErrCodeCorporateEmailUnverified)
	}
	_ = first
}

func TestCorporateLogin_UnverifiedAddressStillCreatesANewAccount(t *testing.T) {
	h := corpTestHandler(t, Config{AllowSignup: true}, nil)
	const email = "newcomer.unverified@example.test"
	dropCorpUser(t, email)
	t.Cleanup(func() { dropCorpUser(t, email) })

	id := corpIdentity(email, "https://sso.example.test|newcomer-subject")
	id.EmailVerified = false
	if _, refusal := h.completeCorporateLogin(httptest.NewRecorder(), corpRequest(), id); refusal != nil {
		t.Fatalf("first login refused: %+v", refusal)
	}
}
