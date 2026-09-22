package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func newTestMcpBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, secretbox.KeySize)
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func withTestMcpBox(t *testing.T, box *secretbox.Box) {
	t.Helper()
	previous := testHandler.MCPSecretBox
	testHandler.MCPSecretBox = box
	t.Cleanup(func() { testHandler.MCPSecretBox = previous })
}

func TestSealOpenMcpConfig_RoundTrip(t *testing.T) {
	h := &Handler{MCPSecretBox: newTestMcpBox(t)}
	plaintext := []byte(`{"mcpServers":{"jira":{"env":{"JIRA_PERSONAL_TOKEN":"pat-secret"}}}}`)

	sealed, err := h.sealMcpConfig(plaintext)
	if err != nil {
		t.Fatalf("sealMcpConfig: %v", err)
	}
	if !isSealedMcpConfig(sealed) {
		t.Fatalf("sealed value is not recognised as an envelope: %s", sealed)
	}
	if bytes.Contains(sealed, []byte("pat-secret")) {
		t.Fatalf("sealed value leaks plaintext: %s", sealed)
	}
	if !json.Valid(sealed) {
		t.Fatalf("sealed envelope must remain valid JSON for the jsonb column: %s", sealed)
	}

	opened, err := h.openMcpConfig(sealed)
	if err != nil {
		t.Fatalf("openMcpConfig: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Errorf("round trip mismatch:\n  want %s\n  got  %s", plaintext, opened)
	}
}

func TestOpenMcpConfig_LegacyPlaintextPassthrough(t *testing.T) {
	legacy := []byte(`{"mcpServers":{"fetch":{"command":"uvx"}}}`)
	for name, h := range map[string]*Handler{
		"with key":    {MCPSecretBox: newTestMcpBox(t)},
		"without key": {},
	} {
		opened, err := h.openMcpConfig(legacy)
		if err != nil {
			t.Errorf("%s: openMcpConfig(legacy plaintext): %v", name, err)
			continue
		}
		if !bytes.Equal(opened, legacy) {
			t.Errorf("%s: legacy plaintext must pass through unchanged; got %s", name, opened)
		}
	}
}

func TestOpenMcpConfig_NilAndEmpty(t *testing.T) {
	h := &Handler{MCPSecretBox: newTestMcpBox(t)}
	for _, stored := range [][]byte{nil, {}} {
		opened, err := h.openMcpConfig(stored)
		if err != nil {
			t.Fatalf("openMcpConfig(%v): %v", stored, err)
		}
		if opened != nil {
			t.Errorf("openMcpConfig(%v) should return nil, got %s", stored, opened)
		}
	}
}

func TestOpenMcpConfig_SealedWithoutKey(t *testing.T) {
	sealer := &Handler{MCPSecretBox: newTestMcpBox(t)}
	sealed, err := sealer.sealMcpConfig([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("sealMcpConfig: %v", err)
	}

	keyless := &Handler{}
	if _, err := keyless.openMcpConfig(sealed); !errors.Is(err, errMcpKeyUnset) {
		t.Errorf("expected errMcpKeyUnset, got %v", err)
	}
}

func TestOpenMcpConfig_MalformedSealedData(t *testing.T) {
	h := &Handler{MCPSecretBox: newTestMcpBox(t)}

	t.Run("invalid base64", func(t *testing.T) {
		envelope := []byte(`{"__goosar_sealed__":"%%% not base64 %%%"}`)
		if _, err := h.openMcpConfig(envelope); err == nil {
			t.Error("expected error for invalid base64 envelope")
		}
	})

	t.Run("truncated ciphertext", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})
		envelope, _ := json.Marshal(mcpSealedEnvelope{Sealed: short})
		if _, err := h.openMcpConfig(envelope); err == nil {
			t.Error("expected error for truncated ciphertext")
		}
	})

	t.Run("tampered ciphertext", func(t *testing.T) {
		sealed, err := h.sealMcpConfig([]byte(`{"a":1}`))
		if err != nil {
			t.Fatalf("sealMcpConfig: %v", err)
		}
		var env mcpSealedEnvelope
		if err := json.Unmarshal(sealed, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(env.Sealed)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		raw[len(raw)-1] ^= 0xFF
		tampered, _ := json.Marshal(mcpSealedEnvelope{Sealed: base64.StdEncoding.EncodeToString(raw)})
		if _, err := h.openMcpConfig(tampered); err == nil {
			t.Error("expected authentication error for tampered ciphertext")
		}
	})
}

func TestIsSealedMcpConfig_Detection(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"envelope", `{"__goosar_sealed__":"YWJj"}`, true},
		{"envelope with extra key", `{"__goosar_sealed__":"YWJj","other":1}`, false},
		{"marker not a string", `{"__goosar_sealed__":{"nested":true}}`, false},
		{"plain config", `{"mcpServers":{}}`, false},
		{"array", `[1,2,3]`, false},
		{"invalid json", `{not json`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		if got := isSealedMcpConfig([]byte(tc.raw)); got != tc.want {
			t.Errorf("isSealedMcpConfig(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPrepareMcpConfigForStore(t *testing.T) {
	t.Run("rejects invalid JSON", func(t *testing.T) {
		h := &Handler{MCPSecretBox: newTestMcpBox(t)}
		if _, err := h.prepareMcpConfigForStore([]byte(`{not json`)); err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("rejects envelope-shaped input when key unset", func(t *testing.T) {
		h := &Handler{}
		if _, err := h.prepareMcpConfigForStore([]byte(`{"__goosar_sealed__":"x"}`)); err == nil {
			t.Error("expected error for reserved envelope shape without a key")
		}
	})

	t.Run("plaintext passthrough when key unset", func(t *testing.T) {
		h := &Handler{}
		in := []byte(`{"mcpServers":{}}`)
		out, err := h.prepareMcpConfigForStore(in)
		if err != nil {
			t.Fatalf("prepareMcpConfigForStore: %v", err)
		}
		if !bytes.Equal(out, in) {
			t.Errorf("expected plaintext passthrough without a key; got %s", out)
		}
	})

	t.Run("envelope-shaped input round-trips when key set", func(t *testing.T) {

		h := &Handler{MCPSecretBox: newTestMcpBox(t)}
		in := []byte(`{"__goosar_sealed__":"user-owned-value"}`)
		stored, err := h.prepareMcpConfigForStore(in)
		if err != nil {
			t.Fatalf("prepareMcpConfigForStore: %v", err)
		}
		opened, err := h.openMcpConfig(stored)
		if err != nil {
			t.Fatalf("openMcpConfig: %v", err)
		}
		if !bytes.Equal(opened, in) {
			t.Errorf("double-seal round trip mismatch: got %s", opened)
		}
	})

	t.Run("empty passthrough", func(t *testing.T) {
		h := &Handler{MCPSecretBox: newTestMcpBox(t)}
		out, err := h.prepareMcpConfigForStore(nil)
		if err != nil || out != nil {
			t.Errorf("expected nil/nil for empty input, got %s / %v", out, err)
		}
	})
}

func mcpConfigColumnText(t *testing.T, agentID string) []byte {
	t.Helper()
	var stored []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT mcp_config FROM agent WHERE id = $1`, agentID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored mcp_config: %v", err)
	}
	return stored
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return reflect.DeepEqual(av, bv)
}

func TestUpdateAgent_SealsMcpConfigAtRest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))

	agentID := createHandlerTestAgent(t, "mcp-seal-roundtrip", nil)
	plaintext := `{"mcpServers":{"jira":{"env":{"JIRA_PERSONAL_TOKEN":"pat-at-rest"}}}}`

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"mcp_config": json.RawMessage(plaintext),
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	stored := mcpConfigColumnText(t, agentID)
	if !isSealedMcpConfig(stored) {
		t.Fatalf("stored mcp_config is not sealed: %s", stored)
	}
	if bytes.Contains(stored, []byte("pat-at-rest")) {
		t.Fatalf("stored mcp_config leaks the plaintext secret: %s", stored)
	}

	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.McpConfigRedacted {
		t.Fatal("owner response must not be redacted")
	}
	if !jsonEqual(t, resp.McpConfig, []byte(plaintext)) {
		t.Errorf("response mcp_config mismatch: got %s", resp.McpConfig)
	}

	req2 := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "no mcp change",
	})
	req2 = withURLParam(req2, "id", agentID)
	w2 := httptest.NewRecorder()
	testHandler.UpdateAgent(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("UpdateAgent (2nd): expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 AgentResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode 2nd response: %v", err)
	}
	if !jsonEqual(t, resp2.McpConfig, []byte(plaintext)) {
		t.Errorf("sealed row did not open on read: got %s", resp2.McpConfig)
	}
}

func TestUpdateAgent_LegacyPlaintextRowStillReads(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	legacy := `{"mcpServers": {"fetch": {"command": "uvx"}}}`
	agentID := createHandlerTestAgent(t, "mcp-legacy-read", []byte(legacy))

	withTestMcpBox(t, newTestMcpBox(t))

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "read legacy row",
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.McpConfigRedacted {
		t.Fatal("legacy plaintext row must not read as redacted")
	}
	if !jsonEqual(t, resp.McpConfig, []byte(legacy)) {
		t.Errorf("legacy plaintext row mismatch: got %s", resp.McpConfig)
	}

	if !jsonEqual(t, mcpConfigColumnText(t, agentID), []byte(legacy)) {
		t.Error("description-only update rewrote the stored mcp_config")
	}
}

func TestUpdateAgent_SealedRowWithoutKeyIsRedacted(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sealer := &Handler{MCPSecretBox: newTestMcpBox(t)}
	sealed, err := sealer.sealMcpConfig([]byte(`{"mcpServers":{"jira":{}}}`))
	if err != nil {
		t.Fatalf("sealMcpConfig: %v", err)
	}
	agentID := createHandlerTestAgent(t, "mcp-sealed-keyless", sealed)
	withTestMcpBox(t, nil)

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "read sealed row without key",
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.McpConfig) > 0 && !bytes.Equal(bytes.TrimSpace(resp.McpConfig), []byte("null")) {
		t.Errorf("unreadable sealed row leaked content: %s", resp.McpConfig)
	}
	if !resp.McpConfigRedacted {
		t.Error("unreadable sealed row should surface mcp_config_redacted=true")
	}
}

func TestBackfillSealedMcpConfigs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, newTestMcpBox(t))
	ctx := context.Background()

	legacy := `{"mcpServers": {"gw": {"headers": {"Authorization": "Bearer backfill-secret"}}}}`
	legacyID := createHandlerTestAgent(t, "mcp-backfill-legacy", []byte(legacy))
	nilID := createHandlerTestAgent(t, "mcp-backfill-nil", nil)

	alreadySealed, err := testHandler.sealMcpConfig([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("sealMcpConfig: %v", err)
	}
	sealedID := createHandlerTestAgent(t, "mcp-backfill-sealed", alreadySealed)

	if _, err := testHandler.BackfillSealedMcpConfigs(ctx); err != nil {
		t.Fatalf("BackfillSealedMcpConfigs: %v", err)
	}

	stored := mcpConfigColumnText(t, legacyID)
	if !isSealedMcpConfig(stored) {
		t.Fatalf("backfill did not seal legacy row: %s", stored)
	}
	if bytes.Contains(stored, []byte("backfill-secret")) {
		t.Fatalf("backfilled row leaks plaintext: %s", stored)
	}
	opened, err := testHandler.openMcpConfig(stored)
	if err != nil {
		t.Fatalf("openMcpConfig(backfilled): %v", err)
	}
	if !jsonEqual(t, opened, []byte(legacy)) {
		t.Errorf("backfilled row does not open to the original config: got %s", opened)
	}

	if stored := mcpConfigColumnText(t, nilID); stored != nil {
		t.Errorf("backfill touched a NULL mcp_config row: %s", stored)
	}
	if got := mcpConfigColumnText(t, sealedID); !jsonEqual(t, got, alreadySealed) {
		t.Errorf("backfill rewrote an already sealed row: %s", got)
	}

	n, err := testHandler.BackfillSealedMcpConfigs(ctx)
	if err != nil {
		t.Fatalf("BackfillSealedMcpConfigs (2nd): %v", err)
	}
	if n != 0 {
		t.Errorf("second backfill run sealed %d rows, want 0", n)
	}
}

func TestCreateAgent_RejectsReservedEnvelopeShapeWithoutKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTestMcpBox(t, nil)

	req := newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "mcp-reserved-shape",
		"runtime_id": handlerTestRuntimeID(t),
		"mcp_config": json.RawMessage(`{"__goosar_sealed__":"looks-like-ciphertext"}`),
	})
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAgent: expected 400 for reserved envelope shape, got %d: %s", w.Code, w.Body.String())
	}
}

func subscribeAgentEventPayloads(t *testing.T, eventType string) func(agentID string) []AgentResponse {
	t.Helper()
	var mu sync.Mutex
	captured := map[string][]AgentResponse{}
	testHandler.Bus.Subscribe(eventType, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agent, ok := payload["agent"].(AgentResponse)
		if !ok {
			return
		}
		mu.Lock()
		captured[agent.ID] = append(captured[agent.ID], agent)
		mu.Unlock()
	})
	return func(agentID string) []AgentResponse {
		mu.Lock()
		defer mu.Unlock()
		return append([]AgentResponse(nil), captured[agentID]...)
	}
}

func assertBroadcastRedacted(t *testing.T, label string, payloads []AgentResponse, hadConfig bool) {
	t.Helper()
	if len(payloads) == 0 {
		t.Fatalf("%s: expected at least one broadcast payload", label)
	}
	for _, agent := range payloads {
		if len(agent.McpConfig) > 0 && !bytes.Equal(bytes.TrimSpace(agent.McpConfig), []byte("null")) {
			t.Errorf("%s: broadcast leaked mcp_config: %s", label, agent.McpConfig)
		}
		if hadConfig && !agent.McpConfigRedacted {
			t.Errorf("%s: broadcast should mark mcp_config_redacted=true", label)
		}
		if agent.ComposioToolkitAllowlist != nil {
			t.Errorf("%s: broadcast leaked composio_toolkit_allowlist", label)
		}
	}
}

func TestArchiveAgentsAndDeleteRuntime_BroadcastRedactsMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := seedIsolatedRuntime(t, "Runtime Cascade Broadcast Redaction")
	agentID := seedAgentOnRuntime(t, runtimeID, "Cascade Broadcast Redaction Agent", false)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET mcp_config = '{"server":"cascade-secret"}'::jsonb WHERE id = $1`, agentID,
	); err != nil {
		t.Fatalf("set mcp_config: %v", err)
	}

	payloadsFor := subscribeAgentEventPayloads(t, protocol.EventAgentArchived)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/archive-agents-and-delete", map[string]any{
		"expected_active_agent_ids": []string{agentID},
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ArchiveAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveAgentsAndDeleteRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	assertBroadcastRedacted(t, "agent:archived (cascade delete)", payloadsFor(agentID), true)
}

func TestPublishRevocation_BroadcastRedactsMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentUUID := parseUUID("7f9c0d5e-1b7a-4f8e-9c2d-3a4b5c6d7e8f")
	archived := db.Agent{
		ID:        agentUUID,
		Name:      "Revocation Broadcast Redaction Agent",
		McpConfig: []byte(`{"server":"revoke-secret"}`),
	}

	payloadsFor := subscribeAgentEventPayloads(t, protocol.EventAgentArchived)

	testHandler.publishRevocation(context.Background(), revocationResult{
		Runtimes:       []db.AgentRuntime{{}},
		ArchivedAgents: []db.Agent{archived},
	}, testWorkspaceID, "member", testUserID)

	assertBroadcastRedacted(t, "agent:archived (revocation)", payloadsFor(uuidToString(agentUUID)), true)
}

func TestBootstrapOnboardingRuntime_BroadcastRedactsMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	cleanupShim := func() {
		testPool.Exec(ctx, `
			DELETE FROM agent_task_queue
			 WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1 AND name = $2)
		`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, onboardingIssueTitle)
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, onboardingAssistantName)
		testPool.Exec(ctx, `UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL WHERE id = $1`, testUserID)
	}
	cleanupShim()
	t.Cleanup(cleanupShim)

	payloadsFor := subscribeAgentEventPayloads(t, protocol.EventAgentCreated)

	w := httptest.NewRecorder()
	testHandler.BootstrapOnboardingRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/runtime-bootstrap", map[string]string{
		"workspace_id": testWorkspaceID,
		"runtime_id":   testRuntimeID,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("BootstrapOnboardingRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp bootstrapOnboardingRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	assertBroadcastRedacted(t, "agent:created (onboarding shim)", payloadsFor(resp.AgentID), false)
}

func TestDeleteAgentRuntime_409BodyGatesMcpConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	requestConflict := func(runtimeID string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest(http.MethodDelete, "/api/runtimes/"+runtimeID, nil)
		req = withURLParam(req, "runtimeId", runtimeID)
		testHandler.DeleteAgentRuntime(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("DeleteAgentRuntime: expected 409, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode 409 body: %v", err)
		}
		return body
	}
	agentConfigs := func(body map[string]any) []any {
		t.Helper()
		agents, ok := body["active_agents"].([]any)
		if !ok || len(agents) == 0 {
			t.Fatalf("409 body has no active_agents: %v", body)
		}
		configs := make([]any, len(agents))
		for i, raw := range agents {
			agent, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("active_agents[%d] is not an object", i)
			}
			configs[i] = agent["mcp_config"]
		}
		return configs
	}
	seed := func(suffix string) string {
		runtimeID := seedIsolatedRuntime(t, "Runtime 409 Redaction "+suffix)
		agentID := seedAgentOnRuntime(t, runtimeID, "409 Redaction Agent "+suffix, false)
		if _, err := testPool.Exec(ctx,
			`UPDATE agent SET mcp_config = '{"server":"conflict-secret"}'::jsonb WHERE id = $1`, agentID,
		); err != nil {
			t.Fatalf("set mcp_config: %v", err)
		}
		return runtimeID
	}

	for _, cfg := range agentConfigs(requestConflict(seed("Owner"))) {
		if cfg == nil {
			t.Errorf("owner should see mcp_config in the 409 body")
		}
	}

	var previousSettings []byte
	if err := testPool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previousSettings); err != nil {
		t.Fatalf("load workspace settings: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"always_redact_env": true}'::jsonb WHERE id = $1`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("set always_redact_env: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previousSettings, testWorkspaceID)
	})

	for _, cfg := range agentConfigs(requestConflict(seed("Redacted"))) {
		if cfg != nil {
			t.Errorf("always_redact_env workspace leaked mcp_config in the 409 body: %v", cfg)
		}
	}
}
