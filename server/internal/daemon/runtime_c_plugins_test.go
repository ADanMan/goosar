package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func writePluginFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func pluginRegistryForTest(t *testing.T, home string) runtimeCPluginRegistry {
	t.Helper()
	reg, ok := resolveRuntimeCPluginRegistry(runtimeCodeC, home, true)
	if !ok {
		t.Fatalf("no plugin registry resolved for %s", runtimeCodeC)
	}
	return reg
}

func writeClaudeHome(t *testing.T, settings, installed string) runtimeCPluginRegistry {
	t.Helper()
	home := t.TempDir()
	reg := pluginRegistryForTest(t, home)
	if settings != "" {
		writePluginFile(t, filepath.Join(reg.home, filepath.FromSlash(reg.spec.SettingsSubpath)), settings)
	}
	if installed != "" {
		writePluginFile(t, filepath.Join(reg.home, filepath.FromSlash(reg.spec.InstalledRegistrySubpath)), installed)
	}
	return reg
}

func writePluginManifest(t *testing.T, reg runtimeCPluginRegistry, installPath, content string) {
	t.Helper()
	writePluginFile(t, filepath.Join(installPath, filepath.FromSlash(reg.spec.ManifestSubpath)), content)
}

func TestListEnabledClaudePlugins(t *testing.T) {
	t.Run("missing settings file", func(t *testing.T) {
		reg := writeClaudeHome(t, "", `{"plugins":{}}`)
		if got := reg.listEnabledRuntimeCPlugins(); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("malformed settings JSON", func(t *testing.T) {
		reg := writeClaudeHome(t, `{not json`, `{"plugins":{}}`)
		if got := reg.listEnabledRuntimeCPlugins(); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("missing installed registry", func(t *testing.T) {
		reg := writeClaudeHome(t, `{"enabledPlugins":{"alpha@m":true}}`, "")
		if got := reg.listEnabledRuntimeCPlugins(); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("malformed installed registry JSON", func(t *testing.T) {
		reg := writeClaudeHome(t, `{"enabledPlugins":{"alpha@m":true}}`, `[broken`)
		if got := reg.listEnabledRuntimeCPlugins(); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("enabled but not installed is skipped", func(t *testing.T) {
		reg := writeClaudeHome(t,
			`{"enabledPlugins":{"ghost@m":true}}`,
			`{"plugins":{}}`)
		if got := reg.listEnabledRuntimeCPlugins(); len(got) != 0 {
			t.Fatalf("expected empty, got %#v", got)
		}
	})

	t.Run("disabled but installed is skipped", func(t *testing.T) {
		reg := writeClaudeHome(t,
			`{"enabledPlugins":{"beta@m":false}}`,
			`{"plugins":{"beta@m":[{"scope":"user","installPath":"`+t.TempDir()+`"}]}}`)
		if got := reg.listEnabledRuntimeCPlugins(); len(got) != 0 {
			t.Fatalf("expected empty, got %#v", got)
		}
	})

	t.Run("user scope preferred over other scopes", func(t *testing.T) {
		userPath := t.TempDir()
		projectPath := t.TempDir()
		reg := writeClaudeHome(t,
			`{"enabledPlugins":{"alpha@m":true}}`,
			`{"plugins":{"alpha@m":[
				{"scope":"user","installPath":"`+userPath+`"},
				{"scope":"project","installPath":"`+projectPath+`"}
			]}}`)
		got := reg.listEnabledRuntimeCPlugins()
		if len(got) != 1 || got[0].InstallPath != userPath {
			t.Fatalf("expected user-scope install %s, got %#v", userPath, got)
		}
		if got[0].ID != "alpha@m" || got[0].Name != "alpha" {
			t.Fatalf("unexpected identity: %#v", got[0])
		}
	})

	t.Run("non-user scope falls back to last install", func(t *testing.T) {
		first := t.TempDir()
		last := t.TempDir()
		reg := writeClaudeHome(t,
			`{"enabledPlugins":{"alpha@m":true}}`,
			`{"plugins":{"alpha@m":[
				{"scope":"project","installPath":"`+first+`"},
				{"scope":"project","installPath":"`+last+`"}
			]}}`)
		got := reg.listEnabledRuntimeCPlugins()
		if len(got) != 1 || got[0].InstallPath != last {
			t.Fatalf("expected last install %s, got %#v", last, got)
		}
	})

	t.Run("empty install path is skipped", func(t *testing.T) {
		reg := writeClaudeHome(t,
			`{"enabledPlugins":{"alpha@m":true}}`,
			`{"plugins":{"alpha@m":[{"scope":"user","installPath":"  "}]}}`)
		if got := reg.listEnabledRuntimeCPlugins(); len(got) != 0 {
			t.Fatalf("expected empty, got %#v", got)
		}
	})

	t.Run("manifest name overrides id prefix, malformed manifest falls back", func(t *testing.T) {
		withManifest := t.TempDir()
		brokenManifest := t.TempDir()
		reg := writeClaudeHome(t,
			`{"enabledPlugins":{"alpha@m":true,"beta@m":true}}`,
			`{"plugins":{
				"alpha@m":[{"scope":"user","installPath":"`+withManifest+`"}],
				"beta@m":[{"scope":"user","installPath":"`+brokenManifest+`"}]
			}}`)
		writePluginManifest(t, reg, withManifest, `{"name":"Pretty Name"}`)
		writePluginManifest(t, reg, brokenManifest, `{broken`)
		got := reg.listEnabledRuntimeCPlugins()
		if len(got) != 2 {
			t.Fatalf("expected 2 plugins, got %#v", got)
		}

		if got[0].Name != "Pretty Name" {
			t.Fatalf("alpha name = %q, want manifest name", got[0].Name)
		}
		if got[1].Name != "beta" {
			t.Fatalf("beta name = %q, want id-prefix fallback", got[1].Name)
		}
	})
}

func TestLoadClaudePluginMcpServerConfigsOnlyEnabledPluginsInjected(t *testing.T) {
	enabledPath := t.TempDir()
	disabledPath := t.TempDir()
	uninstalledEnabled := `"ghost@m":true`

	reg := writeClaudeHome(t,
		`{"enabledPlugins":{"alpha@m":true,"beta@m":false,`+uninstalledEnabled+`}}`,
		`{"plugins":{
			"alpha@m":[{"scope":"user","installPath":"`+enabledPath+`"}],
			"beta@m":[{"scope":"user","installPath":"`+disabledPath+`"}]
		}}`)
	mcpSubpath := filepath.FromSlash(reg.spec.DefaultMCPConfigSubpath)
	writePluginFile(t, filepath.Join(enabledPath, mcpSubpath),
		`{"mcpServers":{"enabled-srv":{"command":"run-enabled"}}}`)
	writePluginFile(t, filepath.Join(disabledPath, mcpSubpath),
		`{"mcpServers":{"disabled-srv":{"command":"run-disabled"}}}`)

	got := loadRuntimeCPluginMcpServerConfigs(reg)
	if len(got) != 1 {
		t.Fatalf("expected exactly one injected server, got %#v", got)
	}
	entry, ok := got["enabled-srv"].(map[string]any)
	if !ok || entry["command"] != "run-enabled" {
		t.Fatalf("unexpected enabled-srv entry: %#v", got["enabled-srv"])
	}
	if _, exists := got["disabled-srv"]; exists {
		t.Fatalf("disabled plugin's MCP server must not be injected: %#v", got)
	}
}

func TestClaudePluginComponentPaths(t *testing.T) {
	install := t.TempDir()

	t.Run("string manifest value plus defaults, deduped", func(t *testing.T) {
		got := runtimeCPluginComponentPaths(install, []byte(`"servers/a.json"`),
			filepath.Join(install, "servers", "a.json"))
		if len(got) != 1 || got[0] != filepath.Join(install, "servers", "a.json") {
			t.Fatalf("unexpected paths: %#v", got)
		}
	})

	t.Run("array manifest value", func(t *testing.T) {
		got := runtimeCPluginComponentPaths(install, []byte(`["one.json","two.json"]`))
		want := []string{filepath.Join(install, "one.json"), filepath.Join(install, "two.json")}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("paths escaping the install dir are rejected", func(t *testing.T) {
		got := runtimeCPluginComponentPaths(install, []byte(`["../outside.json","/etc/passwd"]`))
		if len(got) != 0 {
			t.Fatalf("expected escapes rejected, got %#v", got)
		}
	})

	t.Run("malformed raw yields only defaults", func(t *testing.T) {
		got := runtimeCPluginComponentPaths(install, []byte(`{broken`), filepath.Join(install, "d.json"))
		if len(got) != 1 || got[0] != filepath.Join(install, "d.json") {
			t.Fatalf("unexpected paths: %#v", got)
		}
	})
}

func TestPluginRegistryExtendsForMcpOnly(t *testing.T) {
	home := t.TempDir()
	owner, ok := runtimeDescriptor(runtimeCodeC)
	if !ok || owner.PluginRegistry == nil {
		t.Fatalf("%s has no plugin registry", runtimeCodeC)
	}
	extended := owner.PluginRegistry.ExtendsToCode
	if extended == "" {
		t.Fatal("plugin registry declares no extension")
	}

	if _, ok := resolveRuntimeCPluginRegistry(extended, home, false); !ok {
		t.Fatalf("%s should inherit the plugin registry for MCP", extended)
	}
	if _, ok := resolveRuntimeCPluginRegistry(extended, home, true); ok {
		t.Fatalf("%s must NOT inherit the plugin registry for skills", extended)
	}
	if _, ok := resolveRuntimeCPluginRegistry("runtime-e", home, false); ok {
		t.Fatal("an unrelated runtime must not resolve a plugin registry")
	}
}
