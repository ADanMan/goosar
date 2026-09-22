package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const openclawConfigFile = "openclaw-config.json"

const openclawMcpResetFile = "openclaw-mcp-reset.json"

const openclawMcpResetBody = "{\n  \"mcp\": {\n    \"servers\": null\n  }\n}\n"

const openclawCLITimeout = 5 * time.Second

type OpenclawConfigPrep struct {
	OpenclawBin string

	Timeout time.Duration

	McpConfig json.RawMessage

	Gateway OpenclawGatewayPin
}

type OpenclawGatewayPin struct {
	Host  string `json:"host,omitempty"`
	Port  int    `json:"port,omitempty"`
	Token string `json:"token,omitempty"`
	TLS   bool   `json:"tls,omitempty"`
}

func (p OpenclawGatewayPin) IsZero() bool {
	return p == OpenclawGatewayPin{}
}

func (p OpenclawGatewayPin) String() string {
	tok := ""
	if p.Token != "" {
		tok = "***"
	}
	return fmt.Sprintf("OpenclawGatewayPin{Host:%q Port:%d Token:%s TLS:%t}", p.Host, p.Port, tok, p.TLS)
}

func (p OpenclawGatewayPin) MarshalJSON() ([]byte, error) {
	type alias struct {
		Host  string `json:"host,omitempty"`
		Port  int    `json:"port,omitempty"`
		Token string `json:"token,omitempty"`
		TLS   bool   `json:"tls,omitempty"`
	}
	masked := alias{Host: p.Host, Port: p.Port, TLS: p.TLS}
	if p.Token != "" {
		masked.Token = "***"
	}
	return json.Marshal(masked)
}

type OpenclawConfigResult struct {
	ConfigPath  string
	IncludeRoot string
}

func prepareOpenclawConfig(envRoot, workDir string, opts OpenclawConfigPrep) (OpenclawConfigResult, error) {
	bin := opts.OpenclawBin
	if bin == "" {
		bin = runtimeCLIName(runtimeCodeN)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = openclawCLITimeout
	}

	activePath, exists, err := openclawActiveConfigPath(bin, timeout)
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("locate openclaw active config: %w", err)
	}

	var resolvedList []any
	var agentsFromRegistry bool
	if exists {
		resolvedList, agentsFromRegistry, err = openclawResolvedAgentsList(bin, timeout)
		if err != nil {
			return OpenclawConfigResult{}, fmt.Errorf("read openclaw agents.list: %w", err)
		}
	}

	managedMcp, hasManagedMcp, err := openclawManagedMcpServers(opts.McpConfig)
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("render openclaw mcp_config: %w", err)
	}

	resetPath := ""
	if hasManagedMcp && exists {
		resetPath = filepath.Join(envRoot, openclawMcpResetFile)
		if werr := os.WriteFile(resetPath, []byte(openclawMcpResetBody), 0o600); werr != nil {
			return OpenclawConfigResult{}, fmt.Errorf("write openclaw mcp reset: %w", werr)
		}
	}

	конфиг := buildPerTaskOpenclawConfig(activePath, exists, resetPath, resolvedList, agentsFromRegistry, workDir, managedMcp, hasManagedMcp, opts.Gateway)

	data, err := json.MarshalIndent(конфиг, "", "  ")
	if err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("marshal openclaw config: %w", err)
	}
	outPath := filepath.Join(envRoot, openclawConfigFile)

	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return OpenclawConfigResult{}, fmt.Errorf("write openclaw config: %w", err)
	}
	result := OpenclawConfigResult{ConfigPath: outPath}
	if exists {

		result.IncludeRoot = filepath.Dir(activePath)
	}
	return result, nil
}

func buildPerTaskOpenclawConfig(activePath string, exists bool, resetPath string, resolvedList []any, agentsFromRegistry bool, workDir string, managedMcp map[string]any, hasManagedMcp bool, gateway OpenclawGatewayPin) map[string]any {
	agents := map[string]any{
		"defaults": map[string]any{"workspace": workDir},
	}

	if !agentsFromRegistry {
		if rewritten := rewriteAgentsListWorkspaces(resolvedList, workDir); rewritten != nil {
			agents["list"] = rewritten
		}
	}
	конфиг := map[string]any{
		"agents": agents,
	}
	if hasManagedMcp {

		servers := managedMcp
		if servers == nil {
			servers = map[string]any{}
		}
		конфиг["mcp"] = map[string]any{"servers": servers}
	}

	if gw := buildGatewayOverride(gateway); gw != nil {
		конфиг["gateway"] = gw
	}
	if exists {

		includes := []any{activePath}
		if resetPath != "" {
			includes = append(includes, resetPath)
		}
		конфиг["$include"] = includes
	}
	return конфиг
}

func buildGatewayOverride(p OpenclawGatewayPin) map[string]any {
	if p.IsZero() {
		return nil
	}
	out := map[string]any{}
	if p.Host != "" {
		out["host"] = p.Host
	}
	if p.Port != 0 {
		out["port"] = p.Port
	}
	if p.TLS {
		out["tls"] = true
	}
	if p.Token != "" {
		out["auth"] = map[string]any{
			"mode":  "token",
			"token": p.Token,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func rewriteAgentsListWorkspaces(list []any, workDir string) []any {
	if len(list) == 0 {
		return nil
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {

			continue
		}
		copyEntry := make(map[string]any, len(entry)+1)
		for k, v := range entry {
			copyEntry[k] = v
		}
		copyEntry["workspace"] = workDir
		out = append(out, copyEntry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func openclawActiveConfigPath(bin string, timeout time.Duration) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "file")
	if err != nil {
		if isOpenclawConfigFileUnsupported(err) {
			path, exists, ferr := openclawFallbackActiveConfigPath()
			if ferr != nil {
				return "", false, fmt.Errorf("fallback after unsupported `openclaw config file` (%v): %w", err, ferr)
			}
			return path, exists, nil
		}
		return "", false, err
	}
	return openclawParseActiveConfigPath(out)
}

func openclawParseActiveConfigPath(out string) (string, bool, error) {

	lines := strings.Split(strings.TrimSpace(out), "\n")
	path := ""
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			path = trimmed
			break
		}
	}
	if path == "" {
		return "", false, fmt.Errorf("`openclaw config file` returned empty output")
	}
	var err error
	path, err = expandOpenclawPath(path)
	if err != nil {
		return "", false, err
	}
	return openclawStatConfigPath(path)
}

func openclawFallbackActiveConfigPath() (string, bool, error) {
	if explicitPath := strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")); explicitPath != "" {
		path, err := expandOpenclawPath(explicitPath)
		if err != nil {
			return "", false, err
		}
		return openclawStatConfigPath(path)
	}

	candidates, canonicalPath, err := openclawFallbackConfigCandidates()
	if err != nil {
		return "", false, err
	}
	for _, candidate := range candidates {
		path, err := expandOpenclawPath(candidate)
		if err != nil {
			return "", false, err
		}
		exists, err := openclawConfigPathExists(path)
		if err != nil {
			return "", false, err
		}
		if exists {
			return path, true, nil
		}
	}
	return openclawStatConfigPath(canonicalPath)
}

var openclawFallbackConfigFileNames = []string{
	"openclaw.json",
	"clawdbot.json",
	"moltbot.json",
	"moldbot.json",
}

var openclawFallbackConfigDirNames = []string{
	".openclaw",
	".clawdbot",
	".moltbot",
	".moldbot",
}

func openclawFallbackConfigCandidates() ([]string, string, error) {
	candidates := make([]string, 0, 1+2*len(openclawFallbackConfigFileNames)+len(openclawFallbackConfigDirNames)*len(openclawFallbackConfigFileNames))
	for _, env := range []string{"CLAWDBOT_CONFIG_PATH"} {
		if path := strings.TrimSpace(os.Getenv(env)); path != "" {
			candidates = append(candidates, path)
		}
	}

	for _, env := range []string{"OPENCLAW_STATE_DIR", "CLAWDBOT_STATE_DIR"} {
		if dir := strings.TrimSpace(os.Getenv(env)); dir != "" {
			candidates = appendOpenclawConfigFileCandidates(candidates, dir)
		}
	}

	home := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	var err error
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("resolve openclaw home: %w", err)
		}
	} else {
		home, err = expandOpenclawPath(home)
		if err != nil {
			return nil, "", fmt.Errorf("resolve OPENCLAW_HOME: %w", err)
		}
	}

	for _, dirName := range openclawFallbackConfigDirNames {
		candidates = appendOpenclawConfigFileCandidates(candidates, filepath.Join(home, dirName))
	}
	return candidates, filepath.Join(home, ".openclaw", "openclaw.json"), nil
}

func appendOpenclawConfigFileCandidates(candidates []string, dir string) []string {
	for _, name := range openclawFallbackConfigFileNames {
		candidates = append(candidates, filepath.Join(dir, name))
	}
	return candidates
}

func expandOpenclawPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("expand `~` in openclaw config path: %w", herr)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve openclaw config path %q: %w", path, err)
		}
		path = abs
	}
	return path, nil
}

func openclawStatConfigPath(path string) (string, bool, error) {
	if !filepath.IsAbs(path) {
		return "", false, fmt.Errorf("openclaw reported non-absolute config path %q", path)
	}
	exists, err := openclawConfigPathExists(path)
	if err != nil {
		return "", false, err
	}
	return path, exists, nil
}

func openclawConfigPathExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat openclaw config %s: %w", path, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("openclaw config path %s is a directory, not a file", path)
	}
	return true, nil
}

func isOpenclawConfigFileUnsupported(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many arguments for 'config'") ||
		strings.Contains(msg, "expected 0 arguments but got 1") ||
		(strings.Contains(msg, "unknown") && strings.Contains(msg, "config") && strings.Contains(msg, "file"))
}

func openclawResolvedAgentsList(bin string, timeout time.Duration) ([]any, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "get", "agents.list", "--json")
	if err != nil {
		if isOpenclawKeyMissing(err) {

			list, rerr := openclawRegistryAgentsList(bin, timeout)
			return list, true, rerr
		}
		return nil, false, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "null" {
		return nil, false, nil
	}
	var list []any
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil, false, fmt.Errorf("parse `openclaw config get agents.list --json` output: %w", err)
	}
	return list, false, nil
}

func openclawRegistryAgentsList(bin string, timeout time.Duration) ([]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "agents", "list", "--json")
	if err != nil {

		if isOpenclawKeyMissing(err) || isOpenclawUnknownSubcommand(err) {
			return nil, nil
		}
		return nil, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var list []any
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil, fmt.Errorf("parse `openclaw agents list --json` output: %w", err)
	}
	return list, nil
}

var openclawExec = execOpenclawCLI

func execOpenclawCLI(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		stderrMsg := strings.TrimSpace(stderr.String())
		if stderrMsg != "" {
			return "", fmt.Errorf("openclaw %s: %w (stderr: %s)", strings.Join(args, " "), err, stderrMsg)
		}
		return "", fmt.Errorf("openclaw %s: %w", strings.Join(args, " "), err)
	}
	return string(raw), nil
}

func openclawManagedMcpServers(raw json.RawMessage) (map[string]any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, false, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		return map[string]any{}, true, nil
	}
	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]any, len(names))
	for _, name := range names {
		var entry map[string]any
		if err := json.Unmarshal(parsed.McpServers[name], &entry); err != nil {
			return nil, false, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
		if entry == nil {
			return nil, false, fmt.Errorf("mcp_servers.%s must be a JSON object", name)
		}
		command, _ := entry["command"].(string)
		url, _ := entry["url"].(string)
		if strings.TrimSpace(command) == "" && strings.TrimSpace(url) == "" {
			return nil, false, fmt.Errorf("mcp_servers.%s must declare either `command` (stdio) or `url` (http/sse)", name)
		}
		out[name] = entry
	}
	return out, true, nil
}

func isOpenclawKeyMissing(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no value at ") ||
		strings.Contains(msg, "not set") ||
		strings.Contains(msg, "missing key") ||
		strings.Contains(msg, "path not found")
}

func isOpenclawUnknownSubcommand(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown option") ||
		strings.Contains(msg, "does not recognize") ||
		strings.Contains(msg, "unknown argument")
}
