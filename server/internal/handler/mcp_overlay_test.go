package handler

import (
	"encoding/json"
	"testing"
)

func TestMergeMCPOverlayAgentNilOverlayNil(t *testing.T) {
	cases := []struct {
		name    string
		agent   json.RawMessage
		overlay json.RawMessage
	}{
		{"both_nil", nil, nil},
		{"agent_null_overlay_nil", json.RawMessage("null"), nil},
		{"agent_nil_overlay_null", nil, json.RawMessage("null")},
		{"agent_empty_overlay_empty", json.RawMessage(""), json.RawMessage("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeMCPOverlay(tc.agent, tc.overlay)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil result, got %s", string(got))
			}
		})
	}
}

func TestMergeMCPOverlayAgentOnly(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`)

	got, err := mergeMCPOverlay(agent, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != string(agent) {
		t.Errorf("expected pass-through, got %s", string(got))
	}
}

func TestMergeMCPOverlayOverlayOnly(t *testing.T) {
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc","headers":{"Authorization":"Bearer mcp_xyz"}}}}`)

	got, err := mergeMCPOverlay(nil, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcpServers, got %s", string(got))
	}
	if _, ok := servers["composio"]; !ok {
		t.Errorf("expected composio server, got %s", string(got))
	}
}

func TestMergeMCPOverlayMergesBothSides(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"},"github":{"command":"npx"}}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcpServers, got %s", string(got))
	}
	for _, want := range []string{"fetch", "github", "composio"} {
		if _, ok := servers[want]; !ok {
			t.Errorf("missing server %q in merged result %s", want, string(got))
		}
	}
}

func TestMergeMCPOverlayCollisionOverlayWins(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://placeholder.example/old"}}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/new"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cfg map[string]map[string]map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	gotURL, _ := cfg["mcpServers"]["composio"]["url"].(string)
	if gotURL != "https://mcp.composio.dev/s/new" {
		t.Errorf("collision: expected overlay URL to win, got %q", gotURL)
	}
}

func TestMergeMCPOverlayPreservesAgentTopLevelKeys(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}},"experimental":{"foo":"bar"}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := cfg["experimental"]; !ok {
		t.Errorf("expected experimental key preserved, got %s", string(got))
	}
}

func TestMergeMCPOverlayBadOverlayFallsBackToAgent(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`)
	overlay := json.RawMessage(`{ this is not json`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if string(got) != string(agent) {
		t.Errorf("expected agent config preserved on overlay parse failure, got %s", string(got))
	}
}

func TestMergeMCPOverlayBadAgentReturnsBytesAndError(t *testing.T) {
	agent := json.RawMessage(`{ this is not json`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if string(got) != string(agent) {
		t.Errorf("expected agent bytes returned unchanged, got %s", string(got))
	}
}

func TestMergeMCPOverlayRejectsNonObjectServer(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":"not-an-object"}}`)

	if _, err := mergeMCPOverlay(agent, overlay); err == nil {
		t.Fatalf("expected error for non-object server, got nil")
	}
}
