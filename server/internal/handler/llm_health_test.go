package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withLLMConfig(t *testing.T, baseURL, apiKey string, client *http.Client) {
	t.Helper()
	prevBase, prevKey := testHandler.cfg.LLMBaseURL, testHandler.cfg.LLMAPIKey
	prevClient := testHandler.llmHealthHTTPClient
	prevCache := testHandler.llmHealth

	testHandler.cfg.LLMBaseURL = baseURL
	testHandler.cfg.LLMAPIKey = apiKey
	testHandler.llmHealthHTTPClient = client
	testHandler.llmHealth = nil

	t.Cleanup(func() {
		testHandler.cfg.LLMBaseURL = prevBase
		testHandler.cfg.LLMAPIKey = prevKey
		testHandler.llmHealthHTTPClient = prevClient
		testHandler.llmHealth = prevCache
	})
}

func getLLMHealth(t *testing.T) (*httptest.ResponseRecorder, LLMHealthResponse) {
	t.Helper()
	req := newRequestAsUser(testUserID, http.MethodGet, "/api/llm/health", nil)
	w := httptest.NewRecorder()
	testHandler.GetLLMHealth(w, req)
	var resp LLMHealthResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
		}
	}
	return w, resp
}

func TestLLMHealth_OK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	withLLMConfig(t, upstream.URL, "test-key", upstream.Client())

	w, resp := getLLMHealth(t)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if resp.Status != LLMHealthOK {
		t.Fatalf("verdict = %q, want ok", resp.Status)
	}
	if resp.CheckedAt == "" {
		t.Error("checked_at is empty")
	}

	if strings.Contains(w.Body.String(), "test-key") || strings.Contains(w.Body.String(), "data") {
		t.Errorf("response leaked upstream content: %s", w.Body.String())
	}
}

func TestLLMHealth_AuthRejected(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		withLLMConfig(t, upstream.URL, "bad-key", upstream.Client())

		_, resp := getLLMHealth(t)
		if resp.Status != LLMHealthAuthRejected {
			t.Errorf("code %d: verdict = %q, want auth_rejected", code, resp.Status)
		}
		upstream.Close()
	}
}

func TestLLMHealth_Degraded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	withLLMConfig(t, upstream.URL, "test-key", upstream.Client())

	_, resp := getLLMHealth(t)
	if resp.Status != LLMHealthDegraded {
		t.Fatalf("verdict = %q, want degraded", resp.Status)
	}
}

func TestLLMHealth_Unreachable(t *testing.T) {

	withLLMConfig(t, "http://127.0.0.1:1", "test-key", &http.Client{Timeout: 0})

	_, resp := getLLMHealth(t)
	if resp.Status != LLMHealthUnreachable {
		t.Fatalf("verdict = %q, want unreachable", resp.Status)
	}
}

func TestLLMHealth_Unconfigured(t *testing.T) {
	withLLMConfig(t, "", "", nil)

	w, resp := getLLMHealth(t)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if resp.Status != LLMHealthUnconfigured {
		t.Fatalf("verdict = %q, want unconfigured", resp.Status)
	}
}

func TestLLMHealth_Cached(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	withLLMConfig(t, upstream.URL, "test-key", upstream.Client())

	_, first := getLLMHealth(t)
	_, second := getLLMHealth(t)

	if calls != 1 {
		t.Fatalf("upstream called %d times, want 1 (second call should be served from the 60s cache)", calls)
	}
	if first.CheckedAt != second.CheckedAt {
		t.Fatalf("cached response should be identical: %+v vs %+v", first, second)
	}
}
