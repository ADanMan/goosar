package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func seedEffectiveConfigWorkspaceLayer(t *testing.T, baseURL, model, apiKey string) {
	t.Helper()

	sealed, err := testHandler.sealConfigSecret(apiKey)
	if err != nil {
		t.Fatalf("seal workspace llm api key: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_config (workspace_id, llm_base_url, llm_model, llm_api_key, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id) DO UPDATE SET
			llm_base_url = EXCLUDED.llm_base_url,
			llm_model = EXCLUDED.llm_model,
			llm_api_key = EXCLUDED.llm_api_key,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
	`, testWorkspaceID, baseURL, model, sealed, testUserID); err != nil {
		t.Fatalf("seed workspace config: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func TestGetEffectiveConfigView_MasksSecrets(t *testing.T) {
	withTestMcpBox(t, newTestMcpBox(t))
	seedEffectiveConfigWorkspaceLayer(t, "https://gw.corp.example/v1", "openai/view-model", "sk-must-not-leak")

	mcpDoc, err := testHandler.sealConfigDocument([]byte(`{"outlook":{"enabled":true,"env":{"EWS_PASS":"pw-must-not-leak"}}}`))
	if err != nil {
		t.Fatalf("seal mcp doc: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE workspace_config SET mcp_defaults = $2 WHERE workspace_id = $1
	`, testWorkspaceID, mcpDoc); err != nil {
		t.Fatalf("seed mcp defaults: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAsUser(testUserID, http.MethodGet, "/api/effective-config", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.GetEffectiveConfigView(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "sk-must-not-leak") || strings.Contains(body, "pw-must-not-leak") {
		t.Fatalf("masked view leaked a secret: %s", body)
	}

	var view struct {
		SchemaVersion int `json:"schema_version"`
		LLM           *struct {
			BaseURL   string `json:"base_url"`
			Model     string `json:"model"`
			HasAPIKey bool   `json:"has_api_key"`
			Origin    string `json:"origin"`
			Locked    bool   `json:"locked"`
		} `json:"llm"`
		MCP map[string]struct {
			Enabled bool   `json:"enabled"`
			HasEnv  bool   `json:"has_env"`
			Origin  string `json:"origin"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.LLM == nil {
		t.Fatalf("view has no llm block: %s", body)
	}
	if !view.LLM.HasAPIKey {
		t.Fatalf("view must report has_api_key=true")
	}
	if view.LLM.Origin != ConfigOriginWorkspace {
		t.Fatalf("view origin = %q, want %q", view.LLM.Origin, ConfigOriginWorkspace)
	}
	if view.LLM.Model != "openai/view-model" {
		t.Fatalf("view model = %q", view.LLM.Model)
	}
	entry, ok := view.MCP["outlook"]
	if !ok {
		t.Fatalf("view mcp is missing the outlook entry: %s", body)
	}
	if !entry.Enabled || entry.Origin != ConfigOriginWorkspace {
		t.Fatalf("view mcp entry = %+v", entry)
	}

	if strings.Contains(body, "pw-must-not-leak") || strings.Contains(body, "\"env\"") {
		t.Fatalf("masked view must drop the env block and every value: %s", body)
	}
	if !entry.HasEnv {
		t.Fatalf("view mcp entry must report has_env=true when the layer carries env: %s", body)
	}
}

func TestGetEffectiveConfigView_HasEnvFalseWhenLayerCarriesNoEnv(t *testing.T) {
	withTestMcpBox(t, newTestMcpBox(t))

	seedEffectiveConfigWorkspaceLayer(t, "https://gw.corp.example/v1", "openai/view-model", "sk-unused")

	mcpDoc, err := testHandler.sealConfigDocument([]byte(`{"ews-mcp":{"enabled":true}}`))
	if err != nil {
		t.Fatalf("seal mcp doc: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE workspace_config SET mcp_defaults = $2 WHERE workspace_id = $1
	`, testWorkspaceID, mcpDoc); err != nil {
		t.Fatalf("seed mcp defaults: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAsUser(testUserID, http.MethodGet, "/api/effective-config", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.GetEffectiveConfigView(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var view struct {
		MCP map[string]struct {
			Enabled bool `json:"enabled"`
			HasEnv  bool `json:"has_env"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	entry, ok := view.MCP["ews-mcp"]
	if !ok {
		t.Fatalf("view mcp is missing the ews-mcp entry: %s", w.Body.String())
	}
	if !entry.Enabled {
		t.Fatalf("entry must be enabled: %+v", entry)
	}
	if entry.HasEnv {
		t.Fatalf("has_env must be false when the layer supplies no env: %+v", entry)
	}
}

func TestGetEffectiveConfigView_EmptyLayersIsEmptyView(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequestAsUser(testUserID, http.MethodGet, "/api/effective-config", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.GetEffectiveConfigView(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var view struct {
		SchemaVersion int             `json:"schema_version"`
		LLM           json.RawMessage `json:"llm"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.SchemaVersion != EffectiveConfigSchemaVersion {
		t.Fatalf("schema_version = %d", view.SchemaVersion)
	}
	if len(view.LLM) != 0 {
		t.Fatalf("empty layers must produce no llm block, got %s", view.LLM)
	}
}

func TestGetEffectiveConfigView_NamesTheEnvSlotsTheLayerSupplies(t *testing.T) {
	withTestMcpBox(t, newTestMcpBox(t))
	seedEffectiveConfigWorkspaceLayer(t, "https://gw.corp.example/v1", "openai/view-model", "sk-unused")

	mcpDoc, err := testHandler.sealConfigDocument([]byte(
		`{"ews-mcp":{"enabled":true,"env":{"EWS_SERVER_URL":"https://mail.corp/EWS/Exchange.asmx","EWS_TZ":"Europe/Moscow","EWS_PASS":"pw-must-not-leak"}}}`,
	))
	if err != nil {
		t.Fatalf("seal mcp doc: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE workspace_config SET mcp_defaults = $2 WHERE workspace_id = $1
	`, testWorkspaceID, mcpDoc); err != nil {
		t.Fatalf("seed mcp defaults: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAsUser(testUserID, http.MethodGet, "/api/effective-config", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.GetEffectiveConfigView(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "pw-must-not-leak") {
		t.Fatalf("masked view leaked an env VALUE: %s", body)
	}

	var view struct {
		MCP map[string]struct {
			Enabled bool     `json:"enabled"`
			HasEnv  bool     `json:"has_env"`
			EnvKeys []string `json:"env_keys"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	entry, ok := view.MCP["ews-mcp"]
	if !ok {
		t.Fatalf("view mcp is missing the ews-mcp entry: %s", body)
	}
	if !entry.HasEnv {
		t.Fatalf("has_env must stay true: %+v", entry)
	}

	want := []string{"EWS_PASS", "EWS_SERVER_URL", "EWS_TZ"}
	if len(entry.EnvKeys) != len(want) {
		t.Fatalf("env_keys = %v, want %v", entry.EnvKeys, want)
	}
	for i, name := range want {
		if entry.EnvKeys[i] != name {
			t.Fatalf("env_keys = %v, want %v", entry.EnvKeys, want)
		}
	}
}

func TestGetEffectiveConfigView_NamesNothingWhenLayerSuppliesNoEnv(t *testing.T) {
	withTestMcpBox(t, newTestMcpBox(t))
	seedEffectiveConfigWorkspaceLayer(t, "https://gw.corp.example/v1", "openai/view-model", "sk-unused")

	mcpDoc, err := testHandler.sealConfigDocument([]byte(`{"ews-mcp":{"enabled":true}}`))
	if err != nil {
		t.Fatalf("seal mcp doc: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE workspace_config SET mcp_defaults = $2 WHERE workspace_id = $1
	`, testWorkspaceID, mcpDoc); err != nil {
		t.Fatalf("seed mcp defaults: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAsUser(testUserID, http.MethodGet, "/api/effective-config", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.GetEffectiveConfigView(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "env_keys") {
		t.Fatalf("env_keys must be omitted when the layer supplies no env: %s", w.Body.String())
	}
}
