// Пакет runtimeregistry загружает реестр runtime: соответствие нейтральных кодов
// (runtime-a..runtime-r) реальным CLI, отображаемым названиям и каталогам моделей.
// runtimes.yaml — единственное место с реальными именами вендоров.
package runtimeregistry

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed runtimes.yaml
var defaultData []byte

type ThinkingLevelSpec struct {
	Value       string `yaml:"value"`
	Label       string `yaml:"label"`
	Description string `yaml:"description,omitempty"`
}

type ThinkingSpec struct {
	SupportedLevels []ThinkingLevelSpec `yaml:"supportedLevels"`

	DefaultLevel string `yaml:"defaultLevel,omitempty"`
}

type ModelEntry struct {
	ID      string `yaml:"id"`
	RealID  string `yaml:"realId"`
	Label   string `yaml:"label"`
	Default bool   `yaml:"default"`

	ServiceTiers []string `yaml:"serviceTiers,omitempty"`

	Thinking *ThinkingSpec `yaml:"thinking,omitempty"`

	PricingInputPerMTok  float64 `yaml:"pricingInputPerMTok,omitempty"`
	PricingOutputPerMTok float64 `yaml:"pricingOutputPerMTok,omitempty"`
}

type MCPConfigSpec struct {
	FileEnvVar string `yaml:"fileEnvVar,omitempty"`

	BaseEnvVar string `yaml:"baseEnvVar,omitempty"`

	BaseDefaultSubpath string `yaml:"baseDefaultSubpath,omitempty"`

	Subpath string `yaml:"subpath,omitempty"`

	Format string `yaml:"format"`

	Key string `yaml:"key"`
}

type PluginRegistrySpec struct {
	SettingsSubpath string `yaml:"settingsSubpath"`

	SettingsEnabledKey string `yaml:"settingsEnabledKey"`

	InstalledRegistrySubpath string `yaml:"installedRegistrySubpath"`

	ManifestSubpath string `yaml:"manifestSubpath"`

	DefaultSkillsSubpath    string `yaml:"defaultSkillsSubpath,omitempty"`
	DefaultMCPConfigSubpath string `yaml:"defaultMcpConfigSubpath,omitempty"`

	ExtendsToCode string `yaml:"extendsToCode,omitempty"`
}

type TaskHomeSpec struct {
	EnvVar string `yaml:"envVar,omitempty"`

	PosixSubpath string `yaml:"posixSubpath,omitempty"`

	WindowsAppDataSubpath string `yaml:"windowsAppDataSubpath,omitempty"`
}

type Descriptor struct {
	Code        string       `yaml:"code"`
	CLIName     string       `yaml:"cliName"`
	DisplayName string       `yaml:"displayName"`
	Models      []ModelEntry `yaml:"models"`

	HomeEnvVar         string `yaml:"homeEnvVar,omitempty"`
	HomeDefaultSubpath string `yaml:"homeDefaultSubpath,omitempty"`

	BundledExecutablePaths []string `yaml:"bundledExecutablePaths,omitempty"`

	SkillsHomeSubpath string `yaml:"skillsHomeSubpath,omitempty"`

	SkillsCanDisable bool `yaml:"skillsCanDisable,omitempty"`

	SkillsWorkdirSubpath string `yaml:"skillsWorkdirSubpath,omitempty"`

	SkillsAutoDiscovered bool `yaml:"skillsAutoDiscovered,omitempty"`

	ContextFilename string `yaml:"contextFilename,omitempty"`

	TaskHome *TaskHomeSpec `yaml:"taskHome,omitempty"`

	MCPConfig *MCPConfigSpec `yaml:"mcpConfig,omitempty"`

	PluginRegistry *PluginRegistrySpec `yaml:"pluginRegistry,omitempty"`
}

func (d Descriptor) ResolveHome(osHome string, getenv func(string) string) string {
	if d.HomeEnvVar != "" && getenv != nil {
		if v := strings.TrimSpace(getenv(d.HomeEnvVar)); v != "" {
			return v
		}
	}
	if d.HomeDefaultSubpath != "" {
		return filepath.Join(osHome, filepath.FromSlash(d.HomeDefaultSubpath))
	}
	return osHome
}

func (d Descriptor) SkillsRoot(osHome string, getenv func(string) string) (string, bool) {
	if d.SkillsHomeSubpath == "" {
		return "", false
	}
	return filepath.Join(d.ResolveHome(osHome, getenv), filepath.FromSlash(d.SkillsHomeSubpath)), true
}

func (d Descriptor) ResolvedBundledExecutablePaths(osHome string) []string {
	if len(d.BundledExecutablePaths) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.BundledExecutablePaths))
	for _, p := range d.BundledExecutablePaths {
		native := filepath.FromSlash(p)
		if filepath.IsAbs(native) {
			out = append(out, native)
			continue
		}
		out = append(out, filepath.Join(osHome, native))
	}
	return out
}

func (d Descriptor) MCPConfigPath(osHome string, getenv func(string) string) (string, bool) {
	spec := d.MCPConfig
	if spec == nil {
		return "", false
	}
	if spec.FileEnvVar != "" && getenv != nil {
		if v := strings.TrimSpace(getenv(spec.FileEnvVar)); v != "" {
			return v, true
		}
	}
	if spec.Subpath == "" {
		return "", false
	}
	base := ""
	switch {
	case spec.BaseEnvVar != "" && getenv != nil && strings.TrimSpace(getenv(spec.BaseEnvVar)) != "":
		base = strings.TrimSpace(getenv(spec.BaseEnvVar))
	case spec.BaseDefaultSubpath != "":
		base = filepath.Join(osHome, filepath.FromSlash(spec.BaseDefaultSubpath))
	default:
		base = d.ResolveHome(osHome, getenv)
	}
	return filepath.Join(base, filepath.FromSlash(spec.Subpath)), true
}

type file struct {
	Runtimes []Descriptor `yaml:"runtimes"`
}

type Registry struct {
	byCode    map[string]Descriptor
	byCLIName map[string]Descriptor
}

func parse(data []byte) (*Registry, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("runtimeregistry: parse: %w", err)
	}
	reg := &Registry{byCode: map[string]Descriptor{}, byCLIName: map[string]Descriptor{}}
	for _, d := range f.Runtimes {
		reg.byCode[d.Code] = d
		reg.byCLIName[d.CLIName] = d
	}
	return reg, nil
}

func LoadDefault() (*Registry, error) {
	if path := os.Getenv("GOOSAR_RUNTIME_CONFIG_PATH"); path != "" {
		return Load(path)
	}
	return parse(defaultData)
}

func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("runtimeregistry: read %s: %w", path, err)
	}
	return parse(data)
}

func (r *Registry) ByCode(code string) (Descriptor, bool) {
	d, ok := r.byCode[code]
	return d, ok
}

func (r *Registry) ByCLIName(name string) (Descriptor, bool) {
	d, ok := r.byCLIName[name]
	return d, ok
}

func (r *Registry) Codes() []string {
	codes := make([]string, 0, len(r.byCode))
	for c := range r.byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}
