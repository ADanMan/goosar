package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type registryToken struct {
	mu    sync.Mutex
	value string
}

type bearerChallenge struct {
	realm   string
	service string
	scope   string
}

func parseBearerChallenge(header string) (bearerChallenge, bool) {
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return bearerChallenge{}, false
	}
	var c bearerChallenge
	for _, part := range splitChallengeParams(header[len(prefix):]) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "realm":
			c.realm = value
		case "service":
			c.service = value
		case "scope":
			c.scope = value
		}
	}
	if c.realm == "" {
		return bearerChallenge{}, false
	}
	return c, true
}

func splitChallengeParams(s string) []string {
	var out []string
	var current strings.Builder
	inQuotes := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			current.WriteRune(r)
		case r == ',' && !inQuotes:
			out = append(out, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

func (s *OCIPackageStore) fetchRegistryToken(ctx context.Context, c bearerChallenge) (string, error) {

	if strings.HasPrefix(strings.ToLower(s.BaseURL), "https://") && !strings.HasPrefix(strings.ToLower(c.realm), "https://") {
		return "", fmt.Errorf("provisioning: registry answered an https endpoint with a non-https auth realm — "+
			"refusing to send credentials to %q", c.realm)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.realm, nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	if c.service != "" {
		q.Set("service", c.service)
	}
	if c.scope != "" {
		q.Set("scope", c.scope)
	}
	req.URL.RawQuery = q.Encode()
	if s.Username != "" || s.Password != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("provisioning: registry auth request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("provisioning: registry authentication rejected the configured credentials (status %d) — "+
			"check GOOSAR_PROVISIONING_OCI_USERNAME/PASSWORD; a GHCR token needs the read:packages scope", resp.StatusCode)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", fmt.Errorf("provisioning: registry auth response is not JSON: %w", err)
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("provisioning: registry auth response carried no token")
	}
	return token, nil
}

func (s *OCIPackageStore) doAuthed(ctx context.Context, newReq func() (*http.Request, error)) (*http.Response, error) {
	send := func(token string) (*http.Response, error) {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		} else if s.Username != "" || s.Password != "" {
			req.SetBasicAuth(s.Username, s.Password)
		}
		return s.httpClient().Do(req)
	}

	resp, err := send(s.cachedToken())
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	challenge, ok := parseBearerChallenge(resp.Header.Get("WWW-Authenticate"))
	resp.Body.Close()
	if !ok {
		return nil, fmt.Errorf("provisioning: registry refused the request (401) and advertised no bearer challenge — " +
			"authentication failed; check GOOSAR_PROVISIONING_OCI_USERNAME/PASSWORD")
	}
	token, err := s.fetchRegistryToken(ctx, challenge)
	if err != nil {
		return nil, err
	}
	s.storeToken(token)
	return send(token)
}

func (s *OCIPackageStore) cachedToken() string {
	s.token.mu.Lock()
	defer s.token.mu.Unlock()
	return s.token.value
}

func (s *OCIPackageStore) storeToken(value string) {
	s.token.mu.Lock()
	s.token.value = value
	s.token.mu.Unlock()
}
