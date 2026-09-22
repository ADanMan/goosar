// Пакет perimeterpolicy реализует серверные allowlist-политики закрытого контура:
// какие MCP-хосты и команды допустимы в mcp_config и какие провайдеры могут
// стоять за runtime. Без переменных политики поведение не меняется.
package perimeterpolicy

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

const (
	EnvMCPAllowedHosts = "GOOSAR_MCP_ALLOWED_HOSTS"

	EnvMCPAllowedCommands = "GOOSAR_MCP_ALLOWED_COMMANDS"
)

var perimeterDefaultMCPCommands = []string{
	"mcp-atlassian",
	"mcp-server-fetch",
	"mcp-proxy",
	"ewsmcp",
	"mcp-server-b24",
}

var perimeterDefaultMCPHosts = []string{
	"jira.corp.example",
	"wiki.corp.example",
	"mcp-gateway.corp.example",
	"mail.corp.example",
	"b24.corp.example",
	"kb.corp.example",
}

type MCPPolicy struct {
	hosts map[string]struct{}

	commands map[string]struct{}
}

type MCPViolation struct {
	Server string
	Reason string
}

func MCPPolicyFromEnv(profile deliveryprofile.Profile) *MCPPolicy {
	p := ParseMCPPolicy(profile, os.Getenv(EnvMCPAllowedHosts), os.Getenv(EnvMCPAllowedCommands))
	if p != nil && p.hosts != nil {
		if goosarHost := canonicalPolicyHost(os.Getenv("GOOSAR_PUBLIC_URL")); goosarHost != "" {
			p.hosts[goosarHost] = struct{}{}
		}
	}
	return p
}

func ParseMCPPolicy(profile deliveryprofile.Profile, hostsRaw, commandsRaw string) *MCPPolicy {
	var baselineHosts, baselineCommands []string
	if profile.IsPerimeter() {
		baselineHosts = perimeterDefaultMCPHosts
		baselineCommands = perimeterDefaultMCPCommands
	}
	hosts := buildAllowSet(baselineHosts, hostsRaw, canonicalPolicyHost)
	commands := buildAllowSet(baselineCommands, commandsRaw, canonicalPolicyCommand)
	if hosts == nil && commands == nil {
		return nil
	}
	return &MCPPolicy{hosts: hosts, commands: commands}
}

func buildAllowSet(baseline []string, envRaw string, canon func(string) string) map[string]struct{} {
	entries := make([]string, 0, len(baseline)+4)
	entries = append(entries, baseline...)
	entries = append(entries, strings.Split(envRaw, ",")...)
	var set map[string]struct{}
	for _, raw := range entries {
		entry := canon(raw)
		if entry == "" {
			continue
		}
		if set == nil {
			set = make(map[string]struct{})
		}
		set[entry] = struct{}{}
	}
	return set
}

func canonicalPolicyHost(raw string) string {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return ""
	}
	if !strings.Contains(entry, "://") {
		entry = "https://" + entry
	}
	u, err := url.Parse(entry)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

func canonicalPolicyCommand(raw string) string {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return ""
	}
	entry = strings.ReplaceAll(entry, "\\", "/")
	return strings.ToLower(path.Base(entry))
}

func (p *MCPPolicy) hostAllowed(hostname string) bool {
	if p == nil || p.hosts == nil {
		return true
	}
	_, ok := p.hosts[canonicalPolicyHost(hostname)]
	return ok
}

func (p *MCPPolicy) commandAllowed(command string) bool {
	if p == nil || p.commands == nil {
		return true
	}
	_, ok := p.commands[canonicalPolicyCommand(command)]
	return ok
}

type mcpPolicyEntry struct {
	Command     json.RawMessage   `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Environment map[string]string `json:"environment"`
	URL         string            `json:"url"`
}

func policyCommandSubject(raw json.RawMessage) (subject string, extraArgs []string, ok bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s), nil, true
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return "", nil, true
		}
		return strings.TrimSpace(arr[0]), arr[1:], true
	}
	return "", nil, false
}

const urlSchemeSeparator = "://"

func isSchemeByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
		b == '+' || b == '.' || b == '-'
}

func isLoopbackPolicyHost(host string) bool {
	return host == "localhost" || host == "::1" || host == "127.0.0.1" || strings.HasPrefix(host, "127.")
}

func (p *MCPPolicy) urlScanViolation(name, field, value string) *MCPViolation {
	if p == nil || p.hosts == nil {
		return nil
	}
	from := 0
	for {
		idx := strings.Index(value[from:], urlSchemeSeparator)
		if idx < 0 {
			break
		}
		sep := from + idx

		start := sep
		for start > 0 && isSchemeByte(value[start-1]) {
			start--
		}
		from = sep + len(urlSchemeSeparator)
		if start == sep {
			continue
		}

		candidate := value[start:]

		if end := strings.IndexAny(candidate, " \t\r\n\"'`,;|"); end >= 0 {
			candidate = candidate[:end]
		}
		host := canonicalPolicyHost(candidate)
		if host == "" {

			return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: %s contain a URL whose host could not be determined for the MCP host allowlist check (%s)", name, field, EnvMCPAllowedHosts)}
		}
		if !isLoopbackPolicyHost(host) && !p.hostAllowed(host) {
			return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: %s reference host %q which is not in this deployment's MCP host allowlist (%s)", name, field, host, EnvMCPAllowedHosts)}
		}
	}
	return nil
}

func (p *MCPPolicy) checkEntry(name string, raw json.RawMessage) *MCPViolation {
	if p == nil || (p.hosts == nil && p.commands == nil) {
		return nil
	}
	var entry mcpPolicyEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q could not be parsed for the MCP policy check", name)}
	}
	command, extraArgs, ok := policyCommandSubject(entry.Command)
	if !ok {
		return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: command could not be parsed for the MCP policy check (%s)", name, EnvMCPAllowedCommands)}
	}
	if command != "" && p.commands != nil {

		if strings.ContainsAny(command, `/\`) {
			return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: command must be a bare executable name (no path separators) under this deployment's MCP command allowlist (%s)", name, EnvMCPAllowedCommands)}
		}
		if !p.commandAllowed(command) {
			return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: command %q is not in this deployment's MCP command allowlist (%s)", name, canonicalPolicyCommand(command), EnvMCPAllowedCommands)}
		}
	}
	if rawURL := strings.TrimSpace(entry.URL); rawURL != "" {
		host := canonicalPolicyHost(rawURL)
		if host == "" && p.hosts != nil {
			return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: url host could not be determined for the MCP host allowlist check (%s)", name, EnvMCPAllowedHosts)}
		}
		if !p.hostAllowed(host) {
			return &MCPViolation{Server: name, Reason: fmt.Sprintf("mcp_config server %q: host %q is not in this deployment's MCP host allowlist (%s)", name, host, EnvMCPAllowedHosts)}
		}
	}
	for _, arg := range append(append([]string{}, extraArgs...), entry.Args...) {
		if v := p.urlScanViolation(name, "args", arg); v != nil {
			return v
		}
	}
	for _, env := range []map[string]string{entry.Env, entry.Environment} {
		for _, value := range env {
			if v := p.urlScanViolation(name, "env", value); v != nil {
				return v
			}
		}
	}
	return nil
}

var mcpEnvelopeKeys = []string{"mcpServers", "mcp"}

func mcpServerMaps(raw json.RawMessage) (top map[string]json.RawMessage, serversByKey map[string]map[string]json.RawMessage, ok bool) {
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil, false
	}
	serversByKey = make(map[string]map[string]json.RawMessage, len(mcpEnvelopeKeys))
	for _, key := range mcpEnvelopeKeys {
		serversRaw, has := top[key]
		if !has || strings.TrimSpace(string(serversRaw)) == "null" {
			continue
		}
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(serversRaw, &servers); err != nil {
			return nil, nil, false
		}
		serversByKey[key] = servers
	}
	return top, serversByKey, true
}

func (p *MCPPolicy) ValidateConfig(raw json.RawMessage) error {
	if p == nil || len(raw) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	_, serversByKey, ok := mcpServerMaps(raw)
	if !ok {
		return fmt.Errorf("mcp_config could not be parsed for the MCP policy check (%s / %s are set)", EnvMCPAllowedHosts, EnvMCPAllowedCommands)
	}
	var reasons []string
	for _, key := range mcpEnvelopeKeys {
		servers := serversByKey[key]
		for _, name := range sortedKeys(servers) {
			if v := p.checkEntry(name, servers[name]); v != nil {
				reasons = append(reasons, v.Reason)
			}
		}
	}
	if len(reasons) > 0 {
		return fmt.Errorf("%s", strings.Join(reasons, "; "))
	}
	return nil
}

func (p *MCPPolicy) FilterConfig(raw json.RawMessage) (json.RawMessage, []MCPViolation) {
	if p == nil || len(raw) == 0 {
		return raw, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return raw, nil
	}
	top, serversByKey, ok := mcpServerMaps(raw)
	if !ok {
		return nil, []MCPViolation{{Reason: fmt.Sprintf("mcp_config could not be parsed for the MCP policy check; dropping it from dispatch (%s / %s)", EnvMCPAllowedHosts, EnvMCPAllowedCommands)}}
	}
	var violations []MCPViolation
	keptByKey := make(map[string]map[string]json.RawMessage, len(serversByKey))
	for _, key := range mcpEnvelopeKeys {
		servers, has := serversByKey[key]
		if !has {
			continue
		}
		kept := make(map[string]json.RawMessage, len(servers))
		for _, name := range sortedKeys(servers) {
			if v := p.checkEntry(name, servers[name]); v != nil {
				violations = append(violations, *v)
				continue
			}
			kept[name] = servers[name]
		}
		keptByKey[key] = kept
	}
	if len(violations) == 0 {
		return raw, nil
	}
	failClosed := func() (json.RawMessage, []MCPViolation) {

		return nil, append(violations, MCPViolation{Reason: "mcp_config could not be re-serialized after policy filtering; dropping it from dispatch"})
	}
	for key, kept := range keptByKey {
		serversBytes, err := json.Marshal(kept)
		if err != nil {
			return failClosed()
		}
		top[key] = serversBytes
	}
	out, err := json.Marshal(top)
	if err != nil {
		return failClosed()
	}
	return out, violations
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (p *MCPPolicy) Lists() (hosts []string, hostsRestricted bool, commands []string, commandsRestricted bool) {
	if p == nil {
		return nil, false, nil, false
	}
	return setToSortedSlice(p.hosts), p.hosts != nil, setToSortedSlice(p.commands), p.commands != nil
}

func NewMCPPolicy(hosts []string, hostsRestricted bool, commands []string, commandsRestricted bool) *MCPPolicy {
	if !hostsRestricted && !commandsRestricted {
		return nil
	}
	p := &MCPPolicy{}
	if hostsRestricted {
		p.hosts = canonicalizeSet(hosts, canonicalPolicyHost)
	}
	if commandsRestricted {
		p.commands = canonicalizeSet(commands, canonicalPolicyCommand)
	}
	return p
}

func (p *MCPPolicy) CheckEntry(name string, raw json.RawMessage) *MCPViolation {
	return p.checkEntry(name, raw)
}

func (p *MCPPolicy) Restricted() bool {
	return p != nil && (p.hosts != nil || p.commands != nil)
}

func canonicalizeSet(entries []string, canon func(string) string) map[string]struct{} {
	set := make(map[string]struct{}, len(entries))
	for _, raw := range entries {
		if entry := canon(raw); entry != "" {
			set[entry] = struct{}{}
		}
	}
	return set
}

func setToSortedSlice(set map[string]struct{}) []string {
	if set == nil {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
