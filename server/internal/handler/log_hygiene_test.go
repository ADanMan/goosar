package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/service"
)

type codeCapturingSender struct {
	code string
}

func (s *codeCapturingSender) SendVerificationCode(to, code, linkToken, lang string) error {
	s.code = code
	return nil
}

func (s *codeCapturingSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID, lang string) error {
	return nil
}

func (s *codeCapturingSender) Transport() string { return service.TransportDev }

func TestAuthFlowLogsCarryNoEmailOrCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	const email = "log-hygiene-probe@example.com"

	sender := &codeCapturingSender{}
	origSender := testHandler.EmailService
	testHandler.EmailService = sender
	t.Cleanup(func() { testHandler.EmailService = origSender })

	var logs bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(orig) })

	if w := postSendCode(t, email); w.Code != http.StatusOK {
		t.Fatalf("SendCode: got %d: %s", w.Code, w.Body.String())
	}
	if sender.code == "" {
		t.Fatal("no verification code was produced")
	}

	postVerify := func(code string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": code}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/auth/verify-code", &buf)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		testHandler.VerifyCode(w, req)
		return w
	}

	postVerify("000000")
	if w := postVerify(sender.code); w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: got %d: %s", w.Code, w.Body.String())
	}

	captured := logs.String()
	if strings.Contains(captured, email) {
		t.Fatalf("the address leaked into the log (use hashEmailForLog): %s", captured)
	}
	if strings.Contains(captured, "log-hygiene-probe") {
		t.Fatalf("the local part of the address leaked into the log: %s", captured)
	}
	if strings.Contains(captured, sender.code) {
		t.Fatalf("the verification code %q leaked into the log: %s", sender.code, captured)
	}
}

func TestNoHandlerLogsARawEmailAttribute(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, `"email", email`) {
				continue
			}
			t.Errorf(`%s:%d logs a raw address; use "email_hash", hashEmailForLog(email)`, name, i+1)
		}
	}
}
