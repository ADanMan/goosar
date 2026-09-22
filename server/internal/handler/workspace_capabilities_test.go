package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getWorkspaceCapabilities(t *testing.T, wsID, acceptLanguage string) WorkspaceCapabilitiesResponse {
	t.Helper()
	req := newRequest("GET", "/api/workspaces/"+wsID+"/capabilities", nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	req = withURLParam(req, "id", wsID)
	w := httptest.NewRecorder()
	testHandler.GetWorkspaceCapabilities(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspaceCapabilities: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got WorkspaceCapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	return got
}

func capabilityKeys(resp WorkspaceCapabilitiesResponse) []string {
	out := make([]string, 0, len(resp.Capabilities))
	for _, c := range resp.Capabilities {
		out = append(out, c.Key)
	}
	return out
}

func createWorkspaceFromTemplate(t *testing.T, slug, templateKey string) string {
	t.Helper()

	withTestMcpBox(t, newTestMcpBox(t))
	cleanupWorkspaceBySlug(t, slug)
	w := createWorkspaceRequest(t, "Caps "+slug, slug, templateKey)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create workspace from template %q: %d %s", templateKey, w.Code, w.Body.String())
	}
	return workspaceIDBySlug(t, slug)
}

func TestWorkspaceCapabilities_ComesFromTheRoleTemplate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	hrID := createWorkspaceFromTemplate(t, "caps-hr-251", "hr")
	finID := createWorkspaceFromTemplate(t, "caps-fin-251", "finance")

	hr := getWorkspaceCapabilities(t, hrID, "ru-RU,ru;q=0.9")
	fin := getWorkspaceCapabilities(t, finID, "ru-RU,ru;q=0.9")

	if hr.TemplateKey != "hr" {
		t.Fatalf("hr workspace template_key = %q, want hr", hr.TemplateKey)
	}
	if fin.TemplateKey != "finance" {
		t.Fatalf("finance workspace template_key = %q, want finance", fin.TemplateKey)
	}
	if len(hr.Capabilities) == 0 {
		t.Fatalf("hr workspace returned no capabilities")
	}
	if len(fin.Capabilities) == 0 {
		t.Fatalf("finance workspace returned no capabilities")
	}
	hrKeys := strings.Join(capabilityKeys(hr), ",")
	finKeys := strings.Join(capabilityKeys(fin), ",")
	if hrKeys == finKeys {
		t.Fatalf("hr and finance returned the same capability composition (%s) — the page is not role-typed", hrKeys)
	}

	if hr.RoleName == "" || hr.RoleSummary == "" {
		t.Fatalf("hr role heading incomplete: %+v", hr)
	}
	for _, c := range hr.Capabilities {
		if c.Title == "" {
			t.Fatalf("hr capability %q has an empty title", c.Key)
		}
		if c.Body == "" {
			t.Fatalf("hr capability %q has an empty body", c.Key)
		}
	}
}

func TestWorkspaceCapabilities_IsDataNotCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const key = "caps251-devops"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, description, capabilities, position)
		VALUES ($1, '{"en": "DevOps"}', '{"en": "Fifth role, pure data"}',
			'[{"key": "deploys", "title": {"en": "Shipping"}, "body": {"en": "Rolls a release out."}}]', 99)
	`, key); err != nil {
		t.Fatalf("insert fifth role: %v", err)
	}

	wsID := createWorkspaceFromTemplate(t, "caps-devops-251", key)
	got := getWorkspaceCapabilities(t, wsID, "en")
	if len(got.Capabilities) != 1 || got.Capabilities[0].Key != "deploys" {
		t.Fatalf("fifth role capabilities = %+v, want the single seeded entry", got.Capabilities)
	}
	if got.Capabilities[0].Title != "Shipping" {
		t.Fatalf("fifth role capability title = %q, want Shipping", got.Capabilities[0].Title)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE workspace_template
		SET capabilities = '[{"key": "deploys", "title": {"en": "Shipping"}, "body": {"en": "Rolls a release out."}},
		                     {"key": "oncall", "title": {"en": "On call"}, "body": {"en": "Watches the pager."}}]'
		WHERE key = $1
	`, key); err != nil {
		t.Fatalf("edit fifth role: %v", err)
	}
	edited := getWorkspaceCapabilities(t, wsID, "en")
	if strings.Join(capabilityKeys(edited), ",") != "deploys,oncall" {
		t.Fatalf("edited capabilities = %v, want [deploys oncall]", capabilityKeys(edited))
	}
}

func TestWorkspaceCapabilities_LocalizedPerCaller(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("clear user language: %v", err)
	}
	wsID := createWorkspaceFromTemplate(t, "caps-l10n-251", "sales")

	ru := getWorkspaceCapabilities(t, wsID, "ru-RU,ru;q=0.9")
	en := getWorkspaceCapabilities(t, wsID, "en-US,en;q=0.9")
	if ru.Capabilities[0].Title == en.Capabilities[0].Title {
		t.Fatalf("ru and en returned identical text (%q) — the response is not localized", ru.Capabilities[0].Title)
	}

	ko := getWorkspaceCapabilities(t, wsID, "ko-KR,ko;q=0.9")
	if len(ko.Capabilities) != len(en.Capabilities) {
		t.Fatalf("ko dropped capabilities: %d vs %d", len(ko.Capabilities), len(en.Capabilities))
	}
	if ko.Capabilities[0].Title == "" {
		t.Fatalf("ko capability rendered blank instead of falling back")
	}
}

func TestWorkspaceCapabilities_SeededRolesShipEveryProductLocale(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("clear user language: %v", err)
	}

	translated := map[string]string{
		"ru":      "ru-RU,ru;q=0.9",
		"ja":      "ja-JP,ja;q=0.9",
		"ko":      "ko-KR,ko;q=0.9",
		"zh-Hans": "zh-Hans,zh;q=0.9",
	}
	for _, role := range []string{"hr", "finance", "legal", "sales"} {
		wsID := createWorkspaceFromTemplate(t, "caps-l10n-all-"+role, role)
		en := getWorkspaceCapabilities(t, wsID, "en-US,en;q=0.9")
		if en.RoleSummary == "" {
			t.Fatalf("%s: precondition failed, the English summary is empty", role)
		}
		if len(en.Capabilities) == 0 {
			t.Fatalf("%s: precondition failed, the English role lists no capabilities", role)
		}
		for lang, header := range translated {
			got := getWorkspaceCapabilities(t, wsID, header)
			if got.RoleSummary == "" {
				t.Fatalf("%s/%s: role summary is empty", role, lang)
			}
			if got.RoleSummary == en.RoleSummary {
				t.Fatalf("%s/%s: role summary fell back to English (%q)", role, lang, got.RoleSummary)
			}
			if len(got.Capabilities) != len(en.Capabilities) {
				t.Fatalf("%s/%s: dropped capabilities: %d vs %d",
					role, lang, len(got.Capabilities), len(en.Capabilities))
			}
			for i, entry := range got.Capabilities {
				if entry.Title == en.Capabilities[i].Title {
					t.Fatalf("%s/%s: capability %q fell back to English (%q)",
						role, lang, entry.Key, entry.Title)
				}
				if entry.Body == "" || entry.Body == en.Capabilities[i].Body {
					t.Fatalf("%s/%s: capability %q body fell back to English or is empty (%q)",
						role, lang, entry.Key, entry.Body)
				}
			}
		}
	}
}

func TestWorkspaceCapabilities_NoTemplateDegrades(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	got := getWorkspaceCapabilities(t, testWorkspaceID, "ru")
	if got.TemplateKey != "" {
		t.Fatalf("template-less workspace reported template_key %q", got.TemplateKey)
	}
	if got.Capabilities == nil {
		t.Fatalf("capabilities must serialize as [], never null")
	}
	if len(got.Capabilities) != 0 {
		t.Fatalf("template-less workspace returned capabilities: %+v", got.Capabilities)
	}
}

func TestWorkspaceCapabilities_WithdrawnRoleDegrades(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const key = "caps251-withdrawn"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, capabilities, position)
		VALUES ($1, '{"en": "Temp"}', '[{"key": "a", "title": {"en": "A"}, "body": {"en": "B"}}]', 98)
	`, key); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	wsID := createWorkspaceFromTemplate(t, "caps-withdrawn-251", key)
	if len(getWorkspaceCapabilities(t, wsID, "en").Capabilities) != 1 {
		t.Fatalf("precondition failed: seeded role did not render")
	}

	if _, err := testPool.Exec(ctx, `UPDATE workspace_template SET enabled = false WHERE key = $1`, key); err != nil {
		t.Fatalf("withdraw role: %v", err)
	}
	got := getWorkspaceCapabilities(t, wsID, "en")
	if got.TemplateKey != key {
		t.Fatalf("withdrawn role lost its key: %q", got.TemplateKey)
	}
	if len(got.Capabilities) != 0 {
		t.Fatalf("withdrawn role still lists capabilities: %+v", got.Capabilities)
	}
}

func TestWorkspaceCapabilities_CarriesNoSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := createWorkspaceFromTemplate(t, "caps-secrets-251", "sales")
	req := withURLParam(newRequest("GET", "/api/workspaces/"+wsID+"/capabilities", nil), "id", wsID)
	w := httptest.NewRecorder()
	testHandler.GetWorkspaceCapabilities(w, req)
	body := w.Body.String()
	for _, forbidden := range []string{
		"JIRA_PERSONAL_TOKEN", "CONFLUENCE_PERSONAL_TOKEN", "KB_API_TOKEN",
		"B24_WEBHOOK_URL", "EWS_EMAIL", "mcpServers", "env",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("capabilities response leaked %q: %s", forbidden, body)
		}
	}
}

func TestWorkspaceCapabilities_UnnamedRoleIsUnnamedNotSlugged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const key = "caps251-unnamed"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, description, capabilities, position)
		VALUES ($1, '{}', '{}', '[]', 98)
	`, key); err != nil {
		t.Fatalf("insert unnamed role: %v", err)
	}

	wsID := createWorkspaceFromTemplate(t, "caps-unnamed-251", key)
	got := getWorkspaceCapabilities(t, wsID, "en")
	if got.RoleName != "" {
		t.Fatalf("role_name = %q, want \"\" — the raw template key must never be shown as a role name", got.RoleName)
	}

	if got.TemplateKey != key {
		t.Fatalf("template_key = %q, want %q", got.TemplateKey, key)
	}
}

func TestWorkspaceCapabilities_OneBadEntryDoesNotEraseTheRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const key = "caps251-partial"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, description, capabilities, position)
		VALUES ($1, '{"en": "Support"}', '{"en": "Tickets"}',
			'[{"key": "triage", "title": {"en": "Triage"}, "body": {"en": "Sorts the queue."}},
			  {"key": "mail", "title": "Mail", "body": {"en": "Reads the inbox."}},
			  {"key": "reply", "title": {"en": "Replies"}, "body": {"en": "Drafts an answer."}}]', 97)
	`, key); err != nil {
		t.Fatalf("insert partially malformed role: %v", err)
	}

	wsID := createWorkspaceFromTemplate(t, "caps-partial-251", key)
	got := getWorkspaceCapabilities(t, wsID, "en")
	if strings.Join(capabilityKeys(got), ",") != "triage,reply" {
		t.Fatalf("capabilities = %v, want the two well-formed entries [triage reply]", capabilityKeys(got))
	}
	if got.RoleName != "Support" {
		t.Fatalf("role_name = %q, want Support", got.RoleName)
	}
}

func TestWorkspaceSampleTasks_AreRunnableRoleData(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	finID := createWorkspaceFromTemplate(t, "tasks-fin-349", "finance")
	legID := createWorkspaceFromTemplate(t, "tasks-leg-349", "legal")

	fin := getWorkspaceCapabilities(t, finID, "ru-RU,ru;q=0.9")
	leg := getWorkspaceCapabilities(t, legID, "ru-RU,ru;q=0.9")

	if len(fin.SampleTasks) == 0 {
		t.Fatalf("finance returned no sample tasks — the demonstration has nothing to run")
	}
	if len(leg.SampleTasks) == 0 {
		t.Fatalf("legal returned no sample tasks — the demonstration has nothing to run")
	}

	if sampleTaskKeys(fin) == sampleTaskKeys(leg) {
		t.Fatalf("finance and legal share one sample-task composition (%s)", sampleTaskKeys(fin))
	}
	for _, task := range fin.SampleTasks {
		if task.Title == "" || task.Prompt == "" {
			t.Fatalf("finance sample task %q would file an empty issue: %+v", task.Key, task)
		}
		if task.Requires == nil {
			t.Fatalf("finance sample task %q sent requires=null; the client must never see it", task.Key)
		}
	}
}

func TestWorkspaceSampleTasks_HalfWrittenEntryIsDropped(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const key = "tasks349-support"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, description, sample_tasks, position)
		VALUES ($1, '{"en": "Support"}', '{"en": "Fifth role, pure data"}', '[
			{"key": "good",       "title": {"en": "Triage"}, "prompt": {"en": "Sort the queue."}, "requires": ["ews-mcp"]},
			{"key": "no-prompt",  "title": {"en": "Half"}},
			{"key": "no-title",   "prompt": {"en": "Half"}},
			"a bare string an operator typed by mistake"
		]', 98)
	`, key); err != nil {
		t.Fatalf("insert fifth role: %v", err)
	}

	wsID := createWorkspaceFromTemplate(t, "tasks-support-349", key)
	got := getWorkspaceCapabilities(t, wsID, "en")
	if sampleTaskKeys(got) != "good" {
		t.Fatalf("sample tasks = %v, want only the complete one", sampleTaskKeys(got))
	}
	if len(got.SampleTasks[0].Requires) != 1 || got.SampleTasks[0].Requires[0] != "ews-mcp" {
		t.Fatalf("requires = %v, want [ews-mcp] so the client can refuse honestly",
			got.SampleTasks[0].Requires)
	}
}

func sampleTaskKeys(resp WorkspaceCapabilitiesResponse) string {
	out := make([]string, 0, len(resp.SampleTasks))
	for _, task := range resp.SampleTasks {
		out = append(out, task.Key)
	}
	return strings.Join(out, ",")
}
