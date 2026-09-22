package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

type runtimeCPluginRegistry struct {
	home      string
	ownerCode string
	spec      runtimeregistry.PluginRegistrySpec
}

func resolveRuntimeCPluginRegistry(provider, osHome string, ownedOnly bool) (runtimeCPluginRegistry, bool) {
	if d, ok := runtimeDescriptor(provider); ok && d.PluginRegistry != nil {
		return runtimeCPluginRegistry{
			home:      d.ResolveHome(osHome, os.Getenv),
			ownerCode: d.Code,
			spec:      *d.PluginRegistry,
		}, true
	}
	if ownedOnly {
		return runtimeCPluginRegistry{}, false
	}
	for _, code := range runtimeRegistry.Codes() {
		owner, ok := runtimeRegistry.ByCode(code)
		if !ok || owner.PluginRegistry == nil {
			continue
		}
		if owner.PluginRegistry.ExtendsToCode != provider {
			continue
		}
		return runtimeCPluginRegistry{
			home:      owner.ResolveHome(osHome, os.Getenv),
			ownerCode: owner.Code,
			spec:      *owner.PluginRegistry,
		}, true
	}
	return runtimeCPluginRegistry{}, false
}

func (r runtimeCPluginRegistry) sourceLabel(pluginName string) string {
	return runtimeDisplayName(r.ownerCode) + " plugin · " + pluginName
}

type runtimeCPluginInstall struct {
	ID          string
	Name        string
	InstallPath string
}

type runtimeCInstalledPluginsFile struct {
	Plugins map[string][]struct {
		Scope       string `json:"scope"`
		InstallPath string `json:"installPath"`
	} `json:"plugins"`
}

type runtimeCPluginManifest struct {
	Name       string          `json:"name"`
	Skills     json.RawMessage `json:"skills"`
	MCPServers json.RawMessage `json:"mcpServers"`
}

func (r runtimeCPluginRegistry) listEnabledRuntimeCPlugins() []runtimeCPluginInstall {
	enabledIDs := r.readEnabledPluginIDs()
	if len(enabledIDs) == 0 {
		return nil
	}

	registry, ok := r.readInstalledPluginsRegistry()
	if !ok {
		return nil
	}

	result := make([]runtimeCPluginInstall, 0, len(enabledIDs))
	for _, id := range enabledIDs {
		install, ok := r.pickPluginInstall(registry, id)
		if !ok {
			continue
		}
		result = append(result, install)
	}
	return result
}

func (r runtimeCPluginRegistry) readEnabledPluginIDs() []string {
	raw, err := os.ReadFile(filepath.Join(r.home, filepath.FromSlash(r.spec.SettingsSubpath)))
	if err != nil {
		return nil
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil
	}
	blob, ok := settings[r.spec.SettingsEnabledKey]
	if !ok {
		return nil
	}
	var enabled map[string]bool
	if err := json.Unmarshal(blob, &enabled); err != nil {
		return nil
	}
	ids := make([]string, 0, len(enabled))
	for id, on := range enabled {
		if on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (r runtimeCPluginRegistry) readInstalledPluginsRegistry() (runtimeCInstalledPluginsFile, bool) {
	raw, err := os.ReadFile(filepath.Join(r.home, filepath.FromSlash(r.spec.InstalledRegistrySubpath)))
	if err != nil {
		return runtimeCInstalledPluginsFile{}, false
	}
	var registry runtimeCInstalledPluginsFile
	if err := json.Unmarshal(raw, &registry); err != nil {
		return runtimeCInstalledPluginsFile{}, false
	}
	return registry, true
}

func (r runtimeCPluginRegistry) pickPluginInstall(registry runtimeCInstalledPluginsFile, id string) (runtimeCPluginInstall, bool) {
	entries := registry.Plugins[id]
	if len(entries) == 0 {
		return runtimeCPluginInstall{}, false
	}

	chosen := entries[len(entries)-1]
	for _, entry := range entries {
		if entry.Scope == "user" {
			chosen = entry
		}
	}

	installPath := strings.TrimSpace(chosen.InstallPath)
	if installPath == "" {
		return runtimeCPluginInstall{}, false
	}

	name := strings.TrimSpace(strings.SplitN(id, "@", 2)[0])
	if manifest, ok := r.readRuntimeCPluginManifest(installPath); ok {
		if manifestName := strings.TrimSpace(manifest.Name); manifestName != "" {
			name = manifestName
		}
	}
	if name == "" {
		return runtimeCPluginInstall{}, false
	}
	return runtimeCPluginInstall{ID: id, Name: name, InstallPath: installPath}, true
}

func (r runtimeCPluginRegistry) readRuntimeCPluginManifest(installPath string) (runtimeCPluginManifest, bool) {
	raw, err := os.ReadFile(filepath.Join(installPath, filepath.FromSlash(r.spec.ManifestSubpath)))
	if err != nil {
		return runtimeCPluginManifest{}, false
	}
	var manifest runtimeCPluginManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return runtimeCPluginManifest{}, false
	}
	return manifest, true
}

func (r runtimeCPluginRegistry) skillRoots(plugin runtimeCPluginInstall, manifest runtimeCPluginManifest) []string {
	return runtimeCPluginComponentPaths(plugin.InstallPath, manifest.Skills,
		filepath.Join(plugin.InstallPath, filepath.FromSlash(r.spec.DefaultSkillsSubpath)))
}

func (r runtimeCPluginRegistry) mcpConfigPaths(plugin runtimeCPluginInstall, manifest runtimeCPluginManifest) []string {
	return runtimeCPluginComponentPaths(plugin.InstallPath, manifest.MCPServers,
		filepath.Join(plugin.InstallPath, filepath.FromSlash(r.spec.DefaultMCPConfigSubpath)))
}

func runtimeCPluginComponentPaths(installPath string, raw json.RawMessage, defaults ...string) []string {
	candidates := append([]string(nil), defaults...)
	candidates = append(candidates, decodeManifestPaths(raw)...)

	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		resolved, ok := resolveComponentPath(installPath, candidate)
		if !ok || seen[resolved] {
			continue
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	return out
}

func decodeManifestPaths(raw json.RawMessage) []string {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		if trimmed := strings.TrimSpace(single); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func resolveComponentPath(installPath, candidate string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", false
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(installPath, filepath.FromSlash(candidate))
	}
	candidate = filepath.Clean(candidate)

	rel, err := filepath.Rel(installPath, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return candidate, true
}
