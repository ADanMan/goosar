package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server2/internal/config"
)

// testRSAPrivateKeyPEM — сгенерированный на старте пакетных тестов RSA-ключ
// только для подписи app-JWT в TestGitHubClientInstallationAccount; httptest
// не проверяет саму подпись (фейковый GitHub не разбирает JWT), важно только,
// что appJWT() успешно подписывает валидным ключом.
var testRSAPrivateKeyPEM = generateTestRSAKeyPEM()

func generateTestRSAKeyPEM() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return string(pem.EncodeToMemory(block))
}

func TestVCSClientGitLabValidateToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "tok123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"username": "alice"})
	}))
	defer srv.Close()

	c := newVCSClient(srv.Client())
	login, err := c.ValidateToken(context.Background(), "gitlab", srv.URL, "tok123")
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if login != "alice" {
		t.Fatalf("expected login alice, got %q", login)
	}
}

func TestVCSClientRejectsBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newVCSClient(srv.Client())
	_, err := c.ValidateToken(context.Background(), "gitlab", srv.URL, "bad")
	if !isAuthRejection(err) {
		t.Fatalf("expected auth rejection error, got %v", err)
	}
}

func TestVCSClientUnsupportedProvider(t *testing.T) {
	c := newVCSClient(http.DefaultClient)
	_, err := c.ValidateToken(context.Background(), "svn", "https://example.test", "tok")
	if err != errUnsupportedProvider {
		t.Fatalf("expected errUnsupportedProvider, got %v", err)
	}
}

func TestSlackClientAuthTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth.test" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer xoxb-1" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "team_id": "T1", "user_id": "U1"})
	}))
	defer srv.Close()

	c := newSlackClient(srv.Client(), config.Config{SlackAPIBaseURL: srv.URL})
	teamID, botUserID, err := c.AuthTest(context.Background(), "xoxb-1")
	if err != nil {
		t.Fatalf("AuthTest: %v", err)
	}
	if teamID != "T1" || botUserID != "U1" {
		t.Fatalf("unexpected result %q %q", teamID, botUserID)
	}
	if _, _, err := c.AuthTest(context.Background(), "xoxb-bad"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestComposioClientListToolkitsAndConnectInit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/toolkits":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"slug": "gmail", "name": "Gmail", "connectable": true}}})
		case "/connected_accounts/link":
			_ = json.NewEncoder(w).Encode(map[string]any{"redirect_url": "https://composio.test/oauth"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newComposioClient(srv.Client(), config.Config{ComposioAPIBaseURL: srv.URL, ComposioAPIKey: "key123"})
	if !c.Configured() {
		t.Fatal("expected Configured() true with api key set")
	}
	toolkits, err := c.ListToolkits(context.Background())
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	if len(toolkits) != 1 || toolkits[0].Slug != "gmail" {
		t.Fatalf("unexpected toolkits: %+v", toolkits)
	}
	redirect, err := c.ConnectInit(context.Background(), "gmail", "https://app.test/cb", "state123")
	if err != nil {
		t.Fatalf("ConnectInit: %v", err)
	}
	if redirect != "https://composio.test/oauth" {
		t.Fatalf("unexpected redirect url %q", redirect)
	}
}

func TestComposioClientNotConfigured(t *testing.T) {
	c := newComposioClient(http.DefaultClient, config.Config{})
	if c.Configured() {
		t.Fatal("expected Configured() false without api key")
	}
	if _, err := c.ListToolkits(context.Background()); err != errNotConfigured {
		t.Fatalf("expected errNotConfigured, got %v", err)
	}
}

func TestGitHubClientInstallationAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account": map[string]any{"login": "octo-org", "type": "Organization", "avatar_url": "https://img/1"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := config.Config{GitHubAPIBaseURL: srv.URL, GitHubAppID: "123", GitHubAppPrivateKey: testRSAPrivateKeyPEM}
	c := newGitHubClient(srv.Client(), cfg)
	if !c.Configured() {
		t.Fatal("expected Configured() true")
	}
	acc, err := c.InstallationAccount(context.Background(), 42)
	if err != nil {
		t.Fatalf("InstallationAccount: %v", err)
	}
	if acc.Login != "octo-org" || acc.Type != "Organization" {
		t.Fatalf("unexpected account: %+v", acc)
	}
}

func TestGitHubClientNotConfigured(t *testing.T) {
	c := newGitHubClient(http.DefaultClient, config.Config{})
	if c.Configured() {
		t.Fatal("expected Configured() false without app id/private key")
	}
	if _, err := c.InstallationAccount(context.Background(), 1); err != errNotConfigured {
		t.Fatalf("expected errNotConfigured, got %v", err)
	}
}
