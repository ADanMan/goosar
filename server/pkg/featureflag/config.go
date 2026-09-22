package featureflag

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const EnvFlagFile = "GOOSAR_FEATURE_FLAGS_FILE"

const EnvOverridePrefix = "FF_"

type ruleConfig struct {
	Default *bool          `yaml:"default,omitempty"`
	Variant string         `yaml:"variant,omitempty"`
	Allow   []string       `yaml:"allow,omitempty"`
	AllowBy string         `yaml:"allow_by,omitempty"`
	Deny    []string       `yaml:"deny,omitempty"`
	DenyBy  string         `yaml:"deny_by,omitempty"`
	Percent *percentConfig `yaml:"percent,omitempty"`
}

type percentConfig struct {
	Percent int    `yaml:"percent"`
	By      string `yaml:"by,omitempty"`
}

func (rc ruleConfig) toRule() Rule {
	r := Rule{
		Variant: rc.Variant,
		Allow:   rc.Allow,
		AllowBy: rc.AllowBy,
		Deny:    rc.Deny,
		DenyBy:  rc.DenyBy,
	}
	if rc.Default != nil {
		r.Default = *rc.Default
	}
	if rc.Percent != nil {
		r.Percent = &PercentRollout{
			Percent: rc.Percent.Percent,
			By:      rc.Percent.By,
		}
	}
	return r
}

func LoadRulesFromYAMLFile(path string) (map[string]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("featureflag: read %s: %w", path, err)
	}
	return parseRulesYAML(data)
}

func parseRulesYAML(data []byte) (map[string]Rule, error) {

	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]Rule{}, nil
	}
	var raw map[string]ruleConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("featureflag: parse: %w", err)
	}
	out := make(map[string]Rule, len(raw))
	for key, rc := range raw {
		out[key] = rc.toRule()
	}
	return out, nil
}

func NewServiceFromEnv(opts ...Option) (*Service, error) {
	var providers []Provider
	providers = append(providers, NewEnvProvider(EnvOverridePrefix))

	path := strings.TrimSpace(os.Getenv(EnvFlagFile))
	var loadedCount int
	if path != "" {
		rules, err := LoadRulesFromYAMLFile(path)
		if err != nil {
			return nil, err
		}
		sp := NewStaticProvider()
		sp.LoadRules(rules)
		providers = append(providers, sp)
		loadedCount = len(rules)
	}

	svc := NewService(NewChainProvider(providers...), opts...)
	if svc.logger != nil {
		svc.logger.Info("feature flags initialised",
			slog.String("file", path),
			slog.Int("rules", loadedCount),
			slog.String("env_prefix", EnvOverridePrefix),
		)
	}
	return svc, nil
}
