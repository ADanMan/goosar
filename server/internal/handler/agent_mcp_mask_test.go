package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const maskTestMcpConfig = `{"mcpServers":{` +
	`"jira":{"env":{"JIRA_PERSONAL_TOKEN":"member-jira-token"}},` +
	`"confluence":{"env":{"CONFLUENCE_PERSONAL_TOKEN":"member-confluence-token"}}}}`

func decodeMcpDocument(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode mcp_config %s: %v", raw, err)
	}
	return out
}

func maskedServerNames(t *testing.T, doc map[string]any) []string {
	t.Helper()
	names := []string{}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("masked document has no mcpServers object: %v", doc)
	}
	for name, entry := range servers {
		e, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("masked entry %q is not an object: %v", name, entry)
		}
		if e[mcpMaskedMarkerKey] != true || len(e) != 1 {
			t.Errorf("entry %q must be exactly the masked placeholder, got %v", name, e)
		}
		names = append(names, name)
	}
	return names
}

func TestGetAgent_ForeignMcpConfigKeepsServerNamesWithoutValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "maskshape", []byte(maskTestMcpConfig), nil)

	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, secret := range []string{"member-jira-token", "member-confluence-token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %q: %s", secret, body)
		}
	}

	var resp AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.McpConfigRedacted {
		t.Error("masked response must still carry mcp_config_redacted=true")
	}
	names := maskedServerNames(t, decodeMcpDocument(t, resp.McpConfig))
	if len(names) != 2 {
		t.Fatalf("expected both server names to stay visible, got %v", names)
	}
	got := strings.Join([]string{names[0], names[1]}, ",")
	if !strings.Contains(got, "jira") || !strings.Contains(got, "confluence") {
		t.Errorf("server names missing from masked view: %v", names)
	}
}

func TestUpdateAgent_AdminRevokesOneServerWithoutReadingTheOthers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _ := secretVisibilityFixture(t, "maskwrite", []byte(maskTestMcpConfig), nil)

	body := map[string]any{"mcp_config": map[string]any{
		"mcpServers": map[string]any{
			"confluence": map[string]any{mcpMaskedMarkerKey: true},
		},
	}}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "member-confluence-token") {
		t.Fatalf("update response handed the preserved secret back to a non-owner: %s", w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(ctx, `SELECT mcp_config::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back mcp_config: %v", err)
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(stored), &doc); err != nil {
		t.Fatalf("decode stored mcp_config %s: %v", stored, err)
	}
	if _, ok := doc.McpServers["jira"]; ok {
		t.Error("the admin's delete did not remove the jira server")
	}
	if !strings.Contains(string(doc.McpServers["confluence"]), "member-confluence-token") {
		t.Errorf("the untouched server lost its stored secret: %s", doc.McpServers["confluence"])
	}
	if strings.Contains(stored, mcpMaskedMarkerKey) {
		t.Errorf("a masked placeholder was persisted as if it were a config: %s", stored)
	}
}

func TestUpdateAgent_MaskedPlaceholderForUnknownServerRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, _ := secretVisibilityFixture(t, "maskunknown", []byte(maskTestMcpConfig), nil)

	body := map[string]any{"mcp_config": map[string]any{
		"mcpServers": map[string]any{
			"jira-renamed": map[string]any{mcpMaskedMarkerKey: true},
		},
	}}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unresolvable placeholder, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MaskedPlaceholderRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	body := map[string]any{
		"name":       "masked placeholder create",
		"runtime_id": handlerTestRuntimeID(t),
		"mcp_config": map[string]any{
			"mcpServers": map[string]any{"jira": map[string]any{mcpMaskedMarkerKey: true}},
		},
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusBadRequest {
		var created AgentResponse
		json.Unmarshal(w.Body.Bytes(), &created)
		if created.ID != "" {
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
		}
		t.Fatalf("expected 400 for a placeholder on create, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_OwnerRoundTripsPlaintextUnchanged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, ownerUserID := secretVisibilityFixture(t, "maskowner", []byte(maskTestMcpConfig), nil)

	body := map[string]any{"mcp_config": map[string]any{
		"mcpServers": map[string]any{
			"jira": map[string]any{"env": map[string]any{"JIRA_PERSONAL_TOKEN": "rotated-by-owner"}},
		},
	}}
	req := withURLParam(newRequestAs(ownerUserID, http.MethodPut, "/api/agents/"+agentID, body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stored string
	if err := testPool.QueryRow(ctx, `SELECT mcp_config::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("read back mcp_config: %v", err)
	}
	if !strings.Contains(stored, "rotated-by-owner") {
		t.Errorf("owner write did not land: %s", stored)
	}
	if strings.Contains(stored, "member-confluence-token") {
		t.Errorf("owner sent a full document; the removed server must be gone: %s", stored)
	}
}

func TestMergeMaskedMcpConfig_PreservesTopLevelKeysTheWriterNeverSaw(t *testing.T) {
	stored := []byte(`{"mcpServers":{"jira":{"env":{"T":"secret"}}},"legacyBlock":{"k":"v"}}`)
	incoming := []byte(`{"mcpServers":{"jira":{"` + mcpMaskedMarkerKey + `":true}}}`)

	merged, err := mergeMaskedMcpConfig(stored, incoming, true)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("decode merged: %v", err)
	}
	if !strings.Contains(string(got["mcpServers"]), "secret") {
		t.Errorf("placeholder was not resolved: %s", merged)
	}
	if _, ok := got["legacyBlock"]; !ok {
		t.Errorf("a top-level key the masked writer never saw was dropped: %s", merged)
	}
}

func TestMergeMaskedMcpConfig_OwnerWriteDropsWhatTheyOmitted(t *testing.T) {
	stored := []byte(`{"mcpServers":{"jira":{"env":{"T":"secret"}}},"legacyBlock":{"k":"v"}}`)
	incoming := []byte(`{"mcpServers":{"jira":{"command":"/bin/x"}}}`)

	merged, err := mergeMaskedMcpConfig(stored, incoming, false)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if strings.Contains(string(merged), "legacyBlock") {
		t.Errorf("an owner sees the whole document, so omission means delete: %s", merged)
	}
	if strings.Contains(string(merged), "secret") {
		t.Errorf("owner replaced the entry; the old value must not come back: %s", merged)
	}
}

func TestMergeMaskedMcpConfig_UnresolvablePlaceholderErrors(t *testing.T) {
	stored := []byte(`{"mcpServers":{"jira":{"command":"/bin/x"}}}`)
	incoming := []byte(`{"mcpServers":{"nope":{"` + mcpMaskedMarkerKey + `":true}}}`)

	if _, err := mergeMaskedMcpConfig(stored, incoming, true); err == nil {
		t.Fatal("expected an error for a placeholder with nothing to resolve to")
	}
}

func TestMcpConfigDeclaresServer(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"absent", "", false},
		{"empty container", `{"mcpServers":{}}`, false},
		{"both containers empty", `{"mcpServers":{},"mcp":{}}`, false},
		{"one server", `{"mcpServers":{"jira":{"command":"x"}}}`, true},
		{"masked placeholder still counts", `{"mcpServers":{"jira":{"__goosar_masked__":true}}}`, true},
		{"unparseable over-reports", `{`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpConfigDeclaresServer([]byte(tc.raw)); got != tc.want {
				t.Fatalf("mcpConfigDeclaresServer(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
