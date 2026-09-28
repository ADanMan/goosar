package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captured — то, что Resend-эндпоинт увидел в одном запросе; тесты ниже
// заполняют только интересующие их поля через recordingResendServer.
type captured struct {
	method, path, auth string
	body               resendSendRequest
}

func recordingResendServer(t *testing.T, status int, respBody string) (*captured, *ResendSender) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	sender := NewResendSender("re_secret_key", "Goosar <noreply@goosar.test>")
	sender.BaseURL = srv.URL
	return got, sender
}

func TestResendSenderSendsExpectedRequest(t *testing.T) {
	got, sender := recordingResendServer(t, http.StatusOK, `{"id":"re_test"}`)

	if err := sender.Send(context.Background(), Message{To: "user@example.test", Subject: "Код для входа", Body: "Код: 123456"}); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	switch {
	case got.method != http.MethodPost, got.path != "/emails":
		t.Errorf("request line = %s %s, want POST /emails", got.method, got.path)
	case got.auth != "Bearer re_secret_key":
		t.Errorf("Authorization = %q", got.auth)
	case got.body.From != "Goosar <noreply@goosar.test>":
		t.Errorf("from = %q", got.body.From)
	case len(got.body.To) != 1 || got.body.To[0] != "user@example.test":
		t.Errorf("to = %v", got.body.To)
	case got.body.Subject != "Код для входа" || got.body.Text != "Код: 123456":
		t.Errorf("subject/text mismatch: %+v", got.body)
	}
}

func TestResendSenderPropagatesAPIError(t *testing.T) {
	_, sender := recordingResendServer(t, http.StatusUnauthorized, `{"message":"API key is invalid"}`)

	err := sender.Send(context.Background(), Message{To: "user@example.test", Subject: "s", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "API key is invalid") {
		t.Fatalf("Send error = %v, want it to mention the Resend message", err)
	}
}

func TestResendSenderRequiresAPIKey(t *testing.T) {
	sender := NewResendSender("", "noreply@goosar.test")
	if err := sender.Send(context.Background(), Message{To: "a@b.test", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("Send: expected error when RESEND_API_KEY is empty")
	}
}
