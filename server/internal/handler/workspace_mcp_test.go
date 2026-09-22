package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestResolveAgentMcpConfig_NoAssignmentsIsPassthrough(t *testing.T) {
	agentCfg := json.RawMessage(`{"mcpServers":{"own":{"command":"own-bin"}}}`)

	got, err := ResolveAgentMcpConfig(nil, agentCfg)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	if string(got) != string(agentCfg) {
		t.Fatalf("an agent with no assignments must be handed its own config unchanged; got %s", got)
	}

	if got, err := ResolveAgentMcpConfig(nil, nil); err != nil || got != nil {
		t.Fatalf("no assignments + no config must resolve to nil, got %s (err %v)", got, err)
	}
}

func TestResolveAgentMcpConfig_AssignedServerReachesTheAgent(t *testing.T) {
	assigned := []WorkspaceMcpAssignment{
		{Name: "shared", Config: json.RawMessage(`{"url":"https://mcp.example","headers":{"Authorization":"Bearer t"}}`)},
	}

	got, err := ResolveAgentMcpConfig(assigned, nil)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	servers := serversOf(t, got)
	if _, ok := servers["shared"]; !ok {
		t.Fatalf("an assigned server must reach an agent with no config of its own; got %s", got)
	}
	if len(servers) != 1 {
		t.Fatalf("expected exactly the assigned server, got %s", got)
	}
}

func TestResolveAgentMcpConfig_AgentOwnEntryWinsOnNameCollision(t *testing.T) {
	assigned := []WorkspaceMcpAssignment{
		{Name: "jira", Config: json.RawMessage(`{"url":"https://shared.example"}`)},
		{Name: "shared-only", Config: json.RawMessage(`{"url":"https://shared-only.example"}`)},
	}
	agentCfg := json.RawMessage(`{"mcpServers":{"jira":{"url":"https://agent.example"}}}`)

	got, err := ResolveAgentMcpConfig(assigned, agentCfg)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	servers := serversOf(t, got)
	if want := `{"url":"https://agent.example"}`; string(servers["jira"]) != want {
		t.Fatalf("the agent's own entry must win on a name collision: got %s, want %s", servers["jira"], want)
	}
	if _, ok := servers["shared-only"]; !ok {
		t.Fatalf("a shared server the agent does not redeclare must still be folded in; got %s", got)
	}
}

func TestResolveAgentMcpConfig_LegacyContainerAlsoOwnsItsNames(t *testing.T) {
	assigned := []WorkspaceMcpAssignment{
		{Name: "jira", Config: json.RawMessage(`{"url":"https://shared.example"}`)},
	}
	agentCfg := json.RawMessage(`{"mcp":{"jira":{"url":"https://legacy-agent.example"}}}`)

	got, err := ResolveAgentMcpConfig(assigned, agentCfg)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	servers := serversOf(t, got)
	if want := `{"url":"https://legacy-agent.example"}`; string(servers["jira"]) != want {
		t.Fatalf("a name declared in the legacy container is still the agent's: got %s, want %s", servers["jira"], want)
	}
}

func TestResolveAgentMcpConfig_NormalizesOntoOneContainer(t *testing.T) {
	assigned := []WorkspaceMcpAssignment{
		{Name: "shared", Config: json.RawMessage(`{"url":"https://mcp.example"}`)},
	}
	agentCfg := json.RawMessage(`{"mcp":{"legacy":{"command":"legacy-bin"}},"other":{"keep":true}}`)

	got, err := ResolveAgentMcpConfig(assigned, agentCfg)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("unmarshal resolved document: %v", err)
	}
	if _, present := doc["mcp"]; present {
		t.Fatalf("the legacy container must be consumed, not left behind as a second copy: %s", got)
	}
	if _, present := doc["other"]; !present {
		t.Fatalf("unrelated top-level keys must survive: %s", got)
	}
	servers := serversOf(t, got)
	for _, name := range []string{"shared", "legacy"} {
		if _, ok := servers[name]; !ok {
			t.Fatalf("%q missing from the normalized container: %s", name, got)
		}
	}
}

func TestResolveAgentMcpConfig_MalformedAgentConfigIsPassedThrough(t *testing.T) {
	assigned := []WorkspaceMcpAssignment{
		{Name: "shared", Config: json.RawMessage(`{"url":"https://mcp.example"}`)},
	}
	broken := json.RawMessage(`{"mcpServers":`)

	got, err := ResolveAgentMcpConfig(assigned, broken)
	if err == nil {
		t.Fatal("a malformed agent config must surface an error")
	}
	if string(got) != string(broken) {
		t.Fatalf("the agent's bytes must be returned unchanged on failure; got %s", got)
	}
}

func TestValidateWorkspaceMcpServerEntry_RejectsToggleFields(t *testing.T) {
	for _, body := range []string{`{"enabled":false,"url":"https://x"}`, `{"disabled":true,"url":"https://x"}`} {
		if err := validateWorkspaceMcpServerEntry(json.RawMessage(body)); !errors.Is(err, errWorkspaceMcpEntryToggle) {
			t.Fatalf("validateWorkspaceMcpServerEntry(%s) = %v, want the toggle rejection", body, err)
		}
	}
	if err := validateWorkspaceMcpServerEntry(json.RawMessage(`{"url":"https://x"}`)); err != nil {
		t.Fatalf("a plain entry must be accepted: %v", err)
	}
}

func TestValidateWorkspaceMcpServerEntry_RejectsNonObjectsAndReservedKeys(t *testing.T) {
	for _, body := range []string{``, `[]`, `"nope"`, `{}`, `{"__goosar_masked__":true}`} {
		if err := validateWorkspaceMcpServerEntry(json.RawMessage(body)); err == nil {
			t.Fatalf("validateWorkspaceMcpServerEntry(%q) must fail", body)
		}
	}
}

func TestValidateWorkspaceMcpServerEntry_ErrorDoesNotEchoTheBody(t *testing.T) {
	err := validateWorkspaceMcpServerEntry(json.RawMessage(`{"headers":{"Authorization":"Bearer super-secret-token"`))
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if got := err.Error(); strings.Contains(got, "super-secret-token") {
		t.Fatalf("the validation error echoed the entry: %q", got)
	}
}

func TestMcpTransportOf(t *testing.T) {
	cases := map[string]string{
		`{"command":"npx"}`:                  "stdio",
		`{"url":"https://x"}`:                "http",
		`{"type":"local","command":"npx"}`:   "stdio",
		`{"type":"streamable-http"}`:         "http",
		`{"type":"sse","url":"https://x"}`:   "sse",
		`{"type":"websocket","url":"wss:x"}`: "websocket",
		`{"note":"neither"}`:                 "unknown",
		`not json`:                           "unknown",
	}
	for entry, want := range cases {
		if got := mcpTransportOf(json.RawMessage(entry)); got != want {
			t.Errorf("mcpTransportOf(%s) = %q, want %q — an explicit unknown type must be reported verbatim, never inferred into http", entry, got, want)
		}
	}
}

func serversOf(t *testing.T, doc json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("unmarshal resolved document %s: %v", doc, err)
	}
	servers, err := unmarshalServerMap(parsed["mcpServers"])
	if err != nil {
		t.Fatalf("unmarshal mcpServers of %s: %v", doc, err)
	}
	return servers
}
