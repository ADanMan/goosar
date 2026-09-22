package channel

import (
	"context"
	"encoding/json"
)

type Type string

const (
	TypeFeishu Type = "feishu"
)

type Channel interface {
	Type() Type

	Connect(ctx context.Context) error

	Disconnect(ctx context.Context) error

	Send(ctx context.Context, out OutboundMessage) (SendResult, error)

	Capabilities() Capability
}

type Config struct {
	Type Type
	Raw  json.RawMessage

	Handler InboundHandler
}

type Factory func(cfg Config) (Channel, error)
