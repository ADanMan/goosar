package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// apiClient is a thin, deliberately dumb HTTP client: it knows nothing about
// the server's implementation, only about the wire contract (headers,
// Bearer tokens, JSON bodies) that any client of the API would use.
type apiClient struct {
	baseURL string
	hc      *http.Client

	token       string // Bearer token, set after login
	workspaceID string // X-Workspace-ID, set once a workspace exists
}

func newAPIClient(baseURL string) *apiClient {
	return &apiClient{
		baseURL: baseURL,
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *apiClient) withWorkspace(id string) *apiClient {
	clone := *c
	clone.workspaceID = id
	return &clone
}

// raw performs an HTTP call and returns the status code and raw response
// body, without touching the OpenAPI contract. Most call sites should use
// (*harness).call instead, which also validates the response.
func (c *apiClient) raw(t testing.TB, method, path string, body any) (int, []byte, http.Header) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body for %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.workspaceID != "" {
		req.Header.Set("X-Workspace-ID", c.workspaceID)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body for %s %s: %v", method, path, err)
	}
	return resp.StatusCode, data, resp.Header
}

// decodeJSON is a small convenience for pulling fields out of a JSON object
// response without declaring a Go struct for every schema (the OpenAPI
// contract, not a Go type, is the source of truth for shape here).
func decodeJSON(t testing.TB, body []byte) map[string]any {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode JSON object: %v\nbody: %s", err, truncate(body))
	}
	return m
}

func decodeJSONArray(t testing.TB, body []byte) []any {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
	var a []any
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("decode JSON array: %v\nbody: %s", err, truncate(body))
	}
	return a
}

func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func mustStr(t testing.TB, m map[string]any, key string) string {
	t.Helper()
	v := str(m, key)
	if v == "" {
		t.Fatalf("expected non-empty string field %q in %v", key, m)
	}
	return v
}

// uniqueSuffix gives every test run its own emails/slugs/names, so the
// suite can run repeatedly against the same long-lived database. Kept short
// (a handful of characters) since it gets embedded into names that carry
// their own length limits (e.g. a label name is at most 32 characters).
var suffixCounter int64

func uniqueSuffix() string {
	n := atomic.AddInt64(&suffixCounter, 1)
	return fmt.Sprintf("%x%02x", time.Now().Unix()%0xfffff, n%0xff)
}
