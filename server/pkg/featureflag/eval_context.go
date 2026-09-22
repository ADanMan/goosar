package featureflag

import "context"

type EvalContext struct {
	UserID string

	WorkspaceID string

	Attributes map[string]string
}

func (ec EvalContext) Lookup(name string) (string, bool) {
	switch name {
	case "user_id":
		if ec.UserID != "" {
			return ec.UserID, true
		}
		return "", false
	case "workspace_id":
		if ec.WorkspaceID != "" {
			return ec.WorkspaceID, true
		}
		return "", false
	}
	if ec.Attributes == nil {
		return "", false
	}
	v, ok := ec.Attributes[name]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

type evalContextKey struct{}

func WithEvalContext(parent context.Context, ec EvalContext) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, evalContextKey{}, ec)
}

func EvalContextFrom(ctx context.Context) EvalContext {
	if ctx == nil {
		return EvalContext{}
	}
	v, ok := ctx.Value(evalContextKey{}).(EvalContext)
	if !ok {
		return EvalContext{}
	}
	return v
}
