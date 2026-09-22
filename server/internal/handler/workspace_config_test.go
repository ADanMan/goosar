package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

func cleanupWorkspaceConfigRows(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM user_config_override WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM deployment_policy`)
	})
}

func overrideRequest(userID string, method, targetUserID string, body any) *http.Request {
	req := newRequestAs(userID, method, "/api/workspace-config/overrides/"+targetUserID, body)
	return withURLParam(req, "userId", targetUserID)
}

func TestWorkspaceConfigEndpoints_RequireAdminRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	memberID := nonAdminMemberFixture(t)

	calls := []struct {
		name string
		do   func(w *httptest.ResponseRecorder)
	}{
		{"GET workspace config", func(w *httptest.ResponseRecorder) {
			testHandler.GetWorkspaceConfig(w, newRequestAs(memberID, http.MethodGet, "/api/workspace-config", nil))
		}},
		{"PUT workspace config", func(w *httptest.ResponseRecorder) {
			testHandler.PutWorkspaceConfig(w, newRequestAs(memberID, http.MethodPut, "/api/workspace-config", map[string]any{"llm_model": "m"}))
		}},
		{"GET user override", func(w *httptest.ResponseRecorder) {
			testHandler.GetWorkspaceUserConfigOverride(w, overrideRequest(memberID, http.MethodGet, testUserID, nil))
		}},
		{"PUT user override", func(w *httptest.ResponseRecorder) {
			testHandler.PutWorkspaceUserConfigOverride(w, overrideRequest(memberID, http.MethodPut, testUserID, map[string]any{"llm_model": "m"}))
		}},
		{"DELETE user override", func(w *httptest.ResponseRecorder) {
			testHandler.DeleteWorkspaceUserConfigOverride(w, overrideRequest(memberID, http.MethodDelete, testUserID, nil))
		}},
		{"GET deployment policy", func(w *httptest.ResponseRecorder) {
			testHandler.GetDeploymentPolicy(w, newRequestAs(memberID, http.MethodGet, "/api/deployment-policy", nil))
		}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			call.do(w)
			if w.Code != http.StatusForbidden {
				t.Fatalf("plain member: status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestWorkspaceConfig_PutGetRoundTripMasksApiKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))

	const apiKeySecret = "sk-endpoint-canary-91d2"

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	putBody := map[string]any{
		"llm_base_url": "https://gw.corp.example/v3",
		"llm_model":    "coding-medium",
		"llm_api_key":  apiKeySecret,
		"mcp_defaults": map[string]any{
			"outlook": map[string]any{"enabled": true, "env": map[string]string{"EWS_USER": "svc-user"}},
		},
	}
	w := httptest.NewRecorder()
	testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config", putBody))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), apiKeySecret) {
		t.Fatalf("PUT response leaks the api key: %s", w.Body.String())
	}

	var storedKey []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT llm_api_key FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID).Scan(&storedKey); err != nil {
		t.Fatalf("read stored row: %v", err)
	}
	if len(storedKey) == 0 || bytes.Contains(storedKey, []byte(apiKeySecret)) {
		t.Fatalf("stored llm_api_key must be sealed, got %q", storedKey)
	}

	w = httptest.NewRecorder()
	testHandler.GetWorkspaceConfig(w, newRequest(http.MethodGet, "/api/workspace-config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), apiKeySecret) {
		t.Fatalf("GET response leaks the api key: %s", w.Body.String())
	}
	var resp WorkspaceConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if resp.LlmBaseURL != "https://gw.corp.example/v3" || resp.LlmModel != "coding-medium" {
		t.Fatalf("GET returned wrong llm fields: %+v", resp)
	}
	if !resp.HasLlmAPIKey {
		t.Fatal("GET must report has_llm_api_key=true after the key was stored")
	}

	if strings.Contains(string(resp.McpDefaults), "svc-user") {
		t.Fatalf("GET must mask mcp_defaults env values: %s", resp.McpDefaults)
	}
	if !strings.Contains(string(resp.McpDefaults), `"EWS_USER":true`) {
		t.Fatalf("GET must report env names with has_value markers: %s", resp.McpDefaults)
	}

	if strings.Contains(logBuf.String(), apiKeySecret) {
		t.Fatalf("api key leaked into logs:\n%s", logBuf.String())
	}
}

func TestWorkspaceConfig_McpEnvMaskedAndMerged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))

	const envSecret = "svc-password-canary-77aa"

	put := func(body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config", body))
		return w
	}
	storedDoc := func() string {
		t.Helper()
		var sealed []byte
		if err := testPool.QueryRow(context.Background(),
			`SELECT mcp_defaults FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID).Scan(&sealed); err != nil {
			t.Fatalf("read stored mcp_defaults: %v", err)
		}
		doc, err := testHandler.openConfigDocument(sealed)
		if err != nil {
			t.Fatalf("open stored mcp_defaults: %v", err)
		}
		return string(doc)
	}

	if w := put(map[string]any{"mcp_defaults": map[string]any{
		"outlook": map[string]any{"enabled": true, "env": map[string]any{"EWS_USER": "svc-user", "EWS_PASS": envSecret}},
	}}); w.Code != http.StatusOK {
		t.Fatalf("seed PUT: %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	testHandler.GetWorkspaceConfig(w, newRequest(http.MethodGet, "/api/workspace-config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), envSecret) || strings.Contains(w.Body.String(), "svc-user") {
		t.Fatalf("GET leaks stored env values: %s", w.Body.String())
	}
	var resp WorkspaceConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	var masked map[string]struct {
		Enabled *bool           `json:"enabled"`
		Env     map[string]bool `json:"env"`
	}
	if err := json.Unmarshal(resp.McpDefaults, &masked); err != nil {
		t.Fatalf("parse masked mcp_defaults %s: %v", resp.McpDefaults, err)
	}
	if !masked["outlook"].Env["EWS_USER"] || !masked["outlook"].Env["EWS_PASS"] {
		t.Fatalf("masked doc must report has_value=true per env name: %s", resp.McpDefaults)
	}

	if w := put(map[string]any{"mcp_defaults": map[string]any{
		"outlook": map[string]any{"env": map[string]any{"EWS_PASS": nil, "EWS_HOST": "ews.corp"}},
	}}); w.Code != http.StatusOK {
		t.Fatalf("merge PUT: %d: %s", w.Code, w.Body.String())
	}
	doc := storedDoc()
	if strings.Contains(doc, envSecret) {
		t.Fatalf("deleted env value still stored: %s", doc)
	}
	if !strings.Contains(doc, `"svc-user"`) || !strings.Contains(doc, `"ews.corp"`) {
		t.Fatalf("merge lost untouched or new env values: %s", doc)
	}

	if w := put(map[string]any{"mcp_defaults": map[string]any{
		"outlook": map[string]any{"enabled": false},
	}}); w.Code != http.StatusOK {
		t.Fatalf("toggle PUT: %d: %s", w.Code, w.Body.String())
	}
	doc = storedDoc()
	if !strings.Contains(doc, `"enabled":false`) || !strings.Contains(doc, `"svc-user"`) {
		t.Fatalf("enabled toggle disturbed the entry: %s", doc)
	}

	if w := put(map[string]any{"mcp_defaults": map[string]any{
		"jira":    map[string]any{"enabled": true},
		"outlook": nil,
	}}); w.Code != http.StatusOK {
		t.Fatalf("delete-entry PUT: %d: %s", w.Code, w.Body.String())
	}
	doc = storedDoc()
	if strings.Contains(doc, "outlook") || !strings.Contains(doc, "jira") {
		t.Fatalf("entry delete/keep semantics wrong: %s", doc)
	}
}

func TestPutWorkspaceConfig_MergeAndClearSemantics(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))

	put := func(body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config", body))
		return w
	}

	if w := put(map[string]any{"llm_base_url": "https://gw.example/v1", "llm_api_key": "sk-merge"}); w.Code != http.StatusOK {
		t.Fatalf("first PUT: %d: %s", w.Code, w.Body.String())
	}

	if w := put(map[string]any{"llm_model": "added-later"}); w.Code != http.StatusOK {
		t.Fatalf("second PUT: %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	testHandler.GetWorkspaceConfig(w, newRequest(http.MethodGet, "/api/workspace-config", nil))
	var resp WorkspaceConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LlmBaseURL != "https://gw.example/v1" || resp.LlmModel != "added-later" || !resp.HasLlmAPIKey {
		t.Fatalf("merge lost fields: %+v", resp)
	}

	if w := put(map[string]any{"llm_api_key": "", "llm_base_url": ""}); w.Code != http.StatusOK {
		t.Fatalf("clearing PUT: %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.GetWorkspaceConfig(w, newRequest(http.MethodGet, "/api/workspace-config", nil))
	resp = WorkspaceConfigResponse{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HasLlmAPIKey || resp.LlmBaseURL != "" || resp.LlmModel != "added-later" {
		t.Fatalf("clear semantics wrong: %+v", resp)
	}
}

func TestPutWorkspaceConfig_FailClosedWithoutSecretKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, nil)

	secretBodies := []map[string]any{
		{"llm_api_key": "sk-must-not-store"},
		{"mcp_defaults": map[string]any{"jira": map[string]any{"enabled": true, "env": map[string]string{"TOKEN": "t"}}}},
	}
	for _, body := range secretBodies {
		w := httptest.NewRecorder()
		testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config", body))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("secret write without key: status = %d, want 503: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "GOOSAR_MCP_SECRET_KEY") {
			t.Fatalf("error must name the missing key env var: %s", w.Body.String())
		}
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("fail-closed write still stored a row (count=%d)", count)
	}

	w := httptest.NewRecorder()
	testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config", map[string]any{
		"llm_base_url": "https://gw.example/v1",
		"llm_model":    "m",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("non-secret write without key: status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestPutUserConfigOverride_FailClosedWithoutSecretKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, nil)

	w := httptest.NewRecorder()
	testHandler.PutWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodPut, testUserID, map[string]any{
		"llm_api_key": "sk-personal-must-not-store",
	}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("override secret write without key: status = %d, want 503: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_config_override WHERE workspace_id = $1`, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("fail-closed override write still stored a row (count=%d)", count)
	}
}

func TestPutWorkspaceConfig_Validation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))

	badBodies := []struct {
		name string
		body map[string]any
	}{
		{"base url without scheme", map[string]any{"llm_base_url": "gw.example/v1"}},
		{"base url with bad scheme", map[string]any{"llm_base_url": "ftp://gw.example"}},
		{"mcp defaults not an object", map[string]any{"mcp_defaults": []any{"x"}}},
		{"mcp entry with unknown field", map[string]any{"mcp_defaults": map[string]any{"jira": map[string]any{"enabled": true, "locked": true}}}},
		{"mcp entry with empty name", map[string]any{"mcp_defaults": map[string]any{"": map[string]any{"enabled": true}}}},
	}
	for _, tc := range badBodies {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.PutWorkspaceConfig(w, newRequest(http.MethodPut, "/api/workspace-config", tc.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}

	req := httptest.NewRequest(http.MethodPut, "/api/workspace-config", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.PutWorkspaceConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceUserConfigOverride_Lifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))
	targetID := nonAdminMemberFixture(t)

	w := httptest.NewRecorder()
	testHandler.GetWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodGet, targetID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET before PUT: status = %d, want 404: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.PutWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodPut, targetID, map[string]any{
		"llm_api_key": "sk-personal",
		"llm_model":   "personal-model",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT override: status = %d, want 200: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.GetWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodGet, targetID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET override: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sk-personal") {
		t.Fatalf("override GET leaks the api key: %s", w.Body.String())
	}
	var resp UserConfigOverrideResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode override response: %v", err)
	}
	if !resp.HasLlmAPIKey || resp.LlmModel != "personal-model" || resp.UserID != targetID {
		t.Fatalf("override response wrong: %+v", resp)
	}

	w = httptest.NewRecorder()
	testHandler.DeleteWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodDelete, targetID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE override: status = %d, want 204: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.DeleteWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodDelete, targetID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("second DELETE: status = %d, want 204: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.GetWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodGet, targetID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceUserConfigOverride_TargetValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)

	w := httptest.NewRecorder()
	testHandler.PutWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodPut, "not-a-uuid", map[string]any{"llm_model": "m"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid uuid: status = %d, want 400: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.PutWorkspaceUserConfigOverride(w, overrideRequest(testUserID, http.MethodPut, "00000000-0000-4000-8000-000000000001", map[string]any{"llm_model": "m"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-member target: status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestGetDeploymentPolicy_EmptyAndSeeded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)

	w := httptest.NewRecorder()
	testHandler.GetDeploymentPolicy(w, newRequest(http.MethodGet, "/api/deployment-policy", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("empty policy GET: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp DeploymentPolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(resp.Policy) != "{}" {
		t.Fatalf("empty policy = %s, want {}", resp.Policy)
	}

	seedDeploymentPolicy(t, `{"llm":{"base_url":"https://policy.example/v1","locked":true}}`)
	w = httptest.NewRecorder()
	testHandler.GetDeploymentPolicy(w, newRequest(http.MethodGet, "/api/deployment-policy", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("seeded policy GET: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp = DeploymentPolicyResponse{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(string(resp.Policy), "policy.example") {
		t.Fatalf("seeded policy missing content: %s", resp.Policy)
	}
}

func TestWorkspaceConfigRoutes_RejectTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkspaceConfigRows(t)
	withTestMcpBox(t, newTestMcpBox(t))

	r := chi.NewRouter()
	r.Route("/api/workspace-config", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/", testHandler.GetWorkspaceConfig)
		r.Put("/", testHandler.PutWorkspaceConfig)
		r.Route("/overrides/{userId}", func(r chi.Router) {
			r.Get("/", testHandler.GetWorkspaceUserConfigOverride)
			r.Put("/", testHandler.PutWorkspaceUserConfigOverride)
			r.Delete("/", testHandler.DeleteWorkspaceUserConfigOverride)
		})
	})
	r.With(RequireHumanActor).Get("/api/deployment-policy", testHandler.GetDeploymentPolicy)
	r.With(RequireHumanActor).Get("/api/effective-config", testHandler.GetEffectiveConfigView)

	requests := []struct {
		name string
		req  *http.Request
	}{
		{"GET workspace config", newRequest(http.MethodGet, "/api/workspace-config", nil)},
		{"PUT workspace config", newRequest(http.MethodPut, "/api/workspace-config", map[string]any{"llm_api_key": "sk-agent-smuggled"})},
		{"GET user override", newRequest(http.MethodGet, "/api/workspace-config/overrides/"+testUserID, nil)},
		{"PUT user override", newRequest(http.MethodPut, "/api/workspace-config/overrides/"+testUserID, map[string]any{"llm_api_key": "sk-agent-smuggled"})},
		{"DELETE user override", newRequest(http.MethodDelete, "/api/workspace-config/overrides/"+testUserID, nil)},
		{"GET deployment policy", newRequest(http.MethodGet, "/api/deployment-policy", nil)},
		{"GET effective config view", newRequest(http.MethodGet, "/api/effective-config", nil)},
	}
	for _, tc := range requests {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Header.Set("X-Actor-Source", "task_token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, tc.req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("task-token actor: status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_config WHERE workspace_id = $1`, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected task-token PUT still wrote a config row (count=%d)", count)
	}

	req := newRequest(http.MethodGet, "/api/workspace-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("human owner through wired routes: status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestPutWorkspaceConfig_ConcurrentPatchesBothSurvive(t *testing.T) {
	withTestMcpBox(t, newTestMcpBox(t))
	clearRow := func() {
		if _, err := testPool.Exec(context.Background(),
			"DELETE FROM workspace_config WHERE workspace_id = $1", testWorkspaceID); err != nil {
			t.Fatalf("clear workspace_config: %v", err)
		}
	}
	t.Cleanup(clearRow)

	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/workspace-config",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		testHandler.PutWorkspaceConfig(w, req)
		return w
	}

	const rounds = 8
	for i := 0; i < rounds; i++ {
		var wg sync.WaitGroup
		codes := make([]int, 2)
		wg.Add(2)
		go func() { defer wg.Done(); codes[0] = put(`{"llm_base_url":"https://gw.corp.example/v1"}`).Code }()
		go func() { defer wg.Done(); codes[1] = put(`{"llm_model":"openai/race-model"}`).Code }()
		wg.Wait()
		for _, c := range codes {
			if c != http.StatusOK {
				t.Fatalf("round %d: PUT got %d", i, c)
			}
		}

		var baseURL, model string
		if err := testPool.QueryRow(context.Background(),
			"SELECT COALESCE(llm_base_url,''), COALESCE(llm_model,'') FROM workspace_config WHERE workspace_id = $1",
			testWorkspaceID,
		).Scan(&baseURL, &model); err != nil {
			t.Fatalf("round %d: read row: %v", i, err)
		}
		if baseURL != "https://gw.corp.example/v1" || model != "openai/race-model" {
			t.Fatalf("round %d: a concurrent patch was dropped: base=%q model=%q", i, baseURL, model)
		}

		clearRow()
	}
}

func TestPutWorkspaceConfig_SameFieldLastWriteWins(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	cleanupWorkspaceConfigRows(t)

	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/workspace-config",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		testHandler.PutWorkspaceConfig(w, req)
		return w
	}
	readRow := func() string {
		var baseURL string
		if err := testPool.QueryRow(context.Background(),
			"SELECT COALESCE(llm_base_url,'') FROM workspace_config WHERE workspace_id = $1",
			testWorkspaceID,
		).Scan(&baseURL); err != nil {
			t.Fatalf("read row: %v", err)
		}
		return baseURL
	}
	readGet := func() string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/workspace-config", nil)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		testHandler.GetWorkspaceConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET workspace-config: status = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			LlmBaseURL string `json:"llm_base_url"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode GET body: %v", err)
		}
		return resp.LlmBaseURL
	}

	const first = "https://gw-first.corp.example/v1"
	const second = "https://gw-second.corp.example/v1"

	for _, body := range []string{
		`{"llm_base_url":"` + first + `"}`,
		`{"llm_base_url":"` + second + `"}`,
	} {
		if w := put(body); w.Code != http.StatusOK {
			t.Fatalf("sequential PUT got %d: %s", w.Code, w.Body.String())
		}
	}
	if got := readRow(); got != second {
		t.Fatalf("sequential same-field write: row = %q, want the LAST value %q", got, second)
	}
	if got := readGet(); got != second {
		t.Fatalf("sequential same-field write: GET = %q, want the LAST value %q", got, second)
	}

	const rounds = 8
	for i := 0; i < rounds; i++ {
		var wg sync.WaitGroup
		codes := make([]int, 2)
		wg.Add(2)
		go func() { defer wg.Done(); codes[0] = put(`{"llm_base_url":"` + first + `"}`).Code }()
		go func() { defer wg.Done(); codes[1] = put(`{"llm_base_url":"` + second + `"}`).Code }()
		wg.Wait()
		for _, c := range codes {
			if c != http.StatusOK {
				t.Fatalf("round %d: racing PUT got %d", i, c)
			}
		}
		row := readRow()
		if row != first && row != second {
			t.Fatalf("round %d: same-field race left a hybrid: %q", i, row)
		}
		if got := readGet(); got != row {
			t.Fatalf("round %d: GET (%q) disagrees with the row (%q)", i, got, row)
		}
	}
}
