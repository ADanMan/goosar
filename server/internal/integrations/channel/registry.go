package channel

import (
	"fmt"
	"sort"
	"sync"
)

var ErrUnknownType = fmt.Errorf("channel: no factory registered for type")

type Registry struct {
	mu        sync.RWMutex
	factories map[Type]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[Type]Factory)}
}

func (r *Registry) Register(t Type, factory Factory) {
	if t == "" || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[t] = factory
}

func (r *Registry) Lookup(t Type) (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[t]
	return factory, ok
}

func (r *Registry) Build(cfg Config) (Channel, error) {
	factory, ok := r.Lookup(cfg.Type)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, cfg.Type)
	}
	return factory(cfg)
}

func (r *Registry) Types() []Type {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Type, 0, len(r.factories))
	for t := range r.factories {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
