package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"
)

var workspaceTemplateSeedKeys = roleCatalogKeys

func cleanupWorkspaceBySlug(t *testing.T, slug string) {
	t.Helper()
	clean := func() {
		ctx := context.Background()
		var wsID string
		err := testPool.QueryRow(ctx, `SELECT id FROM workspace WHERE slug = $1`, slug).Scan(&wsID)
		if err == nil {
			testPool.Exec(ctx, `DELETE FROM provisioning_pin WHERE workspace_id = $1`, wsID)
			testPool.Exec(ctx, `DELETE FROM workspace_config WHERE workspace_id = $1`, wsID)
			testPool.Exec(ctx, `DELETE FROM workspace_helper_default WHERE workspace_id = $1`, wsID)
			testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
		}
	}
	clean()
	t.Cleanup(clean)
}

func listWorkspaceTemplatesAs(t *testing.T, acceptLanguage string) []WorkspaceTemplateResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspace-templates", nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	testHandler.ListWorkspaceTemplates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceTemplates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []WorkspaceTemplateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode templates: %v", err)
	}
	return got
}

func templateByKey(list []WorkspaceTemplateResponse, key string) (WorkspaceTemplateResponse, bool) {
	for _, tmpl := range list {
		if tmpl.Key == key {
			return tmpl, true
		}
	}
	return WorkspaceTemplateResponse{}, false
}

func createWorkspaceRequest(t *testing.T, name, slug, templateKey string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"name": name, "slug": slug}
	if templateKey != "" {
		body["template_key"] = templateKey
	}
	w := httptest.NewRecorder()
	testHandler.CreateWorkspace(w, newRequest("POST", "/api/workspaces", body))
	return w
}

func workspaceIDBySlug(t *testing.T, slug string) string {
	t.Helper()
	var wsID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM workspace WHERE slug = $1`, slug).Scan(&wsID); err != nil {
		t.Fatalf("workspace %q not found: %v", slug, err)
	}
	return wsID
}

func workspacePins(t *testing.T, wsID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT package_type, package_name, version FROM provisioning_pin WHERE workspace_id = $1
	`, wsID)
	if err != nil {
		t.Fatalf("list pins: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var typ, name, version string
		if err := rows.Scan(&typ, &name, &version); err != nil {
			t.Fatalf("scan pin: %v", err)
		}
		out = append(out, typ+":"+name+"@"+version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pins: %v", err)
	}
	sort.Strings(out)
	return out
}

func TestListWorkspaceTemplates_SeededRoles(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("clear user language: %v", err)
	}

	english := listWorkspaceTemplatesAs(t, "en-US,en;q=0.9")
	for _, key := range workspaceTemplateSeedKeys {
		tmpl, ok := templateByKey(english, key)
		if !ok {
			t.Fatalf("seeded template %q missing from the list: %+v", key, english)
		}
		if tmpl.Name == "" {
			t.Fatalf("template %q has no localized name", key)
		}
		if tmpl.Description == "" {
			t.Fatalf("template %q has no localized description", key)
		}
	}

	finEn, _ := templateByKey(english, "finance")
	if finEn.Name != "Finance" {
		t.Fatalf("finance template name for en = %q, want %q", finEn.Name, "Finance")
	}

	russian := listWorkspaceTemplatesAs(t, "ru-RU,ru;q=0.9")
	finRu, _ := templateByKey(russian, "finance")
	if finRu.Name != "Финансист" {
		t.Fatalf("finance template name for ru = %q, want %q", finRu.Name, "Финансист")
	}
}

func TestListWorkspaceTemplates_FifthRoleIsDataNotCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const key = "e6test-devops"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, description, position)
		VALUES ($1, '{"en": "DevOps"}', '{"en": "Fifth role, pure data"}', 99)
	`, key); err != nil {
		t.Fatalf("insert fifth role: %v", err)
	}

	list := listWorkspaceTemplatesAs(t, "en")
	tmpl, ok := templateByKey(list, key)
	if !ok {
		t.Fatalf("fifth role %q did not appear in the catalog without a rebuild", key)
	}
	if tmpl.Name != "DevOps" {
		t.Fatalf("fifth role name = %q, want DevOps", tmpl.Name)
	}

	if _, err := testPool.Exec(ctx, `UPDATE workspace_template SET enabled = false WHERE key = $1`, key); err != nil {
		t.Fatalf("disable fifth role: %v", err)
	}
	if _, ok := templateByKey(listWorkspaceTemplatesAs(t, "en"), key); ok {
		t.Fatalf("disabled template %q still offered in the catalog", key)
	}
}

func TestCreateWorkspace_WithTemplateAppliesComposition(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	const slug = "e6test-sales-composition"
	cleanupWorkspaceBySlug(t, slug)

	w := createWorkspaceRequest(t, "Sales Composition", slug, "sales")
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace(sales): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	wsID := workspaceIDBySlug(t, slug)

	wantPins := []string{
		"mcp-server:b24-agent@1.0.0",
		"mcp-server:ews-mcp@0.1.1",
		"skill:mail-triage@1.0.0",
		"skill:office-docx@1.0.0",
		"skill:office-pdf@1.0.0",
		"skill:office-pptx@1.0.0",
		"skill:office-xlsx@1.0.0",
		"skill:research@1.0.0",
	}
	gotPins := workspacePins(t, wsID)
	if fmt.Sprint(gotPins) != fmt.Sprint(wantPins) {
		t.Fatalf("sales pins = %v, want %v", gotPins, wantPins)
	}

	var stored []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT mcp_defaults FROM workspace_config WHERE workspace_id = $1`, wsID,
	).Scan(&stored); err != nil {
		t.Fatalf("workspace_config row missing for templated workspace: %v", err)
	}
	if !isSealedMcpConfig(stored) {
		t.Fatalf("template MCP presets stored unsealed: %s", stored)
	}
	doc, err := testHandler.openConfigDocument(stored)
	if err != nil {
		t.Fatalf("open sealed mcp_defaults: %v", err)
	}
	var entries map[string]struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(doc, &entries); err != nil {
		t.Fatalf("decode mcp_defaults: %v", err)
	}
	for _, server := range []string{"b24-agent", "ews-mcp"} {
		entry, ok := entries[server]
		if !ok {
			t.Fatalf("mcp_defaults missing %q: %s", server, doc)
		}
		if entry.Enabled == nil || !*entry.Enabled {
			t.Fatalf("mcp_defaults[%q].enabled != true: %s", server, doc)
		}
	}

	var templateKey string
	var extraRaw []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT template_key, helper_extra_instructions FROM workspace_helper_default WHERE workspace_id = $1`, wsID,
	).Scan(&templateKey, &extraRaw); err != nil {
		t.Fatalf("workspace_helper_default row missing: %v", err)
	}
	if templateKey != "sales" {
		t.Fatalf("helper default template_key = %q, want sales", templateKey)
	}
	var extra map[string]string
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		t.Fatalf("decode helper_extra_instructions: %v", err)
	}
	if extra["ru"] == "" || extra["en"] == "" {
		t.Fatalf("helper extra instructions missing ru/en variants: %v", extra)
	}
}

func TestCreateWorkspace_WithoutTemplateUnchanged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const slug = "e6test-no-template"
	cleanupWorkspaceBySlug(t, slug)

	w := createWorkspaceRequest(t, "No Template", slug, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantKeys := []string{
		"id", "name", "slug", "description", "context", "settings",
		"repos", "issue_prefix", "avatar_url", "created_at", "updated_at",
	}
	if len(body) != len(wantKeys) {
		t.Fatalf("response key count = %d, want %d: %v", len(body), len(wantKeys), body)
	}
	for _, key := range wantKeys {
		if _, ok := body[key]; !ok {
			t.Fatalf("response missing pre-existing key %q: %v", key, body)
		}
	}

	wsID := workspaceIDBySlug(t, slug)
	for _, probe := range []struct{ table string }{
		{"provisioning_pin"}, {"workspace_config"}, {"workspace_helper_default"},
	} {
		var count int
		if err := testPool.QueryRow(context.Background(),
			`SELECT count(*) FROM `+probe.table+` WHERE workspace_id = $1`, wsID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", probe.table, err)
		}
		if count != 0 {
			t.Fatalf("untemplated workspace grew %d %s rows; the old path must stay byte-identical", count, probe.table)
		}
	}
}

func TestCreateWorkspace_UnknownTemplateKeyRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const slug = "e6test-unknown-template"
	cleanupWorkspaceBySlug(t, slug)

	w := createWorkspaceRequest(t, "Unknown Template", slug, "no-such-role")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown template, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE slug = $1`, slug,
	).Scan(&count); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if count != 0 {
		t.Fatalf("workspace row survived a rejected template application")
	}
}

func TestCreateWorkspace_TemplateMcpFailsClosedWithoutSecretKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	previous := testHandler.MCPSecretBox
	testHandler.MCPSecretBox = nil
	t.Cleanup(func() { testHandler.MCPSecretBox = previous })

	const sealedSlug = "e6test-failclosed-sales"
	cleanupWorkspaceBySlug(t, sealedSlug)
	w := createWorkspaceRequest(t, "Fail Closed", sealedSlug, "sales")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without secret key, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace WHERE slug = $1`, sealedSlug,
	).Scan(&count); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if count != 0 {
		t.Fatalf("workspace row survived a fail-closed template application")
	}

	const plainSlug = "e6test-failclosed-finance"
	cleanupWorkspaceBySlug(t, plainSlug)
	w = createWorkspaceRequest(t, "No Secrets Needed", plainSlug, "finance")
	if w.Code != http.StatusCreated {
		t.Fatalf("finance template (no MCP presets) should not need the secret key: %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWorkspace_TemplateWithSecretEnvRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	ctx := context.Background()
	const key = "e6test-leaky"
	testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_template (key, display_name, mcp_defaults)
		VALUES ($1, '{"en": "Leaky"}', '{"crm": {"enabled": true, "env": {"CRM_TOKEN": "sekret-value"}}}')
	`, key); err != nil {
		t.Fatalf("insert leaky template: %v", err)
	}

	const slug = "e6test-leaky-ws"
	cleanupWorkspaceBySlug(t, slug)
	w := createWorkspaceRequest(t, "Leaky", slug, key)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a template carrying an env value, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, slug).Scan(&count); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if count != 0 {
		t.Fatalf("workspace row survived a rejected secret-bearing template")
	}
}

func TestCreateWorkspace_TemplatesAreIndependent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	keys := []string{"e6test-role-a", "e6test-role-b"}
	for _, key := range keys {
		testPool.Exec(ctx, `DELETE FROM workspace_template WHERE key = $1`, key)
	}
	t.Cleanup(func() {
		for _, key := range keys {
			testPool.Exec(context.Background(), `DELETE FROM workspace_template WHERE key = $1`, key)
		}
	})
	for _, key := range keys {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO workspace_template (key, display_name, pins)
			VALUES ($1, '{"en": "Role"}', '[{"name": "research", "type": "skill", "version": "0.1.0"}]')
		`, key); err != nil {
			t.Fatalf("insert template %s: %v", key, err)
		}
	}

	const slugBefore = "e6test-indep-before"
	cleanupWorkspaceBySlug(t, slugBefore)
	if w := createWorkspaceRequest(t, "Independent Before", slugBefore, "e6test-role-a"); w.Code != http.StatusCreated {
		t.Fatalf("create from role-a: %d: %s", w.Code, w.Body.String())
	}
	pinsBefore := workspacePins(t, workspaceIDBySlug(t, slugBefore))

	if _, err := testPool.Exec(ctx, `
		UPDATE workspace_template
		SET pins = '[{"name": "office-docx", "type": "skill", "version": "9.9.9"}]'
		WHERE key = 'e6test-role-b'
	`); err != nil {
		t.Fatalf("edit role-b: %v", err)
	}

	const slugAfter = "e6test-indep-after"
	cleanupWorkspaceBySlug(t, slugAfter)
	if w := createWorkspaceRequest(t, "Independent After", slugAfter, "e6test-role-a"); w.Code != http.StatusCreated {
		t.Fatalf("create from role-a after editing role-b: %d: %s", w.Code, w.Body.String())
	}
	pinsAfter := workspacePins(t, workspaceIDBySlug(t, slugAfter))
	if fmt.Sprint(pinsBefore) != fmt.Sprint(pinsAfter) {
		t.Fatalf("editing role-b changed role-a's application: before %v, after %v", pinsBefore, pinsAfter)
	}

	const slugB = "e6test-indep-b"
	cleanupWorkspaceBySlug(t, slugB)
	if w := createWorkspaceRequest(t, "Independent B", slugB, "e6test-role-b"); w.Code != http.StatusCreated {
		t.Fatalf("create from role-b: %d: %s", w.Code, w.Body.String())
	}
	gotB := workspacePins(t, workspaceIDBySlug(t, slugB))
	wantB := []string{"skill:office-docx@9.9.9"}
	if fmt.Sprint(gotB) != fmt.Sprint(wantB) {
		t.Fatalf("role-b pins = %v, want %v", gotB, wantB)
	}
}

func TestCreateWorkspace_RejectsTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	r := chi.NewRouter()
	r.With(RequireHumanActor).Post("/api/workspaces", testHandler.CreateWorkspace)

	const slug = "e6test-machine-actor"
	cleanupWorkspaceBySlug(t, slug)
	req := newRequest("POST", "/api/workspaces", map[string]any{"name": "Machine", "slug": slug})
	req.Header.Set("X-Actor-Source", "task_token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("machine-credential CreateWorkspace: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestHelperProvisioning_UsesWorkspaceTemplateDefaults(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-template-defaults")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Template Defaults Runtime",
		provider: "runtime-j",
		status:   "online",
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_helper_default (workspace_id, template_key, helper_name, helper_extra_instructions)
		VALUES ($1, 'hr', '{"en": "HR Assistant"}', '{"en": "## Workspace role\n\nHelp with HR work."}')
	`, wsID); err != nil {
		t.Fatalf("insert helper defaults: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = 'en' WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("set user language: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID)
	})

	listAgentsRequest(t, testUserID, wsID)

	helper := helperRowFor(t, wsID, testUserID)
	if helper.Name != "HR Assistant" {
		t.Fatalf("helper name = %q, want the template default %q", helper.Name, "HR Assistant")
	}
	wantInstructions := helperInstructionsByLang["en"] + "\n\n## Workspace role\n\nHelp with HR work."
	if helper.Instructions != wantInstructions {
		t.Fatalf("helper instructions did not append the template extra:\n--- got ---\n%s", helper.Instructions)
	}
}

func TestHelperProvisioning_TemplateDefaultsFallBackAcrossLanguages(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID := helperTestWorkspace(t, "helper-tests-template-fallback")
	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name:     "Template Fallback Runtime",
		provider: "runtime-j",
		status:   "online",
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_helper_default (workspace_id, template_key, helper_name, helper_extra_instructions)
		VALUES ($1, 'hr', '{}', '{"ru": "## Роль пространства\n\nПомогай с HR-задачами."}')
	`, wsID); err != nil {
		t.Fatalf("insert helper defaults: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE "user" SET language = 'en' WHERE id = $1`, testUserID); err != nil {
		t.Fatalf("set user language: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE "user" SET language = NULL WHERE id = $1`, testUserID)
	})

	listAgentsRequest(t, testUserID, wsID)

	helper := helperRowFor(t, wsID, testUserID)
	if helper.Name != helperAgentName {
		t.Fatalf("empty name map must keep the built-in base name; got %q", helper.Name)
	}
	wantInstructions := helperInstructionsByLang["en"] + "\n\n## Роль пространства\n\nПомогай с HR-задачами."
	if helper.Instructions != wantInstructions {
		t.Fatalf("ru-only extra instructions were lost for an en member:\n--- got tail ---\n%s", helper.Instructions[len(helper.Instructions)-min(120, len(helper.Instructions)):])
	}
}
