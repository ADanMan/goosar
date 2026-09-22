package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestFilterDisabledMcpServersRemovesDisabledOnly(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"on":{"command":"docker"},"off":{"command":"mcp-atlassian","enabled":false},"also-off":{"command":"ewsmcp","disabled":true}}}`)
	got := filterDisabledMcpServers(raw, slog.Default())
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatalf("parse filtered config: %v", err)
	}
	if len(document.McpServers) != 1 {
		t.Fatalf("servers = %#v, want only \"on\"", document.McpServers)
	}
	if _, ok := document.McpServers["on"]; !ok {
		t.Fatalf("enabled entry missing: %s", got)
	}
}

func TestFilterDisabledMcpServersKeepsManagedEnvelopeWhenAllDisabled(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"off":{"command":"x","enabled":false}}}`)
	got := filterDisabledMcpServers(raw, slog.Default())
	if !hasManagedMcpConfig(got) {
		t.Fatalf("filtered config lost managed-ness: %q", string(got))
	}
	var document struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatalf("parse filtered config: %v", err)
	}
	if document.McpServers == nil || len(document.McpServers) != 0 {
		t.Fatalf("want empty mcpServers map, got %q", string(got))
	}
}

func TestFilterDisabledMcpServersPassthrough(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]json.RawMessage{
		"nil":               nil,
		"null":              json.RawMessage("null"),
		"no mcpServers key": json.RawMessage(`{"other":true}`),
		"non-object top":    json.RawMessage(`[1,2]`),
		"non-object map":    json.RawMessage(`{"mcpServers":[1]}`),
		"nothing disabled":  json.RawMessage(`{"mcpServers":{"a":{"command":"docker"}},"extra":1}`),
	} {
		if got := filterDisabledMcpServers(raw, slog.Default()); !bytes.Equal(got, raw) {
			t.Errorf("%s: got %q, want input unchanged %q", name, string(got), string(raw))
		}
	}
}

func TestFilterDisabledMcpServersPreservesOtherTopLevelKeys(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"off":{"command":"x","enabled":false}},"note":"keep-me"}`)
	got := filterDisabledMcpServers(raw, slog.Default())
	var document map[string]json.RawMessage
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatalf("parse filtered config: %v", err)
	}
	if string(document["note"]) != `"keep-me"` {
		t.Fatalf("non-mcpServers key lost: %q", string(got))
	}
}
