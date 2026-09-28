package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyHMACSignature(t *testing.T) {
	body := []byte(`{"a":1}`)
	sig := sign("s3cr3t", body)
	if !verifyHMACSignature("s3cr3t", body, sig) {
		t.Fatal("expected valid signature to verify")
	}
	if verifyHMACSignature("other", body, sig) {
		t.Fatal("expected signature with wrong secret to fail")
	}
	if verifyHMACSignature("s3cr3t", body, "sha256=deadbeef") {
		t.Fatal("expected mismatched signature to fail")
	}
	if verifyHMACSignature("s3cr3t", body, "") {
		t.Fatal("expected missing signature to fail")
	}
}

func TestExtractDisplayKeys(t *testing.T) {
	got := extractDisplayKeys("Fixes T-123 and also mentions t-123 again; branch feature/ABCD-42")
	want := map[string]bool{"T-123": true, "ABCD-42": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, k := range got {
		if !want[k] {
			t.Fatalf("unexpected key %q in %v", k, got)
		}
	}
}

// TestGitHubWebhookPingRespondsPong проверяет ping без похода в БД (не
// требует Store) — только сигнатуру и роутинг по X-GitHub-Event.
func TestGitHubWebhookPingRespondsPong(t *testing.T) {
	d := &Deps{}
	d.Cfg.GitHubWebhookSecret = "whsec"
	body := []byte(`{"zen":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", sign("whsec", body))
	rec := httptest.NewRecorder()
	d.handleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for ping, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"pong"`) {
		t.Fatalf("expected pong body, got %s", rec.Body.String())
	}
}

func TestGitHubWebhookRejectsBadSignature(t *testing.T) {
	d := &Deps{}
	d.Cfg.GitHubWebhookSecret = "whsec"
	body := []byte(`{"zen":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()
	d.handleGitHubWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad signature, got %d", rec.Code)
	}
}

func TestGitHubWebhookUnconfiguredReturns503(t *testing.T) {
	d := &Deps{}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader("{}"))
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	d.handleGitHubWebhook(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when GITHUB_WEBHOOK_SECRET unset, got %d", rec.Code)
	}
}
