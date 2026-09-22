package handler

import (
	"encoding/json"
	"errors"
	"fmt"
)

const mcpMaskedMarkerKey = "__goosar_masked__"

var mcpServerContainers = [...]string{"mcpServers", "mcp"}

var errMcpMaskedUnresolved = errors.New("mcp_config placeholder does not match any stored server")

var errMcpMaskedWriteNotAllowed = errors.New("only the agent owner can add or change an MCP server: an MCP server is a command that runs on the machine hosting this agent, with the agent's environment variables in scope. You can remove entries you were shown")

func isMcpContainer(key string) bool {
	for _, c := range mcpServerContainers {
		if key == c {
			return true
		}
	}
	return false
}

func isMaskedMcpEntry(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	if len(obj) != 1 {
		return false
	}
	v, ok := obj[mcpMaskedMarkerKey]
	if !ok {
		return false
	}
	var b bool
	return json.Unmarshal(v, &b) == nil && b
}

func containsMaskedMcpMarker(raw []byte) bool {
	containers, _, ok := splitMcpDocument(raw)
	if !ok {
		return false
	}
	for _, entries := range containers {
		for _, entry := range entries {
			if isMaskedMcpEntry(entry) {
				return true
			}
		}
	}
	return false
}

func splitMcpDocument(raw []byte) (containers map[string]map[string]json.RawMessage, rest map[string]json.RawMessage, ok bool) {
	if len(raw) == 0 {
		return nil, nil, false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return nil, nil, false
	}
	containers = map[string]map[string]json.RawMessage{}
	rest = map[string]json.RawMessage{}
	for key, value := range doc {
		if !isMcpContainer(key) {
			rest[key] = value
			continue
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(value, &entries); err != nil || entries == nil {

			rest[key] = value
			continue
		}
		containers[key] = entries
	}
	return containers, rest, true
}

func maskMcpConfigDocument(raw []byte) (json.RawMessage, bool) {
	containers, _, ok := splitMcpDocument(raw)
	if !ok {
		return nil, false
	}
	masked := map[string]map[string]json.RawMessage{}
	for name, entries := range containers {
		out := make(map[string]json.RawMessage, len(entries))
		for server := range entries {
			out[server] = json.RawMessage(`{"` + mcpMaskedMarkerKey + `":true}`)
		}
		masked[name] = out
	}
	if len(masked) == 0 {
		return nil, false
	}
	encoded, err := json.Marshal(masked)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func mergeMaskedMcpConfig(stored, incoming []byte, fromMaskedView bool) ([]byte, error) {
	incomingContainers, incomingRest, ok := splitMcpDocument(incoming)
	if !ok {

		if containsMaskedMcpMarker(incoming) {
			return nil, errMcpMaskedUnresolved
		}
		if fromMaskedView {

			return nil, errMcpMaskedWriteNotAllowed
		}
		return incoming, nil
	}
	if fromMaskedView {

		if len(incomingRest) > 0 {
			return nil, errMcpMaskedWriteNotAllowed
		}
		for _, entries := range incomingContainers {
			for _, entry := range entries {
				if !isMaskedMcpEntry(entry) {
					return nil, errMcpMaskedWriteNotAllowed
				}
			}
		}
	}

	storedContainers, storedRest, storedOK := splitMcpDocument(stored)
	if !storedOK {
		storedContainers = map[string]map[string]json.RawMessage{}
		storedRest = map[string]json.RawMessage{}
	}

	out := map[string]json.RawMessage{}
	for key, value := range incomingRest {
		out[key] = value
	}
	if fromMaskedView {
		for key, value := range storedRest {
			if _, present := out[key]; !present {
				out[key] = value
			}
		}
	}

	for container, entries := range incomingContainers {
		resolved := make(map[string]json.RawMessage, len(entries))
		for name, entry := range entries {
			if !isMaskedMcpEntry(entry) {
				resolved[name] = entry
				continue
			}
			previous, found := storedContainers[container][name]
			if !found {
				return nil, fmt.Errorf("%w: %q", errMcpMaskedUnresolved, name)
			}
			resolved[name] = previous
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			return nil, fmt.Errorf("encode mcp_config container %q: %w", container, err)
		}
		out[container] = encoded
	}

	merged, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode mcp_config: %w", err)
	}
	return merged, nil
}
