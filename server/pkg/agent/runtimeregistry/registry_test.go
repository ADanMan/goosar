package runtimeregistry

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultHasAllSeventeenCodes(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	codes := reg.Codes()
	if len(codes) != 17 {
		t.Fatalf("got %d codes, want 17: %v", len(codes), codes)
	}
	for _, c := range codes {
		d, ok := reg.ByCode(c)
		if !ok || d.CLIName == "" {
			t.Fatalf("code %s missing cliName", c)
		}
		for _, m := range d.Models {
			wantPrefix := "rt-" + c[len("runtime-"):] + "/"
			if len(m.ID) < len(wantPrefix) || m.ID[:len(wantPrefix)] != wantPrefix {
				t.Fatalf("model %s under %s has wrong neutral-id prefix, want %s", m.ID, c, wantPrefix)
			}
		}
	}
}

func TestDisplayNamesAreGeneric(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	for _, c := range reg.Codes() {
		d, ok := reg.ByCode(c)
		if !ok {
			t.Fatalf("code %s not found after Codes() listed it", c)
		}
		if strings.Contains(strings.ToLower(d.DisplayName), strings.ToLower(d.CLIName)) {
			t.Fatalf("displayName %q for code %s leaks real CLI name %q", d.DisplayName, c, d.CLIName)
		}
	}
}

func TestByCLIName(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	d, ok := reg.ByCLIName("claude")
	if !ok {
		t.Fatalf("ByCLIName(claude): not found")
	}
	if d.Code != "runtime-c" {
		t.Fatalf("ByCLIName(claude).Code = %q, want runtime-c", d.Code)
	}

	if _, ok := reg.ByCLIName("does-not-exist"); ok {
		t.Fatalf("ByCLIName(does-not-exist): expected not found")
	}
}

func TestCLINamesAreExecAccurate(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	want := map[string]string{
		"runtime-a": "agy",
		"runtime-g": "cursor-agent",
		"runtime-l": "kiro-cli",
		"runtime-p": "qodercli",
	}
	for code, cliName := range want {
		d, ok := reg.ByCode(code)
		if !ok {
			t.Fatalf("ByCode(%s): not found", code)
		}
		if d.CLIName != cliName {
			t.Errorf("ByCode(%s).CLIName = %q, want %q", code, d.CLIName, cliName)
		}
	}
}

func TestCLINamesAreDistinct(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	seen := map[string]string{}
	for _, code := range reg.Codes() {
		d, _ := reg.ByCode(code)
		if prev, dup := seen[d.CLIName]; dup {
			t.Fatalf("cliName %q is shared by %s and %s", d.CLIName, prev, code)
		}
		seen[d.CLIName] = code
	}
}

func TestEveryCodeHasASkillsRoot(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	empty := func(string) string { return "" }
	for _, code := range reg.Codes() {
		d, _ := reg.ByCode(code)
		root, ok := d.SkillsRoot("/home/u", empty)
		if !ok || root == "" {
			t.Fatalf("code %s resolves no skills root", code)
		}
		if !strings.HasPrefix(root, "/home/u/") {
			t.Fatalf("code %s skills root %q escapes the home dir with no env override", code, root)
		}
	}

	codex, _ := reg.ByCode("runtime-e")
	if got, _ := codex.SkillsRoot("/home/u", empty); got != filepath.Join("/home/u", ".codex", "skills") {
		t.Errorf("runtime-e default skills root = %q", got)
	}
	overridden := func(name string) string {
		if name == "CODEX_HOME" {
			return "/elsewhere/cx"
		}
		return ""
	}
	if got, _ := codex.SkillsRoot("/home/u", overridden); got != filepath.Join("/elsewhere/cx", "skills") {
		t.Errorf("runtime-e skills root with home env override = %q", got)
	}
}

func TestMCPConfigRoundTrip(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	empty := func(string) string { return "" }

	claudeLike, _ := reg.ByCode("runtime-c")
	if claudeLike.MCPConfig == nil {
		t.Fatalf("runtime-c has no mcpConfig")
	}
	if claudeLike.MCPConfig.Format != "json" || claudeLike.MCPConfig.Key != "mcpServers" {
		t.Errorf("runtime-c mcpConfig = %+v", *claudeLike.MCPConfig)
	}
	if got, ok := claudeLike.MCPConfigPath("/home/u", empty); !ok || got != filepath.Join("/home/u", ".claude.json") {
		t.Errorf("runtime-c mcp path = %q (ok=%v)", got, ok)
	}

	xdgLike, _ := reg.ByCode("runtime-m")
	if got, _ := xdgLike.MCPConfigPath("/home/u", empty); got != filepath.Join("/home/u", ".config", "opencode", "opencode.json") {
		t.Errorf("runtime-m default mcp path = %q", got)
	}
	withXDG := func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return "/xdg"
		}
		return ""
	}
	if got, _ := xdgLike.MCPConfigPath("/home/u", withXDG); got != filepath.Join("/xdg", "opencode", "opencode.json") {
		t.Errorf("runtime-m mcp path with XDG override = %q", got)
	}

	fileEnvLike, _ := reg.ByCode("runtime-n")
	withFile := func(name string) string {
		if name == "CLAWDBOT_CONFIG_PATH" {
			return "/tmp/pinned.json"
		}
		return ""
	}
	if got, _ := fileEnvLike.MCPConfigPath("/home/u", withFile); got != "/tmp/pinned.json" {
		t.Errorf("runtime-n mcp path with file env override = %q", got)
	}

	var covered int
	for _, code := range reg.Codes() {
		if d, _ := reg.ByCode(code); d.MCPConfig != nil {
			covered++
		}
	}
	if covered != 6 {
		t.Errorf("%d runtimes carry an mcpConfig, want 6", covered)
	}
}

func TestPluginRegistryIsSingleRuntime(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	owners := make([]string, 0, 1)
	for _, code := range reg.Codes() {
		if d, _ := reg.ByCode(code); d.PluginRegistry != nil {
			owners = append(owners, code)
		}
	}
	if len(owners) != 1 || owners[0] != "runtime-c" {
		t.Fatalf("plugin registry owners = %v, want [runtime-c]", owners)
	}
	spec, _ := reg.ByCode("runtime-c")
	if spec.PluginRegistry.ExtendsToCode != "runtime-d" {
		t.Errorf("extendsToCode = %q, want runtime-d", spec.PluginRegistry.ExtendsToCode)
	}
	for _, field := range []string{
		spec.PluginRegistry.SettingsSubpath,
		spec.PluginRegistry.SettingsEnabledKey,
		spec.PluginRegistry.InstalledRegistrySubpath,
		spec.PluginRegistry.ManifestSubpath,
		spec.PluginRegistry.DefaultSkillsSubpath,
		spec.PluginRegistry.DefaultMCPConfigSubpath,
	} {
		if field == "" {
			t.Fatalf("plugin registry spec has an empty required field: %+v", *spec.PluginRegistry)
		}
	}
}

func TestSkillsCanDisable(t *testing.T) {
	reg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	var enabled []string
	for _, code := range reg.Codes() {
		if d, _ := reg.ByCode(code); d.SkillsCanDisable {
			enabled = append(enabled, code)
		}
	}
	if len(enabled) != 2 || enabled[0] != "runtime-c" || enabled[1] != "runtime-e" {
		t.Fatalf("skillsCanDisable runtimes = %v, want [runtime-c runtime-e]", enabled)
	}
}
