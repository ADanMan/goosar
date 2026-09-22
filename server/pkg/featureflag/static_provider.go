package featureflag

import (
	"context"
	"slices"
	"sync"
)

type Rule struct {
	Default bool

	Variant string

	Allow []string

	AllowBy string

	Deny []string

	DenyBy string

	Percent *PercentRollout
}

type PercentRollout struct {
	Percent int

	By string
}

type StaticProvider struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

func NewStaticProvider() *StaticProvider {
	return &StaticProvider{rules: map[string]Rule{}}
}

func (*StaticProvider) Name() string { return "static" }

func (p *StaticProvider) Set(key string, rule Rule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules[key] = rule
}

func (p *StaticProvider) LoadRules(rules map[string]Rule) {
	clone := make(map[string]Rule, len(rules))
	for k, v := range rules {
		clone[k] = v
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = clone
}

func (p *StaticProvider) Keys() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.rules))
	for k := range p.rules {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (p *StaticProvider) Lookup(ctx context.Context, key string) (Decision, bool) {
	p.mu.RLock()
	rule, ok := p.rules[key]
	p.mu.RUnlock()
	if !ok {
		return Decision{}, false
	}
	ec := EvalContextFrom(ctx)
	return evaluateRule(key, rule, ec), true
}

func evaluateRule(key string, rule Rule, ec EvalContext) Decision {

	denyBy := orDefault(rule.DenyBy, "user_id")
	if len(rule.Deny) > 0 {
		if v, ok := ec.Lookup(denyBy); ok && slices.Contains(rule.Deny, v) {
			return decisionFromRule(key, rule, false, ReasonStatic)
		}
	}

	allowBy := orDefault(rule.AllowBy, "user_id")
	if len(rule.Allow) > 0 {
		if v, ok := ec.Lookup(allowBy); ok && slices.Contains(rule.Allow, v) {
			return decisionFromRule(key, rule, true, ReasonStatic)
		}
	}

	if rule.Percent != nil {
		by := orDefault(rule.Percent.By, "user_id")
		identifier, _ := ec.Lookup(by)

		if inPercent(key, identifier, rule.Percent.Percent) {
			return decisionFromRule(key, rule, true, ReasonPercent)
		}
		return decisionFromRule(key, rule, false, ReasonPercent)
	}

	return decisionFromRule(key, rule, rule.Default, ReasonStatic)
}

func decisionFromRule(key string, rule Rule, enabled bool, reason Reason) Decision {

	variant := boolToVariant(enabled)
	if enabled && rule.Variant != "" {
		variant = rule.Variant
	}
	return Decision{
		Key:     key,
		Enabled: enabled,
		Variant: variant,
		Reason:  reason,
		Source:  "static",
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
