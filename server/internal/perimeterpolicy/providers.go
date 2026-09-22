package perimeterpolicy

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/pkg/agent"
)

const EnvAllowedProviders = "GOOSAR_ALLOWED_PROVIDERS"

var perimeterDefaultProviders = []string{"runtime-j"}

type ProviderPolicy struct {
	allowed map[string]struct{}
}

func ProviderPolicyFromEnv(profile deliveryprofile.Profile) (*ProviderPolicy, error) {
	return ParseProviderPolicy(profile, os.Getenv(EnvAllowedProviders))
}

func ParseProviderPolicy(profile deliveryprofile.Profile, raw string) (*ProviderPolicy, error) {
	var entries []string
	for _, part := range strings.Split(raw, ",") {
		slug := strings.ToLower(strings.TrimSpace(part))
		if slug == "" {
			continue
		}
		if !agent.IsSupportedType(slug) {
			return nil, fmt.Errorf("invalid %s entry %q: must be one of %s", EnvAllowedProviders, slug, strings.Join(agent.SupportedTypes, ", "))
		}
		entries = append(entries, slug)
	}
	if profile.IsPerimeter() {
		entries = append(entries, perimeterDefaultProviders...)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(entries))
	for _, slug := range entries {
		allowed[slug] = struct{}{}
	}
	return &ProviderPolicy{allowed: allowed}, nil
}

func (p *ProviderPolicy) Allows(provider string) bool {
	if p == nil {
		return true
	}
	_, ok := p.allowed[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}

func (p *ProviderPolicy) AllowedList() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.allowed))
	for slug := range p.allowed {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}
