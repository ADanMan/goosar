package contract

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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
		hc: &http.Client{
			Timeout: 30 * time.Second,
			// A few operations (OAuth/App-install callbacks) answer with a
			// 302 to FRONTEND_ORIGIN as their documented response — this
			// suite asserts on that redirect itself, not on whatever (if
			// anything) is actually listening at that origin in this
			// environment, so redirects are never followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
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
	// Every real client (web or desktop) identifies itself on the wire; the
	// contract documents this and at least one operation (meUpsertClientUsage)
	// hard-requires it to be "web" or "desktop" (docs/50-api-contract.yaml,
	// docs/50-api-contract.md - "X-Client-Platform"). This suite always talks
	// like a web client.
	req.Header.Set("X-Client-Platform", "web")
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

// randomUUID generates an RFC 4122 v4 UUID — for request fields the contract
// documents as `format: uuid` but that no fixture resource naturally supplies
// (e.g. a client install id).
func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// rawMultipart performs a multipart/form-data POST (currently the only shape
// this suite needs it for: POST /api/upload-file's `file` field, plus a few
// optional plain-text fields), returning status and raw body like raw does.
func (c *apiClient) rawMultipart(t testing.TB, path string, fields map[string]string, fileFieldName, fileName string, fileContent []byte) (int, []byte) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("multipart WriteField(%s): %v", k, err)
		}
	}
	fw, err := w.CreateFormFile(fileFieldName, fileName)
	if err != nil {
		t.Fatalf("multipart CreateFormFile: %v", err)
	}
	if _, err := fw.Write(fileContent); err != nil {
		t.Fatalf("multipart write file content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart Close: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		t.Fatalf("build multipart request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Client-Platform", "web")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.workspaceID != "" {
		req.Header.Set("X-Workspace-ID", c.workspaceID)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("multipart request %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read multipart response body for %s: %v", path, err)
	}
	return resp.StatusCode, data
}
