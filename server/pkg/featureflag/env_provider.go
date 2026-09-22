package featureflag

import (
	"context"
	"os"
	"strconv"
	"strings"
)

type EnvProvider struct {
	Prefix string

	lookup func(string) (string, bool)
}

func NewEnvProvider(prefix string) *EnvProvider {
	return &EnvProvider{Prefix: prefix, lookup: os.LookupEnv}
}

func (*EnvProvider) Name() string { return "env" }

func (p *EnvProvider) Lookup(ctx context.Context, key string) (Decision, bool) {
	envName := p.Prefix + flagKeyToEnv(key)
	get := p.lookup
	if get == nil {
		get = os.LookupEnv
	}
	raw, present := get(envName)
	if !present {
		return Decision{}, false
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Decision{
			Key:     key,
			Enabled: false,
			Variant: "off",
			Reason:  ReasonStatic,
			Source:  "env",
		}, true
	}

	if strings.HasSuffix(trimmed, "%") {
		pctStr := strings.TrimSuffix(trimmed, "%")
		pct, err := strconv.Atoi(strings.TrimSpace(pctStr))
		if err != nil || pct < 0 || pct > 100 {
			return Decision{
				Key:     key,
				Enabled: false,
				Variant: "off",
				Reason:  ReasonError,
				Source:  "env",
			}, true
		}
		ec := EvalContextFrom(ctx)
		ident, _ := ec.Lookup("user_id")
		enabled := inPercent(key, ident, pct)
		return Decision{
			Key:     key,
			Enabled: enabled,
			Variant: boolToVariant(enabled),
			Reason:  ReasonPercent,
			Source:  "env",
		}, true
	}

	switch strings.ToLower(trimmed) {
	case "true", "on", "1", "yes":
		return Decision{
			Key:     key,
			Enabled: true,
			Variant: "on",
			Reason:  ReasonStatic,
			Source:  "env",
		}, true
	case "false", "off", "0", "no":
		return Decision{
			Key:     key,
			Enabled: false,
			Variant: "off",
			Reason:  ReasonStatic,
			Source:  "env",
		}, true
	}

	return Decision{
		Key:     key,
		Enabled: true,
		Variant: trimmed,
		Reason:  ReasonStatic,
		Source:  "env",
	}, true
}

func flagKeyToEnv(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	prevUnderscore := false
	for _, r := range key {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
