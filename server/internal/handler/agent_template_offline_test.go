package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/agenttmpl"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/skillsources"
)

func deleteWorkspaceSkillsByName(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM skill WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name); err != nil {
			t.Fatalf("delete skill %q: %v", name, err)
		}
	}
}

func cleanupCreatedTemplateAgent(t *testing.T, resp CreateAgentFromTemplateResponse) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, resp.Agent.ID)
		for _, skillID := range resp.ImportedSkillIDs {
			testPool.Exec(ctx, `DELETE FROM skill_file WHERE skill_id = $1`, skillID)
			testPool.Exec(ctx, `DELETE FROM agent_skill WHERE skill_id = $1`, skillID)
			testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
		}
	})
}

func TestCreateAgentFromTemplate_FullyOfflineFromVendoredSkills(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Setenv(deliveryprofile.EnvVar, "perimeter")
	t.Setenv(skillsources.EnvVar, "")

	transport := &stubTransport{}
	stubImportClient(t, transport)

	const templateSlug = "webapp-tester"
	tmpl, ok := agentTemplates.Get(templateSlug)
	if !ok {
		t.Fatalf("expected template %q to be loaded", templateSlug)
	}
	if len(tmpl.Skills) == 0 {
		t.Fatalf("template %q must reference at least one skill for this test", templateSlug)
	}
	vendored, ok := vendoredTemplateSkills.ForURL(tmpl.Skills[0].SourceURL)
	if !ok {
		t.Fatalf("template %q skill %s has no vendored copy", templateSlug, tmpl.Skills[0].SourceURL)
	}

	deleteWorkspaceSkillsByName(t, tmpl.Skills[0].CachedName, vendored.Name)

	w := httptest.NewRecorder()
	testHandler.CreateAgentFromTemplate(w, newRequest("POST", "/api/agents/from-template?workspace_id="+testWorkspaceID, map[string]any{
		"template_slug": templateSlug,
		"name":          "offline-template-agent",
		"runtime_id":    handlerTestRuntimeID(t),
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateAgentFromTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cleanupCreatedTemplateAgent(t, resp)

	if got := transport.callCount(); got != 0 {
		t.Fatalf("offline template create must make zero outbound requests, saw %d: %v", got, transport.calls)
	}
	if len(resp.ImportedSkillIDs) != 1 {
		t.Fatalf("expected 1 imported skill, got %d (reused %d)", len(resp.ImportedSkillIDs), len(resp.ReusedSkillIDs))
	}

	var name, content string
	var configRaw []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT name, content, config FROM skill WHERE id = $1`, resp.ImportedSkillIDs[0],
	).Scan(&name, &content, &configRaw); err != nil {
		t.Fatalf("load created skill: %v", err)
	}
	if name != vendored.Name {
		t.Errorf("skill name = %q, want vendored %q", name, vendored.Name)
	}
	if content != vendored.Content {
		t.Errorf("skill content does not match the vendored SKILL.md (len %d vs %d)", len(content), len(vendored.Content))
	}
	var config struct {
		Origin map[string]any `json:"origin"`
	}
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatalf("decode skill config: %v", err)
	}
	if config.Origin["vendored"] != true {
		t.Errorf("origin.vendored = %v, want true", config.Origin["vendored"])
	}
	if config.Origin["type"] != "agent_template" {
		t.Errorf("origin.type = %v, want agent_template", config.Origin["type"])
	}
}

func swapAgentTemplates(t *testing.T, reg *agenttmpl.Registry) {
	t.Helper()
	prev := agentTemplates
	agentTemplates = reg
	t.Cleanup(func() { agentTemplates = prev })
}

func fallbackTestRegistry() *agenttmpl.Registry {
	return agenttmpl.NewRegistryForTest(agenttmpl.Template{
		Slug:         "fallback-probe",
		Name:         "Fallback Probe",
		Instructions: "probe",
		Skills: []agenttmpl.TemplateSkillRef{{
			SourceURL:  "https://github.com/acme/repo/tree/main/skills/probe-skill",
			CachedName: "probe-skill",
		}},
	})
}

func TestCreateAgentFromTemplate_NetworkFallbackWhenNotVendored(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	t.Setenv(deliveryprofile.EnvVar, "")
	t.Setenv(skillsources.EnvVar, "")
	swapAgentTemplates(t, fallbackTestRegistry())
	deleteWorkspaceSkillsByName(t, "probe-skill")

	skillMd := "---\nname: probe-skill\ndescription: probe\n---\n# Probe\n"
	transport := &stubTransport{fn: func(req *http.Request) (*http.Response, error) {
		u := req.URL.String()
		switch {
		case strings.HasPrefix(u, "https://api.github.com/repos/acme/repo/commits/main/"):

			return jsonResponse(http.StatusNotFound, `{"message":"not found"}`), nil
		case strings.HasPrefix(u, "https://api.github.com/repos/acme/repo/commits/main"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.github.v3.sha"}},
				Body:       io.NopCloser(strings.NewReader("abc123")),
			}, nil
		case u == "https://raw.githubusercontent.com/acme/repo/main/skills/probe-skill/SKILL.md":
			return jsonResponse(http.StatusOK, skillMd), nil
		case strings.HasPrefix(u, "https://api.github.com/repos/acme/repo/git/trees/"):
			return jsonResponse(http.StatusOK, `{"tree":[{"path":"skills/probe-skill/SKILL.md","type":"blob","size":10}],"truncated":false}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{"message":"not found"}`), nil
		}
	}}
	stubImportClient(t, transport)

	w := httptest.NewRecorder()
	testHandler.CreateAgentFromTemplate(w, newRequest("POST", "/api/agents/from-template?workspace_id="+testWorkspaceID, map[string]any{
		"template_slug": "fallback-probe",
		"name":          "fallback-template-agent",
		"runtime_id":    handlerTestRuntimeID(t),
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 via network fallback, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateAgentFromTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cleanupCreatedTemplateAgent(t, resp)

	if transport.callCount() == 0 {
		t.Fatal("fallback path should have fetched from the stubbed upstream")
	}
	if len(resp.ImportedSkillIDs) != 1 {
		t.Fatalf("expected 1 imported skill, got %d", len(resp.ImportedSkillIDs))
	}
	var content string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content FROM skill WHERE id = $1`, resp.ImportedSkillIDs[0]).Scan(&content); err != nil {
		t.Fatalf("load created skill: %v", err)
	}
	if content != skillMd {
		t.Errorf("skill content = %q, want the fetched SKILL.md", content)
	}
}

func TestCreateAgentFromTemplate_DisabledSourceBlocksFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	t.Setenv(deliveryprofile.EnvVar, "perimeter")
	t.Setenv(skillsources.EnvVar, "")
	swapAgentTemplates(t, fallbackTestRegistry())
	deleteWorkspaceSkillsByName(t, "probe-skill")

	transport := &stubTransport{}
	stubImportClient(t, transport)

	w := httptest.NewRecorder()
	testHandler.CreateAgentFromTemplate(w, newRequest("POST", "/api/agents/from-template?workspace_id="+testWorkspaceID, map[string]any{
		"template_slug": "fallback-probe",
		"name":          "blocked-template-agent",
		"runtime_id":    handlerTestRuntimeID(t),
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), skillsources.EnvVar) {
		t.Errorf("error must name %s, got %s", skillsources.EnvVar, w.Body.String())
	}
	if got := transport.callCount(); got != 0 {
		t.Errorf("blocked fallback must not reach the network, saw %v", transport.calls)
	}
}
