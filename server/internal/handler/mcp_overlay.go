package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

func mergeMCPOverlay(agentMcpConfig, overlay json.RawMessage) (json.RawMessage, error) {
	if !hasManagedJSON(overlay) {
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}
	if !hasManagedJSON(agentMcpConfig) {

		var oCfg map[string]json.RawMessage
		if err := json.Unmarshal(overlay, &oCfg); err != nil {
			return nil, fmt.Errorf("merge mcp overlay: parse overlay: %w", err)
		}
		out, err := json.Marshal(oCfg)
		if err != nil {
			return nil, fmt.Errorf("merge mcp overlay: marshal overlay: %w", err)
		}
		return out, nil
	}

	var aCfg map[string]json.RawMessage
	if err := json.Unmarshal(agentMcpConfig, &aCfg); err != nil {

		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: parse agent mcp_config: %w", err)
	}
	var oCfg map[string]json.RawMessage
	if err := json.Unmarshal(overlay, &oCfg); err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: parse overlay: %w", err)
	}

	aServers, err := unmarshalServerMap(aCfg["mcpServers"])
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: agent mcpServers: %w", err)
	}
	oServers, err := unmarshalServerMap(oCfg["mcpServers"])
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: overlay mcpServers: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(aServers)+len(oServers))
	for k, v := range aServers {
		merged[k] = v
	}

	for k, v := range oServers {
		merged[k] = v
	}

	out := make(map[string]json.RawMessage, len(aCfg)+1)
	for k, v := range aCfg {
		if k == "mcpServers" {
			continue
		}
		out[k] = v
	}
	if len(merged) > 0 {
		serversBytes, err := json.Marshal(merged)
		if err != nil {
			return nil, fmt.Errorf("merge mcp overlay: marshal merged servers: %w", err)
		}
		out["mcpServers"] = serversBytes
	}
	if len(out) == 0 {
		return nil, nil
	}
	final, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("merge mcp overlay: marshal merged: %w", err)
	}
	return final, nil
}

func hasManagedJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func passthroughAgentMcpConfig(agentMcpConfig json.RawMessage) json.RawMessage {
	if !hasManagedJSON(agentMcpConfig) {
		return nil
	}
	return agentMcpConfig
}

func unmarshalServerMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if !hasManagedJSON(raw) {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]json.RawMessage{}, nil
	}

	for name, server := range m {
		if name == "" {
			return nil, errors.New("mcp server name must not be empty")
		}
		trimmed := bytes.TrimSpace(server)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, fmt.Errorf("mcpServers.%s must be a JSON object", name)
		}
	}
	return m, nil
}
