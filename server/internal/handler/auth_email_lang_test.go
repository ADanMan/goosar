package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/service"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func storedLanguage(lang string) db.User {
	return db.User{Language: pgtype.Text{String: lang, Valid: true}}
}

type recordingEmailSender struct {
	to        string
	code      string
	linkToken string
	lang      string
	sent      bool
}

func (s *recordingEmailSender) SendVerificationCode(to, code, linkToken, lang string) error {
	s.to, s.code, s.linkToken, s.lang, s.sent = to, code, linkToken, lang, true
	return nil
}

func (s *recordingEmailSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID, lang string) error {
	return nil
}

func (s *recordingEmailSender) Transport() string { return service.TransportDev }

func TestSendCodeIgnoresAcceptLanguage(t *testing.T) {

	headers := []string{
		"en-US,en;q=0.9",
		"en",
		"EN-GB",
		"en-GB,ru;q=0.8",
		"fr-FR,en;q=0.7",
		"en_US",
	}
	for i, header := range headers {
		t.Run(header, func(t *testing.T) {

			email := sendCodeTestEmail(t, "accept-lang", i)
			sender := sendCodeWithSpy(t, email, header)

			if !sender.sent {
				t.Fatal("SendCode did not send a verification code")
			}
			if sender.lang != service.EmailLangRU {
				t.Errorf("SendCode(Accept-Language: %q, no account) mailed in %q, want %q",
					header, sender.lang, service.EmailLangRU)
			}
		})
	}
}

func TestSendCodeUsesTheAccountLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language string
		header   string
		want     string
	}{
		{"stored en beats a russian browser", "en", "ru-RU,ru;q=0.9", service.EmailLangEN},
		{"stored en beats a missing header", "en", "", service.EmailLangEN},
		{"stored ru beats an english browser", "ru", "en-US,en;q=0.9", service.EmailLangRU},
		{"stored en-US region tag still means english", "en-US", "ru-RU", service.EmailLangEN},
		{"empty stored language is not a choice", "", "en-US,en;q=0.9", service.EmailLangRU},
		{"ui locale without an email template falls back to russian", "ja", "en-US", service.EmailLangRU},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := sendCodeTestEmail(t, "account-lang", i)
			createUserWithLanguage(t, email, tt.language)

			sender := sendCodeWithSpy(t, email, tt.header)
			if !sender.sent {
				t.Fatal("SendCode did not send a verification code")
			}
			if sender.lang != tt.want {
				t.Errorf("SendCode(Accept-Language: %q, language=%q) mailed in %q, want %q",
					tt.header, tt.language, sender.lang, tt.want)
			}
		})
	}
}

func TestEmailLangForLoginCode(t *testing.T) {
	tests := []struct {
		name string
		user db.User
		want string
	}{
		{"no account yet", db.User{}, service.EmailLangRU},
		{"account exists but never picked a language", storedLanguage(""), service.EmailLangRU},
		{"stored english", storedLanguage("en"), service.EmailLangEN},
		{"stored russian", storedLanguage("ru"), service.EmailLangRU},
		{"ui locale without an email template", storedLanguage("ko"), service.EmailLangRU},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emailLangForLoginCode(tt.user); got != tt.want {
				t.Errorf("emailLangForLoginCode(language=%q) = %q, want %q",
					tt.user.Language.String, got, tt.want)
			}
		})
	}
}

func sendCodeTestEmail(t *testing.T, group string, i int) string {
	t.Helper()
	email := fmt.Sprintf("emaillang-%s-%d@goosar.ru", group, i)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	return email
}

func createUserWithLanguage(t *testing.T, email, language string) {
	t.Helper()
	var lang any
	if language == "" {
		lang = nil
	} else {
		lang = language
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO "user" (name, email, language)
		VALUES ($1, $2, $3)
	`, "Email Lang Test", email, lang); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func sendCodeWithSpy(t *testing.T, email, acceptLanguage string) *recordingEmailSender {
	t.Helper()
	sender := &recordingEmailSender{}
	original := testHandler.EmailService
	testHandler.EmailService = sender
	t.Cleanup(func() { testHandler.EmailService = original })

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"email": email}); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}

	w := httptest.NewRecorder()
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	return sender
}
