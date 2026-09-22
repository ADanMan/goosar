package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const CodexDarwinNetworkAccessFixedVersion = ""

type codexSandboxPolicy struct {
	Mode string

	NetworkAccess bool

	WritableRoots []string

	Reason string

	Hint string
}

func resolveGOOS(goos string) string {
	if goos == "" {
		return runtime.GOOS
	}
	return goos
}

func codexSandboxPolicyFor(goos, detectedVersion string) codexSandboxPolicy {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return codexSandboxPolicy{
			Mode:   "danger-full-access",
			Reason: "codex on windows: compatibility fallback; no native windows.sandbox configured, so workspace-write cannot be enforced (MUL-4957)",
		}
	}
	if goos != "darwin" {
		return codexSandboxPolicy{
			Mode:          "workspace-write",
			NetworkAccess: true,
			Reason:        "non-darwin platform — seatbelt bug does not apply",
		}
	}
	if codexDarwinNetworkAccessFixed(detectedVersion) {
		return codexSandboxPolicy{
			Mode:          "workspace-write",
			NetworkAccess: true,
			Reason:        "codex version includes macOS network_access fix",
		}
	}
	reason := "codex on macOS: seatbelt ignores sandbox_workspace_write.network_access (openai/codex#10390)"
	if detectedVersion == "" {
		reason += " — version unknown, assuming broken"
	}
	return codexSandboxPolicy{
		Mode:          "danger-full-access",
		NetworkAccess: false,
		Reason:        reason,
		Hint:          codexUpgradeHint(),
	}
}

type windowsSandboxConfig int

const (
	windowsSandboxAbsent windowsSandboxConfig = iota

	windowsSandboxNative

	windowsSandboxUndecidable
)

func codexSandboxPolicyForConfig(goos, detectedVersion string, winState windowsSandboxConfig) codexSandboxPolicy {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return codexSandboxPolicyForWindows(winState)
	}
	return codexSandboxPolicyFor(goos, detectedVersion)
}

func codexSandboxPolicyForWindows(state windowsSandboxConfig) codexSandboxPolicy {
	switch state {
	case windowsSandboxNative:
		return codexSandboxPolicy{
			Mode:          "workspace-write",
			NetworkAccess: true,
			Reason:        "codex on windows: native windows.sandbox configured; keeping workspace-write so Codex enforces task isolation",
		}
	case windowsSandboxUndecidable:
		return codexSandboxPolicy{
			Mode:          "workspace-write",
			NetworkAccess: true,
			Reason:        "codex on windows: windows.sandbox config undecidable (unreadable/unparseable/invalid); failing closed to workspace-write rather than loosening (MUL-4957)",
		}
	default:
		return codexSandboxPolicy{
			Mode:   "danger-full-access",
			Reason: "codex on windows: compatibility fallback; no native windows.sandbox configured (MUL-4957)",
		}
	}
}

func windowsSandboxFromConfig(config string) windowsSandboxConfig {
	var probe struct {
		Windows struct {
			Sandbox string `toml:"sandbox"`
		} `toml:"windows"`
	}
	if err := toml.Unmarshal([]byte(config), &probe); err != nil {
		return windowsSandboxUndecidable
	}
	return classifyWindowsSandboxValue(probe.Windows.Sandbox)
}

var codexWindowsSandboxOverrideRe = regexp.MustCompile(`^\s*windows\s*\.\s*sandbox\s*=`)

func windowsSandboxFromCustomArgs(args []string) windowsSandboxConfig {
	state := windowsSandboxAbsent
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag := arg
		value := ""
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			value = arg[idx+1:]
			hasInlineValue = true
		}
		if flag != "-c" && flag != "--config" {
			continue
		}
		if !hasInlineValue {
			if i+1 >= len(args) {
				continue
			}
			i++
			value = args[i]
		}
		if !codexWindowsSandboxOverrideRe.MatchString(value) {
			continue
		}

		if eq := strings.Index(value, "="); eq >= 0 {
			state = classifyWindowsSandboxValue(value[eq+1:])
		}
	}
	return state
}

func classifyWindowsSandboxValue(raw string) windowsSandboxConfig {
	v := strings.TrimSpace(raw)
	v = strings.Trim(v, `"'`)
	v = strings.TrimSpace(v)
	switch v {
	case "":
		return windowsSandboxAbsent
	case "unelevated", "elevated":
		return windowsSandboxNative
	default:
		return windowsSandboxUndecidable
	}
}

func resolveWindowsSandbox(states ...windowsSandboxConfig) windowsSandboxConfig {
	result := windowsSandboxAbsent
	for _, s := range states {
		if s == windowsSandboxUndecidable {
			return windowsSandboxUndecidable
		}
		if s == windowsSandboxNative {
			result = windowsSandboxNative
		}
	}
	return result
}

func codexDarwinNetworkAccessFixed(detectedVersion string) bool {
	if CodexDarwinNetworkAccessFixedVersion == "" || detectedVersion == "" {
		return false
	}
	fixed, err := parseCodexSemver(CodexDarwinNetworkAccessFixedVersion)
	if err != nil {
		return false
	}
	got, err := parseCodexSemver(detectedVersion)
	if err != nil {
		return false
	}
	return !got.lessThan(fixed)
}

func codexUpgradeHint() string {
	return "upgrade Codex CLI (e.g. `brew upgrade codex` or `npm i -g @openai/codex`) once a release including openai/codex#10390 is available to restore workspace-write + network_access"
}

const (
	goosarManagedBeginMarker = "# BEGIN goosar-managed (do not edit; regenerated by daemon)"
	goosarManagedEndMarker   = "# END goosar-managed"
)

func renderGoosarManagedBlock(policy codexSandboxPolicy) string {
	var b strings.Builder
	b.WriteString(goosarManagedBeginMarker)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("sandbox_mode = %q\n", policy.Mode))
	if policy.Mode == "workspace-write" {
		b.WriteString(fmt.Sprintf("sandbox_workspace_write.network_access = %t\n", policy.NetworkAccess))
		if len(policy.WritableRoots) > 0 {
			b.WriteString("sandbox_workspace_write.writable_roots = ")
			b.WriteString(renderTomlStringArray(policy.WritableRoots))
			b.WriteString("\n")
		}
	}
	b.WriteString(goosarManagedEndMarker)
	b.WriteString("\n")
	return b.String()
}

func renderTomlStringArray(vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

var managedBlockRe = regexp.MustCompile(
	`(?ms)^` + regexp.QuoteMeta(goosarManagedBeginMarker) +
		`.*?^` + regexp.QuoteMeta(goosarManagedEndMarker) + `\n*`)

func upsertGoosarManagedBlock(content string, policy codexSandboxPolicy) string {

	content = managedBlockRe.ReplaceAllString(content, "")
	block := renderGoosarManagedBlock(policy)

	content = strings.TrimLeft(content, "\n")
	if content == "" {
		return block
	}
	return block + "\n" + content
}

func stripLegacySandboxDirectives(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inLegacyWorkspaceWrite := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {

			inLegacyWorkspaceWrite = trimmed == "[sandbox_workspace_write]"
			if inLegacyWorkspaceWrite {
				continue
			}
			out = append(out, line)
			continue
		}
		if inLegacyWorkspaceWrite {

			continue
		}
		if strings.HasPrefix(trimmed, "sandbox_mode") {

			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func ensureCodexSandboxConfig(configPath string, policy codexSandboxPolicy, detectedVersion string, logger *slog.Logger) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)

	if existing != "" && !managedBlockRe.MatchString(existing) {
		existing = stripLegacySandboxDirectives(existing)
	}

	updated := upsertGoosarManagedBlock(existing, policy)
	if updated == string(data) {
		return nil
	}

	if policy.Mode == "danger-full-access" && logger != nil {
		version := detectedVersion
		if version == "" {
			version = "unknown"
		}
		attrs := []any{
			"reason", policy.Reason,
			"codex_version", version,
			"config_path", configPath,
		}
		if policy.Hint != "" {
			attrs = append(attrs, "hint", policy.Hint)
		}
		logger.Warn("codex sandbox: running unsandboxed with danger-full-access", attrs...)
	}

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

type codexSemver struct {
	Major, Minor, Patch int
}

var codexSemverRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

func parseCodexSemver(raw string) (codexSemver, error) {
	m := codexSemverRe.FindStringSubmatch(raw)
	if m == nil {
		return codexSemver{}, fmt.Errorf("cannot parse version %q", raw)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return codexSemver{Major: maj, Minor: min, Patch: pat}, nil
}

func (v codexSemver) lessThan(o codexSemver) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}
