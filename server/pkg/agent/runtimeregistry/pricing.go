package runtimeregistry

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModelPriceEntry struct {
	Key            string  `yaml:"key"`
	Provider       string  `yaml:"provider"`
	Model          string  `yaml:"model"`
	InputPerM      float64 `yaml:"inputPerM"`
	CacheReadPerM  float64 `yaml:"cacheReadPerM"`
	CacheWritePerM float64 `yaml:"cacheWritePerM"`
	OutputPerM     float64 `yaml:"outputPerM"`
}

type ModelPriceAliasRule struct {
	Pattern  string `yaml:"pattern"`
	PriceKey string `yaml:"priceKey"`
}

//go:embed pricing.yaml
var defaultPricingData []byte

type pricingFile struct {
	ModelPrices []ModelPriceEntry     `yaml:"modelPrices"`
	AliasRules  []ModelPriceAliasRule `yaml:"aliasRules"`
}

type compiledPriceAliasRule struct {
	re       *regexp.Regexp
	priceKey string
}

type ModelPricing struct {
	byKey map[string]ModelPriceEntry
	rules []compiledPriceAliasRule
}

func parsePricing(data []byte) (*ModelPricing, error) {
	var f pricingFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("runtimeregistry: parse pricing: %w", err)
	}
	mp := &ModelPricing{
		byKey: make(map[string]ModelPriceEntry, len(f.ModelPrices)),
		rules: make([]compiledPriceAliasRule, 0, len(f.AliasRules)),
	}
	for _, entry := range f.ModelPrices {
		mp.byKey[entry.Key] = entry
	}
	for _, rule := range f.AliasRules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("runtimeregistry: compile pricing alias pattern %q: %w", rule.Pattern, err)
		}
		mp.rules = append(mp.rules, compiledPriceAliasRule{re: re, priceKey: rule.PriceKey})
	}
	return mp, nil
}

func LoadModelPricing() (*ModelPricing, error) {
	return parsePricing(defaultPricingData)
}

func (mp *ModelPricing) PriceForAlias(model string) (ModelPriceEntry, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, rule := range mp.rules {
		if rule.re.MatchString(model) {
			price, ok := mp.byKey[rule.priceKey]
			return price, ok
		}
	}
	return ModelPriceEntry{}, false
}
