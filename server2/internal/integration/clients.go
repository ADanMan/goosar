package integration

// Клиенты внешних API этого домена. Каждый ходит через net/http.Client с
// настраиваемым базовым URL (пусто — реальный внешний хост по умолчанию),
// так что юнит-тесты подставляют httptest.Server, не имея сетевого доступа
// (см. server2/internal/integration/deps.go).
import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/config"
)

var errNotConfigured = errors.New("integration: интеграция не настроена на этом деплое")

// --- GitHub -------------------------------------------------------------------

type githubAccount struct {
	Login  string
	Type   string
	Avatar *string
}

type githubClient interface {
	Configured() bool
	InstallationAccount(ctx context.Context, installationID int64) (githubAccount, error)
	ListRepositories(ctx context.Context, installationID int64, page, perPage int) (repos []githubRepo, total int, err error)
}

type githubRepo struct {
	ID            int64   `json:"id"`
	FullName      string  `json:"full_name"`
	HTMLURL       string  `json:"html_url"`
	CloneURL      string  `json:"clone_url"`
	Description   *string `json:"description"`
	Private       bool    `json:"private"`
	Archived      bool    `json:"archived"`
	DefaultBranch string  `json:"default_branch"`
}

type githubHTTPClient struct {
	http       *http.Client
	baseURL    string
	appID      string
	privateKey string
}

func newGitHubClient(hc *http.Client, cfg config.Config) githubClient {
	base := cfg.GitHubAPIBaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	return &githubHTTPClient{http: hc, baseURL: strings.TrimRight(base, "/"), appID: cfg.GitHubAppID, privateKey: cfg.GitHubAppPrivateKey}
}

func (c *githubHTTPClient) Configured() bool { return c.appID != "" && c.privateKey != "" }

// appJWT — минимальный RS256 JWT GitHub App-аутентификации (iss/iat/exp),
// без сторонней библиотеки: тот же принцип, что internal/authn применяет к
// HS256 сессионным токенам (ADR 0001 §"JWT"), только с ассиметричным
// алгоритмом, который здесь требует сам GitHub, а не наш контракт.
func (c *githubHTTPClient) appJWT() (string, error) {
	block, _ := pem.Decode([]byte(c.privateKey))
	if block == nil {
		return "", errors.New("integration: GITHUB_APP_PRIVATE_KEY: невалидный PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		keyAny, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("integration: разбор приватного ключа GitHub App: %w", err)
		}
		rsaKey, ok := keyAny.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("integration: GITHUB_APP_PRIVATE_KEY не RSA")
		}
		key = rsaKey
	}
	now := time.Now()
	header := b64json(map[string]any{"alg": "RS256", "typ": "JWT"})
	claims := b64json(map[string]any{"iat": now.Add(-30 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": c.appID})
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (c *githubHTTPClient) installationToken(ctx context.Context, installationID int64) (string, error) {
	appToken, err := c.appJWT()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", nil)
	if err != nil {
		return "", err
	}
	c.setAppAuth(req, appToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("integration: github access_tokens: %s", resp.Status)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Token, nil
}

func (c *githubHTTPClient) setAppAuth(req *http.Request, bearer string) {
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/vnd.github+json")
}

func (c *githubHTTPClient) InstallationAccount(ctx context.Context, installationID int64) (githubAccount, error) {
	if !c.Configured() {
		return githubAccount{}, errNotConfigured
	}
	appToken, err := c.appJWT()
	if err != nil {
		return githubAccount{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/app/installations/"+strconv.FormatInt(installationID, 10), nil)
	if err != nil {
		return githubAccount{}, err
	}
	c.setAppAuth(req, appToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return githubAccount{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return githubAccount{}, fmt.Errorf("integration: github installations/%d: %s", installationID, resp.Status)
	}
	var out struct {
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return githubAccount{}, err
	}
	avatar := out.Account.AvatarURL
	return githubAccount{Login: out.Account.Login, Type: out.Account.Type, Avatar: &avatar}, nil
}

func (c *githubHTTPClient) ListRepositories(ctx context.Context, installationID int64, page, perPage int) ([]githubRepo, int, error) {
	if !c.Configured() {
		return nil, 0, errNotConfigured
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, 0, err
	}
	u := c.baseURL + "/installation/repositories?" + url.Values{
		"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("integration: github installation/repositories: %s", resp.Status)
	}
	var out struct {
		TotalCount   int          `json:"total_count"`
		Repositories []githubRepo `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	return out.Repositories, out.TotalCount, nil
}

// --- Slack ----------------------------------------------------------------------

type slackClient interface {
	Configured() bool
	AuthTest(ctx context.Context, botToken string) (teamID, botUserID string, err error)
	ValidateAppToken(ctx context.Context, appToken string) error
}

type slackHTTPClient struct {
	http    *http.Client
	baseURL string
}

func newSlackClient(hc *http.Client, cfg config.Config) slackClient {
	base := cfg.SlackAPIBaseURL
	if base == "" {
		base = "https://slack.com/api"
	}
	return &slackHTTPClient{http: hc, baseURL: strings.TrimRight(base, "/")}
}

func (c *slackHTTPClient) Configured() bool { return true } // BYO: приносит свои токены, деплой ничего не хранит заранее

func (c *slackHTTPClient) call(ctx context.Context, method, token string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *slackHTTPClient) AuthTest(ctx context.Context, botToken string) (string, string, error) {
	out, err := c.call(ctx, "auth.test", botToken)
	if err != nil {
		return "", "", err
	}
	if ok, _ := out["ok"].(bool); !ok {
		return "", "", fmt.Errorf("integration: slack auth.test: %v", out["error"])
	}
	teamID, _ := out["team_id"].(string)
	botUserID, _ := out["user_id"].(string)
	return teamID, botUserID, nil
}

func (c *slackHTTPClient) ValidateAppToken(ctx context.Context, appToken string) error {
	out, err := c.call(ctx, "apps.connections.open", appToken)
	if err != nil {
		return err
	}
	if ok, _ := out["ok"].(bool); !ok {
		return fmt.Errorf("integration: slack apps.connections.open: %v", out["error"])
	}
	return nil
}

// --- Self-hosted VCS --------------------------------------------------------------

type vcsClient interface {
	// ValidateToken проверяет access_token у самого провайдера, возвращая
	// логин учётки, которой он принадлежит. errUnsupportedProvider — provider
	// не входит в поддерживаемый на этой сессии список (см.
	// server2/docs/decisions.md, T-029 "Пробелы спецификации").
	ValidateToken(ctx context.Context, provider, instanceURL, accessToken string) (accountLogin string, err error)
}

var errUnsupportedProvider = errors.New("integration: неподдерживаемый VCS-провайдер")
var errProviderRejected = errors.New("integration: провайдер отверг токен")

type vcsHTTPClient struct{ http *http.Client }

func newVCSClient(hc *http.Client) vcsClient { return &vcsHTTPClient{http: hc} }

func (c *vcsHTTPClient) ValidateToken(ctx context.Context, provider, instanceURL, accessToken string) (string, error) {
	switch strings.ToLower(provider) {
	case "gitlab":
		return c.gitlabUser(ctx, instanceURL, accessToken)
	case "gitea":
		return c.giteaUser(ctx, instanceURL, accessToken)
	default:
		return "", errUnsupportedProvider
	}
}

func (c *vcsHTTPClient) gitlabUser(ctx context.Context, instanceURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(instanceURL, "/")+"/api/v4/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	return c.doUsernameRequest(req, "username")
}

func (c *vcsHTTPClient) giteaUser(ctx context.Context, instanceURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(instanceURL, "/")+"/api/v1/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	return c.doUsernameRequest(req, "login")
}

func (c *vcsHTTPClient) doUsernameRequest(req *http.Request, field string) (string, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("integration: vcs недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("%w: %s", errProviderRejected, string(body))
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("integration: vcs: %s", resp.Status)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	login, _ := out[field].(string)
	return login, nil
}

// --- Composio ---------------------------------------------------------------------

type composioToolkit struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Logo        string `json:"logo"`
	Category    string `json:"category"`
	Connectable bool   `json:"connectable"`
}

type composioClient interface {
	Configured() bool
	ListToolkits(ctx context.Context) ([]composioToolkit, error)
	ConnectInit(ctx context.Context, toolkitSlug, redirectURI, state string) (redirectURL string, err error)
	Disconnect(ctx context.Context, externalConnectionID string) error
}

type composioHTTPClient struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

func newComposioClient(hc *http.Client, cfg config.Config) composioClient {
	base := cfg.ComposioAPIBaseURL
	if base == "" {
		base = "https://backend.composio.dev/api/v3"
	}
	return &composioHTTPClient{http: hc, baseURL: strings.TrimRight(base, "/"), apiKey: cfg.ComposioAPIKey}
}

func (c *composioHTTPClient) Configured() bool { return c.apiKey != "" }

func (c *composioHTTPClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("integration: composio %s %s: %s: %s", method, path, resp.Status, string(b))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *composioHTTPClient) ListToolkits(ctx context.Context) ([]composioToolkit, error) {
	if !c.Configured() {
		return nil, errNotConfigured
	}
	var out struct {
		Items []composioToolkit `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/toolkits", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *composioHTTPClient) ConnectInit(ctx context.Context, toolkitSlug, redirectURI, state string) (string, error) {
	if !c.Configured() {
		return "", errNotConfigured
	}
	var out struct {
		RedirectURL string `json:"redirect_url"`
	}
	body := map[string]string{"toolkit_slug": toolkitSlug, "redirect_uri": redirectURI, "state": state}
	if err := c.do(ctx, http.MethodPost, "/connected_accounts/link", body, &out); err != nil {
		return "", err
	}
	return out.RedirectURL, nil
}

func (c *composioHTTPClient) Disconnect(ctx context.Context, externalConnectionID string) error {
	if !c.Configured() || externalConnectionID == "" {
		return nil
	}
	return c.do(ctx, http.MethodDelete, "/connected_accounts/"+url.PathEscape(externalConnectionID), nil, nil)
}

func b64json(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}
