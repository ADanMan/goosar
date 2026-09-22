package featureflag

import (
	"context"
	"log/slog"
)

type Service struct {
	provider Provider
	logger   *slog.Logger
}

type Option func(*Service)

func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

func NewService(provider Provider, opts ...Option) *Service {
	s := &Service{provider: provider}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) IsEnabled(ctx context.Context, key string, defaultVal bool) bool {
	return s.Decision(ctx, key, defaultVal).Enabled
}

func (s *Service) Variant(ctx context.Context, key string, defaultVal string) string {
	d := s.decisionWithVariantDefault(ctx, key, defaultVal)
	return d.Variant
}

func (s *Service) Decision(ctx context.Context, key string, defaultVal bool) Decision {
	if s == nil || s.provider == nil {
		return defaultDecision(key, boolToVariant(defaultVal), defaultVal)
	}
	d, ok := s.provider.Lookup(ctx, key)
	if !ok {
		return defaultDecision(key, boolToVariant(defaultVal), defaultVal)
	}
	if d.Reason == ReasonError && s.logger != nil {
		s.logger.WarnContext(ctx, "feature flag provider returned an error decision",
			slog.String("key", key),
			slog.String("source", d.Source),
		)
	}
	d.Key = key
	return d
}

func (s *Service) decisionWithVariantDefault(ctx context.Context, key, defaultVariant string) Decision {
	if s == nil || s.provider == nil {
		return defaultDecision(key, defaultVariant, variantEnabled(defaultVariant))
	}
	d, ok := s.provider.Lookup(ctx, key)
	if !ok {
		return defaultDecision(key, defaultVariant, variantEnabled(defaultVariant))
	}
	d.Key = key
	return d
}

func (s *Service) Provider() Provider {
	if s == nil {
		return nil
	}
	return s.provider
}

func defaultDecision(key, variant string, enabled bool) Decision {
	return Decision{
		Key:     key,
		Enabled: enabled,
		Variant: variant,
		Reason:  ReasonDefault,
		Source:  "default",
	}
}

func boolToVariant(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func variantEnabled(v string) bool {
	switch v {
	case "", "off", "false", "0":
		return false
	}
	return true
}
