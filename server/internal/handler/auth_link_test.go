package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func linkTestEmail(t *testing.T, group string) string {
	t.Helper()
	email := fmt.Sprintf("loginlink-%s@goosar.ru", group)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	return email
}

func postVerifyLink(t *testing.T, linkToken string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"link_token": linkToken}); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/verify-link", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.VerifyLink(w, req)
	return w
}

func postVerifyCode(t *testing.T, email, code string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": code}); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.VerifyCode(w, req)
	return w
}

func TestSendCodeEmailCarriesOneTimeLinkToken(t *testing.T) {
	email := linkTestEmail(t, "carries-token")
	sender := sendCodeWithSpy(t, email, "")

	if sender.linkToken == "" {
		t.Fatal("SendCode did not hand a link token to the email")
	}
	if sender.linkToken == sender.code || strings.Contains(sender.linkToken, sender.code) {
		t.Fatalf("link token %q must not embed the code %q", sender.linkToken, sender.code)
	}
	if len(sender.linkToken) < 32 {
		t.Fatalf("link token too short to be a credential: %d chars", len(sender.linkToken))
	}

	var storedHash string
	err := testPool.QueryRow(context.Background(),
		`SELECT link_token_hash FROM verification_code WHERE email = $1 ORDER BY created_at DESC LIMIT 1`,
		email).Scan(&storedHash)
	if err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	if storedHash == sender.linkToken {
		t.Fatal("link token stored in plaintext — must be hashed")
	}
	if storedHash != hashLoginLinkToken(sender.linkToken) {
		t.Fatalf("stored hash %q does not match SHA-256 of the mailed token", storedHash)
	}
}

func TestVerifyLinkLogsInOnce(t *testing.T) {
	email := linkTestEmail(t, "logs-in-once")
	sender := sendCodeWithSpy(t, email, "")

	w := postVerifyLink(t, sender.linkToken)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyLink: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp LoginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("VerifyLink: expected a session token")
	}
	if resp.User.Email != email {
		t.Fatalf("VerifyLink: logged in as %q, want %q", resp.User.Email, email)
	}

	second := postVerifyLink(t, sender.linkToken)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("VerifyLink replay: expected 400, got %d: %s", second.Code, second.Body.String())
	}
}

func TestVerifyLinkConsumesTheCode(t *testing.T) {
	email := linkTestEmail(t, "consumes-code")
	sender := sendCodeWithSpy(t, email, "")

	if w := postVerifyLink(t, sender.linkToken); w.Code != http.StatusOK {
		t.Fatalf("VerifyLink: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := postVerifyCode(t, email, sender.code); w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode after link login: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeConsumesTheLink(t *testing.T) {
	email := linkTestEmail(t, "code-kills-link")
	sender := sendCodeWithSpy(t, email, "")

	if w := postVerifyCode(t, email, sender.code); w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := postVerifyLink(t, sender.linkToken); w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyLink after code login: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyLinkExpiredIsRefused(t *testing.T) {
	email := linkTestEmail(t, "expired")
	token, err := generateLoginLinkToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO verification_code (email, code, expires_at, link_token_hash)
		VALUES ($1, $2, $3, $4)
	`, email, "123456", time.Now().Add(-time.Minute), hashLoginLinkToken(token)); err != nil {
		t.Fatalf("seed expired row: %v", err)
	}

	if w := postVerifyLink(t, token); w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyLink on expired row: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyLinkRejectsTheCodeItself(t *testing.T) {
	email := linkTestEmail(t, "rejects-code")
	sender := sendCodeWithSpy(t, email, "")

	if w := postVerifyLink(t, sender.code); w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyLink(code): expected 400, got %d: %s", w.Code, w.Body.String())
	}

	if w := postVerifyCode(t, email, sender.code); w.Code != http.StatusOK {
		t.Fatalf("VerifyCode after refused link attempt: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyLinkRejectsMalformedInput(t *testing.T) {
	for name, token := range map[string]string{
		"empty":    "",
		"blank":    "   ",
		"oversize": strings.Repeat("a", 4096),
	} {
		t.Run(name, func(t *testing.T) {
			if w := postVerifyLink(t, token); w.Code != http.StatusBadRequest {
				t.Fatalf("VerifyLink(%s): expected 400, got %d: %s", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestVerifyCodeRefusedAfterLinkSpent(t *testing.T) {
	email := linkTestEmail(t, "code-after-link")
	sender := sendCodeWithSpy(t, email, "")

	if w := postVerifyLink(t, sender.linkToken); w.Code != http.StatusOK {
		t.Fatalf("spending the link: got %d", w.Code)
	}
	if w := postVerifyCode(t, email, sender.code); w.Code != http.StatusBadRequest {
		t.Fatalf("code after spent link: got %d, want 400", w.Code)
	}
}

func TestSendCodeKillsPriorLink(t *testing.T) {
	email := linkTestEmail(t, "resend-kills-link")
	first := sendCodeWithSpy(t, email, "")

	if _, err := testPool.Exec(context.Background(),
		`UPDATE verification_code SET created_at = created_at - interval '5 minutes' WHERE email = $1`,
		email); err != nil {
		t.Fatalf("age first code: %v", err)
	}

	second := sendCodeWithSpy(t, email, "")

	if w := postVerifyLink(t, first.linkToken); w.Code != http.StatusBadRequest {
		t.Fatalf("first email's link after resend: got %d, want 400", w.Code)
	}
	if w := postVerifyLink(t, second.linkToken); w.Code != http.StatusOK {
		t.Fatalf("fresh link must still work: got %d", w.Code)
	}
}
