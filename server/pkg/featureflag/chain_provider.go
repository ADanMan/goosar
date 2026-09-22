package featureflag

import "context"

type ChainProvider struct {
	providers []Provider
}

func NewChainProvider(providers ...Provider) *ChainProvider {
	cp := &ChainProvider{providers: make([]Provider, 0, len(providers))}
	for _, p := range providers {
		if p != nil {
			cp.providers = append(cp.providers, p)
		}
	}
	return cp
}

func (*ChainProvider) Name() string { return "chain" }

func (cp *ChainProvider) Lookup(ctx context.Context, key string) (Decision, bool) {
	for _, p := range cp.providers {
		if d, ok := p.Lookup(ctx, key); ok {
			return d, true
		}
	}
	return Decision{}, false
}

func (cp *ChainProvider) Providers() []Provider {
	out := make([]Provider, len(cp.providers))
	copy(out, cp.providers)
	return out
}
