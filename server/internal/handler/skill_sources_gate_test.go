package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/skillsources"
)

type stubTransport struct {
	mu    sync.Mutex
	calls []string
	fn    func(req *http.Request) (*http.Response, error)
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req.URL.String())
	s.mu.Unlock()
	if s.fn == nil {
		return nil, fmt.Errorf("unexpected outbound request to %s", req.URL)
	}
	return s.fn(req)
}

func (s *stubTransport) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func stubImportClient(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	prev := newImportHTTPClient
	newImportHTTPClient = func() *http.Client {
		return &http.Client{Transport: transport}
	}
	t.Cleanup(func() { newImportHTTPClient = prev })
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSearchSkills_DisabledClawHubIsClean403NotUpstreamError(t *testing.T) {
	t.Setenv(deliveryprofile.EnvVar, "")
	t.Setenv(skillsources.EnvVar, "github,skillssh")

	transport := &stubTransport{}
	stubImportClient(t, transport)

	w := httptest.NewRecorder()
	testHandler.SearchSkills(w, newRequest(http.MethodGet, "/api/skills/search?q=react", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "source_disabled" {
		t.Errorf("code = %q, want source_disabled", body["code"])
	}
	if !strings.Contains(body["error"], skillsources.EnvVar) {
		t.Errorf("error must name %s, got %q", skillsources.EnvVar, body["error"])
	}
	if got := transport.callCount(); got != 0 {
		t.Errorf("disabled search must not reach the network, saw %d requests: %v", got, transport.calls)
	}
}

func TestImportSkill_SourceGateMatrix(t *testing.T) {
	urls := map[string]string{
		"clawhub":  "https://clawhub.ai/acme/some-skill",
		"skillssh": "https://skills.sh/acme/repo/some-skill",
		"github":   "https://github.com/acme/repo",
	}

	cases := []struct {
		name        string
		profile     string
		sources     string
		wantAllowed map[string]bool
	}{
		{
			name:        "cloud default allows every source",
			profile:     "",
			sources:     "",
			wantAllowed: map[string]bool{"clawhub": true, "skillssh": true, "github": true},
		},
		{
			name:        "perimeter default disables every source",
			profile:     "perimeter",
			sources:     "",
			wantAllowed: map[string]bool{"clawhub": false, "skillssh": false, "github": false},
		},
		{
			name:        "explicit none disables every source on cloud",
			profile:     "",
			sources:     "none",
			wantAllowed: map[string]bool{"clawhub": false, "skillssh": false, "github": false},
		},
		{
			name:        "perimeter with github re-enabled",
			profile:     "perimeter",
			sources:     "github",
			wantAllowed: map[string]bool{"clawhub": false, "skillssh": false, "github": true},
		},
		{
			name:        "cloud with clawhub only",
			profile:     "",
			sources:     "clawhub",
			wantAllowed: map[string]bool{"clawhub": true, "skillssh": false, "github": false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(deliveryprofile.EnvVar, tc.profile)
			t.Setenv(skillsources.EnvVar, tc.sources)

			for sourceName, url := range urls {
				transport := &stubTransport{}
				stubImportClient(t, transport)

				w := httptest.NewRecorder()
				testHandler.ImportSkill(w, newRequest(http.MethodPost, "/api/skills/import", map[string]string{"url": url}))

				if tc.wantAllowed[sourceName] {
					if w.Code == http.StatusForbidden {
						t.Errorf("%s: expected the gate to pass, got 403: %s", sourceName, w.Body.String())
					}
					if transport.callCount() == 0 {
						t.Errorf("%s: enabled source should have attempted a fetch", sourceName)
					}
					continue
				}
				if w.Code != http.StatusForbidden {
					t.Errorf("%s: expected 403, got %d: %s", sourceName, w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), skillsources.EnvVar) {
					t.Errorf("%s: error must name %s, got %s", sourceName, skillsources.EnvVar, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), sourceName) {
					t.Errorf("%s: error must name the disabled source, got %s", sourceName, w.Body.String())
				}
				if got := transport.callCount(); got != 0 {
					t.Errorf("%s: disabled import must not reach the network, saw %d requests: %v", sourceName, got, transport.calls)
				}
			}
		})
	}
}

func TestImportSkill_BareSlugDefaultsToClawHubGate(t *testing.T) {
	t.Setenv(deliveryprofile.EnvVar, "perimeter")
	t.Setenv(skillsources.EnvVar, "")
	transport := &stubTransport{}
	stubImportClient(t, transport)

	w := httptest.NewRecorder()
	testHandler.ImportSkill(w, newRequest(http.MethodPost, "/api/skills/import", map[string]string{"url": "some-skill"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bare slug on perimeter, got %d: %s", w.Code, w.Body.String())
	}
	if got := transport.callCount(); got != 0 {
		t.Fatalf("expected zero outbound requests, saw %v", transport.calls)
	}
}

func TestGitHubTokenNeverAttachedWhenGitHubSourceDisabled(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret-token")

	cases := []struct {
		name     string
		profile  string
		sources  string
		wantAuth bool
	}{
		{name: "cloud default attaches the token", profile: "", sources: "", wantAuth: true},
		{name: "explicit github attaches the token", profile: "perimeter", sources: "github", wantAuth: true},
		{name: "github disabled by list", profile: "", sources: "clawhub,skillssh", wantAuth: false},
		{name: "perimeter default", profile: "perimeter", sources: "", wantAuth: false},
		{name: "explicit none", profile: "", sources: "none", wantAuth: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(deliveryprofile.EnvVar, tc.profile)
			t.Setenv(skillsources.EnvVar, tc.sources)

			apiReq, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/acme/repo", nil)
			if err != nil {
				t.Fatal(err)
			}
			addGitHubAuthHeader(apiReq)
			if got := apiReq.Header.Get("Authorization") != ""; got != tc.wantAuth {
				t.Errorf("api.github.com Authorization present = %v, want %v", got, tc.wantAuth)
			}

			rawReq, err := newRawFileRequest(t.Context(), "https://raw.githubusercontent.com/acme/repo/main/SKILL.md")
			if err != nil {
				t.Fatal(err)
			}
			if got := rawReq.Header.Get("Authorization") != ""; got != tc.wantAuth {
				t.Errorf("raw.githubusercontent.com Authorization present = %v, want %v", got, tc.wantAuth)
			}
		})
	}
}

func TestGetConfig_SkillSourcesField(t *testing.T) {
	getConfig := func(t *testing.T) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.GetConfig(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GetConfig: expected 200, got %d", w.Code)
		}
		var cfg map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg
	}

	t.Run("cloud default omits the field", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "")
		t.Setenv(skillsources.EnvVar, "")
		if _, present := getConfig(t)["skill_sources"]; present {
			t.Fatal("skill_sources must be omitted on the implicit cloud default")
		}
	})

	t.Run("perimeter default advertises an explicit empty list", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "perimeter")
		t.Setenv(skillsources.EnvVar, "")
		got, present := getConfig(t)["skill_sources"]
		if !present {
			t.Fatal("skill_sources must be present on perimeter")
		}
		list, ok := got.([]any)
		if !ok || len(list) != 0 {
			t.Fatalf("skill_sources = %#v, want []", got)
		}
	})

	t.Run("explicit list is advertised in stable order", func(t *testing.T) {
		t.Setenv(deliveryprofile.EnvVar, "")
		t.Setenv(skillsources.EnvVar, "skillssh,clawhub")
		got := getConfig(t)["skill_sources"]
		list, ok := got.([]any)
		if !ok || len(list) != 2 || list[0] != "clawhub" || list[1] != "skillssh" {
			t.Fatalf("skill_sources = %#v, want [clawhub skillssh]", got)
		}
	})
}
