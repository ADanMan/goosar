package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type runtimeMMCPLocal struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Timeout     *int              `json:"timeout,omitempty"`
}

type runtimeMMCPRemote struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	OAuth   json.RawMessage   `json:"oauth,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
	Timeout *int              `json:"timeout,omitempty"`
}

type runtimeMMCPEnabledOnly struct {
	Enabled *bool `json:"enabled"`
}

type runtimeMMCPOAuth struct {
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	Scope        string `json:"scope,omitempty"`
	CallbackPort *int   `json:"callbackPort,omitempty"`
	RedirectURI  string `json:"redirectUri,omitempty"`
}

func buildRuntimeMMCPConfigContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	servers, err := translateMCPConfigForRuntimeM(raw)
	if err != nil {
		return "", err
	}

	if len(servers) == 0 {
		return "", nil
	}
	data, err := json.Marshal(map[string]any{"mcp": servers})
	if err != nil {
		return "", fmt.Errorf("opencode mcp_config: marshal: %w", err)
	}
	return string(data), nil
}

func translateMCPConfigForRuntimeM(raw json.RawMessage) (map[string]any, error) {
	var payload struct {
		MCPServers map[string]map[string]any  `json:"mcpServers"`
		MCP        map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("opencode mcp_config: parse mcp_config: %w", err)
	}
	if len(payload.MCPServers) == 0 {
		if payload.MCP == nil {
			return map[string]any{}, nil
		}
		return validateRuntimeMNativeMCPMap(payload.MCP)
	}

	servers := make(map[string]any, len(payload.MCPServers)+len(payload.MCP))
	for name, rawEntry := range payload.MCP {
		validated, err := validateRuntimeMNativeMCPEntry(name, rawEntry)
		if err != nil {
			return nil, err
		}
		servers[name] = validated
	}
	for name, server := range payload.MCPServers {
		translated, err := translateMCPServerForRuntimeM(name, server)
		if err != nil {
			return nil, err
		}

		rawTranslated, err := json.Marshal(translated)
		if err != nil {
			return nil, fmt.Errorf("opencode mcp_config: server %q: marshal translated entry: %w", name, err)
		}
		validated, err := validateRuntimeMNativeMCPEntry(name, rawTranslated)
		if err != nil {
			return nil, err
		}
		servers[name] = validated
	}
	return servers, nil
}

func validateRuntimeMNativeMCPMap(mcp map[string]json.RawMessage) (map[string]any, error) {
	out := make(map[string]any, len(mcp))
	for name, raw := range mcp {
		validated, err := validateRuntimeMNativeMCPEntry(name, raw)
		if err != nil {
			return nil, err
		}
		out[name] = validated
	}
	return out, nil
}

func validateRuntimeMNativeMCPEntry(name string, raw json.RawMessage) (map[string]any, error) {
	wrap := func(err error) error {
		return fmt.Errorf("opencode mcp_config: server %q: %w", name, err)
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, wrap(errors.New("entry must be a JSON object"))
	}

	var probe struct {
		Type *json.RawMessage `json:"type,omitempty"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, wrap(fmt.Errorf("parse: %w", err))
	}
	var typeStr string
	if probe.Type != nil {
		if err := json.Unmarshal(*probe.Type, &typeStr); err != nil {
			return nil, wrap(fmt.Errorf("`type` must be a string, got %s", strings.TrimSpace(string(*probe.Type))))
		}
	}

	switch typeStr {
	case "local":
		var entry runtimeMMCPLocal
		if err := strictDecode(raw, &entry); err != nil {
			return nil, wrap(err)
		}
		if len(entry.Command) == 0 {
			return nil, wrap(errors.New("local server missing required field `command`"))
		}
		if entry.Timeout != nil && *entry.Timeout <= 0 {
			return nil, wrap(fmt.Errorf("`timeout` must be a positive integer, got %d", *entry.Timeout))
		}
	case "remote":
		var entry runtimeMMCPRemote
		if err := strictDecode(raw, &entry); err != nil {
			return nil, wrap(err)
		}
		if entry.URL == "" {
			return nil, wrap(errors.New("remote server missing required field `url`"))
		}
		if entry.Timeout != nil && *entry.Timeout <= 0 {
			return nil, wrap(fmt.Errorf("`timeout` must be a positive integer, got %d", *entry.Timeout))
		}
		if len(entry.OAuth) > 0 {
			if err := validateRuntimeMOAuth(entry.OAuth); err != nil {
				return nil, wrap(fmt.Errorf("`oauth`: %w", err))
			}
		}
	case "":

		var entry runtimeMMCPEnabledOnly
		if err := strictDecode(raw, &entry); err != nil || entry.Enabled == nil {
			return nil, wrap(errors.New("missing required field `type` (must be \"local\" or \"remote\", or use bare {\"enabled\": bool} to override an inherited server)"))
		}
	default:
		return nil, wrap(fmt.Errorf("invalid type %q (must be \"local\" or \"remote\")", typeStr))
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, wrap(fmt.Errorf("parse: %w", err))
	}
	return out, nil
}

func strictDecode(raw json.RawMessage, target any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

func validateRuntimeMOAuth(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("false")) {
		return nil
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("must be an object or `false`, got %s", string(trimmed))
	}
	var oauth runtimeMMCPOAuth
	if err := strictDecode(raw, &oauth); err != nil {
		return err
	}

	if oauth.CallbackPort != nil && (*oauth.CallbackPort < 1 || *oauth.CallbackPort > 65535) {
		return fmt.Errorf("`callbackPort` must be in 1..65535, got %d", *oauth.CallbackPort)
	}
	return nil
}

func translateMCPServerForRuntimeM(name string, server map[string]any) (map[string]any, error) {
	if url, ok := stringField(server, "url"); ok && url != "" {
		out := map[string]any{
			"type": "remote",
			"url":  url,
		}
		if v, ok := server["enabled"].(bool); ok {
			out["enabled"] = v
		}
		copyIfPresent(out, server, "headers")
		copyIfPresent(out, server, "oauth")
		copyIfPresent(out, server, "timeout")
		return out, nil
	}

	command, err := runtimeMCommand(server)
	if err != nil {
		return nil, fmt.Errorf("server %q: %w", name, err)
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("server %q has neither url nor command", name)
	}
	out := map[string]any{
		"type":    "local",
		"command": command,
	}
	if v, ok := server["enabled"].(bool); ok {
		out["enabled"] = v
	}
	if env, ok := server["env"]; ok {
		out["environment"] = env
	} else {
		copyIfPresent(out, server, "environment")
	}
	copyIfPresent(out, server, "timeout")
	return out, nil
}

func runtimeMCommand(server map[string]any) ([]string, error) {
	raw, ok := server["command"]
	if !ok {
		return nil, nil
	}
	switch v := raw.(type) {
	case string:
		cmd := []string{v}
		args, err := stringSliceField(server, "args")
		if err != nil {
			return nil, err
		}
		return append(cmd, args...), nil
	case []any:
		cmd := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("command array must contain only strings")
			}
			cmd = append(cmd, s)
		}
		return cmd, nil
	default:
		return nil, fmt.Errorf("command must be a string or string array")
	}
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func stringSliceField(m map[string]any, key string) ([]string, error) {
	raw, ok := m[key]
	if !ok {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must contain only strings", key)
		}
		out = append(out, s)
	}
	return out, nil
}

func copyIfPresent(dst, src map[string]any, key string) {
	if v, ok := src[key]; ok {
		dst[key] = v
	}
}
