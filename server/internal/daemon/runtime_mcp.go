package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"github.com/adanman/goosar/server/internal/perimeterpolicy"
)

type runtimeLocalMcpServerSummary struct {
	Name      string `json:"name"`
	Transport string `json:"transport,omitempty"`
	Source    string `json:"source,omitempty"`
	Enabled   bool   `json:"enabled"`

	Blocked       bool   `json:"blocked,omitempty"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

type blockedLocalMcpServer struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func mergeRuntimeAndAgentMcpConfig(provider string, agentConfig json.RawMessage, policy *perimeterpolicy.MCPPolicy) (json.RawMessage, []blockedLocalMcpServer, error) {
	trimmed := bytes.TrimSpace(agentConfig)
	nativeInheritance := len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
	if nativeInheritance && !policy.Restricted() {
		return agentConfig, nil, nil
	}

	runtimeServers, supported, err := loadRuntimeMcpServerConfigs(provider)
	if err != nil {
		return nil, nil, err
	}
	if !supported {

		if policy.Restricted() {
			slog.Warn("mcp_config: deployment MCP allowlist is not enforced on runtime-local servers for this provider",
				"provider", provider,
				"reason", "the daemon does not know where this provider stores its local MCP configuration")
		}
		return agentConfig, nil, nil
	}
	runtimeServers, blocked := filterLocalMcpServersByPolicy(runtimeServers, policy)
	if nativeInheritance {

		raw, err := json.Marshal(map[string]any{"mcpServers": runtimeServers})
		if err != nil {
			return nil, nil, fmt.Errorf("marshal merged MCP config: %w", err)
		}
		return raw, blocked, nil
	}

	var agentDocument map[string]any
	if err := json.Unmarshal(trimmed, &agentDocument); err != nil {
		return nil, nil, fmt.Errorf("parse agent MCP config: %w", err)
	}
	agentServers := map[string]any{}
	if servers, ok := nestedRuntimeMcpMap(agentDocument, "mcpServers"); ok {
		agentServers = servers
	} else if provider == runtimeCodeM {

		if servers, ok := nestedRuntimeMcpMap(agentDocument, "mcp"); ok {
			agentServers = servers
		}
	}

	merged := make(map[string]any, len(runtimeServers)+len(agentServers))
	for name, entry := range runtimeServers {
		merged[name] = entry
	}
	for name, entry := range agentServers {

		if !mcpEntryValueEnabled(entry) {
			delete(merged, name)
			continue
		}
		merged[name] = entry
	}

	raw, err := json.Marshal(map[string]any{"mcpServers": merged})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal merged MCP config: %w", err)
	}
	return raw, blocked, nil
}

func filterLocalMcpServersByPolicy(servers map[string]any, policy *perimeterpolicy.MCPPolicy) (map[string]any, []blockedLocalMcpServer) {
	if !policy.Restricted() || len(servers) == 0 {
		return servers, nil
	}
	kept := make(map[string]any, len(servers))
	var blocked []blockedLocalMcpServer
	for _, name := range sortedMcpNames(servers) {
		raw, err := json.Marshal(servers[name])
		if err != nil {
			blocked = append(blocked, blockedLocalMcpServer{Name: name, Reason: "entry could not be encoded for the MCP policy check"})
			continue
		}
		if v := policy.CheckEntry(name, raw); v != nil {
			blocked = append(blocked, blockedLocalMcpServer{Name: name, Reason: v.Reason})
			continue
		}
		kept[name] = servers[name]
	}
	return kept, blocked
}

func sortedMcpNames(servers map[string]any) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadRuntimeMcpServerConfigs(provider string) (map[string]any, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("resolve user home: %w", err)
	}

	path, key, format, supported := runtimeMcpConfigLocation(provider, home)
	if !supported {
		return map[string]any{}, false, nil
	}

	servers := map[string]any{}
	raw, err := os.ReadFile(path)
	if err == nil {
		var cfg map[string]any
		switch format {
		case "toml":
			if err := toml.Unmarshal(raw, &cfg); err != nil {
				return nil, true, fmt.Errorf("parse runtime MCP config: %w", err)
			}
		case "yaml":
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				return nil, true, fmt.Errorf("parse runtime MCP config: %w", err)
			}
		default:
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, true, fmt.Errorf("parse runtime MCP config: %w", err)
			}
		}
		if configured, ok := nestedRuntimeMcpMap(cfg, key); ok {
			for name, entry := range configured {

				if !mcpEntryValueEnabled(entry) {
					continue
				}
				servers[name] = normalizeRuntimeMcpEntry(provider, entry)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("read runtime MCP config: %w", err)
	}

	if pluginRegistry, ok := resolveRuntimeCPluginRegistry(provider, home, false); ok {
		for name, entry := range loadRuntimeCPluginMcpServerConfigs(pluginRegistry) {
			if !mcpEntryValueEnabled(entry) {
				continue
			}
			if _, exists := servers[name]; !exists {
				servers[name] = entry
			}
		}
	}
	return servers, true, nil
}

func runtimeMcpConfigLocation(provider, home string) (path, key, format string, supported bool) {
	descriptor, ok := runtimeDescriptor(provider)
	if !ok || descriptor.MCPConfig == nil {
		return "", "", "", false
	}
	spec := descriptor.MCPConfig
	path, resolved := descriptor.MCPConfigPath(home, os.Getenv)
	if !resolved {
		return "", "", "", false
	}
	return path, spec.Key, spec.Format, true
}

func normalizeRuntimeMcpEntry(provider string, value any) any {
	entry, ok := value.(map[string]any)
	if !ok || provider != runtimeCodeE {
		return value
	}

	if headers, ok := entry["http_headers"]; ok {
		if _, exists := entry["headers"]; !exists {
			entry["headers"] = headers
		}
	}
	if _, hasURL := entry["url"]; hasURL {
		if _, hasType := entry["type"]; !hasType {
			entry["type"] = "http"
		}
	}
	return entry
}

func loadRuntimeCPluginMcpServerConfigs(pluginRegistry runtimeCPluginRegistry) map[string]any {
	out := map[string]any{}
	for _, plugin := range pluginRegistry.listEnabledRuntimeCPlugins() {
		manifest, _ := pluginRegistry.readRuntimeCPluginManifest(plugin.InstallPath)
		for _, path := range pluginRegistry.mcpConfigPaths(plugin, manifest) {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var cfg map[string]any
			if json.Unmarshal(raw, &cfg) != nil {
				continue
			}
			servers, ok := nestedRuntimeMcpMap(cfg, "mcpServers")
			if !ok {
				continue
			}
			for name, entry := range servers {
				if _, exists := out[name]; !exists {
					out[name] = entry
				}
			}
		}
	}
	return out
}

func listRuntimeLocalMcpServers(provider string, policy *perimeterpolicy.MCPPolicy) ([]runtimeLocalMcpServerSummary, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("resolve user home: %w", err)
	}

	path, key, format, supported := runtimeMcpConfigLocation(provider, home)
	if !supported {
		return []runtimeLocalMcpServerSummary{}, false, nil
	}
	const source = "User config"

	out := make([]runtimeLocalMcpServerSummary, 0)
	raw, err := os.ReadFile(path)
	if err == nil {
		var cfg map[string]any
		switch format {
		case "toml":
			if err := toml.Unmarshal(raw, &cfg); err != nil {
				return nil, true, fmt.Errorf("parse runtime MCP config: %w", err)
			}
		case "yaml":
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				return nil, true, fmt.Errorf("parse runtime MCP config: %w", err)
			}
		default:
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, true, fmt.Errorf("parse runtime MCP config: %w", err)
			}
		}
		if servers, ok := nestedRuntimeMcpMap(cfg, key); ok {
			out = append(out, runtimeMcpSummaries(servers, source, policy)...)
		}
	} else if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("read runtime MCP config: %w", err)
	}

	if pluginRegistry, ok := resolveRuntimeCPluginRegistry(provider, home, false); ok {
		out = append(out, listRuntimeCPluginMcpServers(pluginRegistry, policy)...)
	}

	deduped := make([]runtimeLocalMcpServerSummary, 0, len(out))
	seen := make(map[string]bool)
	for _, server := range out {
		if seen[server.Name] {
			continue
		}
		seen[server.Name] = true
		deduped = append(deduped, server)
	}
	out = deduped
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, true, nil
}

func runtimeMcpSummaries(servers map[string]any, source string, policy *perimeterpolicy.MCPPolicy) []runtimeLocalMcpServerSummary {
	out := make([]runtimeLocalMcpServerSummary, 0, len(servers))
	for name, value := range servers {
		entry, ok := value.(map[string]any)
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		summary := runtimeLocalMcpServerSummary{
			Name:      name,
			Transport: runtimeMcpTransport(entry),
			Source:    source,
			Enabled:   runtimeMcpEntryEnabled(entry),
		}

		if policy.Restricted() {
			if raw, err := json.Marshal(entry); err != nil {
				summary.Blocked = true
				summary.BlockedReason = "entry could not be encoded for the MCP policy check"
			} else if v := policy.CheckEntry(name, raw); v != nil {
				summary.Blocked = true
				summary.BlockedReason = v.Reason
			}
		}
		out = append(out, summary)
	}
	return out
}

func runtimeMcpEntryEnabled(entry map[string]any) bool {
	enabled := true
	if value, ok := entry["enabled"].(bool); ok {
		enabled = value
	}
	if value, ok := entry["disabled"].(bool); ok && value {
		enabled = false
	}
	return enabled
}

func mcpEntryValueEnabled(value any) bool {
	entry, ok := value.(map[string]any)
	if !ok {
		return true
	}
	return runtimeMcpEntryEnabled(entry)
}

func listRuntimeCPluginMcpServers(pluginRegistry runtimeCPluginRegistry, policy *perimeterpolicy.MCPPolicy) []runtimeLocalMcpServerSummary {
	out := make([]runtimeLocalMcpServerSummary, 0)
	for _, plugin := range pluginRegistry.listEnabledRuntimeCPlugins() {
		manifest, _ := pluginRegistry.readRuntimeCPluginManifest(plugin.InstallPath)
		for _, path := range pluginRegistry.mcpConfigPaths(plugin, manifest) {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var cfg map[string]any
			if json.Unmarshal(raw, &cfg) != nil {
				continue
			}
			servers, ok := nestedRuntimeMcpMap(cfg, "mcpServers")
			if !ok {
				continue
			}
			out = append(out, runtimeMcpSummaries(servers, pluginRegistry.sourceLabel(plugin.Name), policy)...)
		}
	}
	return out
}

func nestedRuntimeMcpMap(cfg map[string]any, path string) (map[string]any, bool) {
	current := cfg
	parts := strings.Split(path, ".")
	for index, part := range parts {
		value, exists := current[part]
		if !exists {
			return nil, false
		}
		mapped, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		if index == len(parts)-1 {
			return mapped, true
		}
		current = mapped
	}
	return nil, false
}

func runtimeMcpTransport(entry map[string]any) string {
	kind, _ := entry["type"].(string)
	switch strings.ToLower(kind) {
	case "local", "stdio":
		return "stdio"
	case "remote", "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	}
	if _, ok := entry["command"]; ok {
		return "stdio"
	}
	if _, ok := entry["url"]; ok {
		return "http"
	}
	return "unknown"
}
