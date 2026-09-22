package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/service"
)

type stubEmailSender struct {
	transport string
	sent      bool
}

func (s *stubEmailSender) SendVerificationCode(to, code, linkToken, lang string) error {
	s.sent = true
	return nil
}

func (s *stubEmailSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID, lang string) error {
	return nil
}

func (s *stubEmailSender) Transport() string { return s.transport }

func postSendCode(t *testing.T, email string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"email": email}); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.SendCode(w, req)
	return w
}

func TestSendCodeRefusesWithoutMailTransport(t *testing.T) {
	sender := &stubEmailSender{transport: service.TransportNone}
	original := testHandler.EmailService
	testHandler.EmailService = sender
	t.Cleanup(func() { testHandler.EmailService = original })

	w := postSendCode(t, "no-transport@example.com")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("SendCode: got %d, want 503: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "email_not_configured" {
		t.Fatalf("code = %q, want email_not_configured (body %s)", body["code"], w.Body.String())
	}
	if body["error"] == "" {
		t.Fatal("refusal must keep the error envelope's sentence")
	}
	if sender.sent {
		t.Fatal("nothing may be sent when the transport is unavailable")
	}
}

func TestSendCodeStillWorksOnDevTransport(t *testing.T) {
	sender := &stubEmailSender{transport: service.TransportDev}
	original := testHandler.EmailService
	testHandler.EmailService = sender
	t.Cleanup(func() { testHandler.EmailService = original })

	if w := postSendCode(t, "dev-transport@example.com"); w.Code != http.StatusOK {
		t.Fatalf("SendCode: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if !sender.sent {
		t.Fatal("dev transport must still deliver the code to stdout")
	}
}

func TestGetConfigExposesEmailTransport(t *testing.T) {
	origStorage := testHandler.Storage
	origEmail := testHandler.EmailService
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.EmailService = origEmail
	})

	for _, want := range []string{service.TransportSMTP, service.TransportResend, service.TransportDev, service.TransportNone} {
		testHandler.EmailService = &stubEmailSender{transport: want}
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: got %d: %s", w.Code, w.Body.String())
		}
		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		if cfg.EmailTransport != want {
			t.Fatalf("email_transport = %q, want %q", cfg.EmailTransport, want)
		}
	}
}

func TestHashEmailForLogHidesTheAddress(t *testing.T) {
	const email = "Person@Example.com"
	got := hashEmailForLog(email)
	if got == "" || len(got) != 16 {
		t.Fatalf("hashEmailForLog = %q, want a 16-char digest", got)
	}
	if got != hashEmailForLog("person@example.com") {
		t.Fatal("hash must be case-insensitive so log lines correlate")
	}
	if got == email || got == "person@example.com" {
		t.Fatal("hash must not be the address itself")
	}
	if hashEmailForLog("other@example.com") == got {
		t.Fatal("different addresses must not collide")
	}
}
