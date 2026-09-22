package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adanman/goosar/server/internal/handler"
)

func TestResolvedWorkspaceMcpConfigIsReadableByTheRuntimeMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatalf("create runtime config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".cursor", "mcp.json"),
		[]byte(`{"mcpServers":{"machine-local":{"command":"local-bin"}}}`), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	resolved, err := handler.ResolveAgentMcpConfig(
		[]handler.WorkspaceMcpAssignment{
			{Name: "workspace-shared", Config: json.RawMessage(`{"url":"https://shared.example"}`)},
		},
		json.RawMessage(`{"mcp":{"agent-own":{"command":"agent-bin"}}}`),
	)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}

	merged, _, err := mergeRuntimeAndAgentMcpConfig("runtime-g", resolved, nil)
	if err != nil {
		t.Fatalf("mergeRuntimeAndAgentMcpConfig: %v", err)
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("decode merged config: %v", err)
	}
	for _, name := range []string{"machine-local", "agent-own", "workspace-shared"} {
		if _, ok := doc.McpServers[name]; !ok {
			t.Fatalf("%q is missing from the task-local config the daemon builds: %s", name, merged)
		}
	}
}

func TestResolverLeavesAnUnassignedAgentAloneForTheRuntimeMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if err := os.MkdirAll(filepath.Join(home, "config", "opencode"), 0o755); err != nil {
		t.Fatalf("create runtime config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "opencode", "opencode.json"),
		[]byte(`{"mcp":{"machine-local":{"command":"local-bin"}}}`), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	agentCfg := json.RawMessage(`{"mcp":{"agent-own":{"command":"agent-bin"}}}`)
	resolved, err := handler.ResolveAgentMcpConfig(nil, agentCfg)
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	if string(resolved) != string(agentCfg) {
		t.Fatalf("an agent with no assignments must be handed its own bytes: got %s", resolved)
	}

	merged, _, err := mergeRuntimeAndAgentMcpConfig("runtime-m", resolved, nil)
	if err != nil {
		t.Fatalf("mergeRuntimeAndAgentMcpConfig: %v", err)
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("decode merged config: %v", err)
	}
	for _, name := range []string{"machine-local", "agent-own"} {
		if _, ok := doc.McpServers[name]; !ok {
			t.Fatalf("%q is missing from the task-local config: %s", name, merged)
		}
	}
}
