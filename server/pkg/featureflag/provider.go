package featureflag

import "context"

type Reason string

const (
	ReasonStatic Reason = "static"

	ReasonPercent Reason = "percent"

	ReasonOverride Reason = "override"

	ReasonDefault Reason = "default"

	ReasonError Reason = "error"
)

type Decision struct {
	Key string

	Enabled bool

	Variant string

	Reason Reason

	Source string
}

type Provider interface {
	Lookup(ctx context.Context, key string) (decision Decision, found bool)

	Name() string
}
