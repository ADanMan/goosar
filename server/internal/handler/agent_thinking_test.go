package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAgent_ThinkingLevel_ValidationConsistency(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'thinking-test-%'`,
			testWorkspaceID,
		)
	})

	t.Run("empty value succeeds", func(t *testing.T) {
		body := map[string]any{
			"name":                 "thinking-test-empty",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("empty thinking_level: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("known claude value succeeds", func(t *testing.T) {
		body := map[string]any{
			"name":                 "thinking-test-known",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "high",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("thinking_level=high: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("expected thinking_level=high in response, got %v", resp["thinking_level"])
		}
	})

	t.Run("codex-only token rejected for claude runtime", func(t *testing.T) {

		body := map[string]any{
			"name":                 "thinking-test-codex-only",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "none",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("codex-only thinking_level on claude runtime: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("garbage value rejected", func(t *testing.T) {
		body := map[string]any{
			"name":                 "thinking-test-garbage",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "supersonic",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("garbage thinking_level: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestAgentServiceTierValidationAndTriState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	codexRuntimeID := createCodexProviderRuntime(t)
	claudeRuntimeID := createClaudeProviderRuntime(t)

	t.Run("create persists Codex catalog id", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-create",
			"runtime_id":           codexRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"model":                "gpt-5.6-sol",
			"service_tier":         "priority",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create service_tier: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["service_tier"] != "priority" {
			t.Fatalf("service_tier response = %v, want priority", resp["service_tier"])
		}
		agentID, _ := resp["id"].(string)
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		})

		clear := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
			"service_tier": "",
		}), "id", agentID)
		testHandler.UpdateAgent(clear, req)
		if clear.Code != http.StatusOK {
			t.Fatalf("clear service_tier: expected 200, got %d: %s", clear.Code, clear.Body.String())
		}
		var cleared map[string]any
		_ = json.NewDecoder(clear.Body).Decode(&cleared)
		if cleared["service_tier"] != "" {
			t.Fatalf("cleared service_tier = %v, want empty", cleared["service_tier"])
		}
	})

	t.Run("non-Codex runtime rejects tier", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-rejected",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"service_tier":         "priority",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Claude service_tier: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestUpdateAgent_ThinkingLevel_TriState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	agentID := createAgentOnRuntime(t, "thinking-update-test", claudeRuntimeID, "high")

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	t.Run("omitted field leaves value alone", func(t *testing.T) {
		body := map[string]any{
			"name": "thinking-update-test-renamed",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("name-only update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("name-only update silently changed thinking_level: got %v, want high", resp["thinking_level"])
		}
	})

	t.Run("empty string clears", func(t *testing.T) {
		body := map[string]any{
			"thinking_level": "",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("clear update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "" {
			t.Errorf("empty thinking_level should clear: got %v", resp["thinking_level"])
		}
	})

	t.Run("garbage value is always 400", func(t *testing.T) {
		body := map[string]any{
			"thinking_level": "warp-speed",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("garbage thinking_level: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("codex token on claude runtime is 400, not silent clear", func(t *testing.T) {
		body := map[string]any{
			"thinking_level": "minimal",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("codex token on claude runtime: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestUpdateAgent_RuntimeSwitch_PreservesValidValueRejectsInvalid(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	codexRuntimeID := createCodexProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'runtime-switch-%'`, testWorkspaceID)
	})

	t.Run("existing value still valid for new runtime is kept", func(t *testing.T) {

		agentID := createAgentOnRuntime(t, "runtime-switch-keep", codexRuntimeID, "high")
		body := map[string]any{
			"runtime_id": claudeRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 when existing value is still valid, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("expected thinking_level=high preserved across runtime switch, got %v", resp["thinking_level"])
		}
	})

	t.Run("existing value invalid for new runtime is 400, not silent", func(t *testing.T) {

		agentID := createAgentOnRuntime(t, "runtime-switch-reject", codexRuntimeID, "ultra")
		body := map[string]any{
			"runtime_id": claudeRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when existing value is invalid for new runtime, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("simultaneous explicit clear lets the switch through", func(t *testing.T) {

		agentID := createAgentOnRuntime(t, "runtime-switch-clear", codexRuntimeID, "ultra")
		body := map[string]any{
			"runtime_id":     claudeRuntimeID,
			"thinking_level": "",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with simultaneous clear, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "" {
			t.Errorf("expected thinking_level cleared, got %v", resp["thinking_level"])
		}
	})

	t.Run("simultaneous explicit set to valid value lets the switch through", func(t *testing.T) {

		agentID := createAgentOnRuntime(t, "runtime-switch-replace", codexRuntimeID, "ultra")
		body := map[string]any{
			"runtime_id":     claudeRuntimeID,
			"thinking_level": "high",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with simultaneous set, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("expected thinking_level=high, got %v", resp["thinking_level"])
		}
	})
}

func TestUpdateAgent_RuntimeSwitch_ClearsKnownIncompatibleModel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	codexRuntimeID := createCodexProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'runtime-model-switch-%'`, testWorkspaceID)
	})

	t.Run("runtime-only switch clears known foreign model", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-clear", claudeRuntimeID, "claude-sonnet-4-6")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 switching runtime with incompatible model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "" {
			t.Errorf("expected model cleared across Claude->Codex runtime switch, got %v", resp["model"])
		}
	})

	t.Run("runtime-only switch clears provider-prefixed model not accepted by target", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-prefixed", claudeRuntimeID, "openai/gpt-4o")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 switching runtime with provider-prefixed model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "" {
			t.Errorf("expected provider-prefixed model cleared across runtime switch, got %v", resp["model"])
		}
	})

	t.Run("runtime-only switch keeps exact target accepted model", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-accepted", claudeRuntimeID, "gpt-5.5")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 preserving exact target accepted model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "gpt-5.5" {
			t.Errorf("expected exact target model preserved, got %v", resp["model"])
		}
	})

	t.Run("explicit replacement model wins during switch", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-replace", claudeRuntimeID, "claude-sonnet-4-6")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
			"model":      "gpt-5.5",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with explicit replacement model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "gpt-5.5" {
			t.Errorf("expected explicit model to be persisted, got %v", resp["model"])
		}
	})

	t.Run("unknown custom model is preserved", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-custom", claudeRuntimeID, "private-lab-model")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 preserving unknown custom model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "private-lab-model" {
			t.Errorf("expected unknown custom model preserved, got %v", resp["model"])
		}
	})
}

func createCodexProviderRuntime(t *testing.T) string {
	t.Helper()
	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'runtime-e', 'online', $3, '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, "Codex Thinking Runtime", "Codex thinking-level test runtime", testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create codex runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createClaudeProviderRuntime(t *testing.T) string {
	t.Helper()
	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'runtime-c', 'online', $3, '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, "Claude Thinking Runtime", "Claude thinking-level test runtime", testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create claude runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createAgentOnRuntime(t *testing.T, name, runtimeID, level string) string {
	t.Helper()
	var agentID string
	var levelArg any
	if level == "" {
		levelArg = nil
	} else {
		levelArg = level
	}
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, thinking_level
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, levelArg).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent on runtime %s: %v", runtimeID, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createAgentOnRuntimeWithModel(t *testing.T, name, runtimeID, model string) string {
	t.Helper()
	var agentID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, model
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, model).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent on runtime %s with model %s: %v", runtimeID, model, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}
