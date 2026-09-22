package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type WorkspaceMcpAssignment struct {
	Name   string
	Config json.RawMessage
}

func ResolveAgentMcpConfig(assigned []WorkspaceMcpAssignment, agentMcpConfig json.RawMessage) (json.RawMessage, error) {
	if len(assigned) == 0 {
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}

	shared := make(map[string]json.RawMessage, len(assigned))
	for _, server := range assigned {
		if server.Name == "" || !hasManagedJSON(server.Config) {
			continue
		}
		if _, taken := shared[server.Name]; taken {

			continue
		}
		shared[server.Name] = server.Config
	}
	if len(shared) == 0 {
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}

	if !hasManagedJSON(agentMcpConfig) {

		out, err := json.Marshal(map[string]any{"mcpServers": shared})
		if err != nil {
			return nil, fmt.Errorf("resolve agent mcp_config: marshal assigned servers: %w", err)
		}
		return out, nil
	}

	var agentDoc map[string]json.RawMessage
	if err := json.Unmarshal(agentMcpConfig, &agentDoc); err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("resolve agent mcp_config: parse agent mcp_config: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(shared))
	for _, container := range [...]string{"mcp", "mcpServers"} {
		own, err := unmarshalServerMap(agentDoc[container])
		if err != nil {
			return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("resolve agent mcp_config: agent %s: %w", container, err)
		}
		for name, server := range own {
			merged[name] = server
		}
	}

	for name, server := range shared {
		if _, taken := merged[name]; taken {
			continue
		}
		merged[name] = server
	}

	out := make(map[string]json.RawMessage, len(agentDoc)+1)
	for k, v := range agentDoc {

		if isMcpContainer(k) {
			continue
		}
		out[k] = v
	}
	serversBytes, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("resolve agent mcp_config: marshal merged servers: %w", err)
	}
	out["mcpServers"] = serversBytes

	final, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("resolve agent mcp_config: marshal merged document: %w", err)
	}
	return final, nil
}

var errWorkspaceMcpEntryToggle = errors.New(`config must not contain "enabled" or "disabled": the on/off switch belongs to the per-agent assignment, not to the server entry`)

func validateWorkspaceMcpServerEntry(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return errors.New("config must be a JSON object")
	}
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &entry); err != nil {

		return errors.New("config must be a JSON object")
	}
	if len(entry) == 0 {
		return errors.New("config must not be empty")
	}
	for key := range entry {
		switch key {
		case "enabled", "disabled":
			return errWorkspaceMcpEntryToggle
		case mcpMaskedMarkerKey:

			return errors.New("config must not contain " + mcpMaskedMarkerKey)
		}
	}
	return nil
}

func validateWorkspaceMcpServerName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return errors.New("name may only contain letters, digits, hyphens, and underscores")
		}
	}
	return nil
}

func mcpTransportOf(entry json.RawMessage) string {
	var e struct {
		Type    string          `json:"type"`
		Command json.RawMessage `json:"command"`
		URL     json.RawMessage `json:"url"`
	}
	if err := json.Unmarshal(entry, &e); err != nil {
		return "unknown"
	}
	if declared := strings.ToLower(strings.TrimSpace(e.Type)); declared != "" {
		switch declared {
		case "local", "stdio":
			return "stdio"
		case "remote", "http", "streamable-http":
			return "http"
		}

		return declared
	}
	if len(e.Command) > 0 {
		return "stdio"
	}
	if len(e.URL) > 0 {
		return "http"
	}
	return "unknown"
}
