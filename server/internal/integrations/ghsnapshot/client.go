// Пакет ghsnapshot получает состояние CI и mergeability пул-реквеста из GitHub API
// и считает этот ответ единственным источником истины для карточки PR.
// Вебхуки и открытия страницы только инициируют обновление.
package ghsnapshot

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

const (
	defaultAPIBase = "https://api.github.com"

	tokenRenewSkew = 5 * time.Minute
)

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limited, retry after %s", e.RetryAfter)
}

type Client struct {
	appID      string
	privateKey *rsa.PrivateKey
	apiBase    string
	httpClient *http.Client
	now        func() time.Time

	mu     sync.Mutex
	tokens map[int64]cachedToken

	sf singleflight.Group
}

type cachedToken struct {
	token  string
	expiry time.Time
}

func NewClientFromEnv() (*Client, error) {
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	pemKey := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if appID == "" || pemKey == "" {
		return nil, nil
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pemKey))
	if err != nil {

		return nil, fmt.Errorf("parse GITHUB_APP_PRIVATE_KEY: %w", err)
	}
	return &Client{
		appID:      appID,
		privateKey: key,
		apiBase:    defaultAPIBase,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		now:        time.Now,
		tokens:     map[int64]cachedToken{},
	}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.privateKey != nil }

func (c *Client) signAppJWT(now time.Time) (string, error) {
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.appID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(c.privateKey)
	if err != nil {
		return "", errors.New("sign App JWT failed")
	}
	return signed, nil
}

func (c *Client) installationToken(ctx context.Context, installationID int64) (string, error) {
	if tok, ok := c.cachedToken(installationID); ok {
		return tok, nil
	}
	v, err, _ := c.sf.Do(strconv.FormatInt(installationID, 10), func() (any, error) {

		if tok, ok := c.cachedToken(installationID); ok {
			return tok, nil
		}
		return c.mintInstallationToken(ctx, installationID)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (c *Client) cachedToken(installationID int64) (string, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[installationID]; ok && now.Add(tokenRenewSkew).Before(t.expiry) {
		return t.token, true
	}
	return "", false
}

func (c *Client) mintInstallationToken(ctx context.Context, installationID int64) (string, error) {
	now := c.now()
	appJWT, err := c.signAppJWT(now)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", strings.TrimRight(c.apiBase, "/"), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+appJWT)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {

		return "", fmt.Errorf("github installation token: unexpected status %d", resp.StatusCode)
	}
	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", errors.New("github installation token: malformed response")
	}
	if parsed.Token == "" {
		return "", errors.New("github installation token: empty token")
	}
	expiry := now.Add(time.Hour)
	if parsed.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, parsed.ExpiresAt); err == nil {
			expiry = t
		}
	}
	c.mu.Lock()
	c.tokens[installationID] = cachedToken{token: parsed.Token, expiry: expiry}
	c.mu.Unlock()
	return parsed.Token, nil
}

func (c *Client) graphQL(ctx context.Context, installationID int64, query string, variables map[string]any) (json.RawMessage, error) {
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.apiBase, "/") + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github graphql: unexpected status %d", resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errors.New("github graphql: malformed response")
	}
	if len(envelope.Errors) > 0 {
		for _, e := range envelope.Errors {
			if e.Type == "RATE_LIMITED" {
				return nil, &RateLimitError{RetryAfter: time.Minute}
			}
		}

		return nil, fmt.Errorf("github graphql error: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data) == 0 {
		return nil, errors.New("github graphql: empty data")
	}
	return envelope.Data, nil
}

func rateLimitFromResponse(resp *http.Response, now time.Time) *RateLimitError {
	wait := time.Minute
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		}
	} else if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			if d := time.Unix(unix, 0).Sub(now); d > 0 {
				wait = d
			}
		}
	}
	if wait < time.Second {
		wait = time.Second
	}
	if wait > 5*time.Minute {
		wait = 5 * time.Minute
	}
	return &RateLimitError{RetryAfter: wait}
}
